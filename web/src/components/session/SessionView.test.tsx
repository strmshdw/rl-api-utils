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

  it('renders the header, KPI strip and playlists cell', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    expect(container.textContent).toContain('STARTED 1H 40M AGO · 5 MATCHES');
    const kpis = container.querySelector('[data-testid="session-kpis"]')!;
    expect(kpis.querySelector('[data-kpi="record"]')!.textContent).toContain('4–1');
    expect(kpis.querySelector('[data-kpi="record"]')!.textContent).toContain('80% WIN RATE');
    expect(kpis.querySelector('[data-kpi="goals"]')!.textContent).toContain('16 FOR · 8 AGAINST');
    expect(kpis.textContent).toContain('RANKED DOUBLES');
    expect(kpis.textContent).toContain('1485');
    expect(kpis.textContent).toContain('+29');
  });

  it('lists matches newest first with your score on the left in your team colour', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    const all = rows(container);
    expect(all).toHaveLength(5);
    expect(all[0].textContent).toContain('9 MIN');
    expect(all[4].textContent).toContain('1H');
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

  it('filters by result with the segmented control', async () => {
    const { container } = await renderSession(SAMPLE_SESSION_RESPONSE);
    await clickText(container, 'LOSSES');
    expect(rows(container)).toHaveLength(1);
    expect(rows(container)[0].textContent).toMatch(/^L/);
    await clickText(container, 'WINS');
    expect(rows(container)).toHaveLength(4);
    await clickText(container, 'ALL');
    expect(rows(container)).toHaveLength(5);
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

  it('renders the continue previous session banner and invokes continue / dismiss handlers', async () => {
    const onContinue = vi.fn();
    const onDismiss = vi.fn();
    const prevSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'prev-session-123',
      total_matches: 3,
      total_wins: 2,
      total_losses: 1,
      last_match_at: new Date(Date.now() - 25 * 60 * 1000).toISOString(),
    };
    const currentSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'curr-session-456',
      matches: [],
      total_matches: 0,
      total_wins: 0,
      total_losses: 0,
      continuable_session: prevSession,
    };

    const { container } = await renderSession(currentSession, {
      onContinueSession: onContinue,
      onDismissContinueSession: onDismiss,
    });

    const banner = container.querySelector('[data-testid="continue-session-banner"]')!;
    expect(banner).not.toBeNull();
    expect(banner.textContent).toContain('2W–1L · 3 MATCHES');
    expect(banner.textContent).toContain('LAST PLAYED 25 MIN AGO');

    await clickText(banner, 'CONTINUE SESSION');
    expect(onContinue).toHaveBeenCalledWith('prev-session-123');

    await clickText(banner, 'START NEW');
    expect(onDismiss).toHaveBeenCalledWith('prev-session-123');
  });

  it('displays session history over time and lets the user inspect or continue past sessions', async () => {
    const onContinue = vi.fn();
    const pastSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'past-session-001',
      started_at: '2026-10-16T18:00:00Z',
      last_match_at: '2026-10-16T19:00:00Z',
      total_matches: 2,
      total_wins: 2,
      total_losses: 0,
      can_continue: true,
      matches: SAMPLE_SESSION_RESPONSE.matches.slice(0, 2),
    };
    const currentSession: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'curr-session-002',
    };

    const { container } = await renderSession(currentSession, {
      pastSessions: [currentSession, pastSession],
      onContinueSession: onContinue,
    });

    const historySection = container.querySelector('[data-testid="session-history-section"]')!;
    expect(historySection).not.toBeNull();
    const histRows = historySection.querySelectorAll<HTMLElement>('[data-testid="session-history-row"]');
    expect(histRows).toHaveLength(2);
    expect(histRows[0].textContent).toContain('CURRENT');
    expect(histRows[1].textContent).toContain('CONTINUABLE');

    // Click the past session row to inspect its matches and KPIs
    await act(async () => histRows[1].click());
    expect(container.querySelector('[data-testid="historical-session-banner"]')).not.toBeNull();
    expect(rows(container)).toHaveLength(2);

    // Return to current session
    await clickText(container, '← CURRENT SESSION');
    expect(container.querySelector('[data-testid="historical-session-banner"]')).toBeNull();
    expect(rows(container)).toHaveLength(5);

    // Click CONTINUE directly on the history row
    const continueBtn = histRows[1].querySelector<HTMLElement>('[data-testid="continue-history-row-btn"]')!;
    await act(async () => continueBtn.click());
    expect(onContinue).toHaveBeenCalledWith('past-session-001');
  });
});
