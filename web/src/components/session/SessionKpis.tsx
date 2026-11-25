import React from 'react';
import { MMRHistoryEntry, PlaylistSessionStats, SessionResponse } from '../../types/api';
import {
  SessionKpi,
  SessionPlaylistBreakdownItem,
  deltaInk,
  emptyRoleStats,
  formatMmr,
  formatRecord,
  formatSigned,
  hasSyncedRoleTelemetry,
  matchesPlaylistFilter,
  normalizeActiveModeKey,
  playlistFilterToParam,
  playlistLabels,
  summarizeSessionPlaylists,
} from '../../utils/broadcast';
import { PlayerRoleStatsSummary } from '../../types/api';
import { PANEL_LABEL, RecordCells } from '../common/ui';
import { Sparkline } from '../common/Sparkline';
import { StatMode, useDashboard } from '../../context/DashboardContext';
import { STAT_LABEL_CLASS } from '../../utils/statColors';

interface SessionKpiStripProps {
  kpis: SessionKpi[];
  /** 'idle' = Live tab strip; 'page' = Session page strip with a playlists cell. */
  variant: 'idle' | 'page';
  playlists?: PlaylistSessionStats[];
  mmrHistory?: Record<number, MMRHistoryEntry[]>;
  session?: SessionResponse | null;
  scopeLabel?: string;
}

function reconcileScopedKpis(kpis: SessionKpi[], session?: SessionResponse | null): SessionKpi[] {
  const plEntries = session?.playlists ? Object.values(session.playlists) : [];
  if (plEntries.length !== 1) return kpis;
  const authoritativeDelta = plEntries[0].mmr_delta;
  if (typeof authoritativeDelta !== 'number') return kpis;
  return kpis.map((k) =>
    k.key === 'mmr'
      ? {
          ...k,
          value: formatSigned(authoritativeDelta),
          ink: deltaInk(authoritativeDelta),
        }
      : k,
  );
}

