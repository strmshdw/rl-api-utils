import type { SessionMatchDetail } from '../types/api';
import { parsePlatform } from './platforms';

export {
  formatArenaName,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  playlistFilterToParam,
  positioningRoleLabels,
  type PositioningRoleLabels,
} from './arenaLabels';

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
  6: { name: 'PRIVATE MATCH', short: 'PRIVATE', mode: 'PRIVATE MATCH' },
  7: { name: 'SEASON', short: 'SEASON', mode: 'SEASON' },
  8: { name: 'EXHIBITION', short: 'EXHIBITION', mode: 'EXHIBITION' },
  9: { name: 'FREE PLAY', short: 'FREE PLAY', mode: 'FREE PLAY' },
  10: { name: 'RANKED DUEL', short: 'DUEL', mode: 'RANKED 1V1' },
  11: { name: 'RANKED DOUBLES', short: 'DOUBLES', mode: 'RANKED 2V2' },
  12: { name: 'RANKED SOLO STANDARD', short: 'SOLO STANDARD', mode: 'RANKED SOLO 3V3' },
  13: { name: 'RANKED STANDARD', short: 'STANDARD', mode: 'RANKED 3V3' },
  15: { name: 'CASUAL HOOPS', short: 'HOOPS', mode: 'CASUAL HOOPS' },
  16: { name: 'CASUAL ROCKET LABS', short: 'ROCKET LABS', mode: 'ROCKET LABS' },
  17: { name: 'CASUAL SNOW DAY', short: 'SNOW DAY', mode: 'CASUAL SNOW DAY' },
  18: { name: 'CASUAL RUMBLE', short: 'RUMBLE', mode: 'CASUAL RUMBLE' },
  19: { name: 'CUSTOM TOURNAMENT', short: 'TOURNAMENT', mode: 'CUSTOM TOURNAMENT' },
  22: { name: 'CUSTOM MATCH', short: 'CUSTOM', mode: 'CUSTOM MATCH' },
  23: { name: 'CASUAL', short: 'CASUAL', mode: 'CASUAL' },
  24: { name: 'LAN MATCH', short: 'LAN', mode: 'LAN MATCH' },
  27: { name: 'RANKED HOOPS', short: 'HOOPS', mode: 'RANKED HOOPS' },
  28: { name: 'RANKED RUMBLE', short: 'RUMBLE', mode: 'RANKED RUMBLE' },
  29: { name: 'RANKED DROPSHOT', short: 'DROPSHOT', mode: 'RANKED DROPSHOT' },
  30: { name: 'RANKED SNOW DAY', short: 'SNOW DAY', mode: 'RANKED SNOW DAY' },
  31: { name: 'GHOST HUNT', short: 'GHOST HUNT', mode: 'GHOST HUNT' },
  32: { name: 'BEACH BALL', short: 'BEACH BALL', mode: 'BEACH BALL' },
  33: { name: 'SPIKE RUSH', short: 'SPIKE RUSH', mode: 'SPIKE RUSH' },
  34: { name: 'TOURNAMENT', short: 'TOURNAMENT', mode: 'TOURNAMENT' },
  35: { name: 'ROCKET LABS', short: 'ROCKET LABS', mode: 'ROCKET LABS' },
  37: { name: 'DROPSHOT RUMBLE', short: 'DROPSHOT RUMBLE', mode: 'DROPSHOT RUMBLE' },
  38: { name: 'HEATSEEKER', short: 'HEATSEEKER', mode: 'HEATSEEKER' },
  41: { name: 'BOOMER BALL', short: 'BOOMER BALL', mode: 'BOOMER BALL' },
  43: { name: 'HEATSEEKER DOUBLES', short: 'HEATSEEKER 2V2', mode: 'HEATSEEKER 2V2' },
  44: { name: 'WINTER BREAKAWAY', short: 'WINTER BREAKAWAY', mode: 'WINTER BREAKAWAY' },
  46: { name: 'GRIDIRON', short: 'GRIDIRON', mode: 'GRIDIRON' },
  47: { name: 'SUPER CUBE', short: 'SUPER CUBE', mode: 'SUPER CUBE' },
  48: { name: 'TACTICAL RUMBLE', short: 'TACTICAL RUMBLE', mode: 'TACTICAL RUMBLE' },
  49: { name: 'SPRING LOADED', short: 'SPRING LOADED', mode: 'SPRING LOADED' },
  50: { name: 'SPEED DEMON', short: 'SPEED DEMON', mode: 'SPEED DEMON' },
  52: { name: 'GOTHAM CITY RUMBLE', short: 'GOTHAM RUMBLE', mode: 'GOTHAM RUMBLE' },
  53: { name: 'KNOCKOUT', short: 'KNOCKOUT', mode: 'KNOCKOUT' },
  54: { name: 'CONFIDENTIAL THIRDWHEEL', short: 'THIRDWHEEL', mode: 'THIRDWHEEL' },
  55: { name: 'NIKE FC SHOWDOWN', short: 'NIKE FC', mode: 'NIKE FC SHOWDOWN' },
  61: { name: 'NIKE FC SHOWDOWN', short: 'NIKE FC', mode: 'NIKE FC SHOWDOWN' },
  62: { name: 'ADIDAS SOCCAR STRIKE', short: 'SOCCAR STRIKE', mode: 'SOCCAR STRIKE' },
  64: { name: 'SPLIT SHOT', short: 'SPLIT SHOT', mode: 'SPLIT SHOT' },
  65: { name: 'SPLIT SHOT DOUBLES', short: 'SPLIT SHOT 2V2', mode: 'SPLIT SHOT 2V2' },
  66: { name: 'SPLIT SHOT HEATSEEKER', short: 'SPLIT HEATSEEKER', mode: 'SPLIT HEATSEEKER' },
  67: { name: 'SPLIT SHOT HEATSEEKER DOUBLES', short: 'SPLIT HEATSEEKER 2V2', mode: 'SPLIT HEATSEEKER 2V2' },
  68: { name: 'G-FORCE FRENZY', short: 'G-FORCE FRENZY', mode: 'G-FORCE FRENZY' },
  70: { name: 'CAR WARS', short: 'CAR WARS', mode: 'CAR WARS' },
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
// Team / match helpers & ranks
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

