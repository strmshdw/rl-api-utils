import { describe, it, expect } from 'vitest';
import {
  chronological,
  compactRank,
  deltaInk,
  filterSessionByPlaylist,
  findMvp,
  formatAgo,
  formatArenaName,
  formatElapsed,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  formatSigned,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  normalizeActiveModeKey,
  onlineIdOf,
  platformLabel,
  playlistFilterToParam,
  playlistLabels,
  positioningRoleLabels,
  recordCells,
  recordInk,
  rosterLine,
  shortRank,
  summarizeSession,
  summarizeSessionPlaylists,
  trackerUrl,
} from './broadcast';
import { SessionMatchDetail, SessionMatchPlayer, SessionResponse } from '../types/api';

const NOW = new Date('2026-10-12T20:00:00Z');
const minsAgo = (m: number) => new Date(NOW.getTime() - m * 60000).toISOString();

function player(over: Partial<SessionMatchPlayer>): SessionMatchPlayer {
  return {
    player_id: 'Steam|1|0',
    platform: 'Steam',
    name: 'P',
    team_num: 0,
    is_local: false,
    is_bot: false,
    stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
    rank_name: '',
    tier: 0,
    division: 0,
    mmr: 0,
    ...over,
  };
}

function match(over: Partial<SessionMatchDetail>): SessionMatchDetail {
  return {
    match_guid: 'm',
    playlist_id: 11,
    playlist_name: 'Ranked Doubles',
    started_at: minsAgo(20),
    ended_at: minsAgo(14),
    duration_seconds: 300,
    result: 'victory',
    local_team: 0,
    blue_score: 3,
    orange_score: 1,
    starting_mmr: 1000,
    ending_mmr: 1010,
    mmr_change: 10,
    players: [],
    ...over,
  };
}

describe('record helpers', () => {
  it('builds W/L cell strips wins-first and pads with empty cells', () => {
    expect(recordCells(4, 1)).toEqual(['win', 'win', 'win', 'win', 'loss']);
    expect(recordCells(1, 1)).toEqual(['win', 'loss', 'empty', 'empty', 'empty']);
    expect(recordCells(0, 0)).toEqual(Array(5).fill('empty'));
    expect(recordCells(2, 1, 10)).toHaveLength(10);
  });

  it('scales records with more games than cells instead of showing only wins', () => {
    expect(recordCells(12, 8)).toEqual(['win', 'win', 'win', 'loss', 'loss']);
    expect(recordCells(30, 0)).toEqual(Array(5).fill('win'));
  });

  it('colours records by who is ahead', () => {
    expect(recordInk(0, 0)).toBe('text-faint');
    expect(recordInk(3, 1)).toBe('text-win');
    expect(recordInk(1, 3)).toBe('text-loss');
    expect(recordInk(2, 2)).toBe('text-ink-2');
  });

  it('formats records, percentages and signed deltas', () => {
    expect(formatRecord(4, 1)).toBe('4–1');
    expect(formatRecord(0, 0)).toBe('—');
    expect(formatPct(4, 1)).toBe('80%');
    expect(formatPct(0, 0)).toBe('');
    expect(formatSigned(31.4)).toBe('+31');
    expect(formatSigned(-9.2)).toBe('-9');
    expect(formatSigned(0)).toBe('±0');
    expect(formatSigned(undefined)).toBe('±0');
    expect(deltaInk(5)).toBe('text-win');
    expect(deltaInk(-5)).toBe('text-loss');
    expect(deltaInk(0.2)).toBe('text-muted');
    expect(formatMmr(1485.6)).toBe('1486');
    expect(formatMmr(0)).toBe('—');
    expect(formatMmr(NaN)).toBe('—');
  });
});

