# BRIEFING — 2026-10-17T02:05:00Z

## Mission
Implement Milestone M2: Syncer Post-Upload Telemetry Polling, Multi-Identity Local Player Resolution, PsyNet Replay Matchup Recording, Bounded Retry Queue with Exponential Backoff, and Replay Parser RFC3339 Date Normalization / Overtime / Demolition Extraction (SPEC-01, SPEC-04, SPEC-06).

## 🔒 My Identity
- Archetype: worker
- Roles: implementer, qa, specialist
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_worker_1
- Original parent: b82f99b4-2b9f-46d1-8c45-738eb9e9a7b1
- Milestone: Milestone 2 (Polling Auth & PsyNet Rank Client)
- Milestone 2: Sleek Esports Matchup UX Overhaul & Disconnected Player Treatment (R2 & R3)
- Current parent: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Current parent: 048a592a-b159-4ea3-a5c3-e619b708dd31 (orchestrator_9)
- Active Milestone: Milestone M2 (Syncer Post-Upload Telemetry Polling, Multi-Identity Local Player Resolution, PsyNet Replay Matchup Recording, Bounded Retry Queue with Exponential Backoff, Replay Parser)

## 🔒 Key Constraints
- Exclusive write ownership:
  - `internal/config/config.go`
  - `internal/config/config_test.go`
  - `internal/auth/provider.go`
  - `internal/playertrack/rank_client.go`
  - `internal/playertrack/rank_client_test.go`
  - `configs/config.example.yaml`
- Do not cheat: genuine logic, real state, no hardcoded results or facade implementations.
- 100% test pass on all unit tests (`go test -count=1 ./...`) and zero `go vet` warnings.
- Clean Architecture compliance per PROJECT.md.
- Milestone 2 Write Ownership Boundaries:
  - `web/src/components/live/RosterTable.tsx`
  - `web/src/components/live/PlayerRow.tsx`
  - `web/src/components/live/LiveGameView.tsx`
  - `web/src/components/session/MatchDetailModal.tsx`
  - `web/src/components/live/LiveGameView.layout.test.tsx`
  - `web/src/components/live/LiveGameView.adversarial.test.tsx`
- DO NOT edit any Go backend files (`internal/playertrack/`, `internal/session/`, etc.).
- Mandatory Integrity: No hardcoding test results or creating dummy/facade implementations.
- 100% Vitest pass rate (`npm test -- --run` in `web/`), clean `npm run build`, and clean `go test` / `go build`.
- Milestone M2 Exclusive Write Ownership:
  - `internal/syncer/*` (`syncer.go`, `interfaces.go`, `ballchasing_history.go`, `syncer_test.go`, etc.)
  - `internal/replay/*` (`parser.go`, `replay.go`, `parser_test.go`, etc.)
  - `internal/storage/sqlite.go` (only for `ListPendingDownloads`, `ListPendingUploads`, `ListPendingStatsSync`, `MarkDownloaded`, `ResetMatchRetry`)
  - `internal/storage/jsonstore.go` (only for `ListPendingDownloads`, `ListPendingUploads`, `ListPendingStatsSync`, `MarkDownloaded`, `ResetMatchRetry`, `CalculateBackoff`)
  - `internal/storage/store.go` (only for interface methods / helper definitions if needed)
  - `internal/storage/m2_retry_test.go` (new unit tests for retry queue and backoff)
- DO NOT CHEAT: Genuine implementations maintaining real state and behavior. Independent forensic audit by teamwork_preview_auditor.

## Current Parent
- Conversation ID: 048a592a-b159-4ea3-a5c3-e619b708dd31 (orchestrator_9)
- Updated: 2026-10-17T02:05:00Z

## Task Summary
- **What to build**:
  1. Replay Parser: Add `Demos int` to `ReplayPlayerStats`, implement `normalizeReplayDate`, `extractOvertime`, populate demos in `populateMetadata`, and tests in `parser_test.go`.
  2. Storage Retry Queue: Update `ListPendingDownloads`, `ListPendingUploads`, `ListPendingStatsSync` with bounded retries (maxRetries=3) and exponential backoff in SQLite and JSONStore; fix `MarkDownloaded` retry count reset; fix `ResetMatchRetry` download status reset; add tests in `m2_retry_test.go`.
  3. Syncer: Add `ReplayStatsFetcher` and extend `StateStore` in `interfaces.go`; wire Step 6 (`PollAndSyncPendingStats`) in `RunCycle`; implement 6-tier local player resolution with bot immunity; update `SyncDownloadedReplayStats` to propagate Demos/Overtime, resolve local player/team, record match outcomes with actual match timestamp, and save match players; add unit tests in `syncer_test.go`.
- **Success criteria**:
  - `go vet ./...` succeeds with zero errors.
  - `go test -v -race ./internal/replay/...` passes.
  - `go test -v -race ./internal/storage/...` passes.
  - `go test -v -race ./internal/syncer/...` passes.
  - `go test -race ./...` passes across all packages.
- **Interface contracts**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_9\PROJECT.md`
- **Code layout**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_9\PROJECT.md`

## Key Decisions Made
- Follow analysis recommendations from `m2_exp_parser_1`, `m2_exp_retry_1`, and `m2_exp_syncer_1`.
- Keep changes minimal, clean, robust, and idiomatic Go.

## Artifact Index
- `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\DISPATCH.md` — Assignment instructions
- `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\progress.md` — Heartbeat and step tracking
- `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md` — Final handoff report

## Change Tracker
- **Files modified**:
  - `internal/replay/parser.go`: Added `Demos` to `ReplayPlayerStats`, implemented `normalizeReplayDate`, `extractOvertime`, and metadata extraction.
  - `internal/replay/parser_test.go`: Added tests for dates, overtime, demolitions, and 20 real replays.
  - `internal/storage/store.go`: Added backoff constants (`DefaultMaxRetries`, `DefaultBaseBackoff`, `DefaultMaxBackoff`).
  - `internal/storage/sqlite.go`: Added bounded retries with exponential backoff for pending downloads, uploads, and stats sync; reset retry counts.
  - `internal/storage/jsonstore.go`: Implemented `CalculateBackoff` with overflow protection; added bounded retries with exponential backoff for pending downloads, uploads, and stats sync.
  - `internal/storage/m2_retry_test.go`: Added comprehensive unit tests for backoff calculation and retry filtering across SQLite and JSONStore.
  - `internal/syncer/interfaces.go`: Added `ReplayStatsFetcher` and aligned `StateStore` methods.
  - `internal/syncer/syncer.go`: Added `statsFetcher`, `WithStatsFetcher`, `WithLocalPlayerID`, `WithLocalPlayerName`, `WithMaxRetries`, `WithRateInterval`, `mu sync.Mutex`, Step 6 `PollAndSyncPendingStats` in `RunCycle`, 6-tier `ResolveLocalPlayer` with bot immunity, and matchup/player persistence in `SyncDownloadedReplayStats`.
  - `internal/syncer/syncer_test.go`: Implemented `mockStatsFetcher`, updated `mockStore`, and added comprehensive unit tests for polling, duplicate handling, statuses, retries, recovery, local player tiers, bot immunity, and idempotency.
- **Build status**: PASS
- **Pending issues**: Awaiting completion of full repository `go test -race ./...`

## Quality Status
- **Build/test result**: All targeted package tests passing (`internal/replay`, `internal/storage`, `internal/syncer`).
- **Lint status**: `go vet ./...` clean with zero warnings.
- **Tests added/modified**: 15+ comprehensive unit test suites added covering all SPEC-01, SPEC-04, and SPEC-06 requirements.
