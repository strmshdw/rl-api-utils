import React, { useState, useMemo, useEffect, useRef, useCallback } from 'react';
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
import {
  BrowserNavState,
  PlayerNavTarget,
  buildLocationUrl,
  formatDocumentTitle,
  parseLocationState,
} from './utils/navigation';

const DashboardShell: React.FC = () => {
  const initialNav = useMemo(() => parseLocationState(window.location, window.history.state), []);
  const [activeTab, setActiveTab] = useState<DashboardTab>(initialNav.tab);
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [quickModalPlayer, setQuickModalPlayer] = useState<PlayerNavTarget | null>(initialNav.modalPlayer ?? null);
  const [telemetryMatch, setTelemetryMatch] = useState<SessionMatchDetail | null>(null);
  const [fullPagePlayer, setFullPagePlayer] = useState<PlayerNavTarget | null>(initialNav.player);

  const entriesByDepthRef = useRef<Map<number, BrowserNavState>>(new Map([[initialNav.depth, initialNav]]));
  const currentDepthRef = useRef<number>(initialNav.depth);

  const applyNavState = useCallback((navState: BrowserNavState) => {
    currentDepthRef.current = navState.depth;
    entriesByDepthRef.current.set(navState.depth, navState);
    setActiveTab(navState.tab);
    setFullPagePlayer(navState.tab === 'players' ? navState.player : null);
    setQuickModalPlayer(navState.modalPlayer ?? null);
    if (!navState.modalPlayer) {
      setTelemetryMatch(null);
    }
    document.title = formatDocumentTitle(
      navState.tab,
      navState.tab === 'players' ? navState.player : null,
      navState.modalPlayer,
    );
  }, []);

  const pushNavigation = useCallback(
    (
      nextTab: DashboardTab,
      nextPlayer: PlayerNavTarget | null = null,
      nextModalPlayer: PlayerNavTarget | null = null,
    ) => {
      const normalizedPlayer = nextTab === 'players' ? nextPlayer : null;
      const cur = entriesByDepthRef.current.get(currentDepthRef.current);
      if (
        cur &&
        cur.tab === nextTab &&
        (cur.player?.id ?? null) === (normalizedPlayer?.id ?? null) &&
        (cur.modalPlayer?.id ?? null) === (nextModalPlayer?.id ?? null)
      ) {
        return;
      }

      const newDepth = currentDepthRef.current + 1;
      for (const d of Array.from(entriesByDepthRef.current.keys())) {
        if (d >= newDepth) {
          entriesByDepthRef.current.delete(d);
        }
      }

      const nextState: BrowserNavState = {
        rlsyncNav: true,
        depth: newDepth,
        tab: nextTab,
        player: normalizedPlayer,
        modalPlayer: nextModalPlayer,
      };
      const nextUrl = buildLocationUrl(nextState.tab, nextState.player, window.location.search);
      window.history.pushState(nextState, '', nextUrl);
      applyNavState(nextState);
    },
    [applyNavState],
  );

  useEffect(() => {
    const startUrl = buildLocationUrl(initialNav.tab, initialNav.player, window.location.search);
    window.history.replaceState(initialNav, '', startUrl);
    document.title = formatDocumentTitle(initialNav.tab, initialNav.player, initialNav.modalPlayer);

    const handlePopState = (event: PopStateEvent) => {
      let resolved: BrowserNavState;
      if (
        event.state &&
        typeof event.state === 'object' &&
        (event.state as Partial<BrowserNavState>).rlsyncNav === true
      ) {
        resolved = parseLocationState(window.location, event.state);
      } else if (
        event.state &&
        typeof event.state === 'object' &&
        typeof (event.state as { depth?: unknown }).depth === 'number' &&
        entriesByDepthRef.current.has((event.state as { depth: number }).depth)
      ) {
        resolved = entriesByDepthRef.current.get((event.state as { depth: number }).depth)!;
      } else {
        resolved = parseLocationState(window.location, event.state);
      }
      applyNavState(resolved);
    };

    window.addEventListener('popstate', handlePopState);
    return () => {
      window.removeEventListener('popstate', handlePopState);
      try {
        window.history.replaceState(null, '', '/');
      } catch {
        // ignore in restricted environments
      }
    };
  }, [initialNav, applyNavState]);

  const handleTabChange = useCallback(
    (nextTab: DashboardTab) => {
      pushNavigation(nextTab, null, null);
    },
    [pushNavigation],
  );

  const handleInspectQuickPlayer = useCallback(
    (id: string, name?: string) => {
      pushNavigation(activeTab, fullPagePlayer, { id, name });
    },
    [activeTab, fullPagePlayer, pushNavigation],
  );

  const handleCloseQuickPlayer = useCallback(() => {
    setQuickModalPlayer(null);
    const cur = entriesByDepthRef.current.get(currentDepthRef.current);
    if (cur && cur.modalPlayer) {
      const updated: BrowserNavState = { ...cur, modalPlayer: null };
      entriesByDepthRef.current.set(updated.depth, updated);
      const url = buildLocationUrl(updated.tab, updated.player, window.location.search);
      window.history.replaceState(updated, '', url);
      document.title = formatDocumentTitle(updated.tab, updated.player, null);
    }
  }, []);

  const handleBackFromFullProfile = useCallback(() => {
    const atDeepLinkRoot = currentDepthRef.current === 0 && fullPagePlayer !== null;
    if (atDeepLinkRoot) {
      // Synthesize a Players directory entry when the user landed directly on a deep-linked player URL
      pushNavigation('players', null, null);
      return;
    }
    const prevDepth = currentDepthRef.current - 1;
    const prevEntry = entriesByDepthRef.current.get(prevDepth);
    window.history.back();
    if (prevEntry && window.history.state?.depth !== prevDepth) {
      const url = buildLocationUrl(prevEntry.tab, prevEntry.player, window.location.search);
      window.history.replaceState(prevEntry, '', url);
      applyNavState(prevEntry);
    }
  }, [fullPagePlayer, pushNavigation, applyNavState]);

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

  const handleOpenFullProfile = useCallback(
    (playerId: string, playerName?: string) => {
      setTelemetryMatch(null);
      pushNavigation('players', { id: playerId, name: playerName }, null);
    },
    [pushNavigation],
  );

  return (
    <div className="min-h-screen t-bg-page bg-page text-ink flex flex-col font-display broadcast-app">
      <TopBar
        activeTab={activeTab}
        onTabChange={handleTabChange}
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
                onShowSession={() => handleTabChange('session')}
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
                onOpenFullProfile={handleOpenFullProfile}
              />
            )}
            {activeTab === 'players' && (
              <PlayersView
                localPlayerIds={localPlayerIds}
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
      <DashboardShell />
    </DashboardProvider>
  );
};

export default App;
