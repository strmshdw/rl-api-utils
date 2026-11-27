// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act } from 'react';
import { HistoricalMatchDetailModal } from './HistoricalMatchDetailModal';
import { orientedMatchSummary, playerNames, teamRows } from './historyData';
import { HistoricalMatchRecord } from '../../types/api';
import { cleanup, clickText, mount } from '../../test/dom';

const SAMPLE_HISTORICAL_MATCH: HistoricalMatchRecord = {
  match_guid: 'test-guid-1234',
  record_start_timestamp: 1700000000,
  map_name: 'DFH Stadium',
  playlist: 11,
  replay_url: '',
  download_status: 'COMPLETED',
  local_file_path: '/path/to/replay.replay',
  upload_status: 'COMPLETED',
  ballchasing_id: 'bc-1234',
  ballchasing_url: 'https://ballchasing.com/replay/bc-1234',
  retry_count: 0,
  last_error: '',
  blue_score: 3,
  orange_score: 2,
  winner_team: 0,
  replay_stats_json: JSON.stringify({
    match_guid: 'test-guid-1234',
    map_name: 'DFH Stadium',
    playlist: 11,
    blue_score: 3,
    orange_score: 2,
    duration: 300,
    players: [
      {
        name: 'PlayerOne',
        platform: 'Steam',
        online_id: 'steam_p1',
        team: 0,
        score: 450,
        goals: 2,
        assists: 1,
        saves: 2,
        shots: 4,
        demos: 1,
        rank_name: 'Platinum II Division II',
        division: 1,
        boost: { bpm: 550, avg_amount: 45.2, amount_collected: 2400 },
        movement: { avg_speed: 1450, percent_supersonic_speed: 18.5 },
        positioning: {
          percent_defensive_third: 42.1,
          percent_neutral_third: 35.0,
          percent_offensive_third: 22.9,
          percent_most_forward: 54.0,
          percent_most_back: 46.0,
          avg_distance_to_ball: 2450,
        },
        demo: { inflicted: 1, taken: 0 },
      },
      {
        name: 'PlayerTwo',
        platform: 'Epic',
        online_id: 'epic_p2',
        team: 1,
        score: 320,
        goals: 1,
        assists: 0,
        saves: 1,
        shots: 3,
        demos: 0,
        boost: { bpm: 480, avg_amount: 38.0, amount_collected: 1900 },
        movement: { avg_speed: 1380, percent_supersonic_speed: 12.0 },
        positioning: { percent_defensive_third: 50.0, percent_neutral_third: 30.0, percent_offensive_third: 20.0 },
        demo: { inflicted: 0, taken: 1 },
      },
    ],
  }),
  created_at: '2026-10-16T12:00:00Z',
  updated_at: '2026-10-16T12:05:00Z',
  stats: {
    match_guid: 'test-guid-1234',
    map_name: 'DFH Stadium',
    playlist: 11,
    blue_score: 3,
    orange_score: 2,
    winner_team: 0,
    duration: 300,
    overtime: false,
    date: '2026-10-16T12:00:00Z',
    players: [
      {
        name: 'PlayerOne',
        platform: 'Steam',
        online_id: 'steam_p1',
        team: 0,
        score: 450,
        goals: 2,
        assists: 1,
        saves: 2,
        shots: 4,
        demos: 1,
        rank_name: 'Platinum II Division II',
        division: 1,
        boost: { bpm: 550, avg_amount: 45.2, amount_collected: 2400 },
        movement: { avg_speed: 1450, percent_supersonic_speed: 18.5 },
        positioning: {
          percent_defensive_third: 42.1,
          percent_neutral_third: 35.0,
          percent_offensive_third: 22.9,
          percent_most_forward: 54.0,
          percent_most_back: 46.0,
          avg_distance_to_ball: 2450,
        },
        demo: { inflicted: 1, taken: 0 },
      },
      {
        name: 'PlayerTwo',
        platform: 'Epic',
        online_id: 'epic_p2',
        team: 1,
        score: 320,
        goals: 1,
        assists: 0,
        saves: 1,
        shots: 3,
        demos: 0,
        boost: { bpm: 480, avg_amount: 38.0, amount_collected: 1900 },
        movement: { avg_speed: 1380, percent_supersonic_speed: 12.0 },
        positioning: { percent_defensive_third: 50.0, percent_neutral_third: 30.0, percent_offensive_third: 20.0 },
        demo: { inflicted: 0, taken: 1 },
      },
    ],
  },
};

