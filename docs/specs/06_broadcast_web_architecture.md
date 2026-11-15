# Sub-Spec 06: Broadcast Web Dashboard & OBS Overlay Architecture

---

## 1. Visual Design System & Persistent User Settings

### 1.1 Exact 14 `THEME_STYLES` & CSS Variables (`themeRegistry.ts`)
Preserve all 14 `ThemeStyleKey` IDs with their complete `--t-*` token dictionaries (dark & light mode variants):
1. `broadcast` — `01 · Deco Monolith: Woodblock (Default)` (`#0a0908` / `#181614` / `#789dba` / `#d9825b`)
2. `tactical` — `02 · Deco Monolith: Lacquer & Brass`
3. `swiss` — `03 · Deco Monolith: Carved Inkstone`
4. `arashi` — `04 · Deco Monolith: Warm Parchment (Light)`
5. `classic` — `05 · Classic Broadcast (Original RL/SYNC)`
6. `arena` — `06 · Glassmorphic Arena (Translucent Frosted HUD)`
7. `carbon` — `07 · Carbon Paddock (Motorsport Telemetry)`
8. `synthwave` — `08 · Mannfield Night (Neon Violet & Laser)`
9. `editorial` — `09 · Editorial Gazette (Fraunces Serif & Warm Press)`
10. `nordic` — `10 · Nordic Frost (Clean Arctic Slate & Ice)`
11. `terminal` — `11 · Retro CRT Phosphor (Monospace Mainframe)`
12. `solar` — `12 · Utopia Daylight (Warm Sand, Blue & Terracotta)`
13. `industrial` — `13 · Cobalt Blueprint (CAD Engineering Grid)`
14. `oled` — `14 · Pure OLED Pitch (Zero-Glow True Black)`

Additional styling tokens:
- `ColorMode`: `'dark' | 'light'` (updates `document.documentElement.dataset.style`, `dataset.theme`/`data-mode`, and `colorScheme`)
- `FontScale`: `'normal' | 'xl'` (`16px` / `18.4px` root `fontSize`)

### 1.2 LocalStorage Keys & Custom DOM Events (Preserved 100%)
1. `rlsync_theme_style`
2. `rlsync_color_mode`
3. `rlsync_font_scale`
4. `rlsync_replay_banner_dismissed`
5. `rlsync_pinned_players`
6. `rl_sync_column_config` (dispatches/listens to `window` custom event `'rl_sync_column_config_change'`)
7. `rlsync_preview_mode` (active **only** when `__ENABLE_PREVIEW__` is true in dev mode)
8. `rlsync_active_playlist` (persists active mode filter across reloads, defaulting to `'11'`/`2v2` when idle and auto-syncing on live match kickoff)
9. `rlsync_stamping_config` (caches `StampingConfig` thresholds & toggles for instant live reactivity, synced with `/api/stamping/config`)

---

## 2. Clean Context Architecture (Replacing 892-LOC Monolith)

Decompose dashboard state into 4 focused context modules (`< 350 LOC` each, strictly `< 800 LOC` per `SOLID-LOC-01`):
1. `ThemeContext`: `themeStyle`, `colorMode`, `fontScale`, setters, CSS custom property injection.
2. `NavigationContext`: History API sync (`/` & `/live`, `/session`, `/players`, `/players/:id`, `/history`, `/history/:guid`, `?mode=overlay`), active route state, popstate listener.
3. `SSEContext`: Single `EventSource('/api/events')` connection with reconnect indicator, latest `match_update`, `match_ended`, `clock_updated`, `session_update`, `sync_progress`, plus `lastHeartbeat`.
4. `DashboardContext`: Polls `/status`, holds pinned players, dismissed replay banner state, preview fixture toggle (dev-only), and composes `ThemeProvider`, `NavigationProvider`, and `SSEProvider` cleanly behind `DashboardProvider` / `useDashboard()`.

---

## 3. Feature Views, Shared Abstractions & Honest Telemetry

