# Dispatch for M1 Iteration 2 Explorer 2: JSONStore Delayed Replay URL & Context Cancellation Fix

**Milestone**: M1 - Storage & Configuration (Iteration 2)
**Role**: Explorer (`teamwork_preview_explorer`)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2`
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`
**Failure Evidence**:
- `d:\code\rl-api-utils\.agents\teamwork\m1_challenger_1\handoff.md`
- `d:\code\rl-api-utils\.agents\teamwork\m1_auditor_1\handoff.md`
- Failed Tests: `TestAdversarial_SkippedReplayURLArrival` and `TestAdversarial_ContextCancellation` in `internal/storage/adversarial_test.go`

## Objectives
1. Read `ORIGINAL_REQUEST.md`, `PROJECT.md`, and the challenger/auditor handoffs.
2. Analyze the defects in `internal/storage/jsonstore.go`:
   - Defect 1: In `UpsertDiscoveredMatches`, transition `DownloadSkipped -> DownloadPending` when an existing match with empty `ReplayURL` receives a non-empty `m.ReplayURL`:
     ```go
     if existing.ReplayURL == "" && m.ReplayURL != "" {
         existing.ReplayURL = m.ReplayURL
         if existing.DownloadStatus == DownloadSkipped {
             existing.DownloadStatus = DownloadPending
         }
         existing.UpdatedAt = now
         modified = true
     }
     ```
   - Defect 2: Add context cancellation checks to all public methods in `jsonstore.go`:
     ```go
     if err := ctx.Err(); err != nil {
         return nil, err // or return err
     }
     ```
3. Detail the exact patch recommendation and verify that all JSON store tests and adversarial tests pass.
4. Output your report to `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\handoff.md`.

## 2026-09-25T03:25:13Z
Explore the fix for JSONStore delayed replay URL transition (DownloadSkipped -> DownloadPending) in UpsertDiscoveredMatches and context cancellation checks in all JSONStore methods.
Write your recommendation report to d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\handoff.md and notify parent via send_message.
