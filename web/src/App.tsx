import React, { useState, useMemo, useEffect, useCallback } from 'react';
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
import { NavigationProvider, useNavigation } from './context/NavigationContext';
import { PlayerProfileDialog } from './components/players/PlayerProfileDialog';
import { HistoricalMatchDetailModal } from './components/historical/HistoricalMatchDetailModal';
import { sessionMatchToHistoricalRecord } from './components/historical/historyData';
import { SessionMatchDetail } from './types/api';

const DashboardShell: React.FC = () => {
  const {
    activeTab,
    fullPagePlayer,
    quickModalPlayer,
    handleTabChange,
    pushNavigation,
    handleInspectQuickPlayer,
    handleCloseQuickPlayer,
    handleOpenFullProfile: navOpenFullProfile,
    handleBackFromFullProfile,
  } = useNavigation();

  const [columnsOpen, setColumnsOpen] = useState(false);
  const [telemetryMatch, setTelemetryMatch] = useState<SessionMatchDetail | null>(null);

  const { liveModeOverride, isDevBuild, previewMode } = useDashboard();
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

  useEffect(() => {
    setTelemetryMatch(null);
  }, [activeTab, fullPagePlayer]);

  useEffect(() => {
    const onPopState = () => setTelemetryMatch(null);
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  // Preview / demo mode (dev/debug build only) falls back to sample data when enabled.
  const activePreview = isDevBuild && (previewMode || isDemoMode());
  const session = useMemo(() => {
    if (liveSession && liveSession.matches?.length > 0) return liveSession;
    return activePreview ? SAMPLE_SESSION_RESPONSE : liveSession;
  }, [liveSession, activePreview]);

  const match = useMemo(() => {
    if (liveMatch && (liveMatch.active_match || liveMatch.match_ended)) return liveMatch;
    if (!activePreview) return liveMatch;
    return previewMode || isDemoLiveMatch() ? SAMPLE_CURRENT_MATCH : liveMatch;
  }, [liveMatch, activePreview, previewMode]);

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
    activePreview && liveModeOverride === 'match'
      ? true
      : activePreview && liveModeOverride === 'idle'
      ? false
      : realInMatch;
  const lastMatch = chronological(session?.matches).pop();
  const headerPlaylistId = inMatch ? match?.playlist_id ?? 11 : lastMatch?.playlist_id;

  const onTabChange = useCallback(
    (nextTab: DashboardTab) => {
      setTelemetryMatch(null);
      handleTabChange(nextTab);
    },
    [handleTabChange],
  );

  const handleOpenFullProfile = useCallback(
    (playerId: string, playerName?: string) => {
      setTelemetryMatch(null);
      navOpenFullProfile(playerId, playerName);
    },
    [navOpenFullProfile],
  );

  return (
    <div className="min-h-screen t-bg-page bg-page text-ink flex flex-col font-display broadcast-app">
      <TopBar
        activeTab={activeTab}
        onTabChange={onTabChange}
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
              <span>RL/SYNC TELEMETRY DESK // REAL-TIME MATCH &amp; SESSION ANALYTICS</span>
            </span>
            <span className="t-font-mono text-[11px] text-muted hidden md:inline">
              25S POST-WHISTLE MMR BURST POLLING · ACTIVE
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
                onShowSession={() => onTabChange('session')}
                onContinueSession={continueSession}
                onDismissContinueSession={dismissContinueSession}
                onInspectPlayer={handleInspectQuickPlayer}
                onOpenMatchTelemetry={(m) => setTelemetryMatch(m)}
              />
            )}
            {activeTab === 'session' && (
              <SessionView
                session={session}
                pastSessions={pastSessions}
                liveMatch={match}
                isLoading={sessionLoading}
                error={sessionError}
                onResetSession={resetSession}
                onRefreshSession={refreshSession}
                onContinueSession={continueSession}
                onDismissContinueSession={dismissContinueSession}
                onInspectPlayer={handleInspectQuickPlayer}
              />
            )}
            {activeTab === 'history' && (
              <HistoryView
                localPlayerIds={localPlayerIds}
                liveMatch={match}
                onOpenFullProfile={handleOpenFullProfile}
              />
            )}
            {activeTab === 'players' && (
              <PlayersView
                localPlayerIds={localPlayerIds}
                liveMatch={match}
                initialInspectedPlayer={fullPagePlayer}
                inspectedPlayer={fullPagePlayer}
                onInspectPlayerChange={(player) => {
                  if (player) {
                    pushNavigation('players', player, null);
                  } else {
                    handleBackFromFullProfile();
                  }
                }}
              />
            )}
          </div>
        </div>
      </main>

      {quickModalPlayer && (
        <PlayerProfileDialog
          playerId={quickModalPlayer.id}
          playerName={quickModalPlayer.name}
          onClose={handleCloseQuickPlayer}
          localPlayerIds={localPlayerIds}
          liveMatch={match}
          onOpenFullProfile={handleOpenFullProfile}
        />
      )}

      {telemetryMatch && (
        <HistoricalMatchDetailModal
          match={sessionMatchToHistoricalRecord(telemetryMatch)}
          onClose={() => setTelemetryMatch(null)}
          localPlayerIds={localPlayerIds}
          onPlayerClick={handleInspectQuickPlayer}
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
      <NavigationProvider>
        <DashboardShell />
      </NavigationProvider>
    </DashboardProvider>
  );
};

export default App;
