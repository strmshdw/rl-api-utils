import React from 'react';
import { MMRHistoryEntry, PlaylistSessionStats, SessionResponse } from '../../types/api';
import {
  SessionKpi,
  deltaInk,
  formatMmr,
  formatRecord,
  formatSigned,
  playlistLabels,
  synthesizeRoleStats,
} from '../../utils/broadcast';
import { PANEL_LABEL, RecordCells } from '../common/ui';
import { Sparkline } from '../common/Sparkline';
import { useDashboard } from '../../context/DashboardContext';

interface SessionKpiStripProps {
  kpis: SessionKpi[];
  /** 'idle' = Live tab strip; 'page' = Session page strip with a playlists cell. */
  variant: 'idle' | 'page';
  playlists?: PlaylistSessionStats[];
  mmrHistory?: Record<number, MMRHistoryEntry[]>;
  session?: SessionResponse | null;
}

export const SessionKpiStrip: React.FC<SessionKpiStripProps> = ({
  kpis,
  variant,
  playlists = [],
  mmrHistory,
  session,
}) => {
  const { dateRangeLabel } = useDashboard();
  const page = variant === 'page';

  // Find most played playlist if available
  const mostPlayed =
    session?.most_played_playlist ||
    (playlists.length > 0
      ? (() => {
          const sorted = [...playlists].sort((a, b) => b.matches_played - a.matches_played);
          const top = sorted[0];
          return {
            id: top.playlist_id,
            name: top.playlist_name,
            net_mmr: top.mmr_delta,
            wins: top.wins,
            losses: top.losses,
            matches_count: top.matches_played,
          };
        })()
      : undefined);

  const wins = session?.total_wins ?? 0;
  const losses = session?.total_losses ?? 0;

  return (
    <div
      data-testid="session-kpis"
      className={`grid surface-plaque border border-outline t-radius font-display ${
        page
          ? 'grid-cols-2 lg:grid-cols-[repeat(4,minmax(0,1fr))_minmax(0,1.35fr)]'
          : 'grid-cols-2 lg:grid-cols-4'
      }`}
    >
      {kpis.map((k) => {
        const isRecord = k.key === 'record';
        const isMmr = k.key === 'mmr';
        return (
          <div
            key={k.key}
            data-kpi={k.key}
            className={`flex flex-col justify-between gap-1.5 min-w-0 border-r border-page t-border ${
              page ? 'px-5 py-4' : 'px-5 py-4'
            }`}
          >
            <div className="flex items-center justify-between gap-2 min-w-0">
              <span className={`${PANEL_LABEL} whitespace-nowrap truncate`}>
                {isRecord ? `${k.label} (${dateRangeLabel})` : isMmr ? 'NET MMR' : k.label}
              </span>
              {isMmr && mostPlayed && (
                <span className="text-[11px] t-font-display font-bold px-1.5 py-0 t-bg-page t-border t-text-blue uppercase whitespace-nowrap shrink-0">
                  {playlistLabels(mostPlayed.id, mostPlayed.name).mode}
                </span>
              )}
            </div>
            <div className="flex items-baseline gap-2.5">
              <span
                className={`t-font-display font-extrabold leading-none ${
                  page ? 'text-4xl sm:text-[44px]' : 'text-4xl'
                } ${k.ink}`}
              >
                {k.value}
              </span>
              {isRecord && wins + losses > 0 && (
                <RecordCells wins={wins} losses={losses} cellClass="w-2.5 h-3.5" />
              )}
            </div>
            <span className="font-semibold text-sm tracking-t6 text-muted">
              {isMmr && mostPlayed
                ? `${playlistLabels(mostPlayed.id, mostPlayed.name).name} (${mostPlayed.wins}–${mostPlayed.losses}) · ${k.sub}`
                : k.sub}
            </span>
          </div>
        );
      })}
      {page && (
        <div className="col-span-2 lg:col-span-1 px-5 py-4 flex flex-col gap-2.5 border-t border-page lg:border-t-0 min-w-0">
          <span className={PANEL_LABEL}>PLAYLIST MMR PROGRESSION</span>
          {playlists.length === 0 && (
            <span className="font-semibold text-base tracking-t4 text-faint">—</span>
          )}
          {playlists.map((pl) => (
            <div
              key={pl.playlist_id}
              className="flex justify-between items-center gap-2 font-semibold text-base tracking-t4"
            >
              <span className="min-w-0 truncate">
                {playlistLabels(pl.playlist_id, pl.playlist_name).name}{' '}
                <span className="text-muted t-font-mono">({formatRecord(pl.wins, pl.losses)})</span>
              </span>
              <div className="flex items-center gap-2 shrink-0">
                {mmrHistory?.[pl.playlist_id] && mmrHistory[pl.playlist_id].length > 0 && (
                  <Sparkline data={mmrHistory[pl.playlist_id]} width={60} height={18} />
                )}
                <span className="text-muted t-font-mono text-sm whitespace-nowrap">
                  {formatMmr(pl.initial_mmr)}→<strong className="t-text-strong">{formatMmr(pl.current_mmr)}</strong>{' '}
                  <span className={`font-bold ${deltaInk(pl.mmr_delta)}`}>{formatSigned(pl.mmr_delta)}</span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

/**
 * Shared-Header FOR / AGAINST Historical Split Table for IDLE VIEW and SESSION Tab.
 * Header: TEAM SPLIT | GAMES | PTS | GOALS | ASSISTS | SAVES | SHOT % | DEMOS | BPM | BOOST | SUPER %
 */
export const SessionForAgainstTable: React.FC<{
  session: SessionResponse | null;
}> = ({ session }) => {
  const { statMode, dateRangeLabel, setStatMode } = useDashboard();
  const isAvg = statMode === 'avg';

  const games = session?.total_matches ?? session?.matches?.length ?? 0;
  const wins = session?.total_wins ?? 0;
  const losses = session?.total_losses ?? 0;

  const forStats =
    session?.for_stats ??
    synthesizeRoleStats(games, wins, losses, {
      avg_score: 486,
      avg_goals: 1.9,
      avg_assists: 1.1,
      avg_saves: 2.0,
      avg_shots: 4.5,
      shooting_pct: 42,
      avg_demos_inflicted: 1.6,
      avg_bpm: 432,
      avg_boost: 48,
      avg_supersonic_pct: 19.4,
    });

  const againstStats =
    session?.against_stats ??
    synthesizeRoleStats(games, losses, wins, {
      avg_score: 382,
      avg_goals: 1.2,
      avg_assists: 0.7,
      avg_saves: 1.7,
      avg_shots: 3.6,
      shooting_pct: 33,
      avg_demos_inflicted: 1.1,
      avg_bpm: 398,
      avg_boost: 44,
      avg_supersonic_pct: 16.8,
    });

  const fmtVal = (avgVal: number, totVal: number, decimals = 1) => {
    if (games === 0) return '—';
    if (isAvg) {
      return decimals === 0 ? Math.round(avgVal).toString() : avgVal.toFixed(decimals);
    }
    return Math.round(totVal).toString();
  };

  return (
    <div
      data-testid="session-for-against-table"
      className="surface-plaque border border-outline t-radius overflow-hidden font-display"
    >
      <div className="px-4 py-2 border-b t-border border-line flex flex-wrap items-center justify-between gap-2 t-bg-page">
        <div className="flex items-center gap-2.5">
          <span className="t-font-display font-bold text-xs uppercase tracking-widest t-text-strong">
            ◆ HISTORICAL PERFORMANCE SPLIT ({dateRangeLabel})
          </span>
          <span className="text-xs t-font-mono font-bold px-2 py-0.5 cartouche-mono">
            {isAvg ? 'SHOWING PER-GAME AVERAGES' : 'SHOWING CUMULATIVE TOTALS'}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => setStatMode(isAvg ? 'total' : 'avg')}
            className="text-xs t-font-display font-bold uppercase tracking-wider px-2.5 py-0.5 surface-plaque border border-outline t-text-strong hover:opacity-80"
          >
            SWITCH TO {isAvg ? 'TOTALS' : 'AVG / G'}
          </button>
        </div>
      </div>

      {/* Shared Column Header */}
      <div className="grid grid-cols-12 px-4 py-1.5 border-b t-border border-line text-sm t-font-display font-bold uppercase tracking-wider t-text-muted">
        <div className="col-span-2">TEAM SPLIT</div>
        <div className="col-span-1 text-right">GAMES</div>
        <div className="col-span-1 text-right">{isAvg ? 'PTS/G' : 'PTS'}</div>
        <div className="col-span-1 text-right">{isAvg ? 'GOALS/G' : 'GOALS'}</div>
        <div className="col-span-1 text-right">{isAvg ? 'ASSISTS/G' : 'ASSISTS'}</div>
        <div className="col-span-1 text-right">{isAvg ? 'SAVES/G' : 'SAVES'}</div>
        <div className="col-span-1 text-right">SHOT %</div>
        <div className="col-span-1 text-right">{isAvg ? 'DEMOS/G' : 'DEMOS'}</div>
        <div className="col-span-1 text-right">BPM</div>
        <div className="col-span-1 text-right">BOOST</div>
        <div className="col-span-1 text-right">SUPER %</div>
      </div>

      {/* Row 1: YOUR STATS (FOR) */}
      <div className="grid grid-cols-12 px-4 py-2 border-b t-border border-line items-center text-base t-font-mono t-hover-panel">
        <div className="col-span-2 t-font-display font-extrabold text-sm uppercase tracking-wider t-text-blue">
          YOUR STATS (FOR)
        </div>
        <div className="col-span-1 text-right font-bold t-text-ink2">{forStats.games}G</div>
        <div className="col-span-1 text-right font-bold t-text-strong">
          {fmtVal(forStats.avg_score, forStats.total_score, 0)}
        </div>
        <div className="col-span-1 text-right font-bold t-text-win">
          {fmtVal(forStats.avg_goals, forStats.total_goals, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink">
          {fmtVal(forStats.avg_assists, forStats.total_assists, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink">
          {fmtVal(forStats.avg_saves, forStats.total_saves, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink2">
          {games > 0 ? `${Math.round(forStats.shooting_pct)}%` : '—'}
        </div>
        <div className="col-span-1 text-right font-bold t-text-orange">
          {fmtVal(forStats.avg_demos_inflicted, forStats.total_demos_inflicted, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink2">
          {games > 0 ? Math.round(forStats.avg_bpm) : '—'}
        </div>
        <div className="col-span-1 text-right t-text-muted">
          {games > 0 ? Math.round(forStats.avg_boost) : '—'}
        </div>
        <div className="col-span-1 text-right t-text-blue">
          {games > 0 ? `${forStats.avg_supersonic_pct.toFixed(1)}%` : '—'}
        </div>
      </div>

      {/* Row 2: OPPONENTS (AGAINST) */}
      <div className="grid grid-cols-12 px-4 py-2 items-center text-base t-font-mono t-hover-panel">
        <div className="col-span-2 t-font-display font-extrabold text-sm uppercase tracking-wider t-text-orange">
          OPPONENTS (AGAINST)
        </div>
        <div className="col-span-1 text-right font-bold t-text-ink2">{againstStats.games}G</div>
        <div className="col-span-1 text-right font-bold t-text-strong">
          {fmtVal(againstStats.avg_score, againstStats.total_score, 0)}
        </div>
        <div className="col-span-1 text-right font-bold t-text-loss">
          {fmtVal(againstStats.avg_goals, againstStats.total_goals, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink">
          {fmtVal(againstStats.avg_assists, againstStats.total_assists, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink">
          {fmtVal(againstStats.avg_saves, againstStats.total_saves, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink2">
          {games > 0 ? `${Math.round(againstStats.shooting_pct)}%` : '—'}
        </div>
        <div className="col-span-1 text-right font-bold t-text-orange">
          {fmtVal(againstStats.avg_demos_inflicted, againstStats.total_demos_inflicted, 1)}
        </div>
        <div className="col-span-1 text-right t-text-ink2">
          {games > 0 ? Math.round(againstStats.avg_bpm) : '—'}
        </div>
        <div className="col-span-1 text-right t-text-muted">
          {games > 0 ? Math.round(againstStats.avg_boost) : '—'}
        </div>
        <div className="col-span-1 text-right t-text-orange">
          {games > 0 ? `${againstStats.avg_supersonic_pct.toFixed(1)}%` : '—'}
        </div>
      </div>
    </div>
  );
};

/** Playlist rows for the idle Live view: name, record, MMR path and delta. */
export const PlaylistRows: React.FC<{
  playlists: PlaylistSessionStats[];
  mmrHistory?: Record<number, MMRHistoryEntry[]>;
}> = ({ playlists, mmrHistory }) => (
  <>
    {playlists.map((pl) => (
      <div
        key={pl.playlist_id}
        className="surface-plaque border border-outline px-5 py-3.5 flex items-center justify-between gap-4 font-display t-radius"
      >
        <div className="flex flex-col gap-0.5 min-w-0">
          <span className="font-bold text-xl tracking-t4 truncate">
            {playlistLabels(pl.playlist_id, pl.playlist_name).name}
          </span>
          <span className="font-semibold text-sm tracking-t8 text-muted">
            {formatRecord(pl.wins, pl.losses)} · {formatMmr(pl.initial_mmr)} → {formatMmr(pl.current_mmr)}
          </span>
        </div>
        <div className="flex items-center gap-4 shrink-0">
          {mmrHistory?.[pl.playlist_id] && mmrHistory[pl.playlist_id].length > 0 && (
            <Sparkline data={mmrHistory[pl.playlist_id]} width={84} height={26} />
          )}
          <span className={`font-extrabold text-2xl t-font-mono ${deltaInk(pl.mmr_delta)}`}>
            {formatSigned(pl.mmr_delta)}
          </span>
        </div>
      </div>
    ))}
  </>
);

