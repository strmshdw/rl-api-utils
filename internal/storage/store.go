package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Sentinel errors for the storage layer.
var (
	ErrMatchNotFound         = errors.New("match not found")
	ErrAuthStateNotFound     = errors.New("auth state not found")
	ErrInvalidGUID           = errors.New("match GUID cannot be empty")
	ErrStoreClosed           = errors.New("state store is closed")
	ErrPlayerNotFound        = errors.New("player not found")
	ErrMatchAlreadyProcessed = errors.New("match outcome already processed")
)

// DownloadStatus represents the synchronization status of a replay download.
type DownloadStatus string

const (
	DownloadPending     DownloadStatus = "PENDING"
	DownloadDownloading DownloadStatus = "DOWNLOADING"
	DownloadDownloaded  DownloadStatus = "DOWNLOADED"
	DownloadFailed      DownloadStatus = "FAILED"
	DownloadSkipped     DownloadStatus = "SKIPPED"
)

// UploadStatus represents the synchronization status of a replay upload to Ballchasing.
type UploadStatus string

const (
	UploadPending   UploadStatus = "PENDING"
	UploadUploading UploadStatus = "UPLOADING"
	UploadUploaded  UploadStatus = "UPLOADED"
	UploadDuplicate UploadStatus = "DUPLICATE"
	UploadFailed    UploadStatus = "FAILED"
)

// StatsSyncStatus represents the synchronization status of post-upload Ballchasing stats.
type StatsSyncStatus string

const (
	StatsSyncPending StatsSyncStatus = "PENDING"
	StatsSyncSyncing StatsSyncStatus = "SYNCING"
	StatsSyncSynced  StatsSyncStatus = "SYNCED"
	StatsSyncFailed  StatsSyncStatus = "FAILED"
	StatsSyncSkipped StatsSyncStatus = "SKIPPED"
)

// Default retry policy constants.
const (
	DefaultMaxRetries  = 3
	DefaultBaseBackoff = 30 * time.Second
	DefaultMaxBackoff  = 15 * time.Minute
)

