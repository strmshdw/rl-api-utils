# Dispatch for Milestone M5 Worker (E2E Hardening & Full Regression Worker)

## 2026-09-26T06:46:18Z

**Role**: E2E Hardening & Full Regression Worker
**Parent**: `orchestrator_5` (`cc7be76d-47fc-44da-92e2-fb5c2aae2063`)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m5_worker_1`
**Authoritative References**:
1. `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-09-26T03:23:29Z`)
2. `d:\code\rl-api-utils\.agents\teamwork\orchestrator_5\PROJECT.md`
3. Explorer Reports:
   - `d:\code\rl-api-utils\.agents\teamwork\m5_explorer_1\handoff.md` (Backend Regression & 34-item Checklist)
   - `d:\code\rl-api-utils\.agents\teamwork\m5_explorer_2\handoff.md` (Frontend 112 Vitest Tests & Build Artifacts)
   - `d:\code\rl-api-utils\.agents\teamwork\m5_explorer_3\handoff.md` (Adversarial Hardening Plan & Standalone Delivery)

## Write Ownership
You own exclusively:
- `test/e2e/tier5_dashboard_adversarial_test.go`

## MANDATORY INTEGRITY WARNING
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Mission
1. Implement `test/e2e/tier5_dashboard_adversarial_test.go`:
   Implement the 6 Tier 5 E2E adversarial test scenarios formulated by Explorer 3:
   - **Scenario 1**: `TestTier5_Dashboard_LiveTelemetryPropagationToSSE` — Real-time live match telemetry flowing from mock StatsAPI WebSocket exporter through PlayerTrack and SessionTracker into 50 concurrent SSE client connections.
   - **Scenario 2**: `TestTier5_Dashboard_RapidMatchCyclingAndSessionReset` — High-velocity match start/update/conclude cycles interspersed with concurrent `POST /api/session/reset` calls, verifying session re-anchoring, zero panics, and correct MMR progression.
   - **Scenario 3**: `TestTier5_Dashboard_PlayerSearchUnderContinuousIngestion` — Concurrently hammering `GET /api/players` with substring search and platform filtering while new player records are continuously ingested, verifying SQLite vs JSONStore search consistency.
   - **Scenario 4**: `TestTier5_Dashboard_GracefulShutdownUnderActiveSSELoad` — Initiating daemon shutdown while 50 SSE client connections and active REST requests are in-flight, verifying clean context propagation, zero hung connections, and zero leaked goroutines.
   - **Scenario 5**: `TestTier5_Dashboard_SecurityAndPathTraversalPenetration` — Live penetration testing against daemon endpoints verifying that path traversal attacks (`..`, `%2e`, `//`, `\`) return 400/404, never leak host files, never serve `index.html`, and exact `/api` returns 404.
   - **Scenario 6**: `TestTier5_Dashboard_SingleBinaryBuildAndCLIPrecedence` — End-to-end verification of standalone `rl-sync.exe` build and flag precedence (`--web-enabled`, `--web-host`, `--web-port`).
2. Run Full Verification Commands:
   - `powershell -Command "$p = (Get-Item env:LOCALAPPDATA).Value + '\Programs\go\bin;' + $env:PATH; $env:PATH = $p; go test -v -count=1 ./test/e2e -run TestTier5_Dashboard"`
   - `powershell -Command "$p = (Get-Item env:LOCALAPPDATA).Value + '\Programs\go\bin;' + $env:PATH; $env:PATH = $p; go test -p 1 -count=1 ./..."`
   - `powershell -Command "cd web; npm test; npm run build"`
   - `powershell -Command "$p = (Get-Item env:LOCALAPPDATA).Value + '\Programs\go\bin;' + $env:PATH; $env:PATH = $p; go build -o rl-sync.exe ./cmd/rl-sync; .\rl-sync.exe --help; .\rl-sync.exe --version"`
3. Document all test logs and verification outcomes in `d:\code\rl-api-utils\.agents\teamwork\m5_worker_1\handoff.md` and send message to parent.