describe('time helpers', () => {
  it('formats relative times in broadcast style', () => {
    expect(formatAgo(minsAgo(0.5), true, NOW)).toBe('JUST NOW');
    expect(formatAgo(minsAgo(9), true, NOW)).toBe('9 MIN AGO');
    expect(formatAgo(minsAgo(9), false, NOW)).toBe('9 MIN');
    expect(formatAgo(minsAgo(180), true, NOW)).toBe('3H AGO');
    expect(formatAgo(minsAgo(60 * 30), true, NOW)).toBe('YESTERDAY');
    expect(formatAgo(minsAgo(60 * 24 * 4), true, NOW)).toBe('4 DAYS AGO');
    expect(formatAgo(minsAgo(60 * 24 * 23), true, NOW)).toBe('SEP 19');
    expect(formatAgo('', true, NOW)).toBe('—');
    expect(formatAgo('not a date', true, NOW)).toBe('—');
  });

  it('formats elapsed time and short dates', () => {
    expect(formatElapsed(minsAgo(100), NOW)).toBe('1H 40M');
    expect(formatElapsed(minsAgo(40), NOW)).toBe('40M');
    expect(formatShortDate('2026-08-14T12:00:00')).toBe('AUG 14, 2026');
  });
});

describe('labels', () => {
  it('maps known playlists and cleans unknown names', () => {
    expect(playlistLabels(11)).toEqual({ name: 'RANKED DOUBLES', short: 'DOUBLES', mode: 'RANKED 2V2' });
    expect(playlistLabels(10).mode).toBe('RANKED 1V1');
    expect(playlistLabels(6).name).toBe('PRIVATE MATCH');
    expect(playlistLabels(53).name).toBe('KNOCKOUT');
    expect(playlistLabels(99, 'Heatseeker (2v2)').name).toBe('HEATSEEKER');
    expect(playlistLabels(77).name).toBe('PLAYLIST 77');
  });

  it('formats ranks cleanly without duplicating embedded divisions or shifting 0-based PsyNet divisions', () => {
    expect(shortRank('Platinum II Division II', 1)).toBe('PLAT II · DIV II');
    expect(shortRank('Platinum II Division I', 0)).toBe('PLAT II · DIV I');
    expect(shortRank('Grand Champion I', 3)).toBe('GC I · DIV III');
    expect(shortRank('Grand Champion I', undefined, 'GC I · DIV II')).toBe('GC I · DIV II');
    expect(shortRank('Diamond III', 0, undefined, 15)).toBe('DIA III · DIV I');
    expect(shortRank('Supersonic Legend', 2)).toBe('SSL');
    expect(shortRank('Supersonic Legend Division I', 0)).toBe('SSL');
    expect(shortRank('Unranked', 2)).toBe('UNRANKED');
    expect(shortRank(null, 1)).toBe('UNRANKED');
    expect(shortRank('GC I · DIV II', 1)).toBe('GC I · DIV II');
    expect(compactRank('Grand Champion I', 2)).toBe('GC I · D2');
    expect(compactRank('Platinum II Division II', 1)).toBe('PLAT II · D2');
    expect(compactRank('Diamond III', 0, undefined, 15)).toBe('DIA III · D1');
    expect(compactRank('Supersonic Legend', 1)).toBe('SSL');
    expect(compactRank(undefined, 1)).toBe('UNRANKED');
  });

  it('maps UE3 arena filenames to canonical Rocket League arena names with clean fallback', () => {
    expect(formatArenaName('UF_Day_P')).toBe('Urban Future (Day)');
    expect(formatArenaName('stadium_10a_p')).toBe('DFH Stadium (10th Anniversary)');
    expect(formatArenaName('eurostadium_night_p')).toBe('Mannfield (Night)');
    expect(formatArenaName('wasteland_Night_S_P')).toBe('Wasteland (Night)');
    expect(formatArenaName('cs_hw_p')).toBe('Rivals Arena');
    expect(formatArenaName('DFH Stadium')).toBe('DFH Stadium');
    expect(formatArenaName('custom_desert_arena_p')).toBe('Custom Desert Arena');
    expect(formatArenaName('')).toBe('—');
    expect(formatArenaName(null)).toBe('—');
  });

  it('evaluates playlist mode filters, query params, mode keys, and positioning role labels', () => {
    expect(matchesPlaylistFilter(11, '2v2')).toBe(true);
    expect(matchesPlaylistFilter(12, '3v3')).toBe(true);
    expect(matchesPlaylistFilter(13, '3v3')).toBe(true);
    expect(matchesPlaylistFilter(12, '13')).toBe(false);
    expect(matchesPlaylistFilter(13, '13')).toBe(true);
    expect(matchesPlaylistFilter(27, 'extra')).toBe(true);
    expect(matchesPlaylistFilter(34, 'extra')).toBe(true);
    expect(matchesPlaylistFilter(2, 'casual')).toBe(true);
    expect(matchesPlaylistFilter(6, 'private')).toBe(true);
    expect(matchesPlaylistFilter(28, '27,28')).toBe(true);
    expect(matchesPlaylistFilter(11, '1v1')).toBe(false);

    expect(playlistFilterToParam('all')).toBe('');
    expect(playlistFilterToParam('2v2')).toBe('11');
    expect(playlistFilterToParam('3v3')).toBe('13');
    expect(playlistFilterToParam('hoops')).toBe('27');
    expect(playlistFilterToParam('extra')).toBe('extra');

    expect(modeKeyFromPlaylistId(11)).toBe('11');
    expect(modeKeyFromPlaylistId(12)).toBe('13');
    expect(modeKeyFromPlaylistId(2)).toBe('casual');

    expect(normalizeActiveModeKey(undefined)).toBe('11');
    expect(normalizeActiveModeKey('')).toBe('11');
    expect(normalizeActiveModeKey('all')).toBe('11');
    expect(normalizeActiveModeKey('0')).toBe('11');
    expect(normalizeActiveModeKey('extra')).toBe('27');
    expect(normalizeActiveModeKey('2v2')).toBe('11');
    expect(normalizeActiveModeKey('3v3')).toBe('13');
    expect(normalizeActiveModeKey('hoops')).toBe('27');
    expect(normalizeActiveModeKey('28')).toBe('28');
    expect(normalizeActiveModeKey('casual')).toBe('casual');

    expect(positioningRoleLabels(10)).toEqual({
      showRotation: false,
      forwardLabel: 'MOST FWD',
      backLabel: 'MOST BACK',
    });
    expect(positioningRoleLabels(11)).toEqual({
      showRotation: true,
      forwardLabel: '1ST MAN',
      backLabel: '2ND MAN',
    });
    expect(positioningRoleLabels(13)).toEqual({
      showRotation: true,
      forwardLabel: '1ST MAN',
      backLabel: '3RD MAN',
    });
    expect(positioningRoleLabels(4)).toEqual({
      showRotation: true,
      forwardLabel: 'MOST FWD',
      backLabel: 'MOST BACK',
    });
  });

  it('labels platforms, including PS4/PS5 IDs and bots', () => {
    expect(platformLabel('Steam')).toBe('STEAM');
    expect(platformLabel('Epic|abc|0')).toBe('EPIC');
    expect(platformLabel('PlayStation')).toBe('PLAYSTATION');
    expect(platformLabel('PS4|psn_zenith99|0')).toBe('PLAYSTATION');
    expect(platformLabel('Xbox')).toBe('XBOX');
    expect(platformLabel('Steam', true)).toBe('BOT');
    expect(platformLabel('')).toBe('—');
  });

  it('builds tracker URLs with the Steam64 ID for Steam players', () => {
    expect(trackerUrl('Steam|76561198000000010|0', 'Steam', 'AeroPulse')).toBe(
      'https://rocketleague.tracker.network/rocket-league/profile/steam/76561198000000010/overview',
    );
    expect(trackerUrl('Epic|x|0', 'Epic', 'Daniel RL')).toContain('/epic/Daniel%20RL/overview');
    expect(trackerUrl('PS4|psn_z|0', 'PS4', 'Zenith_99')).toContain('/psn/Zenith_99/');
    expect(trackerUrl('Xbox|1|0', 'Xbox', 'Mistral')).toContain('/xbl/Mistral/');
    expect(trackerUrl('bot', 'bot', 'Bot')).toBeNull();
  });

  it('extracts the online ID from a player ID', () => {
    expect(onlineIdOf('Epic|Demo_Local|0')).toBe('demo_local');
    expect(onlineIdOf('')).toBe('');
  });
});

