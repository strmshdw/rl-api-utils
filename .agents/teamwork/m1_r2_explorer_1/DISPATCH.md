# Dispatch for M1 Iteration 2 Explorer 1: SQLite Delayed Replay URL Transition Fix

**Milestone**: M1 - Storage & Configuration (Iteration 2)
**Role**: Explorer (`teamwork_preview_explorer`)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1`
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`
**Failure Evidence**:
- `d:\code\rl-api-utils\.agents\teamwork\m1_challenger_1\handoff.md`
- `d:\code\rl-api-utils\.agents\teamwork\m1_auditor_1\handoff.md`
- Failed Test: `go test -v -run TestAdversarial_SkippedReplayURLArrival ./internal/storage/...`

## Objectives
1. Read `ORIGINAL_REQUEST.md`, `PROJECT.md`, and the challenger/auditor handoffs.
2. Analyze the defect in `internal/storage/sqlite.go`:
   - When a match is discovered without a replay URL, it has `download_status = 'SKIPPED'`.
   - When a subsequent poll cycle re-upserts this match with a populated `ReplayURL`, `download_status` remains `SKIPPED`, so `ListPendingDownloads()` never returns it.
   - Investigate the fix in `UpsertDiscoveredMatches` (`internal/storage/sqlite.go:208-210`):
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
   - Investigate empty provider validation in `SaveAuthState` and `GetAuthState` (`if provider == "" { return errors.New("provider cannot be empty") }`).
3. Detail the exact patch recommendation and verify that all SQLite tests and `TestAdversarial_SkippedReplayURLArrival` pass.
4. Output your report to `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\handoff.md`.

## 2026-09-25T03:25:13Z

You are m1_r2_explorer_1.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\DISPATCH.md.
Challenger handoff: d:\code\rl-api-utils\.agents\teamwork\m1_challenger_1\handoff.md.

Explore the fix for SQLite delayed replay URL transition (SKIPPED -> PENDING) in UpsertDiscoveredMatches and empty provider validation in SaveAuthState/GetAuthState.
Write your recommendation report to d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\handoff.md and notify parent via send_message.

