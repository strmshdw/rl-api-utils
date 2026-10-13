# BRIEFING — 2026-10-06T11:55:00Z

## Mission
Investigate and design Milestone 2: Disconnected Player UX Treatment (R3) in `web/src/components/live/PlayerRow.tsx`, Concluded Match Parity in `web/src/components/session/MatchDetailModal.tsx`, and Vitest test suite restoration & additions in `web/src/components/live/LiveGameView.layout.test.tsx`.

## 🔒 My Identity
- Archetype: explorer
- Roles: explorer, synthesizer
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M2 - Auth & PsyNet Integration
- Archetype: explorer (re-assigned)
- Roles: explorer, synthesizer
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3
- Parent conversation ID: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Current Milestone: M2 - Disconnected Player UX Treatment & Vitest Tests

## 🔒 Key Constraints
- Read-only investigation — do NOT implement directly into `internal/psynet` (write proposed code and analysis in working directory)
- Must implement `ReplayDownloader` interface (`DownloadReplay(ctx, matchGUID, replayURL, destDir) (localPath, error)`)
- HTTP GET streaming to unique temporary file (`.tmp`) in `destDir`
- Minimum file size validation (>1KB), non-empty validation
- Flush and sync to disk before validation and rename
- Atomic rename (`os.Rename` with Windows retry loop)
- Cleanup on context cancellation or error
- Custom HTTP client configuration (timeout, transport, user-agent)
- Unit test design (`downloader_test.go`) utilizing `testutil.MockCDNServer`
- Output report and proposed code to `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md` and notify parent via `send_message`
- Read-only investigation — do NOT directly modify source code in `web/` (document blueprints, snippets, and verification methods in working directory)
- Must detail disconnected styling: `opacity-65 saturate-60 hover:opacity-85` in `PlayerRow.tsx`
- Must detail clean "DISCONNECTED" badge with `WifiOff` icon (`data-testid="badge-disconnected"`) in player identity cell
- Must preserve all stats, team placement, and outcome logging in the view
- Must detail matching disconnected badge in `web/src/components/session/MatchDetailModal.tsx`
- Must analyze `web/src/components/live/LiveGameView.layout.test.tsx` and formulate updates to restore 100% Vitest pass rate and add dedicated assertions for R2 & R3
- Write handoff report to `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md` and notify parent via `send_message`

## Current Parent
- Conversation ID: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- Updated: 2026-10-06T11:49:35Z

## Investigation State
- **Explored paths**:
  - `ORIGINAL_REQUEST.md`, `orchestrator_8/PROJECT.md`, `survey_explorer_ux_tests/handoff.md`, `DISPATCH.md`
  - `web/src/components/live/PlayerRow.tsx`, `RosterTable.tsx`, `LiveGameView.tsx`, `ScoreboardBanner.tsx`
  - `web/src/components/session/MatchDetailModal.tsx`, `SessionHistoryView.tsx`
  - `web/src/components/live/LiveGameView.layout.test.tsx`, `LiveGameView.adversarial.test.tsx`, `RosterTable.test.tsx`
- **Key findings**:
  - Root cause of 15 Vitest failures: an uncommitted experiment converted `RosterTable.tsx` and `PlayerRow.tsx` into CSS flex/grid card containers, stripping `table`/`thead`/`tbody`/`tr`/`td` tags and `data-testid="th-*"` and `data-testid="stat-*"` attributes.
  - Disconnected visual styling: `opacity-65 saturate-60 hover:opacity-85` provides immediate dimmed/desaturated feedback while maintaining WCAG AA contrast against slate dark background.
  - Disconnected badge: `<span data-testid="badge-disconnected" className="..."><WifiOff className="w-2.5 h-2.5 text-rose-400" />DISCONNECTED</span>` in player identity cell.
  - MatchDetailModal parity: Disconnected players in concluded match history retain `is_disconnected: true`, and rendering the same badge and `opacity-65 saturate-60 hover:opacity-85` provides 100% visual and semantic parity.
  - Zero-scroll compliance: Retaining compact broadcast table rows (~38-42px) keeps the live game stack height <= 416px on 1080p, leaving > 500px headroom.
  - Dedicated test additions: Formulated 4 dedicated test cases for `LiveGameView.layout.test.tsx` verifying `badge-disconnected`, `opacity-65 saturate-60`, `team-goals`, and `team-score`.
- **Unexplored areas**: None. Investigation complete across all scoped files.

## Key Decisions Made
- Confirmed that table row architecture (`<tr>`/`<td>`) must be preserved in `PlayerRow.tsx` to satisfy column customizer parity and pass all 15 Vitest tests.
- Designed exact styling and JSX for `data-testid="badge-disconnected"` with `WifiOff` icon.
- Extended `MatchDetailModal.tsx` to render the matching badge and dimmed row styling for concluded matches.
- Designed comprehensive test fixtures and assertions for `LiveGameView.layout.test.tsx`.

## Artifact Index
- `handoff.md` — Final 5-component handoff report
- `progress.md` — Liveness and progress tracker
- `BRIEFING.md` — Persistent working memory and situational awareness
- `DISPATCH.md` — Task record and message history
