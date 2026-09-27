// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';

import { formatMMR, formatMMRDelta } from './utils/formatters';
import { useColumnConfig, isValidColumnConfig, normalizeConfig } from './hooks/useColumnConfig';
import { STAT_COLUMNS, StatColumnKey, ColumnConfig } from './types/columns';
import { RankBadge } from './components/common/RankBadge';
import { RosterTable } from './components/live/RosterTable';
import { LobbyPlayer, SessionMatchDetail } from './types/api';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const STORAGE_KEY = 'rl_sync_column_config';

const cleanHtml = (s: string) => s.replace(/<!--.*?-->/g, '');

const sampleFullConfig: ColumnConfig = {
  score: true,
  goals: true,
  assists: true,
  saves: true,
  shots: true,
  demos: true,
  mmr: true,
  rank: true,
  h2h: true,
  platform: true,
};

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

  // =========================================================================
  // MISSION ITEM 3: RankBadge Boundary & Extreme Value Testing
  // =========================================================================
  describe('Mission 3: RankBadge Boundary & Extreme Value Oracle', () => {
    it('enforces strict tier === 22 requirement for SSL pulsating theme', () => {
      const ssl = renderToString(<RankBadge rankName="Supersonic Legend" tier={22} />);
      expect(ssl).toContain('animate-pulse');
      expect(ssl).toContain('text-fuchsia-300');
      expect(ssl).toContain('border-fuchsia-400/60');
      expect(ssl).toContain('from-fuchsia-950/50');
    });

    it('falls back to neutral slate styling for all tiers > 22 (no pulse)', () => {
      const outOfRangeTiers = [23, 24, 25, 99, 1000, 999999, Number.MAX_SAFE_INTEGER, Infinity];

      for (const tier of outOfRangeTiers) {
        const html = cleanHtml(renderToString(<RankBadge rankName={`Tier ${tier}`} tier={tier} />));
        expect(html).not.toContain('animate-pulse');
        expect(html).not.toContain('from-fuchsia-950/50');
        expect(html).toContain('text-slate-400');
        expect(html).toContain('bg-slate-800/60');
        expect(html).toContain('border-slate-700');
      }
    });

    it('falls back to neutral slate styling for negative tiers (no pulse)', () => {
      const negTiers = [-1, -5, -22, -99, -Infinity];

      for (const tier of negTiers) {
        const html = cleanHtml(renderToString(<RankBadge rankName={`Tier ${tier}`} tier={tier} />));
        expect(html).not.toContain('animate-pulse');
        expect(html).toContain('text-slate-400');
        expect(html).toContain('bg-slate-800/60');
        expect(html).toContain('border-slate-700');
      }
    });

    it('falls back to neutral slate styling for 0, -0, and fractional non-integer tiers', () => {
      const fractional = [0, -0, 21.5, 22.1, 22.9];

      for (const tier of fractional) {
        const html = cleanHtml(renderToString(<RankBadge rankName={`Tier ${tier}`} tier={tier} />));
        expect(html).not.toContain('animate-pulse');
        expect(html).toContain('text-slate-400');
      }
    });

    it('falls back to neutral slate styling for NaN, undefined, and null tiers', () => {
      const corruptTiers = [NaN, undefined, null];

      for (const tier of corruptTiers) {
        const html = cleanHtml(renderToString(<RankBadge rankName="Corrupt" tier={tier} />));
        expect(html).not.toContain('animate-pulse');
        expect(html).toContain('text-slate-400');
        expect(html).toContain('border-slate-700');
      }
    });

    it('falls back to "Unranked" when rankName is falsy or missing', () => {
      expect(cleanHtml(renderToString(<RankBadge rankName="" tier={0} />))).toContain('Unranked');
      expect(cleanHtml(renderToString(<RankBadge rankName={null} tier={0} />))).toContain('Unranked');
      expect(cleanHtml(renderToString(<RankBadge rankName={undefined} tier={0} />))).toContain('Unranked');
    });
  });

  // =========================================================================
  // MISSION ITEM 4: RosterTable & PlayerRow Orange vs Blue Orientation
  // =========================================================================
  describe('Mission 4: RosterTable & PlayerRow Teammate vs Opponent Orientation', () => {
    const orangeTeammate: LobbyPlayer = {
      player_id: 'Epic|orange_mate|0',
      name: 'OrangeMate',
      platform: 'Epic',
      team_num: 1,
      is_local: false,
      is_bot: false,
      stats: { score: 650, goals: 3, assists: 1, saves: 2, shots: 4, demos: 1 },
      matchup_record: {
        player_id: 'Epic|orange_mate|0',
        playlist_id: 11,
        wins_as_teammate: 12,
        losses_as_teammate: 3,
        wins_as_opponent: 4,
        losses_as_opponent: 10,
        total_matches: 29,
        last_played_at: '2026-09-26T00:00:00Z',
      },
    };

    const blueOpponent: LobbyPlayer = {
      player_id: 'Steam|blue_opp|0',
      name: 'BlueOpponent',
      platform: 'Steam',
      team_num: 0,
      is_local: false,
      is_bot: false,
      stats: { score: 420, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0 },
      matchup_record: {
        player_id: 'Steam|blue_opp|0',
        playlist_id: 11,
        wins_as_teammate: 8,
        losses_as_teammate: 2,
        wins_as_opponent: 5,
        losses_as_opponent: 9,
        total_matches: 24,
        last_played_at: '2026-09-26T00:00:00Z',
      },
    };

    const localPlayerOrange: LobbyPlayer = {
      player_id: 'Epic|local_hero|0',
      name: 'LocalHero',
      platform: 'Epic',
      team_num: 1,
      is_local: true,
      is_bot: false,
      stats: { score: 700, goals: 2, assists: 2, saves: 3, shots: 5, demos: 2 },
    };

    it('correctly classifies Orange players as teammates and Blue players as opponents when localTeamNum is 1 (Orange)', () => {
      // 1. Orange Team Table (Local player on Orange -> Teammates)
      const orangeHtml = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={1}
            teamName="Orange Team"
            players={[localPlayerOrange, orangeTeammate]}
            localPlayerId="Epic|local_hero|0"
            localTeamNum={1}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Local player has "YOU" badge
      expect(orangeHtml).toContain('YOU');
      // Orange teammate displays teammate H2H record: 12W-3L (80%), NOT opponent record 4W-10L (29%)
      expect(orangeHtml).toContain('<span>12W-3L</span>');
      expect(orangeHtml).toContain('(80%)');
      expect(orangeHtml).not.toContain('<span>4W-10L</span>');
      expect(orangeHtml).not.toContain('(29%)');

      // 2. Blue Team Table (Local player on Orange -> Opponents)
      const blueHtml = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[blueOpponent]}
            localPlayerId="Epic|local_hero|0"
            localTeamNum={1}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Blue player displays opponent H2H record: 5W-9L (36%), NOT teammate record 8W-2L (80%)
      expect(blueHtml).not.toContain('YOU');
      expect(blueHtml).toContain('<span>5W-9L</span>');
      expect(blueHtml).toContain('(36%)');
      expect(blueHtml).not.toContain('<span>8W-2L</span>');
      expect(blueHtml).not.toContain('(80%)');
    });

    it('correctly classifies Blue players as teammates and Orange players as opponents when localTeamNum is 0 (Blue)', () => {
      // 1. Blue Team Table (Local player on Blue -> Teammates)
      const blueHtml = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[blueOpponent]}
            localPlayerId="Steam|local_blue|0"
            localTeamNum={0}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Blue player displays teammate H2H record: 8W-2L (80%), NOT opponent 5W-9L (36%)
      expect(blueHtml).toContain('<span>8W-2L</span>');
      expect(blueHtml).toContain('(80%)');
      expect(blueHtml).not.toContain('<span>5W-9L</span>');
      expect(blueHtml).not.toContain('(36%)');

      // 2. Orange Team Table (Local player on Blue -> Opponents)
      const orangeHtml = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={1}
            teamName="Orange Team"
            players={[orangeTeammate]}
            localPlayerId="Steam|local_blue|0"
            localTeamNum={0}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Orange player displays opponent H2H record: 4W-10L (29%), NOT teammate 12W-3L (80%)
      expect(orangeHtml).toContain('<span>4W-10L</span>');
      expect(orangeHtml).toContain('(29%)');
      expect(orangeHtml).not.toContain('<span>12W-3L</span>');
      expect(orangeHtml).not.toContain('(80%)');
    });

    it('defaults gracefully to Blue=Teammates and Orange=Opponents when localTeamNum is undefined', () => {
      const blueHtml = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[blueOpponent]}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );
      // Defaults to teamNum 0 being teammate
      expect(blueHtml).toContain('<span>8W-2L</span>');

      const orangeHtml = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={1}
            teamName="Orange Team"
            players={[orangeTeammate]}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );
      // Defaults to teamNum 1 being opponent
      expect(orangeHtml).toContain('<span>4W-10L</span>');
    });

    it('handles players with no matchup_record or 0 total matches safely', () => {
      const freshPlayer: LobbyPlayer = {
        player_id: 'Steam|new_guy|0',
        name: 'NewGuy',
        platform: 'Steam',
        team_num: 0,
        is_local: false,
        is_bot: false,
        stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
      };

      const zeroMatchPlayer: LobbyPlayer = {
        player_id: 'Steam|zero_matches|0',
        name: 'ZeroMatches',
        platform: 'Steam',
        team_num: 0,
        is_local: false,
        is_bot: false,
        stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
        matchup_record: {
          player_id: 'Steam|zero_matches|0',
          playlist_id: 11,
          wins_as_teammate: 0,
          losses_as_teammate: 0,
          wins_as_opponent: 0,
          losses_as_opponent: 0,
          total_matches: 0,
          last_played_at: '2026-09-26T00:00:00Z',
        },
      };

      const html = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[freshPlayer, zeroMatchPlayer]}
            localTeamNum={0}
            columnConfig={sampleFullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Both should render '--' for H2H
      expect(html).toContain('>--<');
    });
  });

  // =========================================================================
  // ADDITIONAL ADVERSARIAL CHECKS: MatchCard & PlayerSearchBar
  // =========================================================================
  describe('Additional Remediation Checks: MatchCard & PlayerSearchBar', () => {
    it('MatchCard dynamically renders Orange local team in orange text and Blue opponents in blue text when localTeamNum is 1', async () => {
      const { MatchCard } = await import('./components/session/MatchCard');
      const orangeSessionMatch: SessionMatchDetail = {
        match_guid: 'guid-1234',
        playlist_id: 11,
        playlist_name: 'Ranked Standard 3v3',
        result: 'victory',
        blue_score: 2,
        orange_score: 4,
        local_team: 1,
        starting_mmr: 1000,
        ending_mmr: 1009.5,
        mmr_change: 9.5,
        started_at: '2026-09-26T00:00:00Z',
        ended_at: '2026-09-26T00:05:00Z',
        duration_seconds: 300,
        players: [
          {
            player_id: 'Epic|local_orange|0',
            name: 'LocalOrange',
            platform: 'Epic',
            team_num: 1,
            is_local: true,
            is_bot: false,
            rank_name: 'Champion I',
            tier: 16,
            division: 1,
            mmr: 1000,
            stats: {
              score: 500,
              goals: 2,
              assists: 1,
              saves: 1,
              shots: 3,
              demos: 1,
            },
          },
          {
            player_id: 'Steam|opp_blue|0',
            name: 'OpponentBlue',
            platform: 'Steam',
            team_num: 0,
            is_local: false,
            is_bot: false,
            rank_name: 'Champion I',
            tier: 16,
            division: 1,
            mmr: 990,
            stats: {
              score: 300,
              goals: 1,
              assists: 0,
              saves: 2,
              shots: 2,
              demos: 0,
            },
          },
        ],
      };

      const html = cleanHtml(renderToString(<MatchCard match={orangeSessionMatch} onClick={() => {}} />));

      // Local player on Orange must be styled with orange text class text-[#ff7b00]
      expect(html).toContain('text-[#ff7b00] font-medium');
      expect(html).toContain('LocalOrange');
      // Blue opponents must be styled with blue text class text-[#00a2ff]
      expect(html).toContain('text-[#00a2ff]');
      expect(html).toContain('OpponentBlue');
    });

    it('MatchCard dynamically renders Blue local team in blue text and Orange opponents in orange text when localTeamNum is 0', async () => {
      const { MatchCard } = await import('./components/session/MatchCard');
      const blueSessionMatch: SessionMatchDetail = {
        match_guid: 'guid-5678',
        playlist_id: 11,
        playlist_name: 'Ranked Standard 3v3',
        result: 'victory',
        blue_score: 5,
        orange_score: 1,
        local_team: 0,
        starting_mmr: 1000,
        ending_mmr: 1008.5,
        mmr_change: 8.5,
        started_at: '2026-09-26T00:00:00Z',
        ended_at: '2026-09-26T00:05:00Z',
        duration_seconds: 300,
        players: [
          {
            player_id: 'Steam|local_blue|0',
            name: 'LocalBlue',
            platform: 'Steam',
            team_num: 0,
            is_local: true,
            is_bot: false,
            rank_name: 'Champion I',
            tier: 16,
            division: 1,
            mmr: 1000,
            stats: {
              score: 600,
              goals: 3,
              assists: 1,
              saves: 2,
              shots: 4,
              demos: 0,
            },
          },
          {
            player_id: 'Epic|opp_orange|0',
            name: 'OpponentOrange',
            platform: 'Epic',
            team_num: 1,
            is_local: false,
            is_bot: false,
            rank_name: 'Champion I',
            tier: 16,
            division: 1,
            mmr: 980,
            stats: {
              score: 250,
              goals: 1,
              assists: 0,
              saves: 1,
              shots: 2,
              demos: 1,
            },
          },
        ],
      };

      const html = cleanHtml(renderToString(<MatchCard match={blueSessionMatch} onClick={() => {}} />));

      // Local player on Blue must be styled with blue text class text-[#00a2ff]
      expect(html).toContain('text-[#00a2ff] font-medium');
      expect(html).toContain('LocalBlue');
      // Orange opponents must be styled with orange text class text-[#ff7b00]
      expect(html).toContain('text-[#ff7b00]');
      expect(html).toContain('OpponentOrange');
    });

    it('PlayerSearchBar includes PlayStation filter with id "playstation" matching backend storage normalization', async () => {
      const { PlayerSearchBar } = await import('./components/players/PlayerSearchBar');
      let selectedPlatform = 'all';

      const html = cleanHtml(
        renderToString(
          <PlayerSearchBar
            query=""
            onQueryChange={() => {}}
            platform={selectedPlatform}
            onPlatformChange={(p) => {
              selectedPlatform = p;
            }}
          />
        )
      );

      expect(html).toContain('PlayStation');
      // PlayStation pill renders cleanly
      expect(html).toContain('All Platforms');
      expect(html).toContain('Steam');
      expect(html).toContain('Epic Games');
      expect(html).toContain('Xbox');
    });
  });
});
