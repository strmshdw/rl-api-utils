# Project: Rocket League Replay Synchronizer Daemon (rl-api-utils)

## Architecture

The project is an automated daemon written in Go that periodically queries Rocket League match history via `github.com/dank/rlapi` (Rocket League PsyNet RPC), downloads new `.replay` binary payloads, and uploads them to Ballchasing.com.

The system is architected around Clean Architecture principles, ensuring complete decoupling between synchronization orchestration and concrete network implementations:

```
[cmd/rl-sync] (CLI: flags, signals, entry point)
       │
       ▼
[internal/daemon] (Lifecycle: 5m ticker, immediate startup run, OS signal trap, graceful drain)
       │
       ▼
[internal/syncer] (Domain Orchestrator: poll -> diff -> download -> upload -> persist)
       ├──> [internal/storage] (StateStore: pure Go modernc.org/sqlite, idempotency guarantees)
       ├──> [internal/auth]    (AuthProvider: Epic Games OAuth/EOS & Steam ticket exchange)
       ├──> [internal/psynet]  (MatchHistoryProvider & ReplayDownloader: rlapi adapter & atomic file streaming)
       └──> [internal/ballchasing] (ReplayUploader: multipart POST, raw token, 201/409/429 backoff handling)
```

```
[internal/testutil] (E2E Test Harness: Mock PsyNet HTTP/WebSocket, Mock Ballchasing, Mock CDN)
```

---

## Feature Inventory

| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Pure Go SQLite Store | ACID persistence using `modernc.org/sqlite` (zero CGO) with `matches` and `auth_state` tables | M1 | survey_explorer_arch_1 |
| 2 | Idempotency & Crash Recovery | Guaranteed zero duplicate downloads and uploads via status transitions & startup recovery query | M1 | survey_explorer_arch_1 |
| 3 | Layered Configuration | YAML & JSON file loading, env var mappings (`RL_SYNC_*`), CLI overrides, and defaults | M1 | survey_explorer_arch_1 |
| 4 | Epic Games Authentication | OAuth refresh token and auth code exchange for EOS tokens (`egs.ExchangeEOSToken`) and `psyNet.AuthPlayer` | M2 | survey_miner_rlapi_1 |
| 5 | Steam Authentication | Steam session ticket exchange (`egs.ExchangeEOSTokenFromSteam`) and `psyNet.AuthPlayerSteam` with SteamID64 | M2 | survey_miner_rlapi_1 |
| 6 | Match History Polling | Query `Matches/GetMatchHistory v1` via `rlapi.PsyNetRPC` returning GUIDs, timestamps, and replay URLs | M2 | survey_miner_rlapi_1 |
| 7 | Atomic Replay Downloader | HTTP GET from PsyNet signed `ReplayUrl`, stream to `.tmp` file, size validation (>1KB), atomic `os.Rename` | M2 | survey_miner_ballchasing_1 |
| 8 | Ballchasing Multipart Upload | `POST /v2/upload` multipart/form-data with `file` part and user-configured `visibility` | M3 | survey_miner_ballchasing_1 |
| 9 | Ballchasing Raw Auth Header | `Authorization: <token>` (raw token without `Bearer ` prefix) | M3 | survey_miner_ballchasing_1 |
| 10 | Ballchasing HTTP 201 Handling | Process 201 Created response, parse JSON `id` and `location`, record in persistent state | M3 | survey_miner_ballchasing_1 |
| 11 | Ballchasing HTTP 409 Deduplication | Process 409 Conflict, extract existing replay `id` and `location`, mark `DUPLICATE`, no error or retry thrashing | M3 | survey_miner_ballchasing_1 |
| 12 | Ballchasing HTTP 429 Rate Limiting | Detect 429 Too Many Requests, parse `Retry-After` header, apply exponential backoff with retry budget | M3 | survey_miner_ballchasing_1 |
| 13 | Ballchasing HTTP 401 & Permanent Errors | Detect 401 Unauthorized or 400 Bad Request and immediately halt retries with descriptive errors | M3 | survey_miner_ballchasing_1 |
| 14 | Syncer Domain Orchestrator | Core diffing logic: compare PsyNet matches with state store, trigger download & upload, commit state | M4 | survey_explorer_arch_1 |
| 15 | Daemon Engine & Lifecycle | Ticker loop (default 5m), immediate initial run, OS signal trapping (SIGINT/SIGTERM), graceful drain | M4 | survey_explorer_arch_1 |
| 16 | CLI Interface & Modes | Flags `--config`, `--once` (single-run), `--dry-run` (simulation mode), `--log-level`, `--log-format` | M4 | survey_explorer_arch_1 |
| 17 | Structured Logging | JSON and text structured logging via Go `log/slog` with timestamps and contextual attributes | M4 | survey_explorer_arch_1 |
| 18 | Mock PsyNet Server | `httptest.Server` with HTTP bootstrap and WebSocket RPC handler (`MockWSServer`) supporting dynamic matches | E2E Track | survey_explorer_arch_1 |
| 19 | Mock Ballchasing Server | `httptest.Server` simulating 201 Created, 409 Conflict, 429 Rate Limit, and 401 Unauthorized | E2E Track | survey_explorer_arch_1 |
| 20 | Mock Replay CDN Server | `httptest.Server` serving valid mock `.replay` binary payloads with `TAGAME` headers | E2E Track | survey_explorer_arch_1 |
| 21 | Multi-Cycle & Restart Testing | Opaque-box verification of multi-cycle polling (new matches on cycle 2) and persistent restart idempotency | E2E Track | survey_explorer_arch_1 |
| 22 | Final E2E Suite & Adversarial Pass | 100% pass of Tiers 1-4 E2E test suite and Tier 5 adversarial edge-case coverage hardening | M5 | orchestrator_1 |

