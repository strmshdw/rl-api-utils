import type { PlayerDetailResponse, PlayerMatchDetail, PlayerSummary } from '../types/api';
import { SAMPLE_HISTORICAL_MATCHES } from './historyFixtures';
import { isoAgo, serializeRanks, synthesizeRoleStats } from './syntheticStats';

export const SAMPLE_PLAYERS: PlayerSummary[] = [
  {
    player_id: 'Steam|76561198000000010|0',
    platform: 'Steam',
    player_name: 'AeroPulse',
    ranks_json: /*#__PURE__*/ serializeRanks({
      '10': { playlist_id: 10, playlist_name: 'Ranked Duel', tier: 15, division: 2, rank_name: 'Champion III', mmr: 1245, matches_played: 34 },
      '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 16, division: 3, rank_name: 'Grand Champion I', mmr: 1502, matches_played: 142 },
      '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 18, division: 2, rank_name: 'Grand Champion III', mmr: 1795, matches_played: 88 },
    }),
    first_seen_at: '2026-08-14T12:00:00Z',
    last_seen_at: /*#__PURE__*/ isoAgo(9 * 60000),
    total_wins_as_teammate: 4,
    total_losses_as_teammate: 1,
    total_wins_as_opponent: 2,
    total_losses_as_opponent: 1,
    total_matches: 8,
    with_you_stats: /*#__PURE__*/ synthesizeRoleStats(5, 4, 1, {
      avg_score: 446,
      avg_goals: 1.6,
      avg_assists: 1.2,
      avg_saves: 1.8,
      avg_shots: 4.1,
      shooting_pct: 39,
      avg_demos_inflicted: 1.4,
      avg_bpm: 438,
      avg_boost: 48,
      avg_supersonic_pct: 19,
    }),
    against_you_stats: /*#__PURE__*/ synthesizeRoleStats(3, 2, 1, {
      avg_score: 412,
      avg_goals: 1.3,
      avg_assists: 0.7,
      avg_saves: 1.7,
      avg_shots: 3.7,
      shooting_pct: 35,
      avg_demos_inflicted: 1.0,
      avg_bpm: 415,
      avg_boost: 45,
      avg_supersonic_pct: 17,
    }),
  },
  {
    player_id: 'Steam|76561198000000020|0',
    platform: 'Steam',
    player_name: 'VortexRL',
    ranks_json: /*#__PURE__*/ serializeRanks({
      '10': { playlist_id: 10, playlist_name: 'Ranked Duel', tier: 16, division: 1, rank_name: 'Grand Champion I', mmr: 1310, matches_played: 45 },
      '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 15, division: 4, rank_name: 'Champion III', mmr: 1414, matches_played: 110 },
      '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 19, division: 1, rank_name: 'Supersonic Legend', mmr: 1890, matches_played: 215 },
    }),
    first_seen_at: '2026-08-20T15:30:00Z',
    last_seen_at: /*#__PURE__*/ isoAgo(18 * 60000),
    total_wins_as_teammate: 1,
    total_losses_as_teammate: 0,
    total_wins_as_opponent: 2,
    total_losses_as_opponent: 3,
    total_matches: 6,
    with_you_stats: /*#__PURE__*/ synthesizeRoleStats(1, 1, 0, { avg_score: 410, avg_goals: 1.0, avg_assists: 1.0, avg_saves: 2.0 }),
    against_you_stats: /*#__PURE__*/ synthesizeRoleStats(5, 2, 3, { avg_score: 468, avg_goals: 1.8, avg_assists: 1.0, avg_saves: 2.2 }),
  },
  {
    player_id: 'PS4|psn_zenith99|0',
    platform: 'PlayStation',
    player_name: 'Zenith_99',
    ranks_json: /*#__PURE__*/ serializeRanks({
      '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 17, division: 2, rank_name: 'Grand Champion II', mmr: 1580, matches_played: 190 },
      '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 17, division: 1, rank_name: 'Grand Champion II', mmr: 1595, matches_played: 95 },
    }),
    first_seen_at: '2026-09-02T19:00:00Z',
    last_seen_at: /*#__PURE__*/ isoAgo(35 * 60000),
    total_wins_as_teammate: 0,
    total_losses_as_teammate: 0,
    total_wins_as_opponent: 1,
    total_losses_as_opponent: 3,
    total_matches: 4,
    with_you_stats: /*#__PURE__*/ synthesizeRoleStats(0, 0, 0),
    against_you_stats: /*#__PURE__*/ synthesizeRoleStats(4, 1, 3, { avg_score: 492, avg_goals: 2.0, avg_assists: 1.0, avg_saves: 2.5 }),
  },
  {
    player_id: 'Epic|dan_rl|0',
    platform: 'Epic',
    player_name: 'DanielRL',
    ranks_json: /*#__PURE__*/ serializeRanks({
      '10': { playlist_id: 10, playlist_name: 'Ranked Duel', tier: 19, division: 1, rank_name: 'Supersonic Legend', mmr: 1520, matches_played: 120 },
      '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 18, division: 4, rank_name: 'Grand Champion III', mmr: 1810, matches_played: 240 },
      '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 19, division: 1, rank_name: 'Supersonic Legend', mmr: 1960, matches_played: 180 },
    }),
    first_seen_at: '2026-09-10T21:00:00Z',
    last_seen_at: /*#__PURE__*/ isoAgo(120 * 60000),
    total_wins_as_teammate: 0,
    total_losses_as_teammate: 0,
    total_wins_as_opponent: 0,
    total_losses_as_opponent: 3,
    total_matches: 3,
    with_you_stats: /*#__PURE__*/ synthesizeRoleStats(0, 0, 0),
    against_you_stats: /*#__PURE__*/ synthesizeRoleStats(3, 0, 3, { avg_score: 615, avg_goals: 2.3, avg_assists: 1.0, avg_saves: 3.7 }),
  },
  {
    player_id: 'Steam|beastmode_rl|0',
    platform: 'Steam',
    player_name: 'BeastMode',
    ranks_json: /*#__PURE__*/ serializeRanks({
      '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 17, division: 4, rank_name: 'Grand Champion II', mmr: 1645, matches_played: 155 },
      '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 19, division: 1, rank_name: 'Supersonic Legend', mmr: 1915, matches_played: 170 },
    }),
    first_seen_at: '2026-09-12T18:00:00Z',
    last_seen_at: /*#__PURE__*/ isoAgo(240 * 60000),
    total_wins_as_teammate: 3,
    total_losses_as_teammate: 1,
    total_wins_as_opponent: 1,
    total_losses_as_opponent: 2,
    total_matches: 7,
    with_you_stats: /*#__PURE__*/ synthesizeRoleStats(4, 3, 1, { avg_score: 488, avg_goals: 1.8, avg_assists: 1.3, avg_saves: 2.1 }),
    against_you_stats: /*#__PURE__*/ synthesizeRoleStats(3, 1, 2, { avg_score: 455, avg_goals: 1.5, avg_assists: 1.2, avg_saves: 2.4 }),
  },
];

