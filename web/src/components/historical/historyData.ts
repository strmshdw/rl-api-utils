import { HistoricalMatchRecord, ReplayPlayerStats, SessionMatchDetail } from '../../types/api';
import { Outcome, outcomeOf, onlineIdOf } from '../../utils/broadcast';
import { SAMPLE_HISTORICAL_MATCHES } from '../../utils/sampleData';
import { BoxScoreRow } from '../common/BoxScore';

export const isLocal = (p: ReplayPlayerStats, ids: ReadonlySet<string>): boolean => {
  if (p.is_bot) return false;
  if (p.is_local) return true;
  if (!p.online_id || p.online_id === '0') return false;
  const onlineId = p.online_id.toLowerCase();
  if (ids.has(onlineId)) return true;
  for (const raw of ids) {
    const lower = raw.toLowerCase();
    if (lower === onlineId) return true;
    if (lower.includes('|')) {
      const parts = lower.split('|');
      if (parts[1] === onlineId) return true;
    }
  }
  return false;
};

/** W/L from the local player's side when we can find them in the replay, else the stored result. */
export function historyOutcome(m: HistoricalMatchRecord, localIds: ReadonlySet<string>): Outcome {
  const local = m.stats?.players?.find((p) => isLocal(p, localIds));
  if (local && (local.team === 0 || local.team === 1)) {
    const us = local.team === 0 ? m.blue_score : m.orange_score;
    const them = local.team === 0 ? m.orange_score : m.blue_score;
    if (us > them) return 'W';
    if (us < them) return 'L';
    if (m.winner_team === local.team) return 'W';
    if (m.winner_team !== undefined && m.winner_team !== null && m.winner_team >= 0 && m.winner_team !== local.team) return 'L';
    return us || them ? 'D' : null;
  }
  if (m.result) return outcomeOf(m.result);
  if (m.blue_score > m.orange_score) return 'W';
  if (m.orange_score > m.blue_score) return 'L';
  return null;
}

export interface ReplayStatus {
  label: string;
  ink: string;
  href?: string;
  title?: string;
}

export function replayStatus(m: HistoricalMatchRecord): ReplayStatus {
  const bcId = m.ballchasing_id || m.stats?.ballchasing_id;
  let url = m.ballchasing_url || m.stats?.ballchasing_url;
  if (url && url.includes('/api/replays/')) {
    url = url.replace('https://ballchasing.com/api/replays/', 'https://ballchasing.com/replay/');
  } else if (!url && bcId) {
    url = `https://ballchasing.com/replay/${bcId}`;
  }
  const upload = (m.upload_status || '').toUpperCase();
  const download = (m.download_status || '').toUpperCase();
  const statsSync = (m.stats_sync_status || '').toUpperCase();
  if (url || bcId || upload === 'UPLOADED' || upload === 'DUPLICATE' || statsSync === 'SYNCED') {
    return { label: 'BALLCHASING ↗', ink: 'text-ink', href: url || undefined };
  }
  if (download === 'FAILED' || upload === 'FAILED') {
    const tries = m.retry_count > 0 ? ` (${m.retry_count} ${m.retry_count === 1 ? 'ATTEMPT' : 'ATTEMPTS'})` : '';
    return { label: 'FAILED', ink: 'text-loss', title: `${m.last_error || 'Replay sync failed'}${tries}` };
  }
  if (upload === 'UPLOADING') return { label: 'UPLOADING…', ink: 'text-muted' };
  if (download === 'DOWNLOADED') return { label: 'LOCAL ONLY', ink: 'text-muted' };
  if (download === 'PENDING' || download === 'DOWNLOADING') return { label: 'DOWNLOADING…', ink: 'text-muted' };
  return { label: 'NO REPLAY', ink: 'text-faint' };
}

/**
 * Filters a replay's player array to a single team, sorted by score descending,
 * and deduplicates reconnect duplicate entries (including repeated "You" entries),
 * keeping the higher-score entry.
 */
