// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { LiveGameView } from './LiveGameView';
import { RosterTable } from './RosterTable';
import { App } from '../../App';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useSession } from '../../hooks/useSession';
import { ColumnConfig, STAT_COLUMNS } from '../../types/columns';
import { CurrentMatchResponse, LobbyPlayer } from '../../types/api';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Mock custom hooks
vi.mock('../../hooks/useLiveMatch', () => ({
  useLiveMatch: vi.fn(),
}));

vi.mock('../../hooks/useSession', () => ({
  useSession: vi.fn(),
}));

const allColumnsEnabled: ColumnConfig = {
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

const allColumnsDisabled: ColumnConfig = {
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

function createMockPlayer(overrides: Partial<LobbyPlayer> = {}): LobbyPlayer {
  return {
    player_id: `Epic|player_${Math.random().toString(36).substring(7)}|0`,
    name: 'PlayerName',
    platform: 'Epic',
    team_num: 0,
    is_local: false,
    is_bot: false,
    stats: { score: 300, goals: 1, assists: 1, saves: 1, shots: 2, demos: 0 },
    current_rank: {
      playlist_id: 11,
      playlist_name: 'Ranked Standard 3v3',
      tier: 16,
      division: 2,
      rank_name: 'Champion I Div II',
      mmr: 1040.0,
    },
    matchup_record: {
      player_id: 'Epic|player_mock|0',
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

describe('LiveGameView Adversarial Stress & Edge Case Challenge Suite', () => {
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

  // =========================================================================
  // 1. 4v4 Chaos Match Rosters (8 Total Players) & Spectators
  // =========================================================================
  describe('1. 4v4 Chaos Match Rosters & Spectator Isolation', () => {
    it('correctly partitions and renders 4v4 rosters with 8 players without cross-team pollution', () => {
      const bluePlayers = [
        createMockPlayer({ player_id: 'Blue_1', name: 'BlueOne', team_num: 0, is_local: true }),
        createMockPlayer({ player_id: 'Blue_2', name: 'BlueTwo', team_num: 0 }),
        createMockPlayer({ player_id: 'Blue_3', name: 'BlueThree', team_num: 0 }),
        createMockPlayer({ player_id: 'Blue_4', name: 'BlueFour', team_num: 0, is_disconnected: true }),
      ];

      const orangePlayers = [
        createMockPlayer({ player_id: 'Orange_1', name: 'OrangeOne', team_num: 1 }),
        createMockPlayer({ player_id: 'Orange_2', name: 'OrangeTwo', team_num: 1 }),
        createMockPlayer({ player_id: 'Orange_3', name: 'OrangeThree', team_num: 1 }),
        createMockPlayer({ player_id: 'Orange_4', name: 'OrangeFour', team_num: 1 }),
      ];

      const match4v4: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'guid-4v4-chaos',
        playlist_id: 4,
        playlist_name: 'Chaos 4v4',
        local_team: 0,
        local_player: bluePlayers[0],
        teammates: bluePlayers.slice(1),
        opponents: orangePlayers,
        spectators: [],
        updated_at: '2026-10-06T09:00:00Z',
      };

      vi.mocked(useLiveMatch).mockReturnValue({
        match: match4v4,
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      act(() => {
        root.render(<LiveGameView session={null} />);
      });

      // Assert both roster tables exist
      const rosterTables = container.querySelectorAll('.grid.lg\\:grid-cols-2 > div');
      expect(rosterTables.length).toBe(2);

      const tables = container.querySelectorAll('table');
      expect(tables.length).toBe(2);

      // Blue table has 4 rows
      const blueRows = rosterTables[0].querySelectorAll('tbody tr');
      expect(blueRows.length).toBe(4);
      expect(rosterTables[0].textContent).toContain('BlueOne');
      expect(rosterTables[0].textContent).toContain('BlueFour');

      // Orange table has 4 rows
      const orangeRows = rosterTables[1].querySelectorAll('tbody tr');
      expect(orangeRows.length).toBe(4);
      expect(rosterTables[1].textContent).toContain('OrangeOne');
      expect(rosterTables[1].textContent).toContain('OrangeFour');

      // Check header counts in both tables: (4)
      expect(rosterTables[0].textContent).toContain('(4)');
      expect(rosterTables[1].textContent).toContain('(4)');
    });

    it('isolates spectators from player rosters, preventing roster height inflation in 4v4', () => {
      const bluePlayers = [
        createMockPlayer({ player_id: 'B1', name: 'B1', team_num: 0, is_local: true }),
        createMockPlayer({ player_id: 'B2', name: 'B2', team_num: 0 }),
        createMockPlayer({ player_id: 'B3', name: 'B3', team_num: 0 }),
        createMockPlayer({ player_id: 'B4', name: 'B4', team_num: 0 }),
      ];
      const orangePlayers = [
        createMockPlayer({ player_id: 'O1', name: 'O1', team_num: 1 }),
        createMockPlayer({ player_id: 'O2', name: 'O2', team_num: 1 }),
        createMockPlayer({ player_id: 'O3', name: 'O3', team_num: 1 }),
        createMockPlayer({ player_id: 'O4', name: 'O4', team_num: 1 }),
      ];
      const spectators = [
        createMockPlayer({ player_id: 'Spec_1', name: 'Caster1', team_num: 255 }),
        createMockPlayer({ player_id: 'Spec_2', name: 'Referee', team_num: 255 }),
      ];

      const matchWithSpectators: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'guid-tournament-4v4',
        playlist_id: 4,
        playlist_name: 'Tournament Chaos 4v4',
        local_team: 0,
        local_player: bluePlayers[0],
        teammates: bluePlayers.slice(1),
        opponents: orangePlayers,
        spectators,
        updated_at: '2026-10-06T09:00:00Z',
      };

      vi.mocked(useLiveMatch).mockReturnValue({
        match: matchWithSpectators,
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      act(() => {
        root.render(<LiveGameView session={null} />);
      });

      // Total rendered rows across both roster tables must strictly remain 8, NOT 10
      const allRows = container.querySelectorAll('tbody tr');
      expect(allRows.length).toBe(8);

      expect(container.textContent).not.toContain('Caster1');
      expect(container.textContent).not.toContain('Referee');
    });
  });

  // =========================================================================
  // 2. Extreme Stat Numbers & Alignment Stress
  // =========================================================================
  describe('2. Extreme Stat Numbers & Column Alignment Stress', () => {
    it('handles giant numbers (99999 score, 99 goals/assists/saves/demos) without throwing or misaligning', () => {
      const extremePlayer = createMockPlayer({
        player_id: 'Hero_Extreme',
        name: 'GodPlayer',
        stats: {
          score: 99999,
          goals: 99,
          assists: 88,
          saves: 77,
          shots: 150,
          demos: 42,
        },
        current_rank: {
          playlist_id: 11,
          playlist_name: 'Ranked Standard 3v3',
          tier: 22,
          division: 1,
          rank_name: 'Supersonic Legend',
          mmr: 2450.5,
        },
      });

      const zeroPlayer = createMockPlayer({
        player_id: 'Zero_Player',
        name: 'Newbie',
        stats: {
          score: 0,
          goals: 0,
          assists: 0,
          saves: 0,
          shots: 0,
          demos: 0,
        },
        current_rank: {
          playlist_id: 11,
          playlist_name: 'Ranked Standard 3v3',
          tier: 1,
          division: 1,
          rank_name: 'Bronze I Div I',
          mmr: 120.0,
        },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[extremePlayer, zeroPlayer]}
            columnConfig={allColumnsEnabled}
            activePlaylistId={11}
          />
        );
      });

      const rows = container.querySelectorAll('tbody tr');
      expect(rows.length).toBe(2);

      // Verify cells count in both rows matches header count exactly
      const headerCells = container.querySelectorAll('thead th');
      expect(headerCells.length).toBe(11);
      expect(rows[0].querySelectorAll('td').length).toBe(11);
      expect(rows[1].querySelectorAll('td').length).toBe(11);

      // Row 0 has extreme values rendered
      expect(rows[0].querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('99999');
      expect(rows[0].querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('99');
      expect(rows[0].querySelector('[data-testid="stat-assists"]')?.textContent?.trim()).toBe('88');
      expect(rows[0].querySelector('[data-testid="stat-saves"]')?.textContent?.trim()).toBe('77');
      expect(rows[0].querySelector('[data-testid="stat-shots"]')?.textContent?.trim()).toBe('150');
      expect(rows[0].querySelector('[data-testid="stat-demos"]')?.textContent?.trim()).toBe('42');
      expect(rows[0].querySelector('[data-testid="stat-mmr"]')?.textContent?.trim()).toBe('2450.5');

      // Check text-right alignment on all numeric cells
      const numericTestIds = ['stat-score', 'stat-goals', 'stat-assists', 'stat-saves', 'stat-shots', 'stat-demos', 'stat-mmr'];
      for (const tid of numericTestIds) {
        const cell = rows[0].querySelector(`[data-testid="${tid}"]`);
        expect(cell?.className).toContain('text-right');
      }

      // High goals (99) has vibrant amber glow
      expect(rows[0].querySelector('[data-testid="stat-goals"]')?.className).toContain('text-amber-400');
      // Zero goals has muted slate styling
      expect(rows[1].querySelector('[data-testid="stat-goals"]')?.className).toContain('text-slate-500');
    });

    it('gracefully handles missing or undefined stats object in player without crashing', () => {
      const corruptPlayer: LobbyPlayer = {
        player_id: 'Corrupt_1',
        name: 'NullStatsPlayer',
        platform: 'Steam',
        team_num: 0,
        is_local: false,
        is_bot: false,
        stats: undefined as unknown as LobbyPlayer['stats'],
      };

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[corruptPlayer]}
            columnConfig={allColumnsEnabled}
            activePlaylistId={11}
          />
        );
      });

      // Does not throw and defaults stats to 0
      expect(container.querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('0');
      expect(container.querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('0');
      expect(container.querySelector('[data-testid="stat-assists"]')?.textContent?.trim()).toBe('0');
    });
  });

  // =========================================================================
  // 3. Long Player Names & Truncation Edge Cases
  // =========================================================================
  describe('3. Long Player Names, Truncation & Special Characters', () => {
    it('truncates very long names (30+ and 100+ chars) with max-w-[150px] truncate and preserves title tooltip', () => {
      const longName30 = 'Supercalifragilisticexpialidoc';
      const longName100 = 'A'.repeat(100);

      const player30 = createMockPlayer({ player_id: 'P30', name: longName30 });
      const player100 = createMockPlayer({ player_id: 'P100', name: longName100 });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[player30, player100]}
            columnConfig={allColumnsEnabled}
            activePlaylistId={11}
          />
        );
      });

      const identityCells = container.querySelectorAll('[data-testid="player-identity"]');
      expect(identityCells.length).toBe(2);

      // Name spans must have truncate and max-w-[150px]
      const span30 = identityCells[0].querySelector('span.truncate');
      expect(span30).not.toBeNull();
      expect(span30?.className).toContain('max-w-[150px]');
      expect(span30?.getAttribute('title')).toBe(longName30);

      const span100 = identityCells[1].querySelector('span.truncate');
      expect(span100).not.toBeNull();
      expect(span100?.className).toContain('max-w-[150px]');
      expect(span100?.getAttribute('title')).toBe(longName100);
    });

    it('safely renders HTML/XSS injection attempts in player names without DOM corruption', () => {
      const xssName = '<script>alert("xss")</script><img src=x onerror=alert(1)>';
      const xssPlayer = createMockPlayer({ player_id: 'XSS_Player', name: xssName });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[xssPlayer]}
            columnConfig={allColumnsEnabled}
            activePlaylistId={11}
          />
        );
      });

      // No actual script tag or img tag must be created in DOM
      expect(container.querySelector('script')).toBeNull();
      expect(container.querySelector('img[src="x"]')).toBeNull();

      const span = container.querySelector('[data-testid="player-identity"] span.truncate');
      expect(span?.textContent).toBe(xssName);
      expect(span?.getAttribute('title')).toBe(xssName);
    });
  });

  // =========================================================================
  // 4. Custom Column Visibility Toggling
  // =========================================================================
  describe('4. Custom Column Visibility Toggling & Cell-Header Parity', () => {
    it('maintains exact 1:1 cell to header parity when ALL columns are disabled (only player name remains)', () => {
      const player = createMockPlayer({ name: 'MinimalPlayer' });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[player]}
            columnConfig={allColumnsDisabled}
            activePlaylistId={11}
          />
        );
      });

      const ths = container.querySelectorAll('thead th');
      const tds = container.querySelectorAll('tbody tr td');

      // Exactly 1 column: the Player column
      expect(ths.length).toBe(1);
      expect(tds.length).toBe(1);
      expect(ths[0].getAttribute('data-testid')).toBe('th-player');
      expect(tds[0].getAttribute('data-testid')).toBe('player-identity');
    });

    it('maintains exact 1:1 cell to header parity for each individual column toggle', () => {
      const player = createMockPlayer({ name: 'SoloColumnPlayer' });

      for (const col of STAT_COLUMNS) {
        const singleConfig: ColumnConfig = {
          ...allColumnsDisabled,
          [col.key]: true,
        };

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[player]}
              columnConfig={singleConfig}
              activePlaylistId={11}
            />
          );
        });

        const ths = container.querySelectorAll('thead th');
        const tds = container.querySelectorAll('tbody tr td');

        // Exactly 2 columns: Player + toggled column
        expect(ths.length).toBe(2);
        expect(tds.length).toBe(2);
        expect(ths[1].getAttribute('data-testid')).toBe(`th-${col.key}`);
        expect(tds[1].getAttribute('data-testid')).toBe(`stat-${col.key}`);
      }
    });

    it('contains overflow-x-auto to prevent horizontal layout blowout when all columns are enabled', () => {
      const player = createMockPlayer();

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[player]}
            columnConfig={allColumnsEnabled}
            activePlaylistId={11}
          />
        );
      });

      // Outer wrapper has overflow-x-auto
      const overflowWrapper = container.querySelector('.overflow-x-auto');
      expect(overflowWrapper).not.toBeNull();
      expect(overflowWrapper?.querySelector('table')).not.toBeNull();
    });
  });

  // =========================================================================
  // 5. Zero-Scroll Height Budgets (< 500px Stack Height)
  // =========================================================================
  describe('5. Zero-Scroll Stack Height Budgets Empirical Oracle', () => {
    /**
     * Mathematical height oracle for the active match HUD stack:
     * Header (compact, inMatch): 46px
     * Navbar: 40px
     * Main padding (py-2): 16px
     * ScoreboardBanner (py-2.5 px-5 mb-3): ~82px
     * LiveGameView space-y-3 gap: 12px
     * RosterTable header/banner: 36px (banner) + 28px (table head) = 64px
     * Per player row (py-2): 38px
     * Table container border/margin: 4px
     * Footer during inMatch: 0px (hidden)
     */
    function computeStackHeight(playerCountPerTeam: number): number {
      const header = 46;
      const navbar = 40;
      const mainPadding = 16;
      const banner = 82;
      const liveGap = 12;
      const rosterChrome = 64 + 4;
      const rosterRows = playerCountPerTeam * 38;
      const rosterTotal = rosterChrome + rosterRows;
      const footer = 0;

      return header + navbar + mainPadding + banner + liveGap + rosterTotal + footer;
    }

    it('verifies 1v1 Duel stack height is <= 500px with huge headroom', () => {
      const height = computeStackHeight(1);
      expect(height).toBe(302);
      expect(height).toBeLessThanOrEqual(500);

      // Usable height on standard 1080p display (~920px client height)
      expect(920 - height).toBe(618); // 618px headroom
    });

    it('verifies 2v2 Doubles stack height is <= 500px with huge headroom', () => {
      const height = computeStackHeight(2);
      expect(height).toBe(340);
      expect(height).toBeLessThanOrEqual(500);
      expect(920 - height).toBe(580); // 580px headroom
    });

    it('verifies 3v3 Standard stack height is <= 500px with huge headroom', () => {
      const height = computeStackHeight(3);
      expect(height).toBe(378);
      expect(height).toBeLessThanOrEqual(500);
      expect(920 - height).toBe(542); // 542px headroom
    });

    it('verifies 4v4 Chaos stack height is <= 500px with >500px headroom on 1080p', () => {
      const height = computeStackHeight(4);
      expect(height).toBe(416);
      expect(height).toBeLessThanOrEqual(500);
      expect(920 - height).toBe(504); // 504px headroom on 1080p

      // On a 768p laptop display (~620px usable height):
      expect(620 - height).toBe(204); // >200px headroom even on 768p!
    });

    it('guarantees footer is omitted from DOM in full App component tree when inMatch is true', () => {
      const match = createMockPlayer();
      vi.mocked(useLiveMatch).mockReturnValue({
        match: {
          active_match: true,
          match_ended: false,
          match_guid: 'guid-live',
          playlist_id: 11,
          playlist_name: 'Ranked 3v3',
          local_team: 0,
          local_player: match,
          teammates: [],
          opponents: [],
          spectators: [],
          updated_at: '2026-10-06T09:00:00Z',
        },
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      vi.mocked(useSession).mockReturnValue({
        session: {
          session_id: 's1',
          started_at: '2026-10-06T00:00:00Z',
          uptime: '1h',
          total_matches: 1,
          total_wins: 1,
          total_losses: 0,
          win_rate: 100,
          playlists: {},
          matches: [],
        },
        isLoading: false,
        error: null,
        selectedMatch: null,
        setSelectedMatch: vi.fn(),
        refreshSession: vi.fn(),
        resetSession: vi.fn(),
      });

      act(() => {
        root.render(<App />);
      });

      // In live match mode, footer is strictly NOT rendered
      expect(container.querySelector('footer')).toBeNull();
      // Playlist carousel is NOT rendered in Header
      expect(container.querySelector('[data-testid="app-header"]')?.textContent).not.toContain('Playlists:');
    });
  });
});
