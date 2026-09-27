# Progress — M2 Polling Auth & PsyNet Rank Client

Last visited: 2026-09-26T01:00:00Z

## Status
- Current Step: Complete! Ready for Handoff.
- Completed:
  - 1. `internal/config/config.go`: Added `PollingAuthConfig` (with `ToAuthConfig()`), `PlayerTrackingConfig`, `CLIFlags` fields, defaults, environment variable overrides (`RL_SYNC_POLLING_*`, `RL_SYNC_PLAYER_TRACKING_*`, `RL_SYNC_LOCAL_*`), aliases, and anti-collision validation in `Validate()`.
  - 2. `internal/config/config_test.go`: Added 9 unit test suites covering defaults, YAML/JSON loading, env var overrides, aliases, validation, anti-collision Error 67 guard, and CLI flags.
  - 3. `internal/auth/provider.go`: Implemented `NewPollingProvider` enforcing in-memory tokens (`store = nil`) to prevent clobbering primary auth tokens in SQLite/JSONStore.
  - 4. `internal/auth/auth_test.go`: Added `TestNewPollingProvider`.
  - 5. `internal/playertrack/rank_client.go`: Implemented `SkillFetcher`, `SkillRPCClient`, `SkillRPCFactory`, `PsyNetRankClient` (with transparent reconnect and single retry), `NoOpRankClient`, `CheckCredentialCollision`, `FormatRank` (canonical 23 tiers & 4 divisions, Unranked & SSL division suppression), `FormatPlaylist`, `SerializeRanksJSON`, `ParseRanksJSON`, and `MockSkillFetcher`.
  - 6. `internal/playertrack/rank_client_test.go`: Implemented 20 unit test suites covering exhaustive tiers, divisions, Unranked/SSL suppression, out-of-bounds, playlist mapping, serialization roundtrip, mock fetcher, offline degradation, reconnect, concurrency, and auth supplier.
  - 7. `configs/config.example.yaml`: Updated with `polling_auth` and `player_tracking` sections.
  - 8. Full verification: `go build ./cmd/rl-sync`, `go test -count=1 ./...` (100% pass across all packages), `go vet ./...` (0 warnings).
