import React from 'react';
import { HistoricalMatchRecord } from '../../types/api';
import { formatDuration, formatPlaylistName, formatRelativeTime } from '../../utils/formatters';
import { Trophy, Clock, ExternalLink, ShieldAlert } from 'lucide-react';

interface HistoricalMatchCardProps {
  match: HistoricalMatchRecord;
  onClick: () => void;
}

export const HistoricalMatchCard: React.FC<HistoricalMatchCardProps> = ({ match, onClick }) => {
  const result = match.result || (
    match.blue_score > match.orange_score ? 'victory' :
    match.orange_score > match.blue_score ? 'defeat' :
    match.blue_score === match.orange_score && match.blue_score > 0 ? 'draw' : ''
  );

  const isVictory = result === 'victory';
  const isDefeat = result === 'defeat';

  const borderColor = isVictory
    ? 'border-emerald-500/30 hover:border-emerald-500/60'
    : isDefeat
    ? 'border-rose-500/30 hover:border-rose-500/60'
    : 'border-slate-800 hover:border-slate-700';

  const glowBg = isVictory
    ? 'hover:shadow-[0_0_20px_rgba(16,185,129,0.12)]'
    : isDefeat
    ? 'hover:shadow-[0_0_20px_rgba(244,63,94,0.12)]'
    : 'hover:shadow-lg';

  const mapName = match.map_name || match.stats?.map_name || 'Standard Arena';
  const playlistName = formatPlaylistName(match.playlist);
  const matchDate = match.stats?.date || match.created_at;
  const ballchasingURL = match.ballchasing_url || match.stats?.ballchasing_url;

  const bluePlayers = match.stats?.players?.filter((p) => p.team === 0) || [];
  const orangePlayers = match.stats?.players?.filter((p) => p.team === 1) || [];

  return (
    <div
      onClick={onClick}
      className={`group relative overflow-hidden rounded-xl border bg-slate-900/60 p-4 transition-all duration-200 cursor-pointer backdrop-blur-sm ${borderColor} ${glowBg}`}
    >
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        {/* Left: Result Badge, Map & Playlist */}
        <div className="flex items-center gap-3">
          <div
            className={`flex items-center justify-center w-12 h-12 rounded-lg font-black text-xs uppercase tracking-wider border shrink-0 ${
              isVictory
                ? 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30'
                : isDefeat
                ? 'bg-rose-500/15 text-rose-400 border-rose-500/30'
                : 'bg-slate-800/60 text-slate-400 border-slate-700'
            }`}
          >
            {isVictory ? (
              <span className="flex flex-col items-center">
                <Trophy className="w-4 h-4 mb-0.5" />
                WIN
              </span>
            ) : isDefeat ? (
              <span className="flex flex-col items-center">
                <ShieldAlert className="w-4 h-4 mb-0.5" />
                LOSS
              </span>
            ) : (
              <span>DRAW</span>
            )}
          </div>

          <div>
            <div className="flex items-center gap-2">
              <h3 className="font-bold text-slate-100 text-sm group-hover:text-cyan-300 transition-colors">
                {mapName}
              </h3>
              {match.stats?.overtime && (
                <span className="px-1.5 py-0.5 text-[10px] font-mono font-bold rounded bg-amber-500/20 text-amber-300 border border-amber-500/30">
                  OT
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 text-xs text-slate-400 mt-0.5">
              <span className="text-cyan-400 font-semibold">{playlistName}</span>
              <span>•</span>
              <span className="flex items-center gap-1">
                <Clock className="w-3 h-3 text-slate-500" />
                {matchDate ? formatRelativeTime(matchDate) : 'Unknown Date'}
              </span>
              {match.stats?.duration ? (
                <>
                  <span>•</span>
                  <span>{formatDuration(match.stats.duration)}</span>
                </>
              ) : null}
            </div>
          </div>
        </div>

        {/* Center: Score Board */}
        <div className="flex items-center justify-center gap-3 px-4 py-1.5 rounded-lg bg-slate-950/70 border border-slate-800 shrink-0">
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold text-[#00a2ff]">BLUE</span>
            <span className="text-lg font-black font-mono text-[#00a2ff]">{match.blue_score}</span>
          </div>
          <span className="text-slate-600 font-bold">:</span>
          <div className="flex items-center gap-2">
            <span className="text-lg font-black font-mono text-[#ff7b00]">{match.orange_score}</span>
            <span className="text-xs font-bold text-[#ff7b00]">ORG</span>
          </div>
        </div>

        {/* Right: Rosters & Ballchasing link */}
        <div className="flex items-center justify-between sm:justify-end gap-3 text-xs">
          <div className="flex flex-col text-right">
            {bluePlayers.length > 0 && (
              <div className="text-[11px] text-slate-400 truncate max-w-[180px]">
                <span className="text-[#00a2ff] font-semibold">Blue: </span>
                {bluePlayers.map((p) => p.name).join(', ')}
              </div>
            )}
            {orangePlayers.length > 0 && (
              <div className="text-[11px] text-slate-400 truncate max-w-[180px]">
                <span className="text-[#ff7b00] font-semibold">Orange: </span>
                {orangePlayers.map((p) => p.name).join(', ')}
              </div>
            )}
          </div>

          {ballchasingURL && (
            <a
              href={ballchasingURL}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => e.stopPropagation()}
              className="p-1.5 rounded-lg bg-slate-800/60 hover:bg-slate-800 text-slate-400 hover:text-cyan-300 border border-slate-700 transition-colors"
              title="View replay on Ballchasing.com"
            >
              <ExternalLink className="w-4 h-4" />
            </a>
          )}
        </div>
      </div>
    </div>
  );
};
