package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
)

// Sentinel errors for the storage layer.
var (
	ErrMatchNotFound         = errors.New("match not found")
	ErrAuthStateNotFound     = errors.New("auth state not found")
	ErrInvalidGUID           = errors.New("match GUID cannot be empty")
	ErrStoreClosed           = errors.New("state store is closed")
	ErrPlayerNotFound        = errors.New("player not found")
	ErrMatchAlreadyProcessed = errors.New("match outcome already processed")
	ErrSessionNotFound       = errors.New("session not found")
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
	MatchGUID  string `json:"match_guid"`
	PlayerID   string `json:"player_id"`
	Platform   string `json:"platform,omitempty"`
	PlayerName string `json:"player_name,omitempty"`
	TeamNum    int    `json:"team_num"` // 0 = Blue, 1 = Orange
	IsLocal    bool   `json:"is_local"`
	Won        *bool  `json:"won,omitempty"` // nil = unknown/draw, true = win, false = loss
	Score      int    `json:"score"`
	Goals      int    `json:"goals"`
	Assists    int    `json:"assists"`
	Saves      int    `json:"saves"`
	Shots      int    `json:"shots"`
	Demos      int    `json:"demos"`
	CarName    string `json:"car_name"`
	StatsJSON  string `json:"stats_json,omitempty"`
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

// PlayerRoleStatsSummary holds both totals and per-game averages for Core + Ballchasing Mechanics stats.
type PlayerRoleStatsSummary struct {
	Games                 int     `json:"games"`
	Wins                  int     `json:"wins"`
	Losses                int     `json:"losses"`
	WinRate               float64 `json:"win_rate"`
	TotalScore            int     `json:"total_score"`
	TotalGoals            int     `json:"total_goals"`
	TotalAssists          int     `json:"total_assists"`
	TotalSaves            int     `json:"total_saves"`
	TotalShots            int     `json:"total_shots"`
	TotalDemosInflicted   int     `json:"total_demos_inflicted"`
	TotalDemosTaken       int     `json:"total_demos_taken"`
	AvgScore              float64 `json:"avg_score"`
	AvgGoals              float64 `json:"avg_goals"`
	AvgAssists            float64 `json:"avg_assists"`
	AvgSaves              float64 `json:"avg_saves"`
	AvgShots              float64 `json:"avg_shots"`
	ShootingPct           float64 `json:"shooting_pct"`
	AvgDemosInflicted     float64 `json:"avg_demos_inflicted"`
	AvgDemosTaken         float64 `json:"avg_demos_taken"`
	AvgBPM                float64 `json:"avg_bpm"`
	AvgBCPM               float64 `json:"avg_bcpm"`
	AvgBoost              float64 `json:"avg_boost"`
	TotalBoostStolen      int     `json:"total_boost_stolen"`
	TotalBigPads          int     `json:"total_big_pads"`
	TotalSmallPads        int     `json:"total_small_pads"`
	AvgSupersonicPct      float64 `json:"avg_supersonic_pct"`
	AvgBoostSpeedPct      float64 `json:"avg_boost_speed_pct"`
	AvgSlowSpeedPct       float64 `json:"avg_slow_speed_pct"`
	AvgGroundPct          float64 `json:"avg_ground_pct"`
	AvgLowAirPct          float64 `json:"avg_low_air_pct"`
	AvgHighAirPct         float64 `json:"avg_high_air_pct"`
	AvgPowerslideCount    float64 `json:"avg_powerslide_count"`
	AvgPowerslideDuration float64 `json:"avg_powerslide_duration"`
	AvgDefThirdPct        float64 `json:"avg_def_third_pct"`
	AvgNeutralThirdPct    float64 `json:"avg_neutral_third_pct"`
	AvgOffThirdPct        float64 `json:"avg_off_third_pct"`
	AvgBehindBallPct      float64 `json:"avg_behind_ball_pct"`
	AvgFirstManPct        float64 `json:"avg_first_man_pct"`
	AvgThirdManPct        float64 `json:"avg_third_man_pct"`
	AvgDistToBall         float64 `json:"avg_dist_to_ball"`
}

// PlayerMatchFilter specifies filtering, sorting, and pagination for a player's match history.
type PlayerMatchFilter struct {
	Role             string `json:"role"`                        // "all", "teammate"/"with", "opponent"/"against"
	Playlist         int    `json:"playlist"`                    // 0 = all playlists
	PlaylistFilter   string `json:"playlist_filter,omitempty"`   // "all", "1v1", "2v2", "3v3", "extra", or numeric/name
	FromUnix         int64  `json:"from_unix,omitempty"`         // inclusive start timestamp (0 = unbounded)
	ToUnix           int64  `json:"to_unix,omitempty"`           // inclusive end timestamp (0 = unbounded)
	Status           string `json:"status,omitempty"`            // "all", "uploaded"/"synced", "local"/"downloaded", "pending", "failed"
	Result           string `json:"result"`                      // "all", "victory"/"W", "defeat"/"L"
	Search           string `json:"search,omitempty"`            // matches arena, playlist, match GUID, or participant name
	SortBy           string `json:"sort_by,omitempty"`           // "date", "score"/"pts", "goals", "assists", "saves", "shots", "demos", "duration"
	SortDir          string `json:"sort_dir,omitempty"`          // "asc", "desc"
	LocalPerspective bool   `json:"local_perspective,omitempty"` // when true, result is evaluated from local player's ("US") perspective
	Limit            int    `json:"limit"`
	Offset           int    `json:"offset"`
}

// PlayerMatchDetail combines the parent match record with the player's specific performance.
type PlayerMatchDetail struct {
	MatchRecord
	Stats       *ReplayMatchStats `json:"stats,omitempty"`
	PlayerStats MatchPlayerRecord `json:"player_stats"`
	Role        string            `json:"role"`                   // "teammate" or "opponent" relative to local player
	Result      string            `json:"result"`                 // "victory", "defeat", "draw"
	LocalResult string            `json:"local_result,omitempty"` // always from local player's ("US") perspective
}

// ReplayPlayerStats contains per-player statistics extracted from a replay or ballchasing record.
type ReplayPlayerStats struct {
	Name                string                        `json:"name"`
	Platform            string                        `json:"platform"`
	OnlineID            string                        `json:"online_id"`
	Team                int                           `json:"team"` // 0 = Blue, 1 = Orange
	Score               int                           `json:"score"`
	Goals               int                           `json:"goals"`
	Assists             int                           `json:"assists"`
	Saves               int                           `json:"saves"`
	Shots               int                           `json:"shots"`
	Demos               int                           `json:"demos,omitempty"`
	IsBot               bool                          `json:"is_bot"`
	IsLocal             bool                          `json:"is_local,omitempty"`
	CarID               int                           `json:"car_id,omitempty"`
	CarName             string                        `json:"car_name,omitempty"`
	MVP                 bool                          `json:"mvp,omitempty"`
	RankName            string                        `json:"rank_name,omitempty"`
	ShortRank           string                        `json:"short_rank,omitempty"`
	MMR                 float64                       `json:"mmr,omitempty"`
	Camera              *ballchasing.CameraSettings   `json:"camera,omitempty"`
	SteeringSensitivity float64                       `json:"steering_sensitivity,omitempty"`
	Rank                *ballchasing.PlayerRank       `json:"rank,omitempty"`
	Core                *ballchasing.CoreStats        `json:"core,omitempty"`
	Boost               *ballchasing.BoostStats       `json:"boost,omitempty"`
	Movement            *ballchasing.MovementStats    `json:"movement,omitempty"`
	Positioning         *ballchasing.PositioningStats `json:"positioning,omitempty"`
	Demo                *ballchasing.DemoStats        `json:"demo,omitempty"`
	Ball                *ballchasing.BallStats        `json:"ball,omitempty"`
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

// MatchFilter specifies search, filtering, sorting, and pagination criteria for historical matches.
type MatchFilter struct {
	Playlist       int    `json:"playlist"`
	PlaylistFilter string `json:"playlist_filter,omitempty"` // "all", "1v1", "2v2", "3v3", "extra", or numeric/name
	FromUnix       int64  `json:"from_unix,omitempty"`       // inclusive start timestamp (0 = unbounded)
	ToUnix         int64  `json:"to_unix,omitempty"`         // inclusive end timestamp (0 = unbounded)
	Status         string `json:"status,omitempty"`          // "all", "uploaded"/"synced", "local"/"downloaded", "pending", "failed"
	Query          string `json:"query"`
	Search         string `json:"search,omitempty"`
	Result         string `json:"result,omitempty"` // "all", "victory"/"W", "defeat"/"L", "draw"
	SortBy         string `json:"sort_by,omitempty"`
	SortDir        string `json:"sort_dir,omitempty"`
	Limit          int    `json:"limit"`
	Offset         int    `json:"offset"`
}

// PlayerDirectoryFilter specifies criteria for filtering and sorting the player directory.
type PlayerDirectoryFilter struct {
	Query           string  `json:"query"`
	Platform        string  `json:"platform"`
	Role            string  `json:"role"` // "all", "frequent", "teammate", "opponent", "synergy"
	Playlist        string  `json:"playlist,omitempty"`
	FromUnix        int64   `json:"from_unix,omitempty"`
	ToUnix          int64   `json:"to_unix,omitempty"`
	MinMatches      int     `json:"min_matches,omitempty"`
	MinSynergyWR    float64 `json:"min_synergy_wr,omitempty"`
	MinSynergyGames int     `json:"min_synergy_games,omitempty"`
	Sort            string  `json:"sort"` // "last_seen", "match_count"/"games", "win_rate"/"with_wr", "against_wr", "with_pts", "against_pts", "mmr", "rank", "platform", "name"
	Order           string  `json:"order"`
	Limit           int     `json:"limit"`
	Offset          int     `json:"offset"`
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

// PlayerSummary embeds PlayerRecord with aggregate matchup totals and split role telemetry.
type PlayerSummary struct {
	PlayerRecord
	TotalWinsAsTeammate   int                     `json:"total_wins_as_teammate"`
	TotalLossesAsTeammate int                     `json:"total_losses_as_teammate"`
	TotalWinsAsOpponent   int                     `json:"total_wins_as_opponent"`
	TotalLossesAsOpponent int                     `json:"total_losses_as_opponent"`
	TotalMatches          int                     `json:"total_matches"`
	EncountersCount       int                     `json:"encounters_count"`
	TeammateWins          int                     `json:"teammate_wins"`
	TeammateLosses        int                     `json:"teammate_losses"`
	OpponentWins          int                     `json:"opponent_wins"`
	OpponentLosses        int                     `json:"opponent_losses"`
	RelationshipTag       string                  `json:"relationship_tag,omitempty"`
	WithYouStats          *PlayerRoleStatsSummary `json:"with_you_stats,omitempty"`
	AgainstYouStats       *PlayerRoleStatsSummary `json:"against_you_stats,omitempty"`
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

// NormalizeBallchasingURL converts Ballchasing API location URLs (/api/replays/<id>) to web URLs (/replay/<id>).
func NormalizeBallchasingURL(ballchasingID, ballchasingURL string) string {
	id := strings.TrimSpace(ballchasingID)
	u := strings.TrimSpace(ballchasingURL)
	if id != "" && (u == "" || strings.Contains(u, "/api/replays/")) {
		return "https://ballchasing.com/replay/" + id
	}
	return u
}

// MatchesPlatformFilter reports whether playerPlatform matches filterPlatform, accounting for platform aliases
// across Psyonix Stats API (PS4, XboxOne, PsyNet), local replay headers (PlayStation, Xbox), and Ballchasing.
func MatchesPlatformFilter(playerPlatform, filterPlatform string) bool {
	fp := strings.ToLower(strings.TrimSpace(filterPlatform))
	if fp == "" || fp == "all" {
		return true
	}
	pp := strings.ToLower(strings.TrimSpace(playerPlatform))
	if pp == fp {
		return true
	}
	switch fp {
	case "playstation", "ps4", "psn", "psynet":
		return pp == "playstation" || pp == "ps4" || pp == "psn" || pp == "psynet"
	case "xbox", "xboxone":
		return pp == "xbox" || pp == "xboxone"
	case "switch", "nintendo":
		return pp == "switch" || pp == "nintendo"
	}
	return false
}

// SamePlatformFamily reports whether two platform strings belong to the same platform family.
func SamePlatformFamily(platA, platB string) bool {
	a := strings.ToLower(strings.TrimSpace(platA))
	b := strings.ToLower(strings.TrimSpace(platB))
	if a == "" || b == "" || a == "unknown" || b == "unknown" {
		return true
	}
	return MatchesPlatformFilter(a, b)
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

	// --- Play Session Persistence ---
	SaveSession(ctx context.Context, session *SessionRecord) error
	GetSession(ctx context.Context, sessionID string) (*SessionRecord, error)
	ListSessions(ctx context.Context, limit, offset int) ([]*SessionRecord, int, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

// SessionRecord represents the persistent record of a play session.
type SessionRecord struct {
	SessionID      string     `json:"session_id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	LastMatchAt    *time.Time `json:"last_match_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	TotalMatches   int        `json:"total_matches"`
	TotalWins      int        `json:"total_wins"`
	TotalLosses    int        `json:"total_losses"`
	WinRate        float64    `json:"win_rate"`
	PlaylistsJSON  string     `json:"playlists_json,omitempty"`
	MatchesJSON    string     `json:"matches_json,omitempty"`
	LocalPlayerIDs []string   `json:"local_player_ids,omitempty"`
	Dismissed      bool       `json:"dismissed,omitempty"`
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

// MatchPruner is an optional interface implemented by StateStore backends that support
// pruning the oldest stored match/replay records to enforce replay history limits.
type MatchPruner interface {
	PruneOldestMatches(ctx context.Context, count int) (int, error)
}

// ParsePlaylistFilter maps a query parameter ("all", "1v1", "2v2", "3v3", "extra", numeric ID, or canonical name)
// to a list of matching Rocket League playlist IDs. Returns nil if all playlists should be included.
func ParsePlaylistFilter(raw string) []int {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || s == "all" || s == "0" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return []int{n}
	}
	switch s {
	case "1v1", "duel", "ranked duel", "ranked duel (1v1)", "casual duel", "casual duel (1v1)":
		return []int{10, 1}
	case "2v2", "doubles", "ranked doubles", "ranked doubles (2v2)", "casual doubles", "casual doubles (2v2)":
		return []int{11, 2}
	case "3v3", "standard", "ranked standard", "ranked standard (3v3)", "casual standard", "casual standard (3v3)", "solo standard":
		return []int{13, 3, 12}
	case "4v4", "chaos", "casual chaos", "casual chaos (4v4)":
		return []int{4}
	case "extra", "extra modes", "extras":
		return []int{27, 28, 29, 30, 34}
	case "hoops", "ranked hoops":
		return []int{27}
	case "rumble", "ranked rumble":
		return []int{28}
	case "dropshot", "ranked dropshot":
		return []int{29}
	case "snow day", "snowday", "ranked snow day":
		return []int{30}
	case "tournament", "tournaments", "competitive tournament":
		return []int{34}
	}
	return nil
}

// ParseDateFilter parses an ISO date ("YYYY-MM-DD"), RFC3339 timestamp, or unix seconds string into a UTC Unix timestamp.
// When isEndOfDay is true and raw is a date-only string ("YYYY-MM-DD"), it returns 23:59:59 UTC of that day.
func ParseDateFilter(raw string, isEndOfDay bool) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			if isEndOfDay {
				return t.UTC().Add(24*time.Hour - time.Second).Unix()
			}
			return t.UTC().Unix()
		}
	}
	if t, ok := ParseReplayDate(s); ok {
		return t.UTC().Unix()
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return n
	}
	return 0
}

// NormalizeResultFilter normalizes result filter aliases ("victory", "w", "win", "wins" -> "victory"; "defeat", "l", "loss", "losses" -> "defeat").
func NormalizeResultFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "victory", "w", "win", "wins", "won":
		return "victory"
	case "defeat", "l", "loss", "losses", "lost":
		return "defeat"
	case "draw", "tie", "d":
		return "draw"
	default:
		return ""
	}
}

// NormalizeRoleFilter normalizes role filter aliases ("teammate", "with", "with_you" -> "teammate"; "opponent", "against", "against_you", "vs" -> "opponent").
func NormalizeRoleFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "teammate", "with", "with_you", "ally", "mate":
		return "teammate"
	case "opponent", "against", "against_you", "vs", "rival", "foe":
		return "opponent"
	case "frequent":
		return "frequent"
	case "synergy":
		return "synergy"
	default:
		return "all"
	}
}

