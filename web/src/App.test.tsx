// @vitest-environment happy-dom
import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
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
    globalThis.fetch = vi.fn(() => Promise.resolve({ ok: true, json: () => Promise.resolve({ matches: [], total: 0, players: [] }) })) as unknown as typeof fetch;
  });
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('renders a single top bar with tabs, session record, playlist MMR and overlay link', async () => {
    setHooks(null);
    const { container } = await mount(<App />);
    const header = container.querySelector('[data-testid="app-header"]')!;
    expect(nav(container).textContent).toBe('LIVESESSION5HISTORYPLAYERS');
    expect(header.textContent).toContain('SESSION 4–1');
    expect(header.textContent).toContain('DOUBLES 1485 +29');
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

  it('supports 12 design styles, dark/light mode toggle, and anti-squint text scale toggle', async () => {
    setHooks(null);
    const { container } = await mount(<App />);

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
    expect(document.documentElement.style.getPropertyValue('--t-on-slab')).toBe('#000000');

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
    expect(calBar.className).toContain('flex');
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
  });
});

