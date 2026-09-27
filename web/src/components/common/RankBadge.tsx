import React from 'react';

interface RankBadgeProps {
  rankName?: string | null;
  tier?: number | null;
  division?: number | null;
}

export const RankBadge: React.FC<RankBadgeProps> = ({ rankName, tier }) => {
  const getRankTheme = (t?: number | null) => {
    if (t == null || isNaN(t) || t <= 0 || t > 22) {
      return 'text-slate-400 bg-slate-800/60 border-slate-700'; // Unranked / unknown / corrupt fallback
    }
    if (t <= 3) return 'text-amber-500 bg-amber-950/40 border-amber-800/60'; // Bronze
    if (t <= 6) return 'text-slate-200 bg-slate-700/40 border-slate-500/60'; // Silver
    if (t <= 9) return 'text-yellow-400 bg-yellow-950/40 border-yellow-600/60'; // Gold
    if (t <= 12) return 'text-cyan-400 bg-cyan-950/40 border-cyan-600/60'; // Platinum
    if (t <= 15) return 'text-sky-400 bg-sky-950/40 border-sky-500/60'; // Diamond
    if (t <= 18) return 'text-purple-400 bg-purple-950/40 border-purple-600/60'; // Champion
    if (t <= 21) return 'text-rose-400 bg-rose-950/40 border-rose-600/60'; // Grand Champion
    if (t === 22) return 'text-fuchsia-300 bg-gradient-to-r from-fuchsia-950/50 via-purple-950/50 to-cyan-950/50 border-fuchsia-400/60 animate-pulse'; // SSL
    return 'text-slate-400 bg-slate-800/60 border-slate-700'; // Fallback
  };

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-[11px] font-bold border ${getRankTheme(
        tier
      )} whitespace-nowrap`}
    >
      {rankName || 'Unranked'}
    </span>
  );
};
