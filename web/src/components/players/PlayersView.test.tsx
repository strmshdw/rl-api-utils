// @vitest-environment happy-dom
import { describe, it, expect, afterEach, vi } from 'vitest';
import { act } from 'react';
import { PlayersView } from './PlayersView';
import { h2hTag, matchesRole, parseRanks, primaryRank, spotlights } from './playerData';
import { PlayerDetailResponse, PlayerSummary } from '../../types/api';
import { cleanup, clickText, flush, mount } from '../../test/dom';

const RANKS = JSON.stringify({
  '10': { playlist_id: 10, playlist_name: 'Ranked Duel (1v1)', tier: 16, division: 1, rank_name: 'Champion I', mmr: 1240 },
  '13': { playlist_id: 13, playlist_name: 'Ranked Standard (3v3)', tier: 18, division: 1, rank_name: 'Champion III', mmr: 1468 },
  '11': { playlist_id: 11, playlist_name: 'Ranked Doubles (2v2)', tier: 19, division: 1, rank_name: 'Grand Champion I', mmr: 1502 },
});

function summary(name: string, ww: number, wl: number, vw: number, vl: number, over: Partial<PlayerSummary> = {}): PlayerSummary {
  return {
    player_id: `Steam|${name}|0`,
    platform: 'Steam',
    player_name: name,
    ranks_json: RANKS,
    first_seen_at: '2026-08-14T12:00:00Z',
    last_seen_at: new Date().toISOString(),
    total_wins_as_teammate: ww,
    total_losses_as_teammate: wl,
    total_wins_as_opponent: vw,
    total_losses_as_opponent: vl,
    total_matches: ww + wl + vw + vl,
    ...over,
  };
}

const PLAYERS = [
  summary('AeroPulse', 4, 1, 2, 1),
  summary('VortexRL', 1, 0, 2, 3),
  summary('DanielRL', 0, 0, 0, 3),
  summary('Mistral', 1, 1, 0, 0),
  summary('OneOff', 0, 0, 1, 0),
];

function mockFetch(detail?: Partial<PlayerDetailResponse>) {
  const calls: string[] = [];
  globalThis.fetch = vi.fn((url: string) => {
    calls.push(url);
    if (url.startsWith('/api/players?')) {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ players: PLAYERS, total: PLAYERS.length, limit: 24, offset: 0 }) });
    }
    if (url.startsWith('/api/players/')) {
      if (url.includes('/matches')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ matches: [], total: 0, limit: 10, offset: 0 }) });
      }
      const id = decodeURIComponent(url.slice('/api/players/'.length));
      const p = PLAYERS.find((x) => x.player_id === id)!;
      const body: PlayerDetailResponse = {
        player_id: p.player_id,
        platform: p.platform,
        player_name: p.player_name,
        ranks_json: p.ranks_json,
        first_seen_at: p.first_seen_at,
        last_seen_at: p.last_seen_at,
        matchups: [
          { player_id: id, playlist_id: 11, wins_as_teammate: 3, losses_as_teammate: 1, wins_as_opponent: 2, losses_as_opponent: 1, total_matches: 7, last_played_at: new Date().toISOString() },
          { player_id: id, playlist_id: 13, wins_as_teammate: 1, losses_as_teammate: 0, wins_as_opponent: 0, losses_as_opponent: 0, total_matches: 1, last_played_at: new Date().toISOString() },
        ],
        ...detail,
      };
      return Promise.resolve({ ok: true, json: () => Promise.resolve(body) });
    }
    return Promise.reject(new Error(`Unhandled ${url}`));
  }) as unknown as typeof fetch;
  return calls;
}

const rows = (root: ParentNode) => Array.from(root.querySelectorAll<HTMLElement>('[data-testid="player-row"]'));

