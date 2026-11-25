// @vitest-environment happy-dom
import { describe, it, expect, afterEach, vi } from 'vitest';
import { act } from 'react';
import { SessionView } from './SessionView';
import { SessionMatchDetail, SessionResponse } from '../../types/api';
import { SAMPLE_SESSION_RESPONSE } from '../../utils/sampleData';
import { cleanup, clickText, mount } from '../../test/dom';

const rows = (root: ParentNode) => Array.from(root.querySelectorAll<HTMLElement>('[data-testid="session-match-row"]'));

async function renderSession(session: SessionResponse | null, extra: Partial<React.ComponentProps<typeof SessionView>> = {}) {
  return mount(<SessionView session={session} isLoading={false} error={null} onResetSession={() => {}} {...extra} />);
}

describe('SessionView', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('renders the header, mode-scoped KPI strip (with subgrid alignment and non-truncated headers) and playlists cell', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    expect(container.textContent).toContain('STARTED 1H 40M AGO · 4 MATCHES');
    const kpis = container.querySelector('[data-testid="session-kpis"]')!;
    expect(kpis.className).toContain('lg:grid-rows-[auto_auto_auto]');
    expect(kpis.querySelector('[data-kpi="record"]')!.className).toContain('lg:grid-rows-subgrid');
    expect(kpis.querySelector('[data-kpi="record"]')!.textContent).toContain('3–1');
    expect(kpis.querySelector('[data-kpi="record"]')!.textContent).toContain('75% WIN RATE');
    expect(kpis.querySelector('[data-kpi="goals"]')!.textContent).toContain('11 FOR · 6 AGAINST');
    expect(kpis.textContent).toContain('RANKED DOUBLES');
    expect(kpis.textContent).not.toContain('RANKED STANDARD');
    expect(kpis.textContent).toContain('1485');
    expect(kpis.textContent).toContain('+29');
  });

  it('lists mode-scoped matches newest first with your score on the left in your team colour', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    const all = rows(container);
    expect(all).toHaveLength(4);
    expect(all[0].textContent).toContain('9 MIN');
    expect(all[3].textContent).toContain('1H');
    // sample-match-003: local on orange, lost 2–3 (blue 3, orange 2)
    const loss = all[2];
    expect(loss.textContent).toMatch(/^L2–3/);
    expect(loss.querySelector('.text-orange-ink')?.textContent).toBe('2');
  });

  it('shows the MVP chip only when you were MVP', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    const [first, ...rest] = rows(container);
    expect(first.textContent).toContain('MVP');
    expect(first.textContent).toContain('You + AeroPulse vs VortexRL, Zenith_99');
    rest.forEach((r) => expect(r.textContent).not.toContain('MVP'));
  });

  it('does not award MVP to you on a defeat, even with the match-high score', async () => {
    const lost: SessionMatchDetail = {
      ...SAMPLE_SESSION_RESPONSE.matches[0],
      match_guid: 'lost',
      result: 'defeat',
      winner_team: 1,
      blue_score: 1,
      orange_score: 2,
    };
    const { container } = await renderSession({ ...SAMPLE_SESSION_RESPONSE, matches: [lost] });
    expect(rows(container)[0].textContent).not.toContain('MVP');
  });

  it('filters by result with the segmented control within the active game mode', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    await clickText(container, 'LOSSES');
    expect(rows(container)).toHaveLength(1);
    expect(rows(container)[0].textContent).toMatch(/^L/);
    await clickText(container, 'WINS');
    expect(rows(container)).toHaveLength(3);
    await clickText(container, 'ALL');
    expect(rows(container)).toHaveLength(4);
  });

  it('opens the match detail dialog when a row is clicked, and closes it with ESC ✕', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    await act(async () => rows(container)[0].click());
    const dialog = container.querySelector('dialog')!;
    expect(dialog).not.toBeNull();
    expect(dialog.textContent).toContain('SESSION MATCH · 9 MIN AGO');
    expect(dialog.querySelector('[data-testid="box-score-blue"]')!.textContent).toContain('MVP');
    expect(dialog.textContent).toContain('sample-match-001');
    await clickText(dialog, 'ESC ✕');
    expect(container.querySelector('dialog')).toBeNull();
  });

  it('asks before resetting the session', async () => {
    const onReset = vi.fn();
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE, { onResetSession: onReset });
    await clickText(container, 'RESET SESSION');
    expect(onReset).not.toHaveBeenCalled();
    await clickText(container, 'RESET SESSION');
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(onReset).toHaveBeenCalledTimes(1);
  });

  it('shows empty and loading states on the page-title layout', async () => {
    const empty = await renderSession({ ...SAMPLE_SESSION_RESPONSE, matches: [], total_wins: 0, total_losses: 0 });
    expect(empty.container.textContent).toContain('NO MATCHES THIS SESSION YET');
    const loading = await mount(<SessionView session={null} isLoading error={null} onResetSession={() => {}} />);
    expect(loading.container.textContent).toContain('LOADING SESSION…');
  });

  it('renders the mode-scoped continue previous session banner and hides it when the selected mode had 0 games', async () => {
    const onContinue = vi.fn();
    const onDismiss = vi.fn();
    const prevSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'prev-session-123',
      last_match_at: new Date(Date.now() - 25 * 60 * 1000).toISOString(),
    };
    const currentSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'curr-session-456',
      matches: [],
      playlists: {},
      total_matches: 0,
      total_wins: 0,
      total_losses: 0,
      continuable_session: prevSession,
    };

    const mounted = await renderSession(currentSession, {
      activePlaylist: '11',
      onContinueSession: onContinue,
      onDismissContinueSession: onDismiss,
    });
    const { container } = mounted;

    const banner = container.querySelector('[data-testid="continue-session-banner"]')!;
    expect(banner).not.toBeNull();
    expect(banner.textContent).toContain('3W–1L · 4 MATCHES');
    expect(banner.textContent).toContain('LAST PLAYED 25 MIN AGO');

    await clickText(banner, 'CONTINUE SESSION');
    expect(onContinue).toHaveBeenCalledWith('prev-session-123');

    await clickText(banner, 'START NEW');
    expect(onDismiss).toHaveBeenCalledWith('prev-session-123');

    // Switching to Hoops ('27', 0 matches in prevSession) hides the continuable banner cleanly
    await mounted.rerender(
      <SessionView
        session={currentSession}
        activePlaylist="27"
        isLoading={false}
        error={null}
        onResetSession={() => {}}
        onContinueSession={onContinue}
        onDismissContinueSession={onDismiss}
      />,
    );
    expect(container.querySelector('[data-testid="continue-session-banner"]')).toBeNull();
  });

  it('displays mode-scoped session history over time and hides past sessions with 0 matches in the active mode', async () => {
    const onContinue = vi.fn();
    const pastSession2v2: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'past-session-001',
      started_at: '2026-10-16T18:00:00Z',
      last_match_at: '2026-10-16T19:00:00Z',
      total_matches: 2,
      total_wins: 2,
      total_losses: 0,
      can_continue: true,
      playlists: { '11': SAMPLE_SESSION_RESPONSE.playlists['11'] },
      matches: SAMPLE_SESSION_RESPONSE.matches.slice(0, 2),
    };
    const pastSession3v3Only: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'past-session-3v3-only',
      started_at: '2026-10-15T18:00:00Z',
      last_match_at: '2026-10-15T19:00:00Z',
      total_matches: 1,
      total_wins: 1,
      total_losses: 0,
      playlists: { '13': SAMPLE_SESSION_RESPONSE.playlists['13'] },
      matches: [SAMPLE_SESSION_RESPONSE.matches[3]],
    };
    const currentSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'curr-session-002',
    };

    const { container } = await renderSession(currentSession, {
      activePlaylist: '11',
      pastSessions: [currentSession, pastSession2v2, pastSession3v3Only],
      onContinueSession: onContinue,
    });

    const historySection = container.querySelector('[data-testid="session-history-section"]')!;
    expect(historySection).not.toBeNull();
    // pastSession3v3Only has 0 matches in 2v2 ('11'), so only currentSession and pastSession2v2 appear
    const histRows = historySection.querySelectorAll<HTMLElement>('[data-testid="session-history-row"]');
    expect(histRows).toHaveLength(2);
    expect(histRows[0].textContent).toContain('3–1');
    expect(histRows[0].textContent).toContain('4 MATCHES');
    expect(histRows[0].textContent).toContain('CURRENT');
    expect(histRows[1].textContent).toContain('CONTINUABLE');

    // Click the past session row to inspect its matches and KPIs
    await act(async () => histRows[1].click());
    expect(container.querySelector('[data-testid="historical-session-banner"]')).not.toBeNull();
    expect(rows(container)).toHaveLength(2);

    // Return to current session (4 matches in 2v2)
    await clickText(container, '← CURRENT SESSION');
    expect(container.querySelector('[data-testid="historical-session-banner"]')).toBeNull();
    expect(rows(container)).toHaveLength(4);

    // Click CONTINUE directly on the history row
    const continueBtn = histRows[1].querySelector<HTMLElement>('[data-testid="continue-history-row-btn"]')!;
    await act(async () => continueBtn.click());
    expect(onContinue).toHaveBeenCalledWith('past-session-001');
  });

  it('syncs server trigger config after initial null mount, uses ONLY /api/sync/now (never Ballchasing history), and unpins RL BUFFER when new matches arrive', async () => {
    const fetchCalls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        fetchCalls.push(`${init?.method ?? 'GET'} ${url}`);
        if (url === '/api/sync/now' && init?.method === 'POST') {
          return {
            ok: true,
            status: 200,
            json: async () => ({ status: 'sync_triggered', running: false, pending_count: 0 }),
          } as Response;
        }
        if (url === '/api/sync/now/status') {
          return {
            ok: true,
            status: 200,
            json: async () => ({ running: false, discovered_count: 7, downloaded_count: 7, uploaded_count: 7, pending_count: 0 }),
          } as Response;
        }
        return { ok: true, status: 200, json: async () => ({}) } as Response;
      }),
    );

    const mounted = await mount(
      <SessionView session={null} isLoading error={null} onResetSession={() => {}} />,
    );

    const loadedSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'live-session-buffer',
      pending_count: 7,
      trigger_threshold: 12,
      force_sync_on_trigger: true,
    };
    await mounted.rerender(
      <SessionView session={loadedSession} isLoading={false} error={null} onResetSession={() => {}} />,
    );

    const bufferBar = mounted.container.querySelector('[data-testid="session-replay-buffer-bar"]')!;
    const thresholdSelect = mounted.container.querySelector<HTMLSelectElement>('#session-threshold-select')!;
    expect(thresholdSelect.value).toBe('12');
    expect(bufferBar.textContent).toContain('AUTO-SYNC @12G: ON');
    expect(bufferBar.textContent).toContain('RL BUFFER: 7/20 UNSYNCED');

    // Trigger manual sync: calls ONLY POST /api/sync/now (never /api/sync/ballchasing) and optimistically clears to 0/20
    const syncBtn = mounted.container.querySelector<HTMLButtonElement>('[data-testid="session-sync-btn"]')!;
    await act(async () => {
      syncBtn.click();
    });
    expect(fetchCalls).toContain('POST /api/sync/now');
    expect(fetchCalls.some((c) => c.includes('/api/sync/ballchasing'))).toBe(false);
    expect(bufferBar.textContent).toContain('RL BUFFER: 0/20 UNSYNCED');

    // When new matches arrive and pending_count changes (even back to 7 later), RL BUFFER is never stuck at 0
    await mounted.rerender(
      <SessionView
        session={{ ...loadedSession, pending_count: 2 }}
        isLoading={false}
        error={null}
        onResetSession={() => {}}
      />,
    );
    expect(bufferBar.textContent).toContain('RL BUFFER: 2/20 UNSYNCED');

    await mounted.rerender(
      <SessionView
        session={{ ...loadedSession, pending_count: 7 }}
        isLoading={false}
        error={null}
        onResetSession={() => {}}
      />,
    );
    expect(bufferBar.textContent).toContain('RL BUFFER: 7/20 UNSYNCED');
  });

  it('renders SessionModeBreakdownStrip, removes duplicate stat-mode button, and scopes KPIs, For/Against, and match rows per mode', async () => {
    const onSelectPlaylist = vi.fn();
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE, { onSelectPlaylist });

    const strip = container.querySelector('[data-testid="session-mode-breakdown"]')!;
    expect(strip).not.toBeNull();
    expect(strip.textContent).toContain('2V2 DOUBLES');
    expect(strip.textContent).toContain('3V3 STANDARD');
    expect(strip.textContent).not.toContain('ALL SESSION');

    // SessionForAgainstTable preserves badge without duplicate SWITCH TO TOTALS button
    const splitTable = container.querySelector('[data-testid="session-for-against-table"]')!;
    expect(splitTable.textContent).toContain('SHOWING PER-GAME AVERAGES');
    expect(splitTable.textContent).not.toContain('SWITCH TO');

    // Click 2V2 DOUBLES in breakdown strip -> scopes KPIs (3–1, +29, +5), For/Against (4G), and match rows (4)
    await clickText(strip, '2V2 DOUBLES');
    expect(onSelectPlaylist).toHaveBeenCalledWith('11');
    const kpis = container.querySelector('[data-testid="session-kpis"]')!;
    expect(kpis.querySelector('[data-kpi="record"]')!.textContent).toContain('3–1');
    expect(kpis.querySelector('[data-kpi="mmr"]')!.textContent).toContain('+29');
    expect(kpis.querySelector('[data-kpi="goals"]')!.textContent).toContain('11 FOR · 6 AGAINST');
    expect(splitTable.textContent).toContain('4G');
    expect(rows(container)).toHaveLength(4);

    // Click 3V3 STANDARD in breakdown strip -> scopes KPIs (1–0, +11, +3), For/Against (1G), and match rows (1)
    await clickText(strip, '3V3 STANDARD');
    expect(onSelectPlaylist).toHaveBeenCalledWith('13');
    expect(kpis.querySelector('[data-kpi="record"]')!.textContent).toContain('1–0');
    expect(kpis.querySelector('[data-kpi="mmr"]')!.textContent).toContain('+11');
    expect(kpis.querySelector('[data-kpi="goals"]')!.textContent).toContain('5 FOR · 2 AGAINST');
    expect(splitTable.textContent).toContain('1G');
    expect(rows(container)).toHaveLength(1);
  });
});
