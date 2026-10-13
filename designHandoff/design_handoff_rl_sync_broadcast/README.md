# Handoff: RL Sync — "Broadcast" dashboard redesign

## Overview
A visual and information-density redesign of the `rl-sync` web dashboard (`web/src`). It covers the app shell, the Live tab (in-match and between-matches states), Session, History and Players, plus the Player Profile dialog. The goals: replace the generic slate/glow look with a bold esports-broadcast identity, and remove low-value chrome so the box score, opponent ranks and H2H records get the space.

## About the design files
The files in this bundle are **design references built in HTML**: prototypes that show the intended look and behaviour. They are not production code to copy. Recreate them inside the existing app (React 18 + TypeScript + Tailwind 3 + Vite, in `web/src`) using its components, hooks and types. All data in the prototype is hard-coded sample data. In the real app it comes from the existing hooks (`useSession`, `useLiveMatch`, `usePlayerSearch`) and the `/api/*` endpoints.

To view: open `RL Sync Broadcast.dc.html` in a browser (keep `support.js` next to it). Click the top tabs to switch screens. To flip the Live tab between in-match and idle, change `liveState` in the file's `data-props` (or the host Tweaks panel). `reference/Current Dashboard.dc.html` is a faithful recreation of today's Live view, for before/after comparison.

## Fidelity
**High-fidelity.** Colours, type, spacing and layout are final. Match them closely using Tailwind (arbitrary values are fine, e.g. `bg-[#121418]`, `font-['Barlow_Condensed']`).

---

## Design tokens

### Colour
| Token | Value | Use |
|---|---|---|
| bg | `#0a0b0d` | Page background, gaps between panels |
| panel | `#121418` | Rows, cards, KPI strips |
| panel-hover | `#171a1f` | Row hover |
| modal | `#0f1114` | Profile dialog body |
| scrim | `rgba(5,6,8,.82)` | Dialog backdrop |
| line | `#1c1e22` | Top-bar border, hairlines inside panels |
| line-2 | `#23262c` | Stat-strip divider; empty H2H cell |
| outline | `#2a2d33` | Ghost buttons, segmented controls |
| ink | `#f2f3f5` | Primary text; scorebug number tiles; active segment bg |
| ink-strong | `#ffffff` | Stat leader values; active nav |
| ink-2 | `#c4c7cd` | Secondary values (MMR, rank) |
| ink-3 | `#9a9ea7` | Non-leader stat values, shots |
| muted | `#7b7f88` | Labels, meta text, inactive nav |
| muted-2 | `#6c7079` | Small caps labels inside panels |
| faint | `#4a4e57` | Zero values, column headers, "—" |
| blue-slab | `oklch(0.55 0.16 245)` ≈ `#0b6fc4` | Blue team slab / top rule |
| orange-slab | `oklch(0.66 0.17 50)` ≈ `#e0661b` | Orange team slab / top rule (text on it is `#0a0b0d`) |
| blue-ink | `oklch(0.72 0.15 240)` ≈ `#3ba1ea` | Blue score digits, "you" highlight, logo slash |
| orange-ink | `oklch(0.72 0.17 50)` ≈ `#f5822f` | Orange score digits |
| win | `oklch(0.78 0.15 150)` ≈ `#5fd38a` | W slab, positive MMR, win cells, status dots |
| loss / live | `oklch(0.72 0.19 25)` ≈ `#f7666b` | L slab, negative MMR, loss cells, LIVE dot, OT tag, failed state |

There are no gradients, glows, drop-shadows or rounded corners anywhere (radius 0, except status dots, which are circles). The only non-rectangular shapes are the angled outer edges of the live scorebug team slabs (`clip-path`, 28px slant).

### Typography
Google Fonts: **Barlow Condensed** (500/600/700/800, plus italic 800) for everything display/UI, and **Barlow** (400–600) as the body fallback.

| Role | Spec |
|---|---|
| Page title (SESSION / HISTORY / PLAYERS / WAITING FOR KICKOFF) | Barlow Condensed italic 800, 64px (idle: 72px), line-height .9 |
| Profile name | Barlow Condensed italic 800, 64px, uppercase |
| Scorebug numbers | 800, 76px (live) / 60px (last-match strip) |
| Big stat values | 800, 44px (live player cards), 48–52px (KPI strip), 64px (profile H2H) |
| Player name (live card) | 700, 34px, uppercase, letter-spacing .02em |
| Row values | 700, 20–26px |
| Row titles (playlist, names in tables) | 700, 18–22px, letter-spacing .03–.06em |
| Section label | 700, 14px, letter-spacing .16em, uppercase, `muted` |
| Column header | 600, 12px, letter-spacing .14em, `faint` |
| Meta / sublines | 500–600, 13–15px, letter-spacing .06–.1em, `muted` |
| Nav tabs | 700, 15px, letter-spacing .08em |
| Logo | Barlow Condensed italic 800, 22px, "RL/SYNC" (slash in blue-ink) |

