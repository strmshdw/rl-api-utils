# Reviewer Dispatch: m3_reviewer_2

## Task Assignment
**Role**: Layout & Build Integration Reviewer (`m3_reviewer_2`)  
**Milestone**: Milestone M3 (Requirement R1: Live Game UI Revamp & Zero-Scroll Viewport Optimization)  
**Parent Agent**: `orchestrator_6`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m3_reviewer_2`

## Mandatory Documents to Read First
1. Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
2. Project Specification: `d:\code\rl-api-utils\PROJECT.md`
3. Worker Handoff: `d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md`

## Focus Area
Review elimination of superfluous elements, zero-scroll layout architecture, and build asset embedding in:
- `web/src/components/live/ScoreboardBanner.tsx`
- `web/src/components/layout/Header.tsx`
- `web/src/App.tsx`
- `web/src/components/live/LiveGameView.tsx`
- `internal/web/dist` and static asset serving

Verify:
1. Superfluous element elimination:
   - Header debug text (`Port 49125` and `Uptime`) removed.
   - Header playlist carousel hidden when `inMatch` is true.
   - Scoreboard banner redundant player counts (`X Players`) removed.
   - App footer hidden during live match view.
2. Vertical space budget:
   - Margin and padding compressed (ScoreboardBanner `p-6 mb-8` -> `py-2.5 px-5 mb-3`).
   - Standard 3v3 live match vertical stack height <= 450px, guaranteeing zero vertical scrolling on 1080p (inner height ~920px).
3. Production build & Go embedding:
   - `npm run build` succeeds cleanly in `web/`.
   - `go test ./...` in project root passes without regressions.
   - `go build ./cmd/rl-sync` succeeds.

Deliver your verdict (`APPROVE` or `REQUEST_CHANGES`) in `d:\code\rl-api-utils\.agents\teamwork\m3_reviewer_2\handoff.md` and send a message back.


## 2026-10-06T10:02:40Z
You are m3_reviewer_2, an independent review agent for Milestone M3 (Requirement R1: Live Game UI Revamp & Viewport Optimization).
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m3_reviewer_2

You MUST read:
1. Authoritative User Request: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T08:30:09Z)
2. Project specification: d:\code\rl-api-utils\PROJECT.md
3. Your dispatch: d:\code\rl-api-utils\.agents\teamwork\m3_reviewer_2\DISPATCH.md
4. Worker handoff report: d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md

Review elimination of superfluous UI elements, zero-scroll layout architecture, margin/padding compression, and production build embedding:
- web/src/components/live/ScoreboardBanner.tsx
- web/src/components/layout/Header.tsx
- web/src/App.tsx
- web/src/components/live/LiveGameView.tsx
- internal/web/dist and static asset serving

Run builds and tests:
- cd d:\code\rl-api-utils\web && npm run build
- cd d:\code\rl-api-utils && go test ./...
Deliver your verdict (APPROVE or REQUEST_CHANGES) in d:\code\rl-api-utils\.agents\teamwork\m3_reviewer_2\handoff.md and notify orchestrator_6.
