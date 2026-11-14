# `rl-sync` Rewrite — Master Specification Index

> **Status**: Normative Architecture & Contract Baseline (`v2.0` Rewrite)
> **Scope**: Complete clean-room rewrite of `github.com/dank/rl-api-utils` (`rl-sync`) targeting production release quality, 100% backward compatibility with CLI flags/env/YAML/SQLite/HTTP/SSE/UI contracts, and strict compliance with `.agents/rules/pillar_01..03.md`.

---

## 1. Bounded-Context Dependency DAG (Strict Acyclic)

All packages MUST satisfy a strict directed acyclic graph verified statically by `cmd/verify-gate`. Cycles are fatal build errors.

### 1.1 Go Backend Layers (`internal/` & `cmd/`)
```
Layer 0 (SSoT & Build):
  schema/*.yaml ──(schemagen)──▶ internal/contracts, web/src/contracts
  internal/version

Layer 1 (Pure Domain Core — Zero I/O,Zero SQL,Zero HTTP):
  internal/domain           ──▶ contracts

Layer 2 (Infrastructure Foundations):
  internal/config           ──▶ domain, contracts
  internal/replay           ──▶ domain, contracts            (@perf-isolation binary parser)
  internal/storage          ──▶ domain, contracts, config    (SQLite repos + legacy .json importer)

Layer 3 (External Protocol Adapters):
  internal/auth             ──▶ config, storage, contracts
  internal/psynet           ──▶ domain, replay, storage
  internal/ballchasing      ──▶ contracts, domain            (shared context-aware 2 rps limiter)
  internal/statsapi         ──▶ domain, contracts, storage

Layer 4 (Domain Application Orchestrators):
  internal/syncer           ──▶ domain, contracts, storage, psynet, ballchasing, statsapi
  internal/playertrack      ──▶ domain, contracts, config, storage, statsapi, auth
  internal/session          ──▶ domain, contracts, storage, statsapi, playertrack

Layer 5 (Delivery & Presentation):
  internal/web              ──▶ (go:embed all:dist)
  internal/daemon           ──▶ version, domain, contracts, config, storage, statsapi,
                                playertrack, session, syncer, ballchasing, web

Layer 6 (Composition Root & Tooling):
  cmd/rl-sync               ──▶ all runtime packages above
  cmd/schemagen             ──▶ schema/*.yaml only
  cmd/verify-gate           ──▶ stdlib only (independent static/build/test gate)
  internal/testutil         ──▶ domain, contracts, replay, psynet, ballchasing (tests only)
```

### 1.2 React / TypeScript Frontend Layers (`web/src/`)
```
contracts/   (generated from schema/*.yaml — never hand-edited)
   ▲
utils/       (formatters, platforms, broadcast helpers — zero React)
   ▲
types/       (re-exports contracts + UI-only view types)
   ▲
hooks/       (useLiveMatch, useSession, useMMRHistory, usePlayerSearch, useColumnConfig, useBallchasingSync)
   ▲
context/     (ThemeContext, SSEContext, NavigationContext, DashboardStateContext)
   ▲
components/common/      (Modal, BoxScore, Sparkline, ui primitives)
   ▲
components/{live,session,players,historical,layout,overlay}/
   ▲
App.tsx ──▶ main.tsx    (dev-only __ENABLE_PREVIEW__ provider statically excluded in prod builds)
```

---

## 2. Sub-Spec Catalog