All UI labels are uppercase. Numbers use Barlow Condensed (no mono font).

### Spacing
- Page gutter 32px; content max-width 1440px, centred.
- Top bar height 56px.
- Page header block: 40px top padding, 24px bottom (idle: 48/32).
- Section gap 28–40px. Row stacks use a 6px gap. Grid gaps between panels are 2px (the page bg shows through as a divider) or 12px for cards.
- Row heights: session 72px, history 64px, players 60px, profile per-playlist 52px.
- Panel padding 16–26px horizontal.

---

## Screens

### 1. App shell (replaces `layout/Header.tsx` + `layout/Navbar.tsx`)
A single 56px sticky bar, bg `#0a0b0d`, with a 1px `line` bottom border. Header and nav are merged; the playlist-pill carousel and the "Preview UI" button are removed.
- **Left:** logo, then 40px gap, then tabs `LIVE`, `SESSION 5`, `HISTORY`, `PLAYERS` (28px gap).
  - Active tab: white with a 3px white inset bottom bar.
  - Inactive tab: `muted`.
  - LIVE has a 7px dot before the label: loss/live red when in a match, `faint` when idle.
  - SESSION shows the match count in `#4c5059`.
- **Right** (24px gap): `SESSION 4–1` · `DOUBLES 1485 +20` (current playlist MMR, delta in win/loss colour) · `OVERLAY ↗` ghost link (1px `outline` border, 4px/10px padding), which opens `/?mode=overlay`.
- The session reset action moves to the Session page.

### 2. Live — in match (replaces `ScoreboardBanner`, `RosterTable`, `PlayerRow`)
- **Scorebug**, centred, 92px tall, 36px top / 28px bottom padding:
  - [Blue slab, max 300px, shrinks: "BLUE" 30px/800 + "AVG 1494 · YOUR TEAM" 14px]
  - [110px `ink` tile with the blue score in `#0a0b0d`]
  - [150px `#16181c` centre: "● LIVE" red 13px/.14em, playlist "RANKED 2V2" 15px `ink-2`, parity "EVEN MATCH" / "+N BLUE" 12px `muted-2`]
  - [110px orange score tile]
  - [Orange slab, mirror image]
  - Slab outer edges are slanted 28px. "YOUR TEAM" goes on whichever side the local player is on.
  - Parity rule (unchanged from today): |avg diff| < 5 means EVEN MATCH.
- **Teams:** two equal columns, 28px gap. Each column has a 4px rule in the team ink colour, then one card per player (12px gap), sorted by score descending.
- **Player card** (`panel`, padding 22px 26px, 18px internal gap; the local player gets a 3px blue-ink left border):
  - Top-left: name (uppercase 34px; "You" for the local player), then meta `PLATFORM · RANK · MMR` (14px, single line, ellipsis).
  - Top-right: H2H label (`WITH YOU` for teammates, `VS YOU` for opponents, `THIS SESSION` on your own card), then 5 cells (10×18px, 3px gap: wins in green, then losses in red, then `line-2` for empty), then the W–L record at 24px in win, loss or neutral colour.
  - Bottom: stat strip, 1px `line-2` top border, 14px padding-top, grid `1.6fr repeat(5,1fr)`: SCORE, G, A, SV, SH, D. Labels are 12px `muted-2`; values are 44px.
  - **Value colour:** the lobby leader for that stat (value > 0) is `#fff`, other non-zero values are `ink-3`, and zeros are `faint`.
- **Removed:** shots/saves/demos momentum line, per-team aggregate pills, demo banner, icons. The **Customize Columns** feature still exists in code (`ColumnConfigModal`, `useColumnConfig`). Expose it as a small ghost button, e.g. next to the scorebug or in the top bar; columns map 1:1 to the stat strip plus the rank/MMR/H2H/platform pieces.

### 3. Live — between matches (replaces `IdleStateView`)
- **Header row** (bottom border `line`):
  - Left: `● NO ACTIVE MATCH` label, then "WAITING FOR KICKOFF".
  - Right: status items `● STATS API :49124` and `● RANK QUERIES READY`. The dots are green when OK; use loss red when down.
