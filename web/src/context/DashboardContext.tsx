import React, { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import type { StampingConfig } from '../types/api';
import {
  DEFAULT_STAMPING_CONFIG,
  isStampingConfigPayload,
  modeKeyFromPlaylistId,
  normalizeActiveModeKey,
  normalizeStampingConfig,
} from '../utils/broadcast';
import {
  getAppVersion,
  isDemoMode,
  isPreviewBuildAllowed,
  setPreviewModeEnabled,
} from '../utils/sampleData';
import { ThemeProvider, useTheme } from './ThemeContext';
import {
  DateRangePreset,
  DEFAULT_CUSTOM_FROM,
  DEFAULT_CUSTOM_TO,
  buildDateRangeQueryParams,
  getDateRangeLabel,
} from './dateRange';
import {
  STORAGE_KEYS,
  loadPinnedPlayerIds,
  loadStampingConfig,
  safeStorageGet,
  safeStorageSet,
} from './storage';
import { ColorMode, FontScale, PlatformDisplayMode, THEME_STYLES, ThemeStyleKey } from './themeRegistry';

export { THEME_STYLES, buildDateRangeQueryParams, getDateRangeLabel };
export type { ThemeStyleKey, ColorMode, FontScale, PlatformDisplayMode, DateRangePreset };

export type StatMode = 'avg' | 'total';
export type FilterArea = 'live' | 'session' | 'history' | 'players';
export type ReplayLimitPreviewState = 'auto' | 'warn' | 'error' | 'ok';
export type LiveViewMode = 'match' | 'idle';
export type StampingSyncState = 'idle' | 'saving' | 'saved' | 'reset' | 'error';

export interface AreaFilterState {
  playlist: string;
  dateRange: DateRangePreset;
  customFrom: string;
  customTo: string;
  showCustomCalendar: boolean;
  statMode: StatMode;
}

export interface AreaFilterControls extends AreaFilterState {
  dateRangeQueryParams: { from?: string; to?: string };
  dateRangeLabel: string;
  setPlaylist: (playlist: string) => void;
  setDateRange: (range: DateRangePreset) => void;
  setCustomFrom: (date: string) => void;
  setCustomTo: (date: string) => void;
  setShowCustomCalendar: (open: boolean | ((prev: boolean) => boolean)) => void;
  setStatMode: (mode: StatMode) => void;
  resetFilters: () => void;
}

const ACTIVE_PLAYLIST_STORAGE_KEY = 'rlsync_active_playlist';
const AREA_PLAYLIST_KEY_PREFIX = 'rlsync_area_playlist_';
const ALL_FILTER_AREAS: readonly FilterArea[] = ['live', 'session', 'history', 'players'];

export function createDefaultAreaFilterState(playlist = '11'): AreaFilterState {
  return {
    playlist: normalizeActiveModeKey(playlist),
    dateRange: 'all',
    customFrom: DEFAULT_CUSTOM_FROM,
    customTo: DEFAULT_CUSTOM_TO,
    showCustomCalendar: false,
    statMode: 'avg',
  };
}

function loadAreaPlaylist(area: FilterArea): string {
  const areaStored = safeStorageGet(`${AREA_PLAYLIST_KEY_PREFIX}${area}`);
  if (areaStored) return normalizeActiveModeKey(areaStored);
  const legacyStored = safeStorageGet(ACTIVE_PLAYLIST_STORAGE_KEY);
  return normalizeActiveModeKey(legacyStored);
}

function buildInitialAreaFilters(): Record<FilterArea, AreaFilterState> {
  return {
    live: createDefaultAreaFilterState(loadAreaPlaylist('live')),
    session: createDefaultAreaFilterState(loadAreaPlaylist('session')),
    history: createDefaultAreaFilterState(loadAreaPlaylist('history')),
    players: createDefaultAreaFilterState(loadAreaPlaylist('players')),
  };
}

export interface DashboardContextValue {
  isProvided: boolean;
  themeStyle: ThemeStyleKey;
  colorMode: ColorMode;
  setTheme: (style: ThemeStyleKey, mode: ColorMode) => void;
  fontScale: FontScale;
  setFontScale: (scale: FontScale) => void;
  platformDisplay: PlatformDisplayMode;
  setPlatformDisplay: (mode: PlatformDisplayMode) => void;
  areaFilters: Record<FilterArea, AreaFilterState>;
  setAreaPlaylist: (area: FilterArea, playlist: string) => void;
  setAreaDateRange: (area: FilterArea, range: DateRangePreset) => void;
  setAreaCustomFrom: (area: FilterArea, date: string) => void;
  setAreaCustomTo: (area: FilterArea, date: string) => void;
  setAreaShowCustomCalendar: (area: FilterArea, open: boolean | ((prev: boolean) => boolean)) => void;
  setAreaStatMode: (area: FilterArea, mode: StatMode) => void;
  resetAreaFilters: (area: FilterArea) => void;
  statMode: StatMode;
  setStatMode: (mode: StatMode) => void;
  activePlaylist: string;
  setActivePlaylist: (playlist: string) => void;
  syncLivePlaylist: (playlistId: number | null | undefined) => void;
  dateRange: DateRangePreset;
  setDateRange: (range: DateRangePreset) => void;
  customFrom: string;
  setCustomFrom: (date: string) => void;
  customTo: string;
  setCustomTo: (date: string) => void;
  showCustomCalendar: boolean;
  setShowCustomCalendar: (open: boolean | ((prev: boolean) => boolean)) => void;
  dateRangeQueryParams: { from?: string; to?: string };
  dateRangeLabel: string;
  replayLimitPreview: ReplayLimitPreviewState;
  setReplayLimitPreview: (state: ReplayLimitPreviewState) => void;
  replayBannerDismissed: boolean;
  setReplayBannerDismissed: (dismissed: boolean) => void;
  liveModeOverride: 'auto' | 'match' | 'idle';
  setLiveModeOverride: (mode: 'auto' | 'match' | 'idle') => void;
  showLiveHistData: boolean;
  setShowLiveHistData: (show: boolean | ((prev: boolean) => boolean)) => void;
  pinnedPlayerIds: string[];
  togglePinPlayer: (playerId: string) => void;
  isPlayerPinned: (playerId: string) => boolean;
  stampingConfig: StampingConfig;
  stampingSyncState: StampingSyncState;
  refreshStampingConfig: () => Promise<void>;
  updateStampingConfig: (patch: Partial<StampingConfig>) => Promise<void>;
  resetStampingConfig: () => Promise<void>;
  isDevBuild: boolean;
  appVersion: string;
  previewMode: boolean;
  setPreviewMode: (enabled: boolean) => void;
}

const DEFAULT_AREA_MAP: Record<FilterArea, AreaFilterState> = {
  live: createDefaultAreaFilterState('11'),
  session: createDefaultAreaFilterState('11'),
  history: createDefaultAreaFilterState('11'),
  players: createDefaultAreaFilterState('11'),
};

const DEFAULT_CONTEXT: DashboardContextValue = {
  isProvided: false,
  themeStyle: 'broadcast',
  colorMode: 'dark',
  setTheme: () => {},
  fontScale: 'normal',
  setFontScale: () => {},
  platformDisplay: 'icon',
  setPlatformDisplay: () => {},
  areaFilters: DEFAULT_AREA_MAP,
  setAreaPlaylist: () => {},
  setAreaDateRange: () => {},
  setAreaCustomFrom: () => {},
  setAreaCustomTo: () => {},
  setAreaShowCustomCalendar: () => {},
  setAreaStatMode: () => {},
  resetAreaFilters: () => {},
  statMode: 'avg',
  setStatMode: () => {},
  activePlaylist: '11',
  setActivePlaylist: () => {},
  syncLivePlaylist: () => {},
  dateRange: 'all',
  setDateRange: () => {},
  customFrom: DEFAULT_CUSTOM_FROM,
  setCustomFrom: () => {},
  customTo: DEFAULT_CUSTOM_TO,
  setCustomTo: () => {},
  showCustomCalendar: false,
  setShowCustomCalendar: () => {},
  dateRangeQueryParams: {},
  dateRangeLabel: 'ALL TIME',
  replayLimitPreview: 'auto',
  setReplayLimitPreview: () => {},
  replayBannerDismissed: false,
  setReplayBannerDismissed: () => {},
  liveModeOverride: 'auto',
  setLiveModeOverride: () => {},
  showLiveHistData: true,
  setShowLiveHistData: () => {},
  pinnedPlayerIds: [],
  togglePinPlayer: () => {},
  isPlayerPinned: () => false,
  stampingConfig: DEFAULT_STAMPING_CONFIG,
  stampingSyncState: 'idle',
  refreshStampingConfig: async () => {},
  updateStampingConfig: async () => {},
  resetStampingConfig: async () => {},
  isDevBuild: false,
  appVersion: '2.0.0',
  previewMode: false,
  setPreviewMode: () => {},
};

const DashboardContext = createContext<DashboardContextValue>(DEFAULT_CONTEXT);

const DashboardInnerProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const {
    themeStyle,
    colorMode,
    setTheme,
    fontScale,
    setFontScale,
    platformDisplay,
    setPlatformDisplay,
  } = useTheme();
  const isDevBuild = useMemo(() => isPreviewBuildAllowed(), []);
  const appVersion = useMemo(() => getAppVersion(), []);

  const [previewMode, setPreviewModeState] = useState<boolean>(() => isDemoMode());
  const [areaFilters, setAreaFilters] = useState<Record<FilterArea, AreaFilterState>>(
    () => buildInitialAreaFilters(),
  );
  const lastSyncedLivePlaylistRef = useRef<number | null>(null);
  const userOverrodeInCurrentMatchRef = useRef<boolean>(false);

  const [replayLimitPreview, setReplayLimitPreviewState] = useState<ReplayLimitPreviewState>('auto');
  const [replayBannerDismissed, setReplayBannerDismissedState] = useState<boolean>(
    () => safeStorageGet(STORAGE_KEYS.replayBannerDismissed) === 'true',
  );
  const [liveModeOverride, setLiveModeOverride] = useState<'auto' | 'match' | 'idle'>('auto');
  const [showLiveHistData, setShowLiveHistDataState] = useState<boolean>(
    () => safeStorageGet(STORAGE_KEYS.showLiveHistData) !== 'false',
  );
  const [pinnedPlayerIds, setPinnedPlayerIds] = useState<string[]>(() => loadPinnedPlayerIds());
  const [stampingConfig, setStampingConfig] = useState<StampingConfig>(() => loadStampingConfig());
  const [stampingSyncState, setStampingSyncState] = useState<StampingSyncState>('idle');

  const updateArea = useCallback(
    (area: FilterArea, updater: (prev: AreaFilterState) => AreaFilterState) => {
      setAreaFilters((prev) => ({
        ...prev,
        [area]: updater(prev[area]),
      }));
    },
    [],
  );

  const setAreaPlaylist = useCallback(
    (area: FilterArea, playlist: string) => {
      const next = normalizeActiveModeKey(playlist);
      updateArea(area, (prev) => ({ ...prev, playlist: next }));
      safeStorageSet(`${AREA_PLAYLIST_KEY_PREFIX}${area}`, next);
      if (area === 'live') {
        safeStorageSet(ACTIVE_PLAYLIST_STORAGE_KEY, next);
        if (lastSyncedLivePlaylistRef.current !== null && lastSyncedLivePlaylistRef.current > 0) {
          userOverrodeInCurrentMatchRef.current = true;
        }
      }
    },
    [updateArea],
  );

  const setAreaDateRange = useCallback(
    (area: FilterArea, range: DateRangePreset) => {
      updateArea(area, (prev) => ({
        ...prev,
        dateRange: range,
        showCustomCalendar: range === 'custom',
      }));
    },
    [updateArea],
  );

  const setAreaCustomFrom = useCallback(
    (area: FilterArea, date: string) => {
      updateArea(area, (prev) => ({
        ...prev,
        customFrom: date,
        customTo: date && prev.customTo && date > prev.customTo ? date : prev.customTo,
        dateRange: 'custom',
      }));
    },
    [updateArea],
  );

  const setAreaCustomTo = useCallback(
    (area: FilterArea, date: string) => {
      updateArea(area, (prev) => ({
        ...prev,
        customFrom: prev.customFrom && date && prev.customFrom > date ? date : prev.customFrom,
        customTo: date,
        dateRange: 'custom',
      }));
    },
    [updateArea],
  );

  const setAreaShowCustomCalendar = useCallback(
    (area: FilterArea, open: boolean | ((prev: boolean) => boolean)) => {
      updateArea(area, (prev) => ({
        ...prev,
        showCustomCalendar: typeof open === 'function' ? open(prev.showCustomCalendar) : open,
      }));
    },
    [updateArea],
  );

  const setAreaStatMode = useCallback(
    (area: FilterArea, mode: StatMode) => {
      updateArea(area, (prev) => ({ ...prev, statMode: mode }));
    },
    [updateArea],
  );

  const resetAreaFilters = useCallback(
    (area: FilterArea) => {
      updateArea(area, () => createDefaultAreaFilterState('11'));
      safeStorageSet(`${AREA_PLAYLIST_KEY_PREFIX}${area}`, '11');
      if (area === 'live') {
        safeStorageSet(ACTIVE_PLAYLIST_STORAGE_KEY, '11');
      }
    },
    [updateArea],
  );

  const syncLivePlaylist = useCallback(
    (playlistId: number | null | undefined) => {
      if (!playlistId || playlistId <= 0) {
        lastSyncedLivePlaylistRef.current = null;
        userOverrodeInCurrentMatchRef.current = false;
        return;
      }
      if (lastSyncedLivePlaylistRef.current !== playlistId) {
        lastSyncedLivePlaylistRef.current = playlistId;
        userOverrodeInCurrentMatchRef.current = false;
        const next = modeKeyFromPlaylistId(playlistId);
        updateArea('live', (prev) => ({ ...prev, playlist: next }));
        safeStorageSet(`${AREA_PLAYLIST_KEY_PREFIX}live`, next);
        safeStorageSet(ACTIVE_PLAYLIST_STORAGE_KEY, next);
      }
    },
    [updateArea],
  );

  // Legacy convenience aliases operate on the live area (while keeping per-area isolation intact)
  const setActivePlaylist = useCallback(
    (playlist: string) => {
      setAreaPlaylist('live', playlist);
    },
    [setAreaPlaylist],
  );

  const setStatMode = useCallback(
    (mode: StatMode) => {
      setAreaStatMode('live', mode);
    },
    [setAreaStatMode],
  );

  const setDateRange = useCallback(
    (range: DateRangePreset) => {
      setAreaDateRange('live', range);
    },
    [setAreaDateRange],
  );

  const setCustomFrom = useCallback(
    (date: string) => {
      setAreaCustomFrom('live', date);
    },
    [setAreaCustomFrom],
  );

  const setCustomTo = useCallback(
    (date: string) => {
      setAreaCustomTo('live', date);
    },
    [setAreaCustomTo],
  );

  const setShowCustomCalendar = useCallback(
    (open: boolean | ((prev: boolean) => boolean)) => {
      setAreaShowCustomCalendar('live', open);
    },
    [setAreaShowCustomCalendar],
  );

  const refreshStampingConfig = useCallback(async () => {
    try {
      const res = await fetch('/api/stamping/config');
      if (!res.ok) return;
      const data: unknown = await res.json();
      if (isStampingConfigPayload(data)) {
        const normalized = normalizeStampingConfig(data);
        setStampingConfig(normalized);
        safeStorageSet(STORAGE_KEYS.stampingConfig, JSON.stringify(normalized));
      }
    } catch {
      // Offline / static demo mode falls back to localStorage or defaults.
    }
  }, []);

  const updateStampingConfig = useCallback(async (patch: Partial<StampingConfig>) => {
    setStampingSyncState('saving');
    setStampingConfig((prev) => {
      const merged = normalizeStampingConfig({ ...prev, ...patch });
      safeStorageSet(STORAGE_KEYS.stampingConfig, JSON.stringify(merged));
      return merged;
    });
    try {
      const res = await fetch('/api/stamping/config', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      });
      if (res.ok) {
        const data: unknown = await res.json();
        if (isStampingConfigPayload(data)) {
          const normalized = normalizeStampingConfig(data);
          setStampingConfig(normalized);
          safeStorageSet(STORAGE_KEYS.stampingConfig, JSON.stringify(normalized));
        }
        setStampingSyncState('saved');
      } else if (res.status === 404 && isDemoMode()) {
        setStampingSyncState('saved');
      } else {
        setStampingSyncState('error');
      }
    } catch {
      setStampingSyncState(isDemoMode() ? 'saved' : 'error');
    }
  }, []);

  const resetStampingConfig = useCallback(async () => {
    setStampingSyncState('saving');
    setStampingConfig(DEFAULT_STAMPING_CONFIG);
    safeStorageSet(STORAGE_KEYS.stampingConfig, JSON.stringify(DEFAULT_STAMPING_CONFIG));
    try {
      const res = await fetch('/api/stamping/config/reset', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}',
      });
      if (res.ok) {
        const data: unknown = await res.json();
        if (isStampingConfigPayload(data)) {
          const normalized = normalizeStampingConfig(data);
          setStampingConfig(normalized);
          safeStorageSet(STORAGE_KEYS.stampingConfig, JSON.stringify(normalized));
        }
        setStampingSyncState('reset');
      } else if (res.status === 404 && isDemoMode()) {
        setStampingSyncState('reset');
      } else {
        setStampingSyncState('error');
      }
    } catch {
      setStampingSyncState(isDemoMode() ? 'reset' : 'error');
    }
  }, []);

  const setReplayBannerDismissed = useCallback((dismissed: boolean) => {
    setReplayBannerDismissedState(dismissed);
    safeStorageSet(STORAGE_KEYS.replayBannerDismissed, dismissed ? 'true' : 'false');
  }, []);

  const setShowLiveHistData = useCallback((show: boolean | ((prev: boolean) => boolean)) => {
    setShowLiveHistDataState((prev) => {
      const next = typeof show === 'function' ? show(prev) : show;
      safeStorageSet(STORAGE_KEYS.showLiveHistData, next ? 'true' : 'false');
      return next;
    });
  }, []);

  const setReplayLimitPreview = useCallback(
    (state: ReplayLimitPreviewState) => {
      setReplayLimitPreviewState(state);
      if (state !== 'auto') {
        setReplayBannerDismissed(false);
      }
    },
    [setReplayBannerDismissed],
  );

  const setPreviewMode = useCallback((enabled: boolean) => {
    const next = Boolean(enabled && isPreviewBuildAllowed());
    setPreviewModeEnabled(next);
    setPreviewModeState(next);
    if (!next) {
      setLiveModeOverride('auto');
      setReplayLimitPreviewState('auto');
    }
  }, []);

  const togglePinPlayer = useCallback((playerId: string) => {
    setPinnedPlayerIds((prev) => {
      const exists = prev.includes(playerId);
      const next = exists
        ? prev.filter((id) => id !== playerId)
        : prev.length < 3
          ? [...prev, playerId]
          : [...prev.slice(1), playerId];
      safeStorageSet(STORAGE_KEYS.pinnedPlayers, JSON.stringify(next));
      return next;
    });
  }, []);

  const isPlayerPinned = useCallback(
    (playerId: string) => pinnedPlayerIds.includes(playerId),
    [pinnedPlayerIds],
  );

  const liveArea = areaFilters.live;
  const dateRangeQueryParams = useMemo(
    () => buildDateRangeQueryParams(liveArea.dateRange, liveArea.customFrom, liveArea.customTo),
    [liveArea.dateRange, liveArea.customFrom, liveArea.customTo],
  );

  const dateRangeLabel = useMemo(
    () => getDateRangeLabel(liveArea.dateRange, liveArea.customFrom, liveArea.customTo),
    [liveArea.dateRange, liveArea.customFrom, liveArea.customTo],
  );

  const value = useMemo<DashboardContextValue>(
    () => ({
      isProvided: true,
      themeStyle,
      colorMode,
      setTheme,
      fontScale,
      setFontScale,
      platformDisplay,
      setPlatformDisplay,
      areaFilters,
      setAreaPlaylist,
      setAreaDateRange,
      setAreaCustomFrom,
      setAreaCustomTo,
      setAreaShowCustomCalendar,
      setAreaStatMode,
      resetAreaFilters,
      statMode: liveArea.statMode,
      setStatMode,
      activePlaylist: liveArea.playlist,
      setActivePlaylist,
      syncLivePlaylist,
      dateRange: liveArea.dateRange,
      setDateRange,
      customFrom: liveArea.customFrom,
      setCustomFrom,
      customTo: liveArea.customTo,
      setCustomTo,
      showCustomCalendar: liveArea.showCustomCalendar,
      setShowCustomCalendar,
      dateRangeQueryParams,
      dateRangeLabel,
      replayLimitPreview,
      setReplayLimitPreview,
      replayBannerDismissed,
      setReplayBannerDismissed,
      liveModeOverride,
      setLiveModeOverride,
      showLiveHistData,
      setShowLiveHistData,
      pinnedPlayerIds,
      togglePinPlayer,
      isPlayerPinned,
      stampingConfig,
      stampingSyncState,
      refreshStampingConfig,
      updateStampingConfig,
      resetStampingConfig,
      isDevBuild,
      appVersion,
      previewMode,
      setPreviewMode,
    }),
    [
      themeStyle,
      colorMode,
      setTheme,
      fontScale,
      setFontScale,
      platformDisplay,
      setPlatformDisplay,
      areaFilters,
      setAreaPlaylist,
      setAreaDateRange,
      setAreaCustomFrom,
      setAreaCustomTo,
      setAreaShowCustomCalendar,
      setAreaStatMode,
      resetAreaFilters,
      liveArea.statMode,
      liveArea.playlist,
      liveArea.dateRange,
      liveArea.customFrom,
      liveArea.customTo,
      liveArea.showCustomCalendar,
      setStatMode,
      setActivePlaylist,
      syncLivePlaylist,
      setDateRange,
      setCustomFrom,
      setCustomTo,
      setShowCustomCalendar,
      dateRangeQueryParams,
      dateRangeLabel,
      replayLimitPreview,
      setReplayLimitPreview,
      replayBannerDismissed,
      setReplayBannerDismissed,
      liveModeOverride,
      showLiveHistData,
      setShowLiveHistData,
      pinnedPlayerIds,
      togglePinPlayer,
      isPlayerPinned,
      stampingConfig,
      stampingSyncState,
      refreshStampingConfig,
      updateStampingConfig,
      resetStampingConfig,
      isDevBuild,
      appVersion,
      previewMode,
      setPreviewMode,
    ],
  );

  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>;
};

