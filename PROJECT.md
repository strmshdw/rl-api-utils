# Project: Rocket League Replay Synchronizer & Session Dashboard Daemon (rl-api-utils)

## Architecture

The project is an automated daemon written in Go with an embedded React + TypeScript web frontend that interfaces with Rocket League's PsyNet and local Stats APIs:
1. **Core Synchronization & Ingestion**:
   - `internal/syncer`: Polls PsyNet match history, downloads `.replay` files, and uploads to Ballchasing.com.
   - `internal/statsapi`: Ingests real-time match events (`UpdateState`, `MatchEnded`) from `MatchStatsExporter_TA` over WebSocket/TCP.
2. **State & Player Tracking**:
   - `internal/playertrack`: Resolves local player, categorizes lobby participants (teammates, opponents, spectators), retains participant state and accumulated stats across mid-game disconnects, compiles win/loss outcomes on match completion, and persists records into `internal/storage`.
   - `internal/session`: Manages active play session telemetry, aggregates playlist wins/losses and MMR deltas, maintains session match history snapshots, and pushes live Server-Sent Events (`/api/events`).
   - `internal/storage`: Dual-engine persistence (pure Go `modernc.org/sqlite` and structured `jsonstore`) for match sync, player profiles, and head-to-head matchup records.
3. **Web Dashboard & Delivery**:
   - `web/`: Modern single-page React 19 + TypeScript + Tailwind CSS application displaying real-time live game scoreboard, rosters, enlarged performance stats, session match history, and searchable player directory.
   - `internal/web`: Static asset embedding via Go `embed.FS` (`internal/web/dist`).
   - `internal/daemon`: HTTP REST and SSE server (0.0.0.0:49125) delivering dashboard and API endpoints.

---

## Feature Inventory

| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Pure Go SQLite Store | ACID persistence using `modernc.org/sqlite` (zero CGO) with `matches` and `auth_state` tables | Prior | survey_explorer_arch_1 |
| ... | Prior Features 2–22 | Sync, auth, uploader, session engine, player directory, and embedding | Prior | prior_milestones |
| 23 | Persistent Player State on Disconnect | Retain player info and accumulated stats in active match state when a player leaves or disconnects mid-game (`internal/playertrack`, `internal/session`) | M1 | survey_explorer_state_1 |
| 24 | Participant Outcome & Win/Loss Logging | Record win/loss outcomes in persistent storage (`player_matchups`) and session match history for all participants, including disconnected players | M2 | survey_explorer_logging_1 |
| 25 | Live Game UI Revamp & Zero-Scroll Layout | Prioritize and enlarge player stats (Score, Goals, Assists, Saves, Shots, Demos), eliminate superfluous elements (footer, debug info, redundant counts), guarantee zero vertical scrolling on 1080p standard viewports (`web/`) | M3 | survey_explorer_ui_1 |
| 26 | Full Integration & Regression Hardening | 100% test pass rate across all Go packages (710+ tests) and Vitest suite, zero regressions, standalone `rl-sync.exe` build | M4 | orchestrator_6 |

---

## Milestones

| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Persistent Player State on Disconnect (R2) | `internal/playertrack`, `internal/session`: Implement participant retention in `OnUpdateState`, `IsDisconnected` flag, local player retention fallback, and programmatic mid-game disconnect unit tests asserting active stats remain. | none | DONE |
| M2 | Match Outcome Logging for Disconnected Players (R3) | `internal/playertrack`, `internal/session`: Include disconnected participants in `OnMatchEnded` outcomes vector, update `storage.RecordMatchResults` with accurate win/loss records, map `Won` into `SessionMatchPlayer`, sum all goals, and provide automated match history tests. | M1 | DONE |
| M3 | Live Game UI Revamp & Viewport Optimization (R1) | `web/`: Prioritize/enlarge stats in `PlayerRow.tsx` and `RosterTable.tsx`, remove superfluous elements in `Header.tsx`, `App.tsx`, and `ScoreboardBanner.tsx`, reduce vertical spacing, ensure zero vertical scrolling on 1080p, add automated DOM layout/structure tests in `LiveGameView.layout.test.tsx`, build embedded dist. | M1 | DONE |
| M4 | Final Integration, E2E Verification & Adversarial Hardening (R4) | Full system integration: run all 14 Go packages, Vitest test suite, end-to-end multi-cycle disconnect tests, adversarial edge cases (reconnection, bot backfills, casual substitutions), and compile standalone `rl-sync.exe`. | M1, M2, M3 | DONE |

---

## Interface Contracts

### `internal/playertrack` ↔ `internal/session` & REST API
```go
type LobbyPlayer struct {
    PlayerID       string        `json:"player_id"`
    Platform       string        `json:"platform"`
    Name           string        `json:"name"`
    TeamNum        int           `json:"team_num"`
    IsBot          bool          `json:"is_bot"`
    IsLocal        bool          `json:"is_local"`
    IsDisconnected bool          `json:"is_disconnected,omitempty"`
    Stats          PlayerStats   `json:"stats"`
    Ranks          *PlayerRanks  `json:"ranks,omitempty"`
    MatchupRecord  *MatchupStats `json:"matchup_record,omitempty"`
}

type SessionMatchPlayer struct {
    PlayerID       string       `json:"player_id"`
    Platform       string       `json:"platform"`
    Name           string       `json:"name"`
    TeamNum        int          `json:"team_num"`
    IsBot          bool         `json:"is_bot"`
    IsLocal        bool         `json:"is_local"`
    IsDisconnected bool         `json:"is_disconnected,omitempty"`
    Won            *bool        `json:"won,omitempty"`
    Stats          PlayerStats  `json:"stats"`
    Ranks          *PlayerRanks `json:"ranks,omitempty"`
}
```

### TypeScript API Contracts (`web/src/types/api.ts`)
```typescript
export interface LobbyPlayer {
  player_id: string;
  platform: string;
  name: string;
  team_num: number;
  is_bot: boolean;
  is_local: boolean;
  is_disconnected?: boolean;
  stats: PlayerStats;
  ranks?: PlayerRanks;
  matchup_record?: MatchupRecord;
}

export interface SessionMatchPlayer {
  player_id: string;
  platform: string;
  name: string;
  team_num: number;
  is_bot: boolean;
  is_local: boolean;
  is_disconnected?: boolean;
  won?: boolean;
  stats: PlayerStats;
  ranks?: PlayerRanks;
}
```

---

## Code Layout

```
rl-api-utils/
├── cmd/
│   └── rl-sync/
│       └── main.go
├── internal/
│   ├── playertrack/
│   │   ├── tracker.go
│   │   ├── tracker_test.go
│   │   └── models.go
│   ├── session/
│   │   ├── session.go
│   │   ├── session_test.go
│   │   └── models.go
│   ├── storage/
│   │   ├── sqlite.go
│   │   ├── jsonstore.go
│   │   └── store.go
│   ├── statsapi/
│   │   └── listener.go
│   ├── web/
│   │   ├── dist/
│   │   └── web.go
│   └── daemon/
│       └── daemon.go
├── web/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── types/api.ts
│   │   ├── components/
│   │   │   ├── layout/
│   │   │   │   └── Header.tsx
│   │   │   └── live/
│   │   │       ├── LiveGameView.tsx
│   │   │       ├── LiveGameView.layout.test.tsx
│   │   │       ├── ScoreboardBanner.tsx
│   │   │       ├── RosterTable.tsx
│   │   │       └── PlayerRow.tsx
│   └── package.json
└── test/
    └── e2e/
        └── playertrack_e2e_test.go
```
