import {
  DEFAULT_STAMPING_CONFIG,
  normalizeStampingConfig,
  type StampingConfig,
} from '../utils/stampingConfig';

export const STORAGE_KEYS = {
  themeStyle: 'rlsync_theme_style',
  colorMode: 'rlsync_color_mode',
  fontScale: 'rlsync_font_scale',
  platformDisplay: 'rlsync_platform_display',
  replayBannerDismissed: 'rlsync_replay_banner_dismissed',
  pinnedPlayers: 'rlsync_pinned_players',
  stampingConfig: 'rlsync_stamping_config',
} as const;

export function safeStorageGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function safeStorageSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // ignore storage errors in restricted environments
  }
}

export function loadPinnedPlayerIds(): string[] {
  const saved = safeStorageGet(STORAGE_KEYS.pinnedPlayers);
  if (!saved) return [];
  try {
    const parsed = JSON.parse(saved);
    if (Array.isArray(parsed)) {
      return parsed.filter((x): x is string => typeof x === 'string').slice(0, 3);
    }
  } catch {
    // ignore invalid json
  }
  return [];
}

export function loadStampingConfig(): StampingConfig {
  const saved = safeStorageGet(STORAGE_KEYS.stampingConfig);
  if (!saved) return DEFAULT_STAMPING_CONFIG;
  try {
    return normalizeStampingConfig(JSON.parse(saved));
  } catch {
    return DEFAULT_STAMPING_CONFIG;
  }
}
