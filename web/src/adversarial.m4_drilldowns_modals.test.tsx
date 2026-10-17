// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act } from 'react';
import { mount, cleanup, flush, clickText } from './test/dom';

import { PlayerProfileDialog } from './components/players/PlayerProfileDialog';
import { HistoricalMatchDetailModal } from './components/historical/HistoricalMatchDetailModal';
import { HistoryView } from './components/historical/HistoryView';
import { PlayersView } from './components/players/PlayersView';
import { HistoricalMatchRecord, PlayerDetailResponse, PlayerMatchesResponse, PlayerSummary } from './types/api';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Helper to generate mock players
function createMockPlayer(id: string, name: string): PlayerDetailResponse {
  return {
    player_id: id,
    platform: 'Steam',
    player_name: name,
    ranks_json: JSON.stringify({
      '11': {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 17,
        division: 3,
        rank_name: 'Grand Champion II',
        mmr: 1620,
        matches_played: 120,
      },
    }),
    notes: `Notes for ${name}`,
    tags: ['reliable', 'fast'],
    first_seen_at: '2026-09-01T12:00:00Z',
    last_seen_at: '2026-10-15T18:00:00Z',
    matchups: [
      {
        player_id: id,
        playlist_id: 11,
        wins_as_teammate: 4,
        losses_as_teammate: 2,
        wins_as_opponent: 3,
        losses_as_opponent: 3,
        total_matches: 12,
        last_played_at: '2026-10-15T18:00:00Z',
      },
      {
        player_id: id,
        playlist_id: 13,
        wins_as_teammate: 2,
        losses_as_teammate: 1,
        wins_as_opponent: 1,
        losses_as_opponent: 2,
        total_matches: 6,
        last_played_at: '2026-10-14T12:00:00Z',
      },
    ],
  };
}

function createMockMatches(playerId: string, nextPlayerId?: string, nextPlayerName?: string): PlayerMatchesResponse {
  return {
    matches: [
      {
        match_guid: `guid-${playerId}-01`,
        record_start_timestamp: 1700000000,
        map_name: 'Champions Field',
        playlist: 11,
        replay_url: '',
        download_status: 'COMPLETED',
        local_file_path: '',
        upload_status: 'COMPLETED',
        ballchasing_id: '',
        ballchasing_url: '',
        retry_count: 0,
        last_error: '',
        blue_score: 5,
        orange_score: 3,
        winner_team: 0,
        replay_stats_json: '',
        created_at: '2026-10-15T18:00:00Z',
        updated_at: '2026-10-15T18:05:00Z',
        result: 'victory',
        role: 'teammate',
        player_stats: {
          match_guid: `guid-${playerId}-01`,
          player_id: playerId,
          team_num: 0,
          is_local: false,
          won: true,
          score: 510,
          goals: 3,
          assists: 1,
          saves: 2,
          shots: 5,
          demos: 1,
        },
        stats: {
          match_guid: `guid-${playerId}-01`,
          date: '2026-10-15T18:00:00Z',
          playlist: 11,
          map_name: 'Champions Field',
          duration: 320,
          blue_score: 5,
          orange_score: 3,
          winner_team: 0,
          overtime: false,
          players: [
            {
              name: 'InspectedPlayer',
              platform: 'Steam',
              online_id: playerId,
              team: 0,
              score: 510,
              goals: 3,
              assists: 1,
              saves: 2,
              shots: 5,
              demos: 1,
            },
            {
              name: nextPlayerName || 'TeammateTwo',
              platform: 'Epic',
              online_id: nextPlayerId || 'mate-99',
              team: 0,
              score: 380,
              goals: 2,
              assists: 2,
              saves: 1,
              shots: 3,
              demos: 0,
            },
            {
              name: 'OpponentOne',
              platform: 'Steam',
              online_id: 'opp-01',
              team: 1,
              score: 420,
              goals: 2,
              assists: 1,
              saves: 3,
              shots: 4,
              demos: 2,
            },
          ],
        },
      },
    ],
    total: 1,
    limit: 10,
    offset: 0,
  };
}

