// Derivations for the Players directory and Player Profile dialog.
import { PlayerMatchup, PlayerPlaylistRank, PlayerRanksSnapshot, PlayerSummary } from '../../types/api';
import { formatPct, formatRecord } from '../../utils/broadcast';

// Display order for rank cards: 2v2, 3v3, 1v1, then everything else by ID.
const RANK_ORDER = [11, 13, 10, 12];
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

/** Directory rank: 2v2 first, then 3v3, then whatever was recorded. */
export function primaryRank(ranksJson: string | null | undefined): PlayerPlaylistRank | null {
  const ranks = parseRanks(ranksJson);
  return ranks[0] || null;
}

export interface Record2 {
  wins: number;
  losses: number;
}

export const withYou = (p: PlayerSummary): Record2 => ({ wins: p.total_wins_as_teammate, losses: p.total_losses_as_teammate });
export const vsYou = (p: PlayerSummary): Record2 => ({ wins: p.total_wins_as_opponent, losses: p.total_losses_as_opponent });
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

export function spotlights(players: PlayerSummary[]): Spotlight[] {
  let mate: PlayerSummary | null = null;
  let faced: PlayerSummary | null = null;
  let rival: PlayerSummary | null = null;
  const rate = (r: Record2) => (games(r) ? r.wins / games(r) : 0);

  for (const p of players) {
    const m = withYou(p);
    const v = vsYou(p);
    if (games(m) >= 2 && (!mate || rate(m) > rate(withYou(mate)) || (rate(m) === rate(withYou(mate)) && games(m) > games(withYou(mate))))) {
      mate = p;
    }
    if (games(v) >= 2 && (!faced || games(v) > games(vsYou(faced)))) faced = p;
    if (games(v) >= 2 && (!rival || rate(v) < rate(vsYou(rival)) || (rate(v) === rate(vsYou(rival)) && games(v) > games(vsYou(rival))))) {
      rival = p;
    }
  }

  const none = 'NOT ENOUGH GAMES YET';
  const mateRec = mate ? withYou(mate) : null;
  const facedRec = faced ? vsYou(faced) : null;
  const rivalRec = rival ? vsYou(rival) : null;
  return [
    {
      key: 'mate',
      label: 'BEST TEAMMATE',
      player: mate,
      value: mateRec ? formatPct(mateRec.wins, mateRec.losses) : '—',
      sub: mateRec ? `${formatRecord(mateRec.wins, mateRec.losses)} TOGETHER` : none,
      tone: 'win',
    },
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
      sub: rivalRec ? (rivalRec.wins === 0 ? 'NEVER BEATEN' : `${formatPct(rivalRec.wins, rivalRec.losses)} WIN RATE`) : none,
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
