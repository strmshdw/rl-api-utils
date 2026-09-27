import React from 'react';
import { useSession } from '../../hooks/useSession';
import { MatchCard } from './MatchCard';
import { MatchDetailModal } from './MatchDetailModal';
import { RotateCcw, History, AlertCircle } from 'lucide-react';

export const SessionHistoryView: React.FC = () => {
  const { session, isLoading, error, resetSession, selectedMatch, setSelectedMatch } = useSession();

  if (isLoading && !session) {
    return (
      <div className="flex flex-col items-center justify-center p-12 text-slate-400">
        <div className="w-8 h-8 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin mb-3" />
        <p className="text-sm">Loading session match history...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-xl border border-rose-800/60 bg-rose-950/20 p-4 text-rose-300 flex items-center gap-3">
        <AlertCircle className="w-5 h-5 flex-shrink-0" />
        <p className="text-sm">{error}</p>
      </div>
    );
  }

  const matches = session?.matches ?? [];
  // Most recent completed matches first
  const sortedMatches = [...matches].reverse();

  return (
    <div className="space-y-4 py-2">
      {/* Session Controls Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <History className="w-5 h-5 text-cyan-400" />
          <h2 className="text-lg font-bold text-white tracking-tight">
            Session Matches ({matches.length})
          </h2>
        </div>
        <button
          onClick={() => {
            if (window.confirm('Are you sure you want to reset all active session stats and history?')) {
              resetSession();
            }
          }}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-slate-700 bg-slate-800/60 text-slate-300 hover:text-rose-400 hover:border-rose-700/60 transition-colors text-xs font-semibold focus:outline-none"
        >
          <RotateCcw className="w-3.5 h-3.5" />
          Reset Session
        </button>
      </div>

      {sortedMatches.length === 0 ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-8 text-center text-slate-400">
          <p className="text-base font-semibold text-slate-300 mb-1">No matches completed yet this session</p>
          <p className="text-xs text-slate-500">
            Play a match in Rocket League to automatically capture box scores and MMR deltas.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3">
          {sortedMatches.map((m) => (
            <MatchCard key={m.match_guid} match={m} onClick={() => setSelectedMatch(m)} />
          ))}
        </div>
      )}

      {/* Match Detail Modal */}
      <MatchDetailModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />
    </div>
  );
};
