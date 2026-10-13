// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';

import { PlayerRow } from './PlayerRow';
import { RosterTable } from './RosterTable';
import { LiveGameView } from './LiveGameView';
import { MatchDetailModal } from '../session/MatchDetailModal';
import { ColumnConfig } from '../../types/columns';
import { LobbyPlayer, SessionMatchDetail, CurrentMatchResponse } from '../../types/api';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useSession } from '../../hooks/useSession';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('../../hooks/useLiveMatch', () => ({
  useLiveMatch: vi.fn(),
}));

vi.mock('../../hooks/useSession', () => ({
  useSession: vi.fn(),
}));

const cleanHtml = (s: string) => s.replace(/<!--.*?-->/g, '');

const allColumns: ColumnConfig = {
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

function createPlayerFixture(overrides: Partial<LobbyPlayer> = {}): LobbyPlayer {
  return {
    player_id: `Steam|test_player_${Math.random().toString(36).substring(7)}|0`,
    name: 'TestPlayer',
    platform: 'Steam',
    team_num: 0,
    is_local: false,
    is_bot: false,
    is_disconnected: false,
    stats: { score: 350, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0 },
    current_rank: {
      playlist_id: 11,
      playlist_name: 'Ranked Standard 3v3',
      tier: 16,
      division: 2,
      rank_name: 'Champion I Div II',
      mmr: 1040.0,
    },
    matchup_record: {
      player_id: 'Steam|test_player|0',
      playlist_id: 11,
      wins_as_teammate: 5,
      losses_as_teammate: 2,
      wins_as_opponent: 3,
      losses_as_opponent: 4,
      total_matches: 14,
      last_played_at: '2026-10-06T00:00:00Z',
    },
    ...overrides,
  };
}

describe('Disconnected Player UX & Data Integrity Adversarial Stress Suite', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    localStorage.clear();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    vi.clearAllMocks();
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    localStorage.clear();
    vi.restoreAllMocks();
  });

  // =========================================================================
  // SUITE 1: Visual Treatment & Badge Specification Assertions
  // =========================================================================
  describe('Suite 1: Visual Treatment & Badge Specification', () => {
    it('applies exact CSS utility classes (opacity-65, saturate-60, hover:opacity-85) to disconnected row', () => {
      const disconnectedPlayer = createPlayerFixture({
        name: 'GhostRider',
        is_disconnected: true,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={disconnectedPlayer}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const row = container.querySelector('tbody tr');
      expect(row).not.toBeNull();
      expect(row?.className).toContain('opacity-65');
      expect(row?.className).toContain('saturate-60');
      expect(row?.className).toContain('hover:opacity-85');
    });

    it('renders data-testid="badge-disconnected" with uppercase text and WifiOff icon', () => {
      const disconnectedPlayer = createPlayerFixture({
        name: 'Leaver_1',
        is_disconnected: true,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={disconnectedPlayer}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const badge = container.querySelector('[data-testid="badge-disconnected"]');
      expect(badge).not.toBeNull();
      expect(badge?.textContent).toContain('DISCONNECTED');

      // WifiOff icon verification
      const svg = badge?.querySelector('svg');
      expect(svg).not.toBeNull();
      expect(svg?.classList.contains('text-rose-400')).toBe(true);

      // Verify title tooltip explanation
      expect(badge?.getAttribute('title')).toBe(
        'Player disconnected early; stats and match outcomes are preserved'
      );
    });

    it('correctly handles disconnected local player with both YOU and DISCONNECTED badges', () => {
      const localDisconnected = createPlayerFixture({
        name: 'MyHero',
        is_local: true,
        is_disconnected: true,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={localDisconnected}
                isLocal={true}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const row = container.querySelector('tbody tr');
      expect(row?.className).toContain('bg-cyan-500/10');
      expect(row?.className).toContain('opacity-65');
      expect(row?.className).toContain('saturate-60');

      const identityCell = container.querySelector('[data-testid="player-identity"]');
      expect(identityCell?.textContent).toContain('YOU');
      expect(identityCell?.textContent).toContain('DISCONNECTED');
      expect(identityCell?.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
    });

    it('correctly handles disconnected bot player with both BOT and DISCONNECTED badges', () => {
      const botDisconnected = createPlayerFixture({
        name: 'Heater',
        is_bot: true,
        is_disconnected: true,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={botDisconnected}
                isLocal={false}
                isTeammate={false}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const identityCell = container.querySelector('[data-testid="player-identity"]');
      expect(identityCell?.textContent).toContain('BOT');
      expect(identityCell?.textContent).toContain('DISCONNECTED');
      expect(identityCell?.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
    });

    it('renders platform icon cleanly without layout conflict across platforms when disconnected', () => {
      const platforms: Array<LobbyPlayer['platform']> = ['Steam', 'Epic', 'PlayStation', 'Xbox', 'Nintendo'];

      platforms.forEach((plat) => {
        const player = createPlayerFixture({
          platform: plat,
          is_disconnected: true,
        });

        const html = cleanHtml(
          renderToString(
            <table>
              <tbody>
                <PlayerRow
                  player={player}
                  isLocal={false}
                  isTeammate={true}
                  columnConfig={allColumns}
                  activePlaylistId={11}
                />
              </tbody>
            </table>
          )
        );

        expect(html).toContain('data-testid="badge-disconnected"');
        expect(html).toContain('DISCONNECTED');
        expect(html).toContain('opacity-65 saturate-60 hover:opacity-85');
      });
    });
  });

  // =========================================================================
  // SUITE 2: Active Connected Player Isolation & Negation
  // =========================================================================
  describe('Suite 2: Active Connected Player Isolation & Negation', () => {
    it('does NOT apply opacity-65, saturate-60, or hover:opacity-85 when is_disconnected is false', () => {
      const activePlayer = createPlayerFixture({
        is_disconnected: false,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={activePlayer}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const row = container.querySelector('tbody tr');
      expect(row?.className).not.toContain('opacity-65');
      expect(row?.className).not.toContain('saturate-60');
      expect(row?.className).not.toContain('hover:opacity-85');
      expect(container.querySelector('[data-testid="badge-disconnected"]')).toBeNull();
    });

    it('does NOT apply disconnected styling when is_disconnected is undefined', () => {
      const activePlayer = createPlayerFixture();
      delete activePlayer.is_disconnected;

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={activePlayer}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const row = container.querySelector('tbody tr');
      expect(row?.className).not.toContain('opacity-65');
      expect(row?.className).not.toContain('saturate-60');
      expect(container.querySelector('[data-testid="badge-disconnected"]')).toBeNull();
    });

    it('strictly isolates disconnected styling to only disconnected rows in a mixed roster', () => {
      const p1Connected = createPlayerFixture({ player_id: 'p1', name: 'Connected_1', is_disconnected: false });
      const p2Disconnected = createPlayerFixture({ player_id: 'p2', name: 'Disconnected_2', is_disconnected: true });
      const p3Connected = createPlayerFixture({ player_id: 'p3', name: 'Connected_3', is_disconnected: false });
      const p4Disconnected = createPlayerFixture({ player_id: 'p4', name: 'Disconnected_4', is_disconnected: true });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[p1Connected, p2Disconnected, p3Connected, p4Disconnected]}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      const rows = container.querySelectorAll('tbody tr');
      expect(rows.length).toBe(4);

      // Verify exact count of disconnected badges in the table
      const badges = container.querySelectorAll('[data-testid="badge-disconnected"]');
      expect(badges.length).toBe(2);

      // Map rows to player identity and verify styling isolation
      rows.forEach((row) => {
        const text = row.querySelector('[data-testid="player-identity"]')?.textContent || '';
        const isDc = text.includes('Disconnected');
        if (isDc) {
          expect(row.className).toContain('opacity-65');
          expect(row.className).toContain('saturate-60');
          expect(row.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
        } else {
          expect(row.className).not.toContain('opacity-65');
          expect(row.className).not.toContain('saturate-60');
          expect(row.querySelector('[data-testid="badge-disconnected"]')).toBeNull();
        }
      });
    });
  });

  // =========================================================================
  // SUITE 3: Stat Retention & Leaderboard Sorting Invariants
  // =========================================================================
  describe('Suite 3: Stat Retention & Leaderboard Sorting Invariants', () => {
    it('preserves all stat numbers verbatim when player is disconnected', () => {
      const highStatDisconnected = createPlayerFixture({
        name: 'SuperLeaver',
        is_disconnected: true,
        stats: {
          score: 875,
          goals: 4,
          assists: 2,
          saves: 3,
          shots: 7,
          demos: 2,
        },
        current_rank: {
          playlist_id: 11,
          playlist_name: 'Ranked 3v3',
          tier: 17,
          division: 3,
          rank_name: 'Champion II Div III',
          mmr: 1120.5,
        },
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={highStatDisconnected}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      expect(container.querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('875');
      expect(container.querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('4');
      expect(container.querySelector('[data-testid="stat-assists"]')?.textContent?.trim()).toBe('2');
      expect(container.querySelector('[data-testid="stat-saves"]')?.textContent?.trim()).toBe('3');
      expect(container.querySelector('[data-testid="stat-shots"]')?.textContent?.trim()).toBe('7');
      expect(container.querySelector('[data-testid="stat-demos"]')?.textContent?.trim()).toBe('2');
      expect(container.querySelector('[data-testid="stat-mmr"]')?.textContent?.trim()).toBe('1120.5');

      // Positive goals still receive amber highlighting
      const goalsCell = container.querySelector('[data-testid="stat-goals"]');
      expect(goalsCell?.className).toContain('text-amber-400');
    });

    it('safely handles disconnected player with zero or missing stats without throwing', () => {
      const zeroStatDisconnected = createPlayerFixture({
        name: 'InstantLeaver',
        is_disconnected: true,
        stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={zeroStatDisconnected}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      expect(container.querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('0');
      expect(container.querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('0');
      // Zero goals use muted text-slate-500
      expect(container.querySelector('[data-testid="stat-goals"]')?.className).toContain('text-slate-500');
    });

    it('maintains strict score-descending sort order when a disconnected player has top score', () => {
      const disconnectedTop = createPlayerFixture({
        player_id: 'p_dc_top',
        name: 'TopScorer_DC',
        is_disconnected: true,
        stats: { score: 950, goals: 3, assists: 1, saves: 2, shots: 5, demos: 1 },
      });

      const connectedMid = createPlayerFixture({
        player_id: 'p_conn_mid',
        name: 'MidScorer_Active',
        is_disconnected: false,
        stats: { score: 400, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0 },
      });

      const connectedLow = createPlayerFixture({
        player_id: 'p_conn_low',
        name: 'LowScorer_Active',
        is_disconnected: false,
        stats: { score: 200, goals: 0, assists: 0, saves: 1, shots: 1, demos: 0 },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[connectedLow, disconnectedTop, connectedMid]}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      const rows = container.querySelectorAll('tbody tr');
      expect(rows.length).toBe(3);

      // First row MUST be disconnectedTop (score 950)
      expect(rows[0].querySelector('[data-testid="player-identity"]')?.textContent).toContain('TopScorer_DC');
      expect(rows[0].querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('950');
      expect(rows[0].querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();

      // Second row MUST be connectedMid (score 400)
      expect(rows[1].querySelector('[data-testid="player-identity"]')?.textContent).toContain('MidScorer_Active');
      expect(rows[1].querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('400');
      expect(rows[1].querySelector('[data-testid="badge-disconnected"]')).toBeNull();

      // Third row MUST be connectedLow (score 200)
      expect(rows[2].querySelector('[data-testid="player-identity"]')?.textContent).toContain('LowScorer_Active');
      expect(rows[2].querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('200');
    });
  });

  // =========================================================================
  // SUITE 4: Team Goals & Score Aggregation Stress Engine
  // =========================================================================
  describe('Suite 4: Team Goals & Score Aggregation Stress Engine', () => {
    it('aggregates goals and score accurately across mixed connected and disconnected players', () => {
      const p1 = createPlayerFixture({
        is_disconnected: false,
        stats: { score: 320, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 },
      });
      const p2 = createPlayerFixture({
        is_disconnected: true,
        stats: { score: 450, goals: 2, assists: 1, saves: 1, shots: 3, demos: 1 },
      });
      const p3 = createPlayerFixture({
        is_disconnected: true,
        stats: { score: 180, goals: 0, assists: 1, saves: 0, shots: 1, demos: 0 },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[p1, p2, p3]}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      // Total goals: 1 + 2 + 0 = 3
      const goalsBadge = container.querySelector('[data-testid="team-goals"]');
      expect(goalsBadge?.textContent).toContain('3');

      // Total score: 320 + 450 + 180 = 950
      const scoreBadge = container.querySelector('[data-testid="team-score"]');
      expect(scoreBadge?.textContent).toContain('950');
    });

    it('accurately aggregates when 100% of team players are disconnected', () => {
      const p1 = createPlayerFixture({
        is_disconnected: true,
        stats: { score: 500, goals: 2, assists: 0, saves: 2, shots: 4, demos: 0 },
      });
      const p2 = createPlayerFixture({
        is_disconnected: true,
        stats: { score: 300, goals: 1, assists: 2, saves: 0, shots: 2, demos: 0 },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={1}
            teamName="Orange Team"
            players={[p1, p2]}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      const goalsBadge = container.querySelector('[data-testid="team-goals"]');
      expect(goalsBadge?.textContent).toContain('3');

      const scoreBadge = container.querySelector('[data-testid="team-score"]');
      expect(scoreBadge?.textContent).toContain('800');
    });

    it('renders 0 goals and 0 score when roster is empty', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[]}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      const goalsBadge = container.querySelector('[data-testid="team-goals"]');
      expect(goalsBadge?.textContent).toContain('0');

      const scoreBadge = container.querySelector('[data-testid="team-score"]');
      expect(scoreBadge?.textContent).toContain('0');
    });

    it('stress harness: accurately aggregates 30 players with pseudo-random stats and disconnect flags', () => {
      const players: LobbyPlayer[] = [];
      let expectedGoals = 0;
      let expectedScore = 0;

      for (let i = 0; i < 30; i++) {
        const score = (i * 73 + 15) % 1200;
        const goals = (i * 3 + 1) % 6;
        const isDc = i % 3 === 0; // Every 3rd player disconnected

        expectedGoals += goals;
        expectedScore += score;

        players.push(
          createPlayerFixture({
            player_id: `player_stress_${i}`,
            name: `Player_${i}`,
            is_disconnected: isDc,
            stats: { score, goals, assists: 0, saves: 0, shots: goals, demos: 0 },
          })
        );
      }

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={players}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      const goalsBadge = container.querySelector('[data-testid="team-goals"]');
      expect(goalsBadge?.textContent).toContain(expectedGoals.toString());

      const scoreBadge = container.querySelector('[data-testid="team-score"]');
      expect(scoreBadge?.textContent).toContain(expectedScore.toString());
    });
  });

  // =========================================================================
  // SUITE 5: Concluded Match Parity in MatchDetailModal
  // =========================================================================
  describe('Suite 5: Concluded Match Parity in MatchDetailModal', () => {
    it('renders disconnected badges and styling for disconnected players across both Blue and Orange teams in MatchDetailModal', () => {
      const mockDetail: SessionMatchDetail = {
        match_guid: 'modal-adversarial-guid',
        playlist_id: 11,
        playlist_name: 'Ranked 3v3 Standard',
        started_at: '2026-10-06T11:00:00Z',
        ended_at: '2026-10-06T11:06:00Z',
        duration_seconds: 360,
        result: 'victory',
        local_team: 0,
        winner_team: 0,
        blue_score: 4,
        orange_score: 2,
        starting_mmr: 1040,
        ending_mmr: 1049,
        mmr_change: 9,
        players: [
          // Blue Team
          {
            player_id: 'Steam|blue_1|0',
            name: 'BlueActive',
            platform: 'Steam',
            team_num: 0,
            is_local: true,
            is_bot: false,
            is_disconnected: false,
            stats: { score: 620, goals: 2, assists: 1, saves: 1, shots: 4, demos: 1 },
            rank_name: 'Champion I Div II',
            tier: 16,
            division: 2,
            mmr: 1049,
          },
          {
            player_id: 'Steam|blue_dc|0',
            name: 'BlueLeaver',
            platform: 'Steam',
            team_num: 0,
            is_local: false,
            is_bot: false,
            is_disconnected: true,
            stats: { score: 350, goals: 2, assists: 0, saves: 0, shots: 3, demos: 0 },
            rank_name: 'Champion I Div I',
            tier: 16,
            division: 1,
            mmr: 1025,
          },
          // Orange Team
          {
            player_id: 'Epic|orange_1|0',
            name: 'OrangeActive',
            platform: 'Epic',
            team_num: 1,
            is_local: false,
            is_bot: false,
            is_disconnected: false,
            stats: { score: 480, goals: 1, assists: 1, saves: 3, shots: 3, demos: 0 },
            rank_name: 'Champion I Div II',
            tier: 16,
            division: 2,
            mmr: 1038,
          },
          {
            player_id: 'Epic|orange_dc|0',
            name: 'OrangeRageQuitter',
            platform: 'Epic',
            team_num: 1,
            is_local: false,
            is_bot: false,
            is_disconnected: true,
            stats: { score: 210, goals: 1, assists: 0, saves: 1, shots: 1, demos: 1 },
            rank_name: 'Champion I Div I',
            tier: 16,
            division: 1,
            mmr: 1020,
          },
        ],
      };

      act(() => {
        root.render(<MatchDetailModal match={mockDetail} onClose={() => {}} />);
      });

      // Verify BlueLeaver has disconnected styling & badge
      const badges = container.querySelectorAll('[data-testid="badge-disconnected"]');
      expect(badges.length).toBe(2);

      const rows = container.querySelectorAll('tbody tr');
      expect(rows.length).toBe(4);

      // Verify BlueLeaver row
      const blueDcRow = Array.from(rows).find((r) => r.textContent?.includes('BlueLeaver'));
      expect(blueDcRow).not.toBeUndefined();
      expect(blueDcRow?.className).toContain('opacity-65');
      expect(blueDcRow?.className).toContain('saturate-60');
      expect(blueDcRow?.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
      expect(blueDcRow?.textContent).toContain('350'); // Stat retention

      // Verify OrangeRageQuitter row
      const orangeDcRow = Array.from(rows).find((r) => r.textContent?.includes('OrangeRageQuitter'));
      expect(orangeDcRow).not.toBeUndefined();
      expect(orangeDcRow?.className).toContain('opacity-65');
      expect(orangeDcRow?.className).toContain('saturate-60');
      expect(orangeDcRow?.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
      expect(orangeDcRow?.textContent).toContain('210'); // Stat retention

      // Verify active players do NOT have disconnected badge
      const blueActiveRow = Array.from(rows).find((r) => r.textContent?.includes('BlueActive'));
      expect(blueActiveRow?.className).not.toContain('opacity-65');
      expect(blueActiveRow?.querySelector('[data-testid="badge-disconnected"]')).toBeNull();

      const orangeActiveRow = Array.from(rows).find((r) => r.textContent?.includes('OrangeActive'));
      expect(orangeActiveRow?.className).not.toContain('opacity-65');
      expect(orangeActiveRow?.querySelector('[data-testid="badge-disconnected"]')).toBeNull();
    });

    it('returns null gracefully without rendering modal DOM when match is null', () => {
      act(() => {
        root.render(<MatchDetailModal match={null} onClose={() => {}} />);
      });

      expect(container.innerHTML).toBe('');
    });
  });

  // =========================================================================
  // SUITE 6: Dynamic Reconnection Lifecycle & State Transitions
  // =========================================================================
  describe('Suite 6: Dynamic Reconnection Lifecycle & State Transitions', () => {
    it('simulates full live match lifecycle: active -> score -> disconnect -> reconnect -> final outcome', () => {
      const player1Id = 'Steam|p1_anchor|0';
      const player2Id = 'Steam|p2_dynamic|0';

      // Frame 1: Match starts, both players active
      const frame1Players: LobbyPlayer[] = [
        createPlayerFixture({
          player_id: player1Id,
          name: 'AnchorPlayer',
          is_disconnected: false,
          stats: { score: 100, goals: 0, assists: 0, saves: 1, shots: 1, demos: 0 },
        }),
        createPlayerFixture({
          player_id: player2Id,
          name: 'DynamicPlayer',
          is_disconnected: false,
          stats: { score: 100, goals: 0, assists: 0, saves: 1, shots: 1, demos: 0 },
        }),
      ];

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={frame1Players}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      expect(container.querySelectorAll('[data-testid="badge-disconnected"]').length).toBe(0);
      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('200');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('0');

      // Frame 2: DynamicPlayer scores a goal
      const frame2Players: LobbyPlayer[] = [
        frame1Players[0],
        createPlayerFixture({
          player_id: player2Id,
          name: 'DynamicPlayer',
          is_disconnected: false,
          stats: { score: 250, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 },
        }),
      ];

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={frame2Players}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('350');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('1');
      expect(container.querySelectorAll('[data-testid="badge-disconnected"]').length).toBe(0);

      // Frame 3: DynamicPlayer disconnects mid-game (is_disconnected = true)
      const frame3Players: LobbyPlayer[] = [
        frame2Players[0],
        createPlayerFixture({
          player_id: player2Id,
          name: 'DynamicPlayer',
          is_disconnected: true,
          stats: { score: 250, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 }, // stats retained!
        }),
      ];

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={frame3Players}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      // Assert badge appeared
      expect(container.querySelectorAll('[data-testid="badge-disconnected"]').length).toBe(1);
      const dcRow = container.querySelectorAll('tbody tr')[0]; // score 250 vs 100 -> dynamic player is still row 0!
      expect(dcRow.className).toContain('opacity-65');
      expect(dcRow.className).toContain('saturate-60');
      expect(dcRow.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
      // Team totals MUST still include DynamicPlayer's retained stats
      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('350');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('1');

      // Frame 4: AnchorPlayer scores a goal while DynamicPlayer is still disconnected
      const frame4Players: LobbyPlayer[] = [
        createPlayerFixture({
          player_id: player1Id,
          name: 'AnchorPlayer',
          is_disconnected: false,
          stats: { score: 250, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 },
        }),
        frame3Players[1], // still disconnected
      ];

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={frame4Players}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      // Total score: 250 + 250 = 500, goals: 1 + 1 = 2
      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('500');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('2');
      expect(container.querySelectorAll('[data-testid="badge-disconnected"]').length).toBe(1);

      // Frame 5: DynamicPlayer RECONNECTS! (is_disconnected = false)
      const frame5Players: LobbyPlayer[] = [
        frame4Players[0],
        createPlayerFixture({
          player_id: player2Id,
          name: 'DynamicPlayer',
          is_disconnected: false,
          stats: { score: 250, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 },
        }),
      ];

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={frame5Players}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      // Badge must be completely removed, opacity restored
      expect(container.querySelectorAll('[data-testid="badge-disconnected"]').length).toBe(0);
      const rowsAfterReconnect = container.querySelectorAll('tbody tr');
      rowsAfterReconnect.forEach((r) => {
        expect(r.className).not.toContain('opacity-65');
        expect(r.className).not.toContain('saturate-60');
      });
      // Team totals still intact
      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('500');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('2');
    });

    it('flapping stress test: repeatedly toggles disconnected state 10 times without corruption', () => {
      let isDc = false;

      for (let iteration = 0; iteration < 10; iteration++) {
        isDc = !isDc;

        const player = createPlayerFixture({
          player_id: 'flapping_player',
          name: 'Flapper',
          is_disconnected: isDc,
          stats: { score: 400, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0 },
        });

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[player]}
              columnConfig={allColumns}
              activePlaylistId={11}
            />
          );
        });

        const row = container.querySelector('tbody tr');
        const badge = container.querySelector('[data-testid="badge-disconnected"]');

        if (isDc) {
          expect(row?.className).toContain('opacity-65');
          expect(row?.className).toContain('saturate-60');
          expect(badge).not.toBeNull();
        } else {
          expect(row?.className).not.toContain('opacity-65');
          expect(row?.className).not.toContain('saturate-60');
          expect(badge).toBeNull();
        }

        // Team score must ALWAYS remain 400 and goals 1 throughout all flapping
        expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('400');
        expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('1');
      }
    });
  });

  // =========================================================================
  // SUITE 7: LiveGameView Component Tree & Column Config Mutation Resiliency
  // =========================================================================
  describe('Suite 7: LiveGameView Component Tree & Column Config Resiliency', () => {
    it('renders full LiveGameView with disconnected local player, teammate, and opponent simultaneously', () => {
      const mockLiveMatch: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'live-tree-guid-001',
        playlist_id: 11,
        playlist_name: 'Ranked Standard 3v3',
        local_team: 0,
        local_player: createPlayerFixture({
          player_id: 'Steam|local_hero|0',
          name: 'LocalHero',
          team_num: 0,
          is_local: true,
          is_disconnected: true, // Local player disconnected early!
          stats: { score: 500, goals: 2, assists: 0, saves: 1, shots: 3, demos: 0 },
        }),
        teammates: [
          createPlayerFixture({
            player_id: 'Epic|tm8_active|0',
            name: 'ActiveTm8',
            team_num: 0,
            is_local: false,
            is_disconnected: false,
            stats: { score: 300, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0 },
          }),
        ],
        opponents: [
          createPlayerFixture({
            player_id: 'Epic|opp_active|0',
            name: 'ActiveOpp',
            team_num: 1,
            is_local: false,
            is_disconnected: false,
            stats: { score: 400, goals: 1, assists: 0, saves: 2, shots: 3, demos: 0 },
          }),
          createPlayerFixture({
            player_id: 'Epic|opp_dc|0',
            name: 'DisconnectedOpp',
            team_num: 1,
            is_local: false,
            is_disconnected: true, // Opponent disconnected early!
            stats: { score: 150, goals: 0, assists: 1, saves: 0, shots: 1, demos: 0 },
          }),
        ],
        updated_at: '2026-10-06T11:30:00Z',
      };

      (useLiveMatch as unknown as { mockReturnValue: (v: unknown) => void }).mockReturnValue({
        match: mockLiveMatch,
      });
      (useSession as unknown as { mockReturnValue: (v: unknown) => void }).mockReturnValue({
        session: null,
      });

      act(() => {
        root.render(<LiveGameView session={null} />);
      });

      // Verify Blue Team table contains LocalHero with both YOU and DISCONNECTED
      const badges = container.querySelectorAll('[data-testid="badge-disconnected"]');
      expect(badges.length).toBe(2); // LocalHero + DisconnectedOpp

      // Blue Team headers: Goals = 2 + 1 = 3, Score = 500 + 300 = 800
      const teamGoalsBadges = container.querySelectorAll('[data-testid="team-goals"]');
      expect(teamGoalsBadges.length).toBe(2);
      expect(teamGoalsBadges[0].textContent).toContain('3'); // Blue goals

      // Orange Team headers: Goals = 1 + 0 = 1, Score = 400 + 150 = 550
      expect(teamGoalsBadges[1].textContent).toContain('1'); // Orange goals

      const teamScoreBadges = container.querySelectorAll('[data-testid="team-score"]');
      expect(teamScoreBadges.length).toBe(2);
      expect(teamScoreBadges[0].textContent).toContain('800'); // Blue score
      expect(teamScoreBadges[1].textContent).toContain('550'); // Orange score
    });

    it('preserves disconnected styling when column configuration hides optional columns', () => {
      const disconnectedPlayer = createPlayerFixture({
        name: 'GhostMinimal',
        is_disconnected: true,
      });

      const minimalConfig: ColumnConfig = {
        score: false,
        goals: false,
        assists: false,
        saves: false,
        shots: false,
        demos: false,
        mmr: false,
        rank: false,
        h2h: false,
        platform: false,
      };

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[disconnectedPlayer]}
            columnConfig={minimalConfig}
            activePlaylistId={11}
          />
        );
      });

      // Disconnected styling and badge remain intact in the permanent identity column
      const row = container.querySelector('tbody tr');
      expect(row?.className).toContain('opacity-65');
      expect(row?.className).toContain('saturate-60');
      expect(container.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
      // Optional columns are hidden
      expect(container.querySelector('[data-testid="stat-score"]')).toBeNull();
      expect(container.querySelector('[data-testid="stat-goals"]')).toBeNull();
      // Team header aggregated totals are still present and correct
      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('350');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('1');
    });
  });

  // =========================================================================
  // SUITE 8: Extreme Boundary & Resiliency Stress Harness
  // =========================================================================
  describe('Suite 8: Extreme Boundary & Resiliency Stress Harness', () => {
    it('handles disconnected player with extreme Unicode name, HTML entities, and emojis', () => {
      const adversaryPlayer = createPlayerFixture({
        name: '<script>alert("pwn")</script> 🚀🔥 [FaZe] ᚛᚛ᚚ',
        is_disconnected: true,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={adversaryPlayer}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      const identity = container.querySelector('[data-testid="player-identity"]');
      expect(identity?.textContent).toContain('<script>alert("pwn")</script> 🚀🔥 [FaZe] ᚛᚛ᚚ');
      expect(container.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
    });

    it('handles extreme integer stats for disconnected player (100k score, 100 goals)', () => {
      const megaPlayer = createPlayerFixture({
        is_disconnected: true,
        stats: { score: 100000, goals: 100, assists: 50, saves: 75, shots: 200, demos: 10 },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[megaPlayer]}
            columnConfig={allColumns}
            activePlaylistId={11}
          />
        );
      });

      expect(container.querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('100000');
      expect(container.querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('100');
      expect(container.querySelector('[data-testid="team-score"]')?.textContent).toContain('100000');
      expect(container.querySelector('[data-testid="team-goals"]')?.textContent).toContain('100');
    });

    it('handles disconnected player with undefined rank and matchup record cleanly', () => {
      const unrankedPlayer = createPlayerFixture({
        name: 'NoRankLeaver',
        is_disconnected: true,
        current_rank: undefined,
        matchup_record: undefined,
      });

      act(() => {
        root.render(
          <table>
            <tbody>
              <PlayerRow
                player={unrankedPlayer}
                isLocal={false}
                isTeammate={true}
                columnConfig={allColumns}
                activePlaylistId={11}
              />
            </tbody>
          </table>
        );
      });

      expect(container.querySelector('[data-testid="badge-disconnected"]')).not.toBeNull();
      // MMR defaults to '--'
      expect(container.querySelector('[data-testid="stat-mmr"]')?.textContent?.trim()).toBe('--');
      // Rank defaults to Unranked
      expect(container.querySelector('[data-testid="stat-rank"]')?.textContent).toContain('Unranked');
    });
  });
});
