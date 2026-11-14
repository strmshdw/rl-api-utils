import * as Preview from '../preview';
import type {
  CurrentMatchResponse,
  HistoricalMatchRecord,
  PlayerDetailResponse,
  PlayerMatchDetail,
  PlayerSummary,
  SessionResponse,
} from '../types/api';

export const PREVIEW_STORAGE_KEY = 'rlsync_preview_mode';

export const EMPTY_CURRENT_MATCH: CurrentMatchResponse = {
  active_match: false, match_ended: false, match_guid: '', playlist_id: 0, playlist_name: '',
  teammates: [], opponents: [], spectators: [], updated_at: '',
};

export const EMPTY_SESSION_RESPONSE: SessionResponse = {
  session_id: '', started_at: '', uptime: '', total_matches: 0, total_wins: 0,
  total_losses: 0, win_rate: 0, playlists: {}, matches: [],
};

const ENABLED = typeof __ENABLE_PREVIEW__ !== 'undefined' && __ENABLE_PREVIEW__;

export const PREVIEW_FIXTURE_SENTINEL: string = ENABLED ? Preview.PREVIEW_FIXTURE_SENTINEL : '';

export function getAppVersion(): string {
  try {
    if (typeof __APP_VERSION__ !== 'undefined' && __APP_VERSION__) return __APP_VERSION__;
  } catch { /* ignore */ }
  return '2.0.0';
}

export function getAppBuildMode(): 'production' | 'development' {
  try {
    if (typeof __APP_BUILD_MODE__ !== 'undefined') return __APP_BUILD_MODE__ === 'production' ? 'production' : 'development';
  } catch { /* ignore */ }
  const env = (import.meta as unknown as { env?: { DEV?: boolean; MODE?: string } }).env;
  return env?.DEV || env?.MODE === 'test' || env?.MODE === 'development' ? 'development' : 'production';
}

export function isPreviewBuildAllowed(): boolean {
  try {
    if (typeof __ENABLE_PREVIEW__ !== 'undefined') return Boolean(__ENABLE_PREVIEW__);
  } catch { /* ignore */ }
  const env = (import.meta as unknown as { env?: { DEV?: boolean; MODE?: string } }).env;
  return Boolean(env?.DEV || env?.MODE === 'test' || env?.MODE === 'development');
}

export function isDemoMode(): boolean {
  if (typeof window === 'undefined' || !isPreviewBuildAllowed()) return false;
  try {
    const stored = window.localStorage.getItem(PREVIEW_STORAGE_KEY);
    if (stored === 'off') return false;
    if (stored === 'on') return true;
  } catch { /* ignore */ }
  const params = new URLSearchParams(window.location.search);
  return ['demo', 'test', 'preview'].some((k) => {
    const v = params.get(k);
    return v === 'true' || (k === 'preview' && v === 'idle');
  });
}

export function setPreviewModeEnabled(enabled: boolean): void {
  if (typeof window === 'undefined') return;
  try {
    if (enabled && isPreviewBuildAllowed()) {
      window.localStorage.setItem(PREVIEW_STORAGE_KEY, 'on');
      return;
    }
    window.localStorage.setItem(PREVIEW_STORAGE_KEY, 'off');
    const url = new URL(window.location.href);
    let mutated = false;
    for (const k of ['demo', 'test', 'preview']) {
      if (url.searchParams.has(k)) { url.searchParams.delete(k); mutated = true; }
    }
    if (mutated && window.history?.replaceState) {
      window.history.replaceState({}, '', url.pathname + url.search + url.hash);
    }
  } catch { /* ignore */ }
}

export function isDemoLiveMatch(): boolean {
  return isDemoMode() && new URLSearchParams(window.location.search).get('preview') !== 'idle';
}

export const SAMPLE_CURRENT_MATCH: CurrentMatchResponse = ENABLED ? Preview.SAMPLE_CURRENT_MATCH : EMPTY_CURRENT_MATCH;
export const SAMPLE_SESSION_RESPONSE: SessionResponse = ENABLED ? Preview.SAMPLE_SESSION_RESPONSE : EMPTY_SESSION_RESPONSE;
export const SAMPLE_HISTORICAL_MATCHES: HistoricalMatchRecord[] = ENABLED ? Preview.SAMPLE_HISTORICAL_MATCHES : [];
export const SAMPLE_PLAYERS: PlayerSummary[] = ENABLED ? Preview.SAMPLE_PLAYERS : [];

export function getSamplePlayerProfile(playerId: string, playerName?: string): PlayerDetailResponse {
  if (ENABLED) return Preview.getSamplePlayerProfile(playerId, playerName);
  return {
    player_id: playerId, platform: '', player_name: playerName || '', ranks_json: '{}',
    first_seen_at: '', last_seen_at: '', matchups: [],
  };
}

export function getSamplePlayerMatches(playerId: string, playerName?: string): PlayerMatchDetail[] {
  return ENABLED ? Preview.getSamplePlayerMatches(playerId, playerName) : [];
}