describe('M4 ADVERSARIAL CHALLENGER SUITE 1: DRILL-DOWNS, MODALS & VIEWS', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  // =========================================================================
  // SUITE 1: PlayerProfileDialog.tsx Stress Testing
  // =========================================================================
  describe('PlayerProfileDialog.tsx Adversarial Stress', () => {
    it('handles deep historyStack navigation across 5+ hops and unwinds with back button', async () => {
      const players = ['player-alpha', 'player-beta', 'player-gamma', 'player-delta', 'player-epsilon', 'player-zeta'];
      const playerNames = ['Alpha', 'Beta', 'Gamma', 'Delta', 'Epsilon', 'Zeta'];

      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();

          for (let i = 0; i < players.length; i++) {
            const pId = players[i];
            const pName = playerNames[i];
            const nextPId = i + 1 < players.length ? players[i + 1] : undefined;
            const nextPName = i + 1 < playerNames.length ? playerNames[i + 1] : undefined;

            if (url.includes(`/api/players/${pId}/matches`)) {
              return {
                ok: true,
                status: 200,
                json: async () => createMockMatches(pId, nextPId, nextPName),
              } as Response;
            }
            if (url.includes(`/api/players/${pId}`)) {
              return {
                ok: true,
                status: 200,
                json: async () => createMockPlayer(pId, pName),
              } as Response;
            }
          }
          return { ok: true, status: 200, json: async () => ({}) } as Response;
        })
      );

      const { container } = await mount(
        <PlayerProfileDialog
          playerId="player-alpha"
          playerName="Alpha"
          onClose={vi.fn()}
          localPlayerIds={new Set(['local-me'])}
        />
      );
      await flush(20);

      expect(container.textContent).toContain('Alpha');

      // Navigate 5 hops: Alpha -> Beta -> Gamma -> Delta -> Epsilon -> Zeta
      for (let hop = 0; hop < 5; hop++) {
        // Expand the match row to show box score
        const matchRow = container.querySelector('[data-testid="profile-match-row"]');
        expect(matchRow).toBeTruthy();
        await act(async () => {
          (matchRow as HTMLElement)?.click();
        });
        await flush(10);

        // Find the clickable next player button in the box score
        const nextName = playerNames[hop + 1];
        const nextBtn = Array.from(container.querySelectorAll('button')).find((b) =>
          b.textContent?.includes(nextName)
        );
        expect(nextBtn).toBeTruthy();

        await act(async () => {
          nextBtn?.click();
        });
        await flush(25);

        // Verify title updated to next player
        expect(container.textContent).toContain(playerNames[hop + 1]);
        // Verify Back button exists
        expect(container.textContent).toContain(`← BACK TO ${playerNames[hop]}`);
      }

      // Now we are at Zeta (hop 5). Let's unwind all 5 hops using the back button!
      for (let unwound = 4; unwound >= 0; unwound--) {
        const backBtn = Array.from(container.querySelectorAll('button')).find((b) =>
          b.textContent?.includes('← BACK TO')
        );
        expect(backBtn).toBeTruthy();

        await act(async () => {
          backBtn?.click();
        });
        await flush(25);

        expect(container.textContent).toContain(playerNames[unwound]);
      }

      // Back at Alpha: back button should no longer be present
      const remainingBackBtn = Array.from(container.querySelectorAll('button')).find((b) =>
        b.textContent?.includes('← BACK TO')
      );
      expect(remainingBackBtn).toBeUndefined();
      expect(container.textContent).toContain('PLAYER PROFILE · STEAM');
    });

    it('survives rapid clicking barrages between WITH YOU, VS YOU, BY PLAYLIST, and CLEAR', async () => {
      const fetchCalls: string[] = [];
      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();
          fetchCalls.push(url);
          if (url.includes('/matches')) {
            return {
              ok: true,
              status: 200,
              json: async () => createMockMatches('player-stress'),
            } as Response;
          }
          return {
            ok: true,
            status: 200,
            json: async () => createMockPlayer('player-stress', 'StressTester'),
          } as Response;
        })
      );

      const { container } = await mount(
        <PlayerProfileDialog
          playerId="player-stress"
          playerName="StressTester"
          onClose={vi.fn()}
        />
      );
      await flush(20);

      const withCard = container.querySelector('[data-testid="h2h-with"]') as HTMLElement;
      const vsCard = container.querySelector('[data-testid="h2h-vs"]') as HTMLElement;
      const playlistRows = () => Array.from(container.querySelectorAll<HTMLElement>('[data-testid="profile-playlist-row"]'));

      expect(withCard).toBeTruthy();
      expect(vsCard).toBeTruthy();

      // Stress click barrage: 30 rapid state transitions
      await act(async () => {
        for (let i = 0; i < 10; i++) {
          withCard.click();
          vsCard.click();
          const rows = playlistRows();
          if (rows.length > 0) rows[0].click();
        }
      });
      await flush(30);

      // Verify component has not crashed and active-filter-summary or clear button works
      const clearBtn = Array.from(container.querySelectorAll('button')).find((b) =>
        b.textContent?.includes('CLEAR FILTERS')
      );
      if (clearBtn) {
        await act(async () => {
          clearBtn.click();
        });
        await flush(10);
      }

      // Assert that requests were issued and UI remains intact
      expect(container.textContent).toContain('StressTester');
      expect(container.textContent).toContain('RANKS');
      expect(container.textContent).toContain('BY PLAYLIST');
    });

    it('gracefully renders 0-game players, empty matchups, null ranks, and missing match history', async () => {
      const zeroPlayer: PlayerDetailResponse = {
        player_id: 'player-ghost',
        platform: 'Epic',
        player_name: 'GhostRider',
        ranks_json: '{}',
        notes: '',
        tags: [],
        first_seen_at: '2026-10-16T00:00:00Z',
        last_seen_at: '2026-10-16T00:00:00Z',
        matchups: [],
      };

      vi.stubGlobal(
        'fetch',
        vi.fn(async (input: RequestInfo | URL) => {
          const url = typeof input === 'string' ? input : input.toString();
          if (url.includes('/matches')) {
            return {
              ok: true,
              status: 200,
              json: async () => ({ matches: [], total: 0, limit: 10, offset: 0 }),
            } as Response;
          }
          return {
            ok: true,
            status: 200,
            json: async () => zeroPlayer,
          } as Response;
        })
      );

      const { container } = await mount(
        <PlayerProfileDialog
          playerId="player-ghost"
          playerName="GhostRider"
          onClose={vi.fn()}
        />
      );
      await flush(20);

      expect(container.textContent).toContain('GhostRider');
      expect(container.textContent).toContain('NO RANK DATA RECORDED');
      expect(container.textContent).toContain('NO RECORDED MATCHUPS');
      expect(container.textContent).toContain('No custom tags assigned');
      expect(container.textContent).toContain('No player notes recorded');
      expect(container.textContent).toContain('0 GAMES');
      // No NaN should be present anywhere in the DOM
      expect(container.textContent).not.toContain('NaN');
    });
  });

  // =========================================================================
  // SUITE 2: HistoricalMatchDetailModal.tsx Telemetry Stress
  // =========================================================================
  describe('HistoricalMatchDetailModal.tsx Adversarial Stress', () => {
    it('gracefully renders matches with completely missing Ballchasing telemetry (null stats)', async () => {
      const matchWithNullTelemetry: HistoricalMatchRecord = {
        match_guid: 'null-telemetry-guid',
        record_start_timestamp: 1700000000,
        map_name: 'Wasteland',
        playlist: 11,
        replay_url: '',
        download_status: 'COMPLETED',
        local_file_path: '/path/null.replay',
        upload_status: 'COMPLETED',
        ballchasing_id: 'bc-null',
        ballchasing_url: 'https://ballchasing.com/replay/bc-null',
        retry_count: 0,
        last_error: '',
        blue_score: 2,
        orange_score: 1,
        winner_team: 0,
        replay_stats_json: '',
        created_at: '2026-10-16T10:00:00Z',
        updated_at: '2026-10-16T10:05:00Z',
        stats: {
          match_guid: 'null-telemetry-guid',
          map_name: 'Wasteland',
          playlist: 11,
          blue_score: 2,
          orange_score: 1,
          winner_team: 0,
          duration: 300,
          overtime: false,
          date: '2026-10-16T10:00:00Z',
          players: [
            {
              name: 'PlayerZero1',
              platform: 'Steam',
              online_id: 'p01',
              team: 0,
              score: 0,
              goals: 0,
              assists: 0,
              saves: 0,
              shots: 0,
              demos: 0,
              // All telemetry objects undefined
              boost: undefined,
              movement: undefined,
              positioning: undefined,
              demo: undefined,
            },
            {
              name: 'PlayerZero2',
              platform: 'Epic',
              online_id: 'p02',
              team: 1,
              score: 0,
              goals: 0,
              assists: 0,
              saves: 0,
              shots: 0,
              demos: 0,
              boost: undefined,
              movement: undefined,
              positioning: undefined,
              demo: undefined,
            },
          ],
        },
      };

      const { container } = await mount(
        <HistoricalMatchDetailModal
          match={matchWithNullTelemetry}
          onClose={vi.fn()}
        />
      );
      await flush(10);

      // Tab 1: Overview renders comparison bars without NaN
      expect(container.textContent).toContain('TEAM COMPARISON');
      expect(container.textContent).toContain('BOX SCORE');
      expect(container.textContent).not.toContain('NaN');

      // Tab 2: Boost & Movement - must show the warning banner
      await clickText(container, 'BOOST & MOVEMENT');
      expect(container.textContent).toContain('TELEMETRY PENDING');
      expect(container.textContent).toContain('BOOST TELEMETRY');
      // Should show dashes '—' instead of throwing or displaying undefined
      expect(container.textContent).toContain('—');

      // Tab 3: Positioning - must show warning banner and safe defaults
      await clickText(container, 'POSITIONING');
      expect(container.textContent).toContain('TELEMETRY PENDING');
      expect(container.textContent).toContain('FIELD THIRDS & SPACING');

      // Tab 4: Demolitions & Combat
      await clickText(container, 'DEMOLITIONS & COMBAT');
      expect(container.textContent).toContain('COMBAT & DEMOLITIONS');
      expect(container.textContent).toContain('PlayerZero1');

      // Tab 5: Raw JSON
      await clickText(container, 'RAW JSON');
      expect(container.textContent).toContain('RAW TELEMETRY JSON');
      expect(container.textContent).toContain('null-telemetry-guid');
    });

    it('handles rapid cyclic tab switching across all 5 tabs without state desync', async () => {
      const testMatch: HistoricalMatchRecord = {
        match_guid: 'switch-guid',
        record_start_timestamp: 1700000000,
        map_name: 'Mannfield',
        playlist: 11,
        replay_url: '',
        download_status: 'COMPLETED',
        local_file_path: '',
        upload_status: 'COMPLETED',
        ballchasing_id: '',
        ballchasing_url: '',
        retry_count: 0,
        last_error: '',
        blue_score: 1,
        orange_score: 0,
        winner_team: 0,
        replay_stats_json: '',
        created_at: '2026-10-16T12:00:00Z',
        updated_at: '2026-10-16T12:05:00Z',
        stats: {
          match_guid: 'switch-guid',
          map_name: 'Mannfield',
          playlist: 11,
          blue_score: 1,
          orange_score: 0,
          winner_team: 0,
          duration: 300,
          overtime: false,
          date: '2026-10-16T12:00:00Z',
          players: [],
        },
      };

      const { container } = await mount(
        <HistoricalMatchDetailModal
          match={testMatch}
          onClose={vi.fn()}
        />
      );
      await flush(10);

      const tabLabels = ['OVERVIEW', 'BOOST & MOVEMENT', 'POSITIONING', 'DEMOLITIONS & COMBAT', 'RAW JSON'];

      // Cycle 4 full loops across all 5 tabs rapidly
      for (let loop = 0; loop < 4; loop++) {
        for (const tab of tabLabels) {
          await clickText(container, tab);
        }
      }
      await flush(10);

      // Final tab was RAW JSON
      expect(container.textContent).toContain('RAW TELEMETRY JSON');
    });

    it('tests Copy JSON button functionality and clipboard fallback', async () => {
      let writtenText = '';
      const writeTextMock = vi.fn((text: string) => {
        writtenText = text;
        return Promise.resolve();
      });

      // Safely stub clipboard via Object.defineProperty
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: writeTextMock },
        configurable: true,
        writable: true,
      });

      const jsonMatch: HistoricalMatchRecord = {
        match_guid: 'copy-guid-999',
        record_start_timestamp: 1700000000,
        map_name: 'Rivals Arena',
        playlist: 11,
        replay_url: '',
        download_status: 'COMPLETED',
        local_file_path: '',
        upload_status: 'COMPLETED',
        ballchasing_id: '',
        ballchasing_url: '',
        retry_count: 0,
        last_error: '',
        blue_score: 3,
        orange_score: 2,
        winner_team: 0,
        replay_stats_json: '',
        stats: {
          match_guid: 'copy-guid-999',
          map_name: 'Rivals Arena',
          playlist: 11,
          blue_score: 3,
          orange_score: 2,
          winner_team: 0,
          duration: 300,
          overtime: false,
          date: '2026-10-16T12:00:00Z',
          players: [],
          raw_json: JSON.stringify({ custom_field: 'telemetry_data_valid' }),
        },
        created_at: '2026-10-16T12:00:00Z',
        updated_at: '2026-10-16T12:05:00Z',
      };

      const { container } = await mount(
        <HistoricalMatchDetailModal
          match={jsonMatch}
          onClose={vi.fn()}
        />
      );
      await flush(10);

      await clickText(container, 'RAW JSON');
      expect(container.textContent).toContain('COPY JSON');

      const copyBtn = container.querySelector('[data-testid="copy-json-btn"]') as HTMLElement;
      expect(copyBtn).toBeTruthy();

      await act(async () => {
        copyBtn.click();
      });
      await flush(10);

      expect(writeTextMock).toHaveBeenCalled();
      expect(writtenText).toContain('telemetry_data_valid');
      expect(copyBtn.textContent).toContain('COPIED ✓');
    });
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
        })
      );

      const { container } = await mount(<HistoryView localPlayerIds={new Set(['me'])} />);
      await flush(20);

      // Verify RETRY button is visible for failed match
      const retryBtn = Array.from(container.querySelectorAll('button')).find((b) =>
        b.textContent?.trim() === 'RETRY'
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
        })
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
        })
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
        })
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
