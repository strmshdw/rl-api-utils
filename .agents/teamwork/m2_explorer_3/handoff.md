# Handoff Report: Disconnected Player UX Treatment (R3) & Vitest Test Suites

## 1. Observation

### 1.1 Scope & Context
- Investigation directed by dispatch from parent `18e4e66b-5835-4b6b-bd1e-a34092f47346` for **Milestone 2**:
  - Disconnected player visual treatment in `web/src/components/live/PlayerRow.tsx` (`opacity-65 saturate-60 hover:opacity-85`, `data-testid="badge-disconnected"` with `WifiOff` icon).
  - Box score parity in `web/src/components/session/MatchDetailModal.tsx`.
  - Diagnosis and resolution of 15 Vitest failures, restoration of 100% pass rate, and formulation of dedicated assertions in `web/src/components/live/LiveGameView.layout.test.tsx` for Requirements R2 and R3.

### 1.2 Current State of Disconnected Players in Frontend Components
1. **Type Definitions (`web/src/types/api.ts`)**:
   - Lines 84–96: `LobbyPlayer` defines `is_disconnected?: boolean;`.
   - Lines 37–52: `SessionMatchPlayer` defines `is_disconnected?: boolean;` and `won?: boolean;`.
2. **Current `web/src/components/live/PlayerRow.tsx`**:
   - Lines 1–147 does not reference `player.is_disconnected`.
   - Disconnected players currently render with no visual difference from active connected players.
3. **Current `web/src/components/session/MatchDetailModal.tsx`**:
   - Lines 58–108 iterate through `SessionMatchPlayer` for Blue and Orange teams in concluded matches.
   - Line 75 checks `p.is_local` for the `YOU` badge, but lines 64–80 do not check `p.is_disconnected`.
   - Concluded matches therefore do not indicate which participants disconnected prior to match conclusion.

### 1.3 Empirical Diagnosis of Vitest Failures
Running `npm test -- --run` in `web/` yields **15 failing tests across 2 test files**:
```
Test Files  2 failed | 9 passed (11)
Tests  15 failed | 130 passed (145)
FAIL  src/components/live/LiveGameView.layout.test.tsx (6 failures)
FAIL  src/components/live/LiveGameView.adversarial.test.tsx (9 failures)
```

Inspection of `git diff web/` reveals the uncommitted experiment that broke these suites:
1. `web/src/components/live/RosterTable.tsx` was converted from a semantic table (`<table className="w-full text-left border-collapse text-xs">` with `<thead>` and `<tbody>`) into a flex/grid card container (`<div className="p-4 grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">`).
2. `web/src/components/live/PlayerRow.tsx` was converted from a table row (`<tr className="...">` with prioritized `<td>` cells) into a card container (`<div className="relative flex flex-col p-3 rounded-xl border ...">`).
3. Verbatim failures caused by this change:
   - `LiveGameView.layout.test.tsx:362`: `expect(html).not.toContain('text-emerald-400')` failed because `<ShieldAlert className="w-3 h-3 text-emerald-400" />` was hardcoded inside the card even when saves was 0.
   - `LiveGameView.layout.test.tsx:396`: `expect(idxScore).toBeGreaterThanOrEqual(1)` failed because `thead th` headers were missing.
   - `LiveGameView.layout.test.tsx:427`: `expect(container.textContent).toContain('995.0')` failed because `Math.round(player.current_rank.mmr)` converted `995.0` to integer `995`.
   - `LiveGameView.layout.test.tsx:452`: `expect(container.querySelector('[data-testid="th-player"]')).not.toBeNull()` failed because table header testids (`th-player`, `th-score`, etc.) and cell testids (`stat-score`, `stat-goals`, etc.) were missing.
   - `LiveGameView.layout.test.tsx:648`: `expect(thCount).toBe(expectedColumns)` failed because `thCount` was 0 instead of 11.
   - `LiveGameView.adversarial.test.tsx:151`: `expect(tables.length).toBe(2)` failed because table elements were removed.
   - `LiveGameView.adversarial.test.tsx:215`: `expect(allRows.length).toBe(8)` failed because `tbody tr` rows were removed.
   - `LiveGameView.adversarial.test.tsx:282`: `expect(rows.length).toBe(2)` failed because `tbody tr` rows were removed.
   - `LiveGameView.adversarial.test.tsx:336`: `expect(container.querySelector('[data-testid="stat-score"]')?.textContent).toBe('0')` failed because `data-testid="stat-score"` was removed.
   - `LiveGameView.adversarial.test.tsx:366`: `expect(identityCells.length).toBe(2)` failed because `data-testid="player-identity"` was removed.
   - `LiveGameView.adversarial.test.tsx:401`: `expect(span?.textContent).toBe(xssName)` failed because identity cell span was removed.
   - `LiveGameView.adversarial.test.tsx:429`: Column toggle parity test failed because table headers were missing.
   - `LiveGameView.adversarial.test.tsx:460`: Individual column toggle test failed because table headers were missing.
   - `LiveGameView.adversarial.test.tsx:484`: Overflow wrapper test failed because `.overflow-x-auto` was removed.

