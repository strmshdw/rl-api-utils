# Task Dispatch: m4_worker_1

## Mission
You are m4_worker_1, the Milestone M4 Implementation Worker.
Implement the Local Web API query endpoints on port 49125, daemon HTTP server unification, component wiring in `cmd/rl-sync/main.go`, and comprehensive test suites in `internal/daemon/daemon_test.go` and `cmd/rl-sync/main_test.go`.

## Working Directory
`d:\code\rl-api-utils\.agents\teamwork\m4_worker_1`

## Inputs
- Authoritative Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically ## 2026-09-26T00:19:44Z § R4, R5 & Acceptance Criteria)
- Architecture Plan: `c:\Users\strms\.gemini\antigravity\brain\11baea32-4a41-4d49-b959-d518322eea18\player_tracking_plan.md`
- Project Roadmap: `d:\code\rl-api-utils\PROJECT.md`
- Explorer 1 Report: `d:\code\rl-api-utils\.agents\teamwork\m4_pt_explorer_1\handoff.md` and `analysis.md`
- Explorer 2 Report: `d:\code\rl-api-utils\.agents\teamwork\m4_pt_explorer_2\handoff.md` and `analysis.md`
- Explorer 3 Report: `d:\code\rl-api-utils\.agents\teamwork\m4_pt_explorer_3\handoff.md` and `analysis.md`

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Detailed Requirements

### 1. `internal/daemon/daemon.go`
1. Define interfaces (or use concrete types / existing interfaces):
   - `PlayerTracker` with `GetCurrentMatch() *playertrack.CurrentMatchResponse` (or accept `*playertrack.Tracker`).
   - Accept `storage.StateStore` (or `PlayerStore`).
2. Add options to `Daemon`:
   - `WithPlayerTracker(tracker *playertrack.Tracker) Option`
   - `WithStateStore(store storage.StateStore) Option`
3. Expose method `(d *Daemon) Handler(ctx context.Context) http.Handler` returning the unified `http.Handler` for testability without port collisions.
4. Unified HTTP Server (serving both existing trigger endpoints and player tracking endpoints on `127.0.0.1:49125`):
   - Existing endpoints:
     - `POST /sync` or `/sync`: trigger sync cycle.
     - `GET /status` or `/status`: returns stats tracker status JSON.
     - `GET /healthz`: returns `{"status":"running"}`.
   - New Player Tracking query endpoints:
     - `GET /current-match`:
       - If `d.playerTracker == nil`, return HTTP 200 with inactive match snapshot (`{"active_match": false, "teammates": [], "opponents": [], "spectators": []}`).
       - Else call `d.playerTracker.GetCurrentMatch()`. Return HTTP 200 with full snapshot JSON.
     - `GET /players`:
       - Parse query parameters: `limit` (default 50, clamp between 1 and 100), `offset` (default 0, clamp min 0). Non-integer values safely fallback to defaults.
       - If `d.stateStore == nil`, return HTTP 200 with `[]`.
       - Call `d.stateStore.ListPlayerSummaries(ctx, limit, offset)`.
       - Return HTTP 200 with JSON array of player summaries. Never return `null` when empty — return `[]`.
     - `GET /players/{id...}`:
       - Extract path parameter `id` using `r.PathValue("id")`.
       - Unescape via `url.PathUnescape`. If empty or unescape error, return HTTP 404 `{"error":"player not found"}` (or 400 for malformed percent encoding).
       - If `d.stateStore == nil`, return HTTP 404 `{"error":"player not found"}`.
       - Call `d.stateStore.GetPlayer(ctx, playerID)`. If `errors.Is(err, storage.ErrPlayerNotFound)` or record is nil, return HTTP 404 with `{"error":"player not found"}`.
       - Call `d.stateStore.GetPlayerMatchups(ctx, playerID)`.
       - Return HTTP 200 with composite object:
         ```go
         type PlayerDetailResponse struct {
             *storage.PlayerRecord
             Matchups []*storage.PlayerMatchup `json:"matchups"`
         }
         ```
         (Ensure `Matchups` is non-nil empty slice `make([]*storage.PlayerMatchup, 0)` if no matchups found).
   - Server lifecycle in `Start()`:
     - Start server if `(d.cfg.StatsAPI.Enabled && d.cfg.StatsAPI.HTTPTriggerPort > 0) || (d.cfg.PlayerTracking.Enabled && d.cfg.PlayerTracking.Port > 0)`.
     - Bind address: `fmt.Sprintf("127.0.0.1:%d", port)` (port 49125).
     - Track goroutine with `d.wg.Add(1)` / `d.wg.Done()`.
     - Gracefully shutdown server on `<-ctx.Done()` using `httpSrv.Shutdown(shutdownCtx)` with 2-second timeout.

### 2. `cmd/rl-sync/main.go`
1. Add CLI flags for Player Tracking and Polling Auth:
   - `--player-tracking` (bool, default true)
   - `--local-player-id` (string)
   - `--local-player-name` (string)
   - `--auto-fetch-ranks` (bool, default true)
   - `--polling-auth` (bool, default false)
   - `--polling-provider` (string)
2. Add injection hooks in `Runner` struct:
   - `NewPollingAuth func(cfg config.PollingAuthConfig) (auth.AuthProvider, error)`
   - `NewRankClient func(cfg config.Config) (playertrack.SkillFetcher, error)`
   - `NewPlayerTracker func(store storage.StateStore, client playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error)`
   - Initialize them with defaults in `NewDefaultRunner`.