// ComputeRelationshipTag returns the dynamic broadcast relationship badge for a player based on W-L records.
// Note: oppWins = games local player ("US") won against them; oppLosses = games local player ("US") lost to them.
func ComputeRelationshipTag(tmWins, tmLosses, oppWins, oppLosses int) string {
	tmGames := tmWins + tmLosses
	oppGames := oppWins + oppLosses
	if tmGames == 0 && oppGames == 0 {
		return ""
	}
	if tmGames >= oppGames && tmGames > 0 {
		tmWR := float64(tmWins) / float64(tmGames)
		if tmGames >= 3 && tmWR >= 0.65 {
			return "PRIME DUO"
		}
		if tmWR >= 0.50 {
			return "SYNERGY ALLY"
		}
		return "COLD DUO"
	}
	oppWR := float64(oppWins) / float64(oppGames)
	if oppGames >= 3 && oppWR >= 0.40 && oppWR <= 0.60 {
		return "ARCH-RIVAL"
	}
	if oppWR < 0.50 {
		return "NEMESIS"
	}
	return "FAVORABLE FOE"
}

// Resolve1BasedDivision normalizes a division value to 1..4 (where 1=Div I, 4=Div IV, 0=none).
// PsyNet skill payloads use 0..3 with "Division I..IV" in rankName, whereas Ballchasing uses 1..4 with "Div I..IV".
// Inspecting rankName first ensures both 0-based PsyNet ranks and 1-based Ballchasing ranks resolve accurately.
func Resolve1BasedDivision(div int, rankName string) int {
	trimmed := strings.TrimSpace(rankName)
	switch {
	case strings.HasSuffix(trimmed, " Division IV") || strings.HasSuffix(trimmed, " Div IV") || strings.HasSuffix(trimmed, " DIV IV"):
		return 4
	case strings.HasSuffix(trimmed, " Division III") || strings.HasSuffix(trimmed, " Div III") || strings.HasSuffix(trimmed, " DIV III"):
		return 3
	case strings.HasSuffix(trimmed, " Division II") || strings.HasSuffix(trimmed, " Div II") || strings.HasSuffix(trimmed, " DIV II"):
		return 2
	case strings.HasSuffix(trimmed, " Division I") || strings.HasSuffix(trimmed, " Div I") || strings.HasSuffix(trimmed, " DIV I"):
		return 1
	}
	if div >= 1 && div <= 4 {
		return div
	}
	return 0
}

