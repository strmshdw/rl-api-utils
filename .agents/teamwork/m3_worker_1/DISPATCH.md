## 2026-10-17T02:52:48Z
You are Milestone M3 Implementation Worker.
Your working directory is d:\code\rl-api-utils\.agents\teamwork\m3_worker_1.
Read:
- d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\DISPATCH.md
- d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically timestamp 2026-10-16T23:46:37Z)
- d:\code\rl-api-utils\.agents\teamwork\orchestrator_9\PROJECT.md
- d:\code\rl-api-utils\.agents\teamwork\m3_exp_endpoints_1\handoff.md and endpoints_analysis.md
- d:\code\rl-api-utils\.agents\teamwork\m3_exp_mmr_clock_1\handoff.md and mmr_clock_analysis.md
- d:\code\rl-api-utils\.agents\teamwork\m3_exp_security_1\handoff.md and security_analysis.md

Your exclusive write ownership files:
- internal/daemon/*
- internal/storage/* (sqlite.go, jsonstore.go, store.go, m3_endpoints_test.go)
- internal/statsapi/* (listener.go, types.go, notifier.go, listener_test.go, notifier_test.go)
- internal/playertrack/* (tracker.go, tracker_test.go)
- internal/session/* (models.go, session.go, session_test.go)
- internal/config/* (config.go, yaml_update.go, yaml_update_test.go)
- internal/auth/* (steam_generator.go, steam_test.go)
- cmd/rl-sync/main.go
- .gitignore

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement:
1. REST/SSE Endpoints & Filtering (SPEC-02, SPEC-04, SPEC-05, SPEC-07, SPEC-08):
   - POST /api/matches/{guid}/retry: ResetMatchRetry and async TriggerSync.
   - GET /api/players/{id}/matches: Paginated match history with role, playlist, result filters.
   - GET /api/players/{id}: Add recent_matches summary array.
   - GET /api/players: Server-side role filtering (all, frequent, teammate, opponent, synergy), search, multi-field sorting, and dataset-wide spotlights. Implement FilterPlayerSummaries and GetPlayerSpotlights in SQLite and JSONStore.
   - GET /api/matches: result=victory|defeat filtering relative to local player; expose local_player_ids.
   - PATCH /api/players/{id}: Update notes and tags; allow PATCH in corsMiddleware.
   - GET /api/mmr-history: Chronological MMR progression snapshots.
2. Live Clock, Local Outcomes & Post-Match MMR (SPEC-03, SPEC-09):
   - Ingest time_remaining and is_overtime from StatsGame in listener.go and propagate to CurrentMatchResponse and SSE events (clock_updated, match_updated).
   - Evaluate victory/defeat relative to LocalTeam in session.go, sqlite.go, jsonstore.go, and handlers_matches.go.
   - Invalidate rankCache on match end (InvalidateRankCache) and fetch fresh rank telemetry immediately in OnMatchEnded so ConcludeMatch produces an accurate non-zero MMR delta!
   - Expose local_player_ids in CurrentMatchResponse, SessionResponse, and GET /api/session/state.
3. Security, Concurrency & Operational Hardening (SPEC-06):
   - Toast XML escaping and RL_SYNC_TOAST_XML environment variable argument passing in notifier.go.
   - Atomic 0600 config writes via AtomicWriteFile in yaml_update.go.
   - Steam credentials passed via cmd.Env in steam_generator.go.
   - Mutating endpoints: POST on /sync (return 405 on GET), CSRF Origin/Host check, Daemon.wg background sync tracking.
   - Bounded caches: clamp rankCache (1000), matchupCache (1000), failBackoff (500) in tracker.go.
   - Operational: StreamUpload: true in main.go:378, remove duplicate banner logging, untrack rl-sync.exe in git, fix tracker_test.go data race.
4. Verify with:
   - go vet ./...
   - go test -v -race ./internal/daemon/...
   - go test -v -race ./internal/statsapi/...
   - go test -v -race ./internal/playertrack/...
   - go test -v -race ./internal/session/...
   - go test -v -race ./internal/config/...
   - go test -v -race ./internal/auth/...
   - go test -v -race ./internal/storage/...
   - go test -race ./...

When done, write handoff.md in your working directory and notify orchestrator_9.
