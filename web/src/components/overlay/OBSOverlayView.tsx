import React, { useState, useEffect, useMemo } from 'react';
import { CurrentMatchResponse, SessionResponse, SessionMatchDetail } from '../../types/api';
import { sharedSSEManager, ClockUpdatePayload } from '../../context/SSEContext';
import { deltaInk, formatPct, formatRecord, formatSigned, playlistLabels, formatMatchClock } from '../../utils/broadcast';

export const OBSOverlayView: React.FC = () => {
  const [currentMatch, setCurrentMatch] = useState<CurrentMatchResponse | null>(null);
  const [session, setSession] = useState<SessionResponse | null>(null);

  const params = useMemo(() => new URLSearchParams(window.location.search), []);
  const theme = params.get('theme') || 'dark';
  const showList = (params.get('show') || 'clock,score,session,delta')
    .toLowerCase()
    .split(',')
    .map((s) => s.trim());
  const showSet = useMemo(() => new Set(showList), [showList]);
  const font = params.get('font') || 'display';
  const scale = params.get('scale') ? parseFloat(params.get('scale')!) : 1;

  const fontClass = font === 'mono' ? 'font-mono' : font === 'sans' ? 'font-sans' : 'font-display';

  const themeClasses: Record<string, string> = {
    dark: 'bg-[#0c0d12]/90 text-ink border border-line-2',
    light: 'bg-white/95 text-slate-900 border border-slate-200 shadow-md',
    neon: 'bg-black/95 text-cyan-300 border-2 border-cyan-400 shadow-[0_0_20px_rgba(6,182,212,0.4)]',
    transparent: 'bg-transparent text-ink',
  };
  const activeThemeClass = themeClasses[theme] || themeClasses.dark;

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

    // Consume shared SSE manager without duplicate connections
    const unsubMatchUpdate = sharedSSEManager.subscribe('match_update', (data: CurrentMatchResponse) => {
      setCurrentMatch(data);
    });

    const unsubMatchEnded = sharedSSEManager.subscribe('match_ended', (detail: SessionMatchDetail) => {
      setCurrentMatch((prev) => {
        if (!prev) return null;
        return {
          ...prev,
          active_match: false,
          match_ended: true,
          winner_team: detail.winner_team !== undefined ? detail.winner_team : prev.winner_team,
          result: (detail.result as any) || prev.result,
        };
      });
      fetchData();
    });

    const unsubClock = sharedSSEManager.subscribe('clock_updated', (data: ClockUpdatePayload) => {
      setCurrentMatch((prev) => {
        if (!prev) return prev;
        return {
          ...prev,
          time_remaining: data.time_remaining !== undefined ? data.time_remaining : prev.time_remaining,
          is_overtime: data.is_overtime !== undefined ? data.is_overtime : prev.is_overtime,
        };
      });
    });

    const unsubSessionUpdate = sharedSSEManager.subscribe('session_update', (data: SessionResponse) => {
      if (data && typeof data === 'object') {
        setSession(data);
      }
    });

    return () => {
      unsubMatchUpdate();
      unsubMatchEnded();
      unsubClock();
      unsubSessionUpdate();
    };
  }, []);

  const inMatch = currentMatch?.active_match ?? false;
  const matchEnded = currentMatch?.match_ended ?? false;

  const allPlayers = useMemo(() => {
    if (!currentMatch) return [];
    return [
      ...(currentMatch.teammates || []),
      ...(currentMatch.opponents || []),
      ...(currentMatch.local_player ? [currentMatch.local_player] : []),
    ];
  }, [currentMatch]);

  const bluePlayers = allPlayers.filter((p) => p.team_num === 0);
  const orangePlayers = allPlayers.filter((p) => p.team_num === 1);

  // Compute blue / orange scores
  let blueScore = 0;
  let orangeScore = 0;
  for (const p of bluePlayers) blueScore += p.stats?.goals || 0;
  for (const p of orangePlayers) orangeScore += p.stats?.goals || 0;

  const totalWins = session?.total_wins ?? 0;
  const totalLosses = session?.total_losses ?? 0;
  const winPct = formatPct(totalWins, totalLosses);

  // Active playlist MMR delta
  const activePlaylistStats = currentMatch?.playlist_id && session?.playlists
    ? session.playlists[currentMatch.playlist_id.toString()] || session.playlists[currentMatch.playlist_id]
    : null;
  const mmrDelta = activePlaylistStats?.mmr_delta ?? 0;
  const playlistShort = playlistLabels(currentMatch?.playlist_id, currentMatch?.playlist_name).short;

  const sessionBlock = (
    <div className="bg-panel flex flex-col justify-center px-4">
      <span className="font-semibold text-[11px] tracking-t14 text-muted-2">SESSION</span>
      <span className="font-extrabold text-[20px] leading-none whitespace-nowrap">
        {formatRecord(totalWins, totalLosses)}
        {winPct && <span className="text-muted text-[14px] font-semibold"> {winPct}</span>}
      </span>
    </div>
  );

  const deltaBlock = activePlaylistStats && (
    <div className="bg-panel flex flex-col justify-center px-4 border-l border-page">
      <span className="font-semibold text-[11px] tracking-t14 text-muted-2 whitespace-nowrap">
        {playlistLabels(activePlaylistStats.playlist_id, activePlaylistStats.playlist_name).short}
      </span>
      <span className={`font-extrabold text-[20px] leading-none ${deltaInk(mmrDelta)}`}>{formatSigned(mmrDelta)}</span>
    </div>
  );

  return (
    <div
      data-testid="obs-overlay-view"
      style={{
        transform: scale !== 1 ? `scale(${scale})` : undefined,
        transformOrigin: 'top left',
      }}
      className={`p-2 inline-flex flex-col select-none ${fontClass} ${activeThemeClass}`}
    >
      <div className="flex items-stretch h-14">
        {inMatch || matchEnded ? (
          <>
            {showSet.has('score') && (
              <>
                <div className="bg-blue-slab flex items-center px-4 font-extrabold text-[18px] tracking-t6">BLUE</div>
                <div className="w-14 bg-ink text-page flex items-center justify-center font-extrabold text-[40px] leading-none">
                  {blueScore}
                </div>
              </>
            )}

            <div className="bg-tile flex flex-col items-center justify-center px-4 gap-0.5 min-w-[96px]">
              {showSet.has('clock') && (inMatch || matchEnded) && currentMatch?.time_remaining !== undefined && currentMatch?.time_remaining !== null ? (
                <span
                  data-testid="overlay-clock"
                  className={`font-mono font-bold text-[18px] leading-none ${currentMatch?.is_overtime ? 'text-loss animate-pulse' : 'text-ink'}`}
                >
                  {formatMatchClock(currentMatch?.time_remaining, currentMatch?.is_overtime)}
                </span>
              ) : (
                <span className={`font-bold text-[11px] tracking-t14 ${matchEnded ? 'text-muted' : 'text-loss'}`}>
                  {matchEnded ? 'FINAL' : '● LIVE'}
                </span>
              )}
              <span className="font-semibold text-[13px] tracking-t6 text-ink-2 whitespace-nowrap">{playlistShort}</span>
            </div>

            {showSet.has('score') && (
              <>
                <div className="w-14 bg-ink text-page flex items-center justify-center font-extrabold text-[40px] leading-none">
                  {orangeScore}
                </div>
                <div className="bg-orange-slab text-page flex items-center px-4 font-extrabold text-[18px] tracking-t6 mr-[2px]">
                  ORANGE
                </div>
              </>
            )}

            {showSet.has('session') && sessionBlock}
            {showSet.has('delta') && deltaBlock}
          </>
        ) : (
          <div className="flex items-stretch h-14">
            <div className="bg-tile flex items-center gap-2 px-4 font-bold text-[13px] tracking-t14 text-muted mr-[2px]">
              <span className="w-[7px] h-[7px] rounded-full bg-faint" />
              IN QUEUE
            </div>
            {showSet.has('session') && sessionBlock}
            {showSet.has('delta') && deltaBlock}
          </div>
        )}
      </div>

      {showSet.has('rosters') && inMatch && (
        <div data-testid="overlay-rosters" className="flex border-t border-line-2 text-[12px] font-semibold mt-1">
          <div className="flex-1 bg-blue-slab/10 p-2 flex flex-col gap-1">
            {bluePlayers.map((p) => (
              <div key={p.player_id} className="flex justify-between items-center text-blue-ink">
                <span className="truncate">{p.is_local ? 'You' : p.name}</span>
                <span className="font-mono font-bold">{p.stats?.goals || 0}G</span>
              </div>
            ))}
          </div>
          <div className="flex-1 bg-orange-slab/10 p-2 flex flex-col gap-1 border-l border-line-2">
            {orangePlayers.map((p) => (
              <div key={p.player_id} className="flex justify-between items-center text-orange-ink">
                <span className="truncate">{p.is_local ? 'You' : p.name}</span>
                <span className="font-mono font-bold">{p.stats?.goals || 0}G</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
