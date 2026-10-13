# BRIEFING — 2026-10-06T11:58:00Z

## Mission
Investigate Milestone 2: RosterTable.tsx and LiveGameView.tsx. Detail the exact restoration of table semantics, glowing team headers with aggregated goals badge (data-testid="team-goals") and score badge (data-testid="team-score"), ambient glow lines, and th headers.

## 🔒 My Identity
- Archetype: explorer
- Roles: auth explorer, investigation, synthesis
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M2 - Auth & PsyNet Integration
- Current Archetype: explorer
- Current Roles: frontend explorer, layout investigation, UX synthesis
- Current Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1
- Parent (2026-10-06): 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Current Milestone: M2 - Sleek Esports Matchup UX Overhaul (R2 & R3)

## 🔒 Key Constraints
- Read-only investigation — do NOT implement code in src directories (d:\code\rl-api-utils\pkg, internal, etc.)
- Work only inside own folder `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1`
- Output handoff report to `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\handoff.md`
- Communicate all results and status via `send_message` to parent (6e6c9567-59d2-415e-8d6e-41314a903548)
- Read-only investigation for M2 — do NOT modify files in `web/src/` directly
- Deliver complete designs, proposed files, diff patch, and 5-component handoff report to `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\`
- Communicate completion to parent (18e4e66b-5835-4b6b-bd1e-a34092f47346) via `send_message`

## Current Parent
- Conversation ID: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Updated: 2026-10-06T11:58:00Z

## Investigation State
- **Explored paths**:
  - `ORIGINAL_REQUEST.md` (2026-10-06T11:02:53Z): R2 (Esports UX overhaul) & R3 (Disconnected player UX)
  - `PROJECT.md`: Features 30-35, zero vertical scroll budget proof (stack <= 416px vs 920px 1080p innerHeight)
  - `web/src/components/live/RosterTable.tsx` & `PlayerRow.tsx`: Pinpointed exact cause of 15 Vitest failures (card grid replaced semantic table)
  - `web/src/components/live/LiveGameView.tsx`: Analyzed player partitioning, team grouping layout (`grid lg:grid-cols-2`), compact spacing (`py-1 space-y-3`)
  - `web/src/components/live/LiveGameView.layout.test.tsx` & `LiveGameView.adversarial.test.tsx`: Verified all DOM assertions, th/td parity, and styling expectations
- **Key findings**:
  - Semantic `<table>` restoration with `.overflow-x-auto` wrapper resolves all 15 Vitest failures while guaranteeing horizontal alignment across all custom column configurations.
  - Broadcast header integrates dedicated aggregated badges: `data-testid="team-goals"` (amber badge) and `data-testid="team-score"` (blue/orange badge) with glowing ambient line (`bg-gradient-to-r`).
  - Player rows retain high stat prominence with `stat-score` (18px font-black white), `stat-goals` (amber glow when > 0, slate when 0), `stat-assists` (cyan), `stat-saves` (emerald), and `stat-demos` (rose).
  - Disconnected players receive `opacity-65 saturate-60 hover:opacity-85` row treatment and `data-testid="badge-disconnected"` with `WifiOff` icon.
- **Unexplored areas**: None within Milestone 2 live view scope.

## Key Decisions Made
- Revert ad-hoc card grid to streamlined esports broadcast card-rows utilizing semantic `<table>`, `<thead>`, `<tbody>`, `<tr>`, `<td>`.
- Include `data-testid="team-goals"` and `data-testid="team-score"` in team header banner with glowing borders and dropshadows.
- Provide full proposed files and machine-applicable patch `m2_roster_table.patch` in working directory for implementation.

## Artifact Index
- `DISPATCH.md` — incoming task dispatch
- `BRIEFING.md` — persistent working memory
- `progress.md` — liveness heartbeat
- `proposed_RosterTable.tsx` — production-ready proposed RosterTable component
- `proposed_PlayerRow.tsx` — production-ready proposed PlayerRow component
- `proposed_LiveGameView.tsx` — production-ready proposed LiveGameView component
- `proposed_MatchDetailModal.tsx` — production-ready proposed MatchDetailModal component
- `m2_roster_table.patch` — unified UTF-8 git diff patch file
- `handoff.md` — 5-component self-contained handoff report