export function getSamplePlayerProfile(playerId: string, playerName?: string): PlayerDetailResponse {
  const lowerId = (playerId || '').toLowerCase();
  const lowerName = (playerName || '').toLowerCase();
  const isLocalDemo =
    lowerId === 'me' ||
    lowerId === 'you' ||
    lowerId.includes('demo_local_player') ||
    lowerName === 'you' ||
    lowerName === 'you (demo)';

  const found =
    SAMPLE_PLAYERS.find(
      (p) =>
        p.player_id === playerId ||
        p.player_name.toLowerCase() === lowerName ||
        lowerId.includes(p.player_name.toLowerCase()),
    ) || SAMPLE_PLAYERS[0];

  return {
    player_id: playerId || found.player_id,
    platform: isLocalDemo ? 'Epic' : found.platform,
    player_name: isLocalDemo ? 'You' : playerName || found.player_name,
    ranks_json: found.ranks_json,
    notes: isLocalDemo
      ? 'Local player profile.'
      : 'High-tempo rotational player; strong backboard defense and fast 50/50 challenges.',
    tags: isLocalDemo ? ['LOCAL PLAYER'] : ['FAST ROTATOR', 'BACKBOARD', 'HIGH BPM'],
    relationship_tag: isLocalDemo ? 'YOU' : undefined,
    first_seen_at: found.first_seen_at,
    last_seen_at: found.last_seen_at,
    with_you_stats: found.with_you_stats,
    against_you_stats: isLocalDemo ? synthesizeRoleStats(0, 0, 0) : found.against_you_stats,
    matchups: [
      {
        player_id: playerId || found.player_id,
        playlist_id: 11,
        wins_as_teammate: found.total_wins_as_teammate,
        losses_as_teammate: found.total_losses_as_teammate,
        wins_as_opponent: isLocalDemo ? 0 : found.total_wins_as_opponent,
        losses_as_opponent: isLocalDemo ? 0 : found.total_losses_as_opponent,
        total_matches: found.total_matches,
        last_played_at: found.last_seen_at,
      },
      {
        player_id: playerId || found.player_id,
        playlist_id: 13,
        wins_as_teammate: 1,
        losses_as_teammate: 0,
        wins_as_opponent: isLocalDemo ? 0 : 1,
        losses_as_opponent: 0,
        total_matches: isLocalDemo ? 1 : 2,
        last_played_at: found.last_seen_at,
      },
    ],
  };
}

export function getSamplePlayerMatches(playerId: string, playerName?: string): PlayerMatchDetail[] {
  const pNameLower = (playerName || 'AeroPulse').toLowerCase();
  return SAMPLE_HISTORICAL_MATCHES.map((m, idx) => {
    const role: 'teammate' | 'opponent' = idx % 2 === 0 ? 'teammate' : 'opponent';
    const localResult = m.result || (m.winner_team === 0 ? 'victory' : 'defeat');
    const pl = m.stats?.players?.find((x) => x.name?.toLowerCase() === pNameLower) || m.stats?.players?.[1];
    return {
      ...m,
      result: localResult,
      local_result: localResult,
      role,
      player_stats: {
        match_guid: m.match_guid,
        player_id: playerId,
        team_num: role === 'teammate' ? 0 : 1,
        is_local: false,
        won: localResult === 'victory',
        score: pl?.score ?? 410,
        goals: pl?.goals ?? 2,
        assists: pl?.assists ?? 1,
        saves: pl?.saves ?? 2,
        shots: pl?.shots ?? 4,
        demos: pl?.demos ?? 1,
      },
    };
  });
}
