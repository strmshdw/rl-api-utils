import type { DashboardTab } from '../types/matchTypes';

export type { DashboardTab };

export interface PlayerNavTarget {
  id: string;
  name?: string;
}

export interface BrowserNavState {
  rlsyncNav: true;
  depth: number;
  tab: DashboardTab;
  player: PlayerNavTarget | null;
  modalPlayer?: PlayerNavTarget | null;
  rlsyncDialogStack?: PlayerNavTarget[];
}

const VALID_TABS: ReadonlySet<string> = new Set<DashboardTab>(['live', 'session', 'history', 'players']);

const TAB_TITLES: Record<DashboardTab, string> = {
  live: 'RL/SYNC',
  session: 'Session | RL/SYNC',
  history: 'Match History | RL/SYNC',
  players: 'Players | RL/SYNC',
};

/**
 * Formats the document title for the current navigation state so browser history
 * entries, tabs, and screen readers clearly distinguish each view.
 */
export function formatDocumentTitle(
  tab: DashboardTab,
  player: PlayerNavTarget | null,
  modalPlayer?: PlayerNavTarget | null,
): string {
  if (modalPlayer?.name || modalPlayer?.id) {
    return `${modalPlayer.name || modalPlayer.id} · Profile | RL/SYNC`;
  }
  if (tab === 'players' && (player?.name || player?.id)) {
    return `${player.name || player.id} · Players | RL/SYNC`;
  }
  return TAB_TITLES[tab] || 'RL/SYNC';
}

/**
 * Builds a clean URL path + search string for a given tab and optional full-page player target,
 * preserving non-routing query parameters (such as ?demo=true or ?preview=true).
 */
export function buildLocationUrl(
  tab: DashboardTab,
  player: PlayerNavTarget | null,
  currentSearch: string = typeof window !== 'undefined' ? window.location.search : '',
): string {
  const params = new URLSearchParams(currentSearch);
  params.delete('tab');
  params.delete('player');
  params.delete('name');

  let pathname = '/';
  if (tab === 'session') {
    pathname = '/session';
  } else if (tab === 'history') {
    pathname = '/history';
  } else if (tab === 'players') {
    if (player?.id) {
      pathname = `/players/${encodeURIComponent(player.id)}`;
      if (player.name) {
        params.set('name', player.name);
      }
    } else {
      pathname = '/players';
    }
  }

  const qs = params.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

/**
 * Resolves a URL location and optional History API state object into a normalized BrowserNavState.
 */
export function parseLocationState(
  loc: Pick<Location, 'pathname' | 'search'> = typeof window !== 'undefined'
    ? window.location
    : { pathname: '/', search: '' },
  historyState: unknown = typeof window !== 'undefined' ? window.history.state : null,
): BrowserNavState {
  if (
    historyState &&
    typeof historyState === 'object' &&
    (historyState as Partial<BrowserNavState>).rlsyncNav === true
  ) {
    const st = historyState as BrowserNavState;
    const tab: DashboardTab = VALID_TABS.has(st.tab) ? st.tab : 'live';
    const player =
      tab === 'players' && st.player && typeof st.player.id === 'string' && st.player.id.trim() !== ''
        ? { id: st.player.id, name: st.player.name }
        : null;
    const modalPlayer =
      st.modalPlayer && typeof st.modalPlayer.id === 'string' && st.modalPlayer.id.trim() !== ''
        ? { id: st.modalPlayer.id, name: st.modalPlayer.name }
        : null;
    return {
      rlsyncNav: true,
      depth: typeof st.depth === 'number' && st.depth >= 0 ? st.depth : 0,
      tab,
      player,
      modalPlayer,
      rlsyncDialogStack: Array.isArray(st.rlsyncDialogStack) ? st.rlsyncDialogStack : undefined,
    };
  }

  const params = new URLSearchParams(loc.search || '');
  const rawPath = (loc.pathname || '/').replace(/\/+$/, '') || '/';

  if (rawPath.startsWith('/players/')) {
    const encodedId = rawPath.slice('/players/'.length);
    let decodedId = encodedId;
    try {
      decodedId = decodeURIComponent(encodedId);
    } catch {
      // keep raw segment if malformed URI encoding
    }
    const nameParam = params.get('name') || undefined;
    return {
      rlsyncNav: true,
      depth: 0,
      tab: 'players',
      player: decodedId ? { id: decodedId, name: nameParam } : null,
    };
  }

  if (rawPath === '/players') {
    const playerParam = params.get('player');
    const nameParam = params.get('name') || undefined;
    return {
      rlsyncNav: true,
      depth: 0,
      tab: 'players',
      player: playerParam ? { id: playerParam, name: nameParam } : null,
    };
  }

  if (rawPath === '/session') {
    return { rlsyncNav: true, depth: 0, tab: 'session', player: null };
  }

  if (rawPath === '/history') {
    return { rlsyncNav: true, depth: 0, tab: 'history', player: null };
  }

  const queryTab = (params.get('tab') || '').toLowerCase();
  if (VALID_TABS.has(queryTab)) {
    const tab = queryTab as DashboardTab;
    const playerParam = params.get('player');
    const nameParam = params.get('name') || undefined;
    return {
      rlsyncNav: true,
      depth: 0,
      tab,
      player: tab === 'players' && playerParam ? { id: playerParam, name: nameParam } : null,
    };
  }

  return { rlsyncNav: true, depth: 0, tab: 'live', player: null };
}
