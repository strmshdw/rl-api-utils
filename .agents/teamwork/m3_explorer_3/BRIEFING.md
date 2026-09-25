# BRIEFING — 2026-09-25T04:06:00Z

## Mission
Investigate test architecture and design comprehensive unit/integration test suite for `internal/ballchasing`, utilizing `testutil.NewMockBallchasingServer` across all success, error, and rate-limiting edge cases.

## 🔒 My Identity
- Archetype: explorer
- Roles: read-only investigation, synthesize findings, produce structured reports
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M3 - Ballchasing Replay Uploader

## 🔒 Key Constraints
- Read-only investigation — do NOT implement directly in production package `internal/ballchasing`
- Provide precise proposed code for `client_test.go`, test matrix, edge case analysis, and verification steps in `handoff.md`
- Working folder: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3
- Never put source code, tests, or data directly into `.agents/teamwork/`
- Respect communication guidelines: send_message to parent upon completion

## Current Parent
- Conversation ID: 6e6c9567-59d2-415e-8d6e-41314a903548
- Updated: 2026-09-25T04:06:00Z

## Investigation State
- **Explored paths**:
  - `internal/testutil/mock_ballchasing.go`: full mock capabilities and internal request handling logic.
  - `survey_miner_ballchasing_1/handoff.md`: official specifications, raw token header, 201/409 duplicate semantics, 429 backoff requirements, 401 unauth.
  - `test/e2e/e2e_test.go` & `test/e2e/tier1_feature_test.go`: existing E2E test helper `HTTPBallchasingUploader` and Tier 1 F8-F13 test patterns.
  - `internal/config/config.go`: `BallchasingConfig` with `APIKey`, `Visibility`, `BaseURL`, `Timeout`, `MaxRetries`.
  - `.agents/teamwork/m3_explorer_1/proposed_types.go` and `proposed_client.go`: streaming multipart, client structure, option funcs.
  - `.agents/teamwork/m3_explorer_2/handoff.md`: response handling, duplicate detection, backoff engine, ping verification.
- **Key findings**:
  - `MockBallchasingServer` defaults to `expectedToken: "test-ballchasing-token"`. Tests must use this token or call `srv.SetExpectedToken()`.
  - `MockBallchasingServer.handlePing()` does NOT inspect `forcedStatusCode`, so forced status error testing for Ping must point to an error endpoint or `httptest.Server`.
  - 10 required test scenarios from DISPATCH + 7 edge cases completely designed, implemented, and empirically verified (17 tests, 29 subtests, 100% PASS in 2.92s).
- **Unexplored areas**: None. Test architecture is fully explored and verified.

## Key Decisions Made
- Use `ClientConfig` as primary constructor argument with functional options (`WithBaseBackoff`, etc.) to allow ultra-fast sub-millisecond backoff in tests while supporting production 1s backoff defaults.
- All 17 test suites designed with standard library `testing`, using `testutil.NewMockBallchasingServer` and `testutil.GenerateValidReplay`.

## Artifact Index
- DISPATCH.md — Task instructions from orchestrator
- BRIEFING.md — Persistent working memory
- progress.md — Liveness heartbeat and step tracking
- proposed_client_test.go — Complete proposed unit test suite
- handoff.md — Final 5-component handoff report and proposed `client_test.go`
