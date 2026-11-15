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
      latest.dash.setReplayBannerDismissed(true);
      latest.dash.togglePinPlayer('Steam|111|0');
    });

    expect(latest.theme.themeStyle).toBe('tactical');
    expect(latest.theme.colorMode).toBe('light');
    expect(latest.dash.themeStyle).toBe('tactical');
    expect(latest.dash.fontScale).toBe('xl');
    expect(latest.dash.pinnedPlayerIds).toEqual(['Steam|111|0']);
    expect(localStorage.getItem(STORAGE_KEYS.themeStyle)).toBe('tactical');
    expect(localStorage.getItem(STORAGE_KEYS.colorMode)).toBe('light');
    expect(localStorage.getItem(STORAGE_KEYS.fontScale)).toBe('xl');
    expect(localStorage.getItem(STORAGE_KEYS.replayBannerDismissed)).toBe('true');
    expect(localStorage.getItem(STORAGE_KEYS.pinnedPlayers)).toBe('["Steam|111|0"]');
    expect('dateRangeFactor' in latest.dash).toBe(false);
  });

  it('defaults activePlaylist to "all" standalone, "11" inside DashboardProvider, persists to localStorage, and syncs live playlist unless manually overridden in-match', async () => {
    let standalone!: ReturnType<typeof useDashboard>;
    function StandaloneReader() {
      standalone = useDashboard();
      return null;
    }
    const s = await mount(<StandaloneReader />);
    expect(standalone.activePlaylist).toBe('all');
    s.unmount();

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
});
