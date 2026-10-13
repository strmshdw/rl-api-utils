# BRIEFING — 2026-10-06T11:55:00Z

## Mission
Investigate Milestone 2: PlayerRow.tsx design for sleek esports matchup UX overhaul (R2), prominent stat badge pills, true in-game MMR, and mathematical verification of zero vertical scrolling on 1080p.

## 🔒 My Identity
- Archetype: explorer
- Roles: PsyNet Client Explorer, Synthesis
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M2 - Auth & PsyNet Integration
- Current Role (2026-10-06): Explorer for Milestone 2: Sleek Esports Matchup UX Overhaul (PlayerRow.tsx)
- Current Parent: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Current Milestone: Milestone 2: Sleek Esports Matchup UX & Disconnected Treatment (R2 & R3)

## 🔒 Key Constraints
- Read-only investigation — do NOT implement in production directories (write reports, designs, and proposed code in .agents/teamwork/m2_explorer_2/)
- Conforms to PROJECT.md architecture and MatchHistoryProvider interface
- Clean session lifecycle, reconnects, context cancellation, empty ReplayURL handling
- Unit test design using testutil.MockPsyNetServer
- Adhere to semantic table row layout (<tr>, <td>) preserving 1:1 cell-to-header alignment
- Guarantee zero vertical scrolling on standard 1080p viewports (stack height <= 416px, leaving > 500px headroom)
- Ensure true in-game MMR decimal representation ({mmr ? mmr.toFixed(1) : '--'})

## Current Parent
- Conversation ID: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Updated: 2026-10-06T11:49:35Z

## Investigation State
- **Explored paths**:
  - `web/src/components/live/PlayerRow.tsx`
  - `web/src/components/live/RosterTable.tsx`
  - `web/src/components/live/LiveGameView.tsx`
  - `web/src/components/live/ScoreboardBanner.tsx`
  - `web/src/components/live/LiveGameView.layout.test.tsx`
  - `web/src/components/live/LiveGameView.adversarial.test.tsx`
  - `web/src/components/session/MatchDetailModal.tsx`
  - `web/src/types/api.ts` & `web/src/types/columns.ts`
- **Key findings**:
  - Switching `PlayerRow` and `RosterTable` from table elements (`<table>`, `<tr>`, `<td>`) to a grid of card `<div>`s caused 15 Vitest test failures, broke column customizer toggling, and caused vertical layout bloat exceeding the zero-scroll budget.
  - Streamlined broadcast card-row table design (`<tr>` with styled badge pill `<td>` and inner spans) reconciles high-contrast esports visuals with strict semantic table contracts and 100% test pass rate.
  - Primary stat badges:
    - Score (`stat-score`): Bold white mono pill (`text-lg font-black font-mono text-white bg-slate-800/80 border border-slate-700/60`).
    - Goals (`stat-goals`): Glowing amber pill with glow dropshadow when > 0 (`text-lg font-black font-mono text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)] bg-amber-500/20 border-amber-500/40`), muted slate when 0 (`text-slate-500`).
    - Assists (`stat-assists`): Cyan pill when > 0 (`text-base font-extrabold font-mono text-cyan-300 bg-cyan-500/20 border-cyan-500/40`), muted slate when 0 (`text-slate-500`).
    - Saves (`stat-saves`): Emerald pill when > 0 (`text-base font-extrabold font-mono text-emerald-400 bg-emerald-500/20 border-emerald-500/40`), muted slate when 0 (`text-slate-500`).
    - Shots & Demos (`stat-shots`, `stat-demos`): Clean mono numbers.
    - True MMR (`stat-mmr`): `{player.current_rank?.mmr ? player.current_rank.mmr.toFixed(1) : '--'}` (preserves decimal representation expected by tests).
    - Disconnected Treatment (`badge-disconnected`): Row `opacity-65 saturate-60 hover:opacity-85`, badge with `WifiOff` icon.
  - Mathematical zero-scroll proof: Live match stack height on standard 1080p is at most 416px in 4v4 (and 378px in 3v3), leaving 504px to 542px headroom out of 920px usable viewport height.
- **Unexplored areas**:
  - `m2_explorer_1`: RosterTable glowing headers and team aggregation badges (`team-goals`, `team-score`).
  - `m2_explorer_3`: Vitest layout test additions and MatchDetailModal disconnected parity.

## Key Decisions Made
- Revert card `<div>` experiment in `PlayerRow.tsx` and restore semantic `<tr>` broadcast card-rows with styled badge pills.
- Apply font size, weight, text color, and text-right directly on `<td>` elements to satisfy Vitest DOM layout queries (`scoreCell.className`, `stat-goals.className`, `stat-assists.className`), while embedding rounded badge pill spans inside for visual broadcast flair.
- Format true in-game MMR strictly as `player.current_rank.mmr.toFixed(1)` to match `995.0` test assertions.

## Artifact Index
- `DISPATCH.md` — Incoming dispatch messages and scope
- `BRIEFING.md` — Persistent agent memory
- `progress.md` — Liveness heartbeat & milestone progress
- `proposed_PlayerRow.tsx` — Full drop-in replacement file for `web/src/components/live/PlayerRow.tsx`
- `handoff.md` — Complete 5-component handoff report
