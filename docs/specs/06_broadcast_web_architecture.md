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

---

## 2. Clean Context Architecture (Replacing 892-LOC Monolith)

Decompose `DashboardContext.tsx` into 4 focused contexts (`< 250 LOC` each):
1. `ThemeContext`: `themeStyle`, `colorMode`, `fontScale`, setters, CSS custom property injection.
2. `NavigationContext`: History API sync (`/` & `/live`, `/session`, `/players`, `/players/:id`, `/history`, `/history/:guid`, `?mode=overlay`), active route state, popstate listener.
3. `SSEContext`: Single `EventSource('/api/events')` connection with reconnect indicator, latest `match_update`, `match_ended`, `clock_updated`, `session_update`, `sync_progress`, plus `lastHeartbeat`.
4. `DashboardStateContext`: Polls `/status`, holds pinned players, dismissed replay banner state, preview fixture toggle (dev-only), and composes the above cleanly.

---

## 3. Feature Views, Shared Abstractions & Honest Telemetry

### 3.1 Eliminate Legacy Duplication & Fabricated Data
- **Shared `<PlayerDossierView />`**: Encapsulates player header, playlist selector, date range selector, rank grid, H2H W/L split bar, Role Split Stats (`teammate` vs `opponent`), MMR sparkline, editable notes modal trigger, and match history rows. Both `<PlayerProfilePage />` (route `/players/:id`) and `<PlayerProfileDialog />` render `<PlayerDossierView />` inside their page vs modal frame — zero duplicate stat tables!
- **Shared `useBallchasingSync()` Hook**: Single polling/trigger hook for `POST /api/sync/ballchasing` + `GET /api/sync/ballchasing/status` + SSE `sync_progress`, reused by both `HistoryView` and `SettingsPanel`.
- **Zero Fabricated Metrics**:
  - Remove `synthesizeRoleStats` (`avg_score: 420` fake numbers when telemetry isn't synced yet — render explicit `"No synced role telemetry"` state instead!).
  - Remove `getDateRangeFactor` (`0.35 / 0.65 / 0.85` synthetic multipliers — always query real filtered data from `/api/players/{id}?playlist=...&date_range=...`!).

### 3.2 Views & Components (`< 500 LOC` each)
- **Live Game View** (`LiveGameView`, `Scorebug`, `LivePlayerCard`, `IdleStateView`, `ColumnConfigModal`): Live lobby cards with custom preset/column selection via `contracts.STAT_COLUMNS` + `contracts.PRESETS`, H2H record badges, ranks, disconnect indicators, live countdown/OT timer.
- **Session View** (`SessionView`, `SessionKpis`, `MatchDetailModal`): KPIs, per-playlist MMR gain/loss pills, continuation banner (`Continue Previous Session` / `Dismiss`), chronological match list with expandable/modal box score (`<BoxScore />`).
- **Players View** (`PlayersView`, `PlayerDossierView`, `PlayerNotesModal`, `GlobalSpotlightsPanel`): Search + sort + pinned players + global spotlights (`Nemesis`, `FavoriteTeammate`, `BestRivalry`, `MostPlayed`).
- **History View** (`HistoryView`, `HistoricalMatchDetailModal`): Status/playlist/date-range/search filters, Ballchasing backfill controls, match detail drill-down, retry button for `FAILED` matches, match prune dialog.
- **Layout & Overlay** (`TopBar`, `SettingsPanel`, `ReplayLimitBanner`, `OBSOverlayView`):
  - `?mode=overlay`: Chromakey/transparent minimal OBS scoreboard & MMR strip responding instantaneously to SSE events.

### 3.3 Dev Preview Tree-Shaking (`__ENABLE_PREVIEW__`)
- `web/src/preview/sampleData.ts` contains optional rich preview fixtures for `vite build:dev`.
- Import `preview/` exclusively inside `if (__ENABLE_PREVIEW__)` dynamic import (`import('../preview/sampleData')`) so Vite/Rollup tree-shakes 100% of sample fixture code out of `internal/web/dist/assets/*.js` during `npm run build` (`--mode production`).
