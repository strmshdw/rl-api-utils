# Milestone 2 Empirical Challenger Report: Disconnected Player UX Treatment & Data Integrity (R3)

- **Agent**: `m2_challenger_2` (Roles: critic, specialist)
- **Target Subsystem**: `web/src/components/live/PlayerRow.tsx`, `web/src/components/live/RosterTable.tsx`, `web/src/components/session/MatchDetailModal.tsx`, `web/src/components/live/LiveGameView.tsx`
- **Verdict**: **APPROVE**
- **Date**: 2026-10-06T12:27:00Z
- **Parent Conversation ID**: `18e4e66b-5835-4b6b-bd1e-a34092f47346`
- **Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_challenger_2`

---

## 1. Observation

Direct empirical investigation and adversarial execution against the web dashboard and session modal implementation produced the following concrete observations:

### 1.1 Disconnected Player Visual Treatment & Badge Specification
- **`web/src/components/live/PlayerRow.tsx:41-43, 73-81`**:
  ```tsx
  const disconnectedClasses = player.is_disconnected
    ? 'opacity-65 saturate-60 hover:opacity-85'
    : '';
  ...
  {player.is_disconnected && (
    <span
      data-testid="badge-disconnected"
      className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase tracking-wider bg-rose-950/60 text-rose-300 border border-rose-800/60 inline-flex items-center gap-1 shadow-sm"
      title="Player disconnected early; stats and match outcomes are preserved"
    >
      <WifiOff className="w-2.5 h-2.5 text-rose-400" />
      DISCONNECTED
    </span>
  )}
  ```
- In `DisconnectedPlayer.adversarial.test.tsx` (Suite 1):
  - Row container `<tr className="...">` receives `opacity-65`, `saturate-60`, and `hover:opacity-85` when `player.is_disconnected === true`.
  - Identity cell renders `data-testid="badge-disconnected"` containing `"DISCONNECTED"`, an `<svg>` element with `text-rose-400` representing Lucide `WifiOff`, and title tooltip `"Player disconnected early; stats and match outcomes are preserved"`.
  - Disconnected local player (`isLocal = true` && `is_disconnected = true`) renders both `YOU` (`bg-cyan-500/20 text-cyan-300`) and `DISCONNECTED` badges while applying cyan row highlighting (`bg-cyan-500/10`) alongside `opacity-65 saturate-60`.
  - Disconnected bot player (`is_bot = true` && `is_disconnected = true`) renders both `BOT` and `DISCONNECTED` badges.
  - Multi-platform support: Steam (`Laptop text-sky-400`), Epic (`Gamepad2 text-purple-400`), PlayStation (`Gamepad2 text-blue-400`), Xbox (`Gamepad2 text-emerald-500`), and Nintendo render cleanly alongside the disconnected badge without layout clipping.

### 1.2 Active Connected Player Isolation & Negation
- In `DisconnectedPlayer.adversarial.test.tsx` (Suite 2):
  - When `is_disconnected === false` or `is_disconnected === undefined`, rows do NOT contain `opacity-65`, `saturate-60`, or `hover:opacity-85`.
  - Active connected player rows do NOT contain `[data-testid="badge-disconnected"]` or `WifiOff` icons.
  - In a mixed 4-player roster (2 active, 2 disconnected), strict 1-to-1 isolation was confirmed: exactly 2 disconnected badges were rendered, and only the 2 disconnected player rows received dimming classes.

### 1.3 Stat Retention & Leaderboard Sorting Invariants
- **`web/src/components/live/RosterTable.tsx:47`**:
  ```tsx
  const sortedPlayers = [...players].sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0));
  ```
- In `DisconnectedPlayer.adversarial.test.tsx` (Suite 3):
  - When a player disconnects, their performance numbers (`score`, `goals`, `assists`, `saves`, `shots`, `demos`, `mmr`) are preserved verbatim in their respective `data-testid="stat-*"` cells.
  - Disconnected players with positive goals retain amber highlighting (`text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)]`); zero stats retain muted `text-slate-500`.
  - **Sorting Invariant**: A disconnected player with the top score (e.g. 950) ranks 1st in the roster table, ahead of connected teammates with lower scores (400, 200). Early disconnection does not demote or perturb score leaderboard placement.

### 1.4 Team Goals and Score Aggregation Engine
- **`web/src/components/live/RosterTable.tsx:50-51, 76-94`**:
  ```tsx
  const teamGoals = players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  const teamScore = players.reduce((sum, p) => sum + (p.stats?.score || 0), 0);
  ```
- In `DisconnectedPlayer.adversarial.test.tsx` (Suite 4):
  - `data-testid="team-goals"` and `data-testid="team-score"` accurately aggregate stats across mixed connected and disconnected players (e.g. 1 active + 2 disconnected with scores 320, 450, 180 = 950 team score, 3 team goals).
  - 100% disconnected teams (all participants disconnected) correctly sum all disconnected players' goals and score.
  - Empty team rosters cleanly render 0 goals and 0 score.
  - Stress harness with 30 pseudo-randomized players on a single team (alternating disconnected states and varying stat distributions) matched the exact mathematical sum for both team goals and team score.

### 1.5 Concluded Match Parity in `MatchDetailModal.tsx`
- **`web/src/components/session/MatchDetailModal.tsx:62-64, 84-92`**:
  ```tsx
  const disconnectedClasses = p.is_disconnected
    ? 'opacity-65 saturate-60 hover:opacity-85'
    : '';
  ...
  {p.is_disconnected && (
    <span
      data-testid="badge-disconnected"
      className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase tracking-wider bg-rose-950/60 text-rose-300 border border-rose-800/60 inline-flex items-center gap-1 shadow-sm"
      title="Player disconnected early; stats and match outcomes are preserved"
    >
      <WifiOff className="w-2.5 h-2.5 text-rose-400" />
      DISCONNECTED
    </span>
  )}
  ```
- In `DisconnectedPlayer.adversarial.test.tsx` (Suite 5):
  - In post-match reviews, disconnected players on both Blue and Orange teams render with `opacity-65 saturate-60 hover:opacity-85` and `data-testid="badge-disconnected"` with `WifiOff`.
  - Active players in the modal do NOT receive disconnected styling or badge.
  - When `match === null`, the modal returns null gracefully without rendering orphaned DOM nodes.

### 1.6 Dynamic Reconnection Lifecycle & Flapping Stress
- In `DisconnectedPlayer.adversarial.test.tsx` (Suite 6):
  - Simulated a 6-frame live match lifecycle:
    1. Match start: 2 active players (200 team score, 0 goals, 0 badges).
    2. Player 2 scores: (350 team score, 1 goal, 0 badges).
    3. Player 2 disconnects: row dims, badge renders, team totals remain 350 score, 1 goal.
    4. Player 1 scores while Player 2 is disconnected: team totals advance to 500 score, 2 goals; Player 2 remains dimmed.
    5. Player 2 reconnects: row restores normal opacity, badge is removed immediately, team totals remain 500 score, 2 goals.
  - Simulated a 10-cycle connection flapping test (rapid alternating disconnect/reconnect transitions): verified zero stuck badges, zero orphaned nodes, and constant team totals throughout.

### 1.7 LiveGameView Tree Integration & Boundary Resiliency
- In `DisconnectedPlayer.adversarial.test.tsx` (Suites 7 & 8):
  - Full `LiveGameView` component tree renders disconnected local player, teammate, and opponent simultaneously across Blue and Orange tables.
  - Column config mutation (hiding optional columns like score, goals, rank) preserves the disconnected badge and dimmed styling in the permanent player identity column.
  - Malformed/extreme input handling: extreme Unicode/HTML entity player names (`<script>alert("pwn")</script> 🚀🔥`), 100,000 score, 100 goals, and missing ranks/matchup records all render gracefully without throwing.

### 1.8 Verification Command Execution Results
1. **Vitest Test Suite (`npm test -- --run` in `web/`)**:
   ```
   RUN  v3.2.7 D:/code/rl-api-utils/web
   ✓ src/utils/formatters.test.ts (6 tests)
   ✓ src/types/columns.test.ts (5 tests)
   ✓ src/utils/platforms.test.ts (6 tests)
   ✓ src/components/common/H2HBadge.test.tsx (5 tests)
   ✓ src/utils/formatters.stress.test.tsx (26 tests)
   ✓ src/components/common/RankBadge.test.tsx (6 tests)
   ✓ src/hooks/useColumnConfig.stress.test.tsx (22 tests)
   ✓ src/components/live/RosterTable.test.tsx (3 tests)
   ✓ src/adversarial.challenge.test.tsx (33 tests)
   ✓ src/components/live/LiveGameView.adversarial.test.tsx (14 tests)
   ✓ src/components/live/DisconnectedPlayer.adversarial.test.tsx (24 tests)
   ✓ src/components/live/LiveGameView.layout.test.tsx (24 tests)
   ✓ src/components/live/LiveGameView.m2_stress.test.tsx (23 tests)
   Test Files  13 passed (13)
        Tests  197 passed (197)
     Duration  2.82s
   ```
2. **Frontend Production Build (`npm run build` in `web/`)**:
   ```
   > tsc -b && vite build
   ✓ 1923 modules transformed.
   ../internal/web/dist/index.html                   0.54 kB │ gzip:  0.35 kB
   ../internal/web/dist/assets/index-tXepU5qp.css   41.84 kB │ gzip:  7.27 kB
   ../internal/web/dist/assets/index-ev8_Pgz-.js   312.45 kB │ gzip: 90.22 kB
   ✓ built in 2.93s
   ```
3. **Backend Go Test Suite (`go test -count=1 ./...`)**:
   ```
   ok  	github.com/dank/rl-api-utils/cmd/rl-sync          0.226s
   ok  	github.com/dank/rl-api-utils/internal/auth        1.937s
   ok  	github.com/dank/rl-api-utils/internal/ballchasing 7.814s
   ok  	github.com/dank/rl-api-utils/internal/config      0.492s
   ok  	github.com/dank/rl-api-utils/internal/daemon      14.008s
   ok  	github.com/dank/rl-api-utils/internal/playertrack 5.742s
   ok  	github.com/dank/rl-api-utils/internal/psynet      4.234s
   ok  	github.com/dank/rl-api-utils/internal/session     5.735s
   ok  	github.com/dank/rl-api-utils/internal/statsapi    0.864s
   ok  	github.com/dank/rl-api-utils/internal/storage     18.636s
   ok  	github.com/dank/rl-api-utils/internal/syncer      1.036s
   ok  	github.com/dank/rl-api-utils/internal/testutil    1.069s
   ok  	github.com/dank/rl-api-utils/internal/web         0.598s
   ok  	github.com/dank/rl-api-utils/test/e2e             20.103s
   ```
4. **Standalone Binary Compilation (`go build ./cmd/rl-sync`)**:
   - Successfully compiled standalone `rl-sync.exe` with exit code 0.

---

## 2. Logic Chain

1. **Disconnected UX Specification Compliance (Observations 1.1 & 1.2)**:
   - The user requirement (R3) and dispatch scope mandate that disconnected players render with subtle desaturation, dimmed treatment (`opacity-65 saturate-60 hover:opacity-85`), and a clean badge (`data-testid="badge-disconnected"` with `WifiOff` icon).
   - In `PlayerRow.tsx:41-43`, `disconnectedClasses` is conditionally assigned if and only if `player.is_disconnected` is truthy.
   - When falsy (`false` or `undefined`), `disconnectedClasses` is empty, ensuring active connected players remain at full opacity (Observation 1.2).
   - In `PlayerRow.tsx:73-81`, the `badge-disconnected` element renders inside the player identity anchor cell, ensuring it is permanently visible regardless of which optional stat columns are toggled by the user.

2. **Stat Retention and Leaderboard Sorting Integrity (Observation 1.3)**:
   - Early disconnection must not wipe out or reset accumulated stats. In `PlayerRow.tsx`, performance cells directly bind to `stats.score`, `stats.goals`, etc.
   - In `RosterTable.tsx:47`, players are sorted purely by `(b.stats?.score || 0) - (a.stats?.score || 0)`. The `is_disconnected` flag is not used to penalize sorting. Empirical stress tests confirmed that a disconnected player who scored 950 remains 1st in the roster table, honoring their in-game contribution.

3. **Team Score Aggregation Correctness (Observation 1.4)**:
   - `teamGoals` and `teamScore` compute over `players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0)`.
   - Because disconnected players remain in `players` (rather than being dropped or segregated into an auxiliary list), their goals and scores naturally contribute to the team's aggregated totals in `data-testid="team-goals"` and `data-testid="team-score"`.
   - The 30-player randomized stress test empirically verified exact arithmetic parity between individual stats and header totals.

4. **Concluded Match Drill-Down Parity (Observation 1.5)**:
   - `MatchDetailModal.tsx` implements identical `is_disconnected` logic (`opacity-65 saturate-60 hover:opacity-85` and `data-testid="badge-disconnected"` with `WifiOff`) across both Blue and Orange rosters.
   - Post-match review box scores provide full fidelity for participants who left before the final whistle.

5. **Lifecycle State Transitions & Reconnection Cleanliness (Observation 1.6)**:
   - Live state updates flow reactively via props. When a player disconnects, `is_disconnected` flips to `true`; when they reconnect, it flips to `false`.
   - The multi-frame lifecycle test proved that React transitions between connected, disconnected, and reconnected states without stale badge artifacts or memory leaks.

---

## 3. Caveats

- **Network Layer Decoupling**: The tests in `DisconnectedPlayer.adversarial.test.tsx` verify the React presentation, layout, stat retention, and state transition layers. The underlying WebSocket ingestion (`MatchStatsExporter_TA`) and SSE distribution are tested by Go backend suites (`internal/playertrack/tracker_test.go` and `internal/session/session_test.go`).
- **No Source Code Modifications**: As a reviewer/challenger, no implementation files were altered. All challenges were conducted by authoring empirical stress test suites (`web/src/components/live/DisconnectedPlayer.adversarial.test.tsx`).

---

## 4. Conclusion

**Verdict**: **APPROVE**

The disconnected player UX treatment, stat retention, team score aggregation, and concluded match parity implemented for Milestone 2 fully satisfy all requirements:
1. **Disconnected Styling**: Applied correctly via `opacity-65 saturate-60 hover:opacity-85`.
2. **Disconnected Badge**: Renders `data-testid="badge-disconnected"` with `WifiOff` icon, text, and title tooltip.
3. **Active Negation**: Active connected players never receive disconnected styling or badges.
4. **Stat Retention & Sorting**: Player stats are retained verbatim; score-descending sort rank is preserved.
5. **Team Header Aggregation**: `data-testid="team-goals"` and `data-testid="team-score"` accurately sum all connected and disconnected participant contributions.
6. **Concluded Match Parity**: `MatchDetailModal.tsx` mirrors the exact disconnected visual indicators and box score retention.
7. **Reconnection Lifecycle**: Transitions cleanly across connect, disconnect, and reconnect events.
8. **Test & Build Verification**: All 197 frontend tests (13 test files) pass 100%, `npm run build` succeeds cleanly, all 14 Go packages pass `go test -count=1 ./...`, and `rl-sync.exe` compiles without errors.

---

## 5. Verification Method

To independently reproduce and verify this assessment:

1. **Run Vitest Test Suite**:
   ```powershell
   cd d:\code\rl-api-utils\web
   npm test -- --run
   ```
   *Expected result*: 13 test files pass, 197/197 tests pass (0 failures).

2. **Run Frontend Production Build**:
   ```powershell
   cd d:\code\rl-api-utils\web
   npm run build
   ```
   *Expected result*: `tsc -b && vite build` succeeds cleanly, generating `internal/web/dist`.

3. **Run Backend Go Test Suite**:
   ```powershell
   cd d:\code\rl-api-utils
   go test -count=1 ./...
   ```
   *Expected result*: All 14 Go packages pass with `ok`.

4. **Compile Standalone Executable**:
   ```powershell
   cd d:\code\rl-api-utils
   go build ./cmd/rl-sync
   ```
   *Expected result*: Standalone executable `rl-sync.exe` builds cleanly with exit code 0.

### Files to Inspect
- `web/src/components/live/DisconnectedPlayer.adversarial.test.tsx` (Adversarial stress suite)
- `web/src/components/live/PlayerRow.tsx` (Disconnected row styling and badge)
- `web/src/components/live/RosterTable.tsx` (Header goals & score aggregation)
- `web/src/components/session/MatchDetailModal.tsx` (Concluded match parity)
- `d:\code\rl-api-utils\.agents\teamwork\m2_challenger_2\handoff.md` (This report)
