package daemon

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/version"
)

// DaemonStatusResponse represents the composite payload returned by GET /status.
type DaemonStatusResponse struct {
	Status             string                  `json:"status"`
	Version            string                  `json:"version,omitempty"`
	Commit             string                  `json:"commit,omitempty"`
	BuildMode          string                  `json:"build_mode,omitempty"`
	Enabled            bool                    `json:"enabled"`
	GameConnected      bool                    `json:"game_connected"`
	PendingCount       int                     `json:"pending_count"`
	TriggerThreshold   int                     `json:"trigger_threshold"`
	ForceSyncOnTrigger bool                    `json:"force_sync_on_trigger"`
	EnableToast        bool                    `json:"enable_toast"`
	AutoSyncOnExit     bool                    `json:"auto_sync_on_exit"`
	MaxHostedReplays   int                     `json:"max_hosted_replays"`
	PendingMatches     []statsapi.PendingMatch `json:"pending_matches,omitempty"`
	StatsAPIReady      bool                    `json:"stats_api_ready"`
	RankAPIReady       bool                    `json:"rank_api_ready"`
	MMRPollerState     string                  `json:"mmr_poller_state"`
	MMRSyncLatencyMs   int64                   `json:"mmr_sync_latency_ms,omitempty"`
	ReplayCount        int                     `json:"replay_count"`
	ReplayLimit        int                     `json:"replay_limit"`
	ReplayUsagePct     float64                 `json:"replay_usage_pct"`
	ReplayLimitStatus  string                  `json:"replay_limit_status"`
}

func (d *Daemon) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"running"}`))
}

func (d *Daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	d.writeJSONResponse(w, r, http.StatusOK, d.buildStatusResponse(r.Context()), "error encoding status response")
}

// countHostedMatches returns total persisted matches using a pure COUNT(*) path
// without scanning or enriching full match rows.
func (d *Daemon) countHostedMatches(ctx context.Context) int {
	if d.stateStore == nil {
		return 0
	}
	if counter, ok := d.stateStore.(interface {
		CountMatches(context.Context) (int, error)
	}); ok {
		if total, err := counter.CountMatches(ctx); err == nil {
			return total
		}
	}
	// Passing Offset: math.MaxInt hits SQLiteStore's COUNT(*) early return (offset >= total)
	// so no match rows are scanned or enriched.
	if _, total, err := d.stateStore.ListHistoricalMatches(ctx, storage.MatchFilter{Limit: 1, Offset: math.MaxInt}); err == nil {
		return total
	}
	return 0
}

func (d *Daemon) buildStatusResponse(ctx context.Context) DaemonStatusResponse {
	defaultThreshold := 15
	defaultForceSync := false
	defaultToast := true
	defaultAutoExit := true
	if d.cfg != nil {
		if d.cfg.StatsAPI.TriggerThreshold > 0 {
			defaultThreshold = d.cfg.StatsAPI.TriggerThreshold
		}
		defaultForceSync = d.cfg.StatsAPI.ForceSyncOnTrigger
		defaultToast = d.cfg.StatsAPI.EnableToast
		defaultAutoExit = d.cfg.StatsAPI.AutoSyncOnExit
	}

	resp := DaemonStatusResponse{
		Status:             "running",
		Version:            version.Version,
		Commit:             version.Commit,
		BuildMode:          version.BuildMode,
		MMRPollerState:     "idle",
		TriggerThreshold:   defaultThreshold,
		ForceSyncOnTrigger: defaultForceSync,
		EnableToast:        defaultToast,
		AutoSyncOnExit:     defaultAutoExit,
		MaxHostedReplays:   statsapi.MaxHostedReplays,
	}

	if d.statsTracker != nil {
		st := d.statsTracker.Status()
		resp.Enabled = st.Enabled
		resp.GameConnected = st.GameConnected
		resp.PendingCount = st.PendingCount
		resp.TriggerThreshold = st.TriggerThreshold
		resp.ForceSyncOnTrigger = st.ForceSyncOnTrigger
		resp.EnableToast = st.EnableToast
		if st.MaxHostedReplays > 0 {
			resp.MaxHostedReplays = st.MaxHostedReplays
		}
		resp.PendingMatches = st.PendingMatches
	}

	statsReady := resp.GameConnected
	if !statsReady && d.statsListener != nil && d.statsListener.IsConnected() {
		statsReady = true
	}
	resp.StatsAPIReady = statsReady

	if d.playerTracker != nil {
		resp.RankAPIReady = d.playerTracker.IsRankAPIReady()
		resp.MMRPollerState = d.playerTracker.MMRPollerState()
		resp.MMRSyncLatencyMs = d.playerTracker.LastMMRSyncLatencyMs()
	}

	replayLimit := d.getReplayLimit()
	replayCount := d.countHostedMatches(ctx)

	usagePct := 0.0
	if replayLimit > 0 {
		usagePct = math.Round((float64(replayCount)/float64(replayLimit))*1000.0) / 10.0
	}
	limitStatus := "ok"
	if replayLimit > 0 && (replayCount >= replayLimit || usagePct >= 100.0) {
		limitStatus = "exceeded"
	} else if usagePct >= 85.0 {
		limitStatus = "warning"
	}

	resp.ReplayCount = replayCount
	resp.ReplayLimit = replayLimit
	resp.ReplayUsagePct = usagePct
	resp.ReplayLimitStatus = limitStatus

	return resp
}

// handleCurrentMatch handles GET /current-match and GET /api/current-match.
func (d *Daemon) handleCurrentMatch(w http.ResponseWriter, r *http.Request) {
	var resp *playertrack.CurrentMatchResponse
	if d.playerTracker != nil {
		resp = d.playerTracker.GetCurrentMatch()
	}

	if resp == nil {
		resp = &playertrack.CurrentMatchResponse{
			ActiveMatch: false,
			Teammates:   make([]playertrack.LobbyPlayer, 0),
			Opponents:   make([]playertrack.LobbyPlayer, 0),
			Spectators:  make([]playertrack.LobbyPlayer, 0),
			UpdatedAt:   time.Now().UTC(),
		}
	} else {
		if resp.Teammates == nil {
			resp.Teammates = make([]playertrack.LobbyPlayer, 0)
		}
		if resp.Opponents == nil {
			resp.Opponents = make([]playertrack.LobbyPlayer, 0)
		}
		if resp.Spectators == nil {
			resp.Spectators = make([]playertrack.LobbyPlayer, 0)
		}
	}

	if len(resp.LocalPlayerIDs) == 0 {
		if d.playerTracker != nil {
			resp.LocalPlayerIDs = d.playerTracker.GetLocalPlayerIDs(r.Context())
		}
		if len(resp.LocalPlayerIDs) == 0 && d.sessionTracker != nil {
			resp.LocalPlayerIDs = d.sessionTracker.GetLocalPlayerIDs()
		}
		if len(resp.LocalPlayerIDs) == 0 && d.stateStore != nil {
			if ids, err := d.stateStore.GetLocalPlayerIDs(r.Context()); err == nil {
				resp.LocalPlayerIDs = ids
			}
		}
	}

	d.writeJSONResponse(w, r, http.StatusOK, resp, "error encoding current-match response")
}
