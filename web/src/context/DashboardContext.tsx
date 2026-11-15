import React, { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import type { StampingConfig } from '../types/api';
import {
  DEFAULT_STAMPING_CONFIG,
  isStampingConfigPayload,
  modeKeyFromPlaylistId,
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
import { ColorMode, FontScale, THEME_STYLES, ThemeStyleKey } from './themeRegistry';

export { THEME_STYLES, buildDateRangeQueryParams, getDateRangeLabel };
export type { ThemeStyleKey, ColorMode, FontScale, DateRangePreset };

export type StatMode = 'avg' | 'total';
export type ReplayLimitPreviewState = 'auto' | 'warn' | 'error' | 'ok';
export type LiveViewMode = 'match' | 'idle';
export type StampingSyncState = 'idle' | 'saving' | 'saved' | 'reset' | 'error';

export interface DashboardContextValue {
  themeStyle: ThemeStyleKey;
  colorMode: ColorMode;
  setTheme: (style: ThemeStyleKey, mode: ColorMode) => void;
  fontScale: FontScale;
  setFontScale: (scale: FontScale) => void;
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

const DEFAULT_CONTEXT: DashboardContextValue = {
  themeStyle: 'broadcast',
  colorMode: 'dark',
  setTheme: () => {},
  fontScale: 'normal',
  setFontScale: () => {},
  statMode: 'avg',
  setStatMode: () => {},
  activePlaylist: 'all',
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

const ACTIVE_PLAYLIST_STORAGE_KEY = 'rlsync_active_playlist';

const DashboardInnerProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { themeStyle, colorMode, setTheme, fontScale, setFontScale } = useTheme();
  const isDevBuild = useMemo(() => isPreviewBuildAllowed(), []);
  const appVersion = useMemo(() => getAppVersion(), []);

  const [previewMode, setPreviewModeState] = useState<boolean>(() => isDemoMode());
  const [statMode, setStatMode] = useState<StatMode>('avg');
  const [activePlaylist, setActivePlaylistState] = useState<string>(
    () => safeStorageGet(ACTIVE_PLAYLIST_STORAGE_KEY) || '11',
  );
  const lastSyncedLivePlaylistRef = useRef<number | null>(null);
  const userOverrodeInCurrentMatchRef = useRef<boolean>(false);
  const [dateRange, setDateRangeState] = useState<DateRangePreset>('all');
  const [customFrom, setCustomFromState] = useState<string>(DEFAULT_CUSTOM_FROM);
  const [customTo, setCustomToState] = useState<string>(DEFAULT_CUSTOM_TO);
  const [showCustomCalendar, setShowCustomCalendar] = useState<boolean>(false);

  const [replayLimitPreview, setReplayLimitPreviewState] = useState<ReplayLimitPreviewState>('auto');
  const [replayBannerDismissed, setReplayBannerDismissedState] = useState<boolean>(
    () => safeStorageGet(STORAGE_KEYS.replayBannerDismissed) === 'true',
  );
  const [liveModeOverride, setLiveModeOverride] = useState<'auto' | 'match' | 'idle'>('auto');
  const [showLiveHistData, setShowLiveHistData] = useState<boolean>(true);
  const [pinnedPlayerIds, setPinnedPlayerIds] = useState<string[]>(() => loadPinnedPlayerIds());
  const [stampingConfig, setStampingConfig] = useState<StampingConfig>(() => loadStampingConfig());
  const [stampingSyncState, setStampingSyncState] = useState<StampingSyncState>('idle');

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

  const setDateRange = useCallback((range: DateRangePreset) => {
    setDateRangeState(range);
    setShowCustomCalendar(range === 'custom');
  }, []);

  const setActivePlaylist = useCallback((playlist: string) => {
    const next = playlist || '11';
    setActivePlaylistState(next);
    safeStorageSet(ACTIVE_PLAYLIST_STORAGE_KEY, next);
    if (lastSyncedLivePlaylistRef.current !== null && lastSyncedLivePlaylistRef.current > 0) {
      userOverrodeInCurrentMatchRef.current = true;
    }
  }, []);

  const syncLivePlaylist = useCallback((playlistId: number | null | undefined) => {
    if (!playlistId || playlistId <= 0) {
      lastSyncedLivePlaylistRef.current = null;
      userOverrodeInCurrentMatchRef.current = false;
      return;
    }
    if (lastSyncedLivePlaylistRef.current !== playlistId) {
      lastSyncedLivePlaylistRef.current = playlistId;
      userOverrodeInCurrentMatchRef.current = false;
      const next = modeKeyFromPlaylistId(playlistId);
      setActivePlaylistState(next);
      safeStorageSet(ACTIVE_PLAYLIST_STORAGE_KEY, next);
    }
  }, []);

  const setCustomFrom = useCallback(
    (date: string) => {
      if (date && customTo && date > customTo) {
        setCustomToState(date);
      }
      setCustomFromState(date);
      setDateRangeState('custom');
    },
    [customTo],
  );

  const setCustomTo = useCallback(
    (date: string) => {
      if (customFrom && date && customFrom > date) {
        setCustomFromState(date);
      }
      setCustomToState(date);
      setDateRangeState('custom');
    },
    [customFrom],
  );

  const setReplayBannerDismissed = useCallback((dismissed: boolean) => {
    setReplayBannerDismissedState(dismissed);
    safeStorageSet(STORAGE_KEYS.replayBannerDismissed, dismissed ? 'true' : 'false');
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

  const dateRangeQueryParams = useMemo(
    () => buildDateRangeQueryParams(dateRange, customFrom, customTo),
    [dateRange, customFrom, customTo],
  );

  const dateRangeLabel = useMemo(
    () => getDateRangeLabel(dateRange, customFrom, customTo),
    [dateRange, customFrom, customTo],
  );

  const value = useMemo<DashboardContextValue>(
    () => ({
      themeStyle,
      colorMode,
      setTheme,
      fontScale,
      setFontScale,
      statMode,
      setStatMode,
      activePlaylist,
      setActivePlaylist,
      syncLivePlaylist,
      dateRange,
      setDateRange,
      customFrom,
      setCustomFrom,
      customTo,
      setCustomTo,
      showCustomCalendar,
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
      statMode,
      activePlaylist,
      setActivePlaylist,
      syncLivePlaylist,
      dateRange,
      setDateRange,
      customFrom,
      setCustomFrom,
      customTo,
      setCustomTo,
      showCustomCalendar,
      dateRangeQueryParams,
      dateRangeLabel,
      replayLimitPreview,
      setReplayLimitPreview,
      replayBannerDismissed,
      setReplayBannerDismissed,
      liveModeOverride,
      showLiveHistData,
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