// ExtractPrimaryRankFromJSON extracts the best/primary (tier, division, mmr) from a player's ranks_json,
// optionally scoped to playlistIDs (or prioritizing 11 -> 13 -> 10 if playlistIDs is empty).
func ExtractPrimaryRankFromJSON(ranksJSON string, playlistIDs []int) (tier int, div int, mmr float64) {
	if ranksJSON == "" || ranksJSON == "{}" {
		return 0, 0, 0
	}
	var parsed map[string]struct {
		PlaylistID int     `json:"playlist_id"`
		Tier       int     `json:"tier"`
		Division   int     `json:"division"`
		MMR        float64 `json:"mmr"`
		RankName   string  `json:"rank_name"`
	}
	if err := json.Unmarshal([]byte(ranksJSON), &parsed); err != nil || len(parsed) == 0 {
		return 0, 0, 0
	}
	if len(playlistIDs) > 0 {
		for _, pid := range playlistIDs {
			if r, ok := parsed[strconv.Itoa(pid)]; ok && (r.MMR > 0 || r.Tier > 0) {
				return r.Tier, Resolve1BasedDivision(r.Division, r.RankName), r.MMR
			}
		}
	}
	for _, pid := range []int{11, 13, 10, 12} {
		if r, ok := parsed[strconv.Itoa(pid)]; ok && (r.MMR > 0 || r.Tier > 0) {
			return r.Tier, Resolve1BasedDivision(r.Division, r.RankName), r.MMR
		}
	}
	for _, r := range parsed {
		rDiv := Resolve1BasedDivision(r.Division, r.RankName)
		if r.MMR > mmr || (r.MMR == mmr && r.Tier > tier) {
			tier = r.Tier
			div = rDiv
			mmr = r.MMR
		}
	}
	return tier, div, mmr
}