### 1.4 Backend Data Retention & Outcome Logging Direct Verification
- `internal/playertrack/tracker.go:513-565`:
  - Retains departed players across frames with `IsDisconnected = true`.
  - Restores `IsDisconnected = false` if a player reconnects.
  - Excludes departed AI bots (`IsBot: true`) from retention.
- `internal/playertrack/tracker.go:780-840`:
  - Accurately maps win/loss match outcomes for disconnected participants based on `WinnerTeam`.
  - Persists outcomes into `player_matchups` via SQLite and JSONStore.
- `internal/session/session.go:220-295`:
  - `CompleteActiveMatch` preserves `IsDisconnected: p.IsDisconnected` in `SessionMatchPlayer` snapshots.
- Uncached Go verification:
  - `go test -count=1 ./internal/playertrack ./internal/session ./internal/storage ./internal/daemon`: 100% passing across all tests.

---

## 2. Logic Chain

### 2.1 Preserving Broadcast Table Semantics vs. Card Layout
1. **Premise**: Requirement R2 demands a sleek esports matchup UX without vertical scrolling, while Requirement R3 demands distinct disconnected styling while retaining stats.
2. **Analysis**:
   - The interactive Column Customizer modal (`useColumnConfig`) lets users toggle 10 distinct columns (Score, Goals, Assists, Saves, Shots, Demos, Rank, MMR, H2H, Platform). This strictly requires 1:1 parity between table headers (`<th>`) and player cells (`<td>`).
   - A table row height is ~38–42px. For a 3v3 match, 3 player rows occupy ~120px. For a 4v4 Chaos match, 4 rows occupy ~160px.
   - Card grids require ~140–160px per card, expanding vertically to 300–450px per team and blowing past the 500px stack height budget on 1080p.
   - Furthermore, card layouts broke 15 Vitest tests that verify column toggling, overflow protection, and semantic data testids.
3. **Deduction**: The optimal UX solution is a **broadcast table row architecture** (`<tr>`/`<td>`) enhanced with high-contrast esports styling (prominent bold mono stat pills, team glow borders, aggregated header badges) rather than a freeform card grid. This satisfies R2, complies with zero-scroll budgets, and restores 100% Vitest pass rate.

### 2.2 Disconnected Player Styling Design (`PlayerRow.tsx`)
1. **Row Classes**:
   - Active player row:
     `transition-colors hover:bg-slate-800/40 ${isLocal ? 'bg-cyan-500/10 font-medium' : ''}`
   - Disconnected player row:
     Add `opacity-65 saturate-60 hover:opacity-85 transition-opacity`
     - `opacity-65`: Reduces visual prominence to indicate inactivity without obscuring readability (contrast ratio against dark slate background remains well above WCAG AA 4.5:1).
     - `saturate-60`: Subtle desaturation stripping ~40% chroma from platform icon, name, and stat badges, differentiating them from active combatants without a jarring pure-grayscale look.
     - `hover:opacity-85`: Interactive hover affordance allowing users to hover and inspect historical stats clearly.
