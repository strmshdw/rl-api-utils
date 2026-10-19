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
});