// RoleStatsAccumulator accumulates Core and Ballchasing Mechanics stats into a PlayerRoleStatsSummary.
type RoleStatsAccumulator struct {
	Summary     PlayerRoleStatsSummary
	statGames   int
	boostGames  int
	moveGames   int
	posGames    int
	sumBPM      float64
	sumBCPM     float64
	sumAvgBoost float64
	sumSuperPct float64
	sumBoostPct float64
	sumSlowPct  float64
	sumGround   float64
	sumLowAir   float64
	sumHighAir  float64
	sumPSCount  float64
	sumPSDur    float64
	sumDefThird float64
	sumNeuThird float64
	sumOffThird float64
	sumBehind   float64
	sumFirstMan float64
	sumThirdMan float64
	sumDistBall float64
}

// AddMatchOutcome records a single game win/loss outcome (from the local player's perspective).
func (a *RoleStatsAccumulator) AddMatchOutcome(localWon bool, isDraw bool) {
	a.Summary.Games++
	if !isDraw {
		if localWon {
			a.Summary.Wins++
		} else {
			a.Summary.Losses++
		}
	}
}

// AddPlayerBoxAndTelemetry accumulates a player's box score and optional statsJSON (ReplayPlayerStats or PlayerTelemetry).
func (a *RoleStatsAccumulator) AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, demos int, statsJSON string) {
	a.statGames++
	a.Summary.TotalScore += score
	a.Summary.TotalGoals += goals
	a.Summary.TotalAssists += assists
	a.Summary.TotalSaves += saves
	a.Summary.TotalShots += shots
	demosInflicted := demos

	if strings.TrimSpace(statsJSON) != "" && statsJSON != "{}" {
		var rps ReplayPlayerStats
		if err := json.Unmarshal([]byte(statsJSON), &rps); err == nil {
			if rps.Demo != nil {
				if demosInflicted == 0 && rps.Demo.Inflicted > 0 {
					demosInflicted = rps.Demo.Inflicted
				}
				a.Summary.TotalDemosTaken += rps.Demo.Taken
			}
			if rps.Boost != nil {
				a.boostGames++
				a.sumBPM += rps.Boost.BPM
				a.sumBCPM += rps.Boost.BCPM
				a.sumAvgBoost += rps.Boost.AvgAmount
				a.Summary.TotalBoostStolen += rps.Boost.AmountStolen
				a.Summary.TotalBigPads += rps.Boost.CountCollectedBig
				a.Summary.TotalSmallPads += rps.Boost.CountCollectedSmall
			}
			if rps.Movement != nil {
				a.moveGames++
				a.sumSuperPct += rps.Movement.PercentSupersonic
				a.sumBoostPct += rps.Movement.PercentBoostSpeed
				a.sumSlowPct += rps.Movement.PercentSlowSpeed
				a.sumGround += rps.Movement.PercentGround
				a.sumLowAir += rps.Movement.PercentLowAir
				a.sumHighAir += rps.Movement.PercentHighAir
				a.sumPSCount += float64(rps.Movement.CountPowerslide)
				if rps.Movement.TimePowerslide > 0 {
					a.sumPSDur += rps.Movement.TimePowerslide
				} else {
					a.sumPSDur += rps.Movement.AvgPowerslideDuration
				}
			}
			if rps.Positioning != nil {
				a.posGames++
				a.sumDefThird += rps.Positioning.PercentDefensiveThird
				a.sumNeuThird += rps.Positioning.PercentNeutralThird
				a.sumOffThird += rps.Positioning.PercentOffensiveThird
				a.sumBehind += rps.Positioning.PercentBehindBall
				a.sumFirstMan += rps.Positioning.PercentMostForward
				a.sumThirdMan += rps.Positioning.PercentMostBack
				a.sumDistBall += rps.Positioning.AvgDistanceToBall
			}
		}
	}
	a.Summary.TotalDemosInflicted += demosInflicted
}

