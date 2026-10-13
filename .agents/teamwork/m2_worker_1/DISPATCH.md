# Dispatch: m2_worker_1

## Mission & Role
You are `m2_worker_1`, the implementation worker for Milestone 2: Sleek Esports Matchup UX Overhaul & Disconnected Player Treatment (R2 & R3).
Working directory: `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1`

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Write Ownership Boundaries
You exclusively own and may edit:
- `web/src/components/live/RosterTable.tsx`
- `web/src/components/live/PlayerRow.tsx`
- `web/src/components/live/LiveGameView.tsx`
- `web/src/components/session/MatchDetailModal.tsx`
- `web/src/components/live/LiveGameView.layout.test.tsx`
- `web/src/components/live/LiveGameView.adversarial.test.tsx`

DO NOT edit any Go backend files (`internal/playertrack/`, `internal/session/`, etc.).

## Inputs & Blueprints
- `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T11:02:53Z`)
- `d:\code\rl-api-utils\.agents\teamwork\orchestrator_8\PROJECT.md`
- `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\handoff.md` (and proposed files in `m2_explorer_1\`)
- `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2\handoff.md` (and `proposed_PlayerRow.tsx` in `m2_explorer_2\`)
- `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md` (and test blueprints)

## Implementation Tasks
1. **`web/src/components/live/RosterTable.tsx`**:
   - Restore semantic `<table>` wrapped in `.overflow-x-auto`.
   - Implement glowing team headers with ambient glow stripe (`h-1 bg-gradient-to-r`), glowing borders (`border-[#00a2ff]/40` / `border-[#ff7b00]/40`), ambient dropshadows.
   - Implement dedicated aggregated goals badge (`data-testid="team-goals"`) and aggregated score badge (`data-testid="team-score"`).
   - Ensure all 11 semantic `data-testid="th-*"` table headers exist in order.
2. **`web/src/components/live/PlayerRow.tsx`**:
   - Implement streamlined broadcast card-row as `<tr>`.
   - Apply disconnected treatment: `player.is_disconnected ? 'opacity-65 saturate-60 hover:opacity-85' : ''`.
   - Add clean `data-testid="badge-disconnected"` with `WifiOff` icon in `data-testid="player-identity"`.
   - Render prominent stat badge pills in `<td>` cells for Score, Goals (amber glow), Assists (cyan), Saves (emerald), Shots, Demos, Rank, and MMR (`toFixed(1)`).
3. **`web/src/components/session/MatchDetailModal.tsx`**:
   - Apply matching `opacity-65 saturate-60 hover:opacity-85` and `data-testid="badge-disconnected"` badge for concluded matches.
4. **`web/src/components/live/LiveGameView.layout.test.tsx` & `LiveGameView.adversarial.test.tsx`**:
   - Fix all 15 Vitest failures.
   - Add dedicated tests for R2 (`team-goals`, `team-score`) and R3 (`badge-disconnected`, `opacity-65 saturate-60`) from `m2_explorer_3\handoff.md`.
5. **Run builds and tests**:
   - `cd web; npm test -- --run` (assert 100% pass)
   - `npm run build` (assert clean bundle into `internal/web/dist`)
   - `cd ..; go test -count=1 ./...` (assert 100% pass across all 14 Go packages)
   - `go build ./cmd/rl-sync` (assert clean `rl-sync.exe` build)

## Output
Write your complete handoff report to:
`d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md`
Send message to parent when done.


## 2026-10-06T12:00:15Z
[Message] timestamp=2026-10-06T12:00:15Z sender=18e4e66b-5835-4b6b-bd1e-a34092f47346 priority=MESSAGE_PRIORITY_HIGH content=You are m2_worker_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m2_worker_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T11:02:53Z), d:\code\rl-api-utils\.agents\teamwork\orchestrator_8\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\DISPATCH.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement Milestone 2: Sleek Esports Matchup UX Overhaul & Disconnected Player Treatment (R2 & R3):
- Apply proposed components and changes from m2_explorer_1, m2_explorer_2, and m2_explorer_3.
- In RosterTable.tsx: restore table semantics, glowing team headers with aggregated goals badge (data-testid="team-goals") and score badge (data-testid="team-score").
- In PlayerRow.tsx: broadcast card-rows with prominent stat badges (Score, Goals with amber glow, Assists, Saves, Shots, Demos), MMR toFixed(1), and disconnected treatment (opacity-65 saturate-60 hover:opacity-85, data-testid="badge-disconnected" with WifiOff icon).
- In MatchDetailModal.tsx: parity disconnected styling and badge.
- In LiveGameView.layout.test.tsx: fix the 15 failures and add dedicated R2 & R3 test cases.
- Run tests and builds: npm test -- --run in web/, npm run build, and go test -count=1 ./... and go build ./cmd/rl-sync.
Write your complete handoff report to:
d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md
Send a message to parent when done.
