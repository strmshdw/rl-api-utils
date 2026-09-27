# Task Assignment: M2 Worker 1 — Polling Auth & PsyNet Rank Client Implementation

**Agent Identity**: `m2_worker_1`
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1`
**Authoritative Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (section ## 2026-09-26T00:19:44Z)
**Scope Document**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_3\PROJECT.md`
**Explorer Inputs**:
- `d:\code\rl-api-utils\.agents\teamwork\m2_pt_explorer_1\analysis.md`
- `d:\code\rl-api-utils\.agents\teamwork\m2_pt_explorer_2\analysis.md`
- `d:\code\rl-api-utils\.agents\teamwork\m2_pt_explorer_3\analysis.md`

## Files Owned Exclusively
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/auth/provider.go`
- `internal/playertrack/rank_client.go`
- `internal/playertrack/rank_client_test.go`

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Objectives
1. Read the 3 Explorer analysis reports.
2. In `internal/config`:
   - Add `PollingAuthConfig` and `PlayerTrackingConfig` structs.
   - Add YAML/JSON tags, defaults, env var overrides, aliases, and anti-collision validation in `Validate()`.
   - Preserve existing boundary test invariants.
   - Add unit tests in `internal/config/config_test.go`.
3. In `internal/auth`:
   - Add `NewPollingProvider(cfg config.PollingAuthConfig, opts ...Option) (AuthProvider, error)` in `provider.go` with in-memory auth state (`store = nil`) to avoid clobbering primary auth tokens in SQLite.
4. In `internal/playertrack`:
   - Implement `internal/playertrack/rank_client.go`:
     - `SkillFetcher` interface (`GetPlayersSkills`, `IsEnabled`, `Close`).
     - `PsyNetRankClient`: connects with secondary polling credentials, executes `rlapi.GetPlayersSkills`, handles transparent reconnection on connection drops.
     - `NoOpRankClient`: graceful degradation returning `(nil, nil)` when disabled, unconfigured, or on collision.
     - Canonical rank & division formatting: `FormatRank(tier, division int) string` (23 tiers 0-22, 4 divisions 0-3, Unranked & SSL division suppression).
     - Playlist mapping: `FormatPlaylist(playlistID int) string`.
     - `ranks_json` serialization: `SerializeRanksJSON`, `ParseRanksJSON`, `PlayerPlaylistRank`, `PlayerRanksSnapshot`.
   - Implement comprehensive unit tests in `internal/playertrack/rank_client_test.go`.
5. Run verification commands:
   - `go build ./cmd/rl-sync`
   - `go test -v -count=1 ./internal/config/...`
   - `go test -v -count=1 ./internal/auth/...`
   - `go test -v -count=1 ./internal/playertrack/...`
   - `go test -count=1 ./...` (must pass 100% across all packages)
   - `go vet ./...`
6. Document changes and test results in `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md`.