describe('match helpers', () => {
  it('awards MVP to the top scorer on the winning team only', () => {
    const players = [
      player({ player_id: 'local', team_num: 0, is_local: true, stats: { score: 900, goals: 3, assists: 0, saves: 0, shots: 0, demos: 0 } }),
      player({ player_id: 'opp', team_num: 1, stats: { score: 400, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 } }),
    ];
    // Orange won 2-1 even though the local (blue) player had the match-high score.
    expect(findMvp(players, 1, 1, 2)?.player_id).toBe('opp');
    expect(findMvp(players, undefined, 3, 1)?.player_id).toBe('local');
    expect(findMvp(players, undefined, 2, 2)).toBeNull();
  });

  it('never awards MVP to a zero-score player', () => {
    expect(findMvp([player({ team_num: 0 })], 0, 1, 0)).toBeNull();
  });

  it('builds the roster line from the local side', () => {
    const m = match({
      local_team: 1,
      players: [
        player({ name: 'Me', team_num: 1, is_local: true }),
        player({ name: 'Mate', team_num: 1 }),
        player({ name: 'Opp1', team_num: 0 }),
        player({ name: 'Opp2', team_num: 0 }),
        player({ name: 'Spec', team_num: 255 }),
      ],
    });
    expect(rosterLine(m)).toBe('You + Mate vs Opp1, Opp2');
    expect(rosterLine(match({ players: [] }))).toBe('');
  });

  it('orders matches by end time', () => {
    const a = match({ match_guid: 'a', ended_at: minsAgo(5) });
    const b = match({ match_guid: 'b', ended_at: minsAgo(50) });
    expect(chronological([a, b]).map((m) => m.match_guid)).toEqual(['b', 'a']);
  });
});

