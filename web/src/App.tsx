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
import { DashboardProvider, useDashboard } from './context/DashboardContext';
import { PlayerProfileDialog } from './components/players/PlayerProfileDialog';
import { HistoricalMatchDetailModal } from './components/historical/HistoricalMatchDetailModal';
import { sessionMatchToHistoricalRecord } from './components/historical/historyData';
import { SessionMatchDetail } from './types/api';

const DashboardShell: React.FC = () => {
  const [activeTab, setActiveTab] = useState<DashboardTab>('live');
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [quickModalPlayer, setQuickModalPlayer] = useState<{ id: string; name?: string } | null>(null);
  const [telemetryMatch, setTelemetryMatch] = useState<SessionMatchDetail | null>(null);
  const [fullPagePlayer, setFullPagePlayer] = useState<{ id: string; name?: string } | null>(null);

  const { liveModeOverride } = useDashboard();

  const {
    session: liveSession,
    pastSessions = [],
    isLoading: sessionLoading,
    error: sessionError,
    refreshSession,
    resetSession,
    continueSession,
    dismissContinueSession,
  } = useSession();
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

  const realInMatch = !!match?.active_match;
  const inMatch =
    liveModeOverride === 'match' ? true : liveModeOverride === 'idle' ? false : realInMatch;
  const lastMatch = chronological(session?.matches).pop();
  const headerPlaylistId = inMatch ? match?.playlist_id ?? 11 : lastMatch?.playlist_id;

  const handleOpenFullProfile = (playerId: string, playerName?: string) => {
    setQuickModalPlayer(null);
    setTelemetryMatch(null);
    setFullPagePlayer({ id: playerId, name: playerName });
    setActiveTab('players');
  };

  return (
    <div className="min-h-screen t-bg-page bg-page text-ink flex flex-col font-display broadcast-app">
      <TopBar
        activeTab={activeTab}
        onTabChange={setActiveTab}
        inMatch={inMatch}
        session={session}
        playlistId={headerPlaylistId}
        onOpenColumns={activeTab === 'live' && inMatch ? () => setColumnsOpen(true) : undefined}
        onRefreshSession={refreshSession}
      />

      <main className="flex-1 w-full max-w-[1680px] mx-auto box-border px-4 sm:px-6 pb-10 pt-4">
        <div className="t-bg-panel bg-panel ziggurat-monolith overflow-hidden">
          <div className="deco-frieze-rule" />
          <div className="deco-medallion-bar px-5 py-1.5">
            <span className="flex items-center gap-2">
              <span className="text-orange-ink">◆</span>
              <span>ARASHIKAGE MONOLITH // 嵐影之印 · GINZA GEOMETRIC TELEMETRY SCROLL</span>
            </span>
            <span className="t-font-mono text-[11px] text-muted hidden md:inline">
              25S POST-WHISTLE MMR BURST INTERROGATOR · ACTIVE
            </span>
          </div>
          <div className="p-4 sm:p-6">
            {activeTab === 'live' && (
              <LiveGameView
                match={match}
                session={session}
                connection={status}
                columnsOpen={columnsOpen}
                onCloseColumns={() => setColumnsOpen(false)}
                onShowSession={() => setActiveTab('session')}
                onContinueSession={continueSession}
                onDismissContinueSession={dismissContinueSession}
                onInspectPlayer={(id, name) => setQuickModalPlayer({ id, name })}
                onOpenMatchTelemetry={(m) => setTelemetryMatch(m)}
              />
            )}
            {activeTab === 'session' && (
              <SessionView
                session={session}
                pastSessions={pastSessions}
                isLoading={sessionLoading}
                error={sessionError}
                onResetSession={resetSession}
                onRefreshSession={refreshSession}
                onContinueSession={continueSession}
                onDismissContinueSession={dismissContinueSession}
                onInspectPlayer={(id, name) => setQuickModalPlayer({ id, name })}
              />
            )}
            {activeTab === 'history' && (
              <HistoryView
                localPlayerIds={localPlayerIds}
                onOpenFullProfile={handleOpenFullProfile}
              />
            )}
            {activeTab === 'players' && (
              <PlayersView
                localPlayerIds={localPlayerIds}
                initialInspectedPlayer={fullPagePlayer}
                onClearInitialPlayer={() => setFullPagePlayer(null)}
              />
            )}
          </div>
        </div>
      </main>

      {quickModalPlayer && (
        <PlayerProfileDialog
          playerId={quickModalPlayer.id}
          playerName={quickModalPlayer.name}
          onClose={() => setQuickModalPlayer(null)}
          localPlayerIds={localPlayerIds}
          onOpenFullProfile={handleOpenFullProfile}
        />
      )}

      {telemetryMatch && (
        <HistoricalMatchDetailModal
          match={sessionMatchToHistoricalRecord(telemetryMatch)}
          onClose={() => setTelemetryMatch(null)}
          localPlayerIds={localPlayerIds}
          onPlayerClick={(id, name) => setQuickModalPlayer({ id, name })}
        />
      )}
    </div>
  );
};

export const App: React.FC = () => {
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

  if (isOverlayMode) {
    return <OBSOverlayView />;
  }

  return (
    <DashboardProvider>
      <DashboardShell />
    </DashboardProvider>
  );
};

export default App;
