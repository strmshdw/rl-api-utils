import React from 'react';
import { MMRHistoryEntry, PlaylistSessionStats } from '../../types/api';
import { SessionKpi, deltaInk, formatMmr, formatRecord, formatSigned, playlistLabels } from '../../utils/broadcast';
import { PANEL_LABEL } from '../common/ui';
import { Sparkline } from '../common/Sparkline';

interface SessionKpiStripProps {
  kpis: SessionKpi[];
  /** 'idle' = Live tab strip (48px values); 'page' = Session page strip (52px) with a playlists cell. */
  variant: 'idle' | 'page';
  playlists?: PlaylistSessionStats[];
  mmrHistory?: Record<number, MMRHistoryEntry[]>;
}

export const SessionKpiStrip: React.FC<SessionKpiStripProps> = ({ kpis, variant, playlists = [], mmrHistory }) => {
  const page = variant === 'page';
  return (
    <div
      data-testid="session-kpis"
      className={`grid bg-panel font-display ${
        page ? 'grid-cols-4 lg:grid-cols-[repeat(4,minmax(0,1fr))_minmax(0,1.3fr)]' : 'grid-cols-4'
      }`}
    >
      {kpis.map((k) => (
        <div
          key={k.key}
          data-kpi={k.key}
          className={`flex flex-col gap-1.5 min-w-0 border-r border-page ${page ? 'px-[26px] py-[22px]' : 'px-6 py-5'}`}
        >
          <span className={PANEL_LABEL}>{k.label}</span>
          <span className={`font-extrabold leading-none ${page ? 'text-[52px]' : 'text-[48px]'} ${k.ink}`}>{k.value}</span>
          <span className="font-medium text-[13px] tracking-t6 text-muted">{k.sub}</span>
        </div>
      ))}
      {page && (
        <div className="col-span-4 lg:col-span-1 px-[26px] py-[22px] flex flex-col gap-3 border-t border-page lg:border-t-0 min-w-0">
          <span className={PANEL_LABEL}>PLAYLISTS</span>
          {playlists.length === 0 && <span className="font-semibold text-[15px] tracking-t4 text-faint">—</span>}
          {playlists.map((pl) => (
            <div key={pl.playlist_id} className="flex justify-between items-center gap-3 font-semibold text-[17px] tracking-t4">
              <span className="min-w-0 truncate">
                {playlistLabels(pl.playlist_id, pl.playlist_name).name}{' '}
                <span className="text-muted">{formatRecord(pl.wins, pl.losses)}</span>
              </span>
              <div className="flex items-center gap-2.5 shrink-0">
                {mmrHistory?.[pl.playlist_id] && mmrHistory[pl.playlist_id].length > 0 && (
                  <Sparkline data={mmrHistory[pl.playlist_id]} width={64} height={20} />
                )}
                <span className="text-muted whitespace-nowrap">
                  {formatMmr(pl.current_mmr)} <span className={deltaInk(pl.mmr_delta)}>{formatSigned(pl.mmr_delta)}</span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
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
      <div key={pl.playlist_id} className="bg-panel px-[22px] py-4 flex items-center justify-between gap-4 font-display">
        <div className="flex flex-col gap-0.5 min-w-0">
          <span className="font-bold text-[22px] tracking-t4 truncate">{playlistLabels(pl.playlist_id, pl.playlist_name).name}</span>
          <span className="font-medium text-[13px] tracking-t8 text-muted">
            {formatRecord(pl.wins, pl.losses)} · {formatMmr(pl.initial_mmr)} → {formatMmr(pl.current_mmr)}
          </span>
        </div>
        <div className="flex items-center gap-4 shrink-0">
          {mmrHistory?.[pl.playlist_id] && mmrHistory[pl.playlist_id].length > 0 && (
            <Sparkline data={mmrHistory[pl.playlist_id]} width={84} height={26} />
          )}
          <span className={`font-extrabold text-[30px] ${deltaInk(pl.mmr_delta)}`}>{formatSigned(pl.mmr_delta)}</span>
        </div>
      </div>
    ))}
  </>
);
