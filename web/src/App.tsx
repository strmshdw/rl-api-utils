import React, { useState, useMemo, useEffect } from 'react';
import { useSession } from './hooks/useSession';
import { useLiveMatch } from './hooks/useLiveMatch';
import { TopBar, DashboardTab } from './components/layout/TopBar';
import { LiveGameView } from './components/live/LiveGameView';
import { SessionView } from './components/session/SessionView';
import { HistoryView } from './components/historical/HistoryView';
import { PlayersView } from './components/players/PlayersView';
import { OBSOverlayView } from './components/overlay/OBSOverlayView';
import { isDemoLiveMatch, isDemoMode, SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from './utils/sampleData';
import { chronological, onlineIdOf } from './utils/broadcast';

export const App: React.FC = () => {
  const [activeTab, setActiveTab] = useState<DashboardTab>('live');
  const [columnsOpen, setColumnsOpen] = useState(false);

  // Route / mode switch: detect OBS overlay mode via query param or path
  const isOverlayMode = useMemo(() => {
    const params = new URLSearchParams(window.location.search);
    return (
      params.get('mode') === 'overlay' ||
      params.get('overlay') === 'true' ||
      window.location.pathname === '/overlay' ||
      window.location.pathname.startsWith('/overlay')
    );
  }, []);

  useEffect(() => {
    document.documentElement.classList.toggle('overlay-mode', isOverlayMode);
    document.body.classList.toggle('overlay-mode', isOverlayMode);
  }, [isOverlayMode]);

  const { session: liveSession, isLoading: sessionLoading, error: sessionError, resetSession } = useSession();
  const { match: liveMatch, status } = useLiveMatch();

  // Preview / demo mode (?preview=true, or ?preview=idle) falls back to sample data.
  const session = useMemo(() => {
    if (liveSession && liveSession.matches?.length > 0) return liveSession;
    return isDemoMode() ? SAMPLE_SESSION_RESPONSE : liveSession;
  }, [liveSession]);

  const match = useMemo(() => {
    if (liveMatch && (liveMatch.active_match || liveMatch.match_ended)) return liveMatch;
    return isDemoLiveMatch() ? SAMPLE_CURRENT_MATCH : liveMatch;
  }, [liveMatch]);

  // IDs the local player has appeared under, used to orient historical replays.
  const localPlayerIds = useMemo(() => {
    const ids = new Set<string>();
    if (match?.local_player?.player_id) ids.add(onlineIdOf(match.local_player.player_id));
    match?.local_player_ids?.forEach((id) => ids.add(onlineIdOf(id)));
    session?.local_player_ids?.forEach((id) => ids.add(onlineIdOf(id)));
    session?.matches?.forEach((m) => m.players?.forEach((p) => p.is_local && ids.add(onlineIdOf(p.player_id))));
    ids.delete('');
    return ids;
  }, [match, session]);

  if (isOverlayMode) {
    return <OBSOverlayView />;
  }

  const inMatch = !!match?.active_match;
  const lastMatch = chronological(session?.matches).pop();
  const headerPlaylistId = inMatch ? match?.playlist_id : lastMatch?.playlist_id;

  return (
    <div className="min-h-screen bg-page text-ink flex flex-col font-display">
      <TopBar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        inMatch={inMatch}
        session={session}
        playlistId={headerPlaylistId}
        onOpenColumns={activeTab === 'live' && inMatch ? () => setColumnsOpen(true) : undefined}
      />

      <main className="flex-1 w-full max-w-[1440px] mx-auto box-border px-8 pb-12">
        {activeTab === 'live' && (
          <LiveGameView
            match={match}
            session={session}
            connection={status}
            columnsOpen={columnsOpen}
            onCloseColumns={() => setColumnsOpen(false)}
            onShowSession={() => setActiveTab('session')}
          />
        )}
        {activeTab === 'session' && (
          <SessionView session={session} isLoading={sessionLoading} error={sessionError} onResetSession={resetSession} />
        )}
        {activeTab === 'history' && <HistoryView localPlayerIds={localPlayerIds} />}
        {activeTab === 'players' && <PlayersView localPlayerIds={localPlayerIds} />}
      </main>
    </div>
  );
};

export default App;
