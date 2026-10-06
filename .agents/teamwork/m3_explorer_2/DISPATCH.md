# DISPATCH: m3_explorer_2

## Objective
Investigate elimination of superfluous UI elements, vertical spacing compression, and the 1080p zero-scroll layout architecture across `ScoreboardBanner.tsx`, `Header.tsx`, `App.tsx`, and `LiveGameView.tsx` for Milestone M3 (Requirement R1).

## Scope Boundaries
- Read-only technical investigation. Do NOT edit code or test files.
- Deliver `handoff.md` to `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md`.

## Context & Inputs
- Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
- Project Architecture & Milestones: `d:\code\rl-api-utils\PROJECT.md`
- Survey report on UI revamp: `d:\code\rl-api-utils\.agents\teamwork\survey_explorer_ui_1\handoff.md`
- Codebase: `web/src/components/live/ScoreboardBanner.tsx`, `web/src/components/layout/Header.tsx`, `web/src/App.tsx`, `web/src/components/live/LiveGameView.tsx`

## Specific Tasks
1. Detail exact superfluous elements to remove or conditionally hide during live game view:
   - `App.tsx`: Conditionally hide static copyright footer when `activeTab === 'live' && inMatch` (or `activeTab === 'live'`).
   - `Header.tsx`: Remove daemon technical debug text ("Port 49125", "Uptime: 0m"), hide playlist carousel during active match.
   - `ScoreboardBanner.tsx`: Remove redundant player count badges ("X Players" on Blue and Orange sides), streamline icon boxes.
2. Detail vertical spacing reductions:
   - `ScoreboardBanner.tsx`: reduce `mb-8` to `mb-3`, reduce padding from `p-6` to `py-2.5 px-5`.
   - `LiveGameView.tsx`: replace `space-y-6` with `space-y-3`.
   - `App.tsx`: adjust main padding from `py-4` to `py-2`.
3. Calculate and document the exact vertical height budget:
   - Component-by-component heights on 1080p desktop (1920x1080, client viewport height ~920px).
   - Confirm total vertical height is <= 450px, guaranteeing zero vertical scrolling across 1080p, 1440p, 4K, and 768p viewports.
4. Provide concrete code diffs and recommendations.


## 2026-10-06T09:32:17Z
[Message] timestamp=2026-10-06T09:32:17Z sender=f26416a7-29be-4b99-8406-d28bf983644d priority=MESSAGE_PRIORITY_HIGH content=You are m3_explorer_2, an exploration agent for Milestone M3 (Requirement R1: Live Game UI Revamp).
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2

You MUST read:
1. Authoritative User Request: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T08:30:09Z)
2. Project specification: d:\code\rl-api-utils\PROJECT.md
3. Survey report: d:\code\rl-api-utils\.agents\teamwork\survey_explorer_ui_1\handoff.md
4. Your dispatch: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\DISPATCH.md

Investigate elimination of superfluous UI elements, vertical spacing compression, and zero-scroll layout architecture in ScoreboardBanner.tsx, Header.tsx, App.tsx, and LiveGameView.tsx.
Deliver your comprehensive handoff report at: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md and notify orchestrator_6.
