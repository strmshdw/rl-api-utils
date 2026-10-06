# DISPATCH: m3_explorer_3

## Objective
Investigate and design the automated programmatic DOM layout/structure test suite in `web/src/components/live/LiveGameView.layout.test.tsx` for Milestone M3 (Requirement R1).

## Scope Boundaries
- Read-only technical investigation. Do NOT edit code or test files.
- Deliver `handoff.md` to `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md`.

## Context & Inputs
- Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
- Project Architecture & Milestones: `d:\code\rl-api-utils\PROJECT.md`
- Survey report on UI revamp: `d:\code\rl-api-utils\.agents\teamwork\survey_explorer_ui_1\handoff.md`
- Codebase: `web/src/components/live/`, `web/package.json`, Vitest + Happy DOM testing environment

## Specific Tasks
1. Review testing setup in `web/`:
   - Vitest v3, Happy DOM v20, React 19 testing patterns.
2. Design comprehensive automated test suite `LiveGameView.layout.test.tsx`:
   - **Stat Prominence Assertions**: Verify that Score, Goals, Assists, Saves, Shots, Demos render with enlarged typography classes (`text-lg`, `text-base`, `font-black`, `font-extrabold`) and appear before secondary columns.
   - **Superfluous Elements Elimination Assertions**: Verify absence of static footer, daemon debug info ("Port 49125", "Uptime:"), redundant player counts, and large vertical margins (`mb-8`, `space-y-6`).
   - **Standard Viewport Layout Assertions**: Verify standard 1080p viewport budget compliance (rendered DOM elements sum to <= 500px, leaving ample vertical headroom under standard browser client height ~920px, guaranteeing zero vertical scrolling).
   - **Regression Guard**: Verify all 112 existing Vitest tests continue to pass.
3. Provide complete, ready-to-run TypeScript / Vitest test code.

## 2026-10-06T09:32:17Z
You are m3_explorer_3, an exploration agent for Milestone M3 (Requirement R1: Live Game UI Revamp).
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3

You MUST read:
1. Authoritative User Request: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T08:30:09Z)
2. Project specification: d:\code\rl-api-utils\PROJECT.md
3. Survey report: d:\code\rl-api-utils\.agents\teamwork\survey_explorer_ui_1\handoff.md
4. Your dispatch: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\DISPATCH.md

Investigate and design the automated programmatic DOM layout and structure test suite in web/src/components/live/LiveGameView.layout.test.tsx using Vitest and Happy DOM.
Deliver your comprehensive handoff report at: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md and notify orchestrator_6.