// MatchRecord represents the persistent state of a single Rocket League match.
type MatchRecord struct {
	MatchGUID            string          `json:"match_guid"`
	RecordStartTimestamp int64           `json:"record_start_timestamp"`
	MapName              string          `json:"map_name"`
	Playlist             int             `json:"playlist"`
	ReplayURL            string          `json:"replay_url"`
	DownloadStatus       DownloadStatus  `json:"download_status"`
	LocalFilePath        string          `json:"local_file_path"`
	DownloadedAt         *time.Time      `json:"downloaded_at,omitempty"`
	UploadStatus         UploadStatus    `json:"upload_status"`
	BallchasingID        string          `json:"ballchasing_id"`
	BallchasingURL       string          `json:"ballchasing_url"`
	UploadedAt           *time.Time      `json:"uploaded_at,omitempty"`
	RetryCount           int             `json:"retry_count"`
	LastError            string          `json:"last_error"`
	BlueScore            int             `json:"blue_score"`
	OrangeScore          int             `json:"orange_score"`
	WinnerTeam           *int            `json:"winner_team,omitempty"`
	ReplayStatsJSON      string          `json:"replay_stats_json,omitempty"`
	SyncedAt             *time.Time      `json:"synced_at,omitempty"`
	StatsSyncStatus      StatsSyncStatus `json:"stats_sync_status"`
	StatsRetryCount      int             `json:"stats_retry_count"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// MatchPlayerRecord represents an individual player's participation, box score, and telemetry in a match.
type MatchPlayerRecord struct {
	MatchGUID string `json:"match_guid"`
	PlayerID  string `json:"player_id"`
	TeamNum   int    `json:"team_num"` // 0 = Blue, 1 = Orange
	IsLocal   bool   `json:"is_local"`
	Won       *bool  `json:"won,omitempty"` // nil = unknown/draw, true = win, false = loss
	Score     int    `json:"score"`
	Goals     int    `json:"goals"`
	Assists   int    `json:"assists"`
	Saves     int    `json:"saves"`
	Shots     int    `json:"shots"`
	Demos     int    `json:"demos"`
	CarName   string `json:"car_name"`
	StatsJSON string `json:"stats_json,omitempty"`
}

// MMRHistoryRecord represents a point-in-time competitive MMR snapshot.
type MMRHistoryRecord struct {
	ID         int64     `json:"id"`
	RecordedAt time.Time `json:"recorded_at"`
	MatchGUID  string    `json:"match_guid,omitempty"`
	PlaylistID int       `json:"playlist_id"`
	MMR        int       `json:"mmr"`
	MMRChange  int       `json:"mmr_change"`
	Tier       int       `json:"tier"`
	Division   int       `json:"division"`
}

// PlayerMatchFilter specifies filtering and pagination for a player's match history.
type PlayerMatchFilter struct {
	Role     string `json:"role"`     // "all", "teammate", "opponent"
	Playlist int    `json:"playlist"` // 0 = all playlists
	Result   string `json:"result"`   // "all", "victory", "defeat"
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

// PlayerMatchDetail combines the parent match record with the player's specific performance.
type PlayerMatchDetail struct {
	MatchRecord
	Stats       *ReplayMatchStats `json:"stats,omitempty"`
	PlayerStats MatchPlayerRecord `json:"player_stats"`
	Role        string            `json:"role"`   // "teammate" or "opponent" relative to local player
	Result      string            `json:"result"` // "victory", "defeat", "draw"
}

// ReplayPlayerStats contains per-player statistics extracted from a replay or ballchasing record.
type ReplayPlayerStats struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	OnlineID string `json:"online_id"`
	Team     int    `json:"team"` // 0 = Blue, 1 = Orange
	Score    int    `json:"score"`
	Goals    int    `json:"goals"`
	Assists  int    `json:"assists"`
	Saves    int    `json:"saves"`
	Shots    int    `json:"shots"`
	Demos    int    `json:"demos,omitempty"`
	IsBot    bool   `json:"is_bot"`
}

// ReplayMatchStats holds end-of-game replay statistics to enrich local match records.
type ReplayMatchStats struct {
	MatchGUID      string              `json:"match_guid"`
	MapName        string              `json:"map_name"`
	Playlist       int                 `json:"playlist"`
	BlueScore      int                 `json:"blue_score"`
	OrangeScore    int                 `json:"orange_score"`
	WinnerTeam     *int                `json:"winner_team,omitempty"`
	Duration       int                 `json:"duration"`
	Overtime       bool                `json:"overtime"`
	Date           string              `json:"date"`
	Players        []ReplayPlayerStats `json:"players"`
	BallchasingID  string              `json:"ballchasing_id,omitempty"`
	BallchasingURL string              `json:"ballchasing_url,omitempty"`
	RawJSON        string              `json:"raw_json,omitempty"`
}

// MatchFilter specifies search and pagination criteria for historical matches.
type MatchFilter struct {
	Playlist int    `json:"playlist"`
	Query    string `json:"query"`
	Result   string `json:"result,omitempty"` // "all", "victory", "defeat", "draw"
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

// PlayerDirectoryFilter specifies criteria for filtering and sorting the player directory.
type PlayerDirectoryFilter struct {
	Query    string `json:"query"`
	Platform string `json:"platform"`
	Role     string `json:"role"`  // "all", "frequent", "teammate", "opponent", "synergy"
	Sort     string `json:"sort"`  // "last_seen", "match_count", "win_rate", "name"
	Order    string `json:"order"` // "asc", "desc"
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

// GlobalSpotlights contains dataset-wide player highlights.
type GlobalSpotlights struct {
	BestTeammate  *SpotlightSummary `json:"best_teammate"`
	MostFaced     *SpotlightSummary `json:"most_faced"`
	ToughestRival *SpotlightSummary `json:"toughest_rival"`
}

// SpotlightSummary describes a single spotlight player.
type SpotlightSummary struct {
	Player *PlayerSummary `json:"player"`
	Value  string         `json:"value"`
	Sub    string         `json:"sub"`
	Tone   string         `json:"tone"` // "win", "neutral", "loss"
}

// HistoricalMatchRecord represents a historical match enriched with replay and outcome stats.
type HistoricalMatchRecord struct {
	MatchRecord
	Stats  *ReplayMatchStats `json:"stats,omitempty"`
	Result string            `json:"result,omitempty"` // "victory", "defeat", "draw", "unknown"
}

// AuthRecord represents persisted authentication tokens and metadata for an auth provider.
type AuthRecord struct {
	Provider     string    `json:"provider"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id"`
	DisplayName  string    `json:"display_name"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PlayerRecord represents the persistent profile of an encountered player.
type PlayerRecord struct {
	PlayerID    string    `json:"player_id"`
	Platform    string    `json:"platform"`
	PlayerName  string    `json:"player_name"`
	RanksJSON   string    `json:"ranks_json"`
	Notes       string    `json:"notes"`
	Tags        []string  `json:"tags"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// PlayerMatchup represents historical head-to-head records with/against a player in a specific playlist.
type PlayerMatchup struct {
	PlayerID         string    `json:"player_id"`
	PlaylistID       int       `json:"playlist_id"`
	WinsAsTeammate   int       `json:"wins_as_teammate"`
	LossesAsTeammate int       `json:"losses_as_teammate"`
	WinsAsOpponent   int       `json:"wins_as_opponent"`
	LossesAsOpponent int       `json:"losses_as_opponent"`
	TotalMatches     int       `json:"total_matches"`
	LastPlayedAt     time.Time `json:"last_played_at"`
}

// PlayerSummary embeds PlayerRecord with aggregate matchup totals across all playlists.
type PlayerSummary struct {
	PlayerRecord
	TotalWinsAsTeammate   int `json:"total_wins_as_teammate"`
	TotalLossesAsTeammate int `json:"total_losses_as_teammate"`
	TotalWinsAsOpponent   int `json:"total_wins_as_opponent"`
	TotalLossesAsOpponent int `json:"total_losses_as_opponent"`
	TotalMatches          int `json:"total_matches"`
}

// PlayerOutcome specifies the result for a single player in a concluded match.
type PlayerOutcome struct {
	PlayerID   string `json:"player_id"`
	Platform   string `json:"platform,omitempty"`
	PlayerName string `json:"player_name,omitempty"`
	IsTeammate bool   `json:"is_teammate"`
	Won        bool   `json:"won"`
}

// ParseReplayDate attempts to parse various date formats present in Rocket League replays and Ballchasing APIs.
func ParseReplayDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15-04-05",
		"2006-01-02:15-04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// StateStore defines the persistence interface required by the syncer and daemon.
type StateStore interface {
	// GetMatch retrieves a single match record by its GUID.
	// Returns ErrMatchNotFound if the record does not exist.
	GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error)

	// ListPendingDownloads returns all matches ready for replay downloading
	// (download_status = PENDING and non-empty replay_url), ordered by timestamp ASC.
	ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error)

	// ListPendingUploads returns all matches ready for upload
	// (download_status = DOWNLOADED and upload_status = PENDING and non-empty local path).
	ListPendingUploads(ctx context.Context) ([]*MatchRecord, error)

	// UpsertDiscoveredMatches inserts newly discovered matches without clobbering
	// existing download/upload statuses or metadata.
	UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error

	// Status transition methods
	MarkDownloading(ctx context.Context, matchGUID string) error
	MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
	MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
	MarkUploading(ctx context.Context, matchGUID string) error
	MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error

	// RecoverInFlight resets in-flight transitions (DOWNLOADING -> PENDING, UPLOADING -> PENDING)
	// after an abnormal daemon termination or crash.
	RecoverInFlight(ctx context.Context) error

	// Auth token persistence
	SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error
	GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)

	// Close closes the underlying store connection.
	Close() error

	// --- Player Tracking Extensions (Milestone M1) ---

	// UpsertPlayer inserts a new player or updates an existing player's platform, name, and last_seen_at.
	// If the player already exists, first_seen_at is preserved and ranks_json is preserved unless updated.
	UpsertPlayer(ctx context.Context, player *PlayerRecord) error

	// GetPlayer retrieves a single player profile by PlayerID.
	// Returns ErrPlayerNotFound if the player does not exist.
	GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error)

	// ListPlayers returns a paginated list of players ordered by last_seen_at DESC, player_id ASC.
	ListPlayers(ctx context.Context, limit, offset int) ([]*PlayerRecord, error)

	// ListPlayerSummaries returns paginated players with aggregated win/loss statistics across all playlists,
	// ordered by last_seen_at DESC, player_id ASC.
	ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error)

	// SearchPlayerSummaries searches players matching a query string across player_name and player_id
	// (case-insensitive substring match), optionally filtered by platform (case-insensitive; "all" or "" means
	// all platforms), returning paginated PlayerSummary records and the total count of matching records across all pages.
	// Results are ordered by last_seen_at DESC, player_id ASC.
	SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error)

	// FilterPlayerSummaries searches, filters, and sorts players across the entire dataset (SPEC-05).
	FilterPlayerSummaries(ctx context.Context, filter PlayerDirectoryFilter) ([]*PlayerSummary, int, error)

	// GetPlayerSpotlights computes dataset-wide global spotlights (Best Teammate, Most Faced, Toughest Rival) (SPEC-05).
	GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error)

	// GetLocalPlayerIDs returns all known local player account and platform IDs from auth and match records (SPEC-03).
	GetLocalPlayerIDs(ctx context.Context) ([]string, error)

	// UpdatePlayerRanks updates the ranks_json payload and last_seen_at for a player.
	// Returns ErrPlayerNotFound if the player does not exist.
	UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error

	// RecordMatchResults atomically records match outcomes for all players in a match and registers
	// the match GUID in processed_match_outcomes. If the match was already processed, it returns
	// ErrMatchAlreadyProcessed and performs no updates to player or matchup records.
	// Optional matchTime preserves the original replay timestamp rather than wall-clock execution time.
	RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome, matchTime ...time.Time) error

	// GetPlayerMatchup retrieves the head-to-head record for a player in a specific playlist.
	// If no record exists for that playlist, returns a zeroed PlayerMatchup (TotalMatches=0) and nil error.
	GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*PlayerMatchup, error)

	// GetPlayerMatchups retrieves all playlist matchups for a player ordered by playlist_id ASC.
	GetPlayerMatchups(ctx context.Context, playerID string) ([]*PlayerMatchup, error)

	// SyncReplayStats enriches or upserts a match record with end-of-game replay statistics,
	// keyed by MatchGUID.
	SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error

	// ListHistoricalMatches returns paginated historical matches matching the filter,
	// ordered by record_start_timestamp DESC, match_guid ASC.
	ListHistoricalMatches(ctx context.Context, filter MatchFilter) ([]*HistoricalMatchRecord, int, error)

	// --- Match Players (SPEC-02) ---
	SaveMatchPlayers(ctx context.Context, matchGUID string, players []*MatchPlayerRecord) error
	GetMatchPlayers(ctx context.Context, matchGUID string) ([]*MatchPlayerRecord, error)
	ListPlayerMatches(ctx context.Context, playerID string, filter PlayerMatchFilter) ([]*PlayerMatchDetail, int, error)

	// --- MMR History (SPEC-08) ---
	RecordMMR(ctx context.Context, record *MMRHistoryRecord) error
	ListMMRHistory(ctx context.Context, playlistID int, limit int) ([]*MMRHistoryRecord, error)

	// --- Player Notes & Tags (SPEC-07) ---
	UpdatePlayerNotesAndTags(ctx context.Context, playerID string, notes string, tags []string) error

	// --- Match Stats Synchronization & Retry (SPEC-01, SPEC-04) ---
	ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*MatchRecord, error)
	MarkStatsSyncing(ctx context.Context, matchGUID string) error
	MarkStatsSynced(ctx context.Context, matchGUID string) error
	MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error
	ResetMatchRetry(ctx context.Context, matchGUID string) error
}

