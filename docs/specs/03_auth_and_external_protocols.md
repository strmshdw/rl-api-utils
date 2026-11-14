# Sub-Spec 03: Authentication, External Protocols & Replay Parser

---

## 1. Authentication & Credential Hygiene (`internal/auth/`, `steam/`)

### 1.1 `AuthProvider` Contract & Token Rotation Safety
- `AuthProvider`: `Name() string`, `ProviderKey() string`, `Authenticate(ctx context.Context) (*TokenInfo, error)`, `TokenInfo() *TokenInfo`, `SetTokenSaver(TokenSaver)`, `Close() error`.
- **Clock-Injectable `TokenInfo`**: Add `nowFn func() time.Time` (defaults to `time.Now`) and buffer `5 * time.Minute`. `IsExpired()` returns true if nil, `AccessToken == ""`, or `!ExpiresAt.IsZero() && nowFn().Add(5*time.Minute).After(ExpiresAt)`.
- **Mutex-Serialized Refresh**: Both `EpicAuthProvider` and `SteamAuthProvider` guard all token refreshes/re-auths with `refreshMu sync.Mutex` (double-checked under lock) so concurrent callers (`syncer` + `playertrack` rank poller) never trigger parallel refresh token invalidation races.
- **Persistence Precedence & Logging**: Consult `StateStore.LoadAuthState(providerKey)` before prompting interactively, even when stale `auth_code` remains in config; persist refreshed tokens via `TokenSaver` (`yaml_update.go`) and `StateStore.SaveAuthState`, logging persistence failures via `auth.WithLogger(*slog.Logger)` (`logPersistError` -> `slog.Default()` fallback).

### 1.2 Epic Games Services (`EpicAuthProvider`)
Uses `github.com/dank/rlapi` (`EGS` interface):
1. Try in-memory `token` if not expired.
2. Load persisted `AuthState` from `StateStore`; use `refresh_token` if present.
3. Fallback to `cfg.RefreshToken` -> `AuthenticateWithRefreshToken`.
4. Fallback to `cfg.AuthCode` -> `AuthenticateWithCode`.
5. Interactive prompt (`AuthPrompter`): write auth URL (`EGS.GetAuthURL()`), read auth code from user/channel respecting `ctx.Done()`.
6. **EOS Exchange Token Flow** (`ExchangeToEOS`): after Epic OAuth succeeds, call `GetExchangeCode(accessToken)` -> `ExchangeEOSToken(exchangeCode)` when EOS token is required by PsyNet / polling clients.

### 1.3 Steam Authentication (`SteamAuthProvider` & `steam/steam-auth.js`)
- Spawns `node steam/steam-auth.js` communicating line-delimited JSON over stdin/stdout (prompting for Steam Guard / 2FA code via `AuthPrompter`).
- Fix legacy Windows bug in `steam/steam-auth.js`: resolve machine name via `os.hostname()` or `process.env.COMPUTERNAME || process.env.USERNAME || 'rl-sync'` cleanly and flush errors reliably.
- Fix Go data races in `steam.go`: guard `loginKey`, `steamID64`, `epicAccountID`, and `token` under `mu`/`refreshMu`, never mutate shared `cfg` unlocked, and preserve `EpicAccountID` on subsequent token refreshes!
- **Polling Provider Isolation (`roles.go`)**:
  - Polling accounts use provider key `fmt.Sprintf("%s_polling", baseProvider)`.
  - `CheckCredentialCollision(primary, polling config.AuthConfig, store)`: fails fast if primary & polling resolve to the same `EpicAccountID` or same normalized `SteamUsername`, preventing the daemon from accidentally logging the user out of their own Rocket League client!

---

## 2. PsyNet RPC & CDN Downloader (`internal/psynet/`)

