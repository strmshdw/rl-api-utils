import React, { useState } from 'react';
import { PlayerSummary, PlayerRanksSnapshot } from '../../types/api';
import { parsePlatform } from '../../utils/platforms';
import { formatRelativeTime, formatWinRate, getRankTierColor } from '../../utils/formatters';
import { Swords, Users, Shield, Copy, Check, Star, Flame, History } from 'lucide-react';

interface PlayerCardProps {
  player: PlayerSummary;
  onClick: () => void;
}

export const PlayerCard: React.FC<PlayerCardProps> = ({ player, onClick }) => {
  const [copied, setCopied] = useState(false);
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
  const mateWinRate = mateTotal > 0 ? formatWinRate(player.total_wins_as_teammate, player.total_losses_as_teammate) : '---';
  const matePct = mateTotal > 0 ? (player.total_wins_as_teammate / mateTotal) * 100 : 0;

  const oppTotal = player.total_wins_as_opponent + player.total_losses_as_opponent;
  const oppWinRate = oppTotal > 0 ? formatWinRate(player.total_wins_as_opponent, player.total_losses_as_opponent) : '---';
  const oppPct = oppTotal > 0 ? (player.total_wins_as_opponent / oppTotal) * 100 : 0;

  // Relationship & Synergy indicators
  const isTrustedAlly = mateTotal >= 2 && matePct >= 60;
  const isToughRival = oppTotal >= 2 && oppPct <= 40;
  const isFrequent = player.total_matches >= 3;

  const handleCopyId = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(player.player_id);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

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
      <div className="flex items-start justify-between gap-2 mb-2">
        <div className="truncate flex-1">
          <div className="flex items-center gap-2">
            <h3 className="font-bold text-white text-base truncate group-hover:text-cyan-300 transition-colors">
              {player.player_name || 'Unknown Player'}
            </h3>
          </div>
          <div className="flex items-center gap-1.5 text-[11px] text-slate-500 font-mono mt-0.5">
            <span className="truncate max-w-[170px]" title={player.player_id}>{player.player_id}</span>
            <button
              onClick={handleCopyId}
              title="Copy Player ID"
              className="text-slate-500 hover:text-slate-300 p-0.5 rounded transition-colors"
            >
              {copied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
            </button>
          </div>
        </div>
        <span className={`text-[10px] font-bold px-2 py-0.5 rounded border ${plat.badgeClass} flex-shrink-0`}>
          {plat.name}
        </span>
      </div>

      {/* Relationship & Synergy Badges */}
      <div className="flex flex-wrap items-center gap-1.5 mb-2.5 min-h-[22px]">
        {isTrustedAlly && (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-bold uppercase bg-emerald-950/50 text-emerald-300 border border-emerald-800/60 shadow-sm">
            <Star className="w-3 h-3 text-emerald-400 fill-emerald-400/20" />
            Trusted Ally
          </span>
        )}
        {isToughRival && (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-bold uppercase bg-rose-950/50 text-rose-300 border border-rose-800/60 shadow-sm">
            <Flame className="w-3 h-3 text-rose-400" />
            Tough Rival
          </span>
        )}
        {isFrequent && (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-bold uppercase bg-purple-950/50 text-purple-300 border border-purple-800/60 shadow-sm">
            <History className="w-3 h-3 text-purple-400" />
            Frequent
          </span>
        )}
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

      {/* Teammate vs Opponent Stats Breakdown with visual progress bars */}
      <div className="grid grid-cols-2 gap-2 text-xs mb-3">
        {/* Teammate Box */}
        <div className="rounded-lg bg-slate-950/60 border border-slate-800 p-2.5 flex flex-col justify-between">
          <div className="flex items-center justify-between text-slate-400 mb-1">
            <span className="flex items-center gap-1 font-medium">
              <Users className="w-3.5 h-3.5 text-cyan-400" />
              Teammate
            </span>
            <span className="font-bold text-cyan-400">{mateWinRate}</span>
          </div>
          <div className="text-[11px] text-slate-300 mb-1.5">
            {mateTotal > 0 ? (
              <span>
                <strong className="text-emerald-400">{player.total_wins_as_teammate}W</strong> -{' '}
                <strong className="text-rose-400">{player.total_losses_as_teammate}L</strong>
              </span>
            ) : (
              <span className="text-slate-500">0 matches</span>
            )}
          </div>
          {/* Mini progress bar */}
          <div className="w-full bg-slate-900 rounded-full h-1 overflow-hidden">
            <div className="bg-cyan-500 h-full" style={{ width: `${matePct}%` }} />
          </div>
        </div>

        {/* Opponent Box */}
        <div className="rounded-lg bg-slate-950/60 border border-slate-800 p-2.5 flex flex-col justify-between">
          <div className="flex items-center justify-between text-slate-400 mb-1">
            <span className="flex items-center gap-1 font-medium">
              <Swords className="w-3.5 h-3.5 text-amber-500" />
              Opponent
            </span>
            <span className="font-bold text-amber-400">{oppWinRate}</span>
          </div>
          <div className="text-[11px] text-slate-300 mb-1.5">
            {oppTotal > 0 ? (
              <span>
                <strong className="text-emerald-400">{player.total_wins_as_opponent}W</strong> -{' '}
                <strong className="text-rose-400">{player.total_losses_as_opponent}L</strong>
              </span>
            ) : (
              <span className="text-slate-500">0 matches</span>
            )}
          </div>
          {/* Mini progress bar */}
          <div className="w-full bg-slate-900 rounded-full h-1 overflow-hidden">
            <div className="bg-amber-500 h-full" style={{ width: `${oppPct}%` }} />
          </div>
        </div>
      </div>

      {/* Footer Info */}
      <div className="flex items-center justify-between text-[11px] text-slate-500 pt-2 border-t border-slate-800/80">
        <span className="font-medium text-slate-400">Total Encounters: <strong className="text-white">{player.total_matches}</strong></span>
        <span>Last seen {formatRelativeTime(player.last_seen_at)}</span>
      </div>
    </div>
  );
};
