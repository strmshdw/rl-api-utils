import React from 'react';
import { SessionMatchDetail } from '../../types/api';
import { formatDuration, formatMMRDelta, formatRelativeTime } from '../../utils/formatters';
import { Trophy, ShieldAlert, Minus, TrendingUp, TrendingDown, Clock, Swords } from 'lucide-react';

interface MatchCardProps {
  match: SessionMatchDetail;
  onClick: () => void;
}

export const MatchCard: React.FC<MatchCardProps> = ({ match, onClick }) => {
  const isVictory = match.result === 'victory';
  const isDefeat = match.result === 'defeat';

  // Card theme classes based on outcome
  const borderClass = isVictory
    ? 'border-emerald-500/40 hover:border-emerald-400 bg-slate-900/90 hover:shadow-[0_0_20px_rgba(16,185,129,0.15)]'
    : isDefeat
    ? 'border-rose-500/40 hover:border-rose-400 bg-slate-900/90 hover:shadow-[0_0_20px_rgba(244,63,94,0.15)]'
    : 'border-slate-700 hover:border-slate-500 bg-slate-900/90';

  const badgeConfig = isVictory
    ? { text: 'VICTORY', bg: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/40', icon: Trophy }
    : isDefeat
    ? { text: 'DEFEAT', bg: 'bg-rose-500/15 text-rose-400 border-rose-500/40', icon: ShieldAlert }
    : { text: 'DRAW', bg: 'bg-slate-700/40 text-slate-300 border-slate-600', icon: Minus };

  const BadgeIcon = badgeConfig.icon;

  // Separate teammates and opponents for quick preview
  const localPlayer = match.players.find((p) => p.is_local);
  const localTeamNum = localPlayer?.team_num ?? match.local_team ?? 0;
  const teammates = match.players.filter((p) => p.team_num === localTeamNum && !p.is_local);
  const opponents = match.players.filter((p) => p.team_num !== localTeamNum && p.team_num !== 255);

  const localColor = localTeamNum === 0 ? 'text-[#00a2ff]' : 'text-[#ff7b00]';
  const oppColor = localTeamNum === 0 ? 'text-[#ff7b00]' : 'text-[#00a2ff]';

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onClick();
        }
      }}
      className={`group relative rounded-xl border p-4 transition-all duration-200 cursor-pointer focus:outline-none focus:ring-2 focus:ring-cyan-500/50 deferred-card ${borderClass}`}
    >
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-3">
        {/* Outcome banner & Playlist */}
        <div className="flex items-center gap-3">
          <span
            className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-black tracking-wider uppercase border ${badgeConfig.bg}`}
          >
            <BadgeIcon className="w-3.5 h-3.5" />
            {badgeConfig.text}
          </span>
          <div>
            <h3 className="font-semibold text-white group-hover:text-cyan-300 transition-colors flex items-center gap-2">
              <Swords className="w-4 h-4 text-slate-400" />
              {match.playlist_name}
            </h3>
            <div className="flex items-center gap-2 text-xs text-slate-400">
              <span className="flex items-center gap-1">
                <Clock className="w-3.5 h-3.5" />
                {formatDuration(match.duration_seconds)}
              </span>
              <span>•</span>
              <span>{formatRelativeTime(match.ended_at)}</span>
            </div>
          </div>
        </div>

        {/* Score & MMR Delta */}
        <div className="flex items-center gap-5 justify-between md:justify-end">
          {/* Neon Scoreboard Pill */}
          <div className="flex items-center gap-2 px-3.5 py-1.5 rounded-lg bg-slate-950/80 border border-slate-800">
            <span className="text-xl font-extrabold text-[#00a2ff]">{match.blue_score}</span>
            <span className="text-slate-500 font-bold">:</span>
            <span className="text-xl font-extrabold text-[#ff7b00]">{match.orange_score}</span>
          </div>

          {/* MMR Change */}
          <div className="flex items-center gap-1.5 min-w-[90px] justify-end">
            {match.mmr_change > 0 ? (
              <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-bold bg-emerald-950/60 text-emerald-400 border border-emerald-800/60">
                <TrendingUp className="w-3.5 h-3.5" />
                {formatMMRDelta(match.mmr_change)} MMR
              </span>
            ) : match.mmr_change < 0 ? (
              <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-bold bg-rose-950/60 text-rose-400 border border-rose-800/60">
                <TrendingDown className="w-3.5 h-3.5" />
                {formatMMRDelta(match.mmr_change)} MMR
              </span>
            ) : (
              <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-bold bg-slate-800/60 text-slate-400 border border-slate-700">
                {formatMMRDelta(match.mmr_change)} MMR
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Roster Preview Strip */}
      <div className="mt-3 pt-3 border-t border-slate-800/60 flex items-center justify-between text-xs text-slate-400">
        <div className="flex items-center gap-2 overflow-hidden text-ellipsis whitespace-nowrap">
          <span className={`${localColor} font-medium`}>
            {localPlayer?.name ?? 'You'}
            {teammates.length > 0 && ` + ${teammates.map((t) => t.name).join(', ')}`}
          </span>
          <span className="text-slate-600 font-bold">vs</span>
          <span className={oppColor}>
            {opponents.length > 0 ? opponents.map((o) => o.name).join(', ') : 'Opponents'}
          </span>
        </div>
        <span className="text-slate-500 group-hover:text-cyan-400 group-hover:translate-x-0.5 transition-all text-[11px] font-semibold flex items-center gap-1">
          Full Box Scores &rarr;
        </span>
      </div>
    </div>
  );
};
