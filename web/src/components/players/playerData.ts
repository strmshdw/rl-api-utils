// Derivations for the Players directory and Player Profile dialog.
import { PlayerMatchup, PlayerPlaylistRank, PlayerRanksSnapshot, PlayerSummary } from '../../types/api';
import { formatPct, formatRecord } from '../../utils/broadcast';

// Display order for rank cards: 2v2, 3v3, 1v1, then everything else by ID.
const RANK_ORDER = [11, 13, 10, 12];
const EXTRA_RANKED_IDS = new Set([27, 28, 29, 30, 34]);

const rankOrder = (id: number) => {
  const i = RANK_ORDER.indexOf(id);
  return i === -1 ? RANK_ORDER.length + id : i;
};

export function parseRanks(ranksJson: string | null | undefined): PlayerPlaylistRank[] {
  if (!ranksJson || ranksJson === '{}') return [];
  try {
    const parsed: PlayerRanksSnapshot = JSON.parse(ranksJson);
    if (!parsed || typeof parsed !== 'object') return [];
    return Object.values(parsed)
      .filter((r) => r && typeof r === 'object')
      .sort((a, b) => rankOrder(a.playlist_id) - rankOrder(b.playlist_id));
  } catch {
    return [];
  }
}

/** Directory rank when viewing all/casual modes: 2v2 first, then 3v3, then whatever was recorded. */
export function primaryRank(ranksJson: string | null | undefined): PlayerPlaylistRank | null {
  const ranks = parseRanks(ranksJson);
  return ranks[0] || null;
}

/** Maps a playlist filter token ('1v1'/'10', '2v2'/'11', '3v3'/'13', 'hoops'/'27', etc.) to its ranked playlist ID. */
export function resolveRankedPlaylistId(playlistFilter?: string | number | null): number | null {
  if (playlistFilter == null) return null;
  const f = String(playlistFilter).trim().toLowerCase();
  if (
    !f ||
    f === 'all' ||
    f === '0' ||
    f === 'casual' ||
    f === 'unranked' ||
    f === 'private' ||
    f === 'custom'
  ) {
    return null;
  }
  switch (f) {
    case '1v1':
    case 'duel':
    case 'ranked_1v1':
    case '10':
      return 10;
    case '2v2':
    case 'doubles':
    case 'ranked_2v2':
    case '11':
      return 11;
    case '3v3':
    case 'standard':
    case 'ranked_3v3':
    case '13':
      return 13;
    case '12':
      return 12;
    case 'hoops':
    case '27':
      return 27;
    case 'rumble':
    case '28':
      return 28;
    case 'dropshot':
    case '29':
      return 29;
    case 'snowday':
    case 'snow_day':
    case '30':
      return 30;
    case 'tournament':
    case 'tournaments':
    case '34':
      return 34;
    default:
      return null;
  }
}

export function isSoloDuelPlaylist(playlistFilter?: string | number | null): boolean {
  if (playlistFilter == null) return false;
  const f = String(playlistFilter).trim().toLowerCase();
  return f === '10' || f === '1v1' || f === 'duel' || f === 'ranked_1v1' || f === '1';
}

/**
 * Resolves the rank for the active playlist filter without misleadingly leaking
 * a player's 2v2 rank when viewing 1v1, 3v3, or a specific extra mode (STAT-03).
 * Falls back to primaryRank only when playlistFilter is 'all'/0/empty/'casual'.
 */
export function rankForPlaylist(
  ranksJson: string | null | undefined,
  playlistFilter?: string | number | null,
): PlayerPlaylistRank | null {
  const f = playlistFilter == null ? '' : String(playlistFilter).trim().toLowerCase();
  if (!f || f === 'all' || f === '0' || f === 'casual' || f === 'unranked' || f === 'private' || f === 'custom') {
    return primaryRank(ranksJson);
  }
  const ranks = parseRanks(ranksJson);
  if (f === 'extra') {
    return ranks.find((r) => EXTRA_RANKED_IDS.has(r.playlist_id)) ?? null;
  }
  const id = resolveRankedPlaylistId(playlistFilter);
  if (id !== null) {
    const allowLegacySoloStandard = f === '3v3' || f === 'standard' || f === 'ranked_3v3';
    return (
      ranks.find(
        (r) => r.playlist_id === id || (allowLegacySoloStandard && r.playlist_id === 12),
      ) ?? null
    );
  }
  return primaryRank(ranksJson);
}

export interface Record2 {
  wins: number;
  losses: number;
}

export const withYou = (p: PlayerSummary): Record2 => ({
  wins: p.total_wins_as_teammate,
  losses: p.total_losses_as_teammate,
});
export const vsYou = (p: PlayerSummary): Record2 => ({
  wins: p.total_wins_as_opponent,
  losses: p.total_losses_as_opponent,
});
const games = (r: Record2) => r.wins + r.losses;

