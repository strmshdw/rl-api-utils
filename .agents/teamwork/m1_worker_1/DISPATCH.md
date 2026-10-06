# DISPATCH: m1_worker_1

## Objective
Implement Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect):
1. Retain player information and accumulated stats in the active game state when a player leaves or disconnects mid-game.
2. Flag retained participants with `IsDisconnected: true` while preserving their last observed box score stats (`Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`).
3. Handle reconnection cleanly (restores active status, updates stats, no duplicate entries).
4. Preserve `LocalPlayer` and `LocalTeam` if local player disconnects early.
5. Propagate `IsDisconnected` through `SessionTracker.RecordActiveMatch`, `SessionMatchPlayer`, and API models.
6. Provide comprehensive automated programmatic tests simulating mid-game disconnect asserting player stats remain in active match state.
7. Ensure 100% test pass rate across all packages and clean build.

## Scope & File Ownership
You exclusively own and may modify:
- `internal/playertrack/tracker.go`
- `internal/playertrack/tracker_test.go`
- `internal/session/models.go`
- `internal/session/session.go`
- `internal/session/session_test.go`
- `web/src/types/api.ts`

Do NOT touch any other source files outside this scope.

## Context & Inputs
- Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
- Project Architecture & Milestones: `d:\code\rl-api-utils\PROJECT.md`
- Explorer Reports:
  - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md` (exact differential retention algorithm)
  - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\handoff.md` (session propagation, models, DeepClone)
  - `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_3\handoff.md` (complete programmatic test suite)

## Implementation Requirements
1. **`internal/playertrack/tracker.go`**:
   - Add `IsDisconnected bool json:"is_disconnected,omitempty"` to `LobbyPlayer`.
   - Update `OnUpdateState`: implement differential participant retention when `isSameMatch` (`MatchGUID == trimmedGUID`).
   - Retain departed human teammates, opponents, and spectators from previous snapshot with `IsDisconnected = true`.
   - Reconnections update stats and set `IsDisconnected = false` without creating duplicate rows.
   - Departed AI bots (`oldP.IsBot`) are excluded from retention.
   - Retain `LocalPlayer` and `LocalTeam` if local player is omitted.
   - Reset retention cleanly when transitioning to a new match GUID.
2. **`internal/session/models.go` & `session.go`**:
   - Add `IsDisconnected bool json:"is_disconnected,omitempty"` and `Won *bool json:"won,omitempty"` to `SessionMatchPlayer`.
   - Update `SessionMatchPlayer.DeepClone()` to clone `Won` pointer.
   - In `ConcludeMatch`, map `lp.IsDisconnected` to `SessionMatchPlayer.IsDisconnected`, compute `Won`, and sum goals from all participants into team scores.
3. **`web/src/types/api.ts`**:
   - Add `is_disconnected?: boolean;` to `LobbyPlayer` and `SessionMatchPlayer`.
4. **Automated Tests**:
   - Add test suites in `internal/playertrack/tracker_test.go` and `internal/session/session_test.go` as specified in `m1_explorer_3\handoff.md`.
   - Execute verification: `go test -v ./internal/playertrack/...`, `go test -v ./internal/session/...`, and `go test ./...`.
   - Ensure all tests pass 100%.

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Completion Criteria
Deliver a comprehensive report in `handoff.md` in your working directory `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\handoff.md` including exact code modifications, test execution results, and verification commands. Send a completion message back to orchestrator_6.


## 2026-10-06T08:56:14Z
You are m1_worker_1, an implementation worker for Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect).
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m1_worker_1

You MUST read before starting:
1. Authoritative User Request: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T08:30:09Z)
2. Your task assignment & file ownership: d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\DISPATCH.md
3. Explorer Handoff Reports:
   - d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md
   - d:\code\rl-api-utils\.agents\teamwork\m1_explorer_2\handoff.md
   - d:\code\rl-api-utils\.agents\teamwork\m1_explorer_3\handoff.md

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Your Tasks:
1. Implement differential participant retention in internal/playertrack/tracker.go and add IsDisconnected to LobbyPlayer.
2. Update internal/session/models.go and session.go to support IsDisconnected (and Won) in SessionMatchPlayer and ConcludeMatch.
3. Update web/src/types/api.ts with is_disconnected.
4. Add the comprehensive automated programmatic test suites in internal/playertrack/tracker_test.go and internal/session/session_test.go.
5. Run build and tests (e.g., go test -v ./internal/playertrack/..., go test -v ./internal/session/..., go test ./...).
6. Document changes and test results in d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\handoff.md and send a completion message back to orchestrator_6.