describe('HistoricalMatchDetailModal', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders overview tab with score, map, and playlist information', async () => {
    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={SAMPLE_HISTORICAL_MATCH}
        onClose={vi.fn()}
        localPlayerIds={new Set(['steam_p1'])}
      />
    );

    expect(container.textContent).toContain('3');
    expect(container.textContent).toContain('2');
    expect(container.textContent).toContain('DFH Stadium');
    expect(container.textContent).toContain('RANKED DOUBLES');
    expect(container.textContent).toContain('You');
    expect(container.textContent).toContain('PlayerTwo');
  });

  it('switches between all 5 ballchasing telemetry tabs and raw json', async () => {
    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={SAMPLE_HISTORICAL_MATCH}
        onClose={vi.fn()}
        localPlayerIds={new Set(['steam_p1'])}
      />
    );

    // Tab 1: 1. CORE & MECHANICS (default)
    expect(container.textContent).toContain('BLUE TEAM TOTALS / AVG');
    expect(container.textContent).toContain('ORANGE TEAM TOTALS / AVG');
    expect(container.textContent).toContain('P2-D2');
    expect(container.querySelector('[role="region"][aria-label="Core match scoreboard"]')).not.toBeNull();

    // Tab 2: 2. BOOST ECONOMY & PADS (BCPM header, no fabricated bpm * 0.96 = 528)
    await clickText(container, '2. BOOST ECONOMY & PADS');
    expect(container.querySelector('[role="region"][aria-label="Boost economy telemetry"]')).not.toBeNull();
    expect(container.textContent).toContain('BPM');
    expect(container.textContent).toContain('BCPM');
    expect(container.textContent).not.toContain('BCMPM');
    expect(container.textContent).toContain('AVG BST');
    expect(container.textContent).toContain('550');
    expect(container.textContent).not.toContain('528'); // 550 * 0.96 must never be fabricated

    // Tab 3: 3. SPEED & MOVEMENT (native Unreal Units uu/s)
    await clickText(container, '3. SPEED & MOVEMENT');
    expect(container.querySelector('[role="region"][aria-label="Speed and movement telemetry"]')).not.toBeNull();
    expect(container.textContent).toContain('AVG SPEED');
    expect(container.textContent).toContain('SUPERSONIC');
    expect(container.textContent).toContain('1450 uu/s');
    expect(container.textContent).toContain('1380 uu/s');
    expect(container.textContent).not.toContain('km/h');

    // Tab 4: 4. POSITIONING & ROTATION (2v2 shows 1ST MAN / 2ND MAN and native uu distance)
    await clickText(container, '4. POSITIONING & ROTATION');
    expect(container.querySelector('[role="region"][aria-label="Positioning and rotation telemetry"]')).not.toBeNull();
    expect(container.textContent).toContain('DEF 1/3');
    expect(container.textContent).toContain('BEHIND BALL');
    expect(container.textContent).toContain('1ST MAN');
    expect(container.textContent).toContain('2ND MAN');
    expect(container.textContent).not.toContain('3RD MAN');
    expect(container.textContent).toContain('2450 uu');
    expect(container.textContent).not.toContain('24.5 m');

    // Tab 5: 5. TEAM HEAD-TO-HEAD (clean AVG SUPERSONIC % without share-of-sum suffix, plus uu/s AVG SPEED)
    await clickText(container, '5. TEAM HEAD-TO-HEAD');
    expect(container.textContent).toContain('TEAM SCORE (PTS)');
    expect(container.textContent).toContain('TOTAL BOOST COLLECTED');
    expect(container.textContent).toContain('DEMOLITIONS INFLICTED');
    expect(container.textContent).toContain('BLUE 18.5%');
    expect(container.textContent).toContain('12% ORANGE');
    expect(container.textContent).toContain('BLUE 1450 uu/s');
    expect(container.textContent).toContain('1380 uu/s ORANGE');
    expect(container.textContent).not.toContain('km/h');

    // Tab 6: RAW JSON tab
    await clickText(container, 'RAW JSON');
    expect(container.textContent).toContain('RAW TELEMETRY JSON');
    expect(container.textContent).toContain('COPY JSON');
  });

  it('hides teammate-relative rotation columns in 1v1 and formats UE3 arena codes in modal title', async () => {
    const duelMatch: HistoricalMatchRecord = {
      ...SAMPLE_HISTORICAL_MATCH,
      map_name: 'stadium_10a_p',
      playlist: 10,
      stats: {
        ...SAMPLE_HISTORICAL_MATCH.stats!,
        map_name: 'stadium_10a_p',
        playlist: 10,
      },
    };
    const { container } = await mount(
      <HistoricalMatchDetailModal match={duelMatch} onClose={vi.fn()} localPlayerIds={new Set(['steam_p1'])} />,
    );
    expect(container.textContent).toContain('DFH Stadium (10th Anniversary)');

    await clickText(container, '4. POSITIONING & ROTATION');
    expect(container.textContent).toContain('DEF 1/3');
    expect(container.textContent).toContain('2450 uu');
    expect(container.textContent).not.toContain('24.5 m');
    expect(container.textContent).not.toContain('1ST MAN');
    expect(container.textContent).not.toContain('2ND MAN');
    expect(container.textContent).not.toContain('3RD MAN');
    expect(container.textContent).not.toContain('MOST FWD');
  });

  it('deduplicates reconnect player entries in teamRows/playerNames and orients match summary to local player', () => {
    const dupPlayers = [
      { name: 'Me', platform: 'Steam', online_id: 'me_1', team: 1, score: 120, goals: 0, assists: 0, saves: 1, shots: 1, demos: 0, division: 2, rank_name: 'Grand Champion I' },
      { name: 'Me', platform: 'Steam', online_id: 'me_1', team: 1, score: 540, goals: 2, assists: 1, saves: 3, shots: 4, demos: 1, division: 2, rank_name: 'Grand Champion I' },
      { name: 'Mate', platform: 'Steam', online_id: 'mate_1', team: 1, score: 310, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0, division: 3, rank_name: 'Champion III' },
      { name: 'Opp', platform: 'Epic', online_id: 'opp_1', team: 0, score: 400, goals: 2, assists: 0, saves: 2, shots: 3, demos: 0 },
      { name: 'Opp', platform: 'Epic', online_id: 'opp_1', team: 0, score: 90, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
    ];
    const localSet = new Set(['me_1']);
    const rows = teamRows(dupPlayers, 1, localSet);
    expect(rows).toHaveLength(2);
    expect(rows[0].name).toBe('You');
    expect(rows[0].score).toBe(540);
    expect(rows[0].division).toBe(2);
    expect(playerNames(dupPlayers, localSet)).toBe('Opp vs You, Mate');

    const oriented = orientedMatchSummary(
      {
        ...SAMPLE_HISTORICAL_MATCH,
        blue_score: 2,
        orange_score: 3,
        stats: { ...SAMPLE_HISTORICAL_MATCH.stats!, players: dupPlayers },
      },
      localSet,
    );
    expect(oriented).toEqual({
      myTeam: 1,
      myScore: 3,
      oppScore: 2,
      myTeamInk: 'text-orange-ink',
      rosterText: 'You + Mate vs Opp',
    });
  });

  it('invokes onPlayerClick when a player row is clicked', async () => {
    const onPlayerClick = vi.fn();
    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={SAMPLE_HISTORICAL_MATCH}
        onClose={vi.fn()}
        onPlayerClick={onPlayerClick}
      />
    );

    const playerBtn = Array.from(container.querySelectorAll('button')).find((el) =>
      el.textContent?.includes('PlayerTwo')
    );
    expect(playerBtn).toBeTruthy();

    await act(async () => {
      playerBtn?.click();
    });

    expect(onPlayerClick).toHaveBeenCalledWith('epic_p2', 'PlayerTwo');
  });

  it('fetches full Ballchasing telemetry from /api/matches/{guid} when opened with basic scoreboard stats', async () => {
    const basicMatch: HistoricalMatchRecord = {
      ...SAMPLE_HISTORICAL_MATCH,
      stats: {
        ...SAMPLE_HISTORICAL_MATCH.stats!,
        players: SAMPLE_HISTORICAL_MATCH.stats!.players.map((p) => ({
          ...p,
          boost: undefined,
          movement: undefined,
          positioning: undefined,
        })),
      },
    };

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      json: async () => SAMPLE_HISTORICAL_MATCH,
    } as Response);

    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={basicMatch}
        onClose={vi.fn()}
        localPlayerIds={new Set(['steam_p1'])}
      />
    );

    await act(async () => {
      await Promise.resolve();
    });

    expect(fetchSpy).toHaveBeenCalledWith('/api/matches/test-guid-1234');

    await clickText(container, '2. BOOST ECONOMY & PADS');
    expect(container.textContent).not.toContain('TELEMETRY PENDING');
    expect(container.textContent).toContain('550');

    fetchSpy.mockRestore();
  });

  it('defers /api/matches/{guid} fetch when opened directly on initialTab="replay" until a telemetry tab is selected', async () => {
    const sessionOnlyMatch: HistoricalMatchRecord = {
      ...SAMPLE_HISTORICAL_MATCH,
      match_guid: 'session-only-guid',
      stats: {
        ...SAMPLE_HISTORICAL_MATCH.stats!,
        match_guid: 'session-only-guid',
        players: [],
      },
    };

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/matches/session-only-guid') {
        return new Response(JSON.stringify(SAMPLE_HISTORICAL_MATCH), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(
        JSON.stringify({
          match_guid: 'session-only-guid',
          available: false,
          source: '',
          local_available: false,
          ballchasing_available: false,
          ballchasing_configured: false,
          ballchasing_id: '',
          ballchasing_url: '',
          filename: '',
          size_bytes: 0,
          download_url: '',
          download_status: '',
          upload_status: '',
          reason: 'no_replay_source',
          message: 'No replay',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    });

    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={sessionOnlyMatch}
        initialTab="replay"
        onClose={vi.fn()}
      />,
    );

    await act(async () => {
      await Promise.resolve();
    });

    // Opening directly on replay tab MUST NOT hit /api/matches/{guid} (avoids 404 on session-only matches)
    expect(fetchSpy).not.toHaveBeenCalledWith('/api/matches/session-only-guid');

    // Switching to Core tab lazily hydrates match telemetry once
    await clickText(container, '1. CORE & MECHANICS');
    await act(async () => {
      await Promise.resolve();
    });
    expect(fetchSpy).toHaveBeenCalledWith('/api/matches/session-only-guid');

    fetchSpy.mockRestore();
  });
});