export function dedupeTeamPlayers(
  players: ReplayPlayerStats[],
  team: number,
  localIds: ReadonlySet<string>,
): ReplayPlayerStats[] {
  const sorted = (players || [])
    .filter((p) => p.team === team)
    .sort((a, b) => (b.score || 0) - (a.score || 0));

  let seenLocal = false;
  const seenKeys = new Set<string>();
  const out: ReplayPlayerStats[] = [];

  for (const p of sorted) {
    const you = isLocal(p, localIds);
    if (you) {
      if (seenLocal) continue;
      seenLocal = true;
    }
    const idKey =
      p.online_id && p.online_id !== '0'
        ? onlineIdOf(p.online_id) || p.online_id.trim().toLowerCase()
        : '';
    const nameKey = (p.name || '').trim().toLowerCase();
    if ((idKey && seenKeys.has(`id:${idKey}`)) || (nameKey && seenKeys.has(`name:${nameKey}`))) {
      continue;
    }
    if (idKey) seenKeys.add(`id:${idKey}`);
    if (nameKey) seenKeys.add(`name:${nameKey}`);
    out.push(p);
  }

  return out;
}

export function teamRows(
  players: ReplayPlayerStats[],
  team: number,
  localIds: ReadonlySet<string>,
  inspectedIdOrName?: string,
): BoxScoreRow[] {
  const inspectedLower = inspectedIdOrName?.toLowerCase();
  const inspectedOnlineId = inspectedLower?.includes('|') ? inspectedLower.split('|')[1] : inspectedLower;
  return dedupeTeamPlayers(players, team, localIds).map((p, i) => {
    const you = isLocal(p, localIds);
    const isInspected = Boolean(
      inspectedLower &&
        !you &&
        (p.online_id?.toLowerCase() === inspectedLower ||
          p.online_id?.toLowerCase() === inspectedOnlineId ||
          p.name?.toLowerCase() === inspectedLower),
    );
    return {
      key: `${p.online_id || p.name}-${i}`,
      playerId: p.online_id && p.online_id !== '0' ? p.online_id : p.name,
      name: you ? 'You' : p.name,
      you,
      inspected: isInspected,
      score: p.score,
      goals: p.goals,
      assists: p.assists,
      saves: p.saves,
      shots: p.shots,
      demos: p.demos,
      mmr: p.mmr,
      rank: p.rank_name || p.rank?.name || p.short_rank || p.rank?.short_rank,
      shortRank: p.short_rank || p.rank?.short_rank,
      tier: p.tier ?? p.rank?.tier,
      division: p.division ?? p.rank?.division,
    };
  });
}

export function playerNames(players: ReplayPlayerStats[], localIds: ReadonlySet<string>): string {
  const side = (team: number) =>
    dedupeTeamPlayers(players, team, localIds)
      .map((p) => (isLocal(p, localIds) ? 'You' : p.name))
      .join(', ');
  return [side(0), side(1)].filter(Boolean).join(' vs ');
}

export interface MatchSummary {
  blue_score: number;
  orange_score: number;
  stats?: {
    players?: ReplayPlayerStats[];
  };
  player_stats?: {
    team_num?: number;
    is_local?: boolean;
  };
  role?: 'teammate' | 'opponent' | string;
}

export interface OrientedMatchSummary {
  myTeam: 0 | 1;
  myScore: number;
  oppScore: number;
  myTeamInk: string;
  rosterText: string;
}

