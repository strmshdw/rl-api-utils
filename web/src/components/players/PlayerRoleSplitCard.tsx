import React from 'react';
import { PlayerMatchDetail, PlayerRoleStatsSummary } from '../../types/api';
import { RecordCells, SectionLabel } from '../common/ui';
import {
  formatPct,
  formatRecord,
  hasSyncedRoleTelemetry,
  recordFill,
  recordInk,
} from '../../utils/broadcast';
import { h2hTag } from './playerData';
import { STAT_LABEL_CLASS } from '../../utils/statColors';

export type ProfilePlaylistScope =
  | '2v2'
  | '3v3'
  | '1v1'
  | '27'
  | '28'
  | '29'
  | '30'
  | '34'
  | 'casual';
export type ProfileRoleFilter = 'all' | 'teammate' | 'opponent';
export type ProfileResultFilter = 'all' | 'W' | 'L';
export type ProfileSortOption =
  | 'date-desc'
  | 'date-asc'
  | 'pts-desc'
  | 'goals-desc'
  | 'saves-desc'
  | 'demos-desc';

export const PLAYLIST_SCOPE_IDS: Record<ProfilePlaylistScope, number> = {
  '2v2': 11,
  '3v3': 13,
  '1v1': 10,
  '27': 27,
  '28': 28,
  '29': 29,
  '30': 30,
  '34': 34,
  casual: 0,
};

export const CORE_PLAYLIST_SCOPE_BUTTONS: ReadonlyArray<{
  scope: ProfilePlaylistScope;
  label: string;
}> = [
  { scope: '1v1', label: '1V1' },
  { scope: '2v2', label: '2V2' },
  { scope: '3v3', label: '3V3' },
];

export const EXTRA_PLAYLIST_SCOPE_OPTIONS: ReadonlyArray<{
  scope: ProfilePlaylistScope;
  label: string;
}> = [
  { scope: '27', label: 'HOOPS' },
  { scope: '28', label: 'RUMBLE' },
  { scope: '29', label: 'DROPSHOT' },
  { scope: '30', label: 'SNOW DAY' },
  { scope: '34', label: 'TOURNAMENT' },
];

export const PLAYLIST_SCOPE_BUTTONS: ReadonlyArray<{
  scope: ProfilePlaylistScope;
  label: string;
}> = [
  ...CORE_PLAYLIST_SCOPE_BUTTONS,
  { scope: 'casual', label: 'CASUAL' },
];

export function resolveLocalOutcome(
  m: PlayerMatchDetail,
): 'victory' | 'defeat' | 'draw' | '' {
  if (
    m.local_result === 'victory' ||
    m.local_result === 'defeat' ||
    m.local_result === 'draw'
  ) {
    return m.local_result;
  }
  if (m.result === 'victory') return m.role === 'opponent' ? 'defeat' : 'victory';
  if (m.result === 'defeat') return m.role === 'opponent' ? 'victory' : 'defeat';
  if (m.result === 'draw') return 'draw';
  if (m.player_stats?.won !== undefined && m.player_stats?.won !== null) {
    if (m.role === 'opponent') {
      return m.player_stats.won ? 'defeat' : 'victory';
    }
    return m.player_stats.won ? 'victory' : 'defeat';
  }
  return '';
}

export const RecordWithPct: React.FC<{ wins: number; losses: number }> = ({
  wins,
  losses,
}) => (
  <span className={`font-bold text-lg t-font-mono ${recordInk(wins, losses)}`}>
    {formatRecord(wins, losses)}{' '}
    <span className="text-sm text-muted font-semibold">{formatPct(wins, losses)}</span>
  </span>
);

interface CompactRoleSplitCardProps {
  label: string;
  roleTitle: string;
  wins: number;
  losses: number;
  teammate: boolean;
  stats: PlayerRoleStatsSummary;
  isAvg: boolean;
  emptyMessage: string;
  selected?: boolean;
  onClick?: () => void;
}