describe('player data helpers', () => {
  it('picks spotlights: best teammate, most faced, toughest rival (≥2 games)', () => {
    const [mate, faced, rival] = spotlights(PLAYERS);
    expect(mate.player?.player_name).toBe('AeroPulse');
    expect(mate.value).toBe('80%');
    expect(mate.sub).toBe('4–1 TOGETHER');
    expect(faced.player?.player_name).toBe('VortexRL');
    expect(faced.sub).toBe('5 GAMES AS OPPONENT');
    expect(rival.player?.player_name).toBe('DanielRL');
    expect(rival.value).toBe('0–3');
    expect(rival.sub).toBe('NEVER BEATEN');
  });

  it('leaves spotlights empty without enough games', () => {
    const spots = spotlights([summary('Solo', 1, 0, 1, 0)]);
    expect(spots.every((s) => s.player === null && s.value === '—')).toBe(true);
  });

  it('applies the role filters', () => {
    const [aero, , daniel, mistral, oneOff] = PLAYERS;
    expect(matchesRole(oneOff, 'frequent')).toBe(false);
    expect(matchesRole(daniel, 'frequent')).toBe(true);
    expect(matchesRole(daniel, 'teammate')).toBe(false);
    expect(matchesRole(mistral, 'opponent')).toBe(false);
    expect(matchesRole(mistral, 'synergy')).toBe(true); // exactly 50%
    expect(matchesRole(aero, 'synergy')).toBe(true);
    expect(matchesRole(daniel, 'synergy')).toBe(false);
  });

  it('tags H2H records only from two games up', () => {
    expect(h2hTag(1, 0, true)).toBeNull();
    expect(h2hTag(3, 1, true)).toBe('STRONG SYNERGY');
    expect(h2hTag(1, 1, true)).toBe('BALANCED');
    expect(h2hTag(0, 3, true)).toBe('STRUGGLED');
    expect(h2hTag(4, 1, false)).toBe('DOMINANT');
    expect(h2hTag(2, 2, false)).toBe('EVEN RIVALRY');
    expect(h2hTag(1, 4, false)).toBe('TOUGH OPPONENT');
  });

  it('parses ranks defensively and orders 2v2, 3v3, 1v1', () => {
    expect(parseRanks(RANKS).map((r) => r.playlist_id)).toEqual([11, 13, 10]);
    expect(primaryRank(RANKS)?.rank_name).toBe('Grand Champion I');
    expect(parseRanks('{not json')).toEqual([]);
    expect(parseRanks('')).toEqual([]);
  });
});

