# Worker Dispatch: Milestone M3 Implementation

## Task Assignment
**Role**: Implementation Worker (`m3_worker_1`)  
**Milestone**: Milestone M3 (Requirement R1: Live Game UI Revamp & Zero-Scroll Viewport Optimization)  
**Parent Agent**: `orchestrator_6`  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m3_worker_1`

## Mandatory Documents to Read First
1. Authoritative User Request: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-10-06T08:30:09Z`)
2. Project Specification: `d:\code\rl-api-utils\PROJECT.md`
3. Explorer Handoff Reports:
   - `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\handoff.md` (Stat Prioritization & Typography Architecture)
   - `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md` (Superfluous Element Elimination & Zero-Scroll Layout)
   - `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md` (DOM Layout Test Suite Design)

## MANDATORY INTEGRITY WARNING
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## File Ownership (Exclusive)
You have exclusive write ownership over:
- `web/src/components/live/PlayerRow.tsx`
- `web/src/components/live/RosterTable.tsx`
- `web/src/components/live/ScoreboardBanner.tsx`
- `web/src/components/live/LiveGameView.tsx`
- `web/src/components/layout/Header.tsx`
- `web/src/App.tsx`
- `web/src/components/live/LiveGameView.layout.test.tsx` (new file)

## Implementation Instructions

### 1. `web/src/components/live/PlayerRow.tsx`
- Prioritize and reorder columns: `Player` -> `Score` -> `Goals` -> `Assists` -> `Saves` -> `Shots` -> `Demos` -> `Rank` -> `MMR` -> `H2H` -> `Platform`.
- Enlarge fonts & add distinct visual accents:
  - `Score`: `text-lg font-black text-white font-mono` (`data-testid="stat-score"`)
  - `Goals`: `text-lg font-black font-mono` with amber glow when >0: `text-amber-400 drop-shadow-[0_0_8px_rgba(251,191,36,0.35)]`, else `text-slate-500` (`data-testid="stat-goals"`)
  - `Assists`: `text-base font-extrabold font-mono text-cyan-300` when >0, else `text-slate-500` (`data-testid="stat-assists"`)
  - `Saves`: `text-base font-extrabold font-mono text-emerald-400` when >0, else `text-slate-500` (`data-testid="stat-saves"`)
  - `Shots`: `text-sm font-bold font-mono text-slate-200` when >0, else `text-slate-500` (`data-testid="stat-shots"`)
  - `Demos`: `text-sm font-bold font-extrabold font-mono text-rose-400 font-extrabold` when >0, else `text-slate-500` (`data-testid="stat-demos"`)
  - `Rank`: `data-testid="stat-rank"`
  - `MMR`: `data-testid="stat-mmr"`
  - `H2H`: `data-testid="stat-h2h"`
  - `Platform`: `data-testid="stat-platform"`
- Tighten cell padding to `py-2 px-3` (player anchor `py-2 px-4`).
- Preserve all existing strings and badges (`YOU`, `BOT`, `9W-1L`, `>--<`, etc.) so all existing tests pass without regressions.

### 2. `web/src/components/live/RosterTable.tsx`
- Reorder `<thead>` column headers to match the prioritized sequence: `Player`, `Score`, `Goals`, `Assists`, `Saves`, `Shots`, `Demos`, `Rank`, `MMR`, `H2H Record`, `Platform`.
- Add test IDs to headers: `data-testid="th-player"`, `data-testid="th-score"`, `data-testid="th-goals"`, `data-testid="th-assists"`, `data-testid="th-saves"`, `data-testid="th-shots"`, `data-testid="th-demos"`, `data-testid="th-rank"`, `data-testid="th-mmr"`, `data-testid="th-h2h"`, `data-testid="th-platform"`.
- Set table header row to `py-2.5`.

### 3. `web/src/components/live/ScoreboardBanner.tsx`
- Reduce bottom margin from `mb-8` to `mb-3`.
- Reduce padding from `p-6` to `py-2.5 px-5`.
- Streamline team icon boxes to `w-9 h-9` (icons `w-5 h-5`).
- Remove redundant player count strings (`{allPlayers.filter(p => p.team_num === 0).length} Players` and Orange team counterpart).
- Add `data-testid="scoreboard-banner"`.