/** Top scorer (score > 0) on the winning team. */
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

/** Formats match clock: countdown MM:SS during regulation, +MM:SS during overtime. */
export function formatMatchClock(timeRemaining: number | undefined | null, isOvertime?: boolean): string {
  const seconds = Math.max(0, Math.floor(timeRemaining ?? 0));
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  const padded = `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;
  return isOvertime ? `+${padded}` : padded;
}

const ROMAN_DIV = ['', 'I', 'II', 'III', 'IV'];
const EMBEDDED_DIV_RE = /\s+(?:DIVISION|DIV\.?)\s*(IV|III|II|I|[1-4])\s*$/i;
const HINT_DIV_RE = /(?:·\s*)?(?:DIVISION|DIV\.?|D)\s*(IV|III|II|I|[1-4])\s*$/i;

function normalizeDivisionToken(token: string): string {
  const upper = token.toUpperCase();
  const numeric = Number(upper);
  return numeric >= 1 && numeric <= 4 ? ROMAN_DIV[numeric] : upper;
}

/** Formats a rank name + division into compact broadcast format (e.g. "GC I · DIV II"). */
export function shortRank(
  rankName?: string | null,
  division?: number | null,
  shortRankHint?: string | null,
  tier?: number | null,
): string {
  const clean = (rankName || shortRankHint || '').trim();
  if (!clean || clean.toUpperCase() === 'UNRANKED') return 'UNRANKED';
  if (clean.includes('·')) return clean.toUpperCase();

  let embeddedDiv = '';
  let baseRank = clean;
  const divMatch = clean.match(EMBEDDED_DIV_RE);
  if (divMatch && divMatch.index !== undefined) {
    embeddedDiv = normalizeDivisionToken(divMatch[1]);
    baseRank = clean.slice(0, divMatch.index).trim();
  } else if (shortRankHint) {
    const hintMatch = shortRankHint.trim().match(HINT_DIV_RE);
    if (hintMatch) {
      embeddedDiv = normalizeDivisionToken(hintMatch[1]);
    }
  }

  const r = baseRank
    .replace(/Supersonic Legend/i, 'SSL')
    .replace(/Grand Champion/i, 'GC')
    .replace(/Champion/i, 'CHAMP')
    .replace(/Diamond/i, 'DIA')
    .replace(/Platinum/i, 'PLAT')
    .replace(/Gold/i, 'GOLD')
    .replace(/Silver/i, 'SILV')
    .replace(/Bronze/i, 'BRNZ')
    .replace(/\s+/g, ' ')
    .trim()
    .toUpperCase();

  if (!r || r === 'UNRANKED') return 'UNRANKED';
  if (r.startsWith('SSL')) return 'SSL';

  const resolvedNumericDiv =
    division != null && division >= 1 && division <= 4
      ? ROMAN_DIV[division]
      : division === 0 && tier != null && tier > 0 && tier < 22
      ? 'I'
      : '';
  const d = embeddedDiv || resolvedNumericDiv;
  return d ? `${r} · DIV ${d}` : r;
}

/** Ultra-concise rank badge for tight grid cells (e.g. "GC I · D2", "PLAT II · D2", "SSL"). */
export function compactRank(
  rankName?: string | null,
  division?: number | null,
  shortRankHint?: string | null,
  tier?: number | null,
): string {
  const full = shortRank(rankName, division, shortRankHint, tier);
  if (full === 'UNRANKED' || full === 'SSL') return full;
  return full
    .replace(/\s*·\s*DIV\s+IV$/, ' · D4')
    .replace(/\s*·\s*DIV\s+III$/, ' · D3')
    .replace(/\s*·\s*DIV\s+II$/, ' · D2')
    .replace(/\s*·\s*DIV\s+I$/, ' · D1');
}

export interface RelationshipTagInfo {
  label: string;
  colorClass: string;
  tag: string;
  color: string;
  border: string;
}

export interface RelationshipTagOptions {
  minGames?: number;
  activeRole?: 'teammate' | 'opponent';
}

export function computeDynamicRelationshipTag(
  tw: number,
  tl: number,
  ow: number,
  ol: number,
  overrideLabel?: string | boolean,
  options?: RelationshipTagOptions,
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
    if (up === 'YOU') return make('YOU', 't-text-blue text-blue-ink t-border-blue', 'border-blue-ink');
    if (up === 'RECENT ALLY' || up === 'RECENT FOE') {
      return make(up, 't-text-muted text-muted t-border-2', 'border-line');
    }
    if (up.includes('PRIME') || up.includes('SYNERGY')) return make(up, 't-text-win text-win t-border-win', 'border-win');
    if (up.includes('FAVORABLE')) return make(up, 't-text-blue text-blue-ink t-border-blue', 'border-blue-ink');
    if (up.includes('NEMESIS') || up.includes('COLD')) return make(up, 't-text-loss text-loss t-border-loss', 'border-loss');
    if (up.includes('RIVAL')) return make(up, 't-text-orange text-orange-ink t-border-orange', 'border-orange-ink');
  }

  const tG = (tw || 0) + (tl || 0);
  const oG = (ow || 0) + (ol || 0);
  if (tG + oG === 0) return make('NO GAMES IN RANGE', 't-text-muted text-muted t-border-2', 'border-line');

  const minGames = Math.max(1, options?.minGames ?? 3);
  const tWR = tG ? (tw || 0) / tG : 0;
  const oWR = oG ? (ow || 0) / oG : 0;

  const evalTeammate = (): RelationshipTagInfo => {
    if (tG < minGames) return make('RECENT ALLY', 't-text-muted text-muted t-border-2', 'border-line');
    if (tWR >= 0.65) return make('PRIME DUO', 't-text-win text-win t-border-win', 'border-win');
    if (tWR >= 0.5) return make('SYNERGY ALLY', 't-text-win text-win t-border-win', 'border-win');
    return make('COLD DUO', 't-text-loss text-loss t-border-loss', 'border-loss');
  };

  const evalOpponent = (): RelationshipTagInfo => {
    if (oG < minGames) return make('RECENT FOE', 't-text-muted text-muted t-border-2', 'border-line');
    if (oWR <= 0.4) return make('NEMESIS', 't-text-loss text-loss t-border-loss', 'border-loss');
    if (oWR >= 0.6) return make('FAVORABLE FOE', 't-text-blue text-blue-ink t-border-blue', 'border-blue-ink');
    return make('ARCH-RIVAL', 't-text-orange text-orange-ink t-border-orange', 'border-orange-ink');
  };

  if (options?.activeRole === 'opponent' && oG > 0) {
    return evalOpponent();
  }
  if (options?.activeRole === 'teammate' && tG > 0) {
    return evalTeammate();
  }

  return tG >= oG ? evalTeammate() : evalOpponent();
}