export type RoleFilter = 'all' | 'frequent' | 'teammate' | 'opponent' | 'synergy';

/** Same rules as the previous directory's RoleFilter. */
export function matchesRole(p: PlayerSummary, role: RoleFilter): boolean {
  const mate = withYou(p);
  switch (role) {
    case 'frequent':
      return p.total_matches >= 2;
    case 'teammate':
      return games(mate) > 0;
    case 'opponent':
      return games(vsYou(p)) > 0;
    case 'synergy':
      return games(mate) >= 1 && mate.wins / games(mate) >= 0.5;
    default:
      return true;
  }
}

export interface Spotlight {
  key: 'mate' | 'faced' | 'rival';
  label: string;
  player: PlayerSummary | null;
  value: string;
  sub: string;
  /** Tailwind colour suffix used for the top rule and value. */
  tone: 'win' | 'neutral' | 'loss';
}

export function spotlights(
  players: PlayerSummary[],
  playlistFilter?: string | number | null,
): Spotlight[] {
  const soloDuel = isSoloDuelPlaylist(playlistFilter);
  let mate: PlayerSummary | null = null;
  let faced: PlayerSummary | null = null;
  let rival: PlayerSummary | null = null;
  const rate = (r: Record2) => (games(r) ? r.wins / games(r) : 0);

  for (const p of players) {
    const m = withYou(p);
    const v = vsYou(p);
    if (
      !soloDuel &&
      games(m) >= 2 &&
      (!mate ||
        rate(m) > rate(withYou(mate)) ||
        (rate(m) === rate(withYou(mate)) && games(m) > games(withYou(mate))))
    ) {
      mate = p;
    }
    if (games(v) >= 2 && (!faced || games(v) > games(vsYou(faced)))) faced = p;
    if (
      games(v) >= 2 &&
      (!rival ||
        rate(v) < rate(vsYou(rival)) ||
        (rate(v) === rate(vsYou(rival)) && games(v) > games(vsYou(rival))))
    ) {
      rival = p;
    }
  }

  const none = 'NOT ENOUGH GAMES YET';
  const mateRec = mate ? withYou(mate) : null;
  const facedRec = faced ? vsYou(faced) : null;
  const rivalRec = rival ? vsYou(rival) : null;

  const mateSlot: Spotlight = soloDuel
    ? {
        key: 'mate',
        label: 'SOLO DUEL MODE',
        player: null,
        value: '1V1',
        sub: 'NO TEAMMATES IN 1V1',
        tone: 'neutral',
      }
    : {
        key: 'mate',
        label: 'BEST TEAMMATE',
        player: mate,
        value: mateRec ? formatPct(mateRec.wins, mateRec.losses) : '—',
        sub: mateRec ? `${formatRecord(mateRec.wins, mateRec.losses)} TOGETHER` : none,
        tone: 'win',
      };

  return [
    mateSlot,
    {
      key: 'faced',
      label: 'MOST FACED',
      player: faced,
      value: facedRec ? formatRecord(facedRec.wins, facedRec.losses) : '—',
      sub: facedRec ? `${games(facedRec)} GAMES AS OPPONENT` : none,
      tone: 'neutral',
    },
    {
      key: 'rival',
      label: 'TOUGHEST RIVAL',
      player: rival,
      value: rivalRec ? formatRecord(rivalRec.wins, rivalRec.losses) : '—',
      sub: rivalRec
        ? rivalRec.wins === 0
          ? 'NEVER BEATEN'
          : `${formatPct(rivalRec.wins, rivalRec.losses)} WIN RATE`
        : none,
      tone: 'loss',
    },
  ];
}

/** H2H tag; shown only with at least 2 games in that role. */
export function h2hTag(wins: number, losses: number, teammate: boolean): string | null {
  const total = wins + losses;
  if (total < 2) return null;
  const r = wins / total;
  if (teammate) return r >= 0.6 ? 'STRONG SYNERGY' : r >= 0.4 ? 'BALANCED' : 'STRUGGLED';
  return r >= 0.6 ? 'DOMINANT' : r >= 0.4 ? 'EVEN RIVALRY' : 'TOUGH OPPONENT';
}

export function sumMatchups(matchups: PlayerMatchup[] | null | undefined) {
  const totals = { mate: { wins: 0, losses: 0 }, vs: { wins: 0, losses: 0 } };
  for (const m of matchups ?? []) {
    totals.mate.wins += m.wins_as_teammate;
    totals.mate.losses += m.losses_as_teammate;
    totals.vs.wins += m.wins_as_opponent;
    totals.vs.losses += m.losses_as_opponent;
  }
  return totals;
}
