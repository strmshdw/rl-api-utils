import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { DashboardTab } from '../types/matchTypes';
import {
  BrowserNavState,
  PlayerNavTarget,
  buildLocationUrl,
  formatDocumentTitle,
  parseLocationState,
} from '../utils/navigation';

export interface NavigationContextValue {
  activeTab: DashboardTab;
  fullPagePlayer: PlayerNavTarget | null;
  quickModalPlayer: PlayerNavTarget | null;
  handleTabChange: (nextTab: DashboardTab) => void;
  pushNavigation: (
    nextTab: DashboardTab,
    nextPlayer?: PlayerNavTarget | null,
    nextModalPlayer?: PlayerNavTarget | null,
  ) => void;
  handleInspectQuickPlayer: (id: string, name?: string) => void;
  handleCloseQuickPlayer: () => void;
  handleOpenFullProfile: (playerId: string, playerName?: string) => void;
  handleBackFromFullProfile: () => void;
}

const DEFAULT_NAV_STATE: BrowserNavState = {
  rlsyncNav: true,
  depth: 0,
  tab: 'live',
  player: null,
  modalPlayer: null,
};

const DEFAULT_NAVIGATION_CONTEXT: NavigationContextValue = {
  activeTab: 'live',
  fullPagePlayer: null,
  quickModalPlayer: null,
  handleTabChange: () => {},
  pushNavigation: () => {},
  handleInspectQuickPlayer: () => {},
  handleCloseQuickPlayer: () => {},
  handleOpenFullProfile: () => {},
  handleBackFromFullProfile: () => {},
};

const NavigationContext = createContext<NavigationContextValue>(DEFAULT_NAVIGATION_CONTEXT);

export const NavigationProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const initialNav = useMemo<BrowserNavState>(() => {
    if (typeof window === 'undefined') return DEFAULT_NAV_STATE;
    return parseLocationState(window.location, window.history.state);
  }, []);

  const [activeTab, setActiveTab] = useState<DashboardTab>(initialNav.tab);
  const [quickModalPlayer, setQuickModalPlayer] = useState<PlayerNavTarget | null>(initialNav.modalPlayer ?? null);
  const [fullPagePlayer, setFullPagePlayer] = useState<PlayerNavTarget | null>(initialNav.player);

  const entriesByDepthRef = useRef<Map<number, BrowserNavState>>(new Map([[initialNav.depth, initialNav]]));
  const currentDepthRef = useRef<number>(initialNav.depth);

  const applyNavState = useCallback((navState: BrowserNavState) => {
    currentDepthRef.current = navState.depth;
    entriesByDepthRef.current.set(navState.depth, navState);
    setActiveTab(navState.tab);
    setFullPagePlayer(navState.tab === 'players' ? navState.player : null);
    setQuickModalPlayer(navState.modalPlayer ?? null);
    if (typeof document !== 'undefined') {
      document.title = formatDocumentTitle(
        navState.tab,
        navState.tab === 'players' ? navState.player : null,
        navState.modalPlayer,
      );
    }
  }, []);

  const pushNavigation = useCallback(
    (nextTab: DashboardTab, nextPlayer: PlayerNavTarget | null = null, nextModalPlayer: PlayerNavTarget | null = null) => {
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
        if (d >= newDepth) entriesByDepthRef.current.delete(d);
      }

      const nextState: BrowserNavState = {
        rlsyncNav: true,
        depth: newDepth,
        tab: nextTab,
        player: normalizedPlayer,
        modalPlayer: nextModalPlayer,
      };
      if (typeof window !== 'undefined') {
        window.history.pushState(nextState, '', buildLocationUrl(nextState.tab, nextState.player, window.location.search));
      }
      applyNavState(nextState);
    },
    [applyNavState],
  );

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const startUrl = buildLocationUrl(initialNav.tab, initialNav.player, window.location.search);
    window.history.replaceState(initialNav, '', startUrl);
    if (typeof document !== 'undefined') {
      document.title = formatDocumentTitle(initialNav.tab, initialNav.player, initialNav.modalPlayer);
    }

    const handlePopState = (event: PopStateEvent) => {
      const stateObj = event.state as (Partial<BrowserNavState> & { depth?: unknown }) | null;
      let resolved: BrowserNavState;
      if (stateObj && typeof stateObj === 'object' && stateObj.rlsyncNav === true) {
        resolved = parseLocationState(window.location, event.state);
      } else if (stateObj && typeof stateObj.depth === 'number' && entriesByDepthRef.current.has(stateObj.depth)) {
        resolved = entriesByDepthRef.current.get(stateObj.depth)!;
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

  const handleTabChange = useCallback((nextTab: DashboardTab) => {
    pushNavigation(nextTab, null, null);
  }, [pushNavigation]);

  const handleInspectQuickPlayer = useCallback((id: string, name?: string) => {
    pushNavigation(activeTab, fullPagePlayer, { id, name });
  }, [activeTab, fullPagePlayer, pushNavigation]);

  const handleCloseQuickPlayer = useCallback(() => {
    setQuickModalPlayer(null);
    const cur = entriesByDepthRef.current.get(currentDepthRef.current);
    if (cur && cur.modalPlayer) {
      const updated: BrowserNavState = { ...cur, modalPlayer: null };
      entriesByDepthRef.current.set(updated.depth, updated);
      if (typeof window !== 'undefined') {
        window.history.replaceState(updated, '', buildLocationUrl(updated.tab, updated.player, window.location.search));
      }
      if (typeof document !== 'undefined') {
        document.title = formatDocumentTitle(updated.tab, updated.player, null);
      }
    }
  }, []);

  const handleOpenFullProfile = useCallback((playerId: string, playerName?: string) => {
    pushNavigation('players', { id: playerId, name: playerName }, null);
  }, [pushNavigation]);

  const handleBackFromFullProfile = useCallback(() => {
    if (currentDepthRef.current === 0 && fullPagePlayer !== null) {
      pushNavigation('players', null, null);
      return;
    }
    if (typeof window === 'undefined') return;
    const prevDepth = currentDepthRef.current - 1;
    const prevEntry = entriesByDepthRef.current.get(prevDepth);
    window.history.back();
    if (prevEntry && (window.history.state as { depth?: unknown } | null)?.depth !== prevDepth) {
      window.history.replaceState(prevEntry, '', buildLocationUrl(prevEntry.tab, prevEntry.player, window.location.search));
      applyNavState(prevEntry);
    }
  }, [fullPagePlayer, pushNavigation, applyNavState]);

  const value = useMemo<NavigationContextValue>(() => ({
    activeTab, fullPagePlayer, quickModalPlayer,
    handleTabChange, pushNavigation, handleInspectQuickPlayer,
    handleCloseQuickPlayer, handleOpenFullProfile, handleBackFromFullProfile,
  }), [
    activeTab, fullPagePlayer, quickModalPlayer,
    handleTabChange, pushNavigation, handleInspectQuickPlayer,
    handleCloseQuickPlayer, handleOpenFullProfile, handleBackFromFullProfile,
  ]);

  return <NavigationContext.Provider value={value}>{children}</NavigationContext.Provider>;
};

export function useNavigation(): NavigationContextValue {
  return useContext(NavigationContext);
}
