import React, { useState, useEffect } from 'react';
import { CurrentMatchResponse, SessionResponse } from '../../types/api';
import { formatMMRDelta, formatWinRate } from '../../utils/formatters';
import { Radio, TrendingUp, TrendingDown } from 'lucide-react';

export const OBSOverlayView: React.FC = () => {
  const [currentMatch, setCurrentMatch] = useState<CurrentMatchResponse | null>(null);
  const [session, setSession] = useState<SessionResponse | null>(null);

  // Poll or SSE stream for real-time match state
  useEffect(() => {
    const fetchData = async () => {
      try {
        const [matchRes, sessRes] = await Promise.all([
          fetch('/api/current-match'),
          fetch('/api/session'),
        ]);
        if (matchRes.ok) setCurrentMatch(await matchRes.json());
        if (sessRes.ok) setSession(await sessRes.json());
      } catch {
        // Overlay silently retries
      }
    };

    fetchData();

    let eventSource: EventSource | null = null;
    try {
      eventSource = new EventSource('/api/events');
      eventSource.addEventListener('match_update', (e) => {
        try {
          setCurrentMatch(JSON.parse(e.data));
        } catch {}
      });
      eventSource.addEventListener('match_ended', (e) => {
        try {
          setCurrentMatch(JSON.parse(e.data));
          fetchData();
        } catch {}
      });
      eventSource.addEventListener('session_update', () => {
        fetch('/api/session')
          .then((r) => r.json())
          .then((d) => setSession(d))
          .catch(() => {});
      });
    } catch {
      console.warn('SSE fallback in overlay');
    }

    // Backup polling every 3 seconds
    const interval = setInterval(fetchData, 3000);

    return () => {
      if (eventSource) eventSource.close();
      clearInterval(interval);
    };
  }, []);

  const inMatch = currentMatch?.active_match ?? false;
  const matchEnded = currentMatch?.match_ended ?? false;

  // Compute blue / orange scores
  let blueScore = 0;
  let orangeScore = 0;
  if (currentMatch) {
    const allPlayers = [
      ...(currentMatch.teammates || []),
      ...(currentMatch.opponents || []),
      ...(currentMatch.local_player ? [currentMatch.local_player] : []),
    ];
    for (const p of allPlayers) {
      if (p.team_num === 0) blueScore += p.stats?.goals || 0;
      if (p.team_num === 1) orangeScore += p.stats?.goals || 0;
    }
  }

  // Session stats
  const totalWins = session?.total_wins ?? 0;
  const totalLosses = session?.total_losses ?? 0;
  const sessionWinRate = formatWinRate(totalWins, totalLosses);

  // Active playlist MMR delta
  const activePlaylistStats = currentMatch?.playlist_id && session?.playlists
    ? session.playlists[currentMatch.playlist_id.toString()] || session.playlists[currentMatch.playlist_id]
    : null;

  const mmrDelta = activePlaylistStats?.mmr_delta ?? 0;

  return (
    <div className="p-2 inline-block select-none font-sans">
      <div className="flex items-center gap-3 rounded-2xl bg-slate-950/85 backdrop-blur-md border border-slate-700/80 px-4 py-2.5 shadow-[0_4px_24px_rgba(0,0,0,0.8)]">
        {inMatch ? (
          <>
            {/* Live Indicator */}
            <div className="flex items-center gap-1.5 pr-2 border-r border-slate-800">
              <span className="relative flex h-3 w-3">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-3 w-3 bg-emerald-500" />
              </span>
              <span className="text-[11px] font-black uppercase tracking-wider text-emerald-400">
                {matchEnded ? 'FINAL' : 'LIVE'}
              </span>
            </div>

            {/* Scoreboard Widget */}
            <div className="flex items-center gap-2 px-3 py-1 rounded-xl bg-slate-900/90 border border-slate-800">
              <span className="text-2xl font-black text-[#00a2ff] drop-shadow-[0_0_8px_rgba(0,162,255,0.6)]">
                {blueScore}
              </span>
              <span className="text-slate-500 font-bold text-lg">:</span>
              <span className="text-2xl font-black text-[#ff7b00] drop-shadow-[0_0_8px_rgba(255,123,0,0.6)]">
                {orangeScore}
              </span>
            </div>

            {/* Playlist & MMR */}
            <div className="flex flex-col">
              <div className="text-xs font-bold text-white truncate max-w-[140px] drop-shadow-sm">
                {currentMatch?.playlist_name || 'Ranked Match'}
              </div>
              <div className="flex items-center gap-1.5 text-[11px]">
                {mmrDelta > 0 ? (
                  <span className="flex items-center gap-0.5 font-bold text-emerald-400">
                    <TrendingUp className="w-3 h-3" />
                    {formatMMRDelta(mmrDelta)} MMR
                  </span>
                ) : mmrDelta < 0 ? (
                  <span className="flex items-center gap-0.5 font-bold text-rose-400">
                    <TrendingDown className="w-3 h-3" />
                    {formatMMRDelta(mmrDelta)} MMR
                  </span>
                ) : (
                  <span className="font-bold text-slate-400">±0.0 MMR</span>
                )}
              </div>
            </div>

            {/* Session W/L Divider & Pill */}
            <div className="pl-2 border-l border-slate-800 flex flex-col items-end">
              <span className="text-[10px] font-bold text-slate-400 uppercase tracking-wider">
                Session
              </span>
              <span className="text-xs font-black text-white">
                <strong className="text-emerald-400">{totalWins}W</strong> -{' '}
                <strong className="text-rose-400">{totalLosses}L</strong> ({sessionWinRate})
              </span>
            </div>
          </>
        ) : (
          /* Idle HUD */
          <>
            <div className="flex items-center gap-2 pr-2 border-r border-slate-800">
              <Radio className="w-4 h-4 text-cyan-400 animate-pulse" />
              <span className="text-xs font-black uppercase tracking-wider text-slate-300">
                IN QUEUE
              </span>
            </div>

            <div className="flex items-center gap-4 text-xs font-semibold">
              <div>
                <span className="text-slate-400 text-[10px] uppercase block tracking-wider">Session</span>
                <span className="text-white font-bold">
                  <strong className="text-emerald-400">{totalWins}W</strong> -{' '}
                  <strong className="text-rose-400">{totalLosses}L</strong> ({sessionWinRate})
                </span>
              </div>

              {activePlaylistStats && (
                <div className="pl-3 border-l border-slate-800">
                  <span className="text-slate-400 text-[10px] uppercase block tracking-wider">
                    {activePlaylistStats.playlist_name}
                  </span>
                  <span
                    className={`font-bold ${
                      mmrDelta > 0
                        ? 'text-emerald-400'
                        : mmrDelta < 0
                        ? 'text-rose-400'
                        : 'text-slate-400'
                    }`}
                  >
                    {formatMMRDelta(mmrDelta)} MMR
                  </span>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
};
