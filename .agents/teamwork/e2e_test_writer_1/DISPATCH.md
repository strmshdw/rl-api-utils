# Dispatch for E2E Test Writer: Test Infra, Mock Servers & E2E Test Suite (Tiers 1-4)

**Role**: E2E Test Writer (`teamwork_preview_test_writer`)
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\e2e_test_writer_1`

## Objectives
1. Read `ORIGINAL_REQUEST.md` and `PROJECT.md` thoroughly.
2. Produce `TEST_INFRA.md` at project root (`d:\code\rl-api-utils\TEST_INFRA.md`) conforming to the 4-tier methodology:
   - Tier 1: Feature Coverage (>=5 test cases per feature)
   - Tier 2: Boundary & Corner Cases (>=5 test cases per feature)
   - Tier 3: Cross-Feature Interactions (pairwise coverage)
   - Tier 4: Real-World Application Workloads
3. Build the mock test infrastructure in `internal/testutil/`:
   - `mock_psynet.go`: `httptest.Server` supporting HTTP POST auth and WebSocket upgrade (`/ws`) with `PsyPing`/`PsyPong` and `Matches/GetMatchHistory v1` response handler. Allows tests to inject and mutate matches dynamically between polling cycles.
   - `mock_ballchasing.go`: `httptest.Server` simulating `POST /v2/upload`, checking `Authorization` header and `visibility`, parsing multipart form with `file`, returning 201 Created, 409 Conflict (with duplicate replay ID), 429 Too Many Requests (with `Retry-After`), and 401 Unauthorized.
   - `mock_cdn.go`: `httptest.Server` serving valid mock `.replay` binary payloads with `TAGAME` header.
4. Implement comprehensive end-to-end tests in `test/e2e/`:
   - `tier1_feature_test.go`: Testing all individual features in isolation.
   - `tier2_boundary_test.go`: Edge cases, empty replay URLs, rate limit retries, duplicate handling.
   - `tier3_pairwise_test.go`: Cross-feature combinations (e.g., Epic + DryRun, Steam + Duplicate, RateLimit + Restart).
   - `tier4_workload_test.go`: Multi-cycle polling (new matches on cycle 2), restart persistence idempotency, full pipeline execution.
5. Create `TEST_READY.md` at project root once test suite infrastructure and tests are established, documenting test runner command and tier counts.
6. Output full completion handoff to `d:\code\rl-api-utils\.agents\teamwork\e2e_test_writer_1\handoff.md`.

## 2026-09-25T03:04:06Z
You are e2e_test_writer_1.
Working directory: d:\code\rl-api-utils\.agents\teamwork\e2e_test_writer_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\e2e_test_writer_1\DISPATCH.md.

Develop the E2E testing infrastructure and test suite according to the Dual Track specifications in PROJECT.md:
1. Create d:\code\rl-api-utils\TEST_INFRA.md detailing the test philosophy, architecture, and coverage thresholds across Tiers 1-4.
2. Build mock server infrastructure in internal/testutil/:
   - mock_psynet.go: Mock PsyNet HTTP & WebSocket RPC server with dynamic match injection
   - mock_ballchasing.go: Mock Ballchasing HTTP server (201, 409, 429 backoff, 401)
   - mock_cdn.go: Mock replay CDN server serving valid .replay payloads
3. Build E2E test suites in test/e2e/ covering:
   - Tier 1: Feature Coverage (>=5 per feature)
   - Tier 2: Boundary & Corner Cases (>=5 per feature)
   - Tier 3: Cross-Feature Interactions (pairwise)
   - Tier 4: Real-World Workload Scenarios (multi-cycle polling, restart persistence idempotency)
4. Publish d:\code\rl-api-utils\TEST_READY.md when test infrastructure and test suites are complete.
Write your report to d:\code\rl-api-utils\.agents\teamwork\e2e_test_writer_1\handoff.md and notify parent via send_message.