// NewStore initializes a StateStore backend based on the database path or URI.
// If dbPath ends in ".json", a JSONStore is returned; otherwise SQLiteStore is used.
func NewStore(dbPath string) (StateStore, error) {
	if strings.HasSuffix(strings.ToLower(dbPath), ".json") {
		return NewJSONStore(dbPath)
	}
	return NewSQLiteStore(dbPath)
}

// wrapContextErr wraps context cancellation and deadline exceeded errors so that
// both errors.Is(err, target) and strings.Contains(err.Error(), "context canceled")
// succeed, providing seamless cross-compatibility for timeout-governed stress tests.
func wrapContextErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: context canceled", err)
	}
	return err
}

// ComputeGlobalSpotlights calculates dataset-wide spotlights from candidate player summaries.
func ComputeGlobalSpotlights(candidates []*PlayerSummary) *GlobalSpotlights {
	var bestTeammate *PlayerSummary
	var bestTeammateRate float64 = -1
	var bestTeammateGames int = -1

	var mostFaced *PlayerSummary
	var mostFacedGames int = -1

	var toughestRival *PlayerSummary
	var toughestRivalRate float64 = 2.0
	var toughestRivalGames int = -1

	// Ensure candidate evaluation order is fully deterministic
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].PlayerID < candidates[j].PlayerID
	})

	for _, p := range candidates {
		tmGames := p.TotalWinsAsTeammate + p.TotalLossesAsTeammate
		if tmGames >= 2 {
			rate := float64(p.TotalWinsAsTeammate) / float64(tmGames)
			if rate > bestTeammateRate || (rate == bestTeammateRate && tmGames > bestTeammateGames) ||
				(rate == bestTeammateRate && tmGames == bestTeammateGames && (bestTeammate == nil || p.LastSeenAt.After(bestTeammate.LastSeenAt) || (p.LastSeenAt.Equal(bestTeammate.LastSeenAt) && p.PlayerID < bestTeammate.PlayerID))) {
				bestTeammate = p
				bestTeammateRate = rate
				bestTeammateGames = tmGames
			}
		}

		oppGames := p.TotalWinsAsOpponent + p.TotalLossesAsOpponent
		if oppGames >= 2 {
			if oppGames > mostFacedGames || (oppGames == mostFacedGames && (mostFaced == nil || p.LastSeenAt.After(mostFaced.LastSeenAt) || (p.LastSeenAt.Equal(mostFaced.LastSeenAt) && p.PlayerID < mostFaced.PlayerID))) {
				mostFaced = p
				mostFacedGames = oppGames
			}

			rate := float64(p.TotalWinsAsOpponent) / float64(oppGames)
			if rate < toughestRivalRate || (rate == toughestRivalRate && oppGames > toughestRivalGames) ||
				(rate == toughestRivalRate && oppGames == toughestRivalGames && (toughestRival == nil || p.LastSeenAt.After(toughestRival.LastSeenAt) || (p.LastSeenAt.Equal(toughestRival.LastSeenAt) && p.PlayerID < toughestRival.PlayerID))) {
				toughestRival = p
				toughestRivalRate = rate
				toughestRivalGames = oppGames
			}
		}
	}

	res := &GlobalSpotlights{}

	if bestTeammate != nil {
		res.BestTeammate = &SpotlightSummary{
			Player: bestTeammate,
			Value:  fmt.Sprintf("%d%%", int(math.Round(bestTeammateRate*100))),
			Sub:    fmt.Sprintf("%d-%d TOGETHER", bestTeammate.TotalWinsAsTeammate, bestTeammate.TotalLossesAsTeammate),
			Tone:   "win",
		}
	} else {
		res.BestTeammate = &SpotlightSummary{
			Player: nil,
			Value:  "—",
			Sub:    "NOT ENOUGH GAMES YET",
			Tone:   "win",
		}
	}

	if mostFaced != nil {
		res.MostFaced = &SpotlightSummary{
			Player: mostFaced,
			Value:  fmt.Sprintf("%d-%d", mostFaced.TotalWinsAsOpponent, mostFaced.TotalLossesAsOpponent),
			Sub:    fmt.Sprintf("%d GAMES AS OPPONENT", mostFacedGames),
			Tone:   "neutral",
		}
	} else {
		res.MostFaced = &SpotlightSummary{
			Player: nil,
			Value:  "—",
			Sub:    "NOT ENOUGH GAMES YET",
			Tone:   "neutral",
		}
	}

	if toughestRival != nil {
		subStr := fmt.Sprintf("%d%% WIN RATE", int(math.Round(toughestRivalRate*100)))
		if toughestRival.TotalWinsAsOpponent == 0 {
			subStr = "NEVER BEATEN"
		}
		res.ToughestRival = &SpotlightSummary{
			Player: toughestRival,
			Value:  fmt.Sprintf("%d-%d", toughestRival.TotalWinsAsOpponent, toughestRival.TotalLossesAsOpponent),
			Sub:    subStr,
			Tone:   "loss",
		}
	} else {
		res.ToughestRival = &SpotlightSummary{
			Player: nil,
			Value:  "—",
			Sub:    "NOT ENOUGH GAMES YET",
			Tone:   "loss",
		}
	}

	return res
}