3. In `Runner.Run()`:
   - If `cfg.PlayerTracking.Enabled && !cfg.Sync.Once`:
     - Create rank client:
       - If `cfg.PollingAuth.Enabled`, call `r.NewRankClient(cfg)` (handles collision check and fallback).
       - Else `rankClient = playertrack.NewNoOpRankClient()`.
     - Create tracker: `tracker, err := r.NewPlayerTracker(store, rankClient, cfg.PlayerTracking, playertrack.WithTrackerLogger(logger))`.
     - If tracker created:
       - Defer `tracker.Close()`.
       - If `listener != nil`: `listener.SetPlayerEventHandler(tracker)`.
       - Add `daemon.WithPlayerTracker(tracker)` and `daemon.WithStateStore(store)` to `daemonOpts`.
   - Backward compatibility: If `!cfg.PlayerTracking.Enabled`, skip tracker creation and daemon runs normally.

### 3. Tests
1. `internal/daemon/daemon_test.go`:
   - Add tests for `GET /current-match` (inactive vs active).
   - Add tests for `GET /players` (empty, populated, pagination, limit/offset clamping, SQLite and JSONStore backends).
   - Add tests for `GET /players/{id}` (existing player with matchups, unescaped `Steam%7C...%7C0` and raw `Steam|...|0`, 404 for nonexistent, malformed percent handling).
   - Test server lifecycle and graceful drain.
2. `cmd/rl-sync/main_test.go`:
   - Add tests validating runner wiring with player tracking enabled.
   - Add tests validating fallback when polling auth is disabled or fails.
   - Add tests validating backward compatibility when player tracking is disabled.

### 4. Verification
Execute and verify:
- `go build ./cmd/rl-sync`
- `go test -v -count=1 ./internal/daemon/...`
- `go test -v -count=1 ./cmd/rl-sync/...`
- `go test -v -count=1 ./internal/playertrack/...`
- `go test -count=1 ./...` (must pass 100% across all 12 packages)
- `go vet ./...`

Produce handoff report in `d:\code\rl-api-utils\.agents\teamwork\m4_worker_1\handoff.md`.
Send message to parent when done.

## 2026-09-26T01:40:17Z
You are m4_worker_1.
Working Directory: d:\code\rl-api-utils\.agents\teamwork\m4_worker_1
Read d:\code\rl-api-utils\.agents\teamwork\m4_worker_1\DISPATCH.md
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-09-26T00:19:44Z § R4, R5 & Acceptance Criteria)
Read d:\code\rl-api-utils\PROJECT.md
Read Explorer inputs:
- d:\code\rl-api-utils\.agents\teamwork\m4_pt_explorer_1\handoff.md and analysis.md
- d:\code\rl-api-utils\.agents\teamwork\m4_pt_explorer_2\handoff.md and analysis.md
- d:\code\rl-api-utils\.agents\teamwork\m4_pt_explorer_3\handoff.md and analysis.md

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement:
1. internal/daemon/daemon.go:
   - Add WithPlayerTracker(tracker *playertrack.Tracker) and WithStateStore(store storage.StateStore).
   - Expose Handler(ctx context.Context) http.Handler for testability.
   - Unified HTTP server on 127.0.0.1:49125:
     - /sync, /status, /healthz
     - GET /current-match: calls playerTracker.GetCurrentMatch(); returns inactive snapshot if nil or inactive.
     - GET /players: parses limit (default 50, clamp 1-100) and offset (default 0, clamp >=0), calls store.ListPlayerSummaries; returns JSON array (never null).
     - GET /players/{id...}: unescapes path param via url.PathUnescape; queries store.GetPlayer and store.GetPlayerMatchups; returns 404 on ErrPlayerNotFound; returns composite struct with Matchups slice (never null).
     - Server lifecycle: Start() runs background server goroutine tracked by d.wg.Add(1)/Done(), drains cleanly via httpSrv.Shutdown() on ctx.Done() with 2s timeout. Bypasses in --once mode.
2. cmd/rl-sync/main.go:
   - Add CLI flags: --player-tracking, --local-player-id, --local-player-name, --auto-fetch-ranks, --polling-auth, --polling-provider.
   - Add injection hooks in Runner: NewPollingAuth, NewRankClient, NewPlayerTracker.
   - In Run(): if cfg.PlayerTracking.Enabled && !cfg.Sync.Once:
     - Initialize rank client (polling auth with collision check, or NoOpRankClient).
     - Initialize playertrack.Tracker.
     - Connect to statsListener.SetPlayerEventHandler(tracker).
     - Add daemon.WithPlayerTracker and daemon.WithStateStore.
     - Defer tracker.Close().
3. Comprehensive test suites:
   - internal/daemon/daemon_test.go: add all tests specified in m4_pt_explorer_3/analysis.md.
   - cmd/rl-sync/main_test.go: add tests for runner wiring, fallback, and backward compatibility.

Verify with:
- go build ./cmd/rl-sync
- go test -v -count=1 ./internal/daemon/...
- go test -v -count=1 ./cmd/rl-sync/...
- go test -v -count=1 ./internal/playertrack/...
- go test -count=1 ./... (must pass 100% across all 12 packages)
- go vet ./...

Write complete handoff report to d:\code\rl-api-utils\.agents\teamwork\m4_worker_1\handoff.md.
Send message to parent when done.

