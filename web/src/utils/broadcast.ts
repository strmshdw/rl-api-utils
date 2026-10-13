// Display helpers for the "Broadcast" dashboard: record colours, H2H cells,
// uppercase relative times, playlist labels and session KPI derivation.
import { SessionMatchDetail, SessionMatchPlayer, SessionResponse } from '../types/api';
import { parsePlatform } from './platforms';

// ============================================================================
// Records, cells & signed numbers
// ============================================================================

export type CellKind = 'win' | 'loss' | 'empty';

export const CELL_BG: Record<CellKind, string> = {
  win: 'bg-win',
  loss: 'bg-loss',
  empty: 'bg-line-2',
};

/**
 * Builds an n-cell W/L strip: wins first, then losses, then empty cells.
 * Records with more games than cells are scaled so the split stays honest.
 */
export function recordCells(wins: number, losses: number, n = 5): CellKind[] {
  const w = Math.max(0, wins || 0);
  const l = Math.max(0, losses || 0);
  const total = w + l;
  let greens = w;
  let reds = l;
  if (total > n) {
    greens = Math.round((w / total) * n);
    reds = n - greens;
  }
  const cells: CellKind[] = [];
  for (let i = 0; i < greens; i++) cells.push('win');
  for (let i = 0; i < reds; i++) cells.push('loss');
  while (cells.length < n) cells.push('empty');
  return cells;
}

/** Text colour for a W–L record: green when ahead, red when behind. */
export function recordInk(wins: number, losses: number): string {
  if (wins + losses === 0) return 'text-faint';
  if (wins > losses) return 'text-win';
  if (wins < losses) return 'text-loss';
  return 'text-ink-2';
}

/** Solid fill matching recordInk, for tag chips. */
export function recordFill(wins: number, losses: number): string {
  if (wins > losses) return 'bg-win';
  if (wins < losses) return 'bg-loss';
  return 'bg-ink-2';
}

export function formatRecord(wins: number, losses: number): string {
  if (wins + losses === 0) return '—';
  return `${wins}–${losses}`;
}

export function formatPct(wins: number, losses: number): string {
  const total = wins + losses;
  if (total === 0) return '';
  return `${Math.round((wins / total) * 100)}%`;
}

/** Signed integer: "+31", "-9", or "±0". */
export function formatSigned(value: number | null | undefined): string {
  const n = Math.round(Number(value) || 0);
  if (n === 0) return '±0';
  return n > 0 ? `+${n}` : `${n}`;
}

export function deltaInk(value: number | null | undefined): string {
  const n = Math.round(Number(value) || 0);
  if (n > 0) return 'text-win';
  if (n < 0) return 'text-loss';
  return 'text-muted';
}

/** Zero values are drawn in the faint colour. */
export function zeroInk(value: number, nonZero = 'text-ink'): string {
  return value ? nonZero : 'text-faint';
}

export function formatMmr(mmr: number | null | undefined): string {
  if (mmr == null || !Number.isFinite(mmr) || mmr <= 0) return '—';
  return Math.round(mmr).toString();
}

// ============================================================================
// Time
// ============================================================================

const MONTHS = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];

