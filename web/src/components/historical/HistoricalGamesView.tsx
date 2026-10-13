import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { HistoricalMatchRecord, MatchesResponse, BallchasingSyncStatus } from '../../types/api';
import { HistoricalMatchCard } from './HistoricalMatchCard';
import { HistoricalMatchDetailModal } from './HistoricalMatchDetailModal';
import { isDemoMode, SAMPLE_HISTORICAL_MATCHES } from '../../utils/sampleData';
import {
  Search,
  X,
  RefreshCw,
  CloudDownload,
  AlertCircle,
  Database,
  ChevronLeft,
  ChevronRight,
  CheckCircle2,
} from 'lucide-react';

const PLAYLIST_OPTIONS = [
  { id: 0, label: 'All Playlists' },
  { id: 11, label: 'Ranked Doubles (2v2)' },
  { id: 13, label: 'Ranked Standard (3v3)' },
  { id: 10, label: 'Ranked Duel (1v1)' },
  { id: 34, label: 'Tournament' },
  { id: 27, label: 'Ranked Hoops' },
  { id: 28, label: 'Ranked Rumble' },
  { id: 29, label: 'Ranked Dropshot' },
  { id: 30, label: 'Ranked Snow Day' },
  { id: 2, label: 'Casual Doubles (2v2)' },
  { id: 3, label: 'Casual Standard (3v3)' },
];