describe('summarizeSession', () => {
  const session = (matches: SessionMatchDetail[], wins: number, losses: number): SessionResponse => ({
    session_id: 's',
    started_at: minsAgo(100),
    uptime: '1h 40m',
    total_matches: matches.length,
    total_wins: wins,
    total_losses: losses,
    win_rate: 0,
    playlists: {},
    matches,
  });

  it('derives the four KPIs (record, net MMR, goal diff, streak)', () => {
    const s = summarizeSession(
      session(
        [
          match({ ended_at: minsAgo(60), result: 'victory', blue_score: 2, orange_score: 0, mmr_change: 10 }),
          match({ ended_at: minsAgo(40), result: 'defeat', local_team: 1, blue_score: 3, orange_score: 2, mmr_change: -9 }),
          match({
            ended_at: minsAgo(20),
            result: 'victory',
            blue_score: 4,
            orange_score: 2,
            mmr_change: 10,
            players: [player({ is_local: true, stats: { score: 550, goals: 2, assists: 1, saves: 3, shots: 5, demos: 1 } })],
          }),
          match({ ended_at: minsAgo(5), result: 'victory', blue_score: 3, orange_score: 1, mmr_change: 9 }),
        ],
        3,
        1,
      ),
    );
    const kpi = Object.fromEntries(s.kpis.map((k) => [k.key, k]));
    expect(kpi.record.value).toBe('3–1');
    expect(kpi.record.sub).toBe('75% WIN RATE');
    expect(kpi.mmr.value).toBe('+20');
    expect(kpi.mmr.ink).toBe('text-win');
    // For: 2 + 2 (orange side) + 4 + 3 = 11; against: 0 + 3 + 2 + 1 = 6
    expect(kpi.goals.value).toBe('+5');
    expect(kpi.goals.sub).toBe('11 FOR · 6 AGAINST');
    expect(kpi.streak.value).toBe('W2');
    expect(kpi.streak.sub).toBe('BEST W2 · HIGH 550');
  });

  it('ends the current streak at a draw or unknown result', () => {
    const s = summarizeSession(
      session([match({ ended_at: minsAgo(30), result: 'victory' }), match({ ended_at: minsAgo(10), result: 'draw' })], 1, 0),
    );
    expect(s.streakType).toBeNull();
    expect(s.kpis.find((k) => k.key === 'streak')?.value).toBe('—');
  });

  it('handles a missing or 0-match mode-scoped session without cross-mode ALL PLAYLISTS sublabels', () => {
    const s = summarizeSession(null, '28');
    expect(s.kpis.find((k) => k.key === 'record')?.value).toBe('—');
    const mmr = s.kpis.find((k) => k.key === 'mmr')!;
    expect(mmr.value).toBe('±0');
    expect(mmr.sub).toBe('RANKED RUMBLE');
    expect(mmr.sub).not.toContain('ALL PLAYLISTS');
  });

  it('does not sum incompatible playlist MMRs together in multi-playlist sessions and filters by playlist cleanly', () => {
    const multi: SessionResponse = {
      ...session(
        [
          match({ match_guid: 'm1', playlist_id: 11, playlist_name: 'Ranked Doubles', ended_at: minsAgo(40), result: 'victory', mmr_change: 10 }),
          match({ match_guid: 'm2', playlist_id: 11, playlist_name: 'Ranked Doubles', ended_at: minsAgo(25), result: 'victory', mmr_change: 9 }),
          match({ match_guid: 'm3', playlist_id: 13, playlist_name: 'Ranked Standard', ended_at: minsAgo(10), result: 'victory', mmr_change: 12 }),
        ],
        3,
        0,
      ),
      playlists: {
        '11': {
          playlist_id: 11,
          playlist_name: 'Ranked Doubles',
          matches_played: 2,
          wins: 2,
          losses: 0,
          win_rate: 100,
          initial_mmr: 1466,
          current_mmr: 1485,
          mmr_delta: 19,
        },
        '13': {
          playlist_id: 13,
          playlist_name: 'Ranked Standard',
          matches_played: 1,
          wins: 1,
          losses: 0,
          win_rate: 100,
          initial_mmr: 1410,
          current_mmr: 1422,
          mmr_delta: 12,
        },
      },
      most_played_playlist: {
        id: 11,
        name: 'Ranked Doubles',
        net_mmr: 19,
        wins: 2,
        losses: 0,
        matches_count: 2,
      },
    };

    const s = summarizeSession(multi);
    const mmrKpi = s.kpis.find((k) => k.key === 'mmr')!;
    expect(mmrKpi.value).toBe('+19');
    expect(mmrKpi.sub).toBe('RANKED 2V2 MMR');

    const breakdown = summarizeSessionPlaylists(multi);
    expect(breakdown.map((b) => b.playlistId)).toEqual([11, 13]);
    expect(breakdown[0]).toMatchObject({
      playlistId: 11,
      filterKey: '11',
      wins: 2,
      losses: 0,
      matches: 2,
      currentMmr: 1485,
      netMmr: 19,
      winPct: '100%',
    });

    const filtered2v2 = filterSessionByPlaylist(multi, '2v2')!;
    expect(filtered2v2.total_matches).toBe(2);
    expect(filtered2v2.total_wins).toBe(2);
    expect(filtered2v2.total_losses).toBe(0);
    expect(filtered2v2.matches.map((m) => m.match_guid)).toEqual(['m1', 'm2']);
  });
});
