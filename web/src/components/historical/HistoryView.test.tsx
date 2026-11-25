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
    expect(historyOutcome(m, new Set(['Steam|ME_ID|0']))).toBe('L');
    expect(historyOutcome(m, new Set())).toBe('W');
  });

  it('excludes bots with online_id "0" from matching canonical Steam|...|0 IDs and normalizes /api/replays/ URLs', () => {
    const m = record({
      blue_score: 1,
      orange_score: 3,
      ballchasing_url: 'https://ballchasing.com/api/replays/abc-123',
      upload_status: 'UPLOADED',
      result: 'defeat',
      stats: {
        match_guid: 'g2', map_name: '', playlist: 11, blue_score: 1, orange_score: 3, duration: 300, overtime: false, date: '',
        players: [
          { name: 'Bot', platform: 'Unknown', online_id: '0', is_bot: true, team: 1, score: 100, goals: 1, assists: 0, saves: 0, shots: 1, demos: 0 },
          { name: 'LocalUser', platform: 'Steam', online_id: '76561198816594023', team: 0, score: 400, goals: 1, assists: 0, saves: 2, shots: 3, demos: 1 },
        ],
      },
    });
    expect(historyOutcome(m, new Set(['Steam|76561198816594023|0']))).toBe('L');
    expect(replayStatus(m).href).toBe('https://ballchasing.com/replay/abc-123');
  });
});