export const HistoricalGamesView: React.FC = () => {
  const [matches, setMatches] = useState<HistoricalMatchRecord[]>([]);
  const [total, setTotal] = useState<number>(0);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  // Pagination & filter state
  const [page, setPage] = useState<number>(1);
  const [limit, setLimit] = useState<number>(20);
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [playlistFilter, setPlaylistFilter] = useState<number>(0);
  const [outcomeFilter, setOutcomeFilter] = useState<'all' | 'victory' | 'defeat'>('all');

  // Detail Modal
  const [selectedMatch, setSelectedMatch] = useState<HistoricalMatchRecord | null>(null);

  // Sync state
  const [syncStatus, setSyncStatus] = useState<BallchasingSyncStatus | null>(null);
  const [isSyncing, setIsSyncing] = useState<boolean>(false);
  const [syncFeedback, setSyncFeedback] = useState<string | null>(null);

  const offset = (page - 1) * limit;

  // Load matches from backend
  const fetchMatches = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams();
      params.set('limit', limit.toString());
      params.set('offset', offset.toString());
      if (playlistFilter > 0) {
        params.set('playlist', playlistFilter.toString());
      }
      if (searchQuery.trim()) {
        params.set('query', searchQuery.trim());
      }

      const res = await fetch(`/api/matches?${params.toString()}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`);
      const data: MatchesResponse = await res.json();

      if (data.matches.length === 0 && isDemoMode() && !searchQuery && playlistFilter === 0) {
        // Fallback to demo fixture data when in demo/preview mode
        setMatches(SAMPLE_HISTORICAL_MATCHES);
        setTotal(SAMPLE_HISTORICAL_MATCHES.length);
      } else {
        setMatches(data.matches);
        setTotal(data.total);
      }
    } catch (err) {
      if (isDemoMode()) {
        setMatches(SAMPLE_HISTORICAL_MATCHES);
        setTotal(SAMPLE_HISTORICAL_MATCHES.length);
      } else {
        setError(err instanceof Error ? err.message : 'Failed to fetch historical matches');
      }
    } finally {
      setIsLoading(false);
    }
  }, [limit, offset, playlistFilter, searchQuery]);

  useEffect(() => {
    fetchMatches();
  }, [fetchMatches]);

  // Check sync status on mount
  useEffect(() => {
    const checkSyncStatus = async () => {
      try {
        const res = await fetch('/api/sync/ballchasing/status');
        if (res.ok) {
          const data: BallchasingSyncStatus = await res.json();
          setSyncStatus(data);
          if (data.running) {
            setIsSyncing(true);
          }
        }
      } catch {
        // Ignore status check errors on mount
      }
    };
    checkSyncStatus();
  }, []);

  // Poll sync status while active
  useEffect(() => {
    if (!isSyncing) return;

    const interval = setInterval(async () => {
      try {
        const res = await fetch('/api/sync/ballchasing/status');
        if (res.ok) {
          const data: BallchasingSyncStatus = await res.json();
          setSyncStatus(data);
          if (!data.running) {
            setIsSyncing(false);
            setSyncFeedback(`Successfully synchronized ${data.synced_matches} matches and ${data.synced_players} players from Ballchasing.com!`);
            fetchMatches();
          }
        }
      } catch {
        // Continue polling
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [isSyncing, fetchMatches]);

  // Trigger Ballchasing sync
  const triggerSync = async () => {
    setError(null);
    setSyncFeedback(null);
    setIsSyncing(true);
    try {
      const res = await fetch('/api/sync/ballchasing', { method: 'POST' });
      if (!res.ok && res.status !== 409) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || `HTTP ${res.status}`);
      }
      setSyncFeedback('Ballchasing historical sync started in background...');
    } catch (err) {
      setIsSyncing(false);
      setError(err instanceof Error ? err.message : 'Failed to trigger Ballchasing sync');
    }
  };

  // Client-side outcome filter
  const displayedMatches = useMemo(() => {
    if (outcomeFilter === 'all') return matches;
    return matches.filter((m) => {
      const res = m.result || (
        m.blue_score > m.orange_score ? 'victory' :
        m.orange_score > m.blue_score ? 'defeat' : ''
      );
      return res === outcomeFilter;
    });
  }, [matches, outcomeFilter]);

  const totalPages = Math.ceil(total / limit) || 1;

  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value);
    setPage(1);
  };

  const handlePlaylistChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    setPlaylistFilter(Number(e.target.value));
    setPage(1);
  };

  const clearFilters = () => {
    setSearchQuery('');
    setPlaylistFilter(0);
    setOutcomeFilter('all');
    setPage(1);
  };

  const hasActiveFilters = searchQuery !== '' || playlistFilter !== 0 || outcomeFilter !== 'all';

  return (
    <div className="space-y-4">
      {/* Demo Mode Notice */}
      {isDemoMode() && (
        <div className="flex items-center justify-between px-4 py-2.5 rounded-xl bg-cyan-950/40 border border-cyan-500/30 text-xs text-cyan-300">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-cyan-400 animate-pulse" />
            <span className="font-semibold tracking-wide">SIMULATED PREVIEW / DEMO MODE ACTIVE</span>
            <span className="text-cyan-400/70">— Showing simulated persistent store historical matches.</span>
          </div>
          <a href={window.location.pathname} className="text-cyan-400 hover:text-white underline font-mono text-xs">
            Exit Preview
          </a>
        </div>
      )}

      {/* Top Action & Sync Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 rounded-xl bg-slate-900/60 border border-slate-800 backdrop-blur-md">
        <div>
          <h2 className="text-lg font-black tracking-wide text-white flex items-center gap-2">
            <Database className="w-5 h-5 text-cyan-400" />
            Historical Matches Database
          </h2>
          <p className="text-xs text-slate-400 mt-0.5">
            All matches persisted in your local SQLite store, enriched with box scores and Ballchasing telemetry.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <button
            onClick={triggerSync}
            disabled={isSyncing}
            className={`flex items-center gap-2 px-3.5 py-2 rounded-lg text-xs font-bold transition-all border ${
              isSyncing
                ? 'bg-cyan-500/10 text-cyan-400 border-cyan-500/30 cursor-not-allowed'
                : 'bg-cyan-500 hover:bg-cyan-400 text-slate-950 border-cyan-400 shadow-[0_0_15px_rgba(6,182,212,0.3)]'
            }`}
          >
            <CloudDownload className={`w-4 h-4 ${isSyncing ? 'animate-bounce' : ''}`} />
            <span>{isSyncing ? 'Syncing...' : 'Sync from Ballchasing'}</span>
          </button>

          <button
            onClick={fetchMatches}
            disabled={isLoading}
            className="p-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 transition-colors"
            title="Refresh historical match list"
          >
            <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {/* Live Sync Progress Banner */}
      {isSyncing && syncStatus && (
        <div className="p-3.5 rounded-xl bg-cyan-950/40 border border-cyan-500/40 text-xs text-cyan-300 flex items-center justify-between gap-3 animate-pulse">
          <div className="flex items-center gap-2.5">
            <RefreshCw className="w-4 h-4 animate-spin text-cyan-400" />
            <div>
              <span className="font-bold">Ballchasing Sync In Progress: </span>
              <span className="text-cyan-200">
                {syncStatus.synced_matches} matches synced ({syncStatus.synced_players} players found)
                {syncStatus.total_replays > 0 ? ` out of ~${syncStatus.total_replays} replays` : ''}
              </span>
            </div>
          </div>
          <span className="text-[10px] font-mono text-cyan-400/80 uppercase tracking-wider">
            2 req/sec safe rate
          </span>
        </div>
      )}

      {/* Success / Error Feedback */}
      {syncFeedback && (
        <div className="p-3 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-xs text-emerald-400 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <CheckCircle2 className="w-4 h-4 shrink-0" />
            <span>{syncFeedback}</span>
          </div>
          <button onClick={() => setSyncFeedback(null)} className="text-emerald-400/70 hover:text-emerald-300">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {error && (
        <div className="p-3 rounded-xl bg-rose-500/10 border border-rose-500/30 text-xs text-rose-400 flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Search & Filters Controls */}
      <div className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3">
          {/* Search Box */}
          <div className="relative">
            <Search className="w-4 h-4 text-slate-500 absolute left-3 top-1/2 -translate-y-1/2" />
            <input
              type="text"
              value={searchQuery}
              onChange={handleSearchChange}
              placeholder="Search by arena, player name, or ID..."
              className="w-full bg-slate-950 border border-slate-800 rounded-lg pl-9 pr-8 py-2 text-xs text-slate-100 placeholder-slate-500 focus:outline-none focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery('')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>

          {/* Playlist Dropdown */}
          <div className="relative">
            <select
              value={playlistFilter}
              onChange={handlePlaylistChange}
              className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-xs text-slate-200 focus:outline-none focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 cursor-pointer"
            >
              {PLAYLIST_OPTIONS.map((opt) => (
                <option key={opt.id} value={opt.id} className="bg-slate-900 text-slate-200">
                  {opt.label}
                </option>
              ))}
            </select>
          </div>

          {/* Result Filter Pills */}
          <div className="flex items-center gap-1.5 p-1 rounded-lg bg-slate-950 border border-slate-800">
            <button
              onClick={() => setOutcomeFilter('all')}
              className={`flex-1 py-1 text-center text-xs font-semibold rounded transition-colors ${
                outcomeFilter === 'all'
                  ? 'bg-slate-800 text-white'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              All
            </button>
            <button
              onClick={() => setOutcomeFilter('victory')}
              className={`flex-1 py-1 text-center text-xs font-semibold rounded transition-colors ${
                outcomeFilter === 'victory'
                  ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              Wins
            </button>
            <button
              onClick={() => setOutcomeFilter('defeat')}
              className={`flex-1 py-1 text-center text-xs font-semibold rounded transition-colors ${
                outcomeFilter === 'defeat'
                  ? 'bg-rose-500/20 text-rose-300 border border-rose-500/30'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              Losses
            </button>
          </div>
        </div>

        {/* Filter Summary & Pagination Size */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 pt-2 border-t border-slate-800/80 text-xs text-slate-400">
          <div className="flex items-center gap-3">
            <span>
              Showing <strong className="text-white">{displayedMatches.length}</strong> of{' '}
              <strong className="text-white">{total}</strong> total matches
            </span>
            {hasActiveFilters && (
              <button
                onClick={clearFilters}
                className="text-cyan-400 hover:text-cyan-300 font-semibold underline text-xs"
              >
                Clear Filters
              </button>
            )}
          </div>

          <div className="flex items-center gap-2">
            <span className="text-[11px] text-slate-500">Per page:</span>
            <select
              value={limit}
              onChange={(e) => {
                setLimit(Number(e.target.value));
                setPage(1);
              }}
              className="bg-slate-950 border border-slate-800 rounded px-2 py-0.5 text-xs text-slate-300 focus:outline-none"
            >
              <option value={10}>10</option>
              <option value={20}>20</option>
              <option value={50}>50</option>
            </select>
          </div>
        </div>
      </div>

      {/* Match Cards List / Empty States */}
      {isLoading ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-12 text-center text-slate-400 space-y-3">
          <RefreshCw className="w-6 h-6 animate-spin mx-auto text-cyan-400" />
          <p className="text-sm font-semibold text-slate-300">Loading historical games...</p>
        </div>
      ) : total === 0 ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-8 text-center text-slate-400 space-y-4">
          <div className="w-12 h-12 rounded-full bg-cyan-500/10 border border-cyan-500/30 text-cyan-400 flex items-center justify-center mx-auto">
            <Database className="w-6 h-6" />
          </div>
          <div>
            <h3 className="text-base font-bold text-slate-200">No historical matches in local store</h3>
            <p className="text-xs text-slate-500 max-w-md mx-auto mt-1">
              Click &quot;Sync from Ballchasing&quot; to import your entire upload history, or play matches with the synchronizer daemon running to enrich your database automatically.
            </p>
          </div>
          <div className="flex flex-wrap items-center justify-center gap-3">
            <button
              onClick={triggerSync}
              disabled={isSyncing}
              className="px-4 py-2 rounded-lg bg-cyan-500 hover:bg-cyan-400 text-slate-950 text-xs font-bold transition-all shadow-[0_0_15px_rgba(6,182,212,0.3)]"
            >
              Sync from Ballchasing Now
            </button>
          </div>
        </div>
      ) : displayedMatches.length === 0 ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-8 text-center text-slate-400 space-y-3">
          <p className="text-sm font-semibold text-slate-300">No matches found matching your filters</p>
          <button
            onClick={clearFilters}
            className="px-3 py-1.5 rounded-lg bg-cyan-500/10 hover:bg-cyan-500/20 text-cyan-300 border border-cyan-500/30 text-xs font-semibold"
          >
            Clear Filters
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3">
          {displayedMatches.map((m) => (
            <HistoricalMatchCard
              key={m.match_guid}
              match={m}
              onClick={() => setSelectedMatch(m)}
            />
          ))}
        </div>
      )}

      {/* Pagination Controls */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between p-3 rounded-xl bg-slate-900/60 border border-slate-800 text-xs text-slate-400">
          <div>
            Page <strong className="text-white">{page}</strong> of <strong className="text-white">{totalPages}</strong>
          </div>

          <div className="flex items-center gap-1.5">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
              className="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 disabled:opacity-40 disabled:cursor-not-allowed border border-slate-700 transition-colors"
              title="Previous Page"
            >
              <ChevronLeft className="w-4 h-4" />
            </button>
            <span className="px-2 font-mono font-bold text-slate-300">{page}</span>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              className="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 disabled:opacity-40 disabled:cursor-not-allowed border border-slate-700 transition-colors"
              title="Next Page"
            >
              <ChevronRight className="w-4 h-4" />
            </button>
          </div>
        </div>
      )}

      {/* Detail Modal */}
      <HistoricalMatchDetailModal
        match={selectedMatch}
        onClose={() => setSelectedMatch(null)}
      />
    </div>
  );
};
