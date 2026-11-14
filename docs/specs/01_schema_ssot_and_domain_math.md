# Sub-Spec 01: Declarative Schema SSoT & Domain Mathematics

---

## 1. Schema Single Source of Truth (`schema/`)

Three declarative YAML manifests feed `cmd/schemagen`, generating `internal/contracts/generated_*.go` (Go) and `web/src/contracts/generated_*.ts` (TypeScript):

1. `schema/enums.yaml`:
   - `MatchStatus`: `PENDING`, `DOWNLOADED`, `SYNCED`, `FAILED` (also define `UPLOAD_FAILED = FAILED` alias in domain constants for backward compatibility).
   - `ReplayStatsStatus`: `pending`, `fetched`, `expired`, `failed` (plus domain `ReplayStatsUnavailable`).
   - `PlayerClassification`: `self`, `teammate`, `opponent`, `bot`, `spectator`.
   - `SyncMode`: `auto`, `manual`, `startup`.
   - `SSEEventType`: `match_update`, `match_ended`, `clock_updated`, `session_update`, `sync_progress`.
   - `TierNames` (`0..22`), `ShortTierNames` (`0..22`), `DivisionNames` (`0..3`), `ShortDivisionNames` (`0..3`), and SQLite storage `ShortTierStorageNames` (tier 7..9 use `"GOLD I..III"` vs tactical live UI `"GLD I..III"`—both emitted explicitly so 100% of existing rank tests & live badges remain exact).
   - `Playlists` (`1, 2, 3, 4, 10, 11, 12, 13, 27, 28, 29, 30, 34`) with `id`, `name` (canonical `"Casual Duel (1v1)"`, `"Ranked Doubles (2v2)"`, `"Ranked Hoops"`, `"Competitive Tournaments"`), `storage_name` (`StorageName` in Go), `is_ranked`, `is_extra_mode`, and `slugs`.
2. `schema/telemetry.yaml`:
   - `stat_columns` (`score`, `goals`, `assists`, `shots`, `saves`, `demos`, `mmr`, `rank`, `h2h`, `platform`) with `key`, `label`, `short_label` (`shortLabel` in TS), `description`, canonical `column_order`, and `presets` (`full`, `competitive`, `streamer`).
3. `schema/dto.yaml`:
   - 11 shared cross-boundary DTO primitives (`PlayerStatsSummary`, `PlayerPlaylistRank`, `PlayerMatchup`, `RoleStatsBreakdown`, `PlayerRoleStatsSummary`, `MatchClock`, `PendingMatch`, `StatsAPIStatus`, `SyncProgress`, `MMRHistoryRecord` / `MMRHistoryEntry`, `SyncConfigResponse` / `SyncConfig`). Composite HTTP response envelopes (`DaemonStatusResponse`, `SessionResponse`, `MatchesResponse`, `PlayerSearchResponse`, `PlayerDetailResponse`, `CurrentMatchResponse`, `ReplayMatchStats`) live in their owning bounded contexts (`daemon`, `session`, `playertrack`, `storage`) composing these contract primitives.

> **Invariance Rule**: `cmd/schemagen -check` diffs generated files in-memory against disk byte-for-byte. Any manual edit in `internal/contracts/` or `web/src/contracts/` fails Pillar 1 immediately.

---

## 2. Canonical 23-Tier Ranking & Division System

Rocket League tiers span `0..22`:
`0: Unranked` (`UNRANKED`), `1..3: Bronze I..III` (`BRZ`), `4..6: Silver I..III` (`SLV`), `7..9: Gold I..III` (`GLD` live / `GOLD` storage), `10..12: Platinum I..III` (`PLAT`), `13..15: Diamond I..III` (`DIA`), `16..18: Champion I..III` (`CHAMP`), `19..21: Grand Champion I..III` (`GC`), `22: Supersonic Legend` (`SSL`).

### 2.1 Dual Division Convention (Preserved Cleanly)
1. **PsyNet Live API (`rlapi.Skill`)** uses **0-based divisions** `0..3`:
   - `TierName(tier)`: out of `0..22` returns `"Unknown"`.
   - `FormatRank(tier, div)`: Tier `0` or `22` omits division. Tiers `1..21` with `div in 0..3` return `"<Tier> Division <I..IV>"`; out-of-bounds `div` returns base `<Tier>`.
   - `FormatShortRank(tier, div)`: Tier out of bounds -> `"UNRANKED"`; `0` or `22` omits division; `div in 0..3` -> `"<SHORT_TIER> · DIV <I..IV>"`.
2. **Ballchasing Storage Repair (`ReplayPlayerStats.Rank`)** uses **1-based divisions** `1..4`:
   - `FormatStorageRank(tier, div)`: Tier out of bounds -> `fmt.Sprintf("Tier %d", tier)`; `0` or `22` omits division; `div in 1..4` -> `"<Tier> Div <I..IV>"`; out-of-bounds `div` returns `<Tier>`.
   - `FormatStorageShortRank(tier, div)`: Tier out of bounds -> `fmt.Sprintf("T%d", tier)`; `0` or `22` omits division; `div in 1..4` -> `"<SHORT_STORAGE_TIER> · DIV <I..IV>"`.

