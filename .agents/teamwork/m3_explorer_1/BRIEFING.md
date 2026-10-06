# BRIEFING — 2026-10-06T09:35:00Z

## Mission
Investigate player stat prioritization, column reordering, and stat typography enlargement in web/src/components/live/PlayerRow.tsx and RosterTable.tsx for Milestone M3 (Requirement R1: Live Game UI Revamp).

## 🔒 My Identity
- Archetype: explorer
- Roles: Ballchasing API & Streaming Explorer (internal/ballchasing)
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M3 - Ballchasing Replay Uploader
- [M3 Iteration 2026-10-06] Roles: Live Game UI Revamp Explorer (PlayerRow & RosterTable stats prioritization & typography)
- [M3 Iteration 2026-10-06] Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1
- [M3 Iteration 2026-10-06] Original parent: f26416a7-29be-4b99-8406-d28bf983644d
- [M3 Iteration 2026-10-06] Milestone: M3 - Requirement R1: Live Game UI Revamp

## 🔒 Key Constraints
- Read-only investigation — do NOT implement in production source code directory directly (propose code in reports and proposed files)
- Strict raw token format in Authorization header: `Authorization: <token>` (no `Bearer ` prefix)
- Interface contract: `ReplayUploader` (`UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`)
- Multipart form field: `"file"` with filename `"<matchGUID>.replay"`
- Visibility parameter: `"public"`, `"unlisted"`, `"private"`
- Safe file reading and handle closure
- [M3 Iteration 2026-10-06] Read-only technical investigation — do NOT edit source code or test files directly
- [M3 Iteration 2026-10-06] Reorder columns to: Player -> Score -> Goals -> Assists -> Saves -> Shots -> Demos -> Rank -> MMR -> H2H (-> Platform)
- [M3 Iteration 2026-10-06] Enlarge typography: Score (18px white font-black), Goals (18px amber font-black drop-shadow), Assists (16px cyan font-extrabold), Saves (16px emerald font-extrabold), Shots (14px slate font-bold), Demos (14px rose font-extrabold), dimmed 0-values (text-slate-500)
- [M3 Iteration 2026-10-06] Add semantic data-testid attributes: stat-score, stat-goals, stat-assists, stat-saves, stat-shots, stat-demos
- [M3 Iteration 2026-10-06] Maintain full compatibility with useColumnConfig and existing Vitest tests (RosterTable.test.tsx, adversarial.challenge.test.tsx)

## Current Parent
- Conversation ID: f26416a7-29be-4b99-8406-d28bf983644d
- Updated: 2026-10-06T09:35:00Z

## Investigation State
- **Explored paths**:
  - `web/src/components/live/PlayerRow.tsx`, `RosterTable.tsx`, `LiveGameView.tsx`, `ScoreboardBanner.tsx`
  - `web/src/types/columns.ts`, `types/api.ts`, `hooks/useColumnConfig.ts`
  - `web/src/components/live/RosterTable.test.tsx`, `adversarial.challenge.test.tsx`, `types/columns.test.ts`
  - `web/src/components/session/MatchDetailModal.tsx`
  - `PROJECT.md`, `ORIGINAL_REQUEST.md`, `survey_explorer_ui_1/handoff.md`
- **Key findings**:
  - Existing `PlayerRow.tsx` and `RosterTable.tsx` place secondary metadata (`Platform`, `Rank`, `MMR`) *before* active gameplay stats (`Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`), displacing live match numbers to the far right.
  - Reordering to `Player -> Score -> Goals -> Assists -> Saves -> Shots -> Demos -> Rank -> MMR -> H2H -> Platform` brings action stats immediately adjacent to player identity.
  - Sizing hierarchy: 18px (`text-lg font-black`) for Score and Goals, 16px (`text-base font-extrabold`) for Assists and Saves, 14px (`text-sm font-bold`) for Shots and Demos creates immediate visual clarity.
  - Zero-state dimming (`text-slate-500`) keeps inactive stats from cluttering high-velocity play comprehension.
  - Column config toggling and presets (`full`, `competitive`, `streamer`) remain 100% compatible.
  - All 112 existing Vitest tests pass without regressions; H2H formatting (`<span>9W-1L</span>`, `(90%)`), `YOU` badges, and unranked (`>--<`) fallbacks are fully preserved.
- **Unexplored areas**:
  - None within M3 Explorer 1 scope.

## Key Decisions Made
- Reordered `<thead>` in `RosterTable.tsx` and `<tbody>` in `PlayerRow.tsx` to prioritized sequence.
- Added semantic `data-testid` attributes (`stat-score`, `stat-goals`, `stat-assists`, `stat-saves`, `stat-shots`, `stat-demos`, `stat-rank`, `stat-mmr`, `stat-h2h`, `stat-platform`, `player-identity`).
- Compacted row padding slightly (`py-2` vs `py-2.5`) to accommodate 18px line-height while keeping net row height at ~42px, guaranteeing zero-scroll layout.
- Kept `Platform` at the very end of the row if enabled in `columnConfig`, ensuring compatibility with `ColumnConfig` type and user preferences.

## Artifact Index
- `.agents/teamwork/m3_explorer_1/BRIEFING.md` — persistent situational awareness
- `.agents/teamwork/m3_explorer_1/progress.md` — liveness heartbeat
- `.agents/teamwork/m3_explorer_1/DISPATCH.md` — dispatched mission and instructions
- `.agents/teamwork/m3_explorer_1/handoff.md` — comprehensive 5-component handoff report
