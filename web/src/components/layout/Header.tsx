import React from 'react';
import { SessionResponse } from '../../types/api';
import { formatMMRDelta } from '../../utils/formatters';
import { Trophy, TrendingUp, TrendingDown, RotateCcw } from 'lucide-react';

interface HeaderProps {
  session: SessionResponse | null;
  activePlaylistId?: number;
  onResetSession?: () => void;
  inMatch?: boolean;
}

export const Header: React.FC<HeaderProps> = ({
  session,
  activePlaylistId,
  onResetSession,
  inMatch = false,
}) => {
  const playlists = session?.playlists ? Object.values(session.playlists) : [];

  return (
    <header data-testid="app-header" className="border-b border-slate-800 bg-slate-900/90 backdrop-blur-md sticky top-0 z-40">
      <div className="container mx-auto px-4 py-2.5">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          {/* Logo & Identity */}
          <div className="flex items-center gap-3">
            <div className="flex items-center justify-center w-8 h-8 rounded-xl bg-gradient-to-tr from-cyan-500 to-blue-600 text-slate-950 font-black text-xs shadow-[0_0_15px_rgba(6,182,212,0.4)]">
              RL
            </div>
            <div>
              <h1 className="text-sm font-black tracking-wider text-slate-100 uppercase flex items-center gap-2">
                RL Sync
                <span className="text-[10px] font-bold px-1.5 py-0.5 rounded bg-cyan-500/20 text-cyan-300 border border-cyan-500/40">
                  SESSION DASHBOARD
                </span>
              </h1>
            </div>
          </div>

          {/* Session Overview Stats */}
          {session && (
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-3 px-3 py-1.5 rounded-xl bg-slate-950/60 border border-slate-800 text-xs">
                <div className="flex items-center gap-1.5">
                  <Trophy className="w-3.5 h-3.5 text-amber-400" />
                  <span className="text-slate-400 font-medium">Record:</span>
                  <span className="font-black text-slate-100">
                    <strong className="text-emerald-400">{session.total_wins}W</strong> -{' '}
                    <strong className="text-rose-400">{session.total_losses}L</strong>
                  </span>
                </div>
                <div className="w-px h-3 bg-slate-800" />
                <div>
                  <span className="text-slate-400 font-medium">Win Rate: </span>
                  <span className="font-bold text-cyan-400">{session.win_rate.toFixed(1)}%</span>
                </div>
                <div className="w-px h-3 bg-slate-800" />
                <div>
                  <span className="text-slate-400 font-medium">Matches: </span>
                  <span className="font-bold text-slate-200">{session.total_matches}</span>
                </div>
              </div>

              {onResetSession && (
                <button
                  onClick={() => {
                    if (window.confirm('Reset all session stats and match history?')) {
                      onResetSession();
                    }
                  }}
                  title="Reset session counters"
                  className="p-2 rounded-xl text-slate-400 hover:text-rose-400 hover:bg-slate-800/80 border border-slate-800 transition-colors"
                >
                  <RotateCcw className="w-4 h-4" />
                </button>
              )}
            </div>
          )}
        </div>

        {/* Playlist MMR Carousel / Pill Bar (hidden during active match to preserve vertical headroom) */}
        {!inMatch && playlists.length > 0 && (
          <div className="mt-3 pt-2.5 border-t border-slate-800/60 flex items-center gap-2 overflow-x-auto no-scrollbar">
            <span className="text-[11px] font-bold text-slate-500 uppercase tracking-wider whitespace-nowrap">
              Playlists:
            </span>
            {playlists.map((pl) => {
              const isActive = activePlaylistId === pl.playlist_id;
              const hasPositiveDelta = pl.mmr_delta > 0;
              const hasNegativeDelta = pl.mmr_delta < 0;

              return (
                <div
                  key={pl.playlist_id}
                  className={`flex items-center gap-2 px-2.5 py-1 rounded-lg text-xs whitespace-nowrap border transition-all ${
                    isActive
                      ? 'bg-cyan-500/10 border-cyan-500/50 text-cyan-300 shadow-[0_0_10px_rgba(6,182,212,0.15)]'
                      : 'bg-slate-950/40 border-slate-800 text-slate-300'
                  }`}
                >
                  <span className="font-semibold">{pl.playlist_name}</span>
                  <span className="text-[11px] text-slate-400">
                    ({pl.wins}W-{pl.losses}L)
                  </span>
                  <span
                    className={`inline-flex items-center gap-0.5 font-mono font-bold text-[11px] ${
                      hasPositiveDelta
                        ? 'text-emerald-400'
                        : hasNegativeDelta
                        ? 'text-rose-400'
                        : 'text-slate-400'
                    }`}
                  >
                    {hasPositiveDelta ? (
                      <TrendingUp className="w-3 h-3" />
                    ) : hasNegativeDelta ? (
                      <TrendingDown className="w-3 h-3" />
                    ) : null}
                    {formatMMRDelta(pl.mmr_delta)}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </header>
  );
};
