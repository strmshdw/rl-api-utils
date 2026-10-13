# Progress — M2 Sleek Esports Matchup UX & Disconnected Player Treatment (R2 & R3)

Last visited: 2026-10-06T12:15:00Z

## Status
- Current Step: Complete! Ready for Handoff.
- Completed:
  - 1. `web/src/components/live/RosterTable.tsx`: Restored semantic `<table>` wrapped in `.overflow-x-auto`. Implemented glowing team headers with ambient glow stripe (`h-1 bg-gradient-to-r`), glowing borders (`border-[#00a2ff]/40` / `border-[#ff7b00]/40`), ambient dropshadows. Implemented dedicated aggregated goals badge (`data-testid="team-goals"`) and aggregated score badge (`data-testid="team-score"`). Ensured all 11 semantic `data-testid="th-*"` table headers exist in order.
  - 2. `web/src/components/live/PlayerRow.tsx`: Implemented streamlined broadcast card-row as `<tr>`. Applied disconnected treatment (`opacity-65 saturate-60 hover:opacity-85`). Added clean `data-testid="badge-disconnected"` with `WifiOff` icon in `data-testid="player-identity"`. Rendered prominent stat badge styling for Score, Goals (amber glow), Assists (cyan), Saves (emerald), Shots, Demos, Rank, and MMR formatted as `toFixed(1)`.
  - 3. `web/src/components/session/MatchDetailModal.tsx`: Applied matching `opacity-65 saturate-60 hover:opacity-85` and `data-testid="badge-disconnected"` with `WifiOff` icon for concluded matches box score parity.
  - 4. `web/src/components/live/LiveGameView.layout.test.tsx`: Fixed all 15 Vitest failures. Added dedicated R2 tests (`team-goals`, `team-score` aggregations) and R3 tests (`badge-disconnected`, `opacity-65 saturate-60`, active player negation, `MatchDetailModal` parity).
  - 5. Full test & build verification:
    - `npm test -- --run` in `web/`: 100% pass (11/11 test files, 150/150 tests passing).
    - `npm run build` in `web/`: clean build, bundling into `internal/web/dist` with 0 errors.
    - `go test -count=1 ./...`: 100% pass across all 14 Go packages.
    - `go build ./cmd/rl-sync`: compiles `rl-sync.exe` cleanly with embedded frontend.
