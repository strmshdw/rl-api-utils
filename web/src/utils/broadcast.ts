// Display helpers for the "Broadcast" dashboard: record colours, H2H cells,
// uppercase relative times, playlist labels and session KPI derivation.
import {
  LobbyPlayer,
  PlayerRoleStatsSummary,
  SessionMatchDetail,
  SessionMatchPlayer,
  SessionResponse,
} from '../types/api';
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

/**
 * Formats match clock: countdown MM:SS during regulation, +MM:SS during overtime.
 */
export function formatMatchClock(timeRemaining: number | undefined | null, isOvertime?: boolean): string {
  const seconds = Math.max(0, Math.floor(timeRemaining ?? 0));
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  const padded = `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  return isOvertime ? `+${padded}` : padded;
}

const ROMAN_DIV = ['', 'I', 'II', 'III', 'IV'];

/**
 * Formats a rank name + division into the compact broadcast format (e.g. "GC I · DIV II" or "GC I · D2").
 */
export function shortRank(rankName?: string | null, division?: number | null): string {
  if (!rankName) return 'UNRANKED';
  const clean = rankName.trim();
  if (!clean || clean.toUpperCase() === 'UNRANKED') return 'UNRANKED';
  // If already shortened with · or D1..4
  if (clean.includes('·')) return clean.toUpperCase();

  let r = clean
    .replace(/Supersonic Legend/i, 'SSL')
    .replace(/Grand Champion/i, 'GC')
    .replace(/Champion/i, 'CHAMP')
    .replace(/Diamond/i, 'DIA')
    .replace(/Platinum/i, 'PLAT')
    .replace(/Gold/i, 'GOLD')
    .replace(/Silver/i, 'SILV')
    .replace(/Bronze/i, 'BRNZ')
    .toUpperCase();

  if (r.startsWith('SSL')) return 'SSL';
  const d = division && division >= 1 && division <= 4 ? ROMAN_DIV[division] : '';
  return d ? `${r} · DIV ${d}` : r;
}

export interface RelationshipTagInfo {
  label: string;
  colorClass: string;
  tag: string;
  color: string;
  border: string;
}

export function computeDynamicRelationshipTag(
  tw: number,
  tl: number,
  ow: number,
  ol: number,
  overrideLabel?: string | boolean
): RelationshipTagInfo {
  const make = (label: string, colorClass: string, border: string): RelationshipTagInfo => ({
    label,
    colorClass,
    tag: label,
    color: colorClass,
    border,
  });

  if (typeof overrideLabel === 'string' && overrideLabel) {
    const up = overrideLabel.toUpperCase();
    if (up.includes('PRIME') || up.includes('SYNERGY')) {
      return make(up, 't-text-win text-win t-border-win', 'border-win');
    }
    if (up.includes('FAVORABLE')) {
      return make(up, 't-text-blue text-blue-ink t-border-blue', 'border-blue-ink');
    }
    if (up.includes('NEMESIS') || up.includes('COLD')) {
      return make(up, 't-text-loss text-loss t-border-loss', 'border-loss');
    }
    if (up.includes('RIVAL')) {
      return make(up, 't-text-orange text-orange-ink t-border-orange', 'border-orange-ink');
    }
  }
  const tG = (tw || 0) + (tl || 0);
  const oG = (ow || 0) + (ol || 0);
  if (tG + oG === 0) return make('NO GAMES IN RANGE', 't-text-muted text-muted t-border-2', 'border-line');
  const tWR = tG ? Math.round((tw / tG) * 100) : 0;
  const oWR = oG ? Math.round((ow / oG) * 100) : 0;

  if (tG >= oG) {
    if (tWR >= 65 && tG >= 2) return make('PRIME DUO', 't-text-win text-win t-border-win', 'border-win');
    if (tWR >= 50) return make('SYNERGY ALLY', 't-text-win text-win t-border-win', 'border-win');
    return make('COLD DUO', 't-text-loss text-loss t-border-loss', 'border-loss');
  } else {
    if (oWR <= 40 && oG >= 2) return make('NEMESIS', 't-text-loss text-loss t-border-loss', 'border-loss');
    if (oWR >= 60) return make('FAVORABLE FOE', 't-text-blue text-blue-ink t-border-blue', 'border-blue-ink');
    return make('ARCH-RIVAL', 't-text-orange text-orange-ink t-border-orange', 'border-orange-ink');
  }
}

export interface LiveThreatBadge {
  type: 'higher' | 'carry' | 'smurf';
  label: string;
  title: string;
  classes: string;
}

const MODE_SHORT_LABEL: Record<string, string> = {
  '10': '1V1',
  '11': '2V2',
  '13': '3V3',
  '27': 'HOOPS',
  '28': 'RUMBLE',
  '29': 'DROPSHOT',
  '30': 'SNOW DAY',
};

/**
 * Computes Live Match Threat, Carry & Smurf Detection Badges for a player.
 * 1. ⚠ HIGHER MMR (+X VS YOU): opponent active-playlist MMR > local player active-playlist MMR.
 * 2. ⚡ CARRYING (+X MMR VS MATE): opponent active-playlist MMR >= +80 MMR above their own teammate(s).
 * 3. 🚨 SMURF · <MODE> <SHORT_RANK> (<MMR> · +<DIFF> VS YOU): player has >= +120 MMR vs local player in another playlist AND >= +80 MMR above their own active-playlist MMR.
 */
export function computeLiveThreatBadges(
  player: LobbyPlayer,
  localPlayer: LobbyPlayer | undefined | null,
  opposingTeamPlayers: LobbyPlayer[],
  activePlaylistId = 11
): LiveThreatBadge[] {
  if (!player || player.is_local || player.is_bot) return [];
  const badges: LiveThreatBadge[] = [];

  const activeKey = String(activePlaylistId || 11);
  const playerActiveMmr =
    player.current_rank?.mmr || player.ranks?.[activeKey]?.mmr || 0;
  const localActiveMmr =
    localPlayer?.current_rank?.mmr || localPlayer?.ranks?.[activeKey]?.mmr || 1515;

  const isOpponent =
    localPlayer != null ? player.team_num !== localPlayer.team_num : player.team_num === 1;

  // 1. HIGHER MMR (+X VS YOU) on opponents
  if (isOpponent && playerActiveMmr > 0 && localActiveMmr > 0 && playerActiveMmr > localActiveMmr) {
    const diff = Math.round(playerActiveMmr - localActiveMmr);
    if (diff > 0) {
      badges.push({
        type: 'higher',
        label: `戒 · HIGHER MMR (+${diff} VS YOU)`,
        title: `Active playlist MMR is +${diff} higher than yours (${Math.round(playerActiveMmr)} vs ${Math.round(localActiveMmr)})`,
        classes: 'cartouche-gold',
      });
    }
  }

  // 2. CARRYING (+X MMR VS MATE) on opponents when >= +80 MMR above their own teammate(s)
  if (isOpponent && playerActiveMmr > 0 && opposingTeamPlayers.length > 1) {
    const mates = opposingTeamPlayers.filter(
      (m) => m.player_id !== player.player_id && !m.is_bot
    );
    let maxMateDiff = 0;
    for (const mate of mates) {
      const mateMmr = mate.current_rank?.mmr || mate.ranks?.[activeKey]?.mmr || 0;
      if (mateMmr > 0) {
        maxMateDiff = Math.max(maxMateDiff, Math.round(playerActiveMmr - mateMmr));
      }
    }
    if (maxMateDiff >= 80) {
      badges.push({
        type: 'carry',
        label: `双 · CARRYING (+${maxMateDiff} MMR VS MATE)`,
        title: `Significantly higher MMR (+${maxMateDiff}) than their teammate on the opposing team`,
        classes: 'cartouche-carry',
      });
    }
  }

  // 3. SMURF / OFF-MODE PEAK: >= +120 MMR vs local player in another playlist AND >= +80 MMR above their own active-playlist MMR
  if (player.ranks) {
    let bestSmurf: {
      modeLabel: string;
      rankShort: string;
      mmr: number;
      diffVsYou: number;
    } | null = null;

    for (const [pid, r] of Object.entries(player.ranks)) {
      if (pid === activeKey || !r || !r.mmr) continue;
      const localModeMmr =
        localPlayer?.ranks?.[pid]?.mmr || localActiveMmr || 1450;
      const diffVsYou = Math.round(r.mmr - localModeMmr);
      const diffVsOwnActive = playerActiveMmr > 0 ? Math.round(r.mmr - playerActiveMmr) : 999;

      if (diffVsYou >= 120 && diffVsOwnActive >= 80) {
        if (!bestSmurf || diffVsYou > bestSmurf.diffVsYou) {
          const modeLabel = MODE_SHORT_LABEL[pid] || r.playlist_name?.toUpperCase() || `MODE ${pid}`;
          const rankShort = shortRank(r.rank_name, r.division).split('·')[0].trim();
          bestSmurf = {
            modeLabel,
            rankShort,
            mmr: Math.round(r.mmr),
            diffVsYou,
          };
        }
      }
    }

    if (bestSmurf) {
      badges.push({
        type: 'smurf',
        label: `印 · SMURF · ${bestSmurf.modeLabel} ${bestSmurf.rankShort} (${bestSmurf.mmr} · +${bestSmurf.diffVsYou} VS YOU)`,
        title: `Off-playlist peak: ${bestSmurf.modeLabel} ${bestSmurf.rankShort} (${bestSmurf.mmr} MMR, +${bestSmurf.diffVsYou} above you in that playlist)`,
        classes: 'hanko-seal-vermilion',
      });
    }
  }

  return badges;
}

/**
 * Builds a complete PlayerRoleStatsSummary from games/wins/losses and optional partial stats.
 * Supports both synthesizeRoleStats(games, wins, losses, partial, seedPts)
 * and synthesizeRoleStats(wins, losses, seedString).
 */
export function synthesizeRoleStats(
  arg1: number,
  arg2: number,
  arg3?: number | string,
  partial?: Partial<PlayerRoleStatsSummary> | null,
  seedPts = 420
): PlayerRoleStatsSummary & {
  score: number;
  goals: number;
  assists: number;
  saves: number;
  demos: number;
  shot_pct: number;
  avg_demos: number;
} {
  let g: number;
  let w: number;
  let l: number;
  if (typeof arg3 === 'string' || arg3 === undefined) {
    w = Math.max(0, Math.round(arg1 || 0));
    l = Math.max(0, Math.round(arg2 || 0));
    g = w + l;
  } else {
    g = Math.max(0, Math.round(arg1 || 0));
    w = Math.max(0, Math.round(arg2 || 0));
    l = Math.max(0, Math.round(arg3 || 0));
  }

  if (g === 0) {
    return {
      games: 0,
      wins: 0,
      losses: 0,
      win_rate: 0,
      total_score: 0,
      total_goals: 0,
      total_assists: 0,
      total_saves: 0,
      total_shots: 0,
      total_demos_inflicted: 0,
      total_demos_taken: 0,
      avg_score: 0,
      avg_goals: 0,
      avg_assists: 0,
      avg_saves: 0,
      avg_shots: 0,
      shooting_pct: 0,
      avg_demos_inflicted: 0,
      avg_demos_taken: 0,
      avg_bpm: 0,
      avg_bcpm: 0,
      avg_boost: 0,
      total_boost_stolen: 0,
      total_big_pads: 0,
      total_small_pads: 0,
      avg_supersonic_pct: 0,
      avg_boost_speed_pct: 0,
      avg_slow_speed_pct: 0,
      avg_ground_pct: 0,
      avg_low_air_pct: 0,
      avg_high_air_pct: 0,
      avg_powerslide_count: 0,
      avg_powerslide_duration: 0,
      avg_def_third_pct: 0,
      avg_neutral_third_pct: 0,
      avg_off_third_pct: 0,
      avg_behind_ball_pct: 0,
      avg_first_man_pct: 0,
      avg_third_man_pct: 0,
      avg_dist_to_ball: 0,
      score: 0,
      goals: 0,
      assists: 0,
      saves: 0,
      demos: 0,
      shot_pct: 0,
      avg_demos: 0,
    };
  }

  const avgScore = partial?.avg_score ?? seedPts;
  const avgGoals = partial?.avg_goals ?? 1.4;
  const avgAssists = partial?.avg_assists ?? 0.9;
  const avgSaves = partial?.avg_saves ?? 1.6;
  const avgShots = partial?.avg_shots ?? 3.8;
  const shPct = partial?.shooting_pct ?? (avgShots > 0 ? Math.round((avgGoals / avgShots) * 100) : 37);
  const avgDemos = partial?.avg_demos_inflicted ?? 1.2;
  const avgDemosTaken = partial?.avg_demos_taken ?? 1.0;
  const avgBpm = partial?.avg_bpm ?? 415;
  const avgBcpm = partial?.avg_bcpm ?? 430;
  const avgBoost = partial?.avg_boost ?? 46;
  const avgSup = partial?.avg_supersonic_pct ?? 18.2;

  const totalScore = partial?.total_score ?? Math.round(avgScore * g);
  const totalGoals = partial?.total_goals ?? Math.round(avgGoals * g);
  const totalAssists = partial?.total_assists ?? Math.round(avgAssists * g);
  const totalSaves = partial?.total_saves ?? Math.round(avgSaves * g);
  const totalShots = partial?.total_shots ?? Math.round(avgShots * g);
  const totalDemos = partial?.total_demos_inflicted ?? Math.round(avgDemos * g);

  return {
    games: g,
    wins: w,
    losses: l,
    win_rate: g > 0 ? (w / g) * 100 : 0,
    total_score: totalScore,
    total_goals: totalGoals,
    total_assists: totalAssists,
    total_saves: totalSaves,
    total_shots: totalShots,
    total_demos_inflicted: totalDemos,
    total_demos_taken: partial?.total_demos_taken ?? Math.round(avgDemosTaken * g),
    avg_score: avgScore,
    avg_goals: avgGoals,
    avg_assists: avgAssists,
    avg_saves: avgSaves,
    avg_shots: avgShots,
    shooting_pct: shPct,
    avg_demos_inflicted: avgDemos,
    avg_demos_taken: avgDemosTaken,
    avg_bpm: avgBpm,
    avg_bcpm: avgBcpm,
    avg_boost: avgBoost,
    total_boost_stolen: partial?.total_boost_stolen ?? Math.round(520 * g),
    total_big_pads: partial?.total_big_pads ?? Math.round(21 * g),
    total_small_pads: partial?.total_small_pads ?? Math.round(68 * g),
    avg_supersonic_pct: avgSup,
    avg_boost_speed_pct: partial?.avg_boost_speed_pct ?? 39.0,
    avg_slow_speed_pct: partial?.avg_slow_speed_pct ?? 42.8,
    avg_ground_pct: partial?.avg_ground_pct ?? 56.0,
    avg_low_air_pct: partial?.avg_low_air_pct ?? 34.0,
    avg_high_air_pct: partial?.avg_high_air_pct ?? 10.0,
    avg_powerslide_count: partial?.avg_powerslide_count ?? 48,
    avg_powerslide_duration: partial?.avg_powerslide_duration ?? 7.2,
    avg_def_third_pct: partial?.avg_def_third_pct ?? 42,
    avg_neutral_third_pct: partial?.avg_neutral_third_pct ?? 34,
    avg_off_third_pct: partial?.avg_off_third_pct ?? 24,
    avg_behind_ball_pct: partial?.avg_behind_ball_pct ?? 68,
    avg_first_man_pct: partial?.avg_first_man_pct ?? 36,
    avg_third_man_pct: partial?.avg_third_man_pct ?? 32,
    avg_dist_to_ball: partial?.avg_dist_to_ball ?? 2480,
    score: totalScore,
    goals: totalGoals,
    assists: totalAssists,
    saves: totalSaves,
    demos: totalDemos,
    shot_pct: shPct,
    avg_demos: avgDemos,
  };
}



