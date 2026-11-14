// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act } from 'react';
import { mount, cleanup, flush, clickText } from './test/dom';

import { PlayerProfileDialog } from './components/players/PlayerProfileDialog';
import { HistoricalMatchDetailModal } from './components/historical/HistoricalMatchDetailModal';
import { HistoricalMatchRecord, PlayerDetailResponse, PlayerMatchesResponse } from './types/api';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

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
      { player_id: id, playlist_id: 11, wins_as_teammate: 4, losses_as_teammate: 2, wins_as_opponent: 3, losses_as_opponent: 3, total_matches: 12, last_played_at: '2026-10-15T18:00:00Z' },
      { player_id: id, playlist_id: 13, wins_as_teammate: 2, losses_as_teammate: 1, wins_as_opponent: 1, losses_as_opponent: 2, total_matches: 6, last_played_at: '2026-10-14T12:00:00Z' },
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
            { name: 'InspectedPlayer', platform: 'Steam', online_id: playerId, team: 0, score: 510, goals: 3, assists: 1, saves: 2, shots: 5, demos: 1 },
            { name: nextPlayerName || 'TeammateTwo', platform: 'Epic', online_id: nextPlayerId || 'mate-99', team: 0, score: 380, goals: 2, assists: 2, saves: 1, shots: 3, demos: 0 },
            { name: 'OpponentOne', platform: 'Steam', online_id: 'opp-01', team: 1, score: 420, goals: 2, assists: 1, saves: 3, shots: 4, demos: 2 },
          ],
        },
      },
    ],
    total: 1,
    limit: 10,
    offset: 0,
  };
}

function makeHistoricalMatch(
  overrides: Partial<HistoricalMatchRecord> & {
    match_guid: string;
    map_name: string;
    blue_score: number;
    orange_score: number;
    stats: HistoricalMatchRecord['stats'];
  },
): HistoricalMatchRecord {
  return {
    record_start_timestamp: 1700000000,
    playlist: 11,
    replay_url: '',
    download_status: 'COMPLETED',
    local_file_path: '',
    upload_status: 'COMPLETED',
    ballchasing_id: '',
    ballchasing_url: '',
    retry_count: 0,
    last_error: '',
    winner_team: 0,
    replay_stats_json: '',
    created_at: '2026-10-16T12:00:00Z',
    updated_at: '2026-10-16T12:05:00Z',
    ...overrides,
  };
}

