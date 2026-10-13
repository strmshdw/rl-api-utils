import React, { useState, useEffect } from 'react';
import { CurrentMatchResponse, SessionResponse } from '../../types/api';
import { deltaInk, formatPct, formatRecord, formatSigned, playlistLabels } from '../../utils/broadcast';

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
    <div className="p-2 inline-block select-none font-display text-ink">
      {inMatch ? (
        <div className="flex items-stretch h-14">
          <div className="bg-blue-slab flex items-center px-4 font-extrabold text-[18px] tracking-t6">BLUE</div>
          <div className="w-14 bg-ink text-page flex items-center justify-center font-extrabold text-[40px] leading-none">{blueScore}</div>
          <div className="bg-tile flex flex-col items-center justify-center px-4 gap-0.5 min-w-[96px]">
            <span className={`font-bold text-[11px] tracking-t14 ${matchEnded ? 'text-muted' : 'text-loss'}`}>
              {matchEnded ? 'FINAL' : '● LIVE'}
            </span>
            <span className="font-semibold text-[13px] tracking-t6 text-ink-2 whitespace-nowrap">{playlistShort}</span>
          </div>
          <div className="w-14 bg-ink text-page flex items-center justify-center font-extrabold text-[40px] leading-none">{orangeScore}</div>
          <div className="bg-orange-slab text-page flex items-center px-4 font-extrabold text-[18px] tracking-t6 mr-[2px]">ORANGE</div>
          {sessionBlock}
          {deltaBlock}
        </div>
      ) : (
        <div className="flex items-stretch h-14">
          <div className="bg-tile flex items-center gap-2 px-4 font-bold text-[13px] tracking-t14 text-muted mr-[2px]">
            <span className="w-[7px] h-[7px] rounded-full bg-faint" />
            IN QUEUE
          </div>
          {sessionBlock}
          {deltaBlock}
        </div>
      )}
    </div>
  );
};
