# Handoff Report: Player Stat Prioritization & Typography Revamp

**Author**: `m3_explorer_1` (Exploration Agent)  
**Date**: 2026-10-06T09:35:00Z  
**Milestone**: Milestone M3 (Requirement R1: Live Game UI Revamp)  
**Focus Area**: `web/src/components/live/PlayerRow.tsx` & `RosterTable.tsx`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1`

---

## 1. Observation

### 1.1 Existing Component Structure in `PlayerRow.tsx`
Direct inspection of `web/src/components/live/PlayerRow.tsx` (Lines 40–150) reveals:
```tsx
// Current Column Ordering in PlayerRow.tsx (Lines 69-148)
{/* Platform Column */}
{columnConfig.platform && (
  <td className="py-2.5 px-3 text-slate-400 text-xs">
    {player.is_bot ? 'AI' : player.platform}
  </td>
)}

{/* Rank Column */}
{columnConfig.rank && (
  <td className="py-2.5 px-3">
    <RankBadge
      rankName={player.current_rank?.rank_name || 'Unranked'}
      tier={player.current_rank?.tier ?? 0}
      division={player.current_rank?.division ?? 0}
    />
  </td>
)}

{/* MMR Column */}
{columnConfig.mmr && (
  <td className="py-2.5 px-3 text-right font-mono text-slate-300">
    {player.current_rank?.mmr ? player.current_rank.mmr.toFixed(1) : '--'}
  </td>
)}

{/* Box Score Stats Columns */}
{columnConfig.score && (
  <td className="py-2.5 px-3 text-right font-mono font-bold text-slate-100">
    {stats.score}
  </td>
)}

{columnConfig.goals && (
  <td
    className={`py-2.5 px-3 text-right font-mono font-bold ${
      stats.goals > 0 ? 'text-amber-400' : 'text-slate-400'
    }`}
  >
    {stats.goals}
  </td>
)}

{columnConfig.assists && (
  <td className="py-2.5 px-3 text-right font-mono text-slate-300">
    {stats.assists}
  </td>
)}

{columnConfig.saves && (
  <td className="py-2.5 px-3 text-right font-mono text-slate-300">
    {stats.saves}
  </td>
)}

{columnConfig.shots && (
  <td className="py-2.5 px-3 text-right font-mono text-slate-300">
    {stats.shots}
  </td>
)}

{columnConfig.demos && (
  <td
    className={`py-2.5 px-3 text-right font-mono ${
      stats.demos > 0 ? 'text-rose-400 font-bold' : 'text-slate-400'
    }`}
  >
    {stats.demos}
  </td>
)}

{/* Head-to-Head (H2H) Column */}
{columnConfig.h2h && (
  <td className="py-2.5 px-4 text-right">
    <H2HBadge
      matchup={player.matchup_record}
      isTeammate={isTeammate}
    />
  </td>
)}
```

### 1.2 Existing Column Sequence in `RosterTable.tsx`
Direct inspection of `web/src/components/live/RosterTable.tsx` (Lines 65–79) reveals:
```tsx
<table className="w-full text-left border-collapse text-xs">
  <thead>
    <tr className="border-b border-slate-800 text-slate-400 font-bold uppercase tracking-wider text-[11px] bg-slate-950/30">
      <th className="py-3 px-4">Player</th>
      {columnConfig.platform && <th className="py-3 px-3">Platform</th>}
      {columnConfig.rank && <th className="py-3 px-3">Rank</th>}
      {columnConfig.mmr && <th className="py-3 px-3 text-right">MMR</th>}
      {columnConfig.score && <th className="py-3 px-3 text-right">Score</th>}
      {columnConfig.goals && <th className="py-3 px-3 text-right">Goals</th>}
      {columnConfig.assists && <th className="py-3 px-3 text-right">Assists</th>}
      {columnConfig.saves && <th className="py-3 px-3 text-right">Saves</th>}
      {columnConfig.shots && <th className="py-3 px-3 text-right">Shots</th>}
      {columnConfig.demos && <th className="py-3 px-3 text-right">Demos</th>}
      {columnConfig.h2h && <th className="py-3 px-4 text-right">H2H Record</th>}
    </tr>
  </thead>
