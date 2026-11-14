package daemon

import (
	"context"
	"net/http"

	"github.com/dank/rl-api-utils/internal/web"
)

// Handler constructs and returns an http.Handler wired with all active daemon routes and CORS middleware.
// Intended for unit testing via httptest.NewServer or direct httptest.ResponseRecorder execution.
func (d *Daemon) Handler(ctx context.Context) http.Handler {
	if ctx == nil {
		ctx = context.Background()
	}
	d.mu.Lock()
	if d.ctx == nil {
		d.ctx = ctx
	}
	d.mu.Unlock()
	return corsMiddleware(d.setupRoutes(ctx))
}

func (d *Daemon) setupRoutes(ctx context.Context) *http.ServeMux {
	mux := http.NewServeMux()

	// Diagnostic & Health Endpoints
	mux.HandleFunc("GET /healthz", d.handleHealthz)
	mux.HandleFunc("/status", d.handleStatus)

	// Stats API Trigger Endpoint (Backward compatible)
	mux.HandleFunc("/sync", d.handleSync(ctx))

	// REST API v1 Endpoints
	mux.HandleFunc("GET /api/session", d.handleGetSession)
	mux.HandleFunc("GET /api/session/state", d.handleGetSession)
	mux.HandleFunc("/api/session/reset", d.handleResetSession)
	mux.HandleFunc("/api/session/continue", d.handleContinueSession)
	mux.HandleFunc("/api/session/dismiss", d.handleDismissContinueSession)
	mux.HandleFunc("GET /api/sessions", d.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id...}", d.handleGetHistoricalSession)
	mux.HandleFunc("GET /api/current-match", d.handleCurrentMatch)
	mux.HandleFunc("GET /api/players", d.handleListPlayers)
	mux.HandleFunc("GET /api/players/{id}/matches", d.handleGetPlayerMatches)
	mux.HandleFunc("GET /api/players/{id...}", d.handleGetPlayer)
	mux.HandleFunc("PATCH /api/players/{id...}", d.handlePatchPlayer)
	mux.HandleFunc("GET /api/matches", d.handleListMatches)
	mux.HandleFunc("POST /api/matches/prune", d.handlePruneMatches)
	mux.HandleFunc("POST /api/matches/{guid}/retry", d.handleRetryMatch)
	mux.HandleFunc("GET /api/matches/{guid...}", d.handleGetMatch)
	mux.HandleFunc("GET /api/historical-matches", d.handleListMatches)
	mux.HandleFunc("GET /api/mmr-history", d.handleListMMRHistory)
	mux.HandleFunc("POST /api/sync/ballchasing", d.handleTriggerBallchasingSync)
	mux.HandleFunc("GET /api/sync/ballchasing/status", d.handleBallchasingSyncStatus)
	mux.HandleFunc("GET /api/sync/config", d.handleGetSyncConfig)
	mux.HandleFunc("POST /api/sync/config", d.handleUpdateSyncConfig)
	mux.HandleFunc("POST /api/sync/now", d.handleManualSyncNow)
	mux.HandleFunc("GET /api/events", d.handleEvents)

	// Backward Compatible Player Tracking Aliases
	mux.HandleFunc("GET /current-match", d.handleCurrentMatch)
	mux.HandleFunc("GET /players", d.handleListPlayers)
	mux.HandleFunc("GET /players/{id}/matches", d.handleGetPlayerMatches)
	mux.HandleFunc("GET /players/{id...}", d.handleGetPlayer)
	mux.HandleFunc("PATCH /players/{id...}", d.handlePatchPlayer)

	// Embedded Static Frontend SPA (Fallback to index.html for non-API routes)
	if d.webHandler != nil {
		mux.Handle("/", d.webHandler)
	} else {
		mux.Handle("/", web.DistHandler())
	}

	return mux
}