- **LAST MATCH · {relative time}**
  - A strip on `panel`, wrapping, 84px tall: [120px W/L slab "WIN"/"LOSS"] [blue slab 220px "BLUE · YOU" (shrinks)] [84px score tile] [84px score tile] [orange slab 220px] [info area: "RANKED DOUBLES · 5:54" plus the roster line "YOU + AEROPULSE vs VORTEXRL, ZENITH_99"; MMR delta 32px; ghost button "ALL SESSION MATCHES →", which goes to the Session tab].
  - **Box score** below it: two team panels in a 2px-gap grid that stacks below ~1100px (`repeat(auto-fit,minmax(540px,1fr))`). Each panel has a 3px top rule in the slab colour and a header with the team label (`BLUE · WIN`). Columns: `minmax(90px,1fr) minmax(0,.9fr) 52px repeat(6,36px)`, column-gap 8px, for name, rank, MMR, PTS, G, A, SV, SH, D. Your name is in blue-ink. An MVP chip (white bg, dark text, 11px) appears on the winning team's top scorer; this is the same MVP rule as today's `MatchCard`. Zeros are `faint`. Data: the latest `session.matches` entry.
- **Two columns** (1.5fr / 1fr, 28px gap):
  - **THIS SESSION · {uptime}:** a 4-cell KPI strip on `panel`: RECORD (4–1 / "80% WIN RATE"), NET MMR (+31, green), GOAL DIFF (+8 / "16 FOR · 8 AGAINST"), STREAK (W2 / "BEST W2 · HIGH 550").
  - **PLAYLISTS:** one `panel` row per playlist: name 22px, `3–1 · 1465 → 1485`, delta 30px green/red.

### 4. Session (replaces `SessionHistoryView` + `MatchCard`)
- Header: "STARTED 1H 40M AGO · 5 MATCHES" / "SESSION". On the right, a ghost "RESET SESSION" button (hover turns border and text loss red; keep the existing confirm()).
- KPI strip: the same 4 KPIs as idle, plus a 5th wider cell listing playlists (name, record, current MMR, delta).
- "MATCHES · NEWEST FIRST" label, with a segmented control on the right: ALL / WINS / LOSSES. The active segment is `ink` bg with dark text; inactive is transparent with `muted` text.
- **Match row** (72px, `panel`, hover `panel-hover`, click opens the match detail):
  - Grid: `72px 120px 1fr repeat(4,64px) 90px 90px`.
  - [W/L slab, full height] [score as *your* goals – theirs, coloured by team side, 38px] [playlist 18px + duration 14px + MVP chip / roster line "You + X vs Y, Z"] [G] [A] [SV] [PTS] [MMR delta ±N] [relative time].
- Search and sort from today's toolbar are dropped from the main view. If kept, put them inline with the segmented control in the same style as the History search input.

### 5. History (replaces `HistoricalGamesView` + `HistoricalMatchCard`)
- Header: "LOCAL STORE · REPLAYS + BALLCHASING" / "HISTORY". Primary button "SYNC FROM BALLCHASING" (bg `ink`, dark text, 800/15px; hover bg blue-ink). While syncing, show the label "SYNCING · N MATCHES" and disable the button.
- **Filter row:**
  - Search input (`panel` bg, 1px `line` border, 15px/600 uppercase placeholder in `faint`).
  - Playlist segmented control ALL / 1V1 / 2V2 / 3V3.
  - Result segmented control ALL / WINS / LOSSES.
- **Row** (64px): grid `72px 120px 1.2fr 1fr 90px 150px 140px`.
  - Columns: [W/L slab] [blue–orange score] [playlist + arena] [player names, ellipsis] [length + red "OT" tag] [replay status] [date].
  - Replay status labels:
    - `BALLCHASING ↗` (white): uploaded; links to `ballchasing_url`.
    - `LOCAL ONLY` (muted): downloaded but not uploaded.
    - `DOWNLOADING…` (muted): pending.
    - `FAILED · RETRY` (red): failed.
- **Expand:** clicking a row that has parsed stats toggles an inline box score, in place of `HistoricalMatchDetailModal` (the modal can stay for deep detail). Two team panels in a 2px-gap grid; columns `minmax(90px,1fr) repeat(6,36px)`, gap 8px.
- Pagination: simple "PREV / PAGE N OF M / NEXT" ghost controls under the list (not shown in the mock).

### 6. Players (replaces `PlayerDirectoryView` + `PlayerCard`)
- Header: "EVERYONE YOU'VE PLAYED WITH OR AGAINST" / "PLAYERS".
- **Three spotlight cards** (`panel`, 4px top rule): BEST TEAMMATE (green, synergy %), MOST FACED (neutral, W–L), TOUGHEST RIVAL (red, W–L). These map to today's `bestMate`, `topRival`, and a new "lowest opponent win rate with ≥2 games".
- **Filter row:** search input, then a role segmented control: ALL / FREQUENT / TEAMMATES / OPPONENTS / SYNERGY ≥50%. Filter logic is the same as today's `RoleFilter`. A platform filter (from `PlayerSearchBar`) can be added as another segmented group.
- **Row** (60px, click opens the profile): grid `minmax(140px,1.4fr) 90px 1fr 64px 130px 130px 56px 100px`.
  - Columns: name (22px uppercase), platform, rank (ellipsis), MMR, WITH YOU [5 cells + W–L], VS YOU [5 cells + W–L], games, last seen.
  - "—" in `faint` when there are no games in that role.
