// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { useColumnConfig } from './useColumnConfig';
import { STAT_COLUMNS, PRESETS, StatColumnKey } from '../types/columns';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const STORAGE_KEY = 'rl_sync_column_config';
const EVENT_NAME = 'rl_sync_column_config_change';

describe('useColumnConfig Stress & Robustness Suite', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    localStorage.clear();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  function renderHook<T>(hookFn: () => T) {
    const result = { current: null as unknown as T };
    function TestComponent() {
      result.current = hookFn();
      return null;
    }
    act(() => {
      root.render(<TestComponent />);
    });
    return result;
  }

  describe('1. Corrupted localStorage Handling', () => {
    it('gracefully recovers when localStorage contains syntax error JSON', () => {
      localStorage.setItem(STORAGE_KEY, '{invalid json syntax:;;;');
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('gracefully recovers when localStorage contains primitive string', () => {
      localStorage.setItem(STORAGE_KEY, '"hello world"');
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('gracefully recovers when localStorage contains a number', () => {
      localStorage.setItem(STORAGE_KEY, '42');
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('gracefully recovers when localStorage contains JSON array', () => {
      localStorage.setItem(STORAGE_KEY, '[1, 2, 3]');
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('gracefully recovers when localStorage contains string "null"', () => {
      localStorage.setItem(STORAGE_KEY, 'null');
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('fills missing columns with default true when partial JSON object is stored', () => {
      // Only 2 columns defined, 8 missing
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          score: true,
          goals: false,
        })
      );
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.isColumnVisible('score')).toBe(true);
      expect(hook.current.isColumnVisible('goals')).toBe(false);
      // Missing fields default to true
      expect(hook.current.isColumnVisible('assists')).toBe(true);
      expect(hook.current.isColumnVisible('saves')).toBe(true);
      expect(hook.current.isColumnVisible('shots')).toBe(true);
      expect(hook.current.isColumnVisible('demos')).toBe(true);
      expect(hook.current.isColumnVisible('mmr')).toBe(true);
      expect(hook.current.isColumnVisible('rank')).toBe(true);
      expect(hook.current.isColumnVisible('h2h')).toBe(true);
      expect(hook.current.isColumnVisible('platform')).toBe(true);
      expect(hook.current.visibleCount).toBe(9);
      expect(hook.current.activePreset).toBe('custom');
    });

    it('defaults invalid field types to true when non-booleans are provided', () => {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          score: 'yes',
          goals: 123,
          assists: null,
          saves: false, // only this is valid boolean false
        })
      );
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.isColumnVisible('score')).toBe(true);
      expect(hook.current.isColumnVisible('goals')).toBe(true);
      expect(hook.current.isColumnVisible('assists')).toBe(true);
      expect(hook.current.isColumnVisible('saves')).toBe(false);
      expect(hook.current.visibleCount).toBe(9);
    });

    it('preserves an intentional all-false configuration', () => {
      const allFalse: Record<string, boolean> = {};
      for (const col of STAT_COLUMNS) {
        allFalse[col.key] = false;
      }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(allFalse));
      const hook = renderHook(() => useColumnConfig());

      expect(hook.current.visibleCount).toBe(0);
      expect(hook.current.activePreset).toBe('custom');
      for (const col of STAT_COLUMNS) {
        expect(hook.current.isColumnVisible(col.key)).toBe(false);
      }
    });

    it('handles localStorage.getItem throwing SecurityError / DOMException', () => {
      vi.spyOn(localStorage, 'getItem').mockImplementation(() => {
        throw new DOMException('Access denied', 'SecurityError');
      });

      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('handles localStorage.setItem throwing QuotaExceededError without crashing component', () => {
      const consoleWarnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      vi.spyOn(localStorage, 'setItem').mockImplementation(() => {
        throw new DOMException('Quota exceeded', 'QuotaExceededError');
      });

      const hook = renderHook(() => useColumnConfig());

      act(() => {
        hook.current.toggleColumn('score');
      });

      expect(hook.current.isColumnVisible('score')).toBe(false);
      expect(consoleWarnSpy).toHaveBeenCalledWith(
        'Failed to persist column config to localStorage',
        expect.any(DOMException)
      );
    });
  });

  describe('2. Rapid Toggling & Preset Switches', () => {
    it('handles 50 rapid toggles on the same column consistently', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.isColumnVisible('score')).toBe(true);

      for (let i = 0; i < 50; i++) {
        act(() => {
          hook.current.toggleColumn('score');
        });
      }

      // After 50 toggles (even number), should be back to true
      expect(hook.current.isColumnVisible('score')).toBe(true);
    });

    it('handles 51 rapid toggles resulting in inverted state', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.isColumnVisible('score')).toBe(true);

      for (let i = 0; i < 51; i++) {
        act(() => {
          hook.current.toggleColumn('score');
        });
      }

      // After 51 toggles (odd number), should be false
      expect(hook.current.isColumnVisible('score')).toBe(false);
    });

    it('switches between presets and updates activePreset correctly', () => {
      const hook = renderHook(() => useColumnConfig());

      // Initial: full preset (10 columns)
      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);

      // Switch to competitive preset (5 columns)
      act(() => {
        hook.current.applyPreset('competitive');
      });
      expect(hook.current.activePreset).toBe('competitive');
      expect(hook.current.visibleCount).toBe(5);
      expect(hook.current.isColumnVisible('score')).toBe(true);
      expect(hook.current.isColumnVisible('goals')).toBe(true);
      expect(hook.current.isColumnVisible('mmr')).toBe(true);
      expect(hook.current.isColumnVisible('rank')).toBe(true);
      expect(hook.current.isColumnVisible('h2h')).toBe(true);
      expect(hook.current.isColumnVisible('assists')).toBe(false);

      // Switch to streamer preset (4 columns)
      act(() => {
        hook.current.applyPreset('streamer');
      });
      expect(hook.current.activePreset).toBe('streamer');
      expect(hook.current.visibleCount).toBe(4);
      expect(hook.current.isColumnVisible('score')).toBe(true);
      expect(hook.current.isColumnVisible('goals')).toBe(true);
      expect(hook.current.isColumnVisible('assists')).toBe(true);
      expect(hook.current.isColumnVisible('saves')).toBe(true);
      expect(hook.current.isColumnVisible('mmr')).toBe(false);

      // Switch back to full
      act(() => {
        hook.current.applyPreset('full');
      });
      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('transitions to custom preset when a column is toggled off a preset', () => {
      const hook = renderHook(() => useColumnConfig());

      act(() => {
        hook.current.applyPreset('competitive');
      });
      expect(hook.current.activePreset).toBe('competitive');

      // Toggle off 'mmr' from competitive preset
      act(() => {
        hook.current.toggleColumn('mmr');
      });
      expect(hook.current.activePreset).toBe('custom');
      expect(hook.current.isColumnVisible('mmr')).toBe(false);

      // Toggle 'mmr' back on -> should recognize competitive preset again!
      act(() => {
        hook.current.toggleColumn('mmr');
      });
      expect(hook.current.activePreset).toBe('competitive');
    });

    it('resetToDefault restores full preset', () => {
      const hook = renderHook(() => useColumnConfig());

      act(() => {
        hook.current.applyPreset('streamer');
      });
      expect(hook.current.activePreset).toBe('streamer');

      act(() => {
        hook.current.resetToDefault();
      });
      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('setColumn is idempotent when setting same value', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.isColumnVisible('score')).toBe(true);

      const setItemSpy = vi.spyOn(Storage.prototype, 'setItem');

      // Setting score to true when already true
      act(() => {
        hook.current.setColumn('score', true);
      });
      expect(hook.current.isColumnVisible('score')).toBe(true);
      // setItem should not be called because value didn't change
      expect(setItemSpy).not.toHaveBeenCalled();
    });
  });

  describe('3. Cross-Window Storage Event Sync', () => {
    it('synchronizes state when valid storage event is received from another tab', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');

      // Simulate competitive preset saved in another window
      const competitiveConfig = {} as Record<StatColumnKey, boolean>;
      for (const col of STAT_COLUMNS) {
        competitiveConfig[col.key] = PRESETS.competitive.columns.includes(col.key);
      }

      act(() => {
        const storageEvent = new StorageEvent('storage', {
          key: STORAGE_KEY,
          newValue: JSON.stringify(competitiveConfig),
        });
        window.dispatchEvent(storageEvent);
      });

      expect(hook.current.activePreset).toBe('competitive');
      expect(hook.current.visibleCount).toBe(5);
    });

    it('ignores storage events for other localStorage keys', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');

      act(() => {
        const storageEvent = new StorageEvent('storage', {
          key: 'some_other_key',
          newValue: JSON.stringify({ score: false }),
        });
        window.dispatchEvent(storageEvent);
      });

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('ignores storage events with malformed syntax JSON without throwing', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');

      act(() => {
        const storageEvent = new StorageEvent('storage', {
          key: STORAGE_KEY,
          newValue: '{bad: json,',
        });
        window.dispatchEvent(storageEvent);
      });

      // Hook state remains full
      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
    });

    it('synchronizes via same-window CustomEvent', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');

      const streamerConfig = {} as Record<StatColumnKey, boolean>;
      for (const col of STAT_COLUMNS) {
        streamerConfig[col.key] = PRESETS.streamer.columns.includes(col.key);
      }

      act(() => {
        const customEvent = new CustomEvent(EVENT_NAME, {
          detail: streamerConfig,
        });
        window.dispatchEvent(customEvent);
      });

      expect(hook.current.activePreset).toBe('streamer');
      expect(hook.current.visibleCount).toBe(4);
    });

    it('gracefully handles StorageEvent with newValue="null" without crashing or corrupting state', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');

      act(() => {
        const storageEvent = new StorageEvent('storage', {
          key: STORAGE_KEY,
          newValue: 'null',
        });
        window.dispatchEvent(storageEvent);
      });

      expect(hook.current.activePreset).toBe('full');
      expect(hook.current.visibleCount).toBe(10);
      expect(hook.current.isColumnVisible('score')).toBe(true);
    });

    it('gracefully ignores StorageEvent with non-object primitives and arrays', () => {
      const hook = renderHook(() => useColumnConfig());
      expect(hook.current.activePreset).toBe('full');

      act(() => {
        window.dispatchEvent(
          new StorageEvent('storage', {
            key: STORAGE_KEY,
            newValue: '"hello"',
          })
        );
      });
      expect(hook.current.activePreset).toBe('full');

      act(() => {
        window.dispatchEvent(
          new StorageEvent('storage', {
            key: STORAGE_KEY,
            newValue: '[1, 2, 3]',
          })
        );
      });
      expect(hook.current.activePreset).toBe('full');
    });
  });
});