---

## Milestones

| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Storage & Configuration | `internal/storage`, `internal/config`, `go.mod`, data models, pure Go SQLite, JSON fallback, config loading & env vars | none | DONE |
| M2 | Auth & PsyNet Integration | `internal/auth`, `internal/psynet`, Epic Games & Steam auth flows, `MatchHistoryProvider`, atomic `.tmp` replay downloader | M1 | DONE |
| M3 | Ballchasing Replay Uploader | `internal/ballchasing`, multipart upload client, raw token auth, 201 Created, 409 Duplicate handling, 429 backoff engine, 401 rejection | M1 | DONE |
| M4 | Syncer, Daemon & CLI | `internal/syncer`, `internal/daemon`, `cmd/rl-sync`, CLI flags (`--once`, `--dry-run`), signal handling, graceful drain, structured logging | M2, M3 | DONE |
| M5 | Final Milestone & Hardening | Pass 100% of E2E Test Suite (Tiers 1-4) published in `TEST_READY.md`, followed by Tier 5 adversarial coverage hardening | M4, E2E Track | DONE |

*Parallel Track:*
- **E2E Testing Track**: Autonomous track producing `TEST_INFRA.md`, mock test infrastructure (`internal/testutil`), comprehensive 4-tier opaque-box test suite (Tiers 1-4), and publishing `TEST_READY.md`.

---

## Interface Contracts

### `internal/storage` ↔ `internal/syncer`
```go
package storage

import (
    "context"
    "time"
)

type DownloadStatus string
const (
    DownloadPending     DownloadStatus = "PENDING"
    DownloadDownloading DownloadStatus = "DOWNLOADING"
    DownloadDownloaded  DownloadStatus = "DOWNLOADED"
    DownloadFailed      DownloadStatus = "FAILED"
    DownloadSkipped     DownloadStatus = "SKIPPED"
)

type UploadStatus string
const (
    UploadPending   UploadStatus = "PENDING"
    UploadUploading UploadStatus = "UPLOADING"
    UploadUploaded  UploadStatus = "UPLOADED"
    UploadDuplicate UploadStatus = "DUPLICATE"
    UploadFailed    UploadStatus = "FAILED"
)

type MatchRecord struct {
    MatchGUID            string
    RecordStartTimestamp int64
    MapName              string
    Playlist             int
    ReplayURL            string
    DownloadStatus       DownloadStatus
    LocalFilePath        string
    DownloadedAt         *time.Time
    UploadStatus         UploadStatus
    BallchasingID        string
    BallchasingURL       string
    UploadedAt           *time.Time
    RetryCount           int
    LastError            string
    CreatedAt            time.Time
    UpdatedAt            time.Time
}

type StateStore interface {
    GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error)
    ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error)
    ListPendingUploads(ctx context.Context) ([]*MatchRecord, error)
    UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error
    MarkDownloading(ctx context.Context, matchGUID string) error
    MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
    MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
    MarkUploading(ctx context.Context, matchGUID string) error
    MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
    MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
    MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error
    RecoverInFlight(ctx context.Context) error
    SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error
    GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)
    Close() error
}
```

### `internal/psynet` & `internal/auth` ↔ `internal/syncer`
```go
package syncer

import (
    "context"
    "io"
)

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

type ReplayDownloader interface {
    DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
}
```

### `internal/ballchasing` ↔ `internal/syncer`
```go
package syncer

import "context"

type UploadResult struct {
    ID          string
    Location    string
    IsDuplicate bool
}

type ReplayUploader interface {
    UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
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
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── storage/
│   │   ├── store.go
│   │   ├── sqlite.go
│   │   ├── sqlite_test.go
│   │   ├── jsonstore.go
│   │   └── jsonstore_test.go
│   ├── auth/
│   │   ├── provider.go
│   │   ├── epic.go
│   │   ├── steam.go
│   │   └── auth_test.go
│   ├── psynet/
│   │   ├── client.go
│   │   ├── downloader.go
│   │   └── downloader_test.go
│   ├── ballchasing/
│   │   ├── client.go
│   │   ├── types.go
│   │   └── client_test.go
│   ├── syncer/
│   │   ├── interfaces.go
│   │   ├── syncer.go
│   │   └── syncer_test.go
│   ├── daemon/
│   │   ├── daemon.go
│   │   └── daemon_test.go
│   └── testutil/
│       ├── mock_psynet.go
│       ├── mock_ballchasing.go
│       └── mock_cdn.go
├── configs/
│   ├── config.example.yaml
│   └── config.example.json
├── test/
│   └── e2e/
│       ├── e2e_test.go
│       ├── tier1_feature_test.go
│       ├── tier2_boundary_test.go
│       ├── tier3_pairwise_test.go
│       └── tier4_workload_test.go
├── go.mod
├── go.sum
└── README.md
```
