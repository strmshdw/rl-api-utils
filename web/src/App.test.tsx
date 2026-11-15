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

  it('renders a single top bar with tabs, active-mode session record, playlist MMR and overlay link', async () => {
    setHooks(null);
    const { container } = await mount(<App />);
    const header = container.querySelector('[data-testid="app-header"]')!;
    expect(nav(container).textContent).toBe('LIVESESSION5HISTORYPLAYERS');
    expect(header.textContent).not.toContain('SESSION 4–1');
    expect(header.textContent).toContain('DOUBLES 1485 +29 · 3–1');
    expect(header.querySelector('a[href="/?mode=overlay"]')?.textContent).toBe('OVERLAY ↗');
    expect(container.querySelector('footer')).toBeNull();
    expect(header.textContent).not.toContain('COLUMNS');
  });

  it('marks the active tab and switches views', async () => {
    setHooks(null);
    const { container } = await mount(<App />);
    expect(nav(container).querySelector('[aria-current="page"]')?.textContent).toBe('LIVE');
    expect(container.querySelector('[data-testid="live-idle"]')).not.toBeNull();

    await clickText(nav(container), 'SESSION');
    expect(nav(container).querySelector('[aria-current="page"]')?.textContent).toBe('SESSION5');
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

  it('shows the live dot in red and a COLUMNS button during a match', async () => {
    setHooks(SAMPLE_CURRENT_MATCH);
    const { container } = await mount(<App />);
    const live = nav(container).querySelector('button')!;
    expect(live.innerHTML).toContain('bg-loss');
    expect(container.querySelector('[data-testid="live-match"]')).not.toBeNull();

    await clickText(container.querySelector('[data-testid="app-header"]')!, 'COLUMNS');
    const dialog = container.querySelector('dialog')!;
    expect(dialog.textContent).toContain('COLUMNS');
    expect(dialog.querySelectorAll('input[type="checkbox"]')).toHaveLength(10);
    await clickText(dialog, 'DONE');
    expect(container.querySelector('dialog')).toBeNull();

    await clickText(nav(container), 'HISTORY');
    expect(container.querySelector('[data-testid="app-header"]')!.textContent).not.toContain('COLUMNS');
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

    themeSelect.value = 'arena';
    themeSelect.dispatchEvent(new Event('change', { bubbles: true }));
    await new Promise((r) => setTimeout(r, 10));
    expect(document.documentElement.style.getPropertyValue('--t-blur')).toBe('blur(12px)');
  });

  it('prevents accidental modal light-dismiss when pointerdown starts inside the modal panel and releases on backdrop', async () => {
    setHooks(SAMPLE_CURRENT_MATCH);
    const { container } = await mount(<App />);
    await clickText(container.querySelector('[data-testid="app-header"]')!, 'COLUMNS');
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

    // Toggle back to IDLE VIEW
    const idleBtn = container.querySelector<HTMLButtonElement>('#btn-mode-idle')!;
    idleBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(container.querySelector('[data-testid="live-idle"]')).not.toBeNull();

    // Toggle AVG / G vs TOTALS
    const totalBtn = container.querySelector<HTMLButtonElement>('#btn-stat-total')!;
    totalBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    expect(container.textContent).toContain('SHOWING CUMULATIVE TOTALS');

    // Toggle Custom Calendar Range bar
    const calBtn = container.querySelector<HTMLButtonElement>('#btn-cal-toggle')!;
    calBtn.click();
    await new Promise((r) => setTimeout(r, 10));
    const calBar = container.querySelector('#custom-calendar-bar')!;
    expect(calBar).not.toBeNull();
    expect(container.querySelector('#custom-date-from')).not.toBeNull();
    expect(container.querySelector('#custom-date-to')).not.toBeNull();

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
    ownProfileBtn.click();
    await new Promise((r) => setTimeout(r, 20));
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
    window.history.replaceState(playersNavState, '', '/players');
    window.dispatchEvent(new PopStateEvent('popstate', { state: playersNavState }));
    await new Promise((r) => setTimeout(r, 20));
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
    window.history.replaceState(sessionNavState, '', '/session');
    window.dispatchEvent(new PopStateEvent('popstate', { state: sessionNavState }));
    await new Promise((r) => setTimeout(r, 20));
    expect(container.querySelector('[data-testid="session-view"]')).not.toBeNull();
  });

  it('renders Tier 2 ContextFilterBar with mode switching, EXTRA menu Escape/select, contextual AVG/TOTALS visibility, and de-triplicated SettingsPanel', async () => {
    setHooks(null);
    const { container } = await mount(<App />);

    const header = container.querySelector('[data-testid="app-header"]')!;
    const toolbar = header.querySelector<HTMLElement>('#context-filter-bar[role="toolbar"][aria-label="Analytics context filters"]')!;
    expect(toolbar).not.toBeNull();

    // AVG / G and TOTALS are visible on LIVE / SESSION / PLAYERS, hidden on HISTORY
    expect(toolbar.querySelector('#btn-stat-avg')).not.toBeNull();
    expect(toolbar.querySelector('#btn-stat-total')).not.toBeNull();

    // Switch mode from 2V2 to 3V3 in ContextFilterBar: updates header MMR pill to single-mode 3V3 without cross-mode 4–1
    await clickText(toolbar, '3V3');
    expect(nav(container).textContent).toBe('LIVESESSION5HISTORYPLAYERS');
    expect(header.textContent).not.toContain('SESSION 4–1');
    expect(header.textContent).toContain('STANDARD 1421 +11 · 1–0');

    // Switch mode to 1V1 (0 session games): header status pill shows DUEL · 0–0 instead of leaking DOUBLES 1485
    await clickText(toolbar, '1V1');
    expect(header.textContent).toContain('DUEL');
    expect(header.textContent).toContain('0–0');
    expect(header.textContent).not.toContain('DOUBLES 1485');

    // EXTRA ▾ menu opens, supports Escape to close, and selects an Extra mode
    const extraBtn = toolbar.querySelector<HTMLButtonElement>('#btn-mode-extra')!;
    expect(extraBtn).not.toBeNull();
    expect(extraBtn.getAttribute('aria-haspopup')).toBe('menu');
    expect(extraBtn.getAttribute('aria-expanded')).toBe('false');

    await act(async () => {
      extraBtn.click();
    });
    expect(extraBtn.getAttribute('aria-expanded')).toBe('true');
    const extraMenu = toolbar.querySelector<HTMLElement>('[role="menu"]')!;
    expect(extraMenu).not.toBeNull();
    expect(extraMenu.textContent).toContain('HOOPS');
    expect(extraMenu.textContent).toContain('RUMBLE');
    expect(extraMenu.textContent).toContain('DROPSHOT');
    expect(extraMenu.textContent).toContain('SNOW DAY');
    expect(extraMenu.textContent).toContain('TOURNAMENTS');

    // Escape closes EXTRA menu
    await act(async () => {
      extraMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(extraBtn.getAttribute('aria-expanded')).toBe('false');

    // Re-open EXTRA menu and select HOOPS
    await act(async () => {
      extraBtn.click();
    });
    await clickText(toolbar.querySelector('[role="menu"]')!, 'HOOPS');
    expect(extraBtn.getAttribute('aria-expanded')).toBe('false');
    expect(extraBtn.textContent).toContain('HOOPS');

    // Navigate to HISTORY: AVG/G | TOTALS hides and duplicate playlist segmented bar is suppressed
    await clickText(nav(container), 'HISTORY');
    expect(toolbar.querySelector('#btn-stat-avg')).toBeNull();
    expect(toolbar.querySelector('#btn-stat-total')).toBeNull();
    expect(container.querySelector('[data-testid="history-view"] [aria-label="Filter by playlist"]')).toBeNull();

    // SettingsPanel no longer duplicates HISTORICAL STAT AGGREGATION or COLUMNS
    const settingsBtn = container.querySelector<HTMLButtonElement>('#btn-settings')!;
    await act(async () => {
      settingsBtn.click();
    });
    const settingsBody = container.querySelector('#settings-panel-body')!;
    expect(settingsBody.textContent).not.toContain('HISTORICAL STAT AGGREGATION');
    expect(settingsBody.textContent).not.toContain('COLUMNS');
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
});


