# BRIEFING — 2026-10-06T09:40:00Z

## Mission
Investigate elimination of superfluous UI elements, vertical spacing compression, and the 1080p zero-scroll layout architecture across ScoreboardBanner.tsx, Header.tsx, App.tsx, and LiveGameView.tsx for Milestone M3 (Requirement R1: Live Game UI Revamp).

## 🔒 My Identity
- Archetype: explorer
- Roles: read-only investigation, synthesize findings, produce structured reports
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M3 - Ballchasing Replay Uploader
- Updated parent: f26416a7-29be-4b99-8406-d28bf983644d
- Current Milestone: Milestone M3 (Requirement R1: Live Game UI Revamp)

## 🔒 Key Constraints
- Read-only investigation — do NOT implement directly in production package `internal/ballchasing`
- Provide precise proposed code, algorithms, error definitions, and verification steps in `handoff.md`
- Working folder: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2
- Never put source code, tests, or data directly into `.agents/teamwork/`
- Respect communication guidelines: send_message to parent upon completion
- Read-only investigation for Milestone M3 UI revamp: do NOT directly modify frontend source files (`web/src/*`)

## Current Parent
- Conversation ID: f26416a7-29be-4b99-8406-d28bf983644d
- Updated: 2026-10-06T09:32:17Z

## Investigation State
- **Explored paths**:
  - `ORIGINAL_REQUEST.md` (## 2026-10-06T08:30:09Z)
  - `PROJECT.md`
  - `survey_explorer_ui_1/handoff.md`
  - `m3_explorer_2/DISPATCH.md`
  - `web/src/App.tsx`
  - `web/src/components/layout/Header.tsx`
  - `web/src/components/live/ScoreboardBanner.tsx`
  - `web/src/components/live/LiveGameView.tsx`
  - `web/src/components/live/RosterTable.tsx`
  - `web/src/components/live/PlayerRow.tsx`
- **Key findings**:
  - Superfluous elements identified: static copyright footer in `App.tsx` (saves ~41px), daemon debug text `Port 49125`/`Uptime` in `Header.tsx` (saves ~20px), playlist carousel in `Header.tsx` during match (saves ~49px), redundant `Players` count labels in `ScoreboardBanner.tsx`.
  - Spacing reductions: `ScoreboardBanner.tsx` `mb-8` -> `mb-3` and `p-6` -> `py-2.5 px-5` (saves 48px); `LiveGameView.tsx` `space-y-6` -> `space-y-3` (saves 12px); `App.tsx` `py-4` -> `py-2` (saves 16px).
  - Total vertical stack height for 3v3 match: 410px (456px for 4v4 Chaos mode).
  - On standard 1080p desktop (`window.innerHeight` ~920px), headroom is 510px (>55% viewport margin).
  - On 768p laptop (`window.innerHeight` ~650px), headroom is 240px (>36% viewport margin). Zero vertical scrolling is mathematically guaranteed.
- **Unexplored areas**: None within the assigned M3 scope. Ready for implementation.

## Key Decisions Made
- Confirmed zero-scroll architecture budget: total stack height 410px <= 450px threshold.
- Formulated concrete before/after code diffs for all 4 target files in `handoff.md`.
- Specified automated layout test suite `LiveGameView.layout.test.tsx` verifying DOM structure and element absence.

## Artifact Index
- `DISPATCH.md` — Assigned mission instructions
- `BRIEFING.md` — Working memory
- `progress.md` — Liveness heartbeat
- `handoff.md` — Comprehensive 5-component handoff report