// Finalize computes all per-game averages and win rates, applying fallbackWins/fallbackLosses if higher than recorded match rows.
func (a *RoleStatsAccumulator) Finalize(fallbackWins, fallbackLosses int) *PlayerRoleStatsSummary {
	if fallbackWins+fallbackLosses > a.Summary.Games {
		a.Summary.Wins = fallbackWins
		a.Summary.Losses = fallbackLosses
		a.Summary.Games = fallbackWins + fallbackLosses
	}
	decided := a.Summary.Wins + a.Summary.Losses
	if decided > 0 {
		a.Summary.WinRate = math.Round((float64(a.Summary.Wins)/float64(decided))*1000) / 10
	}
	if a.statGames > 0 {
		fg := float64(a.statGames)
		a.Summary.AvgScore = math.Round((float64(a.Summary.TotalScore)/fg)*10) / 10
		a.Summary.AvgGoals = math.Round((float64(a.Summary.TotalGoals)/fg)*100) / 100
		a.Summary.AvgAssists = math.Round((float64(a.Summary.TotalAssists)/fg)*100) / 100
		a.Summary.AvgSaves = math.Round((float64(a.Summary.TotalSaves)/fg)*100) / 100
		a.Summary.AvgShots = math.Round((float64(a.Summary.TotalShots)/fg)*100) / 100
		a.Summary.AvgDemosInflicted = math.Round((float64(a.Summary.TotalDemosInflicted)/fg)*100) / 100
		a.Summary.AvgDemosTaken = math.Round((float64(a.Summary.TotalDemosTaken)/fg)*100) / 100
		if a.Summary.TotalShots > 0 {
			a.Summary.ShootingPct = math.Round((float64(a.Summary.TotalGoals)/float64(a.Summary.TotalShots))*1000) / 10
		}
	}
	if a.boostGames > 0 {
		bg := float64(a.boostGames)
		a.Summary.AvgBPM = math.Round((a.sumBPM/bg)*10) / 10
		a.Summary.AvgBCPM = math.Round((a.sumBCPM/bg)*10) / 10
		a.Summary.AvgBoost = math.Round((a.sumAvgBoost/bg)*10) / 10
	}
	if a.moveGames > 0 {
		mg := float64(a.moveGames)
		a.Summary.AvgSupersonicPct = math.Round((a.sumSuperPct/mg)*10) / 10
		a.Summary.AvgBoostSpeedPct = math.Round((a.sumBoostPct/mg)*10) / 10
		a.Summary.AvgSlowSpeedPct = math.Round((a.sumSlowPct/mg)*10) / 10
		a.Summary.AvgGroundPct = math.Round((a.sumGround/mg)*10) / 10
		a.Summary.AvgLowAirPct = math.Round((a.sumLowAir/mg)*10) / 10
		a.Summary.AvgHighAirPct = math.Round((a.sumHighAir/mg)*10) / 10
		a.Summary.AvgPowerslideCount = math.Round((a.sumPSCount/mg)*10) / 10
		a.Summary.AvgPowerslideDuration = math.Round((a.sumPSDur/mg)*100) / 100
	}
	if a.posGames > 0 {
		pg := float64(a.posGames)
		a.Summary.AvgDefThirdPct = math.Round((a.sumDefThird/pg)*10) / 10
		a.Summary.AvgNeutralThirdPct = math.Round((a.sumNeuThird/pg)*10) / 10
		a.Summary.AvgOffThirdPct = math.Round((a.sumOffThird/pg)*10) / 10
		a.Summary.AvgBehindBallPct = math.Round((a.sumBehind/pg)*10) / 10
		a.Summary.AvgFirstManPct = math.Round((a.sumFirstMan/pg)*10) / 10
		a.Summary.AvgThirdManPct = math.Round((a.sumThirdMan/pg)*10) / 10
		a.Summary.AvgDistToBall = math.Round((a.sumDistBall/pg)*10) / 10
	}
	out := a.Summary
	return &out
}

