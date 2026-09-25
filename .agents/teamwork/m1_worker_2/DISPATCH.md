# Dispatch for M1 Worker 2: Apply Storage & Config Edge-Case Remediations

**Milestone**: M1 - Storage & Configuration (Iteration 2)
**Role**: Worker (`teamwork_preview_worker`)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_worker_2`
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`

## Input Reports & Patches
1. SQLite fixes:
   - Report: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\handoff.md`
   - Patch: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\sqlite_fixes.patch`
2. JSONStore fixes:
   - Report: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\handoff.md`
   - Replacement / Patch: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\proposed_jsonstore.go` and `jsonstore.patch`
3. Config Duration fix:
   - Report: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3\handoff.md`
   - Patch: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3\duration_unmarshal_yaml.patch`

## Exclusive Write Ownership
You exclusively own:
- `internal/storage/sqlite.go`
- `internal/storage/jsonstore.go`
- `internal/config/config.go`

DO NOT touch any other packages.

## Objectives
1. Read `ORIGINAL_REQUEST.md`, `PROJECT.md`, and the input patches.
2. Apply the SQLite fix to `internal/storage/sqlite.go`:
   - In `UpsertDiscoveredMatches`:
     `download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,`
   - In `SaveAuthState` and `GetAuthState`:
     `if provider == "" { return errors.New("provider cannot be empty") }`
3. Apply the JSONStore fix to `internal/storage/jsonstore.go`:
   - Copy or apply `proposed_jsonstore.go` from `m1_r2_explorer_2`.
4. Apply the Config fix to `internal/config/config.go`:
   - In `Duration.UnmarshalYAML`: decode into `int64` before string decoding.
5. Run tests:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/storage/...
   go test -v -count=1 ./internal/config/...
   go vet ./internal/storage/... ./internal/config/...
   ```
   Verify 100% pass across all tests (including `adversarial_test.go` and `boundary_test.go`).
6. MANDATORY INTEGRITY WARNING:
   DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A forensic auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.
7. Write your report to `d:\code\rl-api-utils\.agents\teamwork\m1_worker_2\handoff.md` and notify parent via `send_message`.

## 2026-09-25T03:29:41Z
Apply the three edge-case remediations for Milestone 1 (Storage & Configuration):
1. SQLite: Apply patch from d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\sqlite_fixes.patch to internal/storage/sqlite.go.
2. JSONStore: Replace internal/storage/jsonstore.go with d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\proposed_jsonstore.go.
3. Config: Apply patch from d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_3\duration_unmarshal_yaml.patch to internal/config/config.go.
4. Execute test commands:
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/storage/...
   go test -v -count=1 ./internal/config/...
   go vet ./internal/storage/... ./internal/config/...
   Ensure 100% test pass on all tests (including adversarial_test.go and boundary_test.go).
5. MANDATORY INTEGRITY WARNING:
   DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A forensic auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.
6. Write your handoff to d:\code\rl-api-utils\.agents\teamwork\m1_worker_2\handoff.md and report to parent via send_message.
