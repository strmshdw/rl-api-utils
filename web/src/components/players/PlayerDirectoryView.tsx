import React, { useState, useMemo } from 'react';
import { usePlayerSearch } from '../../hooks/usePlayerSearch';
import { PlayerSearchBar } from './PlayerSearchBar';
import { PlayerCard } from './PlayerCard';
import { PlayerProfileModal } from './PlayerProfileModal';
import { PlayerSummary } from '../../types/api';
import {
  Users,
  ChevronLeft,
  ChevronRight,
  AlertCircle,
  Star,
  Flame,
  ArrowUpDown,
  History,
} from 'lucide-react';

type RoleFilter = 'all' | 'frequent' | 'teammate' | 'opponent' | 'synergy';
type PlayerSort = 'recent' | 'encounters' | 'mate-winrate' | 'opp-winrate' | 'name';

interface DirectoryInsights {
  frequentCount: number;
  bestMate: { player: PlayerSummary; rate: number } | null;
  topRival: { player: PlayerSummary; oppMatches: number } | null;
}

export const PlayerDirectoryView: React.FC = () => {
  const {
    query,
    setQuery,
    platform,
    setPlatform,
    page,
    setPage,
    players,
    total,
    totalPages,
    isLoading,
    error,
  } = usePlayerSearch();

  const [selectedPlayerId, setSelectedPlayerId] = useState<string | null>(null);
  const [roleFilter, setRoleFilter] = useState<RoleFilter>('all');
  const [sortBy, setSortBy] = useState<PlayerSort>('recent');

  // Directory scouting insights calculated from current loaded player pool
  const directoryInsights = useMemo<DirectoryInsights | null>(() => {
    if (!players || players.length === 0) return null;

    const frequentCount = players.filter((p) => p.total_matches >= 2).length;

    // Best teammate synergy (min 2 teammate matches)
    let bestMate: { player: PlayerSummary; rate: number } | null = null;
    players.forEach((p) => {
      const mateTotal = p.total_wins_as_teammate + p.total_losses_as_teammate;
      if (mateTotal >= 2) {
        const rate = (p.total_wins_as_teammate / mateTotal) * 100;
        if (!bestMate || rate > bestMate.rate) {
          bestMate = { player: p, rate };
        }
      }
    });

    // Top rival (min 2 opponent matches with lowest opponent win rate)
    let topRival: { player: PlayerSummary; oppMatches: number } | null = null;
    players.forEach((p) => {
      const oppTotal = p.total_wins_as_opponent + p.total_losses_as_opponent;
      if (oppTotal >= 2) {
        if (!topRival || oppTotal > topRival.oppMatches) {
          topRival = { player: p, oppMatches: oppTotal };
        }
      }
    });

    return {
      frequentCount,
      bestMate,
      topRival,
    };
  }, [players]);

  const bestMate = directoryInsights?.bestMate;
  const topRival = directoryInsights?.topRival;

  // Filter & sort loaded players
  const displayedPlayers = useMemo(() => {
    let list = [...players];

    // Role filtering
    if (roleFilter === 'frequent') {
      list = list.filter((p) => p.total_matches >= 2);
    } else if (roleFilter === 'teammate') {
      list = list.filter((p) => p.total_wins_as_teammate + p.total_losses_as_teammate > 0);
    } else if (roleFilter === 'opponent') {
      list = list.filter((p) => p.total_wins_as_opponent + p.total_losses_as_opponent > 0);
    } else if (roleFilter === 'synergy') {
      list = list.filter((p) => {
        const mateTotal = p.total_wins_as_teammate + p.total_losses_as_teammate;
        return mateTotal >= 1 && (p.total_wins_as_teammate / mateTotal) >= 0.5;
      });
    }

    // Sorting
    list.sort((a, b) => {
      switch (sortBy) {
        case 'encounters':
          return b.total_matches - a.total_matches;
        case 'mate-winrate': {
          const aTotal = a.total_wins_as_teammate + a.total_losses_as_teammate;
          const bTotal = b.total_wins_as_teammate + b.total_losses_as_teammate;
          const aRate = aTotal > 0 ? a.total_wins_as_teammate / aTotal : -1;
          const bRate = bTotal > 0 ? b.total_wins_as_teammate / bTotal : -1;
          return bRate - aRate;
        }
        case 'opp-winrate': {
          const aTotal = a.total_wins_as_opponent + a.total_losses_as_opponent;
          const bTotal = b.total_wins_as_opponent + b.total_losses_as_opponent;
          const aRate = aTotal > 0 ? a.total_wins_as_opponent / aTotal : -1;
          const bRate = bTotal > 0 ? b.total_wins_as_opponent / bTotal : -1;
          return bRate - aRate;
        }
        case 'name':
          return a.player_name.localeCompare(b.player_name);
        case 'recent':
        default:
          return new Date(b.last_seen_at).getTime() - new Date(a.last_seen_at).getTime();
      }
    });

    return list;
  }, [players, roleFilter, sortBy]);

  return (
    <div className="space-y-6 py-2">
      {/* Header & Title */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Users className="w-5 h-5 text-cyan-400" />
          <h2 className="text-lg font-bold text-white tracking-tight">
            Player Directory & Scouting Hub ({total})
          </h2>
        </div>
      </div>

      {/* Directory Scouting Insights KPI Ribbon */}
      {directoryInsights && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <div className="p-3 rounded-xl bg-slate-900/60 border border-slate-800 flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-cyan-500/10 text-cyan-400 border border-cyan-500/30">
              <Users className="w-5 h-5" />
            </div>
            <div>
              <div className="text-[10px] text-slate-400 font-semibold uppercase">Total Players Met</div>
              <div className="text-lg font-black text-white font-mono">{total}</div>
            </div>
          </div>

          <div className="p-3 rounded-xl bg-slate-900/60 border border-slate-800 flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-purple-500/10 text-purple-400 border border-purple-500/30">
              <History className="w-5 h-5" />
            </div>
            <div>
              <div className="text-[10px] text-slate-400 font-semibold uppercase">Frequent Matchups</div>
              <div className="text-lg font-black text-purple-300 font-mono">
                {directoryInsights.frequentCount}
                <span className="text-xs font-normal text-slate-400 font-sans ml-1">(2+ games)</span>
              </div>
            </div>
          </div>

          <div className="p-3 rounded-xl bg-slate-900/60 border border-slate-800 flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
              <Star className="w-5 h-5 fill-emerald-400/20" />
            </div>
            <div className="truncate">
              <div className="text-[10px] text-slate-400 font-semibold uppercase">Top Teammate Synergy</div>
              {bestMate ? (
                <div className="text-xs font-bold text-emerald-300 truncate" title={`${bestMate.player.player_name} (${bestMate.player.total_wins_as_teammate}W - ${bestMate.player.total_losses_as_teammate}L, ${bestMate.rate.toFixed(0)}%)`}>
                  {bestMate.player.player_name} <span className="font-mono text-[11px] text-emerald-400">({bestMate.rate.toFixed(0)}%)</span>
                </div>
              ) : (
                <div className="text-xs text-slate-500">None yet</div>
              )}
            </div>
          </div>

          <div className="p-3 rounded-xl bg-slate-900/60 border border-slate-800 flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-rose-500/10 text-rose-400 border border-rose-500/30">
              <Flame className="w-5 h-5" />
            </div>
            <div className="truncate">
              <div className="text-[10px] text-slate-400 font-semibold uppercase">Frequent Rival</div>
              {topRival ? (
                <div className="text-xs font-bold text-rose-300 truncate" title={`${topRival.player.player_name} (${topRival.player.total_wins_as_opponent}W - ${topRival.player.total_losses_as_opponent}L)`}>
                  {topRival.player.player_name} <span className="font-mono text-[11px] text-rose-400">({topRival.player.total_wins_as_opponent}W-{topRival.player.total_losses_as_opponent}L)</span>
                </div>
              ) : (
                <div className="text-xs text-slate-500">None yet</div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Search Bar & Platform Filters */}
      <PlayerSearchBar
        query={query}
        onQueryChange={setQuery}
        platform={platform}
        onPlatformChange={setPlatform}
      />

      {/* Role Filter Pills & In-View Sorter */}
      <div className="flex flex-wrap items-center justify-between gap-3 p-3 rounded-xl bg-slate-900/50 border border-slate-800/80">
        <div className="flex flex-wrap items-center gap-1.5 text-xs">
          <span className="text-[11px] text-slate-400 font-semibold uppercase mr-1">Filter:</span>
          <button
            onClick={() => setRoleFilter('all')}
            className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
              roleFilter === 'all'
                ? 'bg-cyan-500/20 text-cyan-300 border-cyan-500/50'
                : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
            }`}
          >
            All
          </button>
          <button
            onClick={() => setRoleFilter('frequent')}
            className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
              roleFilter === 'frequent'
                ? 'bg-purple-500/20 text-purple-300 border-purple-500/50'
                : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
            }`}
          >
            Frequent (2+)
          </button>
          <button
            onClick={() => setRoleFilter('teammate')}
            className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
              roleFilter === 'teammate'
                ? 'bg-cyan-500/20 text-cyan-300 border-cyan-500/50'
                : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
            }`}
          >
            Teammates
          </button>
          <button
            onClick={() => setRoleFilter('opponent')}
            className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
              roleFilter === 'opponent'
                ? 'bg-amber-500/20 text-amber-300 border-amber-500/50'
                : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
            }`}
          >
            Opponents
          </button>
          <button
            onClick={() => setRoleFilter('synergy')}
            className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
              roleFilter === 'synergy'
                ? 'bg-emerald-500/20 text-emerald-300 border-emerald-500/50'
                : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
            }`}
          >
            High Synergy (≥50%)
          </button>
        </div>

        {/* Sort dropdown */}
        <div className="flex items-center gap-1.5 text-xs">
          <ArrowUpDown className="w-3.5 h-3.5 text-slate-400" />
          <select
            value={sortBy}
            onChange={(e) => setSortBy(e.target.value as PlayerSort)}
            className="rounded-lg border border-slate-700 bg-slate-950/80 px-2.5 py-1 text-xs text-slate-200 focus:border-cyan-500 focus:outline-none"
          >
            <option value="recent">Most Recent Encounters</option>
            <option value="encounters">Most Encounters</option>
            <option value="mate-winrate">Best Teammate Win Rate</option>
            <option value="opp-winrate">Best Opponent Win Rate</option>
            <option value="name">Name (A - Z)</option>
          </select>
        </div>
      </div>

      {/* Error state */}
      {error && (
        <div className="rounded-xl border border-rose-800/60 bg-rose-950/20 p-4 text-rose-300 flex items-center gap-3">
          <AlertCircle className="w-5 h-5 flex-shrink-0" />
          <p className="text-sm">{error}</p>
        </div>
      )}

      {/* Loading & Grid Display */}
      {isLoading ? (
        <div className="flex flex-col items-center justify-center p-16 text-slate-400">
          <div className="w-8 h-8 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin mb-3" />
          <p className="text-sm">Searching encountered players...</p>
        </div>
      ) : displayedPlayers.length === 0 ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-12 text-center text-slate-400 space-y-3">
          <p className="text-base font-semibold text-slate-300">No players found matching your criteria</p>
          <p className="text-xs text-slate-500">
            Try adjusting your search query, selecting "All Platforms", or clearing role filters.
          </p>
          {(roleFilter !== 'all' || query.trim() !== '' || platform !== 'all') && (
            <button
              onClick={() => {
                setRoleFilter('all');
                setQuery('');
                setPlatform('all');
              }}
              className="px-3 py-1.5 rounded-lg bg-cyan-500/10 hover:bg-cyan-500/20 text-cyan-300 border border-cyan-500/30 text-xs font-semibold"
            >
              Reset Filters
            </button>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {displayedPlayers.map((p) => (
            <PlayerCard key={p.player_id} player={p} onClick={() => setSelectedPlayerId(p.player_id)} />
          ))}
        </div>
      )}

      {/* Pagination Controls */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between pt-4 border-t border-slate-800">
          <button
            onClick={() => setPage(Math.max(1, page - 1))}
            disabled={page <= 1}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-slate-700 bg-slate-800/60 text-slate-300 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed text-xs font-semibold transition-colors"
          >
            <ChevronLeft className="w-4 h-4" />
            Previous
          </button>
          <span className="text-xs text-slate-400">
            Page <strong className="text-white">{page}</strong> of <strong className="text-white">{totalPages}</strong>
          </span>
          <button
            onClick={() => setPage(Math.min(totalPages, page + 1))}
            disabled={page >= totalPages}
            className="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-slate-700 bg-slate-800/60 text-slate-300 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed text-xs font-semibold transition-colors"
          >
            Next
            <ChevronRight className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Player Detail Drill-Down Modal */}
      <PlayerProfileModal playerId={selectedPlayerId} onClose={() => setSelectedPlayerId(null)} />
    </div>
  );
};