describe('HistoryView', () => {
  afterEach(() => {
    cleanup();
    setSearch('');
    vi.restoreAllMocks();
  });

  it('fetches /api/matches scoped to default 2v2 mode and renders a row per match without ALL MODES or blended EXTRA pills', async () => {
    const calls = mockFetch([record({ match_guid: 'a', playlist: 11, upload_status: 'UPLOADED', ballchasing_url: 'https://ballchasing.com/replay/a' })]);
    const { container } = await mount(<HistoryView />);
    expect(calls.some((c) => c.startsWith('GET /api/matches?limit=20&offset=0&playlist=11'))).toBe(true);
    const playlistFilterGroup = container.querySelector('[aria-label="Game mode filter"]')!;
    const labels = Array.from(playlistFilterGroup.querySelectorAll('button')).map((b) => b.textContent?.trim());
    expect(labels).not.toContain('ALL MODES');
    expect(labels).toContain('2V2');
    expect(labels).toContain('3V3');
    expect(labels).toContain('1V1');
    const extraBtn = playlistFilterGroup.querySelector<HTMLButtonElement>('[data-mode-btn="extra"]')!;
    await act(async () => extraBtn.click());
    const extraMenu = playlistFilterGroup.querySelector('[role="menu"]')!;
    expect(extraMenu.textContent).toContain('HOOPS');
    expect(extraMenu.textContent).not.toContain('ALL EXTRA');
    expect(container.querySelector('[role="region"][aria-label="Match history"]')).not.toBeNull();
    const [row] = rows(container);
    expect(row.textContent).toContain('RANKED DOUBLES');
    expect(row.textContent).toContain('Neo Tokyo');
    expect(row.querySelector('a[href="https://ballchasing.com/replay/a"]')?.textContent).toBe('BALLCHASING ↗');
  });

  it('sends concrete playlist filters (including individual extra modes) to the API and resets to page 1', async () => {
    const calls = mockFetch([record({})]);
    const { container } = await mount(<HistoryView />);
    await clickText(container, '3V3');
    expect(calls.some((c) => c.includes('playlist=13'))).toBe(true);
    await clickText(container, '1V1');
    expect(calls.some((c) => c.includes('playlist=10'))).toBe(true);
    const extraBtn = container.querySelector<HTMLButtonElement>('[data-mode-btn="extra"]')!;
    await act(async () => extraBtn.click());
    await clickText(container.querySelector('[role="menu"]')!, 'HOOPS');
    expect(calls.some((c) => c.includes('playlist=27'))).toBe(true);
  });

  it('filters by result and strict LOCAL status (downloaded && !uploaded) and preserves short_rank divisions in box scores', async () => {
    const withShortRankDiv = record({
      match_guid: 'local-dl',
      blue_score: 0,
      orange_score: 2,
      result: 'defeat',
      download_status: 'DOWNLOADED',
      upload_status: 'PENDING',
      stats: {
        match_guid: 'local-dl',
        map_name: 'DFH Stadium',
        playlist: 11,
        blue_score: 0,
        orange_score: 2,
        duration: 300,
        overtime: false,
        date: new Date().toISOString(),
        players: [
          {
            name: 'LocalHero',
            platform: 'Steam',
            online_id: 'me',
            team: 0,
            score: 410,
            goals: 0,
            assists: 0,
            saves: 2,
            shots: 3,
            demos: 1,
            mmr: 1490,
            rank_name: 'Grand Champion I',
            short_rank: 'GC I · DIV II',
          },
        ],
      },
    });
    const pendingNotDownloaded = record({
      match_guid: 'pending-nodl',
      blue_score: 3,
      orange_score: 1,
      download_status: 'PENDING',
      upload_status: 'PENDING',
    });
    mockFetch([withShortRankDiv, pendingNotDownloaded]);
    const { container } = await mount(<HistoryView localPlayerIds={new Set(['me'])} />);
    expect(rows(container)).toHaveLength(2);

    // LOCAL filter must include DOWNLOADED & !UPLOADED, excluding PENDING / non-downloaded
    await clickText(container, 'LOCAL');
    expect(rows(container).map((r) => r.dataset.guid)).toEqual(['local-dl']);

    await clickText(container, 'LOSSES');
    expect(rows(container).map((r) => r.dataset.guid)).toEqual(['local-dl']);

    // Expanding row preserves short_rank division ("GC1-D2") when rank_name is tier-only
    await act(async () => (rows(container)[0].firstElementChild as HTMLElement).click());
    expect(container.querySelector('[data-testid="history-box-score"]')!.textContent).toContain('GC1-D2');
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

  it('falls back to mode-scoped demo matches in preview mode when the store is empty', async () => {
    setSearch('?preview=true');
    mockFetch([]);
    const { container } = await mount(<HistoryView />);
    const text = container.textContent || '';
    expect(text).toContain('DFH Stadium');
    expect(text).toContain('Utopia Coliseum');
    expect(text).not.toContain('Champions Field');
    expect(text).not.toContain('SIMULATED PREVIEW');

    await clickText(container, '3V3');
    const text3v3 = container.textContent || '';
    expect(text3v3).toContain('Champions Field');
    expect(text3v3).not.toContain('DFH Stadium');
  });

  it('shows an empty state on the page-title layout', async () => {
    mockFetch([]);
    const { container } = await mount(<HistoryView />);
    expect(container.textContent).toContain('NO MATCHES IN THE LOCAL STORE YET');
  });

  it('orients HistoryRow collapsed score as You – Opponent when local player is on Orange and formats raw UE3 arena names', async () => {
    const orangeWin = record({
      match_guid: 'orange-win-1',
      map_name: 'UF_Day_P',
      playlist: 11,
      blue_score: 1,
      orange_score: 3,
      winner_team: 1,
      result: 'victory',
      stats: {
        match_guid: 'orange-win-1',
        map_name: 'UF_Day_P',
        playlist: 11,
        blue_score: 1,
        orange_score: 3,
        winner_team: 1,
        duration: 300,
        overtime: false,
        date: new Date().toISOString(),
        players: [
          { name: 'OppBlue1', platform: 'Steam', online_id: 'opp1', team: 0, score: 250, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 },
          { name: 'OppBlue2', platform: 'Steam', online_id: 'opp2', team: 0, score: 200, goals: 0, assists: 1, saves: 0, shots: 1, demos: 0 },
          { name: 'MeOrange', platform: 'Epic', online_id: 'local_orange_id', team: 1, score: 520, goals: 2, assists: 1, saves: 2, shots: 4, demos: 1 },
          { name: 'MateOrange', platform: 'Steam', online_id: 'mate1', team: 1, score: 410, goals: 1, assists: 2, saves: 1, shots: 3, demos: 0 },
        ],
      },
    });

    mockFetch([orangeWin]);
    const { container } = await mount(<HistoryView localPlayerIds={new Set(['local_orange_id'])} />);
    const [row] = rows(container);
    expect(row.textContent).toContain('Urban Future (Day)');
    expect(row.textContent).toContain('You + MateOrange vs OppBlue1, OppBlue2');

    // Score cell shows W + 3–1 (3 in orange ink, 1 in blue ink), never contradictory W + 1–3
    expect(row.textContent).toMatch(/^W3–1/);
    const scoreSpans = row.querySelectorAll('div.t-font-mono span');
    expect(scoreSpans[0].textContent).toBe('3');
    expect(scoreSpans[0].className).toContain('text-orange-ink');
    expect(scoreSpans[2].textContent).toBe('1');
    expect(scoreSpans[2].className).toContain('text-blue-ink');

    // Expanding the row preserves canonical BLUE and ORANGE box score slabs
    await act(async () => (row.firstElementChild as HTMLElement).click());
    const box = row.querySelector('[data-testid="history-box-score"]')!;
    expect(box.textContent).toContain('BLUE');
    expect(box.textContent).toContain('ORANGE');
  });

  it('resets pagination to page 1 when sort changes and omits inline playlist segmented control when hidePlaylistFilter is true', async () => {
    const manyMatches = Array.from({ length: 20 }, (_, i) => record({ match_guid: `m-${i}` }));
    const calls: string[] = [];
    globalThis.fetch = vi.fn((url: string) => {
      calls.push(url);
      if (url.startsWith('/api/sync/ballchasing/status')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ running: false, total_replays: 0, synced_matches: 0, synced_players: 0 }) });
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ matches: manyMatches, total: 45, limit: 20, offset: 0 }),
      });
    }) as unknown as typeof fetch;

    const { container } = await mount(<HistoryView hidePlaylistFilter />);
    expect(container.querySelector('[aria-label="Filter by playlist"]')).toBeNull();
    expect(container.textContent).toContain('SHOWING 1–20 OF 45 MATCHES');

    await clickText(container, 'NEXT');
    expect(calls.some((c) => c.includes('offset=20'))).toBe(true);
    expect(container.textContent).toContain('SHOWING 21–40 OF 45 MATCHES');

    const sortSelect = container.querySelector<HTMLSelectElement>('#history-sort')!;
    sortSelect.value = 'goals-desc';
    await act(async () => {
      sortSelect.dispatchEvent(new Event('change', { bubbles: true }));
    });
    const lastMatchCall = [...calls].reverse().find((c) => c.startsWith('/api/matches?'))!;
    expect(lastMatchCall).toContain('offset=0');
    expect(lastMatchCall).toContain('sort_by=goals');
    expect(lastMatchCall).toContain('sort_dir=desc');
  });
});