- The trusted ally / tough rival / frequent badges are replaced by the record colour and the spotlight cards.

### 7. Player Profile dialog (replaces `PlayerProfileModal`)
- Backdrop `scrim`, fixed and scrollable, with the panel aligned to the top (56px offset), max-width 980px. Panel bg `modal` with a 4px `ink` top rule.
- **Header:**
  - Label "PLAYER PROFILE · STEAM", then the name (italic 64px), then the meta line: full `player_id` (`ink-2`), `COPY ID` (turns into `COPIED ✓`), `RL TRACKER ↗` (same URL builder as today; Steam uses the Steam64 ID).
  - Top-right: ghost button `ESC ✕`.
- **H2H blocks:** 2 columns with a 2px gap.
  - Each block has the label `WITH YOU · N GAMES` / `VS YOU · N GAMES` and an optional tag chip (solid fill in the record colour).
    - Teammate tags: STRONG SYNERGY ≥60%, BALANCED ≥40%, STRUGGLED.
    - Opponent tags: DOMINANT, EVEN RIVALRY, TOUGH OPPONENT.
    - Tags show only when there are ≥2 games.
  - Record at 64px, win % at 30px `muted`, then a 10-cell bar (flex, 14px tall, 3px gap).
- **RANKS:** a grid of `auto-fill minmax(200px,1fr)` cards: playlist label, rank (22px uppercase), MMR. Data from `ranks_json`.
- **BY PLAYLIST:** column headers, then 52px rows with playlist, with you (W–L + %), vs you (W–L + %), games, last met. Data from `matchups[]`.
- **Footer:** `FIRST SEEN {date}` · `LAST SEEN {relative}`, 13px `muted-2`, with a 1px top border.
- **Closing:** Esc, the ✕ button, or a click on the backdrop. The click handler stops propagation inside the panel. Keep the native `<dialog>` from `common/Modal.tsx` and restyle it.

---

## Interactions & state
- `activeTab`: 'live' | 'session' | 'history' | 'players' (existing `DashboardTab`; drop the 'history' alias).
- Live shows in-match when `match.active_match`, otherwise idle. This is automatic; the prototype's `liveState` prop only simulates it.
- Segmented filters are local component state (`outcomeFilter`, `playlistFilter`, `roleFilter`).
- History expand: `openMatchGuid: string | null`. Only one row is open at a time; rows without stats aren't clickable.
- Profile: `selectedPlayerId` (existing). Fetches `/api/players/:id` as today.
- Hover: rows go `panel` → `panel-hover`; ghost buttons go to white text and border; the primary button turns blue-ink. No transitions are needed beyond an optional 120ms colour fade.
- Loading and empty states: reuse the page-title layout with a single `muted` 15px line (e.g. "NO MATCHES THIS SESSION YET"). No spinners with glow.
- Responsive:
  - Grids use `minmax(0,…)` tracks and ellipsis on names.
  - The scorebug slabs shrink (`flex:1 1 300px; max-width:300px; min-width:0`).
  - The last-match strip wraps, and its box-score panels stack below ~1100px.
  - Designed for 1280–1920 widths, and checked at ~900px.

## Assets
No images or icons. The only glyphs are the text characters ● ↗ → ✕ ✓ –. Fonts: Barlow Condensed and Barlow from Google Fonts (add them to `index.html` or self-host; extend Tailwind `fontFamily`). lucide-react is no longer needed by these views.

## Files
- `RL Sync Broadcast.dc.html`: the full interactive prototype (all tabs, idle/in-match, profile dialog). Markup is inline-styled. Sample data and derived display logic live in the `class Component` script at the bottom: leaders, record colours, cells, MVP, filters.
- `support.js`: runtime needed to open the prototype locally.
- `reference/Current Dashboard.dc.html`: recreation of the current Live view, for comparison.

Source files this replaces in `rl-api-utils/web/src/components`: `layout/Header.tsx`, `layout/Navbar.tsx`, `live/ScoreboardBanner.tsx`, `live/RosterTable.tsx`, `live/PlayerRow.tsx`, `live/IdleStateView.tsx`, `session/SessionHistoryView.tsx`, `session/MatchCard.tsx`, `historical/HistoricalGamesView.tsx`, `historical/HistoricalMatchCard.tsx`, `players/PlayerDirectoryView.tsx`, `players/PlayerCard.tsx`, `players/PlayerProfileModal.tsx`, `common/RankBadge.tsx`, `common/H2HBadge.tsx`. Not redesigned: `overlay/OBSOverlayView.tsx`, `live/ColumnConfigModal.tsx`, `session/MatchDetailModal.tsx`, `historical/HistoricalMatchDetailModal.tsx`. Restyle these with the tokens above.