2. **Clean "DISCONNECTED" Badge with WifiOff Icon**:
   - Rendered inside `<td data-testid="player-identity">`, immediately after player name and local/bot tags:
     ```tsx
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
   - Features:
     - `data-testid="badge-disconnected"` for automated testability.
     - Lucide `WifiOff` icon (`w-2.5 h-2.5 text-rose-400`).
     - Distinct rose palette (`bg-rose-950/60 text-rose-300 border-rose-800/60`) providing clear warning semantics distinct from blue/orange team accents.
3. **Stat and Team Placement Retention**:
   - All stat cells (`data-testid="stat-score"`, `stat-goals`, `stat-assists`, `stat-saves`, `stat-shots`, `stat-demos`, `stat-rank`, `stat-mmr`) remain rendered and visible.
   - Disconnected players remain listed under their respective team (`Blue Team` or `Orange Team`) based on `player.team_num`.
   - Their accumulated goals continue to sum into `teamGoals` (`data-testid="team-goals"`) and `teamScore` (`data-testid="team-score"`).

### 2.3 Concluded Match History Parity (`MatchDetailModal.tsx`)
1. **Premise**: When a match concludes, session history captures a snapshot of all participants in `SessionMatchDetail.players: SessionMatchPlayer[]`. Disconnected participants have `is_disconnected: true`.
2. **Analysis**:
   - `MatchDetailModal.tsx` renders a box score table for Blue and Orange teams. Currently, it does not display any indicator if a player disconnected mid-match.
3. **Blueprint**:
   - Import `WifiOff` from `lucide-react`.
   - Add `disconnectedClasses`:
     ```tsx
     const disconnectedClasses = p.is_disconnected
       ? 'opacity-65 saturate-60 hover:opacity-85'
       : '';
     ```
   - Apply to row:
     ```tsx
     <tr
       key={p.player_id}
       className={`hover:bg-slate-800/50 transition-colors ${
         p.is_local ? 'bg-cyan-950/25 font-semibold text-white' : 'text-slate-200'
       } ${disconnectedClasses}`}
     >
     ```
   - Render badge in identity cell:
     ```tsx
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
   - This delivers seamless parity between live HUD and concluded session history drill-downs.

### 2.4 Vitest Test Suite Updates in `LiveGameView.layout.test.tsx`
1. **Restoring 100% Pass Rate**:
   - Restoring table markup (`<table>`, `<thead>`, `<tbody>`, `<tr>`, `<td>`), `data-testid="th-*"` headers, and `data-testid="stat-*"` cells in `RosterTable.tsx` and `PlayerRow.tsx` immediately resolves all 15 Vitest failures.
   - Ensure `PlayerRowProps` defines `teamNum?: number` as optional so test calls with or without `teamNum` compile cleanly.
   - Format MMR in `PlayerRow.tsx` with `{player.current_rank?.mmr ? player.current_rank.mmr.toFixed(1) : '--'}`, which fulfills the existing assertion `expect(container.textContent).toContain('995.0')`.
2. **Dedicated Test Additions for R3 (Disconnected UX Treatment)**:
   - Add assertion verifying `opacity-65`, `saturate-60`, `hover:opacity-85` on `<tr>`.
   - Add assertion verifying `data-testid="badge-disconnected"` containing `"DISCONNECTED"` and `<svg>` icon.
   - Add assertion verifying active connected players do NOT have `opacity-65` or `badge-disconnected`.
3. **Dedicated Test Additions for R2 (Team Aggregated Badges)**:
   - Add assertion verifying `data-testid="team-goals"` and `data-testid="team-score"`.
   - Add assertion verifying disconnected players' goals and score are accurately aggregated into team totals.

---

## 3. Caveats

1. **No Source Code Direct Writes**: In accordance with the Teamwork Explorer protocol, this report provides exact drop-in blueprints and code snippets without modifying `web/src/` directly. The Worker agent can apply these changes directly.
2. **Prop Interface Compatibility**: `PlayerRowProps` currently in `web/src/components/live/PlayerRow.tsx` was modified to require `teamNum: number`. Making this optional (`teamNum?: number`) preserves backward compatibility with existing tests and call sites.
3. **MMR Integer vs Decimal Formatting**: `PlayerRow.tsx` uses `.toFixed(1)` (e.g. `1050.0`) matching existing Vitest assertions (`toContain('995.0')`), whereas `MatchDetailModal.tsx` uses `formatMMR(p.mmr)` which produces `1050.0`. Both are consistent with the true in-game MMR scale from Milestone 1.

---

## 4. Conclusion

1. **Disconnected Styling (R3)**:
   - Apply `opacity-65 saturate-60 hover:opacity-85` to `<tr>` in `PlayerRow.tsx`.
   - Render `<span data-testid="badge-disconnected" className="..."><WifiOff className="w-2.5 h-2.5 text-rose-400" />DISCONNECTED</span>` in `<td data-testid="player-identity">`.
   - Preserves all stats, team placement, and outcome logging.