export const SessionKpiStrip: React.FC<SessionKpiStripProps> = ({
  kpis,
  variant,
  playlists = [],
  mmrHistory,
  session,
  scopeLabel,
}) => {
  const effectiveScopeLabel = scopeLabel ?? 'SESSION';
  const page = variant === 'page';
  const effectiveKpis = React.useMemo(() => reconcileScopedKpis(kpis, session), [kpis, session]);

  // Find most played playlist if available
  const sessionPlaylists = session?.playlists ? Object.values(session.playlists) : playlists;
  const mostPlayed =
    session?.most_played_playlist ||
    (sessionPlaylists.length > 0
      ? (() => {
          const sorted = [...sessionPlaylists].sort((a, b) => b.matches_played - a.matches_played);
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
      className={`grid surface-plaque border border-outline t-radius font-display lg:grid-rows-[auto_auto_auto] ${
        page
          ? 'grid-cols-2 lg:grid-cols-[repeat(4,minmax(0,1fr))_minmax(0,1.35fr)]'
          : 'grid-cols-2 lg:grid-cols-4'
      }`}
    >
      {effectiveKpis.map((k) => {
        const isRecord = k.key === 'record';
        const isMmr = k.key === 'mmr';
        return (
          <div
            key={k.key}
            data-kpi={k.key}
            className="flex flex-col justify-between gap-1.5 lg:row-span-3 lg:grid lg:grid-rows-subgrid min-w-0 border-r border-page t-border px-5 py-4"
          >
            <div className="flex flex-wrap items-start justify-between gap-x-2 gap-y-1 min-w-0">
              <span className={`${PANEL_LABEL} leading-tight break-words`}>
                {isRecord ? `${k.label} (${effectiveScopeLabel})` : isMmr ? 'NET MMR' : k.label}
              </span>
              {isMmr && mostPlayed && (
                <span className="text-[11px] t-font-display font-bold px-1.5 py-0 t-bg-page t-border t-text-blue uppercase leading-tight break-words">
                  {playlistLabels(mostPlayed.id, mostPlayed.name).mode}
                </span>
              )}
            </div>
            <div className="flex items-baseline gap-2.5 self-end">
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
            <span className="font-semibold text-sm tracking-t6 text-muted leading-snug break-words">
              {isMmr && mostPlayed
                ? `${playlistLabels(mostPlayed.id, mostPlayed.name).name} (${mostPlayed.wins}–${mostPlayed.losses}) · ${k.sub}`
                : k.sub}
            </span>
          </div>
        );
      })}
      {page && (
        <div className="col-span-2 lg:col-span-1 lg:row-span-3 px-5 py-4 flex flex-col gap-2.5 border-t border-page lg:border-t-0 min-w-0">
          <span className={`${PANEL_LABEL} leading-tight break-words`}>PLAYLIST MMR PROGRESSION</span>
          {playlists.length === 0 && (
            <span className="font-semibold text-base tracking-t4 text-faint">—</span>
          )}
          {playlists.map((pl) => (
            <div
              key={pl.playlist_id}
              className="flex flex-wrap justify-between items-center gap-x-2 gap-y-1 font-semibold text-base tracking-t4"
            >
              <span
                className="min-w-0 leading-tight break-words"
                title={playlistLabels(pl.playlist_id, pl.playlist_name).name}
              >
                {playlistLabels(pl.playlist_id, pl.playlist_name).short}{' '}
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
  statMode?: StatMode;
  scopeLabel?: string;
}> = ({ session, statMode: statModeProp, scopeLabel }) => {
  const { statMode: contextStatMode } = useDashboard();
  const statMode = statModeProp ?? contextStatMode;
  const effectiveScopeLabel = scopeLabel ?? 'CURRENT SESSION';
  const isAvg = statMode === 'avg';

  const games = session?.total_matches ?? session?.matches?.length ?? 0;
  const wins = session?.total_wins ?? 0;
  const losses = session?.total_losses ?? 0;

  const forStats = session?.for_stats ?? emptyRoleStats(games, wins, losses);
  const againstStats = session?.against_stats ?? emptyRoleStats(games, losses, wins);

  const fmtVal = (synced: boolean, avgVal: number, totVal: number, decimals = 1) => {
    if (games === 0 || !synced) return '—';
    if (isAvg) {
      return decimals === 0 ? Math.round(avgVal).toString() : avgVal.toFixed(decimals);
    }
    return Math.round(totVal).toString();
  };

  const renderSplitRow = (
    rowLabel: string,
    labelColor: string,
    s: PlayerRoleStatsSummary,
    borderBottom: boolean
  ) => {
    const synced = games > 0 && hasSyncedRoleTelemetry(s);
    return (
      <div
        className={`grid grid-cols-12 px-4 py-2 ${
          borderBottom ? 'border-b t-border border-line ' : ''
        }items-center text-base t-font-mono t-hover-panel`}
      >
        <div className={`col-span-2 t-font-display font-extrabold text-sm uppercase tracking-wider leading-tight break-words ${labelColor}`}>
          {rowLabel}
        </div>
        <div className={`col-span-1 text-right font-bold ${STAT_LABEL_CLASS.games}`}>{s.games}G</div>
        {!synced ? (
          <div className="col-span-9 flex items-center justify-between gap-2 pl-4 text-xs t-font-display t-text-faint">
            <span>{games === 0 ? 'No matches recorded yet' : 'No synced role telemetry'}</span>
            <span className="t-font-mono">— · — · — · — · — · — · — · — · —</span>
          </div>
        ) : (
          <>
            <div className={`col-span-1 text-right font-bold ${STAT_LABEL_CLASS.score}`}>
              {fmtVal(synced, s.avg_score, s.total_score, 0)}
            </div>
            <div className={`col-span-1 text-right font-bold ${STAT_LABEL_CLASS.goals}`}>
              {fmtVal(synced, s.avg_goals, s.total_goals, 1)}
            </div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.assists}`}>
              {fmtVal(synced, s.avg_assists, s.total_assists, 1)}
            </div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.saves}`}>
              {fmtVal(synced, s.avg_saves, s.total_saves, 1)}
            </div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.shotpct}`}>
              {`${Math.round(s.shooting_pct)}%`}
            </div>
            <div className={`col-span-1 text-right font-bold ${STAT_LABEL_CLASS.demos}`}>
              {fmtVal(synced, s.avg_demos_inflicted, s.total_demos_inflicted, 1)}
            </div>
            <div className={`col-span-1 text-right ${s.avg_bpm > 0 ? STAT_LABEL_CLASS.bpm : 'text-faint'}`}>
              {s.avg_bpm > 0 ? Math.round(s.avg_bpm) : '—'}
            </div>
            <div className={`col-span-1 text-right ${s.avg_boost > 0 ? STAT_LABEL_CLASS.boost : 'text-faint'}`}>
              {s.avg_boost > 0 ? Math.round(s.avg_boost) : '—'}
            </div>
            <div className={`col-span-1 text-right ${s.avg_supersonic_pct > 0 ? STAT_LABEL_CLASS.speed : 'text-faint'}`}>
              {s.avg_supersonic_pct > 0 ? `${s.avg_supersonic_pct.toFixed(1)}%` : '—'}
            </div>
          </>
        )}
      </div>
    );
  };

  return (
    <div
      data-testid="session-for-against-table"
      className="surface-plaque border border-outline t-radius overflow-hidden font-display"
    >
      <div className="px-4 py-2 border-b t-border border-line flex flex-wrap items-center justify-between gap-2 t-bg-page">
        <div className="flex flex-wrap items-center gap-2.5">
          <span className="t-font-display font-bold text-xs uppercase tracking-widest t-text-strong">
            ◆ SESSION PERFORMANCE SPLIT ({effectiveScopeLabel})
          </span>
          <span className="text-xs t-font-mono font-bold px-2 py-0.5 cartouche-mono">
            {isAvg ? 'SHOWING PER-GAME AVERAGES' : 'SHOWING CUMULATIVE TOTALS'}
          </span>
        </div>
      </div>

      <div
        role="region"
        aria-label="Historical performance split"
        tabIndex={0}
        className="overflow-x-auto"
      >
        <div className="min-w-[760px]">
          {/* Shared Column Header */}
          <div className="grid grid-cols-12 px-4 py-1.5 border-b t-border border-line text-sm t-font-display font-bold uppercase tracking-wider t-text-muted">
            <div className="col-span-2">TEAM SPLIT</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.games}`}>GAMES</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.score}`}>{isAvg ? 'PTS/G' : 'PTS'}</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.goals}`}>{isAvg ? 'GOALS/G' : 'GOALS'}</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.assists}`}>{isAvg ? 'ASSISTS/G' : 'ASSISTS'}</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.saves}`}>{isAvg ? 'SAVES/G' : 'SAVES'}</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.shotpct}`}>SHOT %</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.demos}`}>{isAvg ? 'DEMOS/G' : 'DEMOS'}</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.bpm}`}>BPM</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.boost}`}>BOOST</div>
            <div className={`col-span-1 text-right ${STAT_LABEL_CLASS.speed}`}>SUPER %</div>
          </div>

          {renderSplitRow('YOUR STATS (FOR)', 't-text-blue', forStats, true)}
          {renderSplitRow('OPPONENTS (AGAINST)', 't-text-orange', againstStats, false)}
        </div>
      </div>
    </div>
  );
};

