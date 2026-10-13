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
});