// ExtractPlayerStatsJSONFromReplay returns mpStatsJSON if non-empty, or extracts the matching player's
// ReplayPlayerStats JSON from replayStatsJSON as a fallback.
func ExtractPlayerStatsJSONFromReplay(mpStatsJSON, replayStatsJSON, playerID, playerName string, teamNum int) string {
	if s := strings.TrimSpace(mpStatsJSON); s != "" && s != "{}" {
		return s
	}
	if s := strings.TrimSpace(replayStatsJSON); s != "" && s != "{}" {
		var st ReplayMatchStats
		if err := json.Unmarshal([]byte(s), &st); err == nil {
			lowerID := strings.ToLower(strings.TrimSpace(playerID))
			lowerName := strings.ToLower(strings.TrimSpace(playerName))
			idToken := ""
			if parts := strings.Split(lowerID, "|"); len(parts) >= 2 {
				idToken = strings.TrimSpace(parts[1])
			}
			for _, p := range st.Players {
				pID := strings.ToLower(strings.TrimSpace(p.OnlineID))
				pName := strings.ToLower(strings.TrimSpace(p.Name))
				if (pID != "" && pID != "0" && (pID == lowerID || pID == idToken || strings.Contains(lowerID, "|"+pID+"|") || strings.Contains(pID, "|"+idToken+"|"))) ||
					(lowerName != "" && pName == lowerName && (teamNum < 0 || p.Team == teamNum)) ||
					(idToken != "" && pName == idToken && (teamNum < 0 || p.Team == teamNum)) {
					if b, err := json.Marshal(p); err == nil {
						return string(b)
					}
				}
			}
			// Fallback: if teamNum is specified and there is only 1 player on that team (e.g. 1v1), return that player.
			if teamNum >= 0 {
				var teamCandidate *ReplayPlayerStats
				teamCount := 0
				for i := range st.Players {
					if st.Players[i].Team == teamNum {
						teamCandidate = &st.Players[i]
						teamCount++
					}
				}
				if teamCount == 1 && teamCandidate != nil {
					if b, err := json.Marshal(teamCandidate); err == nil {
						return string(b)
					}
				}
			}
		}
	}
	return ""
}

// PopulatePlayerSummaryComputedFields sets EncountersCount, TeammateWins/Losses, OpponentWins/Losses,
// and RelationshipTag on a PlayerSummary.
func PopulatePlayerSummaryComputedFields(sRec *PlayerSummary) {
	if sRec == nil {
		return
	}
	sRec.TeammateWins = sRec.TotalWinsAsTeammate
	sRec.TeammateLosses = sRec.TotalLossesAsTeammate
	sRec.OpponentWins = sRec.TotalWinsAsOpponent
	sRec.OpponentLosses = sRec.TotalLossesAsOpponent
	sRec.EncountersCount = sRec.TotalMatches
	sRec.RelationshipTag = ComputeRelationshipTag(
		sRec.TotalWinsAsTeammate,
		sRec.TotalLossesAsTeammate,
		sRec.TotalWinsAsOpponent,
		sRec.TotalLossesAsOpponent,
	)
}

// MatchesRoleFilter evaluates whether a PlayerSummary satisfies the role filter in PlayerDirectoryFilter.
func MatchesRoleFilter(sRec *PlayerSummary, filter PlayerDirectoryFilter) bool {
	if sRec == nil {
		return false
	}
	role := NormalizeRoleFilter(filter.Role)
	switch role {
	case "frequent":
		minM := filter.MinMatches
		if minM <= 0 {
			minM = 3
		}
		return sRec.TotalMatches >= minM
	case "teammate":
		return (sRec.TotalWinsAsTeammate + sRec.TotalLossesAsTeammate) > 0
	case "opponent":
		return (sRec.TotalWinsAsOpponent + sRec.TotalLossesAsOpponent) > 0
	case "synergy":
		minGames := filter.MinSynergyGames
		if minGames <= 0 {
			minGames = 2
		}
		minWR := filter.MinSynergyWR
		if minWR <= 0 {
			minWR = 60.0
		}
		tmGames := sRec.TotalWinsAsTeammate + sRec.TotalLossesAsTeammate
		if tmGames < minGames {
			return false
		}
		wrPct := (float64(sRec.TotalWinsAsTeammate) / float64(tmGames)) * 100.0
		return wrPct+1e-9 >= minWR
	default:
		return true
	}
}

