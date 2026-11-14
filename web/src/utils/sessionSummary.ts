import type { SessionMatchDetail, SessionMatchPlayer, SessionResponse } from '../types/api';
import {
  deltaInk,
  formatPct,
  formatRecord,
  formatSigned,
  localTeamOf,
  outcomeOf,
} from './broadcastLabels';

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

/** Session matches oldest → newest by end time (the API appends, but don't rely on it). */
export function chronological(matches: SessionMatchDetail[] | null | undefined): SessionMatchDetail[] {
  const time = (m: SessionMatchDetail) => new Date(m.ended_at || m.started_at).getTime() || 0;
  return [...(matches ?? [])].sort((a, b) => time(a) - time(b));
}

export function summarizeSession(session: SessionResponse | null): SessionSummary {
  const matches = chronological(session?.matches);
  let goalsFor = 0;
  let goalsAgainst = 0;
  let netMmr = 0;
  let highScore = 0;
  let bestWinStreak = 0;
  let run = 0;

  for (const m of matches) {
    const localTeam = localTeamOf(m);
    goalsFor += localTeam === 0 ? m.blue_score : m.orange_score;
    goalsAgainst += localTeam === 0 ? m.orange_score : m.blue_score;
    netMmr += m.mmr_change || 0;
    const local: SessionMatchPlayer | undefined = m.players?.find((p) => p.is_local);
    highScore = Math.max(highScore, local?.stats?.score || 0);
    if (m.result === 'victory') {
      run++;
      bestWinStreak = Math.max(bestWinStreak, run);
    } else if (m.result === 'defeat') {
      run = 0;
    }
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
      sub: 'ALL PLAYLISTS',
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
