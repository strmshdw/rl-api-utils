// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act } from 'react';
import { mount, cleanup, flush, clickText } from './test/dom';

import { HistoryView } from './components/historical/HistoryView';
import { PlayersView } from './components/players/PlayersView';
import { HistoricalMatchRecord, PlayerSummary } from './types/api';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('M4 ADVERSARIAL CHALLENGER SUITE 1B: HISTORY & PLAYERS VIEWS', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  // =========================================================================
  // SUITE 3: HistoryView.tsx & PlayersView.tsx Adversarial Stress
  // =========================================================================
  describe('HistoryView.tsx and PlayersView.tsx Adversarial Stress', () => {
    it('prevents duplicate retry API storms when manual retry button is spam-clicked', async () => {
      const retryCalls: string[] = [];
      let resolveRetry: ((val: unknown) => void) | null = null;

      vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
          const url = typeof input === 'string' ? input : input.toString();
          const method = init?.method || 'GET';

          if (url.includes('/retry') && method === 'POST') {
            retryCalls.push(url);
            return new Promise((resolve) => {
              resolveRetry = () =>
                resolve({
                  ok: true,
                  status: 200,
                  json: async () => ({ status: 'ok', match_guid: 'failed-guid-1' }),
                } as Response);
            });
          }

          if (url.startsWith('/api/sync/ballchasing/status')) {
            return Promise.resolve({
              ok: true,
              json: async () => ({ running: false, total_replays: 0, synced_matches: 0, synced_players: 0 }),
            } as Response);
          }

          if (url.startsWith('/api/matches')) {
            const failedMatch: HistoricalMatchRecord = {
              match_guid: 'failed-guid-1',
              record_start_timestamp: 1700000000,
              map_name: 'Urban Central',
              playlist: 11,
              replay_url: '',
              download_status: 'FAILED',
              local_file_path: '',
              upload_status: 'FAILED',
              ballchasing_id: '',
              ballchasing_url: '',
              retry_count: 2,
              last_error: 'HTTP 500: Server Error',
              blue_score: 1,
              orange_score: 3,
              winner_team: 1,
              replay_stats_json: '',
              created_at: '2026-10-16T12:00:00Z',
              updated_at: '2026-10-16T12:05:00Z',
              stats: {
                match_guid: 'failed-guid-1',
                map_name: 'Urban Central',
                playlist: 11,
                blue_score: 1,
                orange_score: 3,
                winner_team: 1,
                duration: 300,
                overtime: false,
                date: '2026-10-16T12:00:00Z',
                players: [],
              },
            };
            return Promise.resolve({
              ok: true,
              json: async () => ({ matches: [failedMatch], total: 1, limit: 20, offset: 0 }),
            } as Response);
          }

          return Promise.resolve({ ok: true, json: async () => ({}) } as Response);
        }),
      );

      const { container } = await mount(<HistoryView localPlayerIds={new Set(['me'])} />);
      await flush(20);

      // Verify RETRY button is visible for failed match
      const retryBtn = Array.from(container.querySelectorAll('button')).find((b) =>
        b.textContent?.trim() === 'RETRY',
      ) as HTMLButtonElement;
      expect(retryBtn).toBeTruthy();

      // First click dispatches retry
      await act(async () => {
        retryBtn.click();
      });

      // After first click, button should be disabled and showing '…'
      expect(retryBtn.disabled).toBe(true);
      expect(retryBtn.textContent?.trim()).toBe('…');

      // Attempt repeated clicks while request is in-flight
      await act(async () => {
        retryBtn.click();
        retryBtn.click();
        retryBtn.click();
      });

      // Assert that fetch was ONLY called once for /retry!
      expect(retryCalls.length).toBe(1);
      expect(retryCalls[0]).toContain('/api/matches/failed-guid-1/retry');

      // Now resolve the in-flight retry request
      await act(async () => {
        resolveRetry?.(null);
      });
      await flush(20);
    });

    it('handles server-side outcome and playlist filter toggles with rapid pagination', async () => {
      const matchQueries: string[] = [];
      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();
          if (url.startsWith('/api/sync/ballchasing/status')) {
            return {
              ok: true,
              json: async () => ({ running: false, total_replays: 0, synced_matches: 0, synced_players: 0 }),
            } as Response;
          }
          if (url.startsWith('/api/matches')) {
            matchQueries.push(url);
            return {
              ok: true,
              json: async () => ({ matches: [], total: 100, limit: 20, offset: 0 }),
            } as Response;
          }
          return { ok: true, json: async () => ({}) } as Response;
        }),
      );

      const { container } = await mount(<HistoryView />);
      await flush(20);

      // Toggle Outcomes: WINS -> LOSSES -> ALL
      await clickText(container, 'WINS');
      await flush(10);
      expect(matchQueries.some((q) => q.includes('result=victory'))).toBe(true);

      await clickText(container, 'LOSSES');
      await flush(10);
      expect(matchQueries.some((q) => q.includes('result=defeat'))).toBe(true);

      // Toggle Playlists: 2V2 -> 3V3 -> ALL
      await clickText(container, '2V2');
      await flush(10);
      expect(matchQueries.some((q) => q.includes('playlist=11'))).toBe(true);

      await clickText(container, '3V3');
      await flush(10);
      expect(matchQueries.some((q) => q.includes('playlist=13'))).toBe(true);
    });

    it('exercises multi-column sort toggles (name, win_rate, match_count, last_seen) with asc/desc flips', async () => {
      const playerQueries: string[] = [];
      const mockSummaries: PlayerSummary[] = [
        {
          player_id: 'p1',
          platform: 'Steam',
          player_name: 'AlphaPlayer',
          ranks_json: '{}',
          first_seen_at: '2026-09-01T00:00:00Z',
          last_seen_at: '2026-10-15T00:00:00Z',
          total_wins_as_teammate: 5,
          total_losses_as_teammate: 2,
          total_wins_as_opponent: 3,
          total_losses_as_opponent: 1,
          total_matches: 11,
        },
        {
          player_id: 'p2',
          platform: 'Epic',
          player_name: 'ZetaPlayer',
          ranks_json: '{}',
          first_seen_at: '2026-09-02T00:00:00Z',
          last_seen_at: '2026-10-16T00:00:00Z',
          total_wins_as_teammate: 1,
          total_losses_as_teammate: 4,
          total_wins_as_opponent: 0,
          total_losses_as_opponent: 2,
          total_matches: 7,
        },
      ];

      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();
          if (url.startsWith('/api/players?')) {
            playerQueries.push(url);
            return {
              ok: true,
              json: async () => ({
                players: mockSummaries,
                total: 2,
                limit: 24,
                offset: 0,
                spotlights: {
                  best_teammate: { player: mockSummaries[0], value: '71%', sub: '5–2 TOGETHER', tone: 'win' },
                  most_faced: { player: mockSummaries[0], value: '4', sub: '4 GAMES AS OPPONENT', tone: 'neutral' },
                  toughest_rival: { player: mockSummaries[1], value: '0–2', sub: 'NEVER BEATEN', tone: 'loss' },
                },
              }),
            } as Response;
          }
          return { ok: true, json: async () => ({}) } as Response;
        }),
      );

      const { container } = await mount(<PlayersView />);
      await flush(20);

      // Verify Spotlights loaded
      expect(container.textContent).toContain('BEST TEAMMATE');
      expect(container.textContent).toContain('AlphaPlayer');

      // 1. Sort by PLAYER (name) -> asc then desc
      await clickText(container, 'PLAYER');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=name') && q.includes('order=asc'))).toBe(true);

      await clickText(container, 'PLAYER');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=name') && q.includes('order=desc'))).toBe(true);

      // 2. Sort by WITH YOU (win_rate) -> desc then asc
      await clickText(container, 'WITH YOU');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=win_rate') && q.includes('order=desc'))).toBe(true);

      await clickText(container, 'WITH YOU');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=win_rate') && q.includes('order=asc'))).toBe(true);

      // 3. Sort by GAMES (match_count) -> desc then asc
      await clickText(container, 'GAMES');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=match_count') && q.includes('order=desc'))).toBe(true);

      await clickText(container, 'GAMES');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=match_count') && q.includes('order=asc'))).toBe(true);

      // 4. Sort by LAST SEEN (last_seen) -> desc then asc
      await clickText(container, 'LAST SEEN');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=last_seen') && q.includes('order=desc'))).toBe(true);

      await clickText(container, 'LAST SEEN');
      await flush(10);
      expect(playerQueries.some((q) => q.includes('sort=last_seen') && q.includes('order=asc'))).toBe(true);
    });

    it('exercises rapid role filter toggles in PlayersView without desync', async () => {
      const roleQueries: string[] = [];
      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();
          if (url.startsWith('/api/players?')) {
            roleQueries.push(url);
            return {
              ok: true,
              json: async () => ({ players: [], total: 0, limit: 24, offset: 0 }),
            } as Response;
          }
          return { ok: true, json: async () => ({}) } as Response;
        }),
      );

      const { container } = await mount(<PlayersView />);
      await flush(20);

      // Rapidly switch role filters: FREQUENT -> TEAMMATES -> OPPONENTS -> SYNERGY ≥50% -> ALL
      await clickText(container, 'FREQUENT');
      await flush(10);
      expect(roleQueries.some((q) => q.includes('role=frequent'))).toBe(true);

      await clickText(container, 'TEAMMATES');
      await flush(10);
      expect(roleQueries.some((q) => q.includes('role=teammate'))).toBe(true);

      await clickText(container, 'OPPONENTS');
      await flush(10);
      expect(roleQueries.some((q) => q.includes('role=opponent'))).toBe(true);

      await clickText(container, 'SYNERGY ≥50%');
      await flush(10);
      expect(roleQueries.some((q) => q.includes('role=synergy'))).toBe(true);

      expect(container.textContent).toContain('NO PLAYERS FOUND');
    });
  });
});