describe('M4 ADVERSARIAL CHALLENGER SUITE 1A: DRILL-DOWNS & MODALS', () => {
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
              return { ok: true, status: 200, json: async () => createMockMatches(pId, nextPId, nextPName) } as Response;
            }
            if (url.includes(`/api/players/${pId}`)) {
              return { ok: true, status: 200, json: async () => createMockPlayer(pId, pName) } as Response;
            }
          }
          return { ok: true, status: 200, json: async () => ({}) } as Response;
        }),
      );

      const { container } = await mount(
        <PlayerProfileDialog
          playerId="player-alpha"
          playerName="Alpha"
          onClose={vi.fn()}
          localPlayerIds={new Set(['local-me'])}
        />,
      );
      await flush(20);

      expect(container.textContent).toContain('Alpha');

      // Navigate 5 hops: Alpha -> Beta -> Gamma -> Delta -> Epsilon -> Zeta
      for (let hop = 0; hop < 5; hop++) {
        const matchRow = container.querySelector('[data-testid="profile-match-row"]');
        expect(matchRow).toBeTruthy();
        await act(async () => {
          (matchRow as HTMLElement)?.click();
        });
        await flush(10);

        const nextName = playerNames[hop + 1];
        const nextBtn = Array.from(container.querySelectorAll('button')).find((b) =>
          b.textContent?.includes(nextName),
        );
        expect(nextBtn).toBeTruthy();

        await act(async () => {
          nextBtn?.click();
        });
        await flush(25);

        expect(container.textContent).toContain(playerNames[hop + 1]);
        expect(container.textContent).toContain(`← BACK TO ${playerNames[hop]}`);
      }

      // Now we are at Zeta (hop 5). Unwind all 5 hops using the back button.
      for (let unwound = 4; unwound >= 0; unwound--) {
        const backBtn = Array.from(container.querySelectorAll('button')).find((b) =>
          b.textContent?.includes('← BACK TO'),
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
        b.textContent?.includes('← BACK TO'),
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
            return { ok: true, status: 200, json: async () => createMockMatches('player-stress') } as Response;
          }
          return { ok: true, status: 200, json: async () => createMockPlayer('player-stress', 'StressTester') } as Response;
        }),
      );

      const { container } = await mount(
        <PlayerProfileDialog playerId="player-stress" playerName="StressTester" onClose={vi.fn()} />,
      );
      await flush(20);

      const withCard = container.querySelector('[data-testid="h2h-with"]') as HTMLElement;
      const vsCard = container.querySelector('[data-testid="h2h-vs"]') as HTMLElement;
      const playlistRows = () =>
        Array.from(container.querySelectorAll<HTMLElement>('[data-testid="profile-playlist-row"]'));

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

      const clearBtn = Array.from(container.querySelectorAll('button')).find((b) =>
        b.textContent?.includes('CLEAR FILTERS'),
      );
      if (clearBtn) {
        await act(async () => {
          clearBtn.click();
        });
        await flush(10);
      }

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
            return { ok: true, status: 200, json: async () => ({ matches: [], total: 0, limit: 10, offset: 0 }) } as Response;
          }
          return { ok: true, status: 200, json: async () => zeroPlayer } as Response;
        }),
      );

      const { container } = await mount(
        <PlayerProfileDialog playerId="player-ghost" playerName="GhostRider" onClose={vi.fn()} />,
      );
      await flush(20);

      expect(container.textContent).toContain('GhostRider');
      expect(container.textContent).toContain('NO RANK DATA RECORDED');
      expect(container.textContent).toContain('NO RECORDED MATCHUPS');
      expect(container.textContent).toContain('No custom tags assigned');
      expect(container.textContent).toContain('No player notes recorded');
      expect(container.textContent).toContain('0 GAMES');
      expect(container.textContent).not.toContain('NaN');
    });
  });

  // =========================================================================
  // SUITE 2: HistoricalMatchDetailModal.tsx Telemetry Stress
  // =========================================================================
  describe('HistoricalMatchDetailModal.tsx Adversarial Stress', () => {
    it('gracefully renders matches with completely missing Ballchasing telemetry (null stats)', async () => {
      const matchWithNullTelemetry = makeHistoricalMatch({
        match_guid: 'null-telemetry-guid',
        map_name: 'Wasteland',
        local_file_path: '/path/null.replay',
        ballchasing_id: 'bc-null',
        ballchasing_url: 'https://ballchasing.com/replay/bc-null',
        blue_score: 2,
        orange_score: 1,
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
              name: 'PlayerZero1', platform: 'Steam', online_id: 'p01', team: 0,
              score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0,
              boost: undefined, movement: undefined, positioning: undefined, demo: undefined,
            },
            {
              name: 'PlayerZero2', platform: 'Epic', online_id: 'p02', team: 1,
              score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0,
              boost: undefined, movement: undefined, positioning: undefined, demo: undefined,
            },
          ],
        },
      });

      const { container } = await mount(<HistoricalMatchDetailModal match={matchWithNullTelemetry} onClose={vi.fn()} />);
      await flush(10);

      expect(container.textContent).toContain('1. CORE & MECHANICS');
      expect(container.textContent).toContain('BLUE TEAM TOTALS / AVG');
      expect(container.textContent).toContain('PlayerZero1');
      expect(container.textContent).not.toContain('NaN');

      await clickText(container, '2. BOOST ECONOMY & PADS');
      expect(container.textContent).toContain('TELEMETRY PENDING');
      expect(container.textContent).toContain('AVG BST');
      expect(container.textContent).toContain('—');

      await clickText(container, '3. SPEED & MOVEMENT');
      expect(container.textContent).toContain('TELEMETRY PENDING');
      expect(container.textContent).toContain('SUPERSONIC');
      expect(container.textContent).toContain('—');

      await clickText(container, '4. POSITIONING & ROTATION');
      expect(container.textContent).toContain('TELEMETRY PENDING');
      expect(container.textContent).toContain('BEHIND BALL');

      await clickText(container, '5. TEAM HEAD-TO-HEAD');
      expect(container.textContent).toContain('TEAM SCORE (PTS)');
      expect(container.textContent).toContain('DEMOLITIONS INFLICTED');
      expect(container.textContent).not.toContain('NaN');

      await clickText(container, 'RAW JSON');
      expect(container.textContent).toContain('RAW TELEMETRY JSON');
      expect(container.textContent).toContain('null-telemetry-guid');
    });

    it('handles rapid cyclic tab switching across all 5 tabs without state desync', async () => {
      const testMatch = makeHistoricalMatch({
        match_guid: 'switch-guid',
        map_name: 'Mannfield',
        blue_score: 1,
        orange_score: 0,
        stats: {
          match_guid: 'switch-guid', map_name: 'Mannfield', playlist: 11,
          blue_score: 1, orange_score: 0, winner_team: 0, duration: 300, overtime: false,
          date: '2026-10-16T12:00:00Z', players: [],
        },
      });

      const { container } = await mount(<HistoricalMatchDetailModal match={testMatch} onClose={vi.fn()} />);
      await flush(10);

      const tabLabels = [
        '1. CORE & MECHANICS',
        '2. BOOST ECONOMY & PADS',
        '3. SPEED & MOVEMENT',
        '4. POSITIONING & ROTATION',
        '5. TEAM HEAD-TO-HEAD',
        'RAW JSON',
      ];

      for (let loop = 0; loop < 4; loop++) {
        for (const tab of tabLabels) {
          await clickText(container, tab);
        }
      }
      await flush(10);

      expect(container.textContent).toContain('RAW TELEMETRY JSON');
    });

    it('tests Copy JSON button functionality and clipboard fallback', async () => {
      let writtenText = '';
      const writeTextMock = vi.fn((text: string) => {
        writtenText = text;
        return Promise.resolve();
      });

      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: writeTextMock },
        configurable: true,
        writable: true,
      });

      const jsonMatch = makeHistoricalMatch({
        match_guid: 'copy-guid-999',
        map_name: 'Rivals Arena',
        blue_score: 3,
        orange_score: 2,
        stats: {
          match_guid: 'copy-guid-999', map_name: 'Rivals Arena', playlist: 11,
          blue_score: 3, orange_score: 2, winner_team: 0, duration: 300, overtime: false,
          date: '2026-10-16T12:00:00Z', players: [],
          raw_json: JSON.stringify({ custom_field: 'telemetry_data_valid' }),
        },
      });

      const { container } = await mount(<HistoricalMatchDetailModal match={jsonMatch} onClose={vi.fn()} />);
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
});
