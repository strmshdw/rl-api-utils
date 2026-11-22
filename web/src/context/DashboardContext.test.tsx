// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, flush, mount, setSearch } from '../test/dom';
import { DashboardProvider, useDashboard } from './DashboardContext';
import { useTheme } from './ThemeContext';
import { STORAGE_KEYS } from './storage';
import { useSession } from '../hooks/useSession';
import { SAMPLE_SESSION_RESPONSE } from '../utils/sampleData';

function Harness({
  onReady,
}: {
  onReady: (ctx: { dash: ReturnType<typeof useDashboard>; theme: ReturnType<typeof useTheme> }) => void;
}) {
  const dash = useDashboard();
  const theme = useTheme();
  onReady({ dash, theme });
  return <div data-testid="theme">{theme.themeStyle}</div>;
}

describe('DashboardContext + ThemeContext composition', () => {
  afterEach(() => {
    cleanup();
    localStorage.clear();
    vi.restoreAllMocks();
    setSearch('');
  });

  it('shares ThemeProvider state through both useTheme and useDashboard and persists storage keys', async () => {
    let latest!: { dash: ReturnType<typeof useDashboard>; theme: ReturnType<typeof useTheme> };
    await mount(
      <DashboardProvider>
        <Harness
          onReady={(v) => {
            latest = v;
          }}
        />
      </DashboardProvider>,
    );

    act(() => {
      latest.dash.setTheme('tactical', 'light');
      latest.dash.setFontScale('xl');
      latest.dash.setPlatformDisplay('name');
      latest.dash.setShowLiveHistData(false);
      latest.dash.setReplayBannerDismissed(true);
      latest.dash.togglePinPlayer('Steam|111|0');
    });

    expect(latest.theme.themeStyle).toBe('tactical');
    expect(latest.theme.colorMode).toBe('light');
    expect(latest.dash.themeStyle).toBe('tactical');
    expect(latest.dash.fontScale).toBe('xl');
    expect(latest.theme.platformDisplay).toBe('name');
    expect(latest.dash.platformDisplay).toBe('name');
    expect(latest.dash.showLiveHistData).toBe(false);
    expect(latest.dash.pinnedPlayerIds).toEqual(['Steam|111|0']);
    expect(localStorage.getItem(STORAGE_KEYS.themeStyle)).toBe('tactical');
    expect(localStorage.getItem(STORAGE_KEYS.colorMode)).toBe('light');
    expect(localStorage.getItem(STORAGE_KEYS.fontScale)).toBe('xl');
    expect(localStorage.getItem(STORAGE_KEYS.platformDisplay)).toBe('name');
    expect(localStorage.getItem(STORAGE_KEYS.showLiveHistData)).toBe('false');
    expect(localStorage.getItem(STORAGE_KEYS.replayBannerDismissed)).toBe('true');
    expect(localStorage.getItem(STORAGE_KEYS.pinnedPlayers)).toBe('["Steam|111|0"]');
    expect('dateRangeFactor' in latest.dash).toBe(false);
  });

  it('defaults activePlaylist to "11" standalone and inside DashboardProvider, normalizes legacy cross-mode keys, and syncs live playlist unless manually overridden in-match', async () => {
    let standalone!: ReturnType<typeof useDashboard>;
    function StandaloneReader() {
      standalone = useDashboard();
      return null;
    }
    const s = await mount(<StandaloneReader />);
    expect(standalone.activePlaylist).toBe('11');
    s.unmount();

    localStorage.setItem('rlsync_active_playlist', 'all');
    let latest!: { dash: ReturnType<typeof useDashboard>; theme: ReturnType<typeof useTheme> };
    await mount(
      <DashboardProvider>
        <Harness
          onReady={(v) => {
            latest = v;
          }}
        />
      </DashboardProvider>,
    );

    expect(latest.dash.activePlaylist).toBe('11');

    act(() => {
      latest.dash.setActivePlaylist('extra');
    });
    expect(latest.dash.activePlaylist).toBe('27');
    expect(localStorage.getItem('rlsync_active_playlist')).toBe('27');

    // Syncing a live 3v3 match updates activePlaylist to '13'
    act(() => {
      latest.dash.syncLivePlaylist(13);
    });
    expect(latest.dash.activePlaylist).toBe('13');
    expect(localStorage.getItem('rlsync_active_playlist')).toBe('13');

    // User manually switches to '10' during the 3v3 match
    act(() => {
      latest.dash.setActivePlaylist('10');
    });
    expect(latest.dash.activePlaylist).toBe('10');
    expect(localStorage.getItem('rlsync_active_playlist')).toBe('10');

    // Subsequent SSE tick for the same live match (13) does not clobber manual override
    act(() => {
      latest.dash.syncLivePlaylist(13);
    });
    expect(latest.dash.activePlaylist).toBe('10');

    // Transitioning to a new live match (Hoops 27) or after idle syncs to the new match mode
    act(() => {
      latest.dash.syncLivePlaylist(null);
      latest.dash.syncLivePlaylist(27);
    });
    expect(latest.dash.activePlaylist).toBe('27');
  });

  it('useSession scopes synchronously on filter changes and ignores stale out-of-order fetch responses', async () => {
    let resolveDelayed2v2: ((res: Response) => void) | null = null;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url === '/api/session?playlist=11' && !resolveDelayed2v2) {
          return new Promise<Response>((resolve) => {
            resolveDelayed2v2 = resolve;
          });
        }
        if (url.startsWith('/api/session')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => SAMPLE_SESSION_RESPONSE,
          } as Response);
        }
        return Promise.resolve({ ok: true, status: 200, json: async () => ({ sessions: [] }) } as Response);
      }),
    );

    let hookState!: ReturnType<typeof useSession>;
    let setFilterFn!: (f: string) => void;
    function DynamicSessionReader() {
      const [filter, setFilter] = React.useState('13');
      setFilterFn = setFilter;
      hookState = useSession(filter);
      return null;
    }
    const React = await import('react');

    await mount(
      <DashboardProvider>
        <DynamicSessionReader />
      </DashboardProvider>,
    );
    await flush(10);

    expect(hookState.unfilteredSession?.total_matches).toBe(5);
    expect(hookState.session?.total_matches).toBe(1);

    // Switch to '11' (whose network response is delayed), then immediately to '27' (0 matches in sample)
    act(() => {
      setFilterFn('11');
    });
    // Synchronous client-side scoping from unfilteredSession immediately yields 4 matches for '11'
    expect(hookState.session?.total_matches).toBe(4);

    act(() => {
      setFilterFn('27');
    });
    // Synchronous client-side scoping immediately yields 0 matches for Hoops ('27')
    expect(hookState.session?.total_matches).toBe(0);
    await flush(10);

    // Resolve the stale '11' response AFTER '27' is active -> must NOT overwrite '27' with 4 matches!
    await act(async () => {
      resolveDelayed2v2?.({
        ok: true,
        status: 200,
        json: async () => SAMPLE_SESSION_RESPONSE,
      } as Response);
    });
    await flush(10);
    expect(hookState.session?.total_matches).toBe(0);
  });

  it('useSession supports zero-arg and playlist-filtered queries while preserving unfilteredSession', async () => {
    const requestedUrls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        requestedUrls.push(url);
        if (url.startsWith('/api/session')) {
          return {
            ok: true,
            status: 200,
            json: async () => SAMPLE_SESSION_RESPONSE,
          } as Response;
        }
        return { ok: true, status: 200, json: async () => ({ sessions: [] }) } as Response;
      }),
    );

    let hookState!: ReturnType<typeof useSession>;
    function SessionReader({ filter }: { filter?: string }) {
      hookState = useSession(filter);
      return null;
    }

    await mount(
      <DashboardProvider>
        <SessionReader filter="2v2" />
      </DashboardProvider>,
    );
    await flush(10);

    expect(requestedUrls).toContain('/api/session');
    expect(requestedUrls).toContain('/api/session?playlist=11');
    expect(hookState.unfilteredSession?.total_matches).toBe(5);
    expect(hookState.session?.total_matches).toBe(4);
    expect(hookState.session?.total_wins).toBe(3);
    expect(hookState.session?.total_losses).toBe(1);
    expect(Object.keys(hookState.session?.playlists ?? {})).toEqual(['11']);
  });

  it('keeps area filters (live, session, history, players) completely independent and scopes syncLivePlaylist to live only', async () => {
    const { useAreaFilters } = await import('./DashboardContext');
    let live!: ReturnType<typeof useAreaFilters>;
    let session!: ReturnType<typeof useAreaFilters>;
    let history!: ReturnType<typeof useAreaFilters>;
    let players!: ReturnType<typeof useAreaFilters>;
    let syncLive!: (id: number | null) => void;

    function MultiAreaHarness() {
      const { syncLivePlaylist } = useDashboard();
      syncLive = syncLivePlaylist;
      live = useAreaFilters('live');
      session = useAreaFilters('session');
      history = useAreaFilters('history');
      players = useAreaFilters('players');
      return null;
    }

    await mount(
      <DashboardProvider>
        <MultiAreaHarness />
      </DashboardProvider>,
    );

    act(() => {
      history.setPlaylist('13');
      history.setDateRange('7d');
      session.setPlaylist('10');
      session.setStatMode('total');
      players.setPlaylist('27');
      players.setDateRange('30d');
    });

    expect(live.playlist).toBe('11');
    expect(live.dateRange).toBe('all');
    expect(live.statMode).toBe('avg');
    expect(session.playlist).toBe('10');
    expect(session.statMode).toBe('total');
    expect(history.playlist).toBe('13');
    expect(history.dateRange).toBe('7d');
    expect(players.playlist).toBe('27');
    expect(players.dateRange).toBe('30d');

    // Syncing a live Rumble match (28) updates ONLY live, never clobbering session/history/players
    act(() => {
      syncLive(28);
    });
    expect(live.playlist).toBe('28');
    expect(session.playlist).toBe('10');
    expect(history.playlist).toBe('13');
    expect(players.playlist).toBe('27');
  });
});
