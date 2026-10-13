// @vitest-environment happy-dom
import { describe, it, expect, afterEach, vi } from 'vitest';
import { act } from 'react';
import { HistoryView } from './HistoryView';
import { historyOutcome, replayStatus } from './historyData';
import { HistoricalMatchRecord, MatchesResponse } from '../../types/api';
import { isDemoMode, isDemoLiveMatch, SAMPLE_HISTORICAL_MATCHES } from '../../utils/sampleData';
import { cleanup, clickText, flush, mount, setSearch } from '../../test/dom';

function record(over: Partial<HistoricalMatchRecord>): HistoricalMatchRecord {
  return {
    match_guid: 'g',
    record_start_timestamp: 0,
    map_name: 'Neo Tokyo',
    playlist: 13,
    replay_url: '',
    download_status: 'DOWNLOADED',
    local_file_path: '',
    upload_status: 'PENDING',
    ballchasing_id: '',
    ballchasing_url: '',
    retry_count: 0,
    last_error: '',
    blue_score: 4,
    orange_score: 2,
    replay_stats_json: '',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    result: 'victory',
    ...over,
  };
}

type FetchHandler = (url: string, init?: RequestInit) => unknown;

function mockFetch(matches: HistoricalMatchRecord[], extra?: FetchHandler) {
  const calls: string[] = [];
  globalThis.fetch = vi.fn((url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${url}`);
    const custom = extra?.(url, init);
    if (custom !== undefined) return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(custom) });
    if (url.startsWith('/api/sync/ballchasing/status')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ running: false, total_replays: 0, synced_matches: 0, synced_players: 0 }) });
    }
    if (url.startsWith('/api/matches')) {
      const body: MatchesResponse = { matches, total: matches.length, limit: 20, offset: 0 };
      return Promise.resolve({ ok: true, json: () => Promise.resolve(body) });
    }
    return Promise.reject(new Error(`Unhandled ${url}`));
  }) as unknown as typeof fetch;
  return calls;
}

const rows = (root: ParentNode) => Array.from(root.querySelectorAll<HTMLElement>('[data-testid="history-row"]'));

describe('demo / preview mode detection', () => {
  afterEach(() => setSearch(''));

  it('accepts demo, test and preview flags', () => {
    for (const q of ['?demo=true', '?test=true', '?preview=true', '?preview=idle']) {
      setSearch(q);
      expect(isDemoMode()).toBe(true);
    }
    setSearch('?preview=false');
    expect(isDemoMode()).toBe(false);
    setSearch('');
    expect(isDemoMode()).toBe(false);
  });

  it('only simulates a live match for preview=true', () => {
    setSearch('?preview=true');
    expect(isDemoLiveMatch()).toBe(true);
    setSearch('?preview=idle');
    expect(isDemoLiveMatch()).toBe(false);
  });
});

describe('history data helpers', () => {
  it('maps replay sync state to the broadcast status labels', () => {
    expect(replayStatus(record({ ballchasing_url: 'https://ballchasing.com/replay/x', upload_status: 'UPLOADED' }))).toMatchObject({
      label: 'BALLCHASING ↗',
      href: 'https://ballchasing.com/replay/x',
    });
    expect(replayStatus(record({ download_status: 'DOWNLOADED', upload_status: 'PENDING' })).label).toBe('LOCAL ONLY');
    expect(replayStatus(record({ download_status: 'PENDING', upload_status: 'PENDING' })).label).toBe('DOWNLOADING…');
    const failed = replayStatus(record({ download_status: 'FAILED', last_error: 'HTTP 404', retry_count: 2 }));
    expect(failed).toMatchObject({ label: 'FAILED', ink: 'text-loss' });
    expect(failed.title).toContain('HTTP 404');
    expect(replayStatus(record({ download_status: 'SKIPPED', upload_status: '' })).label).toBe('NO REPLAY');
  });

  it('orients W/L to the local player when they are found in the replay', () => {
    const m = record({
      blue_score: 4,
      orange_score: 2,
      result: 'victory',
      stats: {
        match_guid: 'g', map_name: '', playlist: 11, blue_score: 4, orange_score: 2, duration: 300, overtime: false, date: '',
        players: [{ name: 'Me', platform: 'Epic', online_id: 'ME_ID', team: 1, score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 }],
      },
    });
    expect(historyOutcome(m, new Set(['me_id']))).toBe('L');
    expect(historyOutcome(m, new Set())).toBe('W');
  });
});

describe('HistoryView', () => {
  afterEach(() => {
    cleanup();
    setSearch('');
    vi.restoreAllMocks();
  });

  it('fetches /api/matches and renders a row per match', async () => {
    const calls = mockFetch([record({ match_guid: 'a', upload_status: 'UPLOADED', ballchasing_url: 'https://ballchasing.com/replay/a' })]);
    const { container } = await mount(<HistoryView />);
    expect(calls.some((c) => c.startsWith('GET /api/matches?limit=20&offset=0'))).toBe(true);
    const [row] = rows(container);
    expect(row.textContent).toContain('RANKED STANDARD');
    expect(row.textContent).toContain('Neo Tokyo');
    expect(row.querySelector('a[href="https://ballchasing.com/replay/a"]')?.textContent).toBe('BALLCHASING ↗');
  });

  it('sends the playlist filter to the API and resets to page 1', async () => {
    const calls = mockFetch([record({})]);
    const { container } = await mount(<HistoryView />);
    await clickText(container, '2V2');
    expect(calls.some((c) => c.includes('playlist=11'))).toBe(true);
    await clickText(container, '1V1');
    expect(calls.some((c) => c.includes('playlist=10'))).toBe(true);
  });

  it('filters by result on the client', async () => {
    mockFetch([record({ match_guid: 'w', blue_score: 3, orange_score: 1 }), record({ match_guid: 'l', blue_score: 0, orange_score: 2, result: 'defeat' })]);
    const { container } = await mount(<HistoryView />);
    expect(rows(container)).toHaveLength(2);
    await clickText(container, 'LOSSES');
    expect(rows(container).map((r) => r.dataset.guid)).toEqual(['l']);
  });

  it('expands one row at a time, and only rows with parsed stats', async () => {
    const [withStats] = SAMPLE_HISTORICAL_MATCHES;
    const second = { ...SAMPLE_HISTORICAL_MATCHES[1] };
    mockFetch([withStats, second, record({ match_guid: 'nostats' })]);
    const { container } = await mount(<HistoryView localPlayerIds={new Set(['demo_local_player'])} />);
    const toggle = (guid: string) => rows(container).find((r) => r.dataset.guid === guid)!.firstElementChild as HTMLElement;

    expect(toggle('nostats').getAttribute('role')).toBeNull();
    await act(async () => toggle(withStats.match_guid).click());
    expect(container.querySelectorAll('[data-testid="history-box-score"]')).toHaveLength(1);
    const box = container.querySelector('[data-testid="history-box-score"]')!;
    expect(box.querySelector('.text-blue-ink')?.textContent).toBe('You');

    await act(async () => toggle(second.match_guid).click());
    const open = container.querySelectorAll('[data-testid="history-box-score"]');
    expect(open).toHaveLength(1);
    expect(open[0].closest('[data-testid="history-row"]')?.getAttribute('data-guid')).toBe(second.match_guid);

    await act(async () => toggle(second.match_guid).click());
    expect(container.querySelectorAll('[data-testid="history-box-score"]')).toHaveLength(0);
  });

  it('triggers a Ballchasing sync and disables the button while it runs', async () => {
    let started = false;
    const calls = mockFetch([], (url, init) => {
      if (url === '/api/sync/ballchasing' && init?.method === 'POST') {
        started = true;
        return { status: 'started' };
      }
      if (url.startsWith('/api/sync/ballchasing/status')) {
        return { running: started, total_replays: 50, synced_matches: 10, synced_players: 25 };
      }
      return undefined;
    });
    const { container } = await mount(<HistoryView />);
    await clickText(container, 'SYNC FROM BALLCHASING');
    expect(calls).toContain('POST /api/sync/ballchasing');
    await flush(1100);
    const button = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.startsWith('SYNCING'))!;
    expect(button.textContent).toBe('SYNCING · 10 MATCHES');
    expect(button.disabled).toBe(true);
  });

  it('falls back to demo matches in preview mode when the store is empty', async () => {
    setSearch('?preview=true');
    mockFetch([]);
    const { container } = await mount(<HistoryView />);
    const text = container.textContent || '';
    expect(text).toContain('DFH Stadium');
    expect(text).toContain('Champions Field');
    expect(text).toContain('Utopia Coliseum');
    expect(text).not.toContain('SIMULATED PREVIEW');
  });

  it('shows an empty state on the page-title layout', async () => {
    mockFetch([]);
    const { container } = await mount(<HistoryView />);
    expect(container.textContent).toContain('NO MATCHES IN THE LOCAL STORE YET');
  });
});