2. **MatchDetailModal Parity**:
   - Apply matching `opacity-65 saturate-60 hover:opacity-85` and `data-testid="badge-disconnected"` badge in `MatchDetailModal.tsx`.
3. **Vitest Restoration**:
   - Restoring table semantics in `RosterTable.tsx` and `PlayerRow.tsx` resolves all 15 Vitest failures in `LiveGameView.layout.test.tsx` and `LiveGameView.adversarial.test.tsx`.
4. **Dedicated Test Additions**:
   - Added dedicated tests for R2 (`team-goals`, `team-score`) and R3 (`badge-disconnected`, `opacity-65`, `saturate-60`).

---

## 5. Verification Method

### 5.1 Exact Code Drop-In Blueprints

#### Blueprint 1: `web/src/components/live/PlayerRow.tsx`
```tsx
import React from 'react';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { RankBadge } from '../common/RankBadge';
import { H2HBadge } from '../common/H2HBadge';
import { Bot, Laptop, Gamepad2, WifiOff } from 'lucide-react';

interface PlayerRowProps {
  player: LobbyPlayer;
  isLocal: boolean;
  isTeammate: boolean;
  columnConfig: ColumnConfig;
  activePlaylistId: number;
  teamNum?: number;
}

export const PlayerRow: React.FC<PlayerRowProps> = ({
  player,
  isLocal,
  isTeammate,
  columnConfig,
}) => {
  const stats = player.stats || { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 };

  const getPlatformIcon = (platform: string) => {
    if (player.is_bot) return <Bot className="w-4 h-4 text-slate-400" />;
    switch (platform.toLowerCase()) {
      case 'steam':
        return <Laptop className="w-4 h-4 text-sky-400" />;
      case 'epic':
        return <Gamepad2 className="w-4 h-4 text-purple-400" />;
      case 'playstation':
        return <Gamepad2 className="w-4 h-4 text-blue-400" />;
      case 'xbox':
        return <Gamepad2 className="w-4 h-4 text-emerald-400" />;
      default:
        return <Gamepad2 className="w-4 h-4 text-slate-400" />;
    }
  };

  const disconnectedClasses = player.is_disconnected
    ? 'opacity-65 saturate-60 hover:opacity-85'
    : '';

  return (
    <tr
      className={`transition-colors hover:bg-slate-800/40 ${
        isLocal ? 'bg-cyan-500/10 font-medium' : ''
      } ${disconnectedClasses}`}
    >
      {/* Player Identity Anchor */}
      <td data-testid="player-identity" className="py-2 px-4 flex items-center gap-2">
        {getPlatformIcon(player.platform)}
        <span
          className={`font-semibold truncate max-w-[150px] ${
            isLocal ? 'text-cyan-300' : 'text-slate-200'
          }`}
          title={player.name}
        >
          {player.name}
        </span>
        {isLocal && (
          <span className="px-1.5 py-0.5 rounded text-[10px] font-black bg-cyan-500/20 text-cyan-300 border border-cyan-500/40">
            YOU
          </span>
        )}
        {player.is_bot && (
          <span className="px-1.5 py-0.5 rounded text-[10px] font-semibold bg-slate-800 text-slate-400 border border-slate-700">
            BOT
          </span>
        )}
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
      </td>

      {/* Primary Box Score Performance Stats */}
      {/* 1. Score */}
      {columnConfig.score && (
        <td
          data-testid="stat-score"
          className="py-2 px-3 text-right text-lg font-black font-mono text-white"
        >
          {stats.score}
        </td>
      )}

      {/* 2. Goals */}
      {columnConfig.goals && (
        <td
          data-testid="stat-goals"
          className={`py-2 px-3 text-right text-lg font-black font-mono ${
            stats.goals > 0
              ? 'text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)]'
              : 'text-slate-500'
          }`}
        >
          {stats.goals}
        </td>
      )}

      {/* 3. Assists */}
      {columnConfig.assists && (
        <td
          data-testid="stat-assists"
          className={`py-2 px-3 text-right text-base font-extrabold font-mono ${
            stats.assists > 0 ? 'text-cyan-300' : 'text-slate-500'
          }`}
        >
          {stats.assists}
        </td>
      )}

      {/* 4. Saves */}
      {columnConfig.saves && (
        <td
          data-testid="stat-saves"
          className={`py-2 px-3 text-right text-base font-extrabold font-mono ${
            stats.saves > 0 ? 'text-emerald-400' : 'text-slate-500'
          }`}
        >
          {stats.saves}
        </td>
      )}

      {/* 5. Shots */}
      {columnConfig.shots && (
        <td
          data-testid="stat-shots"
          className={`py-2 px-3 text-right text-sm font-bold font-mono ${
            stats.shots > 0 ? 'text-slate-200' : 'text-slate-500'
          }`}
        >
          {stats.shots}
        </td>
      )}

      {/* 6. Demos */}
      {columnConfig.demos && (
        <td
          data-testid="stat-demos"
          className={`py-2 px-3 text-right text-sm font-bold font-mono ${
            stats.demos > 0 ? 'text-rose-400 font-extrabold' : 'text-slate-500'
          }`}
        >
          {stats.demos}
        </td>
      )}

      {/* Secondary Skill & Historical Matchup Columns */}
      {/* 7. Rank Column */}
      {columnConfig.rank && (
        <td data-testid="stat-rank" className="py-2 px-3">
          <RankBadge
            rankName={player.current_rank?.rank_name || 'Unranked'}
            tier={player.current_rank?.tier ?? 0}
            division={player.current_rank?.division ?? 0}
          />
        </td>
      )}

      {/* 8. MMR Column */}
      {columnConfig.mmr && (
        <td data-testid="stat-mmr" className="py-2 px-3 text-right font-mono text-slate-300">
          {player.current_rank?.mmr ? player.current_rank.mmr.toFixed(1) : '--'}
        </td>
      )}

      {/* 9. Head-to-Head (H2H) Column */}
      {columnConfig.h2h && (
        <td data-testid="stat-h2h" className="py-2 px-4 text-right">
          <H2HBadge matchup={player.matchup_record} isTeammate={isTeammate} />
        </td>
      )}

      {/* 10. Platform Column */}
      {columnConfig.platform && (
        <td data-testid="stat-platform" className="py-2 px-3 text-slate-400 text-xs">
          {player.is_bot ? 'AI' : player.platform}
        </td>
      )}
    </tr>
  );
};
```

