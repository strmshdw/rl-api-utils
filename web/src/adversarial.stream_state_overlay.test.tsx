// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';

import { sharedSSEManager } from './context/SSEContext';
import { LiveGameView } from './components/live/LiveGameView';
import { Scorebug } from './components/live/Scorebug';
import { OBSOverlayView } from './components/overlay/OBSOverlayView';
import App from './App';
import { formatMatchClock } from './utils/broadcast';
import { CurrentMatchResponse, LobbyPlayer } from './types/api';
import { SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from './utils/sampleData';
import { cleanup, flush, mount, setSearch } from './test/dom';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Mock EventSource implementation for testing SSE stream mechanics
class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  readyState: number = 0; // 0=CONNECTING, 1=OPEN, 2=CLOSED
  onopen: (() => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: any) => void) | null = null;
  private listeners: Map<string, Set<(e: MessageEvent) => void>> = new Map();
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (e: MessageEvent) => void) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, new Set());
    }
    this.listeners.get(type)!.add(listener);
  }

  removeEventListener(type: string, listener: (e: MessageEvent) => void) {
    this.listeners.get(type)?.delete(listener);
  }

  emit(type: string, data: any) {
    const rawData = typeof data === 'string' ? data : JSON.stringify(data);
    const event = new MessageEvent(type, { data: rawData });
    const handlers = this.listeners.get(type);
    if (handlers) {
      handlers.forEach((h) => h(event));
    }
    if (type === 'message' && this.onmessage) {
      this.onmessage(event);
    }
  }

  triggerOpen() {
    this.readyState = 1;
    if (this.onopen) this.onopen();
  }

  triggerError(err: any = new Error('SSE connection failed')) {
    if (this.onerror) this.onerror(err);
  }

  close() {
    this.closed = true;
    this.readyState = 2;
  }
}