export const CompactRoleSplitCard: React.FC<CompactRoleSplitCardProps> = ({
  label,
  roleTitle,
  wins,
  losses,
  teammate,
  stats,
  isAvg,
  emptyMessage,
  selected,
  onClick,
}) => {
  const games = wins + losses;
  const tag = h2hTag(wins, losses, teammate);
  const gameWord = games === 1 ? 'GAME' : 'GAMES';
  const wr = games > 0 ? Math.round((wins / games) * 100) : 0;
  const synced = games > 0 && hasSyncedRoleTelemetry(stats);

  const pts = isAvg ? Math.round(stats.avg_score) : (stats.score || stats.total_score || 0);
  const g = isAvg ? Number(stats.avg_goals.toFixed(1)) : (stats.goals || stats.total_goals || 0);
  const a = isAvg ? Number(stats.avg_assists.toFixed(1)) : (stats.assists || stats.total_assists || 0);
  const sv = isAvg ? Number(stats.avg_saves.toFixed(1)) : (stats.saves || stats.total_saves || 0);
  const shPct = Math.round(stats.shot_pct || stats.shooting_pct || 0);
  const dem = isAvg
    ? Number((stats.avg_demos || stats.avg_demos_inflicted || 0).toFixed(1))
    : (stats.demos || stats.total_demos_inflicted || 0);
  const bpm = Math.round(stats.avg_bpm || 0);
  const bst = Math.round(stats.avg_boost || 0);
  const sup = Math.round(stats.avg_supersonic_pct || 0);

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if ((e.key === 'Enter' || e.key === ' ') && onClick) {
          e.preventDefault();
          onClick();
        }
      }}
      data-testid={teammate ? 'h2h-with' : 'h2h-vs'}
      className={`t-bg-panel bg-panel t-border border border-line p-4 flex flex-col gap-3 min-w-0 cursor-pointer transition-all duration-150 t-num ${
        selected ? 'ring-2 ring-blue-ink bg-panel-hover' : 'hover:bg-panel-hover'
      }`}
    >
      {/* 2-Row Collision-Free Header (UI-02, STAT-06) */}
      <div className="flex flex-col gap-2 border-b t-border border-line pb-2.5">
        <div className="flex items-center justify-between gap-3 min-w-0">
          <div className="flex items-center gap-2 min-w-0">
            <span
              className={`px-2.5 py-1 text-sm font-extrabold uppercase shrink-0 ${
                teammate
                  ? 't-bg-blue-slab bg-blue-slab text-white'
                  : 't-bg-orange-slab bg-orange-slab t-text-on-slab text-page'
              }`}
            >
              {roleTitle}
            </span>
            <SectionLabel className="truncate">
              {label} · {games} {gameWord}
            </SectionLabel>
          </div>
          <span
            className={`font-extrabold text-2xl sm:text-[28px] leading-none whitespace-nowrap shrink-0 ${recordInk(
              wins,
              losses,
            )}`}
          >
            {formatRecord(wins, losses)}
          </span>
        </div>

        <div className="flex items-center justify-between gap-2 min-w-0">
          {tag ? (
            <span
              className={`font-bold text-xs sm:text-sm tracking-t14 text-page px-2 py-0.5 whitespace-nowrap ${recordFill(
                wins,
                losses,
              )}`}
            >
              {tag}
            </span>
          ) : (
            <span className="text-xs font-bold text-muted uppercase tracking-wider truncate">
              {selected ? 'FILTER ACTIVE · CLICK TO RESET ROLE' : 'CLICK TO FILTER MATCHES'}
            </span>
          )}
          <span className="t-font-mono font-extrabold text-sm text-muted whitespace-nowrap shrink-0">
            {games > 0 ? `${wr}% WIN RATE` : '0 GAMES'}
          </span>
        </div>
      </div>

      <RecordCells wins={wins} losses={losses} count={10} cellClass="flex-1 h-[10px]" />

      {games === 0 ? (
        <div className="t-bg-page bg-page t-border border border-line py-4 px-4 text-center text-sm font-extrabold text-muted uppercase">
          {emptyMessage}
        </div>
      ) : !synced ? (
        <div className="t-bg-page bg-page t-border border border-line overflow-x-auto">
          <div className="min-w-[420px]">
            <div className="grid grid-cols-9 items-center py-1.5 px-2 text-right text-sm font-extrabold text-muted border-b t-border border-line uppercase">
              <span className={STAT_LABEL_CLASS.score}>PTS</span>
              <span className={STAT_LABEL_CLASS.goals}>G</span>
              <span className={STAT_LABEL_CLASS.assists}>A</span>
              <span className={STAT_LABEL_CLASS.saves}>SV</span>
              <span className={STAT_LABEL_CLASS.shotpct}>SH%</span>
              <span className={STAT_LABEL_CLASS.demos}>DEM</span>
              <span className={STAT_LABEL_CLASS.bpm}>BPM</span>
              <span className={STAT_LABEL_CLASS.boost}>BST</span>
              <span className={STAT_LABEL_CLASS.speed}>SUP%</span>
            </div>
            <div className="grid grid-cols-9 items-center py-2 px-2 text-right text-base font-extrabold t-font-mono text-muted">
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span>—</span>
            </div>
          </div>
          <div
            data-testid={teammate ? 'h2h-with-unsynced' : 'h2h-vs-unsynced'}
            className="px-3 py-1.5 border-t border-line text-center text-xs font-semibold text-muted"
          >
            No synced role telemetry
          </div>
        </div>
      ) : (
        <div className="t-bg-page bg-page t-border border border-line overflow-x-auto">
          <div className="min-w-[420px]">
            <div className="grid grid-cols-9 items-center py-1.5 px-2 text-right text-sm font-extrabold text-muted border-b t-border border-line uppercase">
              <span className={STAT_LABEL_CLASS.score}>PTS</span>
              <span className={STAT_LABEL_CLASS.goals}>G</span>
              <span className={STAT_LABEL_CLASS.assists}>A</span>
              <span className={STAT_LABEL_CLASS.saves}>SV</span>
              <span className={STAT_LABEL_CLASS.shotpct}>SH%</span>
              <span className={STAT_LABEL_CLASS.demos}>DEM</span>
              <span className={STAT_LABEL_CLASS.bpm}>BPM</span>
              <span className={STAT_LABEL_CLASS.boost}>BST</span>
              <span className={STAT_LABEL_CLASS.speed}>SUP%</span>
            </div>
            <div className="grid grid-cols-9 items-center py-2 px-2 text-right text-base font-extrabold t-font-mono">
              <span className={STAT_LABEL_CLASS.score}>{pts.toLocaleString()}</span>
              <span className={STAT_LABEL_CLASS.goals}>{g}</span>
              <span className={STAT_LABEL_CLASS.assists}>{a}</span>
              <span className={STAT_LABEL_CLASS.saves}>{sv}</span>
              <span className={STAT_LABEL_CLASS.shotpct}>{shPct}%</span>
              <span className={STAT_LABEL_CLASS.demos}>{dem}</span>
              <span className={STAT_LABEL_CLASS.bpm}>{bpm}</span>
              <span className={STAT_LABEL_CLASS.boost}>{bst}</span>
              <span className={STAT_LABEL_CLASS.speed}>{sup}%</span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