### 3.1 Eliminate Legacy Duplication & Fabricated Data
- **Shared `<PlayerDossierView />`**: Encapsulates player header, threat badges (derived from live `CurrentMatchResponse` in production, or preview fixtures only in demo mode), playlist selector, date range selector, rank grid, H2H W/L split bar, Role Split Stats (`teammate` vs `opponent`), MMR sparkline, editable notes modal trigger, and match history rows. Both `<PlayerProfilePage />` (route `/players/:id`) and `<PlayerProfileDialog />` render `<PlayerDossierView />` inside their page vs modal frame — zero duplicate stat tables!
- **Shared `useBallchasingSync()` Hook**: Single polling/trigger hook for `POST /api/sync/ballchasing` + `GET /api/sync/ballchasing/status` + SSE `sync_progress`, reused by both `HistoryView` and `SettingsPanel`.
- **Zero Fabricated Metrics & Mode-First Analytics**:
  - Remove `synthesizeRoleStats` (`avg_score: 420` fake numbers when telemetry isn't synced yet — render explicit `"No synced role telemetry"` state instead!).
  - Remove `getDateRangeFactor` (`0.35 / 0.65 / 0.85` synthetic multipliers — always query real filtered data from `/api/players/{id}?playlist=...&date_range=...`!).
  - Never synthesize `bcpm` (`bpm * 0.96`); display recorded `bcpm` or `'—'`.
  - Default missing match/session counts neutrally (`pending_count ?? 0`, `map_name: ''`, `'PENDING'`) instead of fabricating `'DFH Stadium'`, `'UPLOADED'`, or `5`.
  - **Per-Game-Mode Stat Isolation**: Aggregate stats (`SessionKpis`, `SessionForAgainstTable`, `PlayersSpotlightGrid`, `LivePlayerCard` session/role records, `PlayersDirectoryTable`, `PlayerDossierView`) are scoped to a single explicit game mode (`activePlaylist`, auto-selecting the live match playlist or most-played competitive playlist with `'11'`/`2v2` fallback). Multi-playlist summaries present side-by-side per-playlist breakdown strips rather than averaging `1v1`, `2v2`, and `3v3` together; `'all'` is reserved strictly for chronological match-log browsing.

### 3.2 2-Tier Mode-First Filter Architecture & Views (`< 650 LOC` each, strictly `< 800 LOC` per `SOLID-LOC-01`)
- **Tier 1 Slim `TopBar`**: Brand, primary view navigation (`LIVE`, `SESSION`, `HISTORY`, `PLAYERS`), active playlist session W–L + MMR status pill, replay sync indicator, `OVERLAY ↗`, and `SETTINGS` — zero overflow at `1280px` + `XL` (`+15%`) font scale.
- **Tier 2 Unified `ContextFilterBar`** (`role="toolbar"`): Shared active Game Mode selector (`2V2`, `3V3`, `1V1`, `EXTRA ▾` [`Hoops`, `Rumble`, `Dropshot`, `Snow Day`, `Tournaments`], `CASUAL`, plus `ALL MATCHES` on `HISTORY`), Timeframe selector with anchored custom date-range popover, and contextual `AVG / G | TOTALS` toggle (shown only on aggregate stat views, never triplicated in `SettingsPanel` or sub-tables).
- **Live Game View** (`LiveGameView`, `Scorebug`, `LivePlayerCard`, `IdleStateView`, `ColumnConfigModal`): Live lobby cards with custom preset/column selection via `contracts.STAT_COLUMNS` + `contracts.PRESETS`, colocated `COLUMNS` + `HISTORICAL DATA` controls, per-playlist H2H record badges, ranks, disconnect indicators, live countdown/OT timer.
- **Session View** (`SessionView`, `SessionKpis`, `MatchDetailModal`): Side-by-side played-playlist breakdown strip + playlist-scoped KPIs (`/api/session?playlist=...`), continuation banner (`Continue Previous Session` / `Dismiss`), chronological match list (`You – Opponent` score orientation) with expandable/modal box score (`<BoxScore />`).
- **Players View** (`PlayersView`, `PlayerDossierView`, `PlayerNotesModal`, `PlayersSpotlightGrid`): Playlist- and timeframe-scoped spotlights (`BestTeammate`, `MostFaced`, `ToughestRival`), semantic responsive directory table, single-source playlist/role controls in Dossier, server-backed match-list filtering/sorting.
- **History View** (`HistoryView`, `HistoricalMatchDetailModal`): Server-backed status/playlist/date-range/search/outcome/sort filters, consistent `You – Opponent` collapsed match score & roster orientation, friendly arena & playlist labels, playlist-aware positioning labels (`1ST/2ND MAN` in `2v2`, `1ST/3RD MAN` in `3v3`, hidden in `1v1`), native Unreal Units (`uu/s` speed & `uu` distance), Ballchasing backfill controls, retry & prune dialogs.
- **Layout & Overlay** (`TopBar`, `ContextFilterBar`, `SettingsPanel`, `ReplayLimitBanner`, `OBSOverlayView`):
  - `?mode=overlay`: Chromakey/transparent minimal OBS scoreboard & active-playlist MMR/session strip responding instantaneously to SSE events (with dev preview fallback when `isDemoMode()` is active).

### 3.3 Unified Multi-Signal Stamping & UI Configuration (`threatBadges.ts`, `StampingSettingsSection.tsx`)
- **Declarative `StampingConfig` Contract (`schema/dto.yaml`)**: 17 user-configurable thresholds & toggles hydrated from `/api/stamping/config`, cached in `rlsync_stamping_config`, and editable live inside `SettingsPanel` (`StampingSettingsSection`) with `PATCH /api/stamping/config` and `POST /api/stamping/config/reset`.
- **Decoupled Multi-Signal Stamp Evaluation**:
  1. **`◆ LIKELY ALT` / `◆ TAGGED ALT` (`hanko-seal-vermilion`)**: Opponent-only in competitive playlists when active `tier >= alt_min_tier` (`12`) and local user is post-placement settled (`!(localSigma > 2.90 && localTotalGames <= 40)`). Fires when `sigma >= alt_high_sigma` (`3.35`) with account games `<= alt_high_sigma_max_games` (`35`), OR `sigma >= alt_streak_sigma` (`2.75`) with games `<= alt_streak_max_games` (`45`) and `win_streak >= alt_min_win_streak` (`4`). Manual `verified`/`not-smurf` tags suppress auto alt seals; manual `smurf`/`alt` tags force `◆ TAGGED ALT`.
  2. **`◆ OFF-MODE PEAK` / `◆ TEAMMATE PEAK`**: Evaluates playlist-relevant off-modes (Core Soccar `10, 11, 13` isolated from Extra Modes `27..30` when in Soccar/Tournaments) requiring `matches_played >= off_mode_min_games` (`15`). Uses continuous Soccar-equivalent tier $S(p, r) = r.\text{tier} + 0.25 \cdot \text{clamp}(r.\text{division}, 0, 3) + \Delta_p$ ($\Delta_{10} = +1.5$ for tier $\ge 10$ or $+0.75$ below; $\Delta_{28} = -1.0$), requiring both $S_{\text{off}} - S_{\text{active}} \ge \text{off\_mode\_self\_tier\_diff}$ (`2.0`) and $S_{\text{off}} - \max(S_{\text{active}}(\text{You}), S_{\text{calibratedSoccarPeak}}(\text{You})) \ge \text{off\_mode\_vs\_you\_tier\_diff}$ (`1.5`) — never comparing against unplayed user playlists.
  3. **`◆ PARTY CARRY` / `◆ HIGHER MMR` / `◆ HOT STREAK`**: `PARTY CARRY` (`carry_mmr_diff = 100`) compares strictly within `m.team_num === player.team_num && !m.is_bot`; `HIGHER MMR` (`higher_mmr_diff = 60`) requires real non-zero active MMRs without hardcoded `1515` fallbacks; `HOT STREAK` (`hot_streak_min_wins = 4`) surfaces active-playlist momentum.
  4. **Relationship Sample Floor (`relationship_min_games = 3`)**: Unifies `ComputeRelationshipTagWithMinGames` (Go) and `computeDynamicRelationshipTag` (TS) so `< minGames` encounters render neutral `RECENT ALLY` / `RECENT FOE` instead of premature `NEMESIS` / `PRIME DUO`, with optional `activeRole` override.

### 3.4 Dev Preview Tree-Shaking (`__ENABLE_PREVIEW__`)
- `web/src/utils/sampleData.ts` gates all preview fixture literals (including `__RL_SYNC_DEV_PREVIEW_FIXTURE__`) behind the compile-time `__ENABLE_PREVIEW__` constant (`vite.config.ts`), exporting inert empty defaults whenever `__ENABLE_PREVIEW__` is `false`.
- During `npm run build` (`vite build --mode production`), Rollup dead-code-eliminates 100% of preview fixture branches from `internal/web/dist/assets/*.js`, verified by `cmd/verify-gate` Pillar 2.