```

### 1.3 Key Observations
1. **Displaced Visual Priority**: Secondary/static metadata (`Platform`, `Rank`, `MMR`) precedes live match performance statistics (`Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`). A user viewing the active match must visually scan past three static columns before reaching actual in-match gameplay metrics.
2. **Minuscule Monochromatic Typography**:
   - `Score` is rendered in `font-bold text-slate-100` inheriting `text-xs` (12px, 0.75rem).
   - `Goals` is rendered with basic `font-bold` at 12px with no visual glow or hero weight.
   - `Assists`, `Saves`, and `Shots` are monochromatic `text-slate-300 font-mono` at 12px.
   - `Demos` is rendered with `text-rose-400 font-bold` at 12px.
   - No distinction is made between offensive playmaking (Assists) and defensive shot-stopping (Saves).
3. **Absence of Semantic Test Attributes**: Neither `PlayerRow.tsx` nor `RosterTable.tsx` has `data-testid` attributes on stat elements, impeding automated DOM structure and layout verification.
4. **Existing Test Suite Baseline**:
   - `npm test` runs Vitest v3 with Happy DOM v20.
   - 9 test files passed, 112 tests passed:
     - `src/components/live/RosterTable.test.tsx` (3 tests)
     - `src/adversarial.challenge.test.tsx` (33 tests)
     - `src/hooks/useColumnConfig.stress.test.tsx` (22 tests)
     - `src/utils/formatters.stress.test.tsx` (26 tests)
     - Plus badges, platform, and column unit tests.
   - Key test assertions:
     - `expect(html).toContain('<span>9W-1L</span>');`
     - `expect(html).toContain('(90%)');`
     - `expect(html).toContain('YOU');`
     - `expect(html).toContain('>--<');`

---

## 2. Logic Chain

### 2.1 Reordering Column Sequence
The authoritative user request specifies:
> "Revamp the live game UI to prioritize and enlarge player stats while removing superfluous elements..."

In competitive Rocket League, the player box score is the primary focal point of in-game telemetry.
Placing `Score`, `Goals`, `Assists`, `Saves`, `Shots`, and `Demos` immediately after `Player` anchors the user's attention directly on in-game performance:

| Order | Column Key | Visual Category | Header Label | Data Alignment |
|---|---|---|---|---|
| 1 | `player` | Player Identity | `Player` | Left (`px-4`) |
| 2 | `score` | Hero Stat (Composite Points) | `Score` | Right (`px-3`) |
| 3 | `goals` | Hero Stat (Offense) | `Goals` | Right (`px-3`) |
| 4 | `assists` | Key Stat (Playmaking) | `Assists` | Right (`px-3`) |
| 5 | `saves` | Key Stat (Defense) | `Saves` | Right (`px-3`) |
| 6 | `shots` | Supporting Stat (Offense) | `Shots` | Right (`px-3`) |
| 7 | `demos` | Supporting Stat (Tactical) | `Demos` | Right (`px-3`) |
| 8 | `rank` | Secondary Skill Metadata | `Rank` | Left (`px-3`) |
| 9 | `mmr` | Secondary Skill Rating | `MMR` | Right (`px-3`) |
| 10 | `h2h` | Historical Matchup Record | `H2H Record` | Right (`px-4`) |
| 11 | `platform` | Identity Network (Optional) | `Platform` | Left (`px-3`) |

Placing `Platform` at the very end ensures that users who toggle it on can view it without displacing the primary stat metrics. Furthermore, this exactly aligns with the definition order in `STAT_COLUMNS` (`web/src/types/columns.ts`) where `platform` is the final column.

### 2.2 Sizing & Color Accent Architecture
To create an intuitive, glanceable HUD hierarchy:

1. **Score (Hero Metric, 18px)**:
   - Sizing: `text-lg` (18px / 1.125rem)
   - Weight: `font-black` (weight 900)
   - Font: `font-mono`
   - Color: `text-white`
   - Test ID: `data-testid="stat-score"`
   - Rationale: Rocket League composite score represents overall match contribution. Must dominate the stat cluster.

2. **Goals (Hero Metric, 18px + Gold Luminescence)**:
   - Sizing: `text-lg` (18px / 1.125rem)
   - Weight: `font-black` (weight 900)
   - Font: `font-mono`
   - Color / Accent: `stats.goals > 0 ? 'text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)]' : 'text-slate-500'`
   - Test ID: `data-testid="stat-goals"`
   - Rationale: Goals determine match outcome. Golden glow with drop shadow creates immediate visual prominence. Zero goals dim to `text-slate-500` to eliminate visual noise.

3. **Assists (Playmaking Metric, 16px Cyan Accent)**:
   - Sizing: `text-base` (16px / 1.0rem)
   - Weight: `font-extrabold` (weight 800)
   - Font: `font-mono`
   - Color / Accent: `stats.assists > 0 ? 'text-cyan-300' : 'text-slate-500'`
   - Test ID: `data-testid="stat-assists"`
   - Rationale: Cyan accent signifies creative playmaking and goal assists.

4. **Saves (Defensive Metric, 16px Emerald Accent)**:
   - Sizing: `text-base` (16px / 1.0rem)
   - Weight: `font-extrabold` (weight 800)
   - Font: `font-mono`
   - Color / Accent: `stats.saves > 0 ? 'text-emerald-400' : 'text-slate-500'`
   - Test ID: `data-testid="stat-saves"`
   - Rationale: Emerald green signifies goal-line defense and clutch stops, contrasting sharply with cyan assists.

5. **Shots (Offensive Activity, 14px Slate)**:
   - Sizing: `text-sm` (14px / 0.875rem)
   - Weight: `font-bold` (weight 700)
   - Font: `font-mono`
   - Color / Accent: `stats.shots > 0 ? 'text-slate-200' : 'text-slate-500'`
   - Test ID: `data-testid="stat-shots"`
   - Rationale: Supporting offensive metric rendered in clear off-white monospace.

6. **Demos (Tactical Combat, 14px Rose Accent)**:
   - Sizing: `text-sm` (14px / 0.875rem)
   - Weight: `font-extrabold font-bold` (weight 800)
   - Font: `font-mono`
   - Color / Accent: `stats.demos > 0 ? 'text-rose-400 font-extrabold' : 'text-slate-500'`
   - Test ID: `data-testid="stat-demos"`
   - Rationale: Rose/red accent represents physical disruptions and aggressive tactical demolition play.

7. **Dimmed Inactive Metric Treatment**:
   - Every metric when `0` uses `text-slate-500` (except `Score`, which remains clean white).
   - This prevents a wall of identical numbers, allowing non-zero achievements to jump off the screen instantaneously.

### 2.3 Compatibility with `useColumnConfig` and Presets
`useColumnConfig` manages column visibility via `Record<StatColumnKey, boolean>` stored in `localStorage`:
- In `RosterTable.tsx` and `PlayerRow.tsx`, every conditional column is guarded by `{columnConfig.<key> && ...}`.
- Because JSX elements render conditionally in their declared document order:
  - **`full` Preset**: Renders all 10 columns: `Score -> Goals -> Assists -> Saves -> Shots -> Demos -> Rank -> MMR -> H2H -> Platform`.
  - **`competitive` Preset**: Renders: `Score -> Goals -> Rank -> MMR -> H2H`.
  - **`streamer` Preset**: Renders: `Score -> Goals -> Assists -> Saves`.
- Any user custom toggling preserves the exact relative sequence for whichever columns are currently active.
- Table `colSpan={12}` on the empty roster state (`No players currently loaded...`) covers all active configurations.

### 2.4 Vertical Budget & Row Height Calculations
- **Row Padding Tightening**: Changing `PlayerRow` cell padding from `py-2.5` (10px top/bottom) to `py-2` (8px top/bottom) compensates for larger typography (18px line-height = 28px).
- **Row Height**:
  - `8px padding-top` + `28px text-lg line-height` + `8px padding-bottom` = **44px row height**.
- **Table Dimensions (Standard 3v3 Match)**:
  - Team Header: `px-4 py-2.5` = **38px**
  - Table `<thead>`: `py-2.5` = **34px**
  - 3 Player Rows: 3 × 44px = **132px**
  - Total `RosterTable` height = **204px** (well under the 250px vertical budget).
- **Horizontal Width**:
  - With all 10 columns enabled, total table width is ~765px.
  - On a 1080p display (1920px width), `lg:grid-cols-2` allocates ~932px per table, guaranteeing zero horizontal scrollbar and ample breathing room.

---

## 3. Caveats

1. **Responsive Viewport Collapse (< 1024px)**:
   - On viewports with width < 1024px, `LiveGameView.tsx` collapses `lg:grid-cols-2` into a single stacked column (`grid-cols-1`). On mobile screens (e.g. 375px width), stacked tables will exceed 667px vertical height. The requirement explicitly scopes zero-scroll guarantees to standard desktop/laptop viewports (1080p, 1440p, 4K, and 768p).
2. **Chaos Mode (4v4) and Spectator Lobbies**:
   - In 4v4 matches (4 players per team), table height is ~248px (4 × 44px + 72px headers), which still fits cleanly within the 920px innerHeight budget.
   - If a private match contains 5+ spectators, spectators are separated into `match.spectators` and should not inflate the active team rosters.
3. **`MatchDetailModal.tsx` Alignment**:
   - Direct inspection of `web/src/components/session/MatchDetailModal.tsx` revealed that the session drill-down modal already followed `Player -> Score -> Goals -> Assists -> Saves -> Shots -> Demos -> MMR -> H2H`. Revamping `PlayerRow.tsx` and `RosterTable.tsx` creates complete architectural and aesthetic consistency across live and historical views.

---

## 4. Conclusion & Proposed Code Implementation

### 4.1 Proposed Implementation: `web/src/components/live/PlayerRow.tsx`
```tsx
import React from 'react';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { RankBadge } from '../common/RankBadge';
import { H2HBadge } from '../common/H2HBadge';
import { Bot, Laptop, Gamepad2 } from 'lucide-react';

