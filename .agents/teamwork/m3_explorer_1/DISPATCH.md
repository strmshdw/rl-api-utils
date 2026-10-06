# DISPATCH: m3_explorer_1

## Objective
Investigate the exact implementation specifications for player stat prioritization, column reordering, and stat typography enlargement in `web/src/components/live/PlayerRow.tsx` and `RosterTable.tsx` for Milestone M3 (Requirement R1).

## Scope Boundaries
- Read-only technical investigation. Do NOT edit code or test files.
- Deliver `handoff.md` to `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\handoff.md`.

## Context & Inputs
- Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
- Project Architecture & Milestones: `d:\code\rl-api-utils\PROJECT.md`
- Survey report on UI revamp: `d:\code\rl-api-utils\.agents\teamwork\survey_explorer_ui_1\handoff.md`
- Codebase: `web/src/components/live/PlayerRow.tsx`, `web/src/components/live/RosterTable.tsx`, `web/src/types/columns.ts`, `web/src/hooks/useColumnConfig.ts`

## Specific Tasks
1. Analyze column ordering in `RosterTable.tsx` (`<thead>`) and `PlayerRow.tsx` (`<tbody>`):
   - Sequence: Player -> Score -> Goals -> Assists -> Saves -> Shots -> Demos -> Rank -> MMR -> H2H.
   - Compatibility with `useColumnConfig` column visibility toggling.
2. Detail typography, font sizing, and visual accents for stat metrics:
   - Score: 18px (`text-lg font-black font-mono text-white`)
   - Goals: 18px (`text-lg font-black font-mono text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)]`)
   - Assists: 16px (`text-base font-extrabold font-mono text-cyan-300`)
   - Saves: 16px (`text-base font-extrabold font-mono text-emerald-400`)
   - Shots: 14px (`text-sm font-bold font-mono text-slate-200`)
   - Demos: 14px (`text-sm font-bold font-mono text-rose-400 font-extrabold`)
   - Include semantic `data-testid` attributes (`stat-score`, `stat-goals`, etc.).
3. Verify backward compatibility with existing tests in `RosterTable.test.tsx` and `adversarial.challenge.test.tsx` (`<span>9W-1L</span>`, `YOU`, etc.).
4. Provide concrete code diffs and recommendations.


## 2026-10-06T09:32:17Z
You are m3_explorer_1, an exploration agent for Milestone M3 (Requirement R1: Live Game UI Revamp).
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1

You MUST read:
1. Authoritative User Request: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T08:30:09Z)
2. Project specification: d:\code\rl-api-utils\PROJECT.md
3. Survey report: d:\code\rl-api-utils\.agents\teamwork\survey_explorer_ui_1\handoff.md
4. Your dispatch: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\DISPATCH.md

Investigate player stat prioritization, column reordering, and stat typography enlargement in web/src/components/live/PlayerRow.tsx and RosterTable.tsx.
Deliver your comprehensive handoff report at: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\handoff.md and notify orchestrator_6.
