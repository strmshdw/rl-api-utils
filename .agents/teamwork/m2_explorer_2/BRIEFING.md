# BRIEFING — 2026-09-25T03:42:00Z

## Mission
Investigate and design PsyNet client subsystem (internal/psynet/client.go) implementing MatchHistoryProvider for Milestone 2.

## 🔒 My Identity
- Archetype: explorer
- Roles: PsyNet Client Explorer, Synthesis
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M2 - Auth & PsyNet Integration

## 🔒 Key Constraints
- Read-only investigation — do NOT implement in production directories (write reports, designs, and proposed code in .agents/teamwork/m2_explorer_2/)
- Conforms to PROJECT.md architecture and MatchHistoryProvider interface
- Clean session lifecycle, reconnects, context cancellation, empty ReplayURL handling
- Unit test design using testutil.MockPsyNetServer

## Current Parent
- Conversation ID: 6e6c9567-59d2-415e-8d6e-41314a903548
- Updated: not yet

## Investigation State
- **Explored paths**:
  - `PROJECT.md` & `ORIGINAL_REQUEST.md` (domain models, clean architecture, M2 scope)
  - `github.com/dank/rlapi` codebase at `$env:TEMP\rlapi_spec\` (`psynet.go`, `psynetrpc.go`, `matches.go`, `auth.go`)
  - `internal/testutil/mock_psynet.go`, `mock_test.go`, `test/e2e/e2e_test.go`
  - `internal/storage/store.go` and `internal/config/config.go`
- **Key findings**:
  - `rlapi.postJSON` strictly expects HTTP response to be wrapped in a `"Result"` JSON object. `MockPsyNetServer.handleAuthPlayer` currently emits unwrapped JSON; proposed fix provides both `"Result": resp` and top-level fields for dual compatibility.
  - `Matches/GetMatchHistory v1` returns `[]MatchEntry` with `ReplayUrl`, `MatchGUID`, timestamps, map names, playlists.
  - Matches with delayed/unavailable replays arrive with `ReplayUrl: ""`. Client maps these without discarding, enabling the syncer to track and promote status on subsequent polling cycles once the URL is signed.
  - Designed `RPCClient` interface abstraction satisfied natively by `*rlapi.PsyNetRPC` for high-fidelity unit testing and transparent reconnects on in-flight socket drop.
  - Designed wire-level redirect transport allowing `MockPsyNetServer` to test full RFC 6455 WebSocket RPC with keepalives.
- **Unexplored areas**:
  - Downloader streaming and atomic rename (`m2_explorer_3` scope).
  - Epic and Steam OAuth token refresh state machine (`m2_explorer_1` scope).

## Key Decisions Made
- Define `DiscoveredMatch` and `MatchHistoryProvider` directly in `internal/psynet` to avoid circular dependency with `internal/syncer`.
- Abstract WebSocket RPC connection via `RPCClient` interface matching `*rlapi.PsyNetRPC` methods.
- Transparent single-retry reconnect on `rlapi.ErrConnectionClosed` or EOF to survive network hiccups between 5-minute polling cycles.
- Provide comprehensive 8-scenario unit test suite in `internal/psynet/client_test.go`.

## Artifact Index
- `DISPATCH.md` — Dispatch instructions & timestamped requests
- `BRIEFING.md` — Persistent agent memory
- `progress.md` — Liveness heartbeat & task progress
- `handoff.md` — Full 5-component handoff report with proposed code for `client.go`, `client_test.go`, and `mock_psynet.go` patch
