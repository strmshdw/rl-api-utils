// @vitest-environment happy-dom
import React from 'react';
import { describe, it, expect, afterEach, vi } from 'vitest';
import { act } from 'react';
import { DashboardProvider, useDashboard } from '../../context/DashboardContext';
import { emptyRoleStats } from '../../utils/roleStats';
import { cleanup, flush, mount } from '../../test/dom';
import { PlayerDossierView } from './PlayerDossierView';

const BASE_PROFILE = {
  player_id: 'Steam|TestPilot|0',
  platform: 'Steam',
  player_name: 'TestPilot',
  ranks_json: JSON.stringify({
    '11': {
      playlist_id: 11,
      playlist_name: 'Ranked Doubles',
      tier: 18,
      division: 2,
      rank_name: 'Champion III',
      mmr: 1475,
      matches_played: 120,
    },
  }),
  notes: '',
  tags: [],
  first_seen_at: '2026-09-01T10:00:00Z',
  last_seen_at: '2026-10-15T18:00:00Z',
  matchups: [
    {
      player_id: 'Steam|TestPilot|0',
      playlist_id: 11,
      wins_as_teammate: 4,
      losses_as_teammate: 1,
      wins_as_opponent: 2,
      losses_as_opponent: 2,
      total_matches: 9,
      last_played_at: '2026-10-15T18:00:00Z',
    },
  ],
};

describe('PlayerDossierView', () => {
  afterEach(() => {
    localStorage.clear();
    cleanup();
    vi.restoreAllMocks();
  });

  it('renders honest empty telemetry state without fake 420/486/518 metrics when unsynced', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.includes('/matches')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({ matches: [], total: 0, limit: 20, offset: 0 }),
          } as Response;
        }
        return {
          ok: true,
          status: 200,
          json: async () => ({
            ...BASE_PROFILE,
            with_you_stats: emptyRoleStats(5, 4, 1),
            against_you_stats: emptyRoleStats(4, 2, 2),
          }),
        } as Response;
      }),
    );

    const { container } = await mount(
      <DashboardProvider>
        <PlayerDossierView playerId="Steam|TestPilot|0" playerName="TestPilot" />
      </DashboardProvider>,
    );
    await flush(10);

    const withCard = container.querySelector('[data-testid="h2h-with"]')!;
    const vsCard = container.querySelector('[data-testid="h2h-vs"]')!;
    expect(withCard.textContent).toContain('No synced role telemetry');
    expect(vsCard.textContent).toContain('No synced role telemetry');
    expect(withCard.textContent).toContain('—');
    expect(container.textContent).not.toContain('420');
    expect(container.textContent).not.toContain('486');
    expect(container.textContent).not.toContain('518');
  });

  it('renders real synced role telemetry and forwards playlist + date-range query params', async () => {
    const urls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        urls.push(url);
        if (url.includes('/matches')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({
              matches: [
                {
                  match_guid: 'm-100',
                  map_name: 'DFH Stadium',
                  playlist: 11,
                  blue_score: 3,
                  orange_score: 1,
                  created_at: '2026-10-15T18:00:00Z',
                  updated_at: '2026-10-15T18:00:00Z',
                  role: 'opponent',
                  result: 'defeat',
                  local_result: 'victory',
                  stats: {
                    match_guid: 'm-100',
                    map_name: 'DFH Stadium',
                    playlist: 11,
                    blue_score: 3,
                    orange_score: 1,
                    duration: 300,
                    overtime: false,
                    date: '2026-10-15T18:00:00Z',
                    players: [
                      { name: 'TestPilot', platform: 'Steam', online_id: 'TestPilot', team: 1, score: 455, goals: 1, assists: 0, saves: 2, shots: 3, demos: 1, mmr: 1440 },
                      { name: 'You', platform: 'Steam', online_id: 'You', team: 0, score: 510, goals: 3, assists: 1, saves: 1, shots: 5, demos: 0, mmr: 1465 },
                    ],
                  },
                },
              ],
              total: 1,
              limit: 20,
              offset: 0,
            }),
          } as Response;
        }
        return {
          ok: true,
          status: 200,
          json: async () => ({
            ...BASE_PROFILE,
            with_you_stats: emptyRoleStats(5, 4, 1, {
              avg_score: 612,
              avg_goals: 2.4,
              avg_assists: 1.6,
              avg_saves: 2.0,
              avg_shots: 4.5,
              shooting_pct: 53,
              avg_demos_inflicted: 1.2,
              avg_bpm: 445,
              avg_boost: 51,
              avg_supersonic_pct: 22,
            }),
            against_you_stats: emptyRoleStats(4, 2, 2, {
              avg_score: 389,
              avg_goals: 1.1,
              avg_assists: 0.5,
              avg_saves: 1.5,
              avg_shots: 3.0,
              shooting_pct: 37,
              avg_demos_inflicted: 0.8,
              avg_bpm: 390,
              avg_boost: 44,
              avg_supersonic_pct: 16,
            }),
          }),
        } as Response;
      }),
    );

    const DateSetter: React.FC = () => {
      const { setDateRange } = useDashboard();
      return (
        <button type="button" data-testid="set-7d" onClick={() => setDateRange('7d')}>
          SET 7D
        </button>
      );
    };

    const { container } = await mount(
      <DashboardProvider>
        <DateSetter />
        <PlayerDossierView playerId="Steam|TestPilot|0" playerName="TestPilot" />
      </DashboardProvider>,
    );
    await flush(10);

    const withCard = container.querySelector('[data-testid="h2h-with"]')!;
    const vsCard = container.querySelector('[data-testid="h2h-vs"]')!;
    expect(withCard.textContent).toContain('612');
    expect(withCard.textContent).toContain('53%');
    expect(vsCard.textContent).toContain('389');
    expect(container.querySelector('[data-testid="profile-match-row"]')?.textContent).toContain('WIN');

    // Scope playlist to 2V2 and date range to 7D
    const btn2v2 = Array.from(container.querySelectorAll<HTMLButtonElement>('.prof-pl-btn')).find(
      (b) => b.textContent?.trim() === '2V2',
    )!;
    await act(async () => btn2v2.click());
    await act(async () => container.querySelector<HTMLButtonElement>('[data-testid="set-7d"]')!.click());
    await flush(10);

    expect(urls.some((u) => u.includes('/api/players/Steam%7CTestPilot%7C0?') && u.includes('playlist=11') && u.includes('from='))).toBe(true);
    expect(urls.some((u) => u.includes('/api/players/Steam%7CTestPilot%7C0/matches?') && u.includes('playlist=11') && u.includes('from='))).toBe(true);
  });
});