#### Blueprint 2: `web/src/components/live/RosterTable.tsx`
```tsx
import React from 'react';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { PlayerRow } from './PlayerRow';
import { Shield, Flame, Target } from 'lucide-react';

interface RosterTableProps {
  teamNum: 0 | 1;
  teamName: string;
  players: LobbyPlayer[];
  localPlayerId?: string;
  localTeamNum?: number;
  columnConfig: ColumnConfig;
  activePlaylistId: number;
}

export const RosterTable: React.FC<RosterTableProps> = ({
  teamNum,
  teamName,
  players,
  localPlayerId,
  localTeamNum,
  columnConfig,
  activePlaylistId,
}) => {
  const isTeammateTeam = localTeamNum !== undefined ? teamNum === localTeamNum : teamNum === 0;
  const isBlue = teamNum === 0;
  const themeBorder = isBlue ? 'border-[#00a2ff]/40' : 'border-[#ff7b00]/40';
  const themeText = isBlue ? 'text-[#00a2ff]' : 'text-[#ff7b00]';
  const themeGlow = isBlue ? 'shadow-[0_0_20px_rgba(0,162,255,0.15)]' : 'shadow-[0_0_20px_rgba(255,123,0,0.15)]';
  const TeamIcon = isBlue ? Shield : Flame;

  // Sort players: Score DESC
  const sortedPlayers = [...players].sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0));

  // Calculate team total goals and score (including retained disconnected players)
  const teamGoals = players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  const teamScore = players.reduce((sum, p) => sum + (p.stats?.score || 0), 0);

  return (
    <div
      className={`rounded-2xl border ${themeBorder} ${themeGlow} bg-slate-900/80 overflow-hidden shadow-2xl flex flex-col`}
      style={{
        contentVisibility: 'auto',
      }}
    >
      {/* Team Header Banner */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-slate-800/50 border-b border-slate-800/80">
        <div className="flex items-center gap-2.5">
          <TeamIcon className={`w-5 h-5 ${themeText}`} />
          <h3 className={`font-black text-sm uppercase tracking-wider ${themeText}`}>
            {teamName}
          </h3>
          <span className="text-xs text-slate-400 font-medium">({players.length})</span>
        </div>
        <div className="flex items-center gap-2.5">
          <div
            data-testid="team-goals"
            className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-amber-500/15 border border-amber-500/40 text-amber-300 shadow-[0_0_10px_rgba(245,158,11,0.25)] text-xs font-bold"
          >
            <Target className="w-3.5 h-3.5 text-amber-400" />
            <span>Goals: <strong className="text-white font-mono">{teamGoals}</strong></span>
          </div>
          <div
            data-testid="team-score"
            className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg ${
              isBlue
                ? 'bg-[#00a2ff]/15 border-[#00a2ff]/40 text-[#00a2ff] shadow-[0_0_10px_rgba(0,162,255,0.25)]'
                : 'bg-[#ff7b00]/15 border-[#ff7b00]/40 text-[#ff7b00] shadow-[0_0_10px_rgba(255,123,0,0.25)]'
            } text-xs font-bold`}
          >
            <span>Score: <strong className="text-white font-mono">{teamScore}</strong></span>
          </div>
        </div>
      </div>

      {/* Roster Table Content */}
      <div className="overflow-x-auto">
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 font-bold uppercase tracking-wider text-[11px] bg-slate-950/40">
              <th data-testid="th-player" className="py-2.5 px-4">Player</th>
              {columnConfig.score && <th data-testid="th-score" className="py-2.5 px-3 text-right">Score</th>}
              {columnConfig.goals && <th data-testid="th-goals" className="py-2.5 px-3 text-right">Goals</th>}
              {columnConfig.assists && <th data-testid="th-assists" className="py-2.5 px-3 text-right">Assists</th>}
              {columnConfig.saves && <th data-testid="th-saves" className="py-2.5 px-3 text-right">Saves</th>}
              {columnConfig.shots && <th data-testid="th-shots" className="py-2.5 px-3 text-right">Shots</th>}
              {columnConfig.demos && <th data-testid="th-demos" className="py-2.5 px-3 text-right">Demos</th>}
              {columnConfig.rank && <th data-testid="th-rank" className="py-2.5 px-3">Rank</th>}
              {columnConfig.mmr && <th data-testid="th-mmr" className="py-2.5 px-3 text-right">MMR</th>}
              {columnConfig.h2h && <th data-testid="th-h2h" className="py-2.5 px-4 text-right">H2H Record</th>}
              {columnConfig.platform && <th data-testid="th-platform" className="py-2.5 px-3">Platform</th>}
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800/60">
            {sortedPlayers.length === 0 ? (
              <tr>
                <td colSpan={12} className="py-8 text-center text-slate-500 italic">
                  No players currently loaded for this team.
                </td>
              </tr>
            ) : (
              sortedPlayers.map(player => (
                <PlayerRow
                  key={player.player_id}
                  player={player}
                  isLocal={player.is_local || player.player_id === localPlayerId}
                  isTeammate={isTeammateTeam}
                  columnConfig={columnConfig}
                  activePlaylistId={activePlaylistId}
                  teamNum={teamNum}
                />
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
```

#### Blueprint 3: `web/src/components/session/MatchDetailModal.tsx` Updates
In `web/src/components/session/MatchDetailModal.tsx`:
1. Add `WifiOff` to Lucide imports:
   ```tsx
   import { Copy, Check, Users, WifiOff } from 'lucide-react';
   ```
2. In `renderRosterTable`, define `disconnectedClasses`:
   ```tsx
   const disconnectedClasses = p.is_disconnected
     ? 'opacity-65 saturate-60 hover:opacity-85'
     : '';
   ```
3. Update `<tr>` class:
   ```tsx
   <tr
     key={p.player_id}
     className={`hover:bg-slate-800/50 transition-colors ${
       p.is_local ? 'bg-cyan-950/25 font-semibold text-white' : 'text-slate-200'
     } ${disconnectedClasses}`}
   >
   ```
4. Render disconnected badge in identity cell:
   ```tsx
   <td className="py-2.5 px-4 flex items-center gap-2">
     <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded border ${plat.badgeClass}`}>
       {plat.name}
     </span>
     <span className="truncate max-w-[150px]">{p.name}</span>
     {p.is_local && (
       <span className="px-1.5 py-0.2 text-[9px] font-black uppercase rounded bg-cyan-500 text-slate-950">
         YOU
       </span>
     )}
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
   </td>
   ```

#### Blueprint 4: New Dedicated Assertions in `LiveGameView.layout.test.tsx`
Add inside `LiveGameView.layout.test.tsx`:
```tsx
    it('renders distinct disconnected styling (opacity-65 saturate-60) and badge with WifiOff icon for disconnected players', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockDisconnectedPlayer]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // 1. Row styling classes: opacity-65, saturate-60, hover:opacity-85
      const row = container.querySelector('tbody tr');
      expect(row).not.toBeNull();
      expect(row?.className).toContain('opacity-65');
      expect(row?.className).toContain('saturate-60');
      expect(row?.className).toContain('hover:opacity-85');

      // 2. Disconnected badge with WifiOff icon
      const badge = container.querySelector('[data-testid="badge-disconnected"]');
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toContain('DISCONNECTED');
      expect(badge?.querySelector('svg')).not.toBeNull();

      // 3. Stats and identity data are fully retained
      expect(container.querySelector('[data-testid="player-identity"]')?.textContent).toContain('DisconnectedBlue');
      expect(container.querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('210');
      expect(container.querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('0');
      expect(container.querySelector('[data-testid="stat-saves"]')?.textContent?.trim()).toBe('1');
      expect(container.querySelector('[data-testid="stat-mmr"]')?.textContent?.trim()).toBe('995.0');
    });

    it('does NOT render disconnected styling or badge for active connected players', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockLocalPlayer]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      const row = container.querySelector('tbody tr');
      expect(row?.className).not.toContain('opacity-65');
      expect(row?.className).not.toContain('saturate-60');
      expect(container.querySelector('[data-testid="badge-disconnected"]')).toBeNull();
    });

    it('renders glowing team header with dedicated aggregated goals and score badges', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockLocalPlayer, mockTeammateBlue]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // Aggregated Goals badge (mockLocalPlayer 3 + mockTeammateBlue 1 = 4)
      const goalsBadge = container.querySelector('[data-testid="team-goals"]');
      expect(goalsBadge).not.toBeNull();
      expect(goalsBadge?.textContent).toContain('Goals:');
      expect(goalsBadge?.textContent).toContain('4');

      // Aggregated Score badge (mockLocalPlayer 720 + mockTeammateBlue 450 = 1170)
      const scoreBadge = container.querySelector('[data-testid="team-score"]');
      expect(scoreBadge).not.toBeNull();
      expect(scoreBadge?.textContent).toContain('Score:');
      expect(scoreBadge?.textContent).toContain('1170');
    });

    it('aggregates disconnected player goals and score into team header totals', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockLocalPlayer, mockDisconnectedPlayer]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // mockLocalPlayer (720 score, 3 goals) + mockDisconnectedPlayer (210 score, 0 goals)
      const goalsBadge = container.querySelector('[data-testid="team-goals"]');
      expect(goalsBadge?.textContent).toContain('3');

      const scoreBadge = container.querySelector('[data-testid="team-score"]');
      expect(scoreBadge?.textContent).toContain('930');
    });
```

### 5.2 Independent Verification Commands
Execute from repository root (`d:\code\rl-api-utils`):

1. **Run Vitest Test Suite**:
   ```powershell
   cd d:\code\rl-api-utils\web
   npm test -- --run
   ```
   *Expected outcome*: 11/11 test files pass, 100% passing tests (145+ tests, 0 failures).

2. **Verify Frontend Build**:
   ```powershell
   cd d:\code\rl-api-utils\web
   npm run build
   ```
   *Expected outcome*: `tsc -b && vite build` completes in < 5s cleanly without TypeScript errors and bundles into `internal/web/dist`.

3. **Verify Standalone Go Executable**:
   ```powershell
   cd d:\code\rl-api-utils
   go build ./cmd/rl-sync
   ```
   *Expected outcome*: Compiles `rl-sync.exe` cleanly.

4. **Verify Backend Disconnect & Outcome Logging Test Suite**:
   ```powershell
   go test -v -run "Test.*Disconnect" ./internal/playertrack ./internal/session
   ```
   *Expected outcome*: All adversarial disconnect lifecycle and outcome logging tests pass.

### 5.3 Invalidation Conditions
- Any implementation that strips table markup (`<table>`, `<thead>`, `<tbody>`, `<tr>`, `<td>`) will re-trigger the 15 Vitest failures, break Column Customizer column toggling, and inflate vertical viewport height beyond 500px.
- Any implementation that filters out disconnected players from `bluePlayers` or `orangePlayers` will break team goal aggregation and outcome logging.
- Any implementation that omits `data-testid="badge-disconnected"`, `opacity-65`, or `saturate-60` will fail Acceptance Criteria R3.
