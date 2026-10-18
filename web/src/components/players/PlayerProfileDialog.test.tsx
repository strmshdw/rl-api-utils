// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { act } from 'react';
import { PlayerProfileDialog } from './PlayerProfileDialog';
import { cleanup, clickText, flush, mount } from '../../test/dom';

const MOCK_PROFILE = {
  player_id: 'player-test-01',
  platform: 'Steam',
  player_name: 'TargetPlayer',
  ranks_json: JSON.stringify({
    '11': {
      playlist_id: 11,
      playlist_name: 'Ranked Doubles',
      tier: 16,
      division: 2,
      rank_name: 'Grand Champion I',
      mmr: 1515,
      matches_played: 240,
    },
  }),
  notes: 'Skilled aerialist',
  tags: ['clutch', 'reliable'],
  first_seen_at: '2026-09-01T10:00:00Z',
  last_seen_at: '2026-10-15T18:00:00Z',
  matchups: [
    {
      player_id: 'player-test-01',
      playlist_id: 11,
      wins_as_teammate: 5,
      losses_as_teammate: 2,
      wins_as_opponent: 3,
      losses_as_opponent: 4,
      total_matches: 14,
      last_played_at: '2026-10-15T18:00:00Z',
    },
  ],
};

const MOCK_MATCHES_RESPONSE = {
  matches: [
    {
      match_guid: 'm-guid-001',
      date: '2026-10-15T18:00:00Z',
      playlist: 11,
      map_name: 'Utopia Coliseum',
      duration: 310,
      blue_score: 4,
      orange_score: 2,
      winner_team: 0,
      result: 'victory',
      role: 'teammate',
      player_won: true,
      player_team: 0,
      player_stats: {
        score: 410,
        goals: 2,
        assists: 1,
        saves: 1,
        shots: 3,
        demos: 1,
      },
      players: [
        {
          name: 'TargetPlayer',
          platform: 'Steam',
          online_id: 'player-test-01',
          team: 0,
          score: 410,
          goals: 2,
          assists: 1,
          saves: 1,
          shots: 3,
          demos: 1,
        },
        {
          name: 'You',
          platform: 'Steam',
          online_id: 'local-player-id',
          team: 0,
          score: 350,
          goals: 2,
          assists: 2,
          saves: 0,
          shots: 4,
          demos: 0,
        },
      ],
    },
  ],
  total: 1,
  limit: 10,
  offset: 0,
};

describe('PlayerProfileDialog', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method || 'GET';

        if (url.includes('/api/players/player-test-01/matches')) {
          return {
            ok: true,
            status: 200,
            json: async () => MOCK_MATCHES_RESPONSE,
          } as Response;
        }

        if (url.includes('/api/players/player-test-01') && method === 'PATCH') {
          return {
            ok: true,
            status: 200,
            json: async () => ({ status: 'ok', player: MOCK_PROFILE }),
          } as Response;
        }

        if (url.includes('/api/players/player-test-01')) {
          return {
            ok: true,
            status: 200,
            json: async () => MOCK_PROFILE,
          } as Response;
        }

        return { ok: true, status: 200, json: async () => ({}) } as Response;
      })
    );
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('renders profile overview, badges, notes, and playlists', async () => {
    const { container } = await mount(
      <PlayerProfileDialog
        playerId="player-test-01"
        playerName="TargetPlayer"
        onClose={vi.fn()}
        localPlayerIds={new Set(['local-player-id'])}
      />
    );
    await flush(10);

    expect(container.textContent).toContain('TargetPlayer');
    expect(container.textContent).toContain('clutch');
    expect(container.textContent).toContain('reliable');
    expect(container.textContent).toContain('Skilled aerialist');
    expect(container.textContent).toContain('WITH YOU');
    expect(container.textContent).toContain('VS YOU');
    expect(container.textContent).toContain('RANKED DOUBLES');
  });

  it('toggles interactive filter cards (WITH YOU / VS YOU)', async () => {
    const { container } = await mount(
      <PlayerProfileDialog
        playerId="player-test-01"
        playerName="TargetPlayer"
        onClose={vi.fn()}
      />
    );
    await flush(10);

    const withYouCard = container.querySelector('[data-testid="h2h-with"]');
    expect(withYouCard).toBeTruthy();

    await act(async () => {
      (withYouCard as HTMLElement)?.click();
    });

    expect(container.textContent).toContain('WITH YOU (TEAMMATE)');
  });

  it('toggles Box Score accordion inside match history', async () => {
    const { container } = await mount(
      <PlayerProfileDialog
        playerId="player-test-01"
        playerName="TargetPlayer"
        onClose={vi.fn()}
        localPlayerIds={new Set(['local-player-id'])}
      />
    );
    await flush(10);

    const matchRow = container.querySelector('[data-testid="profile-match-row"]');
    expect(matchRow).toBeTruthy();

    // Click row to expand Level 1 Box Score
    await act(async () => {
      (matchRow as HTMLElement)?.click();
    });

    expect(container.textContent).toContain('FULL TELEMETRY →');
    expect(container.querySelector('[data-testid="box-score-blue"]')).toBeTruthy();
  });

  it('opens and interacts with notes modal', async () => {
    const { container } = await mount(
      <PlayerProfileDialog
        playerId="player-test-01"
        playerName="TargetPlayer"
        onClose={vi.fn()}
      />
    );
    await flush(10);

    await clickText(container, 'EDIT NOTES & TAGS');
    expect(container.textContent).toContain('EDIT SCOUTING NOTES & TAGS');
  });

  it('displays WIN badge from local perspective when opponent lost (role=opponent, result=defeat)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.includes('/api/players/player-test-01/matches')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({
              matches: [
                {
                  ...MOCK_MATCHES_RESPONSE.matches[0],
                  role: 'opponent',
                  result: 'defeat', // Opponent lost -> You won!
                },
              ],
              total: 1,
              limit: 10,
              offset: 0,
            }),
          } as Response;
        }
        return {
          ok: true,
          status: 200,
          json: async () => MOCK_PROFILE,
        } as Response;
      })
    );

    const { container } = await mount(
      <PlayerProfileDialog
        playerId="player-test-01"
        playerName="TargetPlayer"
        onClose={vi.fn()}
      />
    );
    await flush(10);

    const matchRow = container.querySelector('[data-testid="profile-match-row"]');
    expect(matchRow).toBeTruthy();
    expect(matchRow?.textContent).toContain('WIN');
    expect(matchRow?.textContent).toContain('VS YOU');
  });
});

