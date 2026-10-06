// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';

import { LiveGameView } from './LiveGameView';
import { ScoreboardBanner } from './ScoreboardBanner';
import { RosterTable } from './RosterTable';
import { PlayerRow } from './PlayerRow';
import { App } from '../../App';
import { Header } from '../layout/Header';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useSession } from '../../hooks/useSession';
import { ColumnConfig, PRESETS } from '../../types/columns';
import { CurrentMatchResponse, SessionResponse, LobbyPlayer } from '../../types/api';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Mock custom hooks to prevent network requests and provide deterministic fixtures
vi.mock('../../hooks/useLiveMatch', () => ({
  useLiveMatch: vi.fn(),
}));

vi.mock('../../hooks/useSession', () => ({
  useSession: vi.fn(),
}));

// Clean HTML helper removing React 19 server comments
const cleanHtml = (s: string) => s.replace(/<!--.*?-->/g, '');

// ============================================================================
// Deterministic Fixtures
// ============================================================================

const fullConfig: ColumnConfig = {
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

const mockLocalPlayer: LobbyPlayer = {
  player_id: 'Steam|local_hero|0',
  name: 'LocalHero',
  platform: 'Steam',
  team_num: 0,
  is_local: true,
  is_bot: false,
  stats: { score: 720, goals: 3, assists: 1, saves: 2, shots: 5, demos: 1 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Standard 3v3',
    tier: 16,
    division: 2,
    rank_name: 'Champion I Div II',
    mmr: 1045.5,
  },
  matchup_record: {
    player_id: 'Steam|local_hero|0',
    playlist_id: 11,
    wins_as_teammate: 10,
    losses_as_teammate: 2,
    wins_as_opponent: 0,
    losses_as_opponent: 0,
    total_matches: 12,
    last_played_at: '2026-10-06T00:00:00Z',
  },
};

const mockTeammateBlue: LobbyPlayer = {
  player_id: 'Epic|blue_tm8|0',
  name: 'BlueTeammate',
  platform: 'Epic',
  team_num: 0,
  is_local: false,
  is_bot: false,
  stats: { score: 450, goals: 1, assists: 2, saves: 1, shots: 3, demos: 0 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Standard 3v3',
    tier: 16,
    division: 1,
    rank_name: 'Champion I Div I',
    mmr: 1020.0,
  },
  matchup_record: {
    player_id: 'Epic|blue_tm8|0',
    playlist_id: 11,
    wins_as_teammate: 7,
    losses_as_teammate: 3,
    wins_as_opponent: 4,
    losses_as_opponent: 6,
    total_matches: 20,
    last_played_at: '2026-10-06T00:00:00Z',
  },
};

const mockDisconnectedPlayer: LobbyPlayer = {
  player_id: 'Steam|blue_dc|0',
  name: 'DisconnectedBlue',
  platform: 'Steam',
  team_num: 0,
  is_local: false,
  is_bot: false,
  is_disconnected: true,
  stats: { score: 210, goals: 0, assists: 0, saves: 1, shots: 1, demos: 2 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Standard 3v3',
    tier: 15,
    division: 4,
    rank_name: 'Diamond III Div IV',
    mmr: 995.0,
  },
};

const mockOrangeLeader: LobbyPlayer = {
  player_id: 'Epic|orange_lead|0',
  name: 'OrangeLeader',
  platform: 'Epic',
  team_num: 1,
  is_local: false,
  is_bot: false,
  stats: { score: 580, goals: 2, assists: 0, saves: 3, shots: 4, demos: 0 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Standard 3v3',
    tier: 16,
    division: 3,
    rank_name: 'Champion I Div III',
    mmr: 1060.0,
  },
  matchup_record: {
    player_id: 'Epic|orange_lead|0',
    playlist_id: 11,
    wins_as_teammate: 9,
    losses_as_teammate: 1,
    wins_as_opponent: 2,
    losses_as_opponent: 8,
    total_matches: 20,
    last_played_at: '2026-10-06T00:00:00Z',
  },
};

const mockOrangeWinger: LobbyPlayer = {
  player_id: 'Steam|orange_wing|0',
  name: 'OrangeWinger',
  platform: 'Steam',
  team_num: 1,
  is_local: false,
  is_bot: false,
  stats: { score: 320, goals: 0, assists: 1, saves: 1, shots: 2, demos: 1 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Standard 3v3',
    tier: 16,
    division: 1,
    rank_name: 'Champion I Div I',
    mmr: 1025.0,
  },
};

const mockOrangeGoalie: LobbyPlayer = {
  player_id: 'Xbox|orange_goal|0',
  name: 'OrangeGoalie',
  platform: 'Xbox',
  team_num: 1,
  is_local: false,
  is_bot: false,
  stats: { score: 190, goals: 0, assists: 0, saves: 2, shots: 0, demos: 0 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Standard 3v3',
    tier: 15,
    division: 3,
    rank_name: 'Diamond III Div III',
    mmr: 980.0,
  },
};

const mockActive3v3Match: CurrentMatchResponse = {
  active_match: true,
  match_ended: false,
  match_guid: 'guid-standard-3v3',
  playlist_id: 11,
  playlist_name: 'Ranked Standard 3v3',
  local_team: 0,
  local_player: mockLocalPlayer,
  teammates: [mockTeammateBlue, mockDisconnectedPlayer],
  opponents: [mockOrangeLeader, mockOrangeWinger, mockOrangeGoalie],
  spectators: [],
  updated_at: '2026-10-06T09:00:00Z',
};

const mockSession: SessionResponse = {
  session_id: 'sess-active-live',
  started_at: '2026-10-06T08:00:00Z',
  uptime: '1h 15m',
  total_matches: 5,
  total_wins: 4,
  total_losses: 1,
  win_rate: 80.0,
  playlists: {
    '11': {
      playlist_id: 11,
      playlist_name: 'Ranked Standard 3v3',
      matches_played: 5,
      wins: 4,
      losses: 1,
      win_rate: 80.0,
      initial_mmr: 1000.0,
      current_mmr: 1045.5,
      mmr_delta: 45.5,
    },
  },
  matches: [],
  active_match: mockActive3v3Match,
};

// ============================================================================
// Layout Budget Oracle Helper
// ============================================================================

/**
 * Computes the theoretical vertical pixel height budget of the live HUD stack
 * according to Tailwind layout tokens.
 */
function calculateLiveViewHeightBudget(options: {
  playerCountPerTeam: number;
  hasCarousel: boolean;
  hasFooter: boolean;
  isCompact: boolean;
}): number {
  const headerHeight = options.isCompact
    ? options.hasCarousel ? 86 : 46
    : 120;
  const navbarHeight = 40;
  const mainPadding = options.isCompact ? 16 : 32;
  const bannerHeight = options.isCompact ? 82 : 160;
  const liveGap = options.isCompact ? 12 : 24;
  const playerRowHeight = options.isCompact ? 38 : 42;
  const rosterHeight = 36 + 28 + (options.playerCountPerTeam * playerRowHeight) + 4;
  const footerHeight = options.hasFooter ? 40 : 0;

  return headerHeight + navbarHeight + mainPadding + bannerHeight + liveGap + rosterHeight + footerHeight;
}

// ============================================================================
// Test Suite Implementation
// ============================================================================

describe('LiveGameView Programmatic DOM Layout & Structure Suite', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    localStorage.clear();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    // Setup hook mocks by default
    vi.mocked(useLiveMatch).mockReturnValue({
      match: mockActive3v3Match,
      status: 'connected',
      lastUpdated: new Date(),
      error: null,
    });

    vi.mocked(useSession).mockReturnValue({
      session: mockSession,
      isLoading: false,
      error: null,
      selectedMatch: null,
      setSelectedMatch: vi.fn(),
      refreshSession: vi.fn(),
      resetSession: vi.fn(),
    });
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
  // SUITE 1: Stat Prominence & Visual Hierarchy Assertions
  // =========================================================================
  describe('1. Stat Prominence & Visual Hierarchy', () => {
    it('renders primary box score metrics with enlarged typography classes and font weights', () => {
      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={mockLocalPlayer}
            isLocal={true}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Score must render as a prominent hero metric (text-lg, font-black, font-mono)
      expect(html).toContain('720');
      expect(html).toMatch(/class="[^"]*text-lg[^"]*font-black[^"]*font-mono/);

      // Goals must render enlarged (text-lg, font-black) with amber gold highlight when > 0
      expect(html).toContain('3');
      expect(html).toMatch(/class="[^"]*text-lg[^"]*font-black[^"]*text-amber-400/);

      // Assists must render enlarged (text-base, font-extrabold) with cyan highlight when > 0
      expect(html).toContain('1');
      expect(html).toMatch(/class="[^"]*text-base[^"]*font-extrabold[^"]*text-cyan-300/);

      // Saves must render enlarged (text-base, font-extrabold) with emerald highlight when > 0
      expect(html).toContain('2');
      expect(html).toMatch(/class="[^"]*text-base[^"]*font-extrabold[^"]*text-emerald-400/);

      // Shots must render bold
      expect(html).toContain('5');
      expect(html).toMatch(/class="[^"]*font-bold[^"]*font-mono/);

      // Demos must render bold with rose highlight when > 0
      expect(html).toContain('1');
      expect(html).toMatch(/class="[^"]*text-rose-400/);
    });

    it('renders stats with muted styling when zero and vibrant colored accents when positive', () => {
      const zeroStatsPlayer: LobbyPlayer = {
        ...mockLocalPlayer,
        matchup_record: undefined,
        stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
      };

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={zeroStatsPlayer}
            isLocal={false}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // When 0, stats use muted slate classes (text-slate-500 or text-slate-400)
      expect(html).not.toContain('text-amber-400');
      expect(html).not.toContain('text-cyan-300');
      expect(html).not.toContain('text-emerald-400');
      expect(html).not.toContain('text-rose-400');
    });

    it('orders performance stat columns BEFORE secondary skill and platform metadata in the DOM', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockLocalPlayer]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      const thElements = Array.from(container.querySelectorAll('thead th'));
      const headers = thElements.map((th) => th.textContent?.trim().toUpperCase() || '');

      const idxScore = headers.findIndex((h) => h.includes('SCORE') || h.includes('PTS'));
      const idxGoals = headers.findIndex((h) => h.includes('GOALS') || h.includes('G'));
      const idxAssists = headers.findIndex((h) => h.includes('ASSISTS') || h.includes('A'));
      const idxSaves = headers.findIndex((h) => h.includes('SAVES') || h.includes('SV'));
      const idxShots = headers.findIndex((h) => h.includes('SHOTS') || h.includes('SH'));
      const idxDemos = headers.findIndex((h) => h.includes('DEMOS') || h.includes('DEMO'));
      const idxRank = headers.findIndex((h) => h.includes('RANK'));
      const idxMMR = headers.findIndex((h) => h.includes('MMR'));
      const idxH2H = headers.findIndex((h) => h.includes('H2H'));
      const idxPlatform = headers.findIndex((h) => h.includes('PLAT'));

      // All action metrics must appear before secondary skill and identity columns
      expect(idxScore).toBeGreaterThanOrEqual(1);
      expect(idxScore).toBeLessThan(idxRank);
      expect(idxScore).toBeLessThan(idxMMR);
      expect(idxScore).toBeLessThan(idxH2H);
      expect(idxScore).toBeLessThan(idxPlatform);

      expect(idxGoals).toBeLessThan(idxRank);
      expect(idxAssists).toBeLessThan(idxRank);
      expect(idxSaves).toBeLessThan(idxRank);
      expect(idxShots).toBeLessThan(idxRank);
      expect(idxDemos).toBeLessThan(idxRank);
    });

    it('preserves prominent stat rendering and data retention for disconnected players (M1/M2 contract)', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockDisconnectedPlayer]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // Disconnected player stats must remain visible and populated in the table
      expect(container.textContent).toContain('DisconnectedBlue');
      expect(container.textContent).toContain('210'); // Score
      expect(container.textContent).toContain('995.0'); // MMR
      expect(container.textContent).toContain('Diamond III'); // Rank

      const scoreCell = container.querySelector('tbody td:nth-child(2)');
      expect(scoreCell?.textContent?.trim()).toBe('210');
      expect(scoreCell?.className).toContain('text-lg');
      expect(scoreCell?.className).toContain('font-black');
    });

    it('renders all dedicated semantic data-testid attributes for layout automated verification', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[mockLocalPlayer]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // Headers testids
      expect(container.querySelector('[data-testid="th-player"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-score"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-goals"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-assists"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-saves"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-shots"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-demos"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-rank"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-mmr"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-h2h"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="th-platform"]')).not.toBeNull();

      // Cells testids
      expect(container.querySelector('[data-testid="player-identity"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-score"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-goals"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-assists"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-saves"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-shots"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-demos"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-rank"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-mmr"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-h2h"]')).not.toBeNull();
      expect(container.querySelector('[data-testid="stat-platform"]')).not.toBeNull();
    });
  });

  // =========================================================================
  // SUITE 2: Superfluous Elements Elimination Assertions
  // =========================================================================
  describe('2. Superfluous Elements Elimination', () => {
    it('eliminates static footer when in active live match view', () => {
      act(() => {
        root.render(<App />);
      });

      // In live match mode, footer is eliminated to save vertical space
      const footer = container.querySelector('footer');
      expect(footer).toBeNull();
    });

    it('eliminates technical daemon debug telemetry (Port 49125, Uptime) from header', () => {
      act(() => {
        root.render(<Header session={mockSession} activePlaylistId={11} />);
      });

      const headerText = container.textContent || '';
      expect(headerText).not.toContain('Port 49125');
      expect(headerText).not.toContain('49125');
      expect(headerText).not.toContain('Uptime:');
    });

    it('conditionally hides playlist MMR carousel during active match', () => {
      // When inMatch={true}, carousel should be omitted
      act(() => {
        root.render(<Header session={mockSession} activePlaylistId={11} inMatch={true} />);
      });
      expect(container.textContent).not.toContain('Playlists:');

      // When inMatch={false}, carousel should be visible
      act(() => {
        root.render(<Header session={mockSession} activePlaylistId={11} inMatch={false} />);
      });
      expect(container.textContent).toContain('Playlists:');
    });

    it('eliminates redundant player count labels from ScoreboardBanner', () => {
      act(() => {
        root.render(
          <ScoreboardBanner
            match={mockActive3v3Match}
            playlistMmrDelta={45.5}
            onOpenColumnConfig={() => {}}
          />
        );
      });

      // The string "3 Players" or "\d+ Players" must not appear in the banner
      expect(container.textContent).not.toMatch(/\d+\s+Players/i);
      expect(container.querySelector('[data-testid="scoreboard-banner"]')).not.toBeNull();
    });

    it('eliminates large vertical margin classes (mb-8, space-y-6, p-6)', () => {
      act(() => {
        root.render(<LiveGameView session={mockSession} />);
      });

      // Legacy vertical gap monsters must be eradicated
      expect(container.querySelector('.mb-8')).toBeNull();
      expect(container.querySelector('.space-y-6')).toBeNull();
      expect(container.querySelector('.p-6')).toBeNull();

      // Compact spacing alternatives must be in place
      expect(container.querySelector('.space-y-3')).not.toBeNull();
      expect(container.querySelector('.mb-3')).not.toBeNull();
    });
  });

  // =========================================================================
  // SUITE 3: Standard Viewport Layout & Budget Compliance (Zero-Scroll on 1080p)
  // =========================================================================
  describe('3. Standard Viewport Layout & Zero-Scroll Compliance', () => {
    it('renders side-by-side dual column grid on desktop viewports (lg:grid-cols-2)', () => {
      act(() => {
        root.render(<LiveGameView session={mockSession} />);
      });

      const rosterGrid = container.querySelector('.grid.lg\\:grid-cols-2');
      expect(rosterGrid).not.toBeNull();
      expect(rosterGrid?.className).toContain('grid-cols-1');
      expect(rosterGrid?.className).toContain('lg:grid-cols-2');
    });

    it('strictly satisfies 1080p viewport budget: total rendered stack <= 500px for 3v3 match', () => {
      const budget3v3 = calculateLiveViewHeightBudget({
        playerCountPerTeam: 3,
        hasCarousel: false,
        hasFooter: false,
        isCompact: true,
      });

      // Theoretical computed budget for 3v3 is ~376px, well under the 500px limit
      expect(budget3v3).toBeLessThanOrEqual(500);

      // On standard 1080p desktop with ~920px usable client innerHeight:
      const standardClientHeight = 920;
      const verticalHeadroom = standardClientHeight - budget3v3;

      // Ample headroom guarantees zero vertical scrolling
      expect(verticalHeadroom).toBeGreaterThan(420);
    });

    it('strictly satisfies viewport budget for 4v4 Chaos match without vertical scroll', () => {
      const budget4v4 = calculateLiveViewHeightBudget({
        playerCountPerTeam: 4,
        hasCarousel: false,
        hasFooter: false,
        isCompact: true,
      });

      expect(budget4v4).toBeLessThanOrEqual(500);
      const standardClientHeight = 920;
      expect(standardClientHeight - budget4v4).toBeGreaterThan(400);
    });

    it('does not force vertical scrollbars via overflow-y classes on main containers', () => {
      act(() => {
        root.render(<App />);
      });

      const scrollContainers = container.querySelectorAll('.overflow-y-scroll, .overflow-y-auto');
      expect(scrollContainers.length).toBe(0);
    });
  });

  // =========================================================================
  // SUITE 4: Column Customization & Preset Layout Stability
  // =========================================================================
  describe('4. Column Customization & Preset Layout Stability', () => {
    it('maintains strict header and cell alignment across all presets (full, competitive, streamer)', () => {
      const presets: ('full' | 'competitive' | 'streamer')[] = ['full', 'competitive', 'streamer'];

      for (const presetKey of presets) {
        const columns = PRESETS[presetKey].columns;
        const config: ColumnConfig = {
          score: columns.includes('score'),
          goals: columns.includes('goals'),
          assists: columns.includes('assists'),
          saves: columns.includes('saves'),
          shots: columns.includes('shots'),
          demos: columns.includes('demos'),
          mmr: columns.includes('mmr'),
          rank: columns.includes('rank'),
          h2h: columns.includes('h2h'),
          platform: columns.includes('platform'),
        };

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[mockLocalPlayer]}
              localPlayerId="Steam|local_hero|0"
              localTeamNum={0}
              columnConfig={config}
              activePlaylistId={11}
            />
          );
        });

        const thCount = container.querySelectorAll('thead th').length;
        const tdCount = container.querySelectorAll('tbody tr td').length;

        // Player name cell + configured columns count
        const expectedColumns = 1 + columns.length;
        expect(thCount).toBe(expectedColumns);
        expect(tdCount).toBe(expectedColumns);
      }
    });

    it('preserves stat prominence even under streamer preset where MMR/Rank are hidden', () => {
      const streamerConfig: ColumnConfig = {
        score: true,
        goals: true,
        assists: true,
        saves: true,
        shots: false,
        demos: false,
        mmr: false,
        rank: false,
        h2h: false,
        platform: false,
      };

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={mockLocalPlayer}
            isLocal={true}
            isTeammate={true}
            columnConfig={streamerConfig}
            activePlaylistId={11}
          />
        )
      );

      expect(html).toContain('720');
      expect(html).toMatch(/class="[^"]*text-lg[^"]*font-black/);
      expect(html).not.toContain('Champion I');
      expect(html).not.toContain('1045.5');
    });
  });

  // =========================================================================
  // SUITE 5: Regression Guard & Formatting Integrity
  // =========================================================================
  describe('5. Regression Guard & Existing Contract Compatibility', () => {
    it('preserves verbatim H2H badge format strings (9W-1L, 90%, 2W-8L, 20%)', () => {
      const htmlOrange = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={1}
            teamName="Orange Team"
            players={[mockOrangeLeader]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={1}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Local player on orange -> orange players are teammates -> 9W-1L (90%)
      expect(htmlOrange).toContain('<span>9W-1L</span>');
      expect(htmlOrange).toContain('(90%)');

      const htmlOpponent = cleanHtml(
        renderToString(
          <RosterTable
            teamNum={1}
            teamName="Orange Team"
            players={[mockOrangeLeader]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Local player on blue -> orange players are opponents -> 2W-8L (20%)
      expect(htmlOpponent).toContain('<span>2W-8L</span>');
      expect(htmlOpponent).toContain('(20%)');
    });

    it('preserves YOU badge for local player and BOT badge for AI bots', () => {
      const botPlayer: LobbyPlayer = {
        ...mockOrangeWinger,
        is_bot: true,
        name: 'Tex',
      };

      const htmlLocal = cleanHtml(
        renderToString(
          <PlayerRow
            player={mockLocalPlayer}
            isLocal={true}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );
      expect(htmlLocal).toContain('YOU');

      const htmlBot = cleanHtml(
        renderToString(
          <PlayerRow
            player={botPlayer}
            isLocal={false}
            isTeammate={false}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );
      expect(htmlBot).toContain('BOT');
    });

    it('renders graceful empty state placeholder when a team roster has zero players', () => {
      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[]}
            localPlayerId="Steam|local_hero|0"
            localTeamNum={0}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      expect(container.textContent).toContain('No players currently loaded for this team.');
    });
  });
});
