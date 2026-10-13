# Dispatch: m1_worker_1

## Mission & Role
You are `m1_worker_1`, the implementation worker for Milestone 1: True In-Game MMR Conversion (R1).
Working directory: `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1`

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Write Ownership Boundaries
You exclusively own and may edit/create:
- `internal/playertrack/rank_client.go`
- `internal/playertrack/rank_client_test.go`
- `internal/session/session_mmr_test.go`
- `internal/daemon/daemon_mmr_test.go`

DO NOT edit any files in `web/` or files owned by other milestones.

## Exact Implementation Requirements
1. **`internal/playertrack/rank_client.go`**:
   - Add `"math"` to the import block.
   - Implement `ConvertSkillRatingToMMR(skillRating float64) float64`:
     ```go
     func ConvertSkillRatingToMMR(skillRating float64) float64 {
         if skillRating <= 0.0 {
             return 0.0
         }
         if skillRating > 150.0 {
             return skillRating
         }
         return math.Round((skillRating * 20.0) + 100.0)
     }
     ```
   - Update `FromSkill(s rlapi.Skill)`:
     Set `MMR: ConvertSkillRatingToMMR(s.MMR)`.
   - Update `ParseRanksJSON(raw string)` normalization loop:
     Add migration for legacy stored raw floats:
     ```go
     if rank.MMR > 0.0 && rank.MMR <= 150.0 {
         rank.MMR = ConvertSkillRatingToMMR(rank.MMR)
         changed = true
     } else if rank.MMR < 0.0 {
         rank.MMR = 0.0
         changed = true
     }
     ```
2. **`internal/playertrack/rank_client_test.go`**:
   - Add comprehensive tests specified in `m1_explorer_1\handoff.md` and `m1_explorer_3\handoff.md`:
     - `TestConvertSkillRatingToMMR_StandardValues` (47.5 -> 1050, 47.0 -> 1040, 30.4646 -> 709, etc.)
     - `TestConvertSkillRatingToMMR_CasualAndNegative` (0.0 -> 0.0, negatives -> 0.0)
     - `TestConvertSkillRatingToMMR_HighMMRAndIdempotency` (1040 -> 1040, 1150.5 -> 1150.5)
     - `TestFromSkill_TrueInGameMMR`
     - `TestParseRanksJSON_LegacyRawRatingMigration`
     - `TestBuildRanksSnapshot_And_SerializeRanksJSON_TrueInGameMMR`
3. **`internal/session/session_mmr_test.go`**:
   - Place the session MMR tests from `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\proposed_session_mmr_test.go`.
4. **`internal/daemon/daemon_mmr_test.go`**:
   - Place the daemon API MMR tests from `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\proposed_daemon_mmr_test.go`.
5. **Run builds and tests**:
   - `go test -v -count=1 ./internal/playertrack -run "TestConvertSkillRatingToMMR|TestFromSkill|TestParseRanksJSON"`
   - `go test -v -count=1 ./internal/session -run "TestSessionTracker_TrueInGameMMR"`
   - `go test -v -count=1 ./internal/daemon -run "TestDaemonAPI"`
   - `go test -count=1 ./...`
   - Document all command outputs in your handoff report.

## Output
Write your handoff report to `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\handoff.md`.
Report completion back to parent when done.


## 2026-10-06T11:29:24Z
From parent:
You are m1_worker_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m1_worker_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T11:02:53Z), d:\code\rl-api-utils\.agents\teamwork\orchestrator_8\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\DISPATCH.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement Milestone 1: True In-Game MMR Conversion (R1):
- internal/playertrack/rank_client.go: add math import, ConvertSkillRatingToMMR(skillRating float64), update FromSkill, update ParseRanksJSON.
- internal/playertrack/rank_client_test.go: add comprehensive unit tests.
- internal/session/session_mmr_test.go: copy from d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\proposed_session_mmr_test.go.
- internal/daemon/daemon_mmr_test.go: copy from d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\proposed_daemon_mmr_test.go.
Run all tests and build commands, verify 100% pass, and write handoff to:
d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\handoff.md
Send message to parent when done.