### 4. `web/src/components/layout/Header.tsx`
- Add `inMatch?: boolean` to `HeaderProps`.
- Remove daemon technical debug text (`Port 49125` and `Uptime: {session?.uptime || '0m'}`).
- Adjust padding to `py-2.5`.
- Conditionally render playlist MMR carousel only when `!inMatch` (`{playlists.length > 0 && !inMatch && (...)}`).
- Add `data-testid="app-header"`.

### 5. `web/src/App.tsx`
- Pass `inMatch={inMatch}` to `<Header ... />`.
- Adjust main container padding to `py-2` during live tab.
- Conditionally hide static footer during active live match (`!(activeTab === 'live' && inMatch)`).

### 6. `web/src/components/live/LiveGameView.tsx`
- Adjust vertical layout spacing: change `space-y-6` to `space-y-3`, container padding to `py-1`.

### 7. Automated DOM Layout & Structure Test Suite
- Implement `web/src/components/live/LiveGameView.layout.test.tsx` using Vitest and Happy DOM based on the complete specification in `m3_explorer_3/handoff.md`.
- Ensure tests verify:
  - Prominent stat typography (classes `text-lg`, `font-black`, `text-amber-400`, `drop-shadow-...`, `text-cyan-300`, `text-emerald-400`).
  - Column sequence ordering (`th-player -> th-score -> th-goals -> th-assists -> th-saves -> th-shots -> th-demos -> th-rank -> th-mmr -> th-h2h`).
  - Superfluous UI removal (no debug text in header, playlist carousel hidden in match, player counts removed from scoreboard banner, footer hidden in match).
  - Vertical height budget (< 920px total height).

## Verification Requirements
You must execute:
1. `npm test` in `web/` (all 112+ tests and new layout tests must pass).
2. `npm run build` in `web/` (`tsc -b && vite build` succeeds, generating embedded dist).
3. `go test ./...` in project root (no regressions across all Go packages).
4. `go build ./cmd/rl-sync` (binary compiles cleanly).

Write your handoff report to `d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md` and send a message when complete.


## 2026-10-06T09:42:17Z
[Message] timestamp=2026-10-06T09:42:17Z sender=f26416a7-29be-4b99-8406-d28bf983644d priority=MESSAGE_PRIORITY_HIGH
You are m3_worker_1, an implementation worker for Milestone M3 (Requirement R1: Live Game UI Revamp & Viewport Optimization).
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m3_worker_1

You MUST read before starting:
1. Authoritative User Request: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically ## 2026-10-06T08:30:09Z)
2. Your task assignment & file ownership: d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\DISPATCH.md
3. Explorer Handoff Reports:
   - d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\handoff.md
   - d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md
   - d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Your Tasks:
1. Revamp PlayerRow.tsx and RosterTable.tsx: prioritize stat columns (Player -> Score -> Goals -> Assists -> Saves -> Shots -> Demos -> Rank -> MMR -> H2H -> Platform), enlarge stat typography (Score/Goals 18px text-lg font-black, Assists/Saves 16px text-base font-extrabold, Shots/Demos 14px text-sm font-bold), add distinct accents (amber glow for goals, cyan for assists, emerald for saves, rose for demos), and add data-testid attributes.
2. Optimize viewport and eliminate superfluous elements in ScoreboardBanner.tsx (reduce mb-8 to mb-3, p-6 to py-2.5 px-5, remove redundant player counts, streamline team icons to w-9 h-9), Header.tsx (remove daemon debug text 'Port 49125' and 'Uptime', hide playlist carousel during match), App.tsx (pass inMatch to Header, hide static footer during active live match), and LiveGameView.tsx (reduce space-y-6 to space-y-3).
3. Implement the comprehensive automated programmatic DOM layout and structure test suite in web/src/components/live/LiveGameView.layout.test.tsx using Vitest and Happy DOM per m3_explorer_3/handoff.md.
4. Run tests and builds:
   - npm test in web/ (all existing 112 tests + new layout tests pass)
   - npm run build in web/ (production bundle builds cleanly)
   - go test ./... (all Go packages pass)
   - go build ./cmd/rl-sync (binary compiles cleanly)
5. Document all changes and test outputs in d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md and send a completion message back to orchestrator_6.
