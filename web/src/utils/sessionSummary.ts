import type {
  PlayerRoleStatsSummary,
  PlaylistSessionStats,
  SessionMatchDetail,
  SessionMatchPlayer,
  SessionResponse,
} from '../types/api';
import {
  deltaInk,
  formatPct,
  formatRecord,
  formatSigned,
  localTeamOf,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  outcomeOf,
  playlistLabels,
} from './broadcastLabels';
import { emptyRoleStats } from './roleStats';

export interface SessionKpi {
  key: string;
  label: string;
  value: string;
  sub: string;
  ink: string;
}

export interface SessionSummary {
  wins: number;
  losses: number;
  netMmr: number;
  goalsFor: number;
  goalsAgainst: number;
  streakType: 'W' | 'L' | null;
  streakCount: number;
  bestWinStreak: number;
  highScore: number;
  kpis: SessionKpi[];
}

export interface SessionPlaylistBreakdownItem {
  playlistId: number;
  filterKey: string;
  label: string;
  shortLabel: string;
  wins: number;
  losses: number;
  matches: number;
  currentMmr: number;
  netMmr: number;
  winPct: string;
}

/** Session matches oldest → newest by end time (the API appends, but don't rely on it). */
export function chronological(matches: SessionMatchDetail[] | null | undefined): SessionMatchDetail[] {
  const time = (m: SessionMatchDetail) => new Date(m.ended_at || m.started_at).getTime() || 0;
  return [...(matches ?? [])].sort((a, b) => time(a) - time(b));
}

function aggregateScopedRoleStats(
  matches: SessionMatchDetail[],
  totalGames: number,
  wins: number,
  losses: number,
  side: 'for' | 'against',
): PlayerRoleStatsSummary {
  if (totalGames === 0) return emptyRoleStats(0, 0, 0);

  let sampleCount = 0;
  let sumScore = 0;
  let sumGoals = 0;
  let sumAssists = 0;
  let sumSaves = 0;
  let sumShots = 0;
  let sumDemos = 0;

  for (const m of matches) {
    if (!m.players || m.players.length === 0) continue;
    const localTeam = localTeamOf(m);
    const relevant =
      side === 'for'
        ? m.players.filter((p) => p.is_local)
        : m.players.filter((p) => p.team_num !== localTeam && p.team_num !== 255);
    if (relevant.length === 0) continue;

    sampleCount++;
    const denom = relevant.length;
    sumScore += relevant.reduce((a, p) => a + (p.stats?.score || 0), 0) / denom;
    sumGoals += relevant.reduce((a, p) => a + (p.stats?.goals || 0), 0) / denom;
    sumAssists += relevant.reduce((a, p) => a + (p.stats?.assists || 0), 0) / denom;
    sumSaves += relevant.reduce((a, p) => a + (p.stats?.saves || 0), 0) / denom;
    sumShots += relevant.reduce((a, p) => a + (p.stats?.shots || 0), 0) / denom;
    sumDemos += relevant.reduce((a, p) => a + (p.stats?.demos || 0), 0) / denom;
  }

  if (sampleCount === 0) {
    return emptyRoleStats(totalGames, wins, losses);
  }

  const avg_score = sumScore / sampleCount;
  const avg_goals = sumGoals / sampleCount;
  const avg_assists = sumAssists / sampleCount;
  const avg_saves = sumSaves / sampleCount;
  const avg_shots = sumShots / sampleCount;
  const avg_demos_inflicted = sumDemos / sampleCount;
  const shooting_pct = avg_shots > 0 ? (avg_goals / avg_shots) * 100 : 0;

  return emptyRoleStats(totalGames, wins, losses, {
    avg_score,
    avg_goals,
    avg_assists,
    avg_saves,
    avg_shots,
    shooting_pct,
    avg_demos_inflicted,
    total_score: Math.round(avg_score * totalGames),
    total_goals: Math.round(avg_goals * totalGames),
    total_assists: Math.round(avg_assists * totalGames),
    total_saves: Math.round(avg_saves * totalGames),
    total_shots: Math.round(avg_shots * totalGames),
    total_demos_inflicted: Math.round(avg_demos_inflicted * totalGames),
  });
}

/**
 * Scopes a SessionResponse to a single playlist filter (`2v2`/`11`, `3v3`/`13`, `1v1`/`10`, `extra`, `casual`, etc.),
 * preserving server-computed `for_stats` / `against_stats` when already scoped to that playlist.
 */
