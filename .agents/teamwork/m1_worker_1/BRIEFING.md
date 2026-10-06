# BRIEFING — 2026-10-06T09:09:00Z

## Mission
Implement Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect): differential participant retention in tracker.go, IsDisconnected in LobbyPlayer and SessionMatchPlayer, ConcludeMatch mapping and goal tallying, TypeScript type updates, and comprehensive programmatic unit test suites.

## 🔒 My Identity
- Archetype: worker
- Roles: implementer, qa, specialist
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m1_worker_1
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M1 - Storage & Configuration
- Subagent Conversation ID: 2382f655-9d12-497d-8842-5d57d3fa6050
- Current Parent Conversation ID: b82f99b4-2b9f-46d1-8c45-738eb9e9a7b1
- Expansion Milestone: M1 - Stats API & Storage Schema Expansion
- M1 R2 Milestone: Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect)
- M1 R2 Parent Conversation ID: f26416a7-29be-4b99-8406-d28bf983644d

## 🔒 Key Constraints
- Exclusive write ownership:
  - go.mod, go.sum
  - internal/storage/store.go
  - internal/storage/sqlite.go
  - internal/storage/sqlite_test.go
  - internal/storage/jsonstore.go
  - internal/storage/jsonstore_test.go
  - internal/config/config.go
  - internal/config/config_test.go
  - configs/config.example.yaml
  - configs/config.example.json
  - internal/statsapi/types.go
  - internal/statsapi/listener.go
  - internal/statsapi/listener_test.go
- Expansion constraints:
  - Exclusive ownership for this task:
    - internal/statsapi/types.go
    - internal/statsapi/listener.go
    - internal/statsapi/listener_test.go
    - internal/storage/store.go
    - internal/storage/sqlite.go
    - internal/storage/sqlite_test.go
    - internal/storage/jsonstore.go
    - internal/storage/jsonstore_test.go
- M1 R2 Exclusive write ownership:
  - internal/playertrack/tracker.go
  - internal/playertrack/tracker_test.go
  - internal/session/models.go
  - internal/session/session.go
  - internal/session/session_test.go
  - web/src/types/api.ts
- DO NOT touch other source files outside this scope.
- Integrity mandate: genuine implementations only, zero hardcoding or facades.
- 100% test pass on all repository packages.

## Current Parent
- Conversation ID: f26416a7-29be-4b99-8406-d28bf983644d
- Updated: 2026-10-06T09:09:00Z

## Task Summary
- **What to build**: Persistent Player State on Mid-Game Disconnect (M1 / Requirement R2)
- **Success criteria**:
  - Add `IsDisconnected bool json:"is_disconnected,omitempty"` to `LobbyPlayer`.
  - Differential participant retention in `Tracker.OnUpdateState` when `isSameMatch`.
  - Retain departed teammates, opponents, spectators with `IsDisconnected = true` and preserve last known box score stats.
  - Reconnections restore active status (`IsDisconnected = false`), update stats, without duplicate entries.
  - Preserve `LocalPlayer` and `LocalTeam` if local player disconnects early; ensure `OnMatchEnded` records outcomes.
  - Exclude departed AI bots from retention.
  - Clean reset on new match GUID transition.
  - Add `IsDisconnected bool json:"is_disconnected,omitempty"` and `Won *bool json:"won,omitempty"` to `SessionMatchPlayer`.
  - Update `SessionMatchPlayer.DeepClone()`.
  - In `ConcludeMatch`, map `lp.IsDisconnected` to `SessionMatchPlayer.IsDisconnected`, compute `Won`, and sum goals from all participants into team scores.
  - Update `web/src/types/api.ts` with `is_disconnected?: boolean;` on `LobbyPlayer` and `SessionMatchPlayer`, and `won?: boolean;`.
  - Add programmatic unit test suites in `tracker_test.go` and `session_test.go`.
  - 100% test pass across all packages and clean build.
- **Interface contracts**: PROJECT.md § Interface Contracts
- **Code layout**: PROJECT.md § Code Layout

## Key Decisions Made
- Differential retention in `OnUpdateState`: human participants absent from frame are retained with `IsDisconnected = true`; AI bots are excluded.
- Local player / local team fallback in `OnUpdateState` preserves `LocalTeam` when local player drops, allowing `OnMatchEnded` to record outcomes.
- Frame deduplication lookup (`seenThisFrame`) keyed by normalized `PrimaryId` prevents duplicate entries upon reconnection.
- DeepClone copies `Won *bool` safely to avoid data races across concurrent readers.

## Artifact Index
- internal/playertrack/tracker.go — differential retention algorithm & LobbyPlayer
- internal/playertrack/tracker_test.go — 5 new unit tests for mid-game disconnect lifecycle
- internal/session/models.go — SessionMatchPlayer with IsDisconnected and Won
- internal/session/session.go — ConcludeMatch mapping and team score calculation
- internal/session/session_test.go — 4 new unit tests for disconnect propagation and conclusion
- web/src/types/api.ts — TypeScript interfaces updated with is_disconnected

## Change Tracker
- **Files modified**:
  - `internal/playertrack/tracker.go`: Added `IsDisconnected` to `LobbyPlayer`; implemented differential retention in `OnUpdateState`.
  - `internal/playertrack/tracker_test.go`: Added `makePlayerWithStats` and 5 comprehensive unit tests covering the disconnect lifecycle across SQLite and JSONStore.
  - `internal/session/models.go`: Added `IsDisconnected` and `Won` to `SessionMatchPlayer`; updated `DeepClone`.
  - `internal/session/session.go`: Mapped `IsDisconnected` and computed `Won` in `ConcludeMatch`.
  - `internal/session/session_test.go`: Added 4 tests for active match disconnect observer propagation, SSE broadcasting, concluded match snapshots, and deep cloning.
  - `web/src/types/api.ts`: Added `is_disconnected` and `won` to TypeScript interfaces.
- **Build status**: PASS (`go build ./cmd/rl-sync`, `npm --prefix web run build`, `go test ./...`)
- **Pending issues**: None

## Quality Status
- **Build/test result**: PASS (100% across all 14 Go packages, 112 frontend Vitest tests)
- **Lint status**: clean (`go vet ./...` 0 warnings)
- **Tests added/modified**: 9 new tests added (5 in playertrack, 4 in session)