// SortAndPaginatePlayerSummaries sorts matching PlayerSummary records according to filter.Sort and filter.Order
// and applies filter.Limit and filter.Offset pagination.
func SortAndPaginatePlayerSummaries(matching []*PlayerSummary, filter PlayerDirectoryFilter, playlistIDs []int) ([]*PlayerSummary, int) {
	total := len(matching)
	if total == 0 || filter.Limit == 0 {
		return make([]*PlayerSummary, 0), total
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return make([]*PlayerSummary, 0), total
	}

	cleanSort := strings.ToLower(strings.TrimSpace(filter.Sort))
	isAsc := strings.ToLower(strings.TrimSpace(filter.Order)) == "asc"

	sort.Slice(matching, func(i, j int) bool {
		a, b := matching[i], matching[j]
		switch cleanSort {
		case "games", "match_count", "matches", "encounters":
			if a.TotalMatches != b.TotalMatches {
				if isAsc {
					return a.TotalMatches < b.TotalMatches
				}
				return a.TotalMatches > b.TotalMatches
			}
		case "with_wr", "win_rate":
			tmGamesA := a.TotalWinsAsTeammate + a.TotalLossesAsTeammate
			tmGamesB := b.TotalWinsAsTeammate + b.TotalLossesAsTeammate
			rateA := 0.0
			if tmGamesA > 0 {
				rateA = float64(a.TotalWinsAsTeammate) / float64(tmGamesA)
			}
			rateB := 0.0
			if tmGamesB > 0 {
				rateB = float64(b.TotalWinsAsTeammate) / float64(tmGamesB)
			}
			if rateA != rateB {
				if isAsc {
					return rateA < rateB
				}
				return rateA > rateB
			}
			if a.TotalMatches != b.TotalMatches {
				if isAsc {
					return a.TotalMatches < b.TotalMatches
				}
				return a.TotalMatches > b.TotalMatches
			}
		case "against_wr":
			oppGamesA := a.TotalWinsAsOpponent + a.TotalLossesAsOpponent
			oppGamesB := b.TotalWinsAsOpponent + b.TotalLossesAsOpponent
			rateA := 0.0
			if oppGamesA > 0 {
				rateA = float64(a.TotalWinsAsOpponent) / float64(oppGamesA)
			}
			rateB := 0.0
			if oppGamesB > 0 {
				rateB = float64(b.TotalWinsAsOpponent) / float64(oppGamesB)
			}
			if rateA != rateB {
				if isAsc {
					return rateA < rateB
				}
				return rateA > rateB
			}
			if a.TotalMatches != b.TotalMatches {
				if isAsc {
					return a.TotalMatches < b.TotalMatches
				}
				return a.TotalMatches > b.TotalMatches
			}
		case "with_pts":
			ptsA := 0.0
			if a.WithYouStats != nil {
				ptsA = a.WithYouStats.AvgScore
			}
			ptsB := 0.0
			if b.WithYouStats != nil {
				ptsB = b.WithYouStats.AvgScore
			}
			if ptsA != ptsB {
				if isAsc {
					return ptsA < ptsB
				}
				return ptsA > ptsB
			}
		case "against_pts":
			ptsA := 0.0
			if a.AgainstYouStats != nil {
				ptsA = a.AgainstYouStats.AvgScore
			}
			ptsB := 0.0
			if b.AgainstYouStats != nil {
				ptsB = b.AgainstYouStats.AvgScore
			}
			if ptsA != ptsB {
				if isAsc {
					return ptsA < ptsB
				}
				return ptsA > ptsB
			}
		case "mmr":
			_, _, mmrA := ExtractPrimaryRankFromJSON(a.RanksJSON, playlistIDs)
			_, _, mmrB := ExtractPrimaryRankFromJSON(b.RanksJSON, playlistIDs)
			if mmrA != mmrB {
				if isAsc {
					return mmrA < mmrB
				}
				return mmrA > mmrB
			}
		case "rank":
			tierA, divA, mmrA := ExtractPrimaryRankFromJSON(a.RanksJSON, playlistIDs)
			tierB, divB, mmrB := ExtractPrimaryRankFromJSON(b.RanksJSON, playlistIDs)
			scoreA := tierA*10 + divA
			scoreB := tierB*10 + divB
			if scoreA != scoreB {
				if isAsc {
					return scoreA < scoreB
				}
				return scoreA > scoreB
			}
			if mmrA != mmrB {
				if isAsc {
					return mmrA < mmrB
				}
				return mmrA > mmrB
			}
		case "platform":
			platA := strings.ToLower(a.Platform)
			platB := strings.ToLower(b.Platform)
			if platA != platB {
				if isAsc {
					return platA < platB
				}
				return platA > platB
			}
		case "name":
			nameA := strings.ToLower(a.PlayerName)
			nameB := strings.ToLower(b.PlayerName)
			if nameA != nameB {
				if isAsc {
					return nameA < nameB
				}
				return nameA > nameB
			}
		default: // "last_seen"
			if !a.LastSeenAt.Equal(b.LastSeenAt) {
				if isAsc {
					return a.LastSeenAt.Before(b.LastSeenAt)
				}
				return a.LastSeenAt.After(b.LastSeenAt)
			}
		}

		// Fallback tie-breaker: last_seen_at (respecting isAsc), player_id ASC
		if !a.LastSeenAt.Equal(b.LastSeenAt) {
			if isAsc {
				return a.LastSeenAt.Before(b.LastSeenAt)
			}
			return a.LastSeenAt.After(b.LastSeenAt)
		}
		return a.PlayerID < b.PlayerID
	})

	end := total
	if filter.Limit > 0 {
		end = offset + filter.Limit
		if end > total {
			end = total
		}
	}

	results := make([]*PlayerSummary, end-offset)
	copy(results, matching[offset:end])
	return results, total
}

// MatchesPlaylistIDs returns true if playlistIDs is empty or contains playlist.
func MatchesPlaylistIDs(playlist int, playlistIDs []int) bool {
	if len(playlistIDs) == 0 {
		return true
	}
	for _, id := range playlistIDs {
		if id == playlist {
			return true
		}
	}
	return false
}