describe('CHALLENGER 2: ADVERSARIAL SSE, STATE, CLOCK & OBS OVERLAY SUITE', () => {
  let originalEventSource: typeof EventSource;

  beforeEach(() => {
    originalEventSource = globalThis.EventSource;
    MockEventSource.instances = [];
    (globalThis as any).EventSource = MockEventSource;
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    globalThis.EventSource = originalEventSource;
    vi.restoreAllMocks();
    setSearch('');
  });

  // =========================================================================
  // SECTION 1: Shared SSE Stream Resilience & Singleton Connection Invariant
  // =========================================================================
  describe('1. Shared SSE Stream Resilience & Concurrency', () => {
    it('guarantees exactly ONE active EventSource across multiple concurrent subscribers', () => {
      const initialCount = MockEventSource.instances.length;

      const unsub1 = sharedSSEManager.subscribe('match_update', () => {});
      const unsub2 = sharedSSEManager.subscribe('match_ended', () => {});
      const unsub3 = sharedSSEManager.subscribe('clock_updated', () => {});
      const unsub4 = sharedSSEManager.subscribe('ranks_updated', () => {});
      const unsub5 = sharedSSEManager.subscribe('session_update', () => {});

      // Assert only 1 instance was created after 5 subscriptions
      const created = MockEventSource.instances.slice(initialCount);
      expect(created.length).toBe(1);
      expect(created[0].closed).toBe(false);

      // Unsubscribe 4 of 5
      unsub1();
      unsub2();
      unsub3();
      unsub4();

      // Assert connection remains open for the remaining subscriber
      expect(created[0].closed).toBe(false);

      // Unsubscribe final subscriber
      unsub5();
    });

    it('survives rapid bursts of 50 subscribe and unsubscribe operations without socket leaks', () => {
      const activeUnsubs: Array<() => void> = [];

      for (let i = 0; i < 50; i++) {
        const unsub = sharedSSEManager.subscribe(
          i % 2 === 0 ? 'clock_updated' : 'match_update',
          () => {}
        );
        if (i % 3 === 0) {
          unsub(); // immediately unsubscribe some
        } else {
          activeUnsubs.push(unsub);
        }
      }

      const activeInstances = MockEventSource.instances.filter((inst) => !inst.closed);
      // Invariant: exactly 1 or 0 active instances, never more than 1
      expect(activeInstances.length).toBeLessThanOrEqual(1);

      // Clean up remaining
      activeUnsubs.forEach((u) => u());
    });

    it('executes exponential backoff with ceiling on repeated disconnects/errors', () => {
      vi.useFakeTimers();
      const unsub = sharedSSEManager.subscribe('match_update', () => {});

      const initialInstance = MockEventSource.instances[MockEventSource.instances.length - 1];
      expect(initialInstance).toBeDefined();

      // Simulate connection open
      initialInstance.triggerOpen();
      expect(sharedSSEManager.getStatus()).toBe('connected');

      // Trigger error 1
      initialInstance.triggerError();
      expect(initialInstance.closed).toBe(true);
      expect(sharedSSEManager.getStatus()).toBe('reconnecting');

      // Retry count is 1: expected backoff ~1000ms (+ 0..500ms jitter)
      // Advance by 500ms (should still be reconnecting, not connected yet)
      vi.advanceTimersByTime(500);
      let active = MockEventSource.instances.filter((i) => !i.closed);
      expect(active.length).toBe(0);

      // Advance by another 1200ms -> should have reconnected
      vi.advanceTimersByTime(1200);
      active = MockEventSource.instances.filter((i) => !i.closed);
      expect(active.length).toBe(1);

      // Trigger consecutive errors and verify retryCount increases and backoff stays <= 15500ms
      const secondInstance = active[0];
      secondInstance.triggerError();
      expect(secondInstance.closed).toBe(true);

      // Retry count is 2: expected backoff ~1500ms (+ jitter)
      vi.advanceTimersByTime(1000);
      expect(MockEventSource.instances.filter((i) => !i.closed).length).toBe(0);
      vi.advanceTimersByTime(1200);
      expect(MockEventSource.instances.filter((i) => !i.closed).length).toBe(1);

      unsub();
      vi.useRealTimers();
    });

    it('safely handles rapid bursts of 100 multiplexed SSE events without drops', () => {
      const matchUpdates: any[] = [];
      const matchEnds: any[] = [];
      const clockUpdates: any[] = [];
      const ranksUpdates: any[] = [];

      const unsub1 = sharedSSEManager.subscribe('match_update', (d) => matchUpdates.push(d));
      const unsub2 = sharedSSEManager.subscribe('match_ended', (d) => matchEnds.push(d));
      const unsub3 = sharedSSEManager.subscribe('clock_updated', (d) => clockUpdates.push(d));
      const unsub4 = sharedSSEManager.subscribe('ranks_updated', (d) => ranksUpdates.push(d));

      const activeInst = MockEventSource.instances[MockEventSource.instances.length - 1];
      activeInst.triggerOpen();

      // Rapid burst of 100 interleaved events
      for (let i = 0; i < 25; i++) {
        activeInst.emit('match_update', { match_guid: `match_${i}`, active_match: true });
        activeInst.emit('clock_updated', { time_remaining: 300 - i, is_overtime: false });
        activeInst.emit('ranks_updated', { playlist_id: 11, mmr: 1500 + i });
        activeInst.emit('match_ended', { match_guid: `match_${i}`, winner_team: 0, result: 'victory' });
      }

      expect(matchUpdates.length).toBe(25);
      expect(clockUpdates.length).toBe(25);
      expect(ranksUpdates.length).toBe(25);
      expect(matchEnds.length).toBe(25);
      expect(matchUpdates[24].match_guid).toBe('match_24');
      expect(clockUpdates[24].time_remaining).toBe(276);

      unsub1();
      unsub2();
      unsub3();
      unsub4();
    });

    it('survives corrupted, non-JSON or malformed payloads without crashing SSE manager', () => {
      const received: any[] = [];
      const unsub = sharedSSEManager.subscribe('match_update', (d) => received.push(d));

      const activeInst = MockEventSource.instances[MockEventSource.instances.length - 1];
      activeInst.triggerOpen();

      const badPayloads = [
        '{malformed-json:',
        'undefined',
        '<xml>corrupt</xml>',
        '}{',
        '',
        'NaN',
      ];

      for (const bad of badPayloads) {
        expect(() => {
          activeInst.emit('match_update', bad);
        }).not.toThrow();
      }

      // Valid payload immediately after still succeeds
      activeInst.emit('match_update', { match_guid: 'valid_after_corrupted', active_match: true });
      expect(received.length).toBe(1);
      expect(received[0].match_guid).toBe('valid_after_corrupted');

      unsub();
    });

    it('isolates subscriber errors so one throwing subscriber does not affect others', () => {
      const safeReceived: any[] = [];
      const buggyListener = () => {
        throw new Error('Exploding subscriber exception!');
      };
      const safeListener = (data: any) => {
        safeReceived.push(data);
      };

      const unsubBuggy = sharedSSEManager.subscribe('match_update', buggyListener);
      const unsubSafe = sharedSSEManager.subscribe('match_update', safeListener);

      const activeInst = MockEventSource.instances[MockEventSource.instances.length - 1];
      activeInst.triggerOpen();

      expect(() => {
        activeInst.emit('match_update', { match_guid: 'isolation_test', active_match: true });
      }).not.toThrow();

      // Safe listener must still receive the event
      expect(safeReceived.length).toBe(1);
      expect(safeReceived[0].match_guid).toBe('isolation_test');

      unsubBuggy();
      unsubSafe();
    });
  });

  // =========================================================================
  // SECTION 2: Clean match_ended State Preservation in LiveGameView
  // =========================================================================
  describe('2. Clean match_ended State Preservation in LiveGameView', () => {
    it('preserves final box score, team rosters and player cards upon match_ended', async () => {
      const activeMatch: CurrentMatchResponse = {
        ...SAMPLE_CURRENT_MATCH,
        active_match: true,
        match_ended: false,
      };

      const { container, rerender } = await mount(
        <LiveGameView
          match={activeMatch}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {}}
        />
      );

      // Verify active match is rendered
      expect(container.querySelector('[data-testid="live-match"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="live-idle"]')).toBeNull();
      expect(container.querySelector('[data-testid="match-ended-banner"]')).toBeNull();
      expect(container.querySelectorAll('[data-testid="live-player-card"]').length).toBe(4);

      // Transition to match_ended without losing roster data
      const endedMatch: CurrentMatchResponse = {
        ...activeMatch,
        active_match: false,
        match_ended: true,
        winner_team: 0,
        result: 'victory',
      };

      await rerender(
        <LiveGameView
          match={endedMatch}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {}}
        />
      );

      // Crucial assertion: Screen must NOT wipe to IdleStateView
      expect(container.querySelector('[data-testid="live-idle"]')).toBeNull();
      expect(container.querySelector('[data-testid="live-match"]')).not.toBeNull();

      // Banner must be displayed
      const banner = container.querySelector('[data-testid="match-ended-banner"]');
      expect(banner).not.toBeNull();
      expect(banner?.textContent).toContain('MATCH CONCLUDED · FINAL BOX SCORE');
      expect(banner?.textContent).toContain('VICTORY');

      // Box score and all 4 player cards must remain fully mounted
      const cards = container.querySelectorAll('[data-testid="live-player-card"]');
      expect(cards.length).toBe(4);

      // Scorebug must show WIN instead of ● LIVE
      const scorebug = container.querySelector('[data-testid="scorebug"]');
      expect(scorebug?.textContent).toContain('WIN');
      expect(scorebug?.textContent).not.toContain('● LIVE');
    });

    it('renders DEFEAT badge and LOSS scorebug status when match ends in defeat', async () => {
      const defeatMatch: CurrentMatchResponse = {
        ...SAMPLE_CURRENT_MATCH,
        active_match: false,
        match_ended: true,
        winner_team: 1,
        result: 'defeat',
      };

      const { container } = await mount(
        <LiveGameView
          match={defeatMatch}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {}}
        />
      );

      expect(container.querySelector('[data-testid="live-idle"]')).toBeNull();
      expect(container.querySelector('[data-testid="live-match"]')).not.toBeNull();

      const banner = container.querySelector('[data-testid="match-ended-banner"]');
      expect(banner?.textContent).toContain('DEFEAT');

      const scorebug = container.querySelector('[data-testid="scorebug"]');
      expect(scorebug?.textContent).toContain('LOSS');
    });

    it('triggers onShowSession callback when user clicks VIEW SESSION on match ended banner', async () => {
      let sessionClicked = false;
      const endedMatch: CurrentMatchResponse = {
        ...SAMPLE_CURRENT_MATCH,
        active_match: false,
        match_ended: true,
        winner_team: 0,
        result: 'victory',
      };

      const { container } = await mount(
        <LiveGameView
          match={endedMatch}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {
            sessionClicked = true;
          }}
        />
      );

      const banner = container.querySelector('[data-testid="match-ended-banner"]');
      const viewSessionBtn = banner?.querySelector('button');
      expect(viewSessionBtn).not.toBeNull();

      await act(async () => {
        viewSessionBtn?.click();
      });

      expect(sessionClicked).toBe(true);
    });

    it('switches to IdleStateView when match is null or neither active nor ended', async () => {
      const { container, rerender } = await mount(
        <LiveGameView
          match={null}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {}}
        />
      );

      expect(container.querySelector('[data-testid="live-idle"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="live-match"]')).toBeNull();

      // Also when match object has both active_match and match_ended as false
      await rerender(
        <LiveGameView
          match={{ ...SAMPLE_CURRENT_MATCH, active_match: false, match_ended: false }}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {}}
        />
      );

      expect(container.querySelector('[data-testid="live-idle"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="live-match"]')).toBeNull();
    });
  });

  // =========================================================================
  // SECTION 3: Live Game Clock Adversarial Stress & Edge Cases
  // =========================================================================
  describe('3. Live Game Clock Adversarial Stress', () => {
    describe('formatMatchClock Oracle', () => {
      it('handles negative seconds (kickoff countdown e.g. -3) by clamping to 00:00 without crashing', () => {
        expect(formatMatchClock(-3)).toBe('00:00');
        expect(formatMatchClock(-1)).toBe('00:00');
        expect(formatMatchClock(-100)).toBe('00:00');
        expect(formatMatchClock(-0)).toBe('00:00');
      });

      it('formats 00:00 boundaries accurately for regulation and overtime', () => {
        expect(formatMatchClock(0, false)).toBe('00:00');
        expect(formatMatchClock(0, true)).toBe('+00:00');
        expect(formatMatchClock(59, false)).toBe('00:59');
        expect(formatMatchClock(60, false)).toBe('01:00');
        expect(formatMatchClock(300, false)).toBe('05:00');
      });

      it('formats extreme overtime values up to and exceeding +99:59 cleanly', () => {
        // Overtime 1 second
        expect(formatMatchClock(1, true)).toBe('+00:01');
        // Overtime 5 minutes
        expect(formatMatchClock(300, true)).toBe('+05:00');
        // Overtime 99 minutes 59 seconds (5999 seconds)
        expect(formatMatchClock(5999, true)).toBe('+99:59');
        // Extreme overtime 120 minutes (7200 seconds)
        expect(formatMatchClock(7200, true)).toBe('+120:00');
      });

      it('handles null, undefined and fractional seconds safely', () => {
        expect(formatMatchClock(null)).toBe('00:00');
        expect(formatMatchClock(undefined)).toBe('00:00');
        expect(formatMatchClock(185.8)).toBe('03:05');
        expect(formatMatchClock(0.4)).toBe('00:00');
      });
    });

    describe('Scorebug Clock Component Integration', () => {
      const dummyPlayers: LobbyPlayer[] = [
        {
          player_id: 'p1',
          platform: 'Steam',
          name: 'Player 1',
          team_num: 0,
          is_local: true,
          is_bot: false,
          stats: { score: 100, goals: 1, assists: 0, saves: 0, shots: 1, demos: 0 },
        },
        {
          player_id: 'p2',
          platform: 'Steam',
          name: 'Player 2',
          team_num: 1,
          is_local: false,
          is_bot: false,
          stats: { score: 50, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
        },
      ];

      it('renders countdown clock with negative seconds as 00:00', async () => {
        const matchWithNegativeClock: CurrentMatchResponse = {
          ...SAMPLE_CURRENT_MATCH,
          active_match: true,
          match_ended: false,
          time_remaining: -3,
          is_overtime: false,
        };

        const { container } = await mount(
          <Scorebug
            match={matchWithNegativeClock}
            bluePlayers={[dummyPlayers[0]]}
            orangePlayers={[dummyPlayers[1]]}
            localTeam={0}
          />
        );

        const clockEl = container.querySelector('[data-testid="scorebug-clock"]');
        expect(clockEl).not.toBeNull();
        expect(clockEl?.textContent).toBe('00:00');
      });

      it('renders extreme overtime (+99:59) with OT badge and highlighted styling', async () => {
        const matchWithExtremeOT: CurrentMatchResponse = {
          ...SAMPLE_CURRENT_MATCH,
          active_match: true,
          match_ended: false,
          time_remaining: 5999,
          is_overtime: true,
        };

        const { container } = await mount(
          <Scorebug
            match={matchWithExtremeOT}
            bluePlayers={[dummyPlayers[0]]}
            orangePlayers={[dummyPlayers[1]]}
            localTeam={0}
          />
        );

        const clockEl = container.querySelector('[data-testid="scorebug-clock"]');
        expect(clockEl).not.toBeNull();
        expect(clockEl?.textContent).toBe('+99:59');
        expect(clockEl?.className).toContain('text-loss');
        expect(container.textContent).toContain('OT');
      });

      it('falls back to default LIVE badge when time_remaining is undefined', async () => {
        const matchWithoutClock: CurrentMatchResponse = {
          ...SAMPLE_CURRENT_MATCH,
          active_match: true,
          match_ended: false,
          time_remaining: undefined,
        };

        const { container } = await mount(
          <Scorebug
            match={matchWithoutClock}
            bluePlayers={[dummyPlayers[0]]}
            orangePlayers={[dummyPlayers[1]]}
            localTeam={0}
          />
        );

        expect(container.querySelector('[data-testid="scorebug-clock"]')).toBeNull();
        expect(container.textContent).toContain('● LIVE');
      });
    });
  });

  // =========================================================================
  // SECTION 4: OBS Overlay Parameter Stress & Route Variations
  // =========================================================================
  describe('4. OBS Overlay Parameter Stress & Route Variations', () => {
    beforeEach(() => {
      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();
          if (url.includes('/api/current-match')) {
            return {
              ok: true,
              status: 200,
              json: async () => ({
                ...SAMPLE_CURRENT_MATCH,
                time_remaining: 120,
                is_overtime: false,
              }),
            } as Response;
          }
          if (url.includes('/api/session')) {
            return {
              ok: true,
              status: 200,
              json: async () => SAMPLE_SESSION_RESPONSE,
            } as Response;
          }
          return { ok: true, status: 200, json: async () => ({}) } as Response;
        })
      );
    });

    it('falls back to dark theme when ?theme is corrupted or unrecognized', async () => {
      setSearch('theme=DROP_TABLE_USERS&overlay=true');
      const { container } = await mount(<OBSOverlayView />);
      await flush(10);

      const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
      expect(overlay).toBeTruthy();
      // Must fallback to dark theme classes
      expect(overlay?.className).toContain('bg-[#0c0d12]/90');
      expect(overlay?.className).toContain('text-ink');
    });

    it('falls back to font-display when ?font is invalid', async () => {
      setSearch('font=comic_sans_unknown&overlay=true');
      const { container } = await mount(<OBSOverlayView />);
      await flush(10);

      const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
      expect(overlay).toBeTruthy();
      expect(overlay?.className).toContain('font-display');
    });

    it('handles negative and malformed scale parameters without throwing', async () => {
      setSearch('scale=-5&overlay=true');
      const { container } = await mount(<OBSOverlayView />);
      await flush(10);

      const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
      expect(overlay).toBeTruthy();
      expect(overlay?.getAttribute('style')).toContain('scale(-5)');
    });

    it('defaults to full elements when ?show is empty', async () => {
      setSearch('show=&overlay=true');
      const { container } = await mount(<OBSOverlayView />);
      await flush(10);

      const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
      expect(overlay).toBeTruthy();
      // Empty ?show defaults to clock, score, session, delta
      expect(overlay?.textContent).toContain('02:00'); // clock
      expect(overlay?.textContent).toContain('+29'); // delta
    });

    it('renders overlay mode under all supported route and query representations in App', async () => {
      // 1. ?overlay=true
      setSearch('overlay=true');
      const res1 = await mount(<App />);
      expect(res1.container.querySelector('[data-testid="obs-overlay-view"]')).not.toBeNull();
      expect(res1.container.querySelector('[data-testid="app-header"]')).toBeNull();
      res1.unmount();

      // 2. ?mode=overlay
      setSearch('mode=overlay');
      const res2 = await mount(<App />);
      expect(res2.container.querySelector('[data-testid="obs-overlay-view"]')).not.toBeNull();
      res2.unmount();

      // 3. /overlay pathname
      setSearch('');
      window.history.replaceState(null, '', '/overlay');
      const res3 = await mount(<App />);
      expect(res3.container.querySelector('[data-testid="obs-overlay-view"]')).not.toBeNull();
      res3.unmount();

      // 4. Standard dashboard route /
      window.history.replaceState(null, '', '/');
      const res4 = await mount(<App />);
      expect(res4.container.querySelector('[data-testid="obs-overlay-view"]')).toBeNull();
      expect(res4.container.querySelector('[data-testid="app-header"]')).not.toBeNull();
      res4.unmount();
    });

    it('synchronizes document body overlay-mode class for transparent OBS backgrounds', async () => {
      setSearch('overlay=true');
      const res = await mount(<App />);
      expect(document.body.classList.contains('overlay-mode')).toBe(true);
      expect(document.documentElement.classList.contains('overlay-mode')).toBe(true);

      res.unmount();
      setSearch('');
      window.history.replaceState(null, '', '/');
      const res2 = await mount(<App />);
      expect(document.body.classList.contains('overlay-mode')).toBe(false);
      expect(document.documentElement.classList.contains('overlay-mode')).toBe(false);
      res2.unmount();
    });
  });
});
