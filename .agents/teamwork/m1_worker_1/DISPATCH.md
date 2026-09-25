# Dispatch for M1 Worker: Implement Storage & Configuration

**Milestone**: M1 - Storage & Configuration
**Role**: Worker (`teamwork_preview_worker`)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1`
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`

## Input Reports & Reference Code
The three M1 explorers have authored complete, production-ready code proposals:
1. SQLite Engine & Store Interface:
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md`
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\proposed_store.go`
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\proposed_sqlite.go`
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\proposed_sqlite_test.go`
2. JSON Store Fallback:
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\handoff.md`
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\proposed_jsonstore.go`
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\proposed_jsonstore_test.go`
3. Configuration System & Module Setup:
   - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_3\handoff.md`
   - Contains proposed `internal/config/config.go`, `internal/config/config_test.go`, `configs/config.example.yaml`, `configs/config.example.json`, and `go.mod`

## Exclusive Write Ownership
You exclusively own and must create/write the following files in the project root (`d:\code\rl-api-utils`):
- `go.mod`
- `internal/storage/store.go`
- `internal/storage/sqlite.go`
- `internal/storage/sqlite_test.go`
- `internal/storage/jsonstore.go`
- `internal/storage/jsonstore_test.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `configs/config.example.yaml`
- `configs/config.example.json`

DO NOT touch or modify any other directories (e.g. `internal/testutil` or `test/e2e` belong to the E2E Testing Track).

## Objectives
1. Read `ORIGINAL_REQUEST.md` and `PROJECT.md`.
2. Review all explorer proposals.
3. Write `go.mod` and resolve dependencies (`go mod tidy` / fetch dependencies).
4. Implement `internal/storage/store.go`, `sqlite.go`, and `jsonstore.go`.
5. Implement `internal/config/config.go`.
6. Implement `configs/config.example.yaml` and `configs/config.example.json`.
7. Implement unit tests `internal/storage/sqlite_test.go`, `internal/storage/jsonstore_test.go`, and `internal/config/config_test.go`.
8. Run build and tests:
   `go test -v -race ./internal/storage/...`
   `go test -v -race ./internal/config/...`
   Ensure 100% test pass with zero race warnings.
9. Write `handoff.md` in your working directory documenting the files created, build/test commands executed, test output, and layout compliance.
10. Send a completion message to the parent orchestrator.

## MANDATORY INTEGRITY WARNING
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.
