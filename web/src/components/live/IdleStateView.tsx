import React, { useMemo } from 'react';
import { Radio, Trophy, TrendingUp, TrendingDown, Swords, Flame, Shield, ArrowRight, Clock } from 'lucide-react';
import { SessionResponse, SessionMatchDetail } from '../../types/api';
import { formatDuration, formatMMRDelta, formatRelativeTime } from '../../utils/formatters';

interface IdleStateViewProps {
  session: SessionResponse | null;
  onSelectMatch?: (match: SessionMatchDetail) => void;
}

export const IdleStateView: React.FC<IdleStateViewProps> = ({ session, onSelectMatch }) => {
  const matches = session?.matches ?? [];
  const lastMatch: SessionMatchDetail | null = matches.length > 0 ? matches[matches.length - 1] : null;
  const lastLocalTeam = lastMatch ? (lastMatch.players?.find((p) => p.is_local)?.team_num ?? lastMatch.local_team ?? 0) : 0;

  // Session deep telemetry
  const sessionAnalytics = useMemo(() => {
    if (!session || matches.length === 0) {
      return null;
    }

    let goalsFor = 0;
    let goalsAgainst = 0;
    let totalScore = 0;
    let totalSaves = 0;
    let netMmr = 0;

    matches.forEach((m) => {
      const localP = m.players?.find((p) => p.is_local);
      const localTeam = localP?.team_num ?? m.local_team ?? 0;
      const myTeamScore = localTeam === 0 ? m.blue_score : m.orange_score;
      const oppTeamScore = localTeam === 0 ? m.orange_score : m.blue_score;
      goalsFor += myTeamScore;
      goalsAgainst += oppTeamScore;
      netMmr += m.mmr_change || 0;

      if (localP?.stats) {
        totalScore += localP.stats.score || 0;
        totalSaves += localP.stats.saves || 0;
      }
    });

    // Calculate current streak (consecutive wins or losses ending at the latest match)
    let streakCount = 0;
    let streakType: 'victory' | 'defeat' | null = null;
    for (let i = matches.length - 1; i >= 0; i--) {
      const res = matches[i].result;
      if (res !== 'victory' && res !== 'defeat') {
        // Draw or non-conclusive match terminates the active streak
        break;
      }
      if (streakType === null) {
        streakType = res;
        streakCount = 1;
      } else if (res === streakType) {
        streakCount++;
      } else {
        break;
      }
    }

    const avgScore = Math.round(totalScore / matches.length);
    const goalDiff = goalsFor - goalsAgainst;

    return {
      goalsFor,
      goalsAgainst,
      goalDiff,
      avgScore,
      totalSaves,
      netMmr,
      streakCount,
      streakType,
    };
  }, [session, matches]);

  const playlists = session?.playlists ? Object.values(session.playlists) : [];

  return (
    <div className="space-y-6">
      {/* Stadium Waiting Radar Card */}
      <div className="relative overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/60 p-6 md:p-8 text-center shadow-xl backdrop-blur-md">
        <div className="flex justify-center mb-4">
          <div className="relative flex items-center justify-center w-16 h-16 rounded-full bg-cyan-500/10 border border-cyan-500/30 text-cyan-400 shadow-[0_0_25px_rgba(6,182,212,0.25)]">
            <Radio className="w-8 h-8 animate-pulse" />
            <span className="absolute inset-0 rounded-full border border-cyan-400/40 animate-ping" />
          </div>
        </div>

        <h2 className="text-xl font-black tracking-wide text-slate-100 uppercase">
          Waiting for Match...
        </h2>
        <p className="max-w-md mx-auto text-xs sm:text-sm text-slate-400 mt-2">
          Launch Rocket League and enter any online match. Live telemetry, team rosters, MMR ratings, and historical head-to-head records will appear automatically.
        </p>

        {/* Telemetry Status Indicators */}
        <div className="mt-5 flex flex-wrap items-center justify-center gap-3 text-xs">
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
            <span className="w-2 h-2 rounded-full bg-emerald-400" />
            Stats API: Listening on :49124
          </span>
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
            <span className="w-2 h-2 rounded-full bg-emerald-400" />
            PsyNet Rank Queries: Ready
          </span>
        </div>
      </div>

      {/* Last Match Quick Recap Card */}
      {lastMatch && (
        <div className="rounded-2xl border border-slate-800 bg-gradient-to-r from-slate-900/90 via-slate-900/70 to-slate-950/90 p-5 shadow-lg backdrop-blur-sm">
          <div className="flex items-center justify-between pb-3 border-b border-slate-800/80 mb-4">
            <div className="flex items-center gap-2">
              <Swords className="w-4 h-4 text-cyan-400" />
              <h3 className="font-bold text-xs sm:text-sm text-slate-200 uppercase tracking-wider">
                Last Match Quick Recap
              </h3>
            </div>
            <span className="text-[11px] text-slate-400 flex items-center gap-1">
              <Clock className="w-3 h-3 text-slate-500" />
              {formatRelativeTime(lastMatch.ended_at)}
            </span>
          </div>

          <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
            <div className="flex items-center gap-3.5">
              <span
                className={`inline-flex items-center px-3 py-1 rounded-full text-xs font-black tracking-wider uppercase border ${
                  lastMatch.result === 'victory'
                    ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40 shadow-[0_0_12px_rgba(16,185,129,0.25)]'
                    : lastMatch.result === 'defeat'
                    ? 'bg-rose-500/20 text-rose-400 border-rose-500/40 shadow-[0_0_12px_rgba(244,63,94,0.25)]'
                    : 'bg-slate-700/30 text-slate-300 border-slate-600'
                }`}
              >
                {lastMatch.result ? lastMatch.result.toUpperCase() : 'CONCLUDED'}
              </span>

              <div>
                <h4 className="font-bold text-sm text-white">{lastMatch.playlist_name}</h4>
                <div className="flex items-center gap-2 text-xs text-slate-400 mt-0.5">
                  <span>Duration: {formatDuration(lastMatch.duration_seconds)}</span>
                  <span>•</span>
                  <span>
                    Your Team: <strong className={lastLocalTeam === 0 ? 'text-[#00a2ff]' : 'text-[#ff7b00]'}>
                      {lastLocalTeam === 0 ? 'Blue' : 'Orange'}
                    </strong>
                  </span>
                </div>
              </div>
            </div>

            <div className="flex items-center gap-4">
              {/* Score pill */}
              <div className="flex items-center gap-2 px-3.5 py-1.5 rounded-xl bg-slate-950/80 border border-slate-800">
                <span className="text-xl font-black text-[#00a2ff]">{lastMatch.blue_score}</span>
                <span className="text-slate-500 font-bold">:</span>
                <span className="text-xl font-black text-[#ff7b00]">{lastMatch.orange_score}</span>
              </div>

              {/* MMR delta */}
              <div className="text-right">
                <div className="text-[10px] text-slate-400 uppercase font-semibold">MMR Delta</div>
                <div
                  className={`text-sm font-black font-mono ${
                    lastMatch.mmr_change > 0
                      ? 'text-emerald-400'
                      : lastMatch.mmr_change < 0
                      ? 'text-rose-400'
                      : 'text-slate-400'
                  }`}
                >
                  {formatMMRDelta(lastMatch.mmr_change)} MMR
                </div>
              </div>

              {onSelectMatch && (
                <button
                  onClick={() => onSelectMatch(lastMatch)}
                  className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold bg-cyan-500/10 hover:bg-cyan-500/20 text-cyan-300 border border-cyan-500/30 hover:border-cyan-500/50 transition-colors"
                >
                  <span>Box Score</span>
                  <ArrowRight className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Session Progress & Deep Performance Overview */}
      {session && (
        <div className="rounded-2xl border border-slate-800 bg-slate-900/50 p-6 shadow-lg backdrop-blur-sm">
          <div className="flex items-center justify-between pb-4 border-b border-slate-800/80 mb-5">
            <div className="flex items-center gap-2">
              <Trophy className="w-5 h-5 text-amber-400" />
              <h3 className="font-bold text-sm text-slate-200 uppercase tracking-wider">
                Current Session Insights
              </h3>
            </div>
            <span className="text-xs text-slate-400">
              Started: {new Date(session.started_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          </div>

          {/* Primary KPI Grid */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-5">
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Matches Played</span>
              <div className="text-2xl font-black text-slate-100 mt-1">{session.total_matches}</div>
            </div>
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Record (W - L)</span>
              <div className="text-2xl font-black text-emerald-400 mt-1">
                {session.total_wins}W - {session.total_losses}L
              </div>
            </div>
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Win Rate</span>
              <div className="text-2xl font-black text-cyan-400 mt-1">
                {session.win_rate.toFixed(1)}%
              </div>
            </div>
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Playlists Tracked</span>
              <div className="text-2xl font-black text-purple-400 mt-1">
                {Object.keys(session.playlists || {}).length}
              </div>
            </div>
          </div>

          {/* Secondary Deep Analytics: Streak, Goal Differential, Net MMR */}
          {sessionAnalytics && session.total_matches > 0 && (
            <div className="pt-4 border-t border-slate-800/80 grid grid-cols-2 sm:grid-cols-4 gap-3">
              {/* Streak Badge */}
              <div className="p-3 rounded-lg bg-slate-950/40 border border-slate-800/80 flex items-center gap-2.5">
                <div className="p-2 rounded-md bg-amber-500/10 text-amber-400">
                  <Flame className="w-4 h-4" />
                </div>
                <div>
                  <div className="text-[10px] uppercase font-semibold text-slate-400">Current Streak</div>
                  <div className="text-xs font-bold text-white">
                    {sessionAnalytics.streakType
                      ? `${sessionAnalytics.streakCount} ${sessionAnalytics.streakType === 'victory' ? 'Wins' : 'Losses'}`
                      : 'None'}
                  </div>
                </div>
              </div>

              {/* Net MMR Delta */}
              <div className="p-3 rounded-lg bg-slate-950/40 border border-slate-800/80 flex items-center gap-2.5">
                <div className={`p-2 rounded-md ${sessionAnalytics.netMmr >= 0 ? 'bg-emerald-500/10 text-emerald-400' : 'bg-rose-500/10 text-rose-400'}`}>
                  {sessionAnalytics.netMmr >= 0 ? <TrendingUp className="w-4 h-4" /> : <TrendingDown className="w-4 h-4" />}
                </div>
                <div>
                  <div className="text-[10px] uppercase font-semibold text-slate-400">Net Session MMR</div>
                  <div className={`text-xs font-bold font-mono ${sessionAnalytics.netMmr >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
                    {formatMMRDelta(sessionAnalytics.netMmr)}
                  </div>
                </div>
              </div>

              {/* Goal Differential */}
              <div className="p-3 rounded-lg bg-slate-950/40 border border-slate-800/80 flex items-center gap-2.5">
                <div className="p-2 rounded-md bg-cyan-500/10 text-cyan-400">
                  <Shield className="w-4 h-4" />
                </div>
                <div>
                  <div className="text-[10px] uppercase font-semibold text-slate-400">Goal Differential</div>
                  <div className={`text-xs font-bold font-mono ${sessionAnalytics.goalDiff > 0 ? 'text-emerald-400' : sessionAnalytics.goalDiff < 0 ? 'text-rose-400' : 'text-slate-300'}`}>
                    {sessionAnalytics.goalDiff > 0 ? `+${sessionAnalytics.goalDiff}` : sessionAnalytics.goalDiff} ({sessionAnalytics.goalsFor}F / {sessionAnalytics.goalsAgainst}A)
                  </div>
                </div>
              </div>

              {/* Average Score */}
              <div className="p-3 rounded-lg bg-slate-950/40 border border-slate-800/80 flex items-center gap-2.5">
                <div className="p-2 rounded-md bg-purple-500/10 text-purple-400">
                  <Trophy className="w-4 h-4" />
                </div>
                <div>
                  <div className="text-[10px] uppercase font-semibold text-slate-400">Avg Player Score</div>
                  <div className="text-xs font-bold font-mono text-purple-300">
                    {sessionAnalytics.avgScore} pts
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Playlist Breakdown Cards */}
          {playlists.length > 0 && (
            <div className="mt-5 pt-4 border-t border-slate-800/80">
              <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-3">
                Playlists Trajectory
              </h4>
              <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3">
                {playlists.map((pl) => (
                  <div
                    key={pl.playlist_id}
                    className="p-3.5 rounded-xl bg-slate-950/60 border border-slate-800/80 flex flex-col justify-between"
                  >
                    <div className="flex items-center justify-between mb-2">
                      <span className="font-semibold text-xs text-white truncate max-w-[140px]">
                        {pl.playlist_name}
                      </span>
                      <span
                        className={`inline-flex items-center gap-0.5 text-xs font-bold font-mono ${
                          pl.mmr_delta > 0
                            ? 'text-emerald-400'
                            : pl.mmr_delta < 0
                            ? 'text-rose-400'
                            : 'text-slate-400'
                        }`}
                      >
                        {pl.mmr_delta > 0 ? <TrendingUp className="w-3 h-3" /> : pl.mmr_delta < 0 ? <TrendingDown className="w-3 h-3" /> : null}
                        {formatMMRDelta(pl.mmr_delta)}
                      </span>
                    </div>

                    <div className="flex items-center justify-between text-[11px] text-slate-400 mb-1.5">
                      <span>{pl.wins}W - {pl.losses}L</span>
                      <span className="font-bold text-cyan-400">{pl.win_rate.toFixed(1)}%</span>
                    </div>

                    {/* Progress bar */}
                    <div className="w-full bg-slate-800 rounded-full h-1.5 overflow-hidden flex">
                      <div
                        className="bg-emerald-500 h-full"
                        style={{ width: `${pl.win_rate}%` }}
                      />
                      <div
                        className="bg-rose-500 h-full"
                        style={{ width: `${100 - pl.win_rate}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