export function filterSessionByPlaylist(
  session: SessionResponse | null,
  playlistFilter?: string,
): SessionResponse | null {
  if (!session || !playlistFilter || playlistFilter === 'all') return session;

  const allMatches = session.matches ?? [];
  const matches = allMatches.filter((m) => matchesPlaylistFilter(m.playlist_id, playlistFilter));

  const filteredPlaylists: Record<string, PlaylistSessionStats> = {};
  for (const [key, pl] of Object.entries(session.playlists ?? {})) {
    const pid = pl.playlist_id || Number(key);
    if (matchesPlaylistFilter(pid, playlistFilter)) {
      filteredPlaylists[key] = pl;
    }
  }

  const playlistEntries = Object.values(filteredPlaylists);
  const hasMatches = matches.length > 0;
  const total_wins = hasMatches
    ? matches.filter((m) => m.result === 'victory').length
    : playlistEntries.reduce((acc, p) => acc + (p.wins || 0), 0);
  const total_losses = hasMatches
    ? matches.filter((m) => m.result === 'defeat').length
    : playlistEntries.reduce((acc, p) => acc + (p.losses || 0), 0);
  const total_matches = hasMatches
    ? matches.length
    : playlistEntries.reduce((acc, p) => acc + (p.matches_played || 0), 0);
  const win_rate =
    total_wins + total_losses > 0 ? (total_wins / (total_wins + total_losses)) * 100 : 0;

  const alreadyServerScoped =
    allMatches.length > 0 &&
    matches.length === allMatches.length &&
    Object.keys(session.playlists ?? {}).length === playlistEntries.length;

  const topPl = [...playlistEntries].sort((a, b) => b.matches_played - a.matches_played)[0];
  const most_played_playlist = topPl
    ? {
        id: topPl.playlist_id,
        name: topPl.playlist_name,
        net_mmr: topPl.mmr_delta,
        wins: topPl.wins,
        losses: topPl.losses,
        matches_count: topPl.matches_played,
      }
    : matches.length > 0
      ? {
          id: matches[0].playlist_id,
          name: matches[0].playlist_name,
          net_mmr: matches.reduce((acc, m) => acc + (m.mmr_change || 0), 0),
          wins: total_wins,
          losses: total_losses,
          matches_count: total_matches,
        }
      : undefined;

  return {
    ...session,
    total_matches,
    total_wins,
    total_losses,
    win_rate,
    playlists: filteredPlaylists,
    matches,
    most_played_playlist,
    for_stats: alreadyServerScoped
      ? session.for_stats
      : aggregateScopedRoleStats(matches, total_matches, total_wins, total_losses, 'for'),
    against_stats: alreadyServerScoped
      ? session.against_stats
      : aggregateScopedRoleStats(matches, total_matches, total_losses, total_wins, 'against'),
  };
}

const PLAYLIST_PRIORITY: Record<number, number> = {
  11: 1,
  13: 2,
  10: 3,
  27: 4,
  28: 5,
  29: 6,
  30: 7,
  34: 8,
};

/** Returns sorted per-playlist session summaries for side-by-side mode breakdown strips. */
export function summarizeSessionPlaylists(
  session: SessionResponse | null,
): SessionPlaylistBreakdownItem[] {
  if (!session) return [];

  const byId = new Map<number, SessionPlaylistBreakdownItem>();
  for (const [key, pl] of Object.entries(session.playlists ?? {})) {
    const pid = pl.playlist_id || Number(key);
    if (!pid) continue;
    const wins = pl.wins || 0;
    const losses = pl.losses || 0;
    const matches = pl.matches_played || wins + losses;
    if (matches <= 0 && (pl.mmr_delta || 0) === 0) continue;
    const labels = playlistLabels(pid, pl.playlist_name);
    byId.set(pid, {
      playlistId: pid,
      filterKey: modeKeyFromPlaylistId(pid),
      label: labels.name,
      shortLabel: labels.short,
      wins,
      losses,
      matches,
      currentMmr: pl.current_mmr || 0,
      netMmr: pl.mmr_delta || 0,
      winPct: formatPct(wins, losses),
    });
  }

  for (const m of chronological(session.matches)) {
    const pid = m.playlist_id;
    if (!pid || byId.has(pid)) continue;
    const same = (session.matches ?? []).filter((x) => x.playlist_id === pid);
    const wins = same.filter((x) => x.result === 'victory').length;
    const losses = same.filter((x) => x.result === 'defeat').length;
    const latest = same[same.length - 1];
    const labels = playlistLabels(pid, m.playlist_name);
    byId.set(pid, {
      playlistId: pid,
      filterKey: modeKeyFromPlaylistId(pid),
      label: labels.name,
      shortLabel: labels.short,
      wins,
      losses,
      matches: same.length,
      currentMmr: latest?.ending_mmr || 0,
      netMmr: same.reduce((acc, x) => acc + (x.mmr_change || 0), 0),
      winPct: formatPct(wins, losses),
    });
  }

  return Array.from(byId.values()).sort((a, b) => {
    if (b.matches !== a.matches) return b.matches - a.matches;
    const pa = PLAYLIST_PRIORITY[a.playlistId] ?? 99;
    const pb = PLAYLIST_PRIORITY[b.playlistId] ?? 99;
    if (pa !== pb) return pa - pb;
    return a.playlistId - b.playlistId;
  });
}