interface PlayerRowProps {
  player: LobbyPlayer;
  isLocal: boolean;
  isTeammate: boolean;
  columnConfig: ColumnConfig;
  activePlaylistId: number;
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

  return (
    <tr
      className={`transition-colors hover:bg-slate-800/40 ${
        isLocal ? 'bg-cyan-500/10 font-medium' : ''
      }`}
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
      </td>

      {/* Prioritized Box Score Performance Stats */}
      {/* 1. Score: 18px font-black white */}
      {columnConfig.score && (
        <td
          data-testid="stat-score"
          className="py-2 px-3 text-right font-mono text-lg font-black text-white"
        >
          {stats.score}
        </td>
      )}

      {/* 2. Goals: 18px font-black amber glow */}
      {columnConfig.goals && (
        <td
          data-testid="stat-goals"
          className={`py-2 px-3 text-right font-mono text-lg font-black ${
            stats.goals > 0
              ? 'text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)]'
              : 'text-slate-500'
          }`}
        >
          {stats.goals}
        </td>
      )}

      {/* 3. Assists: 16px font-extrabold cyan */}
      {columnConfig.assists && (
        <td
          data-testid="stat-assists"
          className={`py-2 px-3 text-right font-mono text-base font-extrabold ${
            stats.assists > 0 ? 'text-cyan-300' : 'text-slate-500'
          }`}
        >
          {stats.assists}
        </td>
      )}

      {/* 4. Saves: 16px font-extrabold emerald */}
      {columnConfig.saves && (
        <td
          data-testid="stat-saves"
          className={`py-2 px-3 text-right font-mono text-base font-extrabold ${
            stats.saves > 0 ? 'text-emerald-400' : 'text-slate-500'
          }`}
        >
          {stats.saves}
        </td>
      )}

      {/* 5. Shots: 14px font-bold slate */}
      {columnConfig.shots && (
        <td
          data-testid="stat-shots"
          className={`py-2 px-3 text-right font-mono text-sm font-bold ${
            stats.shots > 0 ? 'text-slate-200' : 'text-slate-500'
          }`}
        >
          {stats.shots}
        </td>
      )}

      {/* 6. Demos: 14px font-bold font-extrabold rose */}
      {columnConfig.demos && (
        <td
          data-testid="stat-demos"
          className={`py-2 px-3 text-right font-mono text-sm font-bold ${
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
          <H2HBadge
            matchup={player.matchup_record}
            isTeammate={isTeammate}
          />
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

### 4.2 Proposed Implementation: `web/src/components/live/RosterTable.tsx`
```tsx
import React from 'react';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { PlayerRow } from './PlayerRow';
import { Shield, Flame } from 'lucide-react';

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
  const themeBorder = isBlue ? 'border-[#00a2ff]/30' : 'border-[#ff7b00]/30';
  const themeText = isBlue ? 'text-[#00a2ff]' : 'text-[#ff7b00]';
  const TeamIcon = isBlue ? Shield : Flame;

  // Sort players: Score DESC
  const sortedPlayers = [...players].sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0));

  // Calculate team total goals and score
  const teamGoals = players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  const teamScore = players.reduce((sum, p) => sum + (p.stats?.score || 0), 0);

  return (
    <div
      className={`rounded-2xl border ${themeBorder} bg-slate-900/70 overflow-hidden shadow-xl flex flex-col`}
      style={{
        contentVisibility: 'auto',
        containIntrinsicSize: 'auto 600px auto 350px',
      }}
    >
      {/* Team Header Banner */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-slate-800/40 border-b border-slate-800">
        <div className="flex items-center gap-2.5">
          <TeamIcon className={`w-5 h-5 ${themeText}`} />
          <h3 className={`font-bold text-sm uppercase tracking-wider ${themeText}`}>
            {teamName}
          </h3>
          <span className="text-xs text-slate-400 font-medium">({players.length})</span>
        </div>
        <div className="flex items-center gap-4 text-xs font-semibold text-slate-300">
          <span>Goals: <strong className="text-slate-100">{teamGoals}</strong></span>
          <span>Score: <strong className="text-slate-100">{teamScore}</strong></span>
        </div>
      </div>

      {/* Roster Table Content */}
      <div className="overflow-x-auto">
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 font-bold uppercase tracking-wider text-[11px] bg-slate-950/30">
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

---

## 5. Verification Method

To verify these findings and confirm the proposed changes when applied by the worker agent:

1. **Verify Test Pass Rate (Zero Regressions)**:
   ```bash
   cd d:\code\rl-api-utils\web
   npm test
   ```
   *Expected Result*: All 112 existing tests across 9 test files pass cleanly with Exit Code 0.

2. **Verify TypeScript & Production Build**:
   ```bash
   cd d:\code\rl-api-utils\web
   npm run build
   ```
   *Expected Result*: `tsc -b && vite build` succeeds with Exit Code 0 and outputs clean production bundle to `../internal/web/dist`.

3. **Verify DOM Column Order and Typography Assertions**:
   In Vitest with Happy DOM (or `renderToString`):
   - Assert `data-testid="stat-score"` has `classList.contains('text-lg')` and `classList.contains('font-black')`.
   - Assert `data-testid="stat-goals"` for non-zero goals has `classList.contains('text-amber-400')`.
   - Assert `data-testid="stat-assists"` has `classList.contains('text-cyan-300')`.
   - Assert `data-testid="stat-saves"` has `classList.contains('text-emerald-400')`.
   - Assert `<th>` order in `<thead>` matches: `Player`, `Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`, `Rank`, `MMR`, `H2H Record`.

4. **Invalidation Conditions**:
   - If `npm test` fails on any existing H2H badge (`<span>9W-1L</span>`) or `YOU` badge assertion.
   - If player performance stats appear to the right of `Rank`, `MMR`, or `Platform`.
   - If stat numbers remain `text-xs` (12px) without the 3-tier visual hierarchy.
