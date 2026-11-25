// @vitest-environment happy-dom
import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { act } from 'react';
import App from './App';
import { useLiveMatch } from './hooks/useLiveMatch';
import { useSession } from './hooks/useSession';
import { CurrentMatchResponse } from './types/api';
import { SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from './utils/sampleData';
import { cleanup, clickText, mount } from './test/dom';

vi.mock('./hooks/useLiveMatch', () => ({ useLiveMatch: vi.fn() }));
vi.mock('./hooks/useSession', () => ({ useSession: vi.fn() }));

function setHooks(match: CurrentMatchResponse | null) {
  vi.mocked(useLiveMatch).mockReturnValue({ match, status: 'connected', lastUpdated: null, error: null });
  vi.mocked(useSession).mockReturnValue({
    session: SAMPLE_SESSION_RESPONSE,
    pastSessions: [],
    isLoading: false,
    error: null,
    refreshSession: vi.fn(),
    refreshPastSessions: vi.fn(),
    resetSession: vi.fn(),
    continueSession: vi.fn(),
    dismissContinueSession: vi.fn(),
    selectedMatch: null,
    setSelectedMatch: vi.fn(),
  });
}

const nav = (root: ParentNode) => root.querySelector('nav[aria-label="Dashboard"]')!;

describe('App shell', () => {
  beforeEach(() => {
    localStorage.clear();
    window.history.replaceState(null, '', '/');
    globalThis.fetch = vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve({ matches: [], total: 0, players: [] }) })) as unknown as typeof fetch;
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('renders a clean single top bar without version badge or telemetry desk banner, and removes the live filter bar', async () => {
    setHooks(null);
    const { container } = await mount(<App />);
    const header = container.querySelector('[data-testid="app-header"]')!;
    expect(nav(container).textContent).toBe('LIVESESSION4HISTORYPLAYERS');
    expect(header.textContent).not.toContain('SESSION 4–1');
    expect(header.textContent).not.toContain('ALL MODES');
    expect(header.textContent).not.toContain('v2.0.0');
    expect(header.querySelector('#btn-mode-all')).toBeNull();
    expect(header.textContent).toContain('DOUBLES 1485 +29 · 3–1');
    expect(header.querySelector('a[href="/?mode=overlay"]')?.textContent).toBe('OVERLAY ↗');
    expect(container.querySelector('footer')).toBeNull();
    expect(header.textContent).not.toContain('COLUMNS');
    expect(container.textContent).not.toContain('RL/SYNC TELEMETRY DESK');
    expect(container.textContent).not.toContain('25S POST-WHISTLE MMR BURST POLLING');
    expect(container.querySelector('[data-testid="live-filter-bar"]')).toBeNull();
  });

  it('marks the active tab and switches views', async () => {
    setHooks(null);
    const { container } = await mount(<App />);
    expect(nav(container).querySelector('[aria-current="page"]')?.textContent).toBe('LIVE');
    expect(container.querySelector('[data-testid="live-idle"]')).not.toBeNull();

    await clickText(nav(container), 'SESSION');
    expect(nav(container).querySelector('[aria-current="page"]')?.textContent).toBe('SESSION4');
    expect(container.querySelector('[data-testid="session-view"]')).not.toBeNull();

    await clickText(nav(container), 'HISTORY');
    expect(container.querySelector('[data-testid="history-view"]')).not.toBeNull();

    await clickText(nav(container), 'PLAYERS');
    expect(container.querySelector('[data-testid="players-view"]')).not.toBeNull();
  });

  it('jumps from the last-match strip to the Session tab', async () => {
    setHooks(null);
    const { container } = await mount(<App />);
    await clickText(container, 'ALL SESSION MATCHES →');
    expect(container.querySelector('[data-testid="session-view"]')).not.toBeNull();
  });

  it('shows the live dot in red with zero live filter bar and opens live card field columns from Settings', async () => {
    setHooks(SAMPLE_CURRENT_MATCH);
    const { container } = await mount(<App />);
    const live = nav(container).querySelector('button')!;
    expect(live.innerHTML).toContain('bg-loss');
    const liveMatchEl = container.querySelector('[data-testid="live-match"]')!;
    expect(liveMatchEl).not.toBeNull();
    expect(container.querySelector('[data-testid="live-filter-bar"]')).toBeNull();
    expect(container.querySelector('[data-testid="app-header"]')!.textContent).not.toContain('COLUMNS');

    await act(async () => {
      container.querySelector<HTMLButtonElement>('#btn-settings')!.click();
    });
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="settings-live-columns-btn"]')!.click();
    });
    const dialog = container.querySelector('dialog')!;
    expect(dialog.textContent).toContain('COLUMNS');
    expect(dialog.querySelectorAll('input[type="checkbox"]')).toHaveLength(10);
    await clickText(dialog, 'DONE');
    expect(container.querySelector('dialog')).toBeNull();
  });

  it('hides theme and configuration complexity behind the Settings gear button and supports all 14 design styles, dark/light mode, and text scale', async () => {
    setHooks(null);
    const { container } = await mount(<App />);

    // Settings panel is closed by default so header remains clean
    expect(container.querySelector('#theme-select')).toBeNull();
    const settingsBtn = container.querySelector<HTMLButtonElement>('#btn-settings')!;
    expect(settingsBtn).not.toBeNull();
    settingsBtn.click();
    await new Promise((r) => setTimeout(r, 10));

    const lightBtn = container.querySelector<HTMLButtonElement>('#btn-color-light')!;
    lightBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.getAttribute('data-mode')).toBe('light');

    const scaleBtn = container.querySelector<HTMLButtonElement>('#btn-scale-xl')!;
    expect(scaleBtn.textContent).toContain('TEXT: XL (+15%)');
    scaleBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.style.fontSize).toBe('18.4px');

    const themeSelect = container.querySelector<HTMLSelectElement>('#theme-select')!;
    themeSelect.value = 'oled';
    themeSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.getAttribute('data-style')).toBe('oled');
    expect(document.documentElement.style.getPropertyValue('--t-on-slab')).toBe('#ffffff');

    const darkBtn = container.querySelector<HTMLButtonElement>('#btn-color-dark')!;
    darkBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.style.getPropertyValue('--t-page')).toBe('#000000');
    expect(document.documentElement.style.getPropertyValue('--t-blue-slab')).toBe('#163878');
    expect(document.documentElement.style.getPropertyValue('--t-orange-slab')).toBe('#a33f14');

    themeSelect.value = 'classic';
    themeSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.getAttribute('data-style')).toBe('classic');
    expect(document.documentElement.style.getPropertyValue('--t-page')).toBe('#0a0b0d');
    expect(document.documentElement.style.getPropertyValue('--t-panel')).toBe('#121418');
    expect(document.documentElement.style.getPropertyValue('--t-blue-slab')).toBe('#12598c');
    expect(document.documentElement.style.getPropertyValue('--t-orange-slab')).toBe('#b85214');
    expect(document.documentElement.style.getPropertyValue('--t-blue-ink')).toBe('#30aff8');
    expect(document.documentElement.style.getPropertyValue('--t-orange-ink')).toBe('#f67f2f');

    themeSelect.value = 'classicOled';
    themeSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.getAttribute('data-style')).toBe('classicOled');
    expect(document.documentElement.getAttribute('data-mode')).toBe('dark');
    expect(document.documentElement.style.getPropertyValue('--t-page')).toBe('#000000');
    expect(document.documentElement.style.getPropertyValue('--t-plaque')).toBe('#000000');
    expect(document.documentElement.style.getPropertyValue('--t-card-header')).toBe('#12151c');
    expect(document.documentElement.style.getPropertyValue('--t-blue-ink')).toBe('#30aff8');
    expect(document.documentElement.style.getPropertyValue('--t-orange-ink')).toBe('#f67f2f');

    themeSelect.value = 'arena';
    themeSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.style.getPropertyValue('--t-blur')).toBe('blur(12px)');

    // Player Platform Display preference toggles between Icons (default) and Name
    const platIconBtn = container.querySelector<HTMLButtonElement>('[data-testid="platform-display-icon"]')!;
    const platNameBtn = container.querySelector<HTMLButtonElement>('[data-testid="platform-display-name"]')!;
    expect(platIconBtn.getAttribute('aria-pressed')).toBe('true');
    expect(platNameBtn.getAttribute('aria-pressed')).toBe('false');
    expect(container.querySelectorAll('[data-testid="platform-badge"][data-platform-display="icon"] svg').length).toBeGreaterThan(0);

    await act(async () => {
      platNameBtn.click();
    });
    expect(platIconBtn.getAttribute('aria-pressed')).toBe('false');
    expect(platNameBtn.getAttribute('aria-pressed')).toBe('true');
    expect(localStorage.getItem('rlsync_platform_display')).toBe('name');
    expect(container.querySelectorAll('[data-testid="platform-badge"][data-platform-display="name"]').length).toBeGreaterThan(0);
    expect(container.querySelectorAll('[data-testid="platform-badge"][data-platform-display="icon"]').length).toBe(0);

    await act(async () => {
      platIconBtn.click();
    });
    expect(localStorage.getItem('rlsync_platform_display')).toBe('icon');
    expect(container.querySelectorAll('[data-testid="platform-badge"][data-platform-display="icon"] svg').length).toBeGreaterThan(0);
  });

  it('prevents accidental modal light-dismiss when pointerdown starts inside the modal panel and releases on backdrop', async () => {
    setHooks(SAMPLE_CURRENT_MATCH);
    const { container } = await mount(<App />);
    await act(async () => {
      container.querySelector<HTMLButtonElement>('#btn-settings')!.click();
    });
    const dialog = container.querySelector('dialog')!;
    expect(dialog).not.toBeNull();
    const backdropWrapper = dialog.firstElementChild as HTMLElement;
    const panel = backdropWrapper.firstElementChild as HTMLElement;

    // Simulate pointerdown inside panel followed by pointerup and click on backdropWrapper
    panel.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    backdropWrapper.dispatchEvent(new PointerEvent('pointerup', { bubbles: true }));
    backdropWrapper.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('dialog')).not.toBeNull();

    // Simulate genuine outside pointerdown + pointerup on backdropWrapper
    backdropWrapper.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    backdropWrapper.dispatchEvent(new PointerEvent('pointerup', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('dialog')).toBeNull();
  });

  it('toggles AVG/G vs TOTALS, IN-MATCH vs IDLE VIEW preview, custom calendar range, and Replay Limit Banner', async () => {
    setHooks(null);
    const { container } = await mount(<App />);

    // Open Settings panel to access dev/debug preview and buffer controls
    const settingsBtn = container.querySelector<HTMLButtonElement>('#btn-settings')!;
    settingsBtn.click();
    await new Promise((r) => setTimeout(r, 10));

    // Toggle IN-MATCH preview when no live match is running
    const matchBtn = container.querySelector<HTMLButtonElement>('#btn-mode-match')!;
    matchBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('[data-testid="live-match"]')).not.toBeNull();
    expect(container.querySelectorAll('[data-testid="live-player-hist-split"]').length).toBeGreaterThan(0);

    // Settings option hides and restores historical stats in live view and persists choice
    const hideLiveHistBtn = container.querySelector<HTMLButtonElement>('[data-testid="settings-live-hist-hide"]')!;
    const showLiveHistBtn = container.querySelector<HTMLButtonElement>('[data-testid="settings-live-hist-show"]')!;
    expect(showLiveHistBtn.getAttribute('aria-pressed')).toBe('true');
    await act(async () => {
      hideLiveHistBtn.click();
    });
    expect(hideLiveHistBtn.getAttribute('aria-pressed')).toBe('true');
    expect(localStorage.getItem('rlsync_show_live_hist_data')).toBe('false');
    expect(container.querySelectorAll('[data-testid="live-player-hist-split"]').length).toBe(0);

    await act(async () => {
      showLiveHistBtn.click();
    });
    expect(showLiveHistBtn.getAttribute('aria-pressed')).toBe('true');
    expect(localStorage.getItem('rlsync_show_live_hist_data')).toBe('true');
    expect(container.querySelectorAll('[data-testid="live-player-hist-split"]').length).toBeGreaterThan(0);

    // Toggle back to IDLE VIEW (no live filter bar rendered)
    const idleBtn = container.querySelector<HTMLButtonElement>('#btn-mode-idle')!;
    idleBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('[data-testid="live-idle"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="live-filter-bar"]')).toBeNull();

    // Switch to SESSION to toggle AVG / G vs TOTALS locally
    await clickText(nav(container), 'SESSION');
    const totalBtn = container.querySelector<HTMLButtonElement>('[data-testid="session-filter-bar"] [data-stat-mode="total"]')!;
    await act(async () => {
      totalBtn.click();
    });
    expect(container.textContent).toContain('SHOWING CUMULATIVE TOTALS');

    // Switch to HISTORY to toggle local Custom Calendar Range bar
    await clickText(nav(container), 'HISTORY');
    const calBtn = container.querySelector<HTMLButtonElement>('[data-testid="history-cal-toggle"]')!;
    await act(async () => {
      calBtn.click();
    });
    const calBar = container.querySelector('[data-testid="custom-calendar-bar"]')!;
    expect(calBar).not.toBeNull();
    expect(container.querySelector('[data-testid="custom-date-from"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="custom-date-to"]')).not.toBeNull();

    // Replay Limit Banner states (20-game hosted replay buffer guard)
    const limitSelect = container.querySelector<HTMLSelectElement>('#limit-state-select')!;
    limitSelect.value = 'warn';
    limitSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    const banner = container.querySelector('#replay-limit-banner')!;
    expect(banner.textContent).toContain('REPLAY LIMIT WARNING');
    expect(banner.textContent).toContain('16 / 20 UNDOWNLOADED REPLAYS IN ROCKET LEAGUE QUEUE');

    // Configure threshold & toggle Auto-Sync at Threshold inside banner
    const thresholdSelect = banner.querySelector<HTMLSelectElement>('#banner-threshold-select')!;
    thresholdSelect.value = '12';
    thresholdSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));

    const autoSyncBtn = banner.querySelector<HTMLButtonElement>('[data-testid="banner-autosync-toggle"]')!;
    expect(autoSyncBtn.textContent).toContain('AUTO-SYNC AT 12G: OFF');
    autoSyncBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(autoSyncBtn.textContent).toContain('AUTO-SYNC AT 12G: ON');

    limitSelect.value = 'error';
    limitSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(banner.textContent).toContain('REPLAY HISTORY LIMIT EXCEEDED');
    expect(banner.textContent).toContain('20 / 20 UNDOWNLOADED REPLAYS');

    // Dismiss banner on demand even in exceeded state, and restore via SHOW SYNC BANNER inside Settings
    const dismissBtn = banner.querySelector<HTMLButtonElement>('[data-testid="banner-dismiss-btn"]')!;
    expect(dismissBtn).not.toBeNull();
    dismissBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('#replay-limit-banner')).toBeNull();

    const restoreBtn = container.querySelector<HTMLButtonElement>('[data-testid="restore-sync-banner-btn"]')!;
    expect(restoreBtn).not.toBeNull();
    restoreBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('#replay-limit-banner')).not.toBeNull();
  });

  it('pushes browser history states on tab/player navigation, restores state on popstate, and supports viewing own profile', async () => {
    setHooks(null);
    const { container } = await mount(<App />);

    expect(window.location.pathname).toBe('/');
    expect(nav(container).querySelector('[aria-current="page"]')?.textContent).toBe('LIVE');

    // Switch to SESSION tab -> pushes /session
    await clickText(nav(container), 'SESSION');
    expect(window.location.pathname).toBe('/session');
    expect(document.title).toContain('Session | RL/SYNC');

    // Switch to PLAYERS tab -> pushes /players
    await clickText(nav(container), 'PLAYERS');
    expect(window.location.pathname).toBe('/players');
    expect(container.querySelector('[data-testid="players-view"]')).not.toBeNull();

    // Click VIEW MY PROFILE -> pushes /players/<local-id> and renders PlayerProfilePage
    const ownProfileBtn = container.querySelector<HTMLButtonElement>('[data-testid="view-own-profile-btn"]')!;
    expect(ownProfileBtn).not.toBeNull();
    await act(async () => {
      ownProfileBtn.click();
    });
    expect(window.location.pathname).toContain('/players/');
    expect(container.querySelector('[data-testid="player-profile-page"]')).not.toBeNull();

    // Simulate browser Back via popstate to /players
    const playersNavState = {
      rlsyncNav: true,
      depth: 2,
      tab: 'players' as const,
      player: null,
      modalPlayer: null,
    };
    await act(async () => {
      window.history.replaceState(playersNavState, '', '/players');
      window.dispatchEvent(new PopStateEvent('popstate', { state: playersNavState }));
    });
    expect(container.querySelector('[data-testid="player-profile-page"]')).toBeNull();
    expect(container.querySelector('[data-testid="players-view"]')).not.toBeNull();

    // Simulate browser Back via popstate to /session
    const sessionNavState = {
      rlsyncNav: true,
      depth: 1,
      tab: 'session' as const,
      player: null,
      modalPlayer: null,
    };
    await act(async () => {
      window.history.replaceState(sessionNavState, '', '/session');
      window.dispatchEvent(new PopStateEvent('popstate', { state: sessionNavState }));
    });
    expect(container.querySelector('[data-testid="session-view"]')).not.toBeNull();
  });

  it('removes universal filter banner from TopBar, renders zero filters on LIVE, orders mode filters 1V1/2V2/3V3/EXTRA/CASUAL, and keeps SESSION/HISTORY/PLAYERS independent', async () => {
    setHooks(null);
    const { container } = await mount(<App />);

    // TopBar is single-tier with zero universal filter banner controls and LIVE has zero filter bar
    const header = container.querySelector('[data-testid="app-header"]')!;
    expect(header.querySelector('#context-filter-bar')).toBeNull();
    expect(header.querySelector('[role="toolbar"]')).toBeNull();
    expect(container.querySelector('[data-testid="live-filter-bar"]')).toBeNull();

    // Switch to SESSION -> mode filters are ordered 1V1, 2V2, 3V3, EXTRA, CASUAL and default to 2V2
    await clickText(nav(container), 'SESSION');
    const sessionBar = container.querySelector<HTMLElement>('[data-testid="session-filter-bar"]')!;
    expect(sessionBar).not.toBeNull();
    expect(sessionBar.querySelector('[data-testid="session-date-range"]')).toBeNull();
    const sessionModeOrder = Array.from(sessionBar.querySelectorAll<HTMLButtonElement>('[data-mode-btn]')).map(
      (b) => b.getAttribute('data-mode-btn'),
    );
    expect(sessionModeOrder).toEqual(['10', '11', '13', 'extra', 'casual']);
    expect(sessionBar.querySelector('[data-mode-btn="11"]')?.getAttribute('aria-pressed')).toBe('true');
    expect(header.textContent).toContain('DOUBLES 1485 +29 · 3–1');

    // Switch SESSION mode to 1V1 -> header reflects SESSION 1V1 (0–0)
    await clickText(sessionBar, '1V1');
    expect(nav(container).textContent).toBe('LIVESESSIONHISTORYPLAYERS');
    expect(header.textContent).toContain('DUEL');
    expect(header.textContent).toContain('0–0');
    expect(header.textContent).not.toContain('DOUBLES 1485');

    // EXTRA ▾ menu opens, supports Escape to close, and exposes only concrete individual Extra modes (no ALL EXTRA MODES)
    const extraBtn = sessionBar.querySelector<HTMLButtonElement>('[data-mode-btn="extra"]')!;
    expect(extraBtn.getAttribute('aria-haspopup')).toBe('menu');
    expect(extraBtn.getAttribute('aria-expanded')).toBe('false');

    await act(async () => {
      extraBtn.click();
    });
    expect(extraBtn.getAttribute('aria-expanded')).toBe('true');
    const extraMenu = sessionBar.querySelector<HTMLElement>('[role="menu"]')!;
    expect(extraMenu).not.toBeNull();
    expect(extraMenu.textContent).toContain('HOOPS');
    expect(extraMenu.textContent).toContain('RUMBLE');
    expect(extraMenu.textContent).toContain('DROPSHOT');
    expect(extraMenu.textContent).toContain('SNOW DAY');
    expect(extraMenu.textContent).toContain('TOURNAMENTS');
    expect(extraMenu.textContent).not.toContain('ALL EXTRA');

    await act(async () => {
      extraMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(extraBtn.getAttribute('aria-expanded')).toBe('false');

    await act(async () => {
      extraBtn.click();
    });
    await clickText(sessionBar.querySelector('[role="menu"]')!, 'HOOPS');
    expect(extraBtn.getAttribute('aria-expanded')).toBe('false');
    expect(extraBtn.textContent).toContain('HOOPS');

    // Navigate to HISTORY: has its own Mode + Timeframe bar (starts on 2V2, NOT Hoops), and hides AVG/G | TOTALS
    await clickText(nav(container), 'HISTORY');
    const historyBar = container.querySelector<HTMLElement>('[data-testid="history-filter-bar"]')!;
    expect(historyBar).not.toBeNull();
    expect(historyBar.querySelector('[data-mode-btn="11"]')?.getAttribute('aria-pressed')).toBe('true');
    expect(historyBar.querySelector('[data-testid="history-date-range"]')).not.toBeNull();
    expect(historyBar.querySelector('[data-stat-mode="avg"]')).toBeNull();
    expect(historyBar.querySelector('[data-stat-mode="total"]')).toBeNull();

    // Navigate to PLAYERS: has its own Mode + Timeframe + AVG/TOTALS bar (independent of HISTORY/SESSION)
    await clickText(nav(container), 'PLAYERS');
    const playersBar = container.querySelector<HTMLElement>('[data-testid="players-filter-bar"]')!;
    expect(playersBar).not.toBeNull();
    expect(playersBar.querySelector('[data-mode-btn="11"]')?.getAttribute('aria-pressed')).toBe('true');
    expect(playersBar.querySelector('[data-testid="players-date-range"]')).not.toBeNull();
    expect(playersBar.querySelector('[data-stat-mode="avg"]')).not.toBeNull();

    // Returning to LIVE still renders no filter bar and derives the most recent session mode automatically
    await clickText(nav(container), 'LIVE');
    expect(container.querySelector('[data-testid="live-filter-bar"]')).toBeNull();
    expect(header.textContent).toContain('DOUBLES 1485 +29 · 3–1');

    // SettingsPanel exposes LIVE VIEW HISTORICAL STATS, CONFIGURE LIVE FIELDS, Sync Banner & OBS Overlay
    const settingsBtn = container.querySelector<HTMLButtonElement>('#btn-settings')!;
    await act(async () => {
      settingsBtn.click();
    });
    const settingsBody = container.querySelector('#settings-panel-body')!;
    expect(settingsBody.textContent).not.toContain('HISTORICAL STAT AGGREGATION');
    expect(settingsBody.querySelector('[data-testid="settings-live-hist-hide"]')).not.toBeNull();
    expect(settingsBody.querySelector('[data-testid="settings-live-columns-btn"]')).not.toBeNull();
    expect(settingsBody.querySelector('#btn-restore-sync-banner')).not.toBeNull();
    expect(settingsBody.textContent).toContain('OBS OVERLAY ↗');
  });

  it('scopes preview fallback session KPIs and For/Against stats cleanly when a live daemon returns an empty 0-match session', async () => {
    const emptyLiveSession = {
      ...SAMPLE_SESSION_RESPONSE,
      total_matches: 0,
      total_wins: 0,
      total_losses: 0,
      win_rate: 0,
      playlists: {},
      matches: [],
      most_played_playlist: undefined,
      for_stats: undefined,
      against_stats: undefined,
    };
    window.history.replaceState(null, '', '/session?demo=true');
    vi.mocked(useLiveMatch).mockReturnValue({
      match: null,
      status: 'connected',
      lastUpdated: null,
      error: null,
    });
    vi.mocked(useSession).mockReturnValue({
      session: emptyLiveSession,
      unfilteredSession: emptyLiveSession,
      pastSessions: [],
      isLoading: false,
      error: null,
      refreshSession: vi.fn(),
      refreshPastSessions: vi.fn(),
      resetSession: vi.fn(),
      continueSession: vi.fn(),
      dismissContinueSession: vi.fn(),
      selectedMatch: null,
      setSelectedMatch: vi.fn(),
    });

    const { container } = await mount(<App />);
    const sessionView = container.querySelector('[data-testid="session-view"]')!;
    expect(sessionView).not.toBeNull();
    expect(sessionView.textContent).toContain('3–1');
    expect(sessionView.textContent).toContain('75% WIN RATE');
    expect(sessionView.textContent).toContain('+29');
    expect(sessionView.textContent).toContain('11 FOR · 6 AGAINST');
    expect(sessionView.textContent).toContain('4G');
    expect(sessionView.textContent).not.toContain('NO RESULTS YET');
    expect(sessionView.textContent).not.toContain('No matches recorded yet');
    expect(
      container.querySelector('[data-testid="app-header"] a[href="/?mode=overlay&demo=true"]')?.textContent,
    ).toBe('OVERLAY ↗');
  });

  it('shows a SYNC NOW button inside the header session pill and adds a bright red border and warning label when pending replays reach the warning count', async () => {
    const fetchCalls: string[] = [];
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      fetchCalls.push(`${init?.method ?? 'GET'} ${url}`);
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ running: false, synced_matches: 0, total_matches: 0, matches: [], total: 0, players: [] }),
      });
    }) as unknown as typeof fetch;

    // Below threshold (pending 5 < trigger 15): SYNC NOW is visible, no warning border or label
    vi.mocked(useLiveMatch).mockReturnValue({ match: null, status: 'connected', lastUpdated: null, error: null });
    vi.mocked(useSession).mockReturnValue({
      session: { ...SAMPLE_SESSION_RESPONSE, pending_count: 5, trigger_threshold: 15, max_hosted_replays: 20 },
      pastSessions: [],
      isLoading: false,
      error: null,
      refreshSession: vi.fn(),
      refreshPastSessions: vi.fn(),
      resetSession: vi.fn(),
      continueSession: vi.fn(),
      dismissContinueSession: vi.fn(),
      selectedMatch: null,
      setSelectedMatch: vi.fn(),
    });

    const first = await mount(<App />);
    const okPill = first.container.querySelector('[data-testid="header-session-pill"]')!;
    expect(okPill).not.toBeNull();
    expect(okPill.getAttribute('data-sync-warning')).toBe('false');
    expect(okPill.className).not.toContain('border-[#ff3b4e]');
    expect(okPill.querySelector('[data-testid="header-sync-warning"]')).toBeNull();
    expect(okPill.querySelector('[data-testid="header-sync-now-btn"]')?.textContent).toContain('SYNC NOW');
    first.unmount();

    // At/over warning threshold (pending 16 >= trigger 15): bright red border + warning label + SYNC NOW triggers replay sync
    const onRefresh = vi.fn();
    vi.mocked(useSession).mockReturnValue({
      session: { ...SAMPLE_SESSION_RESPONSE, pending_count: 16, trigger_threshold: 15, max_hosted_replays: 20 },
      pastSessions: [],
      isLoading: false,
      error: null,
      refreshSession: onRefresh,
      refreshPastSessions: vi.fn(),
      resetSession: vi.fn(),
      continueSession: vi.fn(),
      dismissContinueSession: vi.fn(),
      selectedMatch: null,
      setSelectedMatch: vi.fn(),
    });

    const warned = await mount(<App />);
    const warnPill = warned.container.querySelector('[data-testid="header-session-pill"]')!;
    expect(warnPill.getAttribute('data-sync-warning')).toBe('true');
    expect(warnPill.className).toContain('border-[#ff3b4e]');
    expect(warnPill.querySelector('[data-testid="header-sync-warning"]')?.textContent).toContain('SYNC WARN 16/20');

    const syncBtn = warnPill.querySelector<HTMLButtonElement>('[data-testid="header-sync-now-btn"]')!;
    await act(async () => {
      syncBtn.click();
    });
    expect(fetchCalls).toContain('POST /api/sync/now');
    expect(fetchCalls).toContain('POST /api/sync/ballchasing');
    expect(warnPill.getAttribute('data-sync-warning')).toBe('false');
    expect(warnPill.querySelector('[data-testid="header-sync-warning"]')).toBeNull();
  });
});


