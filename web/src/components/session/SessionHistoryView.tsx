import React, { useState, useMemo } from 'react';
import { useSession } from '../../hooks/useSession';
import { MatchCard } from './MatchCard';
import { MatchDetailModal } from './MatchDetailModal';
import { formatMMRDelta } from '../../utils/formatters';
import {
  RotateCcw,
  History,
  AlertCircle,
  Search,
  X,
  Trophy,
  TrendingUp,
  TrendingDown,
  Shield,
  Flame,
  ArrowUpDown,
  Filter,
} from 'lucide-react';

import { isDemoMode, SAMPLE_SESSION_RESPONSE } from '../../utils/sampleData';

type OutcomeFilter = 'all' | 'victory' | 'defeat';
type SortOption = 'newest' | 'oldest' | 'mmr-desc' | 'mmr-asc' | 'score-desc';

export const SessionHistoryView: React.FC = () => {
  const { session, isLoading, error, resetSession, selectedMatch, setSelectedMatch } = useSession();

  // Search, filter, and sorting states
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedPlaylist, setSelectedPlaylist] = useState<string>('all');
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>('all');
  const [sortBy, setSortBy] = useState<SortOption>('newest');

  // Fallback to sample session in preview / demo mode if session has no matches
  const effectiveSession = useMemo(() => {
    if (session && session.matches && session.matches.length > 0) return session;
    if (isDemoMode()) return SAMPLE_SESSION_RESPONSE;
    return session;
  }, [session]);

  const matches = effectiveSession?.matches ?? [];

  // Extract unique playlists from session matches
  const availablePlaylists = useMemo(() => {
    const map = new Map<number, string>();
    matches.forEach((m) => {
      if (m.playlist_id && m.playlist_name) {
        map.set(m.playlist_id, m.playlist_name);
      }
    });
    return Array.from(map.entries()).map(([id, name]) => ({ id, name }));
  }, [matches]);

  // Aggregate session performance KPIs
  const sessionStats = useMemo(() => {
    if (matches.length === 0) return null;

    let totalGoalsFor = 0;
    let totalGoalsAgainst = 0;
    let netMmr = 0;
    let totalLocalScore = 0;
    let totalLocalSaves = 0;
    let highestGameScore = 0;

    let currentStreakCount = 0;
    let currentStreakType: 'victory' | 'defeat' | null = null;

    let longestWinStreak = 0;
    let tempWinStreak = 0;

    matches.forEach((m) => {
      const localTeam = m.local_team ?? 0;
      const myGoals = localTeam === 0 ? m.blue_score : m.orange_score;
      const oppGoals = localTeam === 0 ? m.orange_score : m.blue_score;
      totalGoalsFor += myGoals;
      totalGoalsAgainst += oppGoals;
      netMmr += m.mmr_change || 0;

      if (m.result === 'victory') {
        tempWinStreak++;
        if (tempWinStreak > longestWinStreak) longestWinStreak = tempWinStreak;
      } else if (m.result === 'defeat') {
        tempWinStreak = 0;
      }

      const localPlayer = m.players.find((p) => p.is_local);
      if (localPlayer?.stats) {
        totalLocalScore += localPlayer.stats.score;
        totalLocalSaves += localPlayer.stats.saves;
        if (localPlayer.stats.score > highestGameScore) {
          highestGameScore = localPlayer.stats.score;
        }
      }
    });

    // Current streak backwards from newest (consecutive wins or losses)
    for (let i = matches.length - 1; i >= 0; i--) {
      const res = matches[i].result;
      if (res !== 'victory' && res !== 'defeat') {
        // Draw or non-decisive match terminates the active streak
        break;
      }
      if (currentStreakType === null) {
        currentStreakType = res;
        currentStreakCount = 1;
      } else if (currentStreakType === res) {
        currentStreakCount++;
      } else {
        break;
      }
    }

    const wins = matches.filter((m) => m.result === 'victory').length;
    const losses = matches.filter((m) => m.result === 'defeat').length;
    const winRate = matches.length > 0 ? (wins / matches.length) * 100 : 0;
    const goalDiff = totalGoalsFor - totalGoalsAgainst;
    const avgScore = Math.round(totalLocalScore / matches.length);
    const avgGoals = (totalGoalsFor / matches.length).toFixed(1);

    return {
      wins,
      losses,
      winRate,
      netMmr,
      totalGoalsFor,
      totalGoalsAgainst,
      goalDiff,
      avgScore,
      avgGoals,
      totalLocalSaves,
      highestGameScore,
      longestWinStreak,
      currentStreakCount,
      currentStreakType,
    };
  }, [matches]);

  // Filtered and sorted matches
  const filteredMatches = useMemo(() => {
    let result = [...matches];

    // Search query filter: check player names or playlist name
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase().trim();
      result = result.filter((m) => {
        const matchPlaylist = m.playlist_name.toLowerCase().includes(q);
        const matchGuid = m.match_guid.toLowerCase().includes(q);
        const matchPlayer = m.players.some((p) => p.name.toLowerCase().includes(q));
        return matchPlaylist || matchGuid || matchPlayer;
      });
    }

    // Playlist filter
    if (selectedPlaylist !== 'all') {
      const pid = parseInt(selectedPlaylist, 10);
      result = result.filter((m) => m.playlist_id === pid);
    }

    // Outcome filter
    if (outcomeFilter !== 'all') {
      result = result.filter((m) => m.result === outcomeFilter);
    }

    // Sorting
    result.sort((a, b) => {
      switch (sortBy) {
        case 'newest':
          return new Date(b.ended_at).getTime() - new Date(a.ended_at).getTime();
        case 'oldest':
          return new Date(a.ended_at).getTime() - new Date(b.ended_at).getTime();
        case 'mmr-desc':
          return b.mmr_change - a.mmr_change;
        case 'mmr-asc':
          return a.mmr_change - b.mmr_change;
        case 'score-desc': {
          const aScore = a.players && a.players.length > 0 ? Math.max(...a.players.map((p) => p.stats?.score || 0)) : 0;
          const bScore = b.players && b.players.length > 0 ? Math.max(...b.players.map((p) => p.stats?.score || 0)) : 0;
          return bScore - aScore;
        }
        default:
          return 0;
      }
    });

    return result;
  }, [matches, searchQuery, selectedPlaylist, outcomeFilter, sortBy]);

  const hasActiveFilters = searchQuery.trim() !== '' || selectedPlaylist !== 'all' || outcomeFilter !== 'all';

  const clearFilters = () => {
    setSearchQuery('');
    setSelectedPlaylist('all');
    setOutcomeFilter('all');
    setSortBy('newest');
  };

  if (isLoading && !effectiveSession) {
    return (
      <div className="flex flex-col items-center justify-center p-12 text-slate-400">
        <div className="w-8 h-8 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin mb-3" />
        <p className="text-sm">Loading session match history...</p>
      </div>
    );
  }

  if (error && !isDemoMode()) {
    return (
      <div className="rounded-xl border border-rose-800/60 bg-rose-950/20 p-4 text-rose-300 flex items-center gap-3">
        <AlertCircle className="w-5 h-5 flex-shrink-0" />
        <p className="text-sm">{error}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6 py-2">
      {/* Demo Mode Notice */}
      {isDemoMode() && (!session || session.matches?.length === 0) && (
        <div className="flex items-center justify-between px-4 py-2.5 rounded-xl bg-cyan-950/40 border border-cyan-500/30 text-xs text-cyan-300">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-cyan-400 animate-pulse" />
            <span className="font-semibold tracking-wide">SIMULATED PREVIEW / DEMO MODE ACTIVE</span>
            <span className="text-cyan-400/70">— Showing simulated session matches and KPI performance analytics.</span>
          </div>
          <a href={window.location.pathname} className="text-cyan-400 hover:text-white underline font-mono text-xs">
            Exit Preview
          </a>
        </div>
      )}

      {/* Session Controls Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <History className="w-5 h-5 text-cyan-400" />
          <h2 className="text-lg font-bold text-white tracking-tight">
            Session Match Analysis ({matches.length})
          </h2>
        </div>
        <button
          onClick={() => {
            if (window.confirm('Are you sure you want to reset all active session stats and history?')) {
              resetSession();
            }
          }}
          className="self-start sm:self-auto flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-slate-700 bg-slate-800/60 text-slate-300 hover:text-rose-400 hover:border-rose-700/60 transition-colors text-xs font-semibold focus:outline-none"
        >
          <RotateCcw className="w-3.5 h-3.5" />
          Reset Session
        </button>
      </div>

      {/* Comprehensive Session Analytics Dashboard */}
      {sessionStats && matches.length > 0 && (
        <div className="rounded-2xl border border-slate-800 bg-slate-900/60 p-5 shadow-xl backdrop-blur-md space-y-4">
          <div className="flex items-center justify-between border-b border-slate-800/80 pb-3">
            <span className="text-xs font-bold text-slate-300 uppercase tracking-wider flex items-center gap-1.5">
              <Trophy className="w-4 h-4 text-amber-400" />
              Session Performance Metrics
            </span>
            <span className="text-xs font-mono text-slate-400">
              {sessionStats.wins}W - {sessionStats.losses}L ({sessionStats.winRate.toFixed(1)}%)
            </span>
          </div>

          {/* Primary Metric Grid */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            {/* Net Session MMR */}
            <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 flex items-center gap-3">
              <div
                className={`p-2.5 rounded-lg ${
                  sessionStats.netMmr >= 0
                    ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                    : 'bg-rose-500/15 text-rose-400 border border-rose-500/30'
                }`}
              >
                {sessionStats.netMmr >= 0 ? <TrendingUp className="w-5 h-5" /> : <TrendingDown className="w-5 h-5" />}
              </div>
              <div>
                <div className="text-[10px] text-slate-400 font-semibold uppercase">Net Session MMR</div>
                <div
                  className={`text-lg font-black font-mono ${
                    sessionStats.netMmr >= 0 ? 'text-emerald-400' : 'text-rose-400'
                  }`}
                >
                  {formatMMRDelta(sessionStats.netMmr)} MMR
                </div>
              </div>
            </div>

            {/* Goal Differential */}
            <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 flex items-center gap-3">
              <div className="p-2.5 rounded-lg bg-cyan-500/15 text-cyan-400 border border-cyan-500/30">
                <Shield className="w-5 h-5" />
              </div>
              <div>
                <div className="text-[10px] text-slate-400 font-semibold uppercase">Goal Differential</div>
                <div
                  className={`text-lg font-black font-mono ${
                    sessionStats.goalDiff > 0
                      ? 'text-emerald-400'
                      : sessionStats.goalDiff < 0
                      ? 'text-rose-400'
                      : 'text-slate-300'
                  }`}
                >
                  {sessionStats.goalDiff > 0 ? `+${sessionStats.goalDiff}` : sessionStats.goalDiff}
                  <span className="text-[11px] font-sans text-slate-500 font-normal ml-1">
                    ({sessionStats.totalGoalsFor}F / {sessionStats.totalGoalsAgainst}A)
                  </span>
                </div>
              </div>
            </div>

            {/* Current Streak */}
            <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 flex items-center gap-3">
              <div className="p-2.5 rounded-lg bg-amber-500/15 text-amber-400 border border-amber-500/30">
                <Flame className="w-5 h-5" />
              </div>
              <div>
                <div className="text-[10px] text-slate-400 font-semibold uppercase">Current Streak</div>
                <div className="text-lg font-black text-white">
                  {sessionStats.currentStreakType
                    ? `${sessionStats.currentStreakCount} ${sessionStats.currentStreakType === 'victory' ? 'Wins' : 'Losses'}`
                    : 'None'}
                </div>
              </div>
            </div>

            {/* Best Win Streak & High Score */}
            <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 flex items-center gap-3">
              <div className="p-2.5 rounded-lg bg-purple-500/15 text-purple-400 border border-purple-500/30">
                <Trophy className="w-5 h-5" />
              </div>
              <div>
                <div className="text-[10px] text-slate-400 font-semibold uppercase">Best Streak / High Score</div>
                <div className="text-sm font-bold text-purple-300">
                  {sessionStats.longestWinStreak} Wins <span className="text-slate-500">•</span> {sessionStats.highestGameScore} pts
                </div>
              </div>
            </div>
          </div>

          {/* Visual Win/Loss Segmented Progress Bar */}
          <div className="space-y-1">
            <div className="flex items-center justify-between text-xs text-slate-400 font-medium">
              <span>Win Rate Progress</span>
              <span className="font-bold text-cyan-400">{sessionStats.winRate.toFixed(1)}%</span>
            </div>
            <div className="w-full bg-slate-950 rounded-full h-2 overflow-hidden flex border border-slate-800">
              <div
                className="bg-emerald-500 h-full transition-all duration-300"
                style={{ width: `${sessionStats.winRate}%` }}
                title={`${sessionStats.wins} Wins`}
              />
              <div
                className="bg-rose-500 h-full transition-all duration-300"
                style={{ width: `${100 - sessionStats.winRate}%` }}
                title={`${sessionStats.losses} Losses`}
              />
            </div>
          </div>
        </div>
      )}

      {/* Interactive Filter & Search Toolbar */}
      {matches.length > 0 && (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-4 space-y-3 shadow-md">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            {/* Search Input */}
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search by player or playlist..."
                className="w-full rounded-lg border border-slate-700 bg-slate-950/80 pl-9 pr-8 py-1.5 text-xs text-slate-100 placeholder-slate-500 focus:border-cyan-500 focus:outline-none transition-colors"
              />
              {searchQuery && (
                <button
                  onClick={() => setSearchQuery('')}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-white"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>

            {/* Playlist Filter */}
            <div className="flex items-center gap-1.5">
              <Filter className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
              <select
                value={selectedPlaylist}
                onChange={(e) => setSelectedPlaylist(e.target.value)}
                className="w-full rounded-lg border border-slate-700 bg-slate-950/80 px-2.5 py-1.5 text-xs text-slate-200 focus:border-cyan-500 focus:outline-none"
              >
                <option value="all">All Playlists ({matches.length})</option>
                {availablePlaylists.map((pl) => (
                  <option key={pl.id} value={pl.id.toString()}>
                    {pl.name}
                  </option>
                ))}
              </select>
            </div>

            {/* Sort Selector */}
            <div className="flex items-center gap-1.5">
              <ArrowUpDown className="w-3.5 h-3.5 text-slate-400 flex-shrink-0" />
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value as SortOption)}
                className="w-full rounded-lg border border-slate-700 bg-slate-950/80 px-2.5 py-1.5 text-xs text-slate-200 focus:border-cyan-500 focus:outline-none"
              >
                <option value="newest">Most Recent First</option>
                <option value="oldest">Oldest First</option>
                <option value="mmr-desc">Highest MMR Gain</option>
                <option value="mmr-asc">Biggest MMR Loss</option>
                <option value="score-desc">Highest Game Score</option>
              </select>
            </div>
          </div>

          {/* Outcome Filter Pills & Status */}
          <div className="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-slate-800/80">
            <div className="flex items-center gap-1.5 text-xs">
              <span className="text-[11px] text-slate-400 font-semibold uppercase mr-1">Result:</span>
              <button
                onClick={() => setOutcomeFilter('all')}
                className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
                  outcomeFilter === 'all'
                    ? 'bg-cyan-500/20 text-cyan-300 border-cyan-500/50'
                    : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
                }`}
              >
                All
              </button>
              <button
                onClick={() => setOutcomeFilter('victory')}
                className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
                  outcomeFilter === 'victory'
                    ? 'bg-emerald-500/20 text-emerald-300 border-emerald-500/50'
                    : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
                }`}
              >
                Victories Only
              </button>
              <button
                onClick={() => setOutcomeFilter('defeat')}
                className={`px-2.5 py-0.5 rounded-full text-xs font-semibold transition-colors border ${
                  outcomeFilter === 'defeat'
                    ? 'bg-rose-500/20 text-rose-300 border-rose-500/50'
                    : 'bg-slate-800/60 text-slate-400 border-slate-700 hover:text-white'
                }`}
              >
                Defeats Only
              </button>
            </div>

            <div className="flex items-center gap-3 text-xs text-slate-400">
              <span>
                Showing <strong className="text-white">{filteredMatches.length}</strong> of{' '}
                <strong className="text-white">{matches.length}</strong> matches
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
          </div>
        </div>
      )}

      {/* Match Cards List */}
      {matches.length === 0 ? (
        <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-8 text-center text-slate-400">
          <p className="text-base font-semibold text-slate-300 mb-1">No matches completed yet this session</p>
          <p className="text-xs text-slate-500">
            Play a match in Rocket League to automatically capture box scores and MMR deltas.
          </p>
        </div>
      ) : filteredMatches.length === 0 ? (
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
          {filteredMatches.map((m) => (
            <MatchCard key={m.match_guid} match={m} onClick={() => setSelectedMatch(m)} />
          ))}
        </div>
      )}

      {/* Match Detail Modal */}
      <MatchDetailModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />
    </div>
  );
};
