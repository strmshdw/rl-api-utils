// Derivations for stored historical matches: local-relative outcome, replay
// status label and box score rows.
import { HistoricalMatchRecord, ReplayPlayerStats } from '../../types/api';
import { Outcome, outcomeOf } from '../../utils/broadcast';
import { BoxScoreRow } from '../common/BoxScore';

export const isLocal = (p: ReplayPlayerStats, ids: Set<string>) => !!p.online_id && ids.has(p.online_id.toLowerCase());

/** W/L from the local player's side when we can find them in the replay, else the stored result. */
export function historyOutcome(m: HistoricalMatchRecord, localIds: Set<string>): Outcome {
  const local = m.stats?.players?.find((p) => isLocal(p, localIds));
  if (local && (local.team === 0 || local.team === 1)) {
    const us = local.team === 0 ? m.blue_score : m.orange_score;
    const them = local.team === 0 ? m.orange_score : m.blue_score;
    if (us > them) return 'W';
    if (us < them) return 'L';
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
  const url = m.ballchasing_url || m.stats?.ballchasing_url;
  const upload = (m.upload_status || '').toUpperCase();
  const download = (m.download_status || '').toUpperCase();
  if (url || upload === 'UPLOADED' || upload === 'DUPLICATE') {
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

export function teamRows(players: ReplayPlayerStats[], team: number, localIds: Set<string>): BoxScoreRow[] {
  return players
    .filter((p) => p.team === team)
    .sort((a, b) => b.score - a.score)
    .map((p, i) => {
      const you = isLocal(p, localIds);
      return {
        key: `${p.online_id || p.name}-${i}`,
        name: you ? 'You' : p.name,
        you,
        score: p.score,
        goals: p.goals,
        assists: p.assists,
        saves: p.saves,
        shots: p.shots,
        demos: p.demos,
      };
    });
}

export function playerNames(players: ReplayPlayerStats[], localIds: Set<string>): string {
  const side = (team: number) =>
    players
      .filter((p) => p.team === team)
      .map((p) => (isLocal(p, localIds) ? 'You' : p.name))
      .join(', ');
  return [side(0), side(1)].filter(Boolean).join(' vs ');
}
