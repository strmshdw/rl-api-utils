import React, { useState, useMemo, useEffect, useCallback } from 'react';
import { useSession } from './hooks/useSession';
import { useLiveMatch } from './hooks/useLiveMatch';
import { useColumnConfig } from './hooks/useColumnConfig';
import { TopBar, DashboardTab } from './components/layout/TopBar';
import { LiveGameView } from './components/live/LiveGameView';
import { ColumnConfigModal } from './components/live/ColumnConfigModal';
import { SessionView } from './components/session/SessionView';
import { HistoryView } from './components/historical/HistoryView';
import { PlayersView } from './components/players/PlayersView';
import { OBSOverlayView } from './components/overlay/OBSOverlayView';
import { isDemoLiveMatch, isDemoMode, SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from './utils/sampleData';
import {
  chronological,
  filterSessionByPlaylist,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  normalizeActiveModeKey,
  onlineIdOf,
} from './utils/broadcast';
import { DashboardProvider, useAreaFilters, useDashboard } from './context/DashboardContext';
import { NavigationProvider, useNavigation } from './context/NavigationContext';
import { PlayerProfileDialog } from './components/players/PlayerProfileDialog';
import { HistoricalMatchDetailModal, TelemetryTab } from './components/historical/HistoricalMatchDetailModal';
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
  const [telemetryInitialTab, setTelemetryInitialTab] = useState<TelemetryTab>('core');
  const columnCfg = useColumnConfig();

  const {
    syncLivePlaylist,
    liveModeOverride,
    refreshStampingConfig,
    isDevBuild,
    previewMode,
  } = useDashboard();
  const sessionFilters = useAreaFilters('session');
  const { match: liveMatch, status } = useLiveMatch();
  const activePreview = isDevBuild && (previewMode || isDemoMode());

  const match = useMemo(() => {
    if (liveMatch && (liveMatch.active_match || liveMatch.match_ended)) return liveMatch;
    if (!activePreview) return liveMatch;
    return previewMode || isDemoLiveMatch() ? SAMPLE_CURRENT_MATCH : liveMatch;
  }, [liveMatch, activePreview, previewMode]);

  const realInMatch = !!match?.active_match;
  const inMatch =
    activePreview && liveModeOverride === 'match'
      ? true
      : activePreview && liveModeOverride === 'idle'
      ? false
      : realInMatch;

  const liveMatchMode = match?.playlist_id ? modeKeyFromPlaylistId(match.playlist_id) : undefined;
  const activeSessionMode =
    activeTab === 'live' ? liveMatchMode ?? sessionFilters.playlist : sessionFilters.playlist;

  const {
    session: liveSession,
    unfilteredSession: liveUnfilteredSession,
    pastSessions = [],
    isLoading: sessionLoading,
    error: sessionError,
    refreshSession,
    resetSession,
    continueSession,
    dismissContinueSession,
  } = useSession(activeSessionMode);

  useEffect(() => {
    setTelemetryMatch(null);
  }, [activeTab, fullPagePlayer]);

  useEffect(() => {
    const onPopState = () => setTelemetryMatch(null);
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  useEffect(() => {
    if (!activePreview) {
      void refreshStampingConfig();
    }
  }, [activePreview, refreshStampingConfig]);

  const rawSession = useMemo(() => {
    const candidate = liveUnfilteredSession ?? liveSession;
    if (candidate && (candidate.matches?.length ?? 0) > 0) return candidate;
    return activePreview ? SAMPLE_SESSION_RESPONSE : candidate;
  }, [liveUnfilteredSession, liveSession, activePreview]);

  const normalizedLiveMode = useMemo(() => {
    if (inMatch && match?.playlist_id) return modeKeyFromPlaylistId(match.playlist_id);
    if (match?.match_ended && match?.playlist_id) return modeKeyFromPlaylistId(match.playlist_id);
    const latestMatch = chronological(rawSession?.matches).pop();
    if (latestMatch?.playlist_id) return modeKeyFromPlaylistId(latestMatch.playlist_id);
    if (rawSession?.most_played_playlist?.id) {
      return modeKeyFromPlaylistId(rawSession.most_played_playlist.id);
    }
    return '11';
  }, [inMatch, match?.match_ended, match?.playlist_id, rawSession]);

  const normalizedSessionMode = useMemo(
    () => normalizeActiveModeKey(sessionFilters.playlist),
    [sessionFilters.playlist],
  );
  const normalizedHeaderMode = activeTab === 'live' ? normalizedLiveMode : normalizedSessionMode;

  const liveScopedSession = useMemo(() => {
    if (!rawSession) return null;
    if (
      activeTab === 'live' &&
      liveUnfilteredSession &&
      rawSession === liveUnfilteredSession &&
      liveSession &&
      activeSessionMode === normalizedLiveMode
    ) {
      return liveSession;
    }
    return filterSessionByPlaylist(rawSession, normalizedLiveMode);
  }, [rawSession, activeTab, normalizedLiveMode, liveUnfilteredSession, liveSession, activeSessionMode]);

  const sessionScopedSession = useMemo(() => {
    if (!rawSession) return null;
    if (
      activeTab !== 'live' &&
      liveUnfilteredSession &&
      rawSession === liveUnfilteredSession &&
      liveSession
    ) {
      return liveSession;
    }
    return filterSessionByPlaylist(rawSession, normalizedSessionMode);
  }, [rawSession, activeTab, normalizedSessionMode, liveUnfilteredSession, liveSession]);

  const headerScopedSession = activeTab === 'live' ? liveScopedSession : sessionScopedSession;

  // IDs the local player has appeared under, used to orient historical replays.
  const localPlayerIds = useMemo(() => {
    const ids = new Set<string>();
    if (match?.local_player?.player_id) ids.add(onlineIdOf(match.local_player.player_id));
    match?.local_player_ids?.forEach((id) => ids.add(onlineIdOf(id)));
    rawSession?.local_player_ids?.forEach((id) => ids.add(onlineIdOf(id)));
    rawSession?.matches?.forEach((m) =>
      m.players?.forEach((p) => p.is_local && ids.add(onlineIdOf(p.player_id))),
    );
    ids.delete('');
    return ids;
  }, [match, rawSession]);

  useEffect(() => {
    if (inMatch && match?.playlist_id) {
      syncLivePlaylist(match.playlist_id);
    } else if (!inMatch) {
      syncLivePlaylist(null);
    }
  }, [inMatch, match?.playlist_id, syncLivePlaylist]);

  const lastMatch = chronological(headerScopedSession?.matches).pop();
  const headerPlaylistId = useMemo(() => {
    const param = normalizedHeaderMode;
    const numericId = Number(param);
    if (Number.isInteger(numericId) && numericId > 0) {
      return numericId;
    }
    if (param) {
      const matched = Object.values(rawSession?.playlists ?? {}).find((pl) =>
        matchesPlaylistFilter(pl.playlist_id, param),
      );
      return matched?.playlist_id;
    }
    if (inMatch && match?.playlist_id) return match.playlist_id;
    return headerScopedSession?.most_played_playlist?.id ?? lastMatch?.playlist_id;
  }, [inMatch, match?.playlist_id, normalizedHeaderMode, rawSession, headerScopedSession, lastMatch?.playlist_id]);

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
        session={rawSession}
        scopedSession={headerScopedSession}
        statusMode={normalizedHeaderMode}
        playlistId={headerPlaylistId}
        onRefreshSession={refreshSession}
        onOpenColumns={() => setColumnsOpen(true)}
      />

      <main className="flex-1 w-full max-w-[1680px] mx-auto box-border px-4 sm:px-6 pb-10 pt-4">
        <div className="t-bg-panel bg-panel ziggurat-monolith overflow-hidden">
          <div className="deco-frieze-rule" />
          <div className="p-4 sm:p-6">
            {activeTab === 'live' && (
              <LiveGameView
                match={match}
                session={rawSession}
                scopedSession={liveScopedSession}
                activePlaylist={normalizedLiveMode}
                connection={status}
                columnsOpen={false}
                onCloseColumns={() => setColumnsOpen(false)}
                onShowSession={() => onTabChange('session')}
                onContinueSession={continueSession}
                onDismissContinueSession={dismissContinueSession}
                onInspectPlayer={handleInspectQuickPlayer}
                onOpenMatchTelemetry={(m, tab = 'core') => {
                  setTelemetryInitialTab(tab);
                  setTelemetryMatch(m);
                }}
              />
            )}
            {activeTab === 'session' && (
              <SessionView
                session={rawSession}
                scopedSession={sessionScopedSession}
                activePlaylist={sessionFilters.playlist}
                onSelectPlaylist={sessionFilters.setPlaylist}
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
          initialTab={telemetryInitialTab}
          onClose={() => setTelemetryMatch(null)}
          localPlayerIds={localPlayerIds}
          onPlayerClick={handleInspectQuickPlayer}
        />
      )}

      <ColumnConfigModal
        isOpen={columnsOpen}
        onClose={() => setColumnsOpen(false)}
        config={columnCfg.config}
        toggleColumn={columnCfg.toggleColumn}
        applyPreset={columnCfg.applyPreset}
        resetToDefault={columnCfg.resetToDefault}
        activePreset={columnCfg.activePreset}
      />
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