/** Orients score and roster summary ("You + Mate vs Opp") from the local player's perspective. */
export function orientedMatchSummary(
  m: MatchSummary,
  localIds: ReadonlySet<string>,
): OrientedMatchSummary {
  const players = m.stats?.players ?? [];
  const local = players.find((p) => isLocal(p, localIds));

  let myTeam: 0 | 1 = 0;
  if (local && (local.team === 0 || local.team === 1)) {
    myTeam = local.team;
  } else if (m.player_stats && (m.player_stats.team_num === 0 || m.player_stats.team_num === 1)) {
    if (m.player_stats.is_local || m.role === 'teammate') {
      myTeam = m.player_stats.team_num;
    } else if (m.role === 'opponent') {
      myTeam = m.player_stats.team_num === 0 ? 1 : 0;
    }
  }

  const oppTeam: 0 | 1 = myTeam === 0 ? 1 : 0;
  const myScore = myTeam === 0 ? m.blue_score : m.orange_score;
  const oppScore = myTeam === 0 ? m.orange_score : m.blue_score;
  const myTeamInk = myTeam === 0 ? 'text-blue-ink' : 'text-orange-ink';

  const myPlayers = dedupeTeamPlayers(players, myTeam, localIds);
  const oppPlayers = dedupeTeamPlayers(players, oppTeam, localIds);

  let rosterText = '';
  if (myPlayers.length > 0 || oppPlayers.length > 0) {
    const hasLocalOnMySide = myPlayers.some((p) => isLocal(p, localIds));
    const mates = myPlayers.filter((p) => !isLocal(p, localIds)).map((p) => p.name);
    const us = hasLocalOnMySide
      ? ['You', mates.join(', ')].filter(Boolean).join(' + ')
      : mates.join(', ');
    const opps = oppPlayers.map((p) => (isLocal(p, localIds) ? 'You' : p.name)).join(', ');
    rosterText = opps ? (us ? `${us} vs ${opps}` : opps) : us;
  }

  return {
    myTeam,
    myScore,
    oppScore,
    myTeamInk,
    rosterText,
  };
}

export function sessionMatchToHistoricalRecord(match: SessionMatchDetail): HistoricalMatchRecord {
  const sample = SAMPLE_HISTORICAL_MATCHES.find((m) => m.match_guid === match.match_guid);
  const samplePlayers = sample?.stats?.players ?? [];
  const players: ReplayPlayerStats[] = (match.players || []).map((p) => {
    const oid = onlineIdOf(p.player_id) || p.player_id;
    const sp = samplePlayers.find(
      (s) =>
        s.online_id?.toLowerCase() === oid.toLowerCase() ||
        s.name?.toLowerCase() === p.name?.toLowerCase(),
    );
    return {
      name: p.is_local ? 'You' : p.name,
      platform: p.platform || 'Steam',
      online_id: oid,
      is_bot: p.is_bot,
      is_local: p.is_local,
      team: p.team_num,
      score: p.stats?.score ?? sp?.score ?? 0,
      goals: p.stats?.goals ?? sp?.goals ?? 0,
      assists: p.stats?.assists ?? sp?.assists ?? 0,
      saves: p.stats?.saves ?? sp?.saves ?? 0,
      shots: p.stats?.shots ?? sp?.shots ?? 0,
      demos: p.stats?.demos ?? sp?.demos ?? 0,
      mmr: p.mmr ?? sp?.mmr,
      rank_name: p.rank_name ?? sp?.rank_name,
      short_rank: p.short_rank ?? sp?.short_rank,
      tier: p.tier ?? sp?.tier,
      division: p.division ?? sp?.division,
      boost: sp?.boost,
      movement: sp?.movement,
      positioning: sp?.positioning,
      demo: sp?.demo,
    };
  });

  return {
    match_guid: match.match_guid,
    record_start_timestamp: Math.floor(new Date(match.ended_at).getTime() / 1000) || 0,
    map_name: sample?.map_name || '',
    playlist: match.playlist_id,
    replay_url: sample?.replay_url || '',
    download_status: sample?.download_status || 'PENDING',
    local_file_path: sample?.local_file_path || '',
    upload_status: sample?.upload_status || 'PENDING',
    ballchasing_id: sample?.ballchasing_id || '',
    ballchasing_url: sample?.ballchasing_url || '',
    retry_count: 0,
    last_error: '',
    blue_score: match.blue_score,
    orange_score: match.orange_score,
    winner_team: match.winner_team,
    replay_stats_json: sample?.replay_stats_json || '',
    created_at: match.ended_at,
    updated_at: match.ended_at,
    result: match.result,
    stats: {
      match_guid: match.match_guid,
      map_name: sample?.map_name || '',
      playlist: match.playlist_id,
      blue_score: match.blue_score,
      orange_score: match.orange_score,
      winner_team: match.winner_team,
      duration: match.duration_seconds,
      overtime: sample?.stats?.overtime ?? match.duration_seconds > 300,
      date: match.ended_at,
      ballchasing_id: sample?.ballchasing_id,
      ballchasing_url: sample?.ballchasing_url,
      players,
    },
  };
}