function breakdownModeTitle(item: SessionPlaylistBreakdownItem): string {
  if (item.playlistId === 11) return '2V2 DOUBLES';
  if (item.playlistId === 13 || item.playlistId === 12) return '3V3 STANDARD';
  if (item.playlistId === 10) return '1V1 DUEL';
  if (item.playlistId === 2) return 'CASUAL 2V2';
  if (item.playlistId === 3) return 'CASUAL 3V3';
  if (item.playlistId === 1) return 'CASUAL 1V1';
  return item.shortLabel || item.label;
}

/**
 * Clickable per-mode session breakdown strip (`2V2 DOUBLES · 3–1 · 1485 (+29)` | `3V3 STANDARD ...`).
 * Strictly surfaces concrete game modes played in the session without cross-mode rollups.
 */
export const SessionModeBreakdownStrip: React.FC<{
  session: SessionResponse | null;
  activeFilter: string;
  onSelectFilter: (filterKey: string) => void;
}> = ({ session, activeFilter, onSelectFilter }) => {
  const items = React.useMemo(() => summarizeSessionPlaylists(session), [session]);
  if (items.length === 0) return null;

  const normalizedActive = normalizeActiveModeKey(activeFilter);

  return (
    <div
      data-testid="session-mode-breakdown"
      role="group"
      aria-label="Session mode breakdown"
      className="flex flex-wrap items-center gap-2"
    >
      {items.map((item) => {
        const itemParam = playlistFilterToParam(item.filterKey) || item.filterKey;
        const isActive =
          normalizedActive === itemParam || matchesPlaylistFilter(item.playlistId, normalizedActive);
        return (
          <button
            key={item.playlistId}
            type="button"
            aria-pressed={isActive}
            onClick={() => onSelectFilter(item.filterKey)}
            className={`toolbar-btn px-3 py-1.5 t-border border t-radius t-font-display font-bold text-xs uppercase tracking-wider flex items-center gap-1.5 cursor-pointer transition-colors duration-120 ${
              isActive
                ? 't-bg-blue-ink t-text-on-accent border-blue-ink'
                : 'surface-plaque border-outline t-text-ink hover:border-ink'
            }`}
          >
            <span className="font-extrabold">{breakdownModeTitle(item)}</span>
            <span className={isActive ? 'opacity-80' : 't-text-muted'}>·</span>
            <span className="t-font-mono">{formatRecord(item.wins, item.losses)}</span>
            {(item.currentMmr > 0 || item.netMmr !== 0) && (
              <>
                <span className={isActive ? 'opacity-80' : 't-text-muted'}>·</span>
                <span className="t-font-mono">
                  {item.currentMmr > 0 ? `${formatMmr(item.currentMmr)} ` : ''}
                  <span className={isActive ? 'font-extrabold' : `font-extrabold ${deltaInk(item.netMmr)}`}>
                    ({formatSigned(item.netMmr)})
                  </span>
                </span>
              </>
            )}
          </button>
        );
      })}
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
        className="surface-plaque border border-outline px-5 py-3.5 flex flex-wrap items-center justify-between gap-4 font-display t-radius"
      >
        <div className="flex flex-col gap-0.5 min-w-0">
          <span className="font-bold text-xl tracking-t4 leading-tight break-words">
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