export const DashboardProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <ThemeProvider>
    <DashboardInnerProvider>{children}</DashboardInnerProvider>
  </ThemeProvider>
);

export function useDashboard(): DashboardContextValue {
  return useContext(DashboardContext);
}

/**
 * Per-area independent filter hook.
 * Persists independent filter state per dashboard area when mounted under
 * DashboardProvider, and seamlessly falls back to local component state when
 * a view is rendered standalone (e.g., isolated unit tests).
 */
export function useAreaFilters(area: FilterArea): AreaFilterControls {
  const ctx = useContext(DashboardContext);
  const [localState, setLocalState] = useState<AreaFilterState>(() =>
    createDefaultAreaFilterState('11'),
  );

  const state = ctx.isProvided ? ctx.areaFilters[area] : localState;

  const setPlaylist = useCallback(
    (playlist: string) => {
      const next = normalizeActiveModeKey(playlist);
      if (ctx.isProvided) ctx.setAreaPlaylist(area, next);
      else setLocalState((prev) => ({ ...prev, playlist: next }));
    },
    [ctx, area],
  );

  const setDateRange = useCallback(
    (range: DateRangePreset) => {
      if (ctx.isProvided) ctx.setAreaDateRange(area, range);
      else setLocalState((prev) => ({ ...prev, dateRange: range, showCustomCalendar: range === 'custom' }));
    },
    [ctx, area],
  );

  const setCustomFrom = useCallback(
    (date: string) => {
      if (ctx.isProvided) ctx.setAreaCustomFrom(area, date);
      else {
        setLocalState((prev) => ({
          ...prev,
          customFrom: date,
          customTo: date && prev.customTo && date > prev.customTo ? date : prev.customTo,
          dateRange: 'custom',
        }));
      }
    },
    [ctx, area],
  );

  const setCustomTo = useCallback(
    (date: string) => {
      if (ctx.isProvided) ctx.setAreaCustomTo(area, date);
      else {
        setLocalState((prev) => ({
          ...prev,
          customFrom: prev.customFrom && date && prev.customFrom > date ? date : prev.customFrom,
          customTo: date,
          dateRange: 'custom',
        }));
      }
    },
    [ctx, area],
  );

  const setShowCustomCalendar = useCallback(
    (open: boolean | ((prev: boolean) => boolean)) => {
      if (ctx.isProvided) ctx.setAreaShowCustomCalendar(area, open);
      else {
        setLocalState((prev) => ({
          ...prev,
          showCustomCalendar: typeof open === 'function' ? open(prev.showCustomCalendar) : open,
        }));
      }
    },
    [ctx, area],
  );

  const setStatMode = useCallback(
    (mode: StatMode) => {
      if (ctx.isProvided) ctx.setAreaStatMode(area, mode);
      else setLocalState((prev) => ({ ...prev, statMode: mode }));
    },
    [ctx, area],
  );

  const resetFilters = useCallback(() => {
    if (ctx.isProvided) ctx.resetAreaFilters(area);
    else setLocalState(createDefaultAreaFilterState('11'));
  }, [ctx, area]);

  const dateRangeQueryParams = useMemo(
    () => buildDateRangeQueryParams(state.dateRange, state.customFrom, state.customTo),
    [state.dateRange, state.customFrom, state.customTo],
  );

  const dateRangeLabel = useMemo(
    () => getDateRangeLabel(state.dateRange, state.customFrom, state.customTo),
    [state.dateRange, state.customFrom, state.customTo],
  );

  return useMemo<AreaFilterControls>(
    () => ({
      ...state,
      dateRangeQueryParams,
      dateRangeLabel,
      setPlaylist,
      setDateRange,
      setCustomFrom,
      setCustomTo,
      setShowCustomCalendar,
      setStatMode,
      resetFilters,
    }),
    [
      state,
      dateRangeQueryParams,
      dateRangeLabel,
      setPlaylist,
      setDateRange,
      setCustomFrom,
      setCustomTo,
      setShowCustomCalendar,
      setStatMode,
      resetFilters,
    ],
  );
}

export { ALL_FILTER_AREAS };