### 2.1 `MatchHistoryProvider` & Bounded Timeouts
- Wraps `rlapi.PsyNet` (`AuthPlayer` for Epic, `AuthPlayerSteam` for Steam) + `CredentialsSupplier` + `StateStore`.
- **Fix legacy unbounded RPC hang**: Every `GetMatchHistory` and underlying `AuthPlayer`/`AuthPlayerSteam` connection runs under `ctx, cancel := context.WithTimeout(parentCtx, cfg.RequestTimeout)` (default `30s`) with a `select` abort path so stalled sockets never hang goroutines.
- Auto-reconnect once on `rlapi.ErrConnectionClosed` or `"connection closed"`/`"EOF"` error; never swallow `context.Canceled`.

### 2.2 `ReplayDownloader`
- HTTP client with configurable timeout (`30s` default) + exponential backoff (up to `maxRetries` on 5xx / transient transport errors, **immediate fail** on `404`/`410` expired CDN URL or `4xx` except `429`).
- **Download & Verify**: Stream response up to `DefaultMaxDownloadBytes = 64 * 1024 * 1024` (64 MiB) into temp file `.tmp-<safeGUID>-*.replay` inside `replayDir`, validate header when `psynet.WithValidateReplay(true)` is enabled (enforced in `cmd/rl-sync/main.go`) via `replay.ParseReplayFile` / UE3 parser to ensure valid Rocket League replay, `Sync()` + close, then `os.Rename` atomically to `<safeGUID>.replay` (cleaning up `.tmp-*` on any error or `ctx.Done()`).

---

## 3. Ballchasing v2 API Client (`internal/ballchasing/`)

### 3.1 Endpoints & Streaming Upload
- Base URL default `https://ballchasing.com/api`, header `Authorization: <token>`.
- `Ping(ctx)`: `GET /api/` verifying key validity & uploader identity.
- `UploadReplay(ctx, filePath, visibility)`:
  - Validates visibility (`public`, `unlisted`, `private`, `DefaultVisibility = "public"`).
  - Streams multipart body via `io.Pipe` or `io.MultiReader` + `onceCloser` (zero whole-file memory copy).
  - HTTP `201`: returns `UploadResult{ID, Location, Duplicate: false}`.
  - HTTP `409`: parses `error` / existing `id` if returned and returns `UploadResult{ID, Duplicate: true}` as non-fatal success.
  - HTTP `429`: honors `Retry-After` header (seconds or HTTP date) or exponential backoff while checking `ctx.Done()`.
- `GetUploadedReplays(ctx, beforeTime)` / `GetReplay(ctx, id)`:
  - Custom `UnmarshalJSON` handles varied integer/float fields cleanly (`ReplayPlayerStats` nested `core`, `boost`, `movement`, `positioning`, `demo`).
  - HTTP `404` on `GetReplay` wraps `ErrNotFound` explicitly so telemetry sync marks `expired` rather than retrying indefinitely.

### 3.2 Shared Context-Aware Rate Limiter (`ratelimit.go`)
- Fixes legacy double-rate bug where `Syncer` + `BallchasingHistorySyncer` created independent limiters (hitting 4 req/sec instead of Ballchasing's 2 req/sec ceiling).
- Export `NewRateLimiter(interval time.Duration)` shared across client instances or configured on `Client`; `Wait(ctx)` uses `time.NewTimer` + `select { case <-ctx.Done(): return ctx.Err(); case <-timer.C: return nil }` without holding a mutex while sleeping!

---

## 4. UE3 `.replay` Header Parser (`internal/replay/`)

Pure Go zero-dependency binary parser wrapped with `@perf-isolation(parity_test="tests/parity/replay_parser_parity_test.go")`:
- Reads header size + CRC (`[0:8]`), engine & licensee version, net version (if `engine >= 868 && licensee >= 18`), game type `FString`.
- Parses Unreal property map until `"None"`: `IntProperty`, `FloatProperty`, `StrProperty`, `NameProperty`, `ByteProperty`, `QWordProperty`, `BoolProperty`, `ArrayProperty` (`PlayerStats`, `Goals`), handling both ASCII and negative-length UTF-16 `FString`s safely without panicking on truncated input.
