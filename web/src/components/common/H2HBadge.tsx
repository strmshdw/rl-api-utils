import React from 'react';
import { PlayerMatchup } from '../../types/api';

interface H2HBadgeProps {
  matchup?: PlayerMatchup;
  isTeammate: boolean;
}

export const H2HBadge: React.FC<H2HBadgeProps> = ({ matchup, isTeammate }) => {
  if (!matchup || matchup.total_matches === 0) {
    return <span className="text-slate-500 font-mono text-[11px]">--</span>;
  }

  const wins = isTeammate ? matchup.wins_as_teammate : matchup.wins_as_opponent;
  const losses = isTeammate ? matchup.losses_as_teammate : matchup.losses_as_opponent;
  const total = wins + losses;

  if (total === 0) {
    return <span className="text-slate-500 font-mono text-[11px]">0-0</span>;
  }

  const winRate = ((wins / total) * 100).toFixed(0);

  const getColor = (rate: number) => {
    if (rate >= 60) return 'text-emerald-400 bg-emerald-950/30 border-emerald-800/40';
    if (rate >= 40) return 'text-slate-300 bg-slate-800/40 border-slate-700/40';
    return 'text-rose-400 bg-rose-950/30 border-rose-800/40';
  };

  return (
    <span
      title={`Teammate: ${matchup.wins_as_teammate}W-${matchup.losses_as_teammate}L | Opponent: ${matchup.wins_as_opponent}W-${matchup.losses_as_opponent}L (Total: ${matchup.total_matches})`}
      className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-mono font-bold border ${getColor(
        Number(winRate)
      )}`}
    >
      <span>{wins}W-{losses}L</span>
      <span className="text-[10px] opacity-80">({winRate}%)</span>
    </span>
  );
};
