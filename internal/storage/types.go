package storage

import (
	"context"
	"errors"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
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

// ReplayPlayerStats contains per-player statistics extracted from a replay or Ballchasing record.
type ReplayPlayerStats struct {
	Name                string                   `json:"name"`
	Platform            string                   `json:"platform"`
	OnlineID            string                   `json:"online_id"`
	Team                int                      `json:"team"` // 0 = Blue, 1 = Orange
	Score               int                      `json:"score"`
	Goals               int                      `json:"goals"`
	Assists             int                      `json:"assists"`
	Saves               int                      `json:"saves"`
	Shots               int                      `json:"shots"`
	Demos               int                      `json:"demos,omitempty"`
	IsBot               bool                     `json:"is_bot"`
	IsLocal             bool                     `json:"is_local,omitempty"`
	CarID               int                      `json:"car_id,omitempty"`
	CarName             string                   `json:"car_name,omitempty"`
	MVP                 bool                     `json:"mvp,omitempty"`
	RankName            string                   `json:"rank_name,omitempty"`
	ShortRank           string                   `json:"short_rank,omitempty"`
	MMR                 float64                  `json:"mmr,omitempty"`
	Camera              *domain.CameraSettings   `json:"camera,omitempty"`
	SteeringSensitivity float64                  `json:"steering_sensitivity,omitempty"`
	Rank                *domain.PlayerRank       `json:"rank,omitempty"`
	Core                *domain.CoreStats        `json:"core,omitempty"`
	Boost               *domain.BoostStats       `json:"boost,omitempty"`
	Movement            *domain.MovementStats    `json:"movement,omitempty"`
	Positioning         *domain.PositioningStats `json:"positioning,omitempty"`
	Demo                *domain.DemoStats        `json:"demo,omitempty"`
	Ball                *domain.BallStats        `json:"ball,omitempty"`
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

// StateStore defines the persistence interface required by the syncer and daemon.
type StateStore interface {
	GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error)
	ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error)
	ListPendingUploads(ctx context.Context) ([]*MatchRecord, error)
	UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error

	MarkDownloading(ctx context.Context, matchGUID string) error
	MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
	MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
	MarkUploading(ctx context.Context, matchGUID string) error
	MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error

	RecoverInFlight(ctx context.Context) error

	SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error
	GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)

	Close() error

	UpsertPlayer(ctx context.Context, player *PlayerRecord) error
	GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error)
	ListPlayers(ctx context.Context, limit, offset int) ([]*PlayerRecord, error)
	ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error)
	SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error)
	FilterPlayerSummaries(ctx context.Context, filter PlayerDirectoryFilter) ([]*PlayerSummary, int, error)
	GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error)
	GetLocalPlayerIDs(ctx context.Context) ([]string, error)
	UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error
	RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome, matchTime ...time.Time) error
	GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*PlayerMatchup, error)
	GetPlayerMatchups(ctx context.Context, playerID string) ([]*PlayerMatchup, error)
	SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error
	ListHistoricalMatches(ctx context.Context, filter MatchFilter) ([]*HistoricalMatchRecord, int, error)

	SaveMatchPlayers(ctx context.Context, matchGUID string, players []*MatchPlayerRecord) error
	GetMatchPlayers(ctx context.Context, matchGUID string) ([]*MatchPlayerRecord, error)
	ListPlayerMatches(ctx context.Context, playerID string, filter PlayerMatchFilter) ([]*PlayerMatchDetail, int, error)

	RecordMMR(ctx context.Context, record *MMRHistoryRecord) error
	ListMMRHistory(ctx context.Context, playlistID int, limit int) ([]*MMRHistoryRecord, error)

	UpdatePlayerNotesAndTags(ctx context.Context, playerID string, notes string, tags []string) error

	ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*MatchRecord, error)
	MarkStatsSyncing(ctx context.Context, matchGUID string) error
	MarkStatsSynced(ctx context.Context, matchGUID string) error
	MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error
	ResetMatchRetry(ctx context.Context, matchGUID string) error

	SaveSession(ctx context.Context, session *SessionRecord) error
	GetSession(ctx context.Context, sessionID string) (*SessionRecord, error)
	ListSessions(ctx context.Context, limit, offset int) ([]*SessionRecord, int, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

// MatchPruner is an optional interface implemented by StateStore backends that support
// pruning the oldest stored match/replay records to enforce replay history limits.
type MatchPruner interface {
	PruneOldestMatches(ctx context.Context, count int) (int, error)
}