// MatchesStatusFilter evaluates whether a MatchRecord matches the given status filter
// ("all", "uploaded"/"synced", "local"/"downloaded", "pending", "failed").
func MatchesStatusFilter(m *MatchRecord, status string) bool {
	if m == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" || s == "all" {
		return true
	}
	isUploaded := m.UploadStatus == UploadUploaded || m.UploadStatus == UploadDuplicate || m.StatsSyncStatus == StatsSyncSynced || strings.TrimSpace(m.BallchasingID) != ""
	isFailed := m.DownloadStatus == DownloadFailed || m.UploadStatus == UploadFailed || m.StatsSyncStatus == StatsSyncFailed
	isDownloaded := m.DownloadStatus == DownloadDownloaded || strings.TrimSpace(m.LocalFilePath) != ""
	switch s {
	case "uploaded", "synced", "ballchasing":
		return isUploaded
	case "local":
		return isDownloaded && !isUploaded
	case "downloaded":
		return isDownloaded
	case "failed", "error":
		return isFailed
	case "pending", "syncing", "queued":
		return !isUploaded && !isFailed
	default:
		return true
	}
}

// SortHistoricalMatches sorts HistoricalMatchRecord slice by sortBy ("date", "score"/"pts", "goals", "assists", "saves", "shots", "demos", "duration")
// and sortDir ("asc", "desc"), using localStatsByGUID for local player box score metrics.
func SortHistoricalMatches(records []*HistoricalMatchRecord, sortBy, sortDir string, localStatsByGUID map[string]*MatchPlayerRecord) {
	s := strings.ToLower(strings.TrimSpace(sortBy))
	isAsc := strings.ToLower(strings.TrimSpace(sortDir)) == "asc"

	getMetric := func(r *HistoricalMatchRecord) float64 {
		if r == nil {
			return 0
		}
		var lp *MatchPlayerRecord
		if localStatsByGUID != nil {
			lp = localStatsByGUID[r.MatchGUID]
		}
		switch s {
		case "score", "pts", "points":
			if lp != nil {
				return float64(lp.Score)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				return float64(r.Stats.Players[0].Score)
			}
		case "goals":
			if lp != nil {
				return float64(lp.Goals)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				return float64(r.Stats.Players[0].Goals)
			}
		case "assists":
			if lp != nil {
				return float64(lp.Assists)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				return float64(r.Stats.Players[0].Assists)
			}
		case "saves":
			if lp != nil {
				return float64(lp.Saves)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				return float64(r.Stats.Players[0].Saves)
			}
		case "shots":
			if lp != nil {
				return float64(lp.Shots)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				return float64(r.Stats.Players[0].Shots)
			}
		case "demos", "demos_inflicted":
			if lp != nil {
				return float64(lp.Demos)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				return float64(r.Stats.Players[0].Demos)
			}
		case "duration":
			if r.Stats != nil {
				return float64(r.Stats.Duration)
			}
		}
		return float64(r.RecordStartTimestamp)
	}

	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if s == "" || s == "date" || s == "time" || s == "timestamp" {
			if a.RecordStartTimestamp != b.RecordStartTimestamp {
				if isAsc {
					return a.RecordStartTimestamp < b.RecordStartTimestamp
				}
				return a.RecordStartTimestamp > b.RecordStartTimestamp
			}
			return a.MatchGUID < b.MatchGUID
		}
		mA := getMetric(a)
		mB := getMetric(b)
		if mA != mB {
			if isAsc {
				return mA < mB
			}
			return mA > mB
		}
		if a.RecordStartTimestamp != b.RecordStartTimestamp {
			return a.RecordStartTimestamp > b.RecordStartTimestamp
		}
		return a.MatchGUID < b.MatchGUID
	})
}

// SortPlayerMatchDetails sorts PlayerMatchDetail slice by sortBy ("date", "score"/"pts", "goals", "assists", "saves", "shots", "demos", "duration")
// and sortDir ("asc", "desc").
func SortPlayerMatchDetails(details []*PlayerMatchDetail, sortBy, sortDir string) {
	s := strings.ToLower(strings.TrimSpace(sortBy))
	isAsc := strings.ToLower(strings.TrimSpace(sortDir)) == "asc"

	getMetric := func(d *PlayerMatchDetail) float64 {
		if d == nil {
			return 0
		}
		switch s {
		case "score", "pts", "points":
			return float64(d.PlayerStats.Score)
		case "goals":
			return float64(d.PlayerStats.Goals)
		case "assists":
			return float64(d.PlayerStats.Assists)
		case "saves":
			return float64(d.PlayerStats.Saves)
		case "shots":
			return float64(d.PlayerStats.Shots)
		case "demos", "demos_inflicted":
			return float64(d.PlayerStats.Demos)
		case "duration":
			if d.Stats != nil {
				return float64(d.Stats.Duration)
			}
		}
		return float64(d.RecordStartTimestamp)
	}

	sort.Slice(details, func(i, j int) bool {
		a, b := details[i], details[j]
		if s == "" || s == "date" || s == "time" || s == "timestamp" {
			if a.RecordStartTimestamp != b.RecordStartTimestamp {
				if isAsc {
					return a.RecordStartTimestamp < b.RecordStartTimestamp
				}
				return a.RecordStartTimestamp > b.RecordStartTimestamp
			}
			return a.MatchGUID < b.MatchGUID
		}
		mA := getMetric(a)
		mB := getMetric(b)
		if mA != mB {
			if isAsc {
				return mA < mB
			}
			return mA > mB
		}
		if a.RecordStartTimestamp != b.RecordStartTimestamp {
			return a.RecordStartTimestamp > b.RecordStartTimestamp
		}
		return a.MatchGUID < b.MatchGUID
	})
}
