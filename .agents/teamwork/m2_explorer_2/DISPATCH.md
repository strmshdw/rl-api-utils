# Dispatch: m2_explorer_2

**Milestone**: M2 - Auth & PsyNet Integration
**Role**: PsyNet Client Explorer (internal/psynet)
**Scope**:
Investigate and design `internal/psynet/client.go` implementing `syncer.MatchHistoryProvider`:
- Interface contract from PROJECT.md:
  ```go
  type DiscoveredMatch struct {
      MatchGUID            string
      RecordStartTimestamp int64
      MapName              string
      Playlist             int
      ReplayURL            string
  }
  type MatchHistoryProvider interface {
      GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
      Close() error
  }
  ```
- Implementation details:
  - Connect to PsyNet using auth tokens (from Epic or Steam auth)
  - Interfacing with `rlapi.NewPsyNet()` and `rlapi.PsyNetRPC`
  - Querying `Matches/GetMatchHistory v1`
  - Extracting match GUIDs, timestamps, map names, playlist IDs, and signed CDN `ReplayUrl`
  - Handling cases where `ReplayUrl` is temporarily empty (delayed replay URL arrival)
  - Handling connection loss, reconnects, context cancellation, and graceful Close()
  - Working with `internal/testutil/mock_psynet.go` for testing
- Unit test strategy (`client_test.go`) using `testutil.NewMockPsyNetServer`.

Output report: `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2\handoff.md`

## 2026-09-25T03:37:53Z
You are m2_explorer_2.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2\DISPATCH.md.
Also review d:\code\rl-api-utils\.agents\teamwork\survey_miner_rlapi_1\handoff.md and d:\code\rl-api-utils\internal\testutil\mock_psynet.go.

Explore the PsyNet client subsystem for Milestone 2 (internal/psynet/client.go):
- Interface: MatchHistoryProvider (GetRecentMatches(ctx) ([]DiscoveredMatch, error), Close() error)
- Connection bootstrap and WebSocket RPC with PsyNet
- Querying Matches/GetMatchHistory v1, parsing matches, GUIDs, timestamps, map names, playlist IDs, and signed ReplayUrl
- Handling empty ReplayUrl (delayed URL arrival)
- Session lifecycle, keepalive pings, reconnects, context cancellation
- Unit test design (client_test.go) utilizing testutil.MockPsyNetServer
Write your report and proposed code to d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2\handoff.md and notify parent via send_message.