| Spec | Domain | Key Contracts & Invariants |
| :--- | :--- | :--- |
| [01_schema_ssot_and_domain_math.md](./01_schema_ssot_and_domain_math.md) | SSoT & Domain Math | `schema/` layout, 23 rank tiers (0-based & 1-based div), 13 playlists, SplitStats/Spotlights math, primary ID parser, replay binary invariants |
| [02_configuration_and_storage.md](./02_configuration_and_storage.md) | Config & Persistence | 49 config fields, 58 `RL_SYNC_*` env vars, 22 CLI flags, comment-preserving YAML writes, 8-table SQLite DDL, v1→v4 migrations, legacy `.json` importer, batched zero-N+1 queries |
| [03_auth_and_external_protocols.md](./03_auth_and_external_protocols.md) | Auth & External APIs | Epic OAuth (`EGS`), Steam IPC (`steam/steam-auth.js`), credential collision check, PsyNet RPC (`rlapi v0.1.28`), CDN replay downloader, Ballchasing v2 + shared 2 rps limiter, UE3 `.replay` parser |
| [04_sync_engine_and_statsapi.md](./04_sync_engine_and_statsapi.md) | Sync & Live Telemetry | Syncer state machine, separate retry counters, `017db99` / `3f9d1d8` guarantees, Ballchasing history sync, `TAStatsAPI` WS/TCP listener, 20-match tracker, Windows toast |
| [05_playertrack_session_daemon_cli.md](./05_playertrack_session_daemon_cli.md) | Tracking, Daemon & CLI | Rank LRU/TTL cache, local player resolution (`3744274`), Session epoch/continuation/archival, SSE broadcaster, all 30 HTTP routes/aliases, CSRF/origin/traversal guards, CLI runner |
| [06_broadcast_web_architecture.md](./06_broadcast_web_architecture.md) | Web UI & Overlay | 14 CSS themes, 4 font scales, 7 `localStorage` keys, split contexts, `<PlayerDossierView />`, honest telemetry (zero fake data multipliers), OBS overlay, `__ENABLE_PREVIEW__` tree-shaking |

---

## 3. Production Engineering Standards (`AGENTS.md` v2.0)

1. **File Cohesion (`SOLID-LOC-01`)**: Every `.go`, `.ts`, and `.tsx` file (production, generated, and test) MUST be **`< 800 LOC`** (target `150–550 LOC`).
2. **Schema SSoT (`INV-SCHEMA-SSOT`)**: Shared cross-boundary enums, playlist/tier tables, stat column definitions, and Go↔TS API/SSE DTOs are declared once in `schema/*.yaml` and code-generated into `internal/contracts` and `web/src/contracts` by `cmd/schemagen`.
3. **Magic Slice / Offset Ban (`SOLID-SLICE-01` & `SOLID-PERF-01`)**: Indexing raw bytes or slices with literal bounds `>= 16` outside `schema/`, `*_test.go`, `*.test.ts*`, and `web/src/contracts/` is forbidden unless annotated with:
   `# @perf-isolation(parity_test="tests/parity/...")` accompanied by `What:`, `Why:`, `How:` comments and a real passing parity test on disk.
4. **4-Pillar Pre-Merge Gate (`INV-HARNESS-DOD`)**: `scripts/verify.ps1` / `cmd/verify-gate`:
   - **Pillar 4 (Trunk Lock)**: Immediately exit `42` on `main` or `master`.
   - **Pillar 1 (Static & Structure)**: `go run ./cmd/schemagen -check`, `docs/specs/*.md < 46,080 B`, every source/test file `< 800 LOC`, acyclic Go & TS import DAG, `SOLID-SLICE-01` / `SOLID-PERF-01`, `go vet ./...`, `npm --prefix web exec tsc -- -b`.
   - **Pillar 2 (Production Artifacts)**: `npm --prefix web run build` (asserting preview fixtures pruned from bundle), `go build -o rl-sync.exe ./cmd/rl-sync` (clean build).
   - **Pillar 3 (Verification)**: `go test -race ./...` and `npm --prefix web test -- --run`.
5. **Known Legacy Bugs Eliminated by Construction**:
   - Eliminate dual-store SQLite/JSON bloat (`sqlite.go` 5.6k LOC + `jsonstore.go` 3.8k LOC) via small SQLite repository modules + `< 250 LOC` legacy JSON auto-importer.
   - Eliminate N+1 SQLite queries (`ListHistoricalMatches`, `ListPlayerMatches`, `GetSessionSummary`, `enrichSessionTelemetry`).
   - Serialize OAuth refreshes (`refreshMu`), make `TokenInfo.IsExpired` clock-injectable, fix Steam `LoginKey`/`EpicAccountID` races, fix Windows `USERNAME` fallback in `steam-auth.js`, consult `StateStore` even when `auth_code` remains in YAML.
   - Bound blocking PsyNet RPCs to 30s timeout contexts; wrap Ballchasing 404 as `ErrNotFound`; throttle all background Ballchasing GETs through a shared 2 req/sec limiter; serialize `syncer.RunCycle` and `PollAndSyncPendingStats`.
   - Remove fake UI telemetry (`synthesizeRoleStats` avg_score=420, `getDateRangeFactor`), deduplicate `PlayerProfilePage`/`Dialog` and Ballchasing sync polling.