export function summarizeSession(session: SessionResponse | null): SessionSummary {
  const matches = chronological(session?.matches);
  let goalsFor = 0;
  let goalsAgainst = 0;
  let summedMmr = 0;
  let highScore = 0;
  let bestWinStreak = 0;
  let run = 0;

  const distinctPlaylists = new Set<number>();
  const perPlaylist = new Map<number, { count: number; netMmr: number; name: string }>();

  for (const m of matches) {
    const localTeam = localTeamOf(m);
    goalsFor += localTeam === 0 ? m.blue_score : m.orange_score;
    goalsAgainst += localTeam === 0 ? m.orange_score : m.blue_score;
    summedMmr += m.mmr_change || 0;
    if (m.playlist_id > 0) {
      distinctPlaylists.add(m.playlist_id);
      const prev = perPlaylist.get(m.playlist_id) ?? {
        count: 0,
        netMmr: 0,
        name: m.playlist_name,
      };
      prev.count += 1;
      prev.netMmr += m.mmr_change || 0;
      perPlaylist.set(m.playlist_id, prev);
    }
    const local: SessionMatchPlayer | undefined = m.players?.find((p) => p.is_local);
    highScore = Math.max(highScore, local?.stats?.score || 0);
    if (m.result === 'victory') {
      run++;
      bestWinStreak = Math.max(bestWinStreak, run);
    } else if (m.result === 'defeat') {
      run = 0;
    }
  }

  let netMmr = summedMmr;
  let mmrSub = 'ALL PLAYLISTS';

  if (distinctPlaylists.size > 1) {
    if (session?.most_played_playlist && session.most_played_playlist.id > 0) {
      netMmr = session.most_played_playlist.net_mmr;
      mmrSub = `${playlistLabels(session.most_played_playlist.id, session.most_played_playlist.name).mode} MMR`;
    } else {
      const sorted = Array.from(perPlaylist.entries()).sort((a, b) => b[1].count - a[1].count);
      if (sorted.length > 0) {
        const [topId, topInfo] = sorted[0];
        const plDelta = session?.playlists?.[String(topId)]?.mmr_delta;
        netMmr = plDelta !== undefined ? plDelta : topInfo.netMmr;
        mmrSub = `${playlistLabels(topId, topInfo.name).mode} MMR`;
      }
    }
  } else if (distinctPlaylists.size === 1) {
    const [onlyId] = Array.from(distinctPlaylists);
    mmrSub = onlyId ? playlistLabels(onlyId).mode : 'ALL PLAYLISTS';
  } else if (session?.most_played_playlist && session.most_played_playlist.id > 0) {
    netMmr = session.most_played_playlist.net_mmr;
    mmrSub = playlistLabels(session.most_played_playlist.id, session.most_played_playlist.name).mode;
  }

  let streakType: 'W' | 'L' | null = null;
  let streakCount = 0;
  for (let i = matches.length - 1; i >= 0; i--) {
    const o = outcomeOf(matches[i].result);
    if (o !== 'W' && o !== 'L') break;
    if (streakType === null) {
      streakType = o;
      streakCount = 1;
    } else if (o === streakType) {
      streakCount++;
    } else {
      break;
    }
  }

  const wins = session?.total_wins ?? matches.filter((m) => m.result === 'victory').length;
  const losses = session?.total_losses ?? matches.filter((m) => m.result === 'defeat').length;
  const goalDiff = goalsFor - goalsAgainst;
  const winPct = formatPct(wins, losses);

  const kpis: SessionKpi[] = [
    {
      key: 'record',
      label: 'RECORD',
      value: formatRecord(wins, losses),
      sub: winPct ? `${winPct} WIN RATE` : 'NO RESULTS YET',
      ink: 'text-ink',
    },
    {
      key: 'mmr',
      label: 'NET MMR',
      value: formatSigned(netMmr),
      sub: mmrSub,
      ink: deltaInk(netMmr),
    },
    {
      key: 'goals',
      label: 'GOAL DIFF',
      value: formatSigned(goalDiff),
      sub: `${goalsFor} FOR · ${goalsAgainst} AGAINST`,
      ink: 'text-ink',
    },
    {
      key: 'streak',
      label: 'STREAK',
      value: streakType ? `${streakType}${streakCount}` : '—',
      sub: `BEST W${bestWinStreak} · HIGH ${highScore}`,
      ink: 'text-ink',
    },
  ];

  return { wins, losses, netMmr, goalsFor, goalsAgainst, streakType, streakCount, bestWinStreak, highScore, kpis };
}