describe('PlayersView', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('renders spotlight cards and one row per player', async () => {
    mockFetch();
    const { container } = await mount(<PlayersView />);
    expect(container.querySelector('[data-testid="spotlight-mate"]')!.textContent).toContain('AeroPulse');
    expect(container.querySelector('[data-testid="spotlight-rival"]')!.textContent).toContain('NEVER BEATEN');
    expect(rows(container)).toHaveLength(5);
    const daniel = rows(container).find((r) => r.textContent?.includes('DanielRL'))!;
    expect(daniel.textContent).toContain('STEAM');
    expect(daniel.textContent).toContain('Grand Champion I');
    expect(daniel.textContent).toContain('1502');
    expect(daniel.textContent).toContain('—'); // no games together
    expect(daniel.textContent).toContain('0–3');
  });

  it('filters rows by role', async () => {
    mockFetch();
    const { container } = await mount(<PlayersView />);
    await clickText(container, 'TEAMMATES');
    expect(rows(container).map((r) => r.textContent)).toHaveLength(3);
    await clickText(container, 'FREQUENT');
    expect(rows(container).some((r) => r.textContent?.includes('OneOff'))).toBe(false);
  });

  it('passes the platform filter to the search API', async () => {
    const calls = mockFetch();
    const { container } = await mount(<PlayersView />);
    await clickText(container, 'PSN');
    await flush();
    expect(calls.some((c) => c.includes('platform=playstation'))).toBe(true);
  });

  it('opens the full-page player profile (PlayerProfilePage) with H2H blocks, ranks and extra modes toggle when a row is clicked', async () => {
    const calls = mockFetch();
    const { container } = await mount(<PlayersView />);
    const aero = rows(container).find((r) => r.textContent?.includes('AeroPulse'))!;
    await act(async () => aero.click());
    await flush();

    expect(calls).toContain('/api/players/Steam%7CAeroPulse%7C0');
    const page = container.querySelector('[data-testid="player-profile-page"]')!;
    expect(page).not.toBeNull();
    expect(page.textContent).toContain('PLAYER PROFILE · STEAM');
    expect(page.textContent).toContain('Steam|AeroPulse|0');
    expect(page.querySelector('a')?.getAttribute('href')).toBe(
      'https://rocketleague.tracker.network/rocket-league/profile/steam/AeroPulse/overview',
    );
    const withYou = page.querySelector('[data-testid="h2h-with"]')!;
    expect(withYou.textContent).toContain('5 GAMES');
    expect(withYou.textContent).toContain('STRONG SYNERGY');
    expect(withYou.textContent).toContain('4–1');
    expect(withYou.querySelectorAll('[data-cell]')).toHaveLength(10);
    expect(page.querySelector('[data-testid="h2h-vs"]')!.textContent).toContain('DOMINANT');
    expect(page.textContent).toContain('RANKED 2V2');
    expect(page.textContent).toContain('FIRST SEEN AUG 14, 2026');

    await clickText(page, 'VIEW FULL PLAYLIST STATS & EXTRA MODES');
    expect(page.textContent).toContain('HOOPS (2V2)');
    expect(page.textContent).toContain('RUMBLE (3V3)');

    await clickText(page, 'COPY ID');
    expect(page.textContent).toContain('COPIED ✓');
    await clickText(page, '← BACK TO PLAYERS DIRECTORY');
    expect(container.querySelector('[data-testid="player-profile-page"]')).toBeNull();
  });

  it('allows pinning and unpinning up to 3 players with dynamic relationship tags and shifts oldest when 4th is pinned', async () => {
    const { DashboardProvider } = await import('../../context/DashboardContext');
    localStorage.clear();
    mockFetch();
    const { container } = await mount(
      <DashboardProvider>
        <PlayersView />
      </DashboardProvider>,
    );
    const pinByName = async (name: string) => {
      const r = rows(container).find((row) => row.textContent?.includes(name))!;
      const btn = r.querySelector('button')!;
      await act(async () => btn.click());
    };

    await pinByName('DanielRL');
    expect(container.textContent).toContain('NEMESIS');

    // Pin 3 more players (VortexRL, Mistral, OneOff) -> DanielRL is shifted out and OneOff becomes pinned
    await pinByName('VortexRL');
    await pinByName('Mistral');
    await pinByName('OneOff');

    const savedPinned = JSON.parse(localStorage.getItem('rlsync_pinned_players') || '[]');
    expect(savedPinned).toEqual(['Steam|VortexRL|0', 'Steam|Mistral|0', 'Steam|OneOff|0']);
  });

  it('sorts directory rows by AGAINST, WITH PTS/G, and VS PTS/G and forwards sort params to API', async () => {
    const calls = mockFetch();
    const { container } = await mount(<PlayersView />);

    await clickText(container, 'AGAINST');
    await flush();
    expect(calls.some((c) => c.includes('sort=opponent_wr'))).toBe(true);

    await clickText(container, 'WITH PTS/G');
    await flush();
    expect(calls.some((c) => c.includes('sort=with_pts'))).toBe(true);

    await clickText(container, 'VS PTS/G');
    await flush();
    expect(calls.some((c) => c.includes('sort=vs_pts'))).toBe(true);
  });

  it('uses backend with_you_stats on playlist filter in PlayerProfilePage and respects local_result on opponent matches', async () => {
    const { emptyRoleStats } = await import('../../utils/roleStats');
    globalThis.fetch = vi.fn((url: string) => {
      if (url.startsWith('/api/players?')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ players: PLAYERS, total: PLAYERS.length, limit: 24, offset: 0 }) });
      }
      if (url.includes('/matches')) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              matches: [
                {
                  match_guid: 'opp-win-guid',
                  record_start_timestamp: 1700000000,
                  map_name: 'DFH Stadium',
                  playlist: 11,
                  blue_score: 4,
                  orange_score: 1,
                  created_at: new Date().toISOString(),
                  updated_at: new Date().toISOString(),
                  role: 'opponent',
                  result: 'victory',
                  local_result: 'victory',
                  player_stats: {
                    match_guid: 'opp-win-guid',
                    player_id: 'Steam|AeroPulse|0',
                    team_num: 1,
                    is_local: false,
                    won: false,
                    score: 310,
                    goals: 1,
                    assists: 0,
                    saves: 2,
                    shots: 3,
                    demos: 0,
                  },
                },
              ],
              total: 1,
              limit: 20,
              offset: 0,
            }),
        });
      }
      return Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve({
            player_id: 'Steam|AeroPulse|0',
            platform: 'Steam',
            player_name: 'AeroPulse',
            ranks_json: RANKS,
            first_seen_at: '2026-08-14T12:00:00Z',
            last_seen_at: new Date().toISOString(),
            matchups: [
              { player_id: 'Steam|AeroPulse|0', playlist_id: 11, wins_as_teammate: 3, losses_as_teammate: 1, wins_as_opponent: 2, losses_as_opponent: 1, total_matches: 7, last_played_at: new Date().toISOString() },
            ],
            with_you_stats: emptyRoleStats(4, 3, 1, { avg_score: 777 }),
            against_you_stats: emptyRoleStats(3, 2, 1, { avg_score: 666 }),
          }),
      });
    }) as unknown as typeof fetch;

    const { container } = await mount(<PlayersView />);
    const aero = rows(container).find((r) => r.textContent?.includes('AeroPulse'))!;
    await act(async () => aero.click());
    await flush();

    const page = container.querySelector('[data-testid="player-profile-page"]')!;
    // Click 2V2 playlist scope button inside PlayerProfilePage
    const btn2v2 = Array.from(page.querySelectorAll<HTMLButtonElement>('.prof-pl-btn')).find(
      (b) => b.textContent?.trim() === '2V2',
    )!;
    await act(async () => btn2v2.click());
    await flush();

    // Must use backend with_you_stats (777) and against_you_stats (666) even when 2V2 is selected
    expect(page.querySelector('[data-testid="h2h-with"]')!.textContent).toContain('777');
    expect(page.querySelector('[data-testid="h2h-vs"]')!.textContent).toContain('666');

    // Opponent match where local_result='victory' must display WIN (not inverted to LOSS)
    const matchRow = page.querySelector('[data-testid="profile-match-row"]')!;
    expect(matchRow.textContent).toContain('WIN');
  });
});