---

## 3. Player Identity, Classification & Playlist Rules

### 3.1 Composite Player ID Format
Every Stats API / stored ID follows `<Platform>|<AccountID>|<Splitscreen>` (3 pipe-separated parts):
- `ParsePrimaryID(raw)`: trims outer whitespace & segments, validates 3 segments and integer `Splitscreen`. `IsBot == true` when `strings.EqualFold(Platform, "Unknown") || AccountID == "0"`.
- Special lookup aliases: `strings.EqualFold(id, "me") || strings.EqualFold(id, "you")` resolve to the highest-priority non-polling local player ID.
- Steam <-> Epic normalization: When matching a player against local identities or lookup queries, check exact lowercase match, account ID match, and Steam64 <-> Epic cross-mapped account ID when recorded in `auth_state`.

### 3.2 Playlist Resolution & Normalization
- Ranked playlists: `{10, 11, 12, 13, 27, 28, 29, 30, 34}`.
- Extra modes: `{27, 28, 29, 30}`.
- **Storage vs Live Display Names**: `contracts.FormatPlaylist(id)` uses canonical `"Ranked Doubles (2v2)"` etc.; storage display fallback maps Ballchasing slugs (`"ranked-doubles"` -> `11`, `"duels"`/`"ranked-duels"` -> `10`, `"standard"`/`"ranked-standard"` -> `13`, `"hoops"` -> `27`, `"rumble"` -> `28`, `"dropshot"` -> `29`, `"snowday"`/`"snow-day"` -> `30`, `"tournament"` -> `34`, `"unranked-*"` -> `1..4`) cleanly via generated lookup table.

---

## 4. Statistical & Domain Mathematical Formulas

### 4.1 Rounding & Win Rate
- `RoundFloat(val float64, precision int) float64`: `ratio := math.Pow10(precision); return math.Round(val*ratio) / ratio`.
- `ComputeWinRate(wins, total int) float64`: `0.0` if `total <= 0`, else `RoundFloat((float64(wins)/float64(total))*100.0, 1)`.
- `ComputeMMRDelta(start, end float64) float64`: `0.0` if `start <= 0 || end <= 0`, else `RoundFloat(end-start, 2)`.

### 4.2 Role Split Stats & Global Spotlights (`domain/stats.go`)
For each player `P` across completed `match_players` rows filtered by `(playlist_id, date_range)` (all dates, `7d`, `30d`, `90d` computed strictly relative to injected/UTC `now`):
- Group rows by `role` (`"teammate"` vs `"opponent"`) using distinct `match_guid` count (`Games`).
- Sum `Score, Goals, Assists, Saves, Shots, Demos`, `Wins` (where `outcome == "victory"`), `Losses` (`"defeat"`).
- Compute per-game averages only when `Games > 0` rounded to 2 decimals (`float64(sum)/float64(Games)`).
- Compute shooting accuracy `ShotPct = (Goals / Shots) * 100` (`0` when `Shots == 0`).
- **Global Spotlights** (`storage/spotlights.go` `ComputeGlobalSpotlights` excluding bots & local players):
  - `BestTeammate`: highest win rate with teammate (tie-break highest teammate games, most recent `LastSeenAt`, then lexicographical `PlayerID`).
  - `MostFaced`: highest games against opponent (tie-break most recent `LastSeenAt`, then lexicographical `PlayerID`).
  - `ToughestRival`: lowest local win rate against opponent (tie-break highest opponent games, most recent `LastSeenAt`, then lexicographical `PlayerID`).

### 4.3 Network Disconnect Error Classification (`IsDisconnectError`)
Returns `true` if `err != nil` and:
`errors.Is(err, context.Canceled)`, `errors.Is(err, syscall.EPIPE)`, `errors.Is(err, syscall.ECONNRESET)`, OR lowercase error message contains `"broken pipe"`, `"connection reset by peer"`, `"wsasend"`, `"wsarecv"`, `"forcibly closed by the remote host"`, or `"an established connection was aborted by the software in your host machine"` (WinSock 10053/10054).

---

## 5. UE3 `.replay` Binary Validation Invariant
Replay downloads are validated by `internal/replay` before atomic persistence:
- Little-endian header layout: `int32 headerSize`, `int32 headerCRC`, `int32 engineVersion`, `int32 licenseeVersion`, optional `int32 netVersion` when `engineVersion >= 868 && licenseeVersion >= 18`, `FString` gameType (`"TAGame.Replay_Soccar_TA"`).
- Every `FString` length read enforces signed/UTF-16 decoding and hard bounds against remaining buffer (`<= maxReplayStringBytes`) so malformed payloads never OOM or panic.
- All fixed header/CRC offsets `>= 16` live in annotated `@perf-isolation(parity_test="tests/parity/replay_parser_parity_test.go")` helpers.
