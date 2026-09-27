import React from 'react';
import { PlayerSummary, PlayerRanksSnapshot } from '../../types/api';
import { parsePlatform } from '../../utils/platforms';
import { formatRelativeTime, formatWinRate, getRankTierColor } from '../../utils/formatters';
import { Swords, Users, Shield } from 'lucide-react';

interface PlayerCardProps {
  player: PlayerSummary;
  onClick: () => void;
}

export const PlayerCard: React.FC<PlayerCardProps> = ({ player, onClick }) => {
  const plat = parsePlatform(player.platform || player.player_id);

  // Parse ranks_json safely to display highest or active rank
  let primaryRank: { name: string; tier: number; mmr: number } | null = null;
  try {
    if (player.ranks_json && player.ranks_json !== '{}') {
      const ranks: PlayerRanksSnapshot = JSON.parse(player.ranks_json);
      // Prefer 2v2 (11) or 3v3 (13) if available
      const r = ranks['11'] || ranks['13'] || Object.values(ranks)[0];
      if (r) {
        primaryRank = { name: r.rank_name, tier: r.tier, mmr: r.mmr };
      }
    }
  } catch {
    // Graceful fallback on JSON parse failure
  }

  const mateTotal = player.total_wins_as_teammate + player.total_losses_as_teammate;
  const mateWinRate = formatWinRate(player.total_wins_as_teammate, player.total_losses_as_teammate);

  const oppTotal = player.total_wins_as_opponent + player.total_losses_as_opponent;
  const oppWinRate = formatWinRate(player.total_wins_as_opponent, player.total_losses_as_opponent);

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
      className="group relative rounded-xl border border-slate-800 bg-slate-900/80 p-4 hover:border-cyan-500/50 hover:bg-slate-900 transition-all duration-200 cursor-pointer shadow-lg hover:shadow-[0_0_15px_rgba(6,182,212,0.1)] focus:outline-none focus:ring-2 focus:ring-cyan-500/50 deferred-card"
    >
      {/* Header: Name & Platform */}
      <div className="flex items-start justify-between gap-2 mb-3">
        <div className="truncate">
          <h3 className="font-bold text-white text-base truncate group-hover:text-cyan-300 transition-colors">
            {player.player_name || 'Unknown Player'}
          </h3>
          <span className="text-[11px] text-slate-500 font-mono truncate block">
            {player.player_id}
          </span>
        </div>
        <span className={`text-[10px] font-bold px-2 py-0.5 rounded border ${plat.badgeClass}`}>
          {plat.name}
        </span>
      </div>

      {/* Rank Badge if available */}
      {primaryRank && (
        <div className="mb-3">
          <span
            className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-semibold border ${getRankTierColor(
              primaryRank.tier
            )}`}
          >
            <Shield className="w-3.5 h-3.5" />
            {primaryRank.name} ({primaryRank.mmr.toFixed(0)} MMR)
          </span>
        </div>
      )}

      {/* Teammate vs Opponent Stats Breakdown */}
      <div className="grid grid-cols-2 gap-2 text-xs mb-3">
        {/* Teammate Box */}
        <div className="rounded-lg bg-slate-950/60 border border-slate-800 p-2.5">
          <div className="flex items-center justify-between text-slate-400 mb-1">
            <span className="flex items-center gap-1 font-medium">
              <Users className="w-3.5 h-3.5 text-cyan-400" />
              Teammate
            </span>
            <span className="font-bold text-cyan-400">{mateWinRate}</span>
          </div>
          <div className="text-[11px] text-slate-300">
            {mateTotal > 0 ? (
              <span>
                <strong className="text-emerald-400">{player.total_wins_as_teammate}W</strong> -{' '}
                <strong className="text-rose-400">{player.total_losses_as_teammate}L</strong>
              </span>
            ) : (
              <span className="text-slate-500">0 matches</span>
            )}
          </div>
        </div>

        {/* Opponent Box */}
        <div className="rounded-lg bg-slate-950/60 border border-slate-800 p-2.5">
          <div className="flex items-center justify-between text-slate-400 mb-1">
            <span className="flex items-center gap-1 font-medium">
              <Swords className="w-3.5 h-3.5 text-amber-500" />
              Opponent
            </span>
            <span className="font-bold text-amber-400">{oppWinRate}</span>
          </div>
          <div className="text-[11px] text-slate-300">
            {oppTotal > 0 ? (
              <span>
                <strong className="text-emerald-400">{player.total_wins_as_opponent}W</strong> -{' '}
                <strong className="text-rose-400">{player.total_losses_as_opponent}L</strong>
              </span>
            ) : (
              <span className="text-slate-500">0 matches</span>
            )}
          </div>
        </div>
      </div>

      {/* Footer Info */}
      <div className="flex items-center justify-between text-[11px] text-slate-500 pt-2 border-t border-slate-800/80">
        <span>Total Encounters: {player.total_matches}</span>
        <span>Last seen {formatRelativeTime(player.last_seen_at)}</span>
      </div>
    </div>
  );
};
