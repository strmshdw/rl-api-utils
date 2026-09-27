import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { ColumnConfig, StatColumnKey, PresetKey, PRESETS, STAT_COLUMNS } from '../types/columns';

const STORAGE_KEY = 'rl_sync_column_config';
const EVENT_NAME = 'rl_sync_column_config_change';

export function isValidColumnConfig(obj: unknown): obj is ColumnConfig {
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) {
    return false;
  }
  const record = obj as Record<string, unknown>;
  return STAT_COLUMNS.every(col => typeof record[col.key] === 'boolean');
}

export const normalizeConfig = (parsed: unknown): ColumnConfig | null => {
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null;
  }
  const obj = parsed as Record<string, unknown>;
  const valid = {} as ColumnConfig;
  let hasValidKeys = false;
  for (const col of STAT_COLUMNS) {
    if (typeof obj[col.key] === 'boolean') {
      valid[col.key] = obj[col.key] as boolean;
      hasValidKeys = true;
    } else {
      valid[col.key] = true;
    }
  }
  return hasValidKeys ? valid : null;
};

const createDefaultConfig = (preset: PresetKey = 'full'): ColumnConfig => {
  const allowed = new Set(PRESETS[preset].columns);
  const config = {} as ColumnConfig;
  for (const col of STAT_COLUMNS) {
    config[col.key] = allowed.has(col.key);
  }
  return config;
};

const loadStoredConfig = (): ColumnConfig => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return createDefaultConfig('full');
    const parsed = JSON.parse(raw);
    if (isValidColumnConfig(parsed)) return parsed;
    return normalizeConfig(parsed) ?? createDefaultConfig('full');
  } catch {
    return createDefaultConfig('full');
  }
};

export function useColumnConfig() {
  const [config, setConfig] = useState<ColumnConfig>(loadStoredConfig);
  const configRef = useRef(config);
  configRef.current = config;

  const saveConfig = useCallback((newConfig: ColumnConfig) => {
    configRef.current = newConfig;
    setConfig(newConfig);
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(newConfig));
      window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail: newConfig }));
    } catch (e) {
      console.warn('Failed to persist column config to localStorage', e);
    }
  }, []);

  const toggleColumn = useCallback((key: StatColumnKey) => {
    const next = { ...configRef.current, [key]: !configRef.current[key] };
    saveConfig(next);
  }, [saveConfig]);

  const setColumn = useCallback((key: StatColumnKey, visible: boolean) => {
    if (configRef.current[key] === visible) return;
    const next = { ...configRef.current, [key]: visible };
    saveConfig(next);
  }, [saveConfig]);

  const applyPreset = useCallback((presetKey: PresetKey) => {
    const next = createDefaultConfig(presetKey);
    saveConfig(next);
  }, [saveConfig]);

  const resetToDefault = useCallback(() => {
    applyPreset('full');
  }, [applyPreset]);

  // Synchronize across tabs and custom events
  useEffect(() => {
    const handleStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY && e.newValue) {
        try {
          const parsed = JSON.parse(e.newValue);
          if (isValidColumnConfig(parsed)) {
            configRef.current = parsed;
            setConfig(parsed);
          } else {
            const normalized = normalizeConfig(parsed);
            if (normalized) {
              configRef.current = normalized;
              setConfig(normalized);
            }
          }
        } catch {
          // ignore parsing error
        }
      }
    };

    const handleCustomChange = (e: Event) => {
      const customEvent = e as CustomEvent<ColumnConfig>;
      if (customEvent.detail && typeof customEvent.detail === 'object') {
        if (customEvent.detail === configRef.current) return;
        if (isValidColumnConfig(customEvent.detail)) {
          configRef.current = customEvent.detail;
          setConfig(customEvent.detail);
        } else {
          const normalized = normalizeConfig(customEvent.detail);
          if (normalized) {
            configRef.current = normalized;
            setConfig(normalized);
          }
        }
      }
    };

    window.addEventListener('storage', handleStorage);
    window.addEventListener(EVENT_NAME, handleCustomChange);

    return () => {
      window.removeEventListener('storage', handleStorage);
      window.removeEventListener(EVENT_NAME, handleCustomChange);
    };
  }, []);

  // Compute active preset match (if any)
  const activePreset = useMemo<PresetKey | 'custom'>(() => {
    for (const [key, preset] of Object.entries(PRESETS)) {
      const presetCols = new Set(preset.columns);
      const isMatch = STAT_COLUMNS.every(col => config[col.key] === presetCols.has(col.key));
      if (isMatch) return key as PresetKey;
    }
    return 'custom';
  }, [config]);

  const isColumnVisible = useCallback((key: StatColumnKey) => {
    return !!config[key];
  }, [config]);

  const visibleCount = useMemo(() => {
    return Object.values(config).filter(Boolean).length;
  }, [config]);

  return {
    config,
    toggleColumn,
    setColumn,
    applyPreset,
    resetToDefault,
    isColumnVisible,
    activePreset,
    visibleCount,
  };
}
