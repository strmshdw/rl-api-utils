// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';

import { formatMMR, formatMMRDelta } from './utils/formatters';
import { useColumnConfig, isValidColumnConfig, normalizeConfig } from './hooks/useColumnConfig';
import { STAT_COLUMNS, StatColumnKey } from './types/columns';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const STORAGE_KEY = 'rl_sync_column_config';

describe('ADVERSARIAL STRESS CHALLENGE SUITE — MILESTONE M3 ITERATION 2', () => {
  // =========================================================================
  // MISSION ITEM 1: formatMMR and formatMMRDelta Stress Testing
  // =========================================================================
  describe('Mission 1: formatMMR & formatMMRDelta Stress Oracle', () => {
    describe('formatMMR edge cases', () => {
      it('handles null, undefined, NaN, and ±0 cleanly without throwing', () => {
        expect(formatMMR(null)).toBe('---');
        expect(formatMMR(undefined)).toBe('---');
        expect(formatMMR(NaN)).toBe('---');
        expect(formatMMR(0)).toBe('---');
        expect(formatMMR(+0)).toBe('---');
        expect(formatMMR(-0)).toBe('---');
      });

      it('handles Infinity and -Infinity safely', () => {
        expect(formatMMR(Infinity)).toBe('Infinity');
        expect(formatMMR(-Infinity)).toBe('-Infinity');
      });

      it('handles extreme numeric values (MAX_VALUE, MIN_VALUE, MAX_SAFE_INTEGER)', () => {
        expect(formatMMR(Number.MAX_VALUE)).toBe(Number.MAX_VALUE.toFixed(1));
        expect(formatMMR(Number.MIN_VALUE)).toBe('0.0');
        expect(formatMMR(-Number.MIN_VALUE)).toBe('-0.0');
        expect(formatMMR(Number.MAX_SAFE_INTEGER)).toBe('9007199254740991.0');
        expect(formatMMR(Number.MIN_SAFE_INTEGER)).toBe('-9007199254740991.0');
      });

      it('rejects non-numeric inputs passed through dynamic/loose typing without throwing', () => {
        expect(formatMMR('1500' as unknown as number)).toBe('---');
        expect(formatMMR('NaN' as unknown as number)).toBe('---');
        expect(formatMMR(true as unknown as number)).toBe('---');
        expect(formatMMR(false as unknown as number)).toBe('---');
        expect(formatMMR({} as unknown as number)).toBe('---');
        expect(formatMMR([] as unknown as number)).toBe('---');
        expect(formatMMR(Symbol('mmr') as unknown as number)).toBe('---');
      });

      it('formats standard positive and negative MMRs with 1 decimal precision', () => {
        expect(formatMMR(1234.56)).toBe('1234.6');
        expect(formatMMR(1234.54)).toBe('1234.5');
        expect(formatMMR(-50.25)).toBe('-50.3');
        expect(formatMMR(100)).toBe('100.0');
      });
    });

    describe('formatMMRDelta edge cases', () => {
      it('handles null, undefined, NaN, and ±0 cleanly without throwing', () => {
        expect(formatMMRDelta(null)).toBe('±0.0');
        expect(formatMMRDelta(undefined)).toBe('±0.0');
        expect(formatMMRDelta(NaN)).toBe('±0.0');
        expect(formatMMRDelta(0)).toBe('±0.0');
        expect(formatMMRDelta(+0)).toBe('±0.0');
        expect(formatMMRDelta(-0)).toBe('±0.0');
      });

      it('handles Infinity and -Infinity with explicit signs', () => {
        expect(formatMMRDelta(Infinity)).toBe('+Infinity');
        expect(formatMMRDelta(-Infinity)).toBe('-Infinity');
      });

      it('handles extreme numeric values (MAX_VALUE, MIN_VALUE, MAX_SAFE_INTEGER)', () => {
        expect(formatMMRDelta(Number.MAX_VALUE)).toBe(`+${Number.MAX_VALUE.toFixed(1)}`);
        expect(formatMMRDelta(-Number.MAX_VALUE)).toBe(`-${Number.MAX_VALUE.toFixed(1)}`);
        expect(formatMMRDelta(Number.MIN_VALUE)).toBe('+0.0');
        expect(formatMMRDelta(-Number.MIN_VALUE)).toBe('-0.0');
        expect(formatMMRDelta(Number.MAX_SAFE_INTEGER)).toBe('+9007199254740991.0');
        expect(formatMMRDelta(Number.MIN_SAFE_INTEGER)).toBe('-9007199254740991.0');
      });

      it('rejects non-numeric inputs passed through dynamic/loose typing without throwing', () => {
        expect(formatMMRDelta('18.5' as unknown as number)).toBe('±0.0');
        expect(formatMMRDelta('NaN' as unknown as number)).toBe('±0.0');
        expect(formatMMRDelta(true as unknown as number)).toBe('±0.0');
        expect(formatMMRDelta(false as unknown as number)).toBe('±0.0');
        expect(formatMMRDelta({} as unknown as number)).toBe('±0.0');
        expect(formatMMRDelta([] as unknown as number)).toBe('±0.0');
        expect(formatMMRDelta(Symbol('delta') as unknown as number)).toBe('±0.0');
      });

      it('formats positive and negative MMR deltas with correct signs and precision', () => {
        expect(formatMMRDelta(18.54)).toBe('+18.5');
        expect(formatMMRDelta(18.56)).toBe('+18.6');
        expect(formatMMRDelta(-9.04)).toBe('-9.0');
        expect(formatMMRDelta(-9.06)).toBe('-9.1');
        expect(formatMMRDelta(0.0001)).toBe('+0.0');
        expect(formatMMRDelta(-0.0001)).toBe('-0.0');
      });
    });
  });

  // =========================================================================
  // MISSION ITEM 2: useColumnConfig Stress Testing
  // =========================================================================
  describe('Mission 2: useColumnConfig Adversarial Stress & Concurrency', () => {
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

    describe('Schema validation & Normalizer Oracles', () => {
      it('validates schema correctly against corrupted inputs', () => {
        expect(isValidColumnConfig(null)).toBe(false);
        expect(isValidColumnConfig(undefined)).toBe(false);
        expect(isValidColumnConfig(123)).toBe(false);
        expect(isValidColumnConfig('test')).toBe(false);
        expect(isValidColumnConfig([])).toBe(false);
        expect(isValidColumnConfig({})).toBe(false);

        // Missing columns
        expect(isValidColumnConfig({ score: true })).toBe(false);

        // Complete columns
        const complete: Record<string, boolean> = {};
        for (const col of STAT_COLUMNS) {
          complete[col.key] = true;
        }
        expect(isValidColumnConfig(complete)).toBe(true);

        // Complete but with non-boolean value
        complete.score = 'yes' as unknown as boolean;
        expect(isValidColumnConfig(complete)).toBe(false);
      });

      it('normalizes partial objects while ignoring non-boolean pollution', () => {
        expect(normalizeConfig(null)).toBe(null);
        expect(normalizeConfig(undefined)).toBe(null);
        expect(normalizeConfig({})).toBe(null);
        expect(normalizeConfig({ invalid_key: true })).toBe(null);

        // Partial object with valid boolean
        const normalized = normalizeConfig({ score: false, invalid_key: 123 });
        expect(normalized).not.toBe(null);
        expect(normalized?.score).toBe(false);
        // Missing keys normalized to default true
        expect(normalized?.goals).toBe(true);
        expect(normalized?.platform).toBe(true);
        // Pollution keys not present in returned object
        expect((normalized as unknown as Record<string, unknown>).invalid_key).toBeUndefined();
      });
    });

    describe('Corrupted localStorage payload recovery', () => {
      it('survives corrupted syntax, arrays, primitives, and null strings', () => {
        const attackVectors = [
          '{malformed json:',
          'undefined',
          '<xml>not json</xml>',
          '',
          '"primitive string"',
          '12345',
          'true',
          'false',
          'null',
          '[1, 2, 3]',
          '{"__proto__": {"polluted": true}}',
        ];

        for (const vector of attackVectors) {
          localStorage.setItem(STORAGE_KEY, vector);
          const hook = renderHook(() => useColumnConfig());
          expect(hook.current.activePreset).toBe('full');
          expect(hook.current.visibleCount).toBe(10);
        }
      });
    });

    describe('Cross-tab StorageEvents with null and attack payloads', () => {
      it('safely handles StorageEvent with null newValue (e.g. cross-tab removeItem or clear)', () => {
        const hook = renderHook(() => useColumnConfig());
        expect(hook.current.activePreset).toBe('full');

        act(() => {
          // Cross-tab removal dispatches newValue: null
          window.dispatchEvent(
            new StorageEvent('storage', {
              key: STORAGE_KEY,
              newValue: null,
            })
          );
        });

        // Must NOT crash or set config to null
        expect(hook.current.activePreset).toBe('full');
        expect(hook.current.visibleCount).toBe(10);
        expect(hook.current.isColumnVisible('score')).toBe(true);
      });

      it('safely handles StorageEvent with string "null" without state corruption', () => {
        const hook = renderHook(() => useColumnConfig());
        expect(hook.current.activePreset).toBe('full');

        act(() => {
          window.dispatchEvent(
            new StorageEvent('storage', {
              key: STORAGE_KEY,
              newValue: 'null',
            })
          );
        });

        expect(hook.current.activePreset).toBe('full');
        expect(hook.current.visibleCount).toBe(10);
      });

      it('safely ignores StorageEvent with invalid or corrupted JSON payloads', () => {
        const hook = renderHook(() => useColumnConfig());

        const badEvents = [
          '{syntax error',
          '123',
          '"text"',
          '[]',
          '{"goals": "not-bool"}',
        ];

        for (const bad of badEvents) {
          act(() => {
            window.dispatchEvent(
              new StorageEvent('storage', {
                key: STORAGE_KEY,
                newValue: bad,
              })
            );
          });
          // Remains full or valid
          expect(hook.current.config).toBeDefined();
          expect(typeof hook.current.config.score).toBe('boolean');
        }
      });
    });

    describe('Rapid toggling stress & state consistency', () => {
      it('survives 100 rapid sequential toggles on a single column without desyncing', () => {
        const hook = renderHook(() => useColumnConfig());
        expect(hook.current.isColumnVisible('score')).toBe(true);

        for (let i = 0; i < 100; i++) {
          act(() => {
            hook.current.toggleColumn('score');
          });
        }

        // Even count (100) -> back to true
        expect(hook.current.isColumnVisible('score')).toBe(true);
        expect(hook.current.activePreset).toBe('full');

        act(() => {
          hook.current.toggleColumn('score');
        });
        // Odd toggle -> false
        expect(hook.current.isColumnVisible('score')).toBe(false);
        expect(hook.current.activePreset).toBe('custom');
      });

      it('handles rapid interleaved toggles across multiple columns in round-robin', () => {
        const hook = renderHook(() => useColumnConfig());
        const cols: StatColumnKey[] = ['score', 'goals', 'assists', 'saves', 'shots'];

        for (let cycle = 0; cycle < 20; cycle++) {
          for (const col of cols) {
            act(() => {
              hook.current.toggleColumn(col);
            });
          }
        }

        // 20 cycles is even -> all toggled columns return to original state (true)
        for (const col of cols) {
          expect(hook.current.isColumnVisible(col)).toBe(true);
        }
        expect(hook.current.activePreset).toBe('full');
      });

      it('handles 50 rapid alternating preset applications', () => {
        const hook = renderHook(() => useColumnConfig());

        for (let i = 0; i < 50; i++) {
          act(() => {
            hook.current.applyPreset(i % 2 === 0 ? 'competitive' : 'streamer');
          });
        }

        // i=49 was odd -> 'streamer'
        expect(hook.current.activePreset).toBe('streamer');
        expect(hook.current.visibleCount).toBe(4);
        expect(hook.current.isColumnVisible('mmr')).toBe(false);
        expect(hook.current.isColumnVisible('assists')).toBe(true);
      });

      it('synchronizes multiple hook instances simultaneously in the same window via CustomEvent', () => {
        let hook1: ReturnType<typeof useColumnConfig>;
        let hook2: ReturnType<typeof useColumnConfig>;

        function DualHookComponent() {
          hook1 = useColumnConfig();
          hook2 = useColumnConfig();
          return null;
        }

        act(() => {
          root.render(<DualHookComponent />);
        });

        expect(hook1!.activePreset).toBe('full');
        expect(hook2!.activePreset).toBe('full');

        // Toggle 'score' via hook1
        act(() => {
          hook1.toggleColumn('score');
        });

        // Both hook1 and hook2 must have score = false
        expect(hook1!.isColumnVisible('score')).toBe(false);
        expect(hook2!.isColumnVisible('score')).toBe(false);
        expect(hook2!.activePreset).toBe('custom');
      });
    });
  });
});
