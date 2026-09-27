import React, { useState } from 'react';
import { usePlayerSearch } from '../../hooks/usePlayerSearch';
import { PlayerSearchBar } from './PlayerSearchBar';
import { PlayerCard } from './PlayerCard';
import { PlayerProfileModal } from './PlayerProfileModal';
import { Users, ChevronLeft, ChevronRight, AlertCircle } from 'lucide-react';

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

  return (
    <div className="space-y-6 py-2">
      {/* Header & Search */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Users className="w-5 h-5 text-cyan-400" />
            <h2 className="text-lg font-bold text-white tracking-tight">
              Player Directory ({total})
            </h2>
          </div>
        </div>

        <PlayerSearchBar
          query={query}
          onQueryChange={setQuery}
          platform={platform}
          onPlatformChange={setPlatform}
        />
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
      ) : players.length === 0 ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-12 text-center text-slate-400">
          <p className="text-base font-semibold text-slate-300 mb-1">No players found</p>
          <p className="text-xs text-slate-500">
            Try adjusting your search query or selecting "All Platforms".
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {players.map((p) => (
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