function parseDate(dateStr: string | null | undefined): Date | null {
  if (!dateStr) return null;
  const d = new Date(dateStr);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** "9 MIN AGO", "3H AGO", "YESTERDAY", "4 DAYS AGO", "SEP 19". Pass suffix=false for "9 MIN". */
export function formatAgo(dateStr: string | null | undefined, suffix = true, now: Date = new Date()): string {
  const date = parseDate(dateStr);
  if (!date) return '—';
  const ago = suffix ? ' AGO' : '';
  const mins = Math.floor((now.getTime() - date.getTime()) / 60000);
  if (mins < 1) return 'JUST NOW';
  if (mins < 60) return `${mins} MIN${ago}`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}H${ago}`;
  const days = Math.floor(hours / 24);
  if (days === 1) return 'YESTERDAY';
  if (days < 7) return `${days} DAYS${ago}`;
  const sameYear = date.getFullYear() === now.getFullYear();
  return `${MONTHS[date.getMonth()]} ${String(date.getDate()).padStart(2, '0')}${sameYear ? '' : `, ${date.getFullYear()}`}`;
}

/** "AUG 14, 2026" */
export function formatShortDate(dateStr: string | null | undefined): string {
  const date = parseDate(dateStr);
  if (!date) return '—';
  return `${MONTHS[date.getMonth()]} ${String(date.getDate()).padStart(2, '0')}, ${date.getFullYear()}`;
}

/** Elapsed time since a timestamp as "1H 40M" (or "40M"). */
export function formatElapsed(dateStr: string | null | undefined, now: Date = new Date()): string {
  const date = parseDate(dateStr);
  if (!date) return '—';
  const mins = Math.max(0, Math.floor((now.getTime() - date.getTime()) / 60000));
  const hours = Math.floor(mins / 60);
  return hours > 0 ? `${hours}H ${mins % 60}M` : `${mins}M`;
}

// ============================================================================
// Playlists & platforms
// ============================================================================

export interface PlaylistLabels {
  /** "RANKED DOUBLES" */
  name: string;
  /** "DOUBLES" */
  short: string;
  /** "RANKED 2V2" */
  mode: string;
}

const PLAYLISTS: Record<number, PlaylistLabels> = {
  1: { name: 'CASUAL DUEL', short: 'DUEL', mode: 'CASUAL 1V1' },
  2: { name: 'CASUAL DOUBLES', short: 'DOUBLES', mode: 'CASUAL 2V2' },
  3: { name: 'CASUAL STANDARD', short: 'STANDARD', mode: 'CASUAL 3V3' },
  4: { name: 'CASUAL CHAOS', short: 'CHAOS', mode: 'CASUAL 4V4' },
  10: { name: 'RANKED DUEL', short: 'DUEL', mode: 'RANKED 1V1' },
  11: { name: 'RANKED DOUBLES', short: 'DOUBLES', mode: 'RANKED 2V2' },
  12: { name: 'RANKED SOLO STANDARD', short: 'SOLO STANDARD', mode: 'RANKED SOLO 3V3' },
  13: { name: 'RANKED STANDARD', short: 'STANDARD', mode: 'RANKED 3V3' },
  27: { name: 'RANKED HOOPS', short: 'HOOPS', mode: 'RANKED HOOPS' },
  28: { name: 'RANKED RUMBLE', short: 'RUMBLE', mode: 'RANKED RUMBLE' },
  29: { name: 'RANKED DROPSHOT', short: 'DROPSHOT', mode: 'RANKED DROPSHOT' },
  30: { name: 'RANKED SNOW DAY', short: 'SNOW DAY', mode: 'RANKED SNOW DAY' },
  34: { name: 'TOURNAMENT', short: 'TOURNAMENT', mode: 'TOURNAMENT' },
};

export function playlistLabels(id: number | null | undefined, fallbackName?: string): PlaylistLabels {
  const known = id != null ? PLAYLISTS[id] : undefined;
  if (known) return known;
  const cleaned = (fallbackName || (id ? `Playlist ${id}` : 'Match')).replace(/\s*\(.*?\)\s*/g, ' ').trim().toUpperCase();
  return { name: cleaned, short: cleaned, mode: cleaned };
}

/** "STEAM", "EPIC", "PLAYSTATION", "XBOX", "SWITCH", "BOT" */
export function platformLabel(platformOrId: string | null | undefined, isBot = false): string {
  if (isBot) return 'BOT';
  const raw = (platformOrId || '').toLowerCase();
  if (raw.startsWith('ps4') || raw.startsWith('ps5')) return 'PLAYSTATION';
  if (raw.startsWith('switch') || raw.startsWith('nintendo')) return 'SWITCH';
  switch (parsePlatform(platformOrId || '').code) {
    case 'steam': return 'STEAM';
    case 'epic': return 'EPIC';
    case 'psn': return 'PLAYSTATION';
    case 'xbox': return 'XBOX';
    case 'bot': return 'BOT';
    default: return raw ? raw.split('|')[0].toUpperCase() : '—';
  }
}

/** rocketleague.tracker.network profile URL; Steam uses the Steam64 ID. */
export function trackerUrl(playerId: string, platform: string, name: string): string | null {
  const code = parsePlatform(platform || playerId).code;
  const lower = (platform || playerId || '').toLowerCase();
  const trackerPlatform =
    code === 'steam' ? 'steam'
    : code === 'epic' ? 'epic'
    : code === 'psn' || lower.startsWith('ps4') || lower.startsWith('ps5') ? 'psn'
    : code === 'xbox' ? 'xbl'
    : null;
  const identifier = code === 'steam' ? playerId.split('|')[1] || name : name;
  if (!trackerPlatform || !identifier) return null;
  return `https://rocketleague.tracker.network/rocket-league/profile/${trackerPlatform}/${encodeURIComponent(identifier)}/overview`;
}

