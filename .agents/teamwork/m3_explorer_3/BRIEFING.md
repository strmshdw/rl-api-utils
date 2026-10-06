# BRIEFING — 2026-10-06T09:36:00Z

## Mission
Investigate and design the automated programmatic DOM layout and structure test suite in `web/src/components/live/LiveGameView.layout.test.tsx` using Vitest and Happy DOM for Milestone M3 (Requirement R1: Live Game UI Revamp).

## 🔒 My Identity
- Archetype: explorer
- Roles: investigation, synthesis
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3
- Original parent: f26416a7-29be-4b99-8406-d28bf983644d
- Milestone: M3 (Requirement R1: Live Game UI Revamp)

## 🔒 Key Constraints
- Read-only investigation — do NOT implement code changes to production or test files directly.
- Design comprehensive layout and DOM structure test suite in `web/src/components/live/LiveGameView.layout.test.tsx`.
- Must address stat prominence assertions, superfluous elements elimination assertions, standard viewport budget assertions, and regression guard.
- Output handoff report to `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md`.

## Current Parent
- Conversation ID: f26416a7-29be-4b99-8406-d28bf983644d
- Updated: 2026-10-06T09:32:17Z

## Investigation State
- **Explored paths**:
  - `ORIGINAL_REQUEST.md` (## 2026-10-06T08:30:09Z, Requirement R1)
  - `PROJECT.md` (Feature 25, Milestone M3)
  - `survey_explorer_ui_1/handoff.md` (detailed UI revamp recommendations)
  - `web/package.json` (Vitest v3.2.7, Happy DOM v20.14.5, React 19.0.0)
  - `web/vite.config.ts` (test environment node, per-file `// @vitest-environment happy-dom`)
  - `web/src/components/live/LiveGameView.tsx` (component structure, `space-y-6`)
  - `web/src/components/live/ScoreboardBanner.tsx` (`mb-8`, `p-6`, duplicate player counts)
  - `web/src/components/live/RosterTable.tsx` (column sequence putting Platform/Rank/MMR before stats)
  - `web/src/components/live/PlayerRow.tsx` (12px text-xs stats, lack of defensive/offensive accents)
  - `web/src/components/layout/Header.tsx` (Port 49125, Uptime: 0m, multi-playlist carousel)
  - `web/src/App.tsx` (static footer, py-4 padding)
  - `web/src/hooks/useLiveMatch.ts` and `web/src/hooks/useSession.ts` (fetch & SSE hooks)
  - Existing test suite (`npm test`: 9 test files, 112 tests passed in 1.61s)
- **Key findings**:
  - Existing tests in `web/` use `// @vitest-environment happy-dom` and `IS_REACT_ACT_ENVIRONMENT = true`.
  - Happy DOM supports full DOM manipulation via `createRoot` and `renderToString`.
  - Reordering table columns and enlarging stat fonts will NOT break any of the 112 existing tests.
  - Total vertical live stack can be compacted to ~376px (3v3) / ~414px (4v4), leaving >540px vertical headroom under 920px 1080p inner viewport height.
  - Complete 5-suite programmatic test file designed for `LiveGameView.layout.test.tsx`.
- **Unexplored areas**: None within the scope of M3 layout test design.

## Key Decisions Made
- Standardize DOM mounting and traversal via `createRoot` and `renderToString` with Happy DOM.
- Structure test suite into 5 dedicated sections:
  1. Stat Prominence & Typography Hierarchy
  2. Superfluous Elements Elimination
  3. Standard Viewport Layout & Budget Compliance (Zero Vertical Scrolling on 1080p)
  4. Column Customization & Preset Layout Stability
  5. Regression Guard & Existing Contract Compatibility
- Model vertical budget mathematically and enforce `<= 500px` rendered budget in automated tests.

## Artifact Index
- `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\DISPATCH.md` — Agent dispatch tasks and instructions
- `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\BRIEFING.md` — Situational awareness and state tracking
- `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\progress.md` — Liveness heartbeat
- `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md` — Comprehensive handoff report with complete test code