/** Online ID portion of a "Platform|id|split" player ID. */
export function onlineIdOf(playerId: string | null | undefined): string {
  if (!playerId) return '';
  const parts = playerId.split('|');
  return (parts.length > 1 ? parts[1] : parts[0]).toLowerCase();
}

// ============================================================================
// Team / match helpers
// ============================================================================

export const TEAM_INK = ['text-blue-ink', 'text-orange-ink'] as const;
export const TEAM_SLAB_BORDER = ['border-blue-slab', 'border-orange-slab'] as const;

export function teamName(team: number): 'BLUE' | 'ORANGE' {
  return team === 1 ? 'ORANGE' : 'BLUE';
}

export function localTeamOf(match: SessionMatchDetail): number {
  return match.players?.find((p) => p.is_local)?.team_num ?? match.local_team ?? 0;
}

export type Outcome = 'W' | 'L' | 'D' | null;

export function outcomeOf(result: string | null | undefined): Outcome {
  if (result === 'victory') return 'W';
  if (result === 'defeat') return 'L';
  if (result === 'draw') return 'D';
  return null;
}

export const OUTCOME_SLAB: Record<'W' | 'L' | 'D', string> = {
  W: 'bg-win',
  L: 'bg-loss',
  D: 'bg-ink-3',
};

/** Same rule as today's MatchCard: top scorer (score > 0) on the winning team. */
export function findMvp<P extends { team_num: number; player_id: string; stats?: { score: number } }>(
  players: P[],
  winnerTeam: number | null | undefined,
  blueScore: number,
  orangeScore: number,
): P | null {
  const winner = winnerTeam ?? (blueScore > orangeScore ? 0 : orangeScore > blueScore ? 1 : null);
  if (winner === null) return null;
  let top: P | null = null;
  for (const p of players) {
    if (p.team_num !== winner) continue;
    if (!top || (p.stats?.score || 0) > (top.stats?.score || 0)) top = p;
  }
  return top && (top.stats?.score || 0) > 0 ? top : null;
}

/** "You + AeroPulse vs VortexRL, Zenith_99" */
export function rosterLine(match: SessionMatchDetail): string {
  const localTeam = localTeamOf(match);
  const players = match.players || [];
  const mates = players.filter((p) => p.team_num === localTeam && !p.is_local).map((p) => p.name);
  const opps = players.filter((p) => p.team_num !== localTeam && p.team_num !== 255).map((p) => p.name);
  if (mates.length === 0 && opps.length === 0) return '';
  const us = ['You', mates.join(', ')].filter(Boolean).join(' + ');
  return opps.length ? `${us} vs ${opps.join(', ')}` : us;
}

// ============================================================================
// Session KPIs (shared by the idle Live view and the Session page)
// ============================================================================

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

  // Current streak, newest backwards; a draw or unknown result ends it.
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
