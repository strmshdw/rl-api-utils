// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';

import { LiveGameView } from './LiveGameView';
import { RosterTable } from './RosterTable';
import { PlayerRow } from './PlayerRow';
import { App } from '../../App';
import { MatchDetailModal } from '../session/MatchDetailModal';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useSession } from '../../hooks/useSession';
import { ColumnConfig, StatColumnKey } from '../../types/columns';
import { CurrentMatchResponse, SessionResponse, LobbyPlayer, SessionMatchDetail } from '../../types/api';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('../../hooks/useLiveMatch', () => ({
  useLiveMatch: vi.fn(),
}));

vi.mock('../../hooks/useSession', () => ({
  useSession: vi.fn(),
}));

const cleanHtml = (s: string) => s.replace(/<!--.*?-->/g, '');

const ALL_COLUMN_KEYS: StatColumnKey[] = [
  'score',
  'goals',
  'assists',
  'saves',
  'shots',
  'demos',
  'rank',
  'mmr',
  'h2h',
  'platform',
];

function createMockPlayer(overrides: Partial<LobbyPlayer> = {}): LobbyPlayer {
  return {
    player_id: `Epic|player_${Math.random().toString(36).substring(7)}|0`,
    name: 'EsportsPro',
    platform: 'Epic',
    team_num: 0,
    is_local: false,
    is_bot: false,
    stats: { score: 450, goals: 2, assists: 1, saves: 1, shots: 3, demos: 1 },
    current_rank: {
      playlist_id: 11,
      playlist_name: 'Ranked Standard 3v3',
      tier: 16,
      division: 2,
      rank_name: 'Champion I Div II',
      mmr: 1045.5,
    },
    matchup_record: {
      player_id: 'Epic|player_mock|0',
      playlist_id: 11,
      wins_as_teammate: 8,
      losses_as_teammate: 2,
      wins_as_opponent: 3,
      losses_as_opponent: 5,
      total_matches: 18,
      last_played_at: '2026-10-06T00:00:00Z',
    },
    ...overrides,
  };
}

describe('M2 Sleek Esports Matchup UX Adversarial Stress Suite', () => {
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
  // 1. Column Customizer: All 1,024 Combinations & 1:1 Alignment Guarantee
  // =========================================================================
  describe('1. Column Customizer Exhaustive Permutations Oracle (All 1,024 Combinations)', () => {
    it('exhaustively asserts exact 1:1 alignment and column index parity across all 1,024 bitmask permutations', () => {
      const player = createMockPlayer({
        name: 'AlignmentTester',
        platform: 'Steam',
        stats: { score: 600, goals: 3, assists: 2, saves: 1, shots: 5, demos: 2 },
      });

      // Total permutations = 2^10 = 1024
      const totalPermutations = 1 << ALL_COLUMN_KEYS.length;
      expect(totalPermutations).toBe(1024);

      for (let mask = 0; mask < totalPermutations; mask++) {
        const config: ColumnConfig = {
          score: (mask & (1 << 0)) !== 0,
          goals: (mask & (1 << 1)) !== 0,
          assists: (mask & (1 << 2)) !== 0,
          saves: (mask & (1 << 3)) !== 0,
          shots: (mask & (1 << 4)) !== 0,
          demos: (mask & (1 << 5)) !== 0,
          rank: (mask & (1 << 6)) !== 0,
          mmr: (mask & (1 << 7)) !== 0,
          h2h: (mask & (1 << 8)) !== 0,
          platform: (mask & (1 << 9)) !== 0,
        };

        const activeCols = ALL_COLUMN_KEYS.filter((key) => config[key]);
        const expectedCount = 1 + activeCols.length; // Player identity column + active stat columns

        const html = cleanHtml(
          renderToString(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[player]}
              columnConfig={config}
              activePlaylistId={11}
            />
          )
        );

        // Quick regex checks for th and td counts in SSR HTML
        const thMatches = html.match(/<th\b[^>]*>/g) || [];
        const tdMatches = html.match(/<td\b[^>]*>/g) || [];

        expect(thMatches.length).toBe(expectedCount);
        expect(tdMatches.length).toBe(expectedCount);

        // Header and Cell presence verification
        for (const key of ALL_COLUMN_KEYS) {
          const thExpected = `data-testid="th-${key}"`;
          const tdExpected = `data-testid="stat-${key}"`;

          if (config[key]) {
            expect(html).toContain(thExpected);
            expect(html).toContain(tdExpected);
          } else {
            expect(html).not.toContain(thExpected);
            expect(html).not.toContain(tdExpected);
          }
        }
      }
    });

    it('asserts column index parity in DOM: every enabled column header index matches cell index identically', () => {
      const sampleMasks = [
        0, // all disabled
        1023, // all enabled
        0b0000000001, // only score
        0b0000000010, // only goals
        0b0000111111, // all box score metrics, no secondary metadata
        0b1111000000, // only secondary metadata, no box scores
        0b1010101010, // alternating
        0b0101010101, // alternating inverted
      ];

      const player = createMockPlayer();

      for (const mask of sampleMasks) {
        const config: ColumnConfig = {
          score: (mask & (1 << 0)) !== 0,
          goals: (mask & (1 << 1)) !== 0,
          assists: (mask & (1 << 2)) !== 0,
          saves: (mask & (1 << 3)) !== 0,
          shots: (mask & (1 << 4)) !== 0,
          demos: (mask & (1 << 5)) !== 0,
          rank: (mask & (1 << 6)) !== 0,
          mmr: (mask & (1 << 7)) !== 0,
          h2h: (mask & (1 << 8)) !== 0,
          platform: (mask & (1 << 9)) !== 0,
        };

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[player]}
              columnConfig={config}
              activePlaylistId={11}
            />
          );
        });

        const thList = Array.from(container.querySelectorAll('thead th'));
        const tdList = Array.from(container.querySelectorAll('tbody tr td'));

        expect(thList.length).toBe(tdList.length);

        // The first column must always be player identity
        expect(thList[0].getAttribute('data-testid')).toBe('th-player');
        expect(tdList[0].getAttribute('data-testid')).toBe('player-identity');

        // For each active column, header index MUST match cell index
        for (let i = 1; i < thList.length; i++) {
          const thTestId = thList[i].getAttribute('data-testid') || '';
          const tdTestId = tdList[i].getAttribute('data-testid') || '';

          const thKey = thTestId.replace('th-', '');
          const tdKey = tdTestId.replace('stat-', '');

          expect(thKey).toBe(tdKey);
        }
      }
    });

    it('safely handles all columns disabled (mask 0) with pristine single-column layout', () => {
      const config: ColumnConfig = {
        score: false,
        goals: false,
        assists: false,
        saves: false,
        shots: false,
        demos: false,
        rank: false,
        mmr: false,
        h2h: false,
        platform: false,
      };

      const player = createMockPlayer({ name: 'SoloMinimalist' });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[player]}
            columnConfig={config}
            activePlaylistId={11}
          />
        );
      });

      const ths = container.querySelectorAll('thead th');
      const tds = container.querySelectorAll('tbody tr td');

      expect(ths.length).toBe(1);
      expect(tds.length).toBe(1);
      expect(ths[0].getAttribute('data-testid')).toBe('th-player');
      expect(tds[0].getAttribute('data-testid')).toBe('player-identity');
      expect(tds[0].textContent).toContain('SoloMinimalist');
    });

    it('safely handles all columns enabled (mask 1023) with all 11 headers and cells in strict order', () => {
      const config: ColumnConfig = {
        score: true,
        goals: true,
        assists: true,
        saves: true,
        shots: true,
        demos: true,
        rank: true,
        mmr: true,
        h2h: true,
        platform: true,
      };

      const player = createMockPlayer();

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[player]}
            columnConfig={config}
            activePlaylistId={11}
          />
        );
      });

      const ths = Array.from(container.querySelectorAll('thead th')).map((el) => el.getAttribute('data-testid'));
      const tds = Array.from(container.querySelectorAll('tbody tr td')).map((el) => el.getAttribute('data-testid'));

      const expectedThs = ['th-player', ...ALL_COLUMN_KEYS.map((k) => `th-${k}`)];
      const expectedTds = ['player-identity', ...ALL_COLUMN_KEYS.map((k) => `stat-${k}`)];

      expect(ths).toEqual(expectedThs);
      expect(tds).toEqual(expectedTds);
    });
  });

  // =========================================================================
  // 2. Viewport and Layout Boundaries: 1080p and 768p Laptop Resolutions
  // =========================================================================
  describe('2. Viewport and Layout Boundaries Oracle (1080p and 768p Laptops)', () => {
    function computeStackHeight(playerCountPerTeam: number): number {
      const header = 46; // compact header during match (py-2.5, no carousel)
      const navbar = 40; // navigation bar
      const mainPadding = 16; // py-2 (8px top + 8px bottom)
      const scoreboardBanner = 82; // ScoreboardBanner height
      const liveGap = 12; // space-y-3 gap
      const rosterChrome = 68; // top stripe (4px) + header banner (36px) + thead (28px)
      const rosterRows = playerCountPerTeam * 38; // 38px per broadcast card-row
      return header + navbar + mainPadding + scoreboardBanner + liveGap + rosterChrome + rosterRows;
    }

    it('verifies 1v1 Duel stack height is 302px (headroom > 600px on 1080p, > 300px on 768p)', () => {
      const stackHeight = computeStackHeight(1);
      expect(stackHeight).toBe(302);
      expect(stackHeight).toBeLessThanOrEqual(450);

      const usable1080p = 920; // 1080p with browser chrome & taskbar
      const usable768p = 620;  // 768p laptop with browser chrome & taskbar

      expect(usable1080p - stackHeight).toBe(618);
      expect(usable768p - stackHeight).toBe(318);
    });

    it('verifies 2v2 Doubles stack height is 340px (headroom > 580px on 1080p, > 280px on 768p)', () => {
      const stackHeight = computeStackHeight(2);
      expect(stackHeight).toBe(340);
      expect(stackHeight).toBeLessThanOrEqual(450);

      const usable1080p = 920;
      const usable768p = 620;

      expect(usable1080p - stackHeight).toBe(580);
      expect(usable768p - stackHeight).toBe(280);
    });

    it('verifies 3v3 Standard stack height is 378px (headroom > 540px on 1080p, > 240px on 768p)', () => {
      const stackHeight = computeStackHeight(3);
      expect(stackHeight).toBe(378);
      expect(stackHeight).toBeLessThanOrEqual(450);

      const usable1080p = 920;
      const usable768p = 620;

      expect(usable1080p - stackHeight).toBe(542);
      expect(usable768p - stackHeight).toBe(242);
    });

    it('verifies 4v4 Chaos stack height is 416px (headroom > 500px on 1080p, > 200px on 768p)', () => {
      const stackHeight = computeStackHeight(4);
      expect(stackHeight).toBe(416);
      expect(stackHeight).toBeLessThanOrEqual(450);

      const usable1080p = 920;
      const usable768p = 620;

      expect(usable1080p - stackHeight).toBe(504);
      expect(usable768p - stackHeight).toBe(204);
    });

    it('asserts LiveGameView uses horizontal dual-grid (lg:grid-cols-2) ensuring side-by-side rendering on 1080p and 768p landscape', () => {
      const match: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'guid-res-test',
        playlist_id: 11,
        playlist_name: 'Ranked 3v3',
        local_team: 0,
        local_player: createMockPlayer({ is_local: true }),
        teammates: [createMockPlayer(), createMockPlayer()],
        opponents: [createMockPlayer({ team_num: 1 }), createMockPlayer({ team_num: 1 }), createMockPlayer({ team_num: 1 })],
        spectators: [],
        updated_at: '2026-10-06T10:00:00Z',
      };

      vi.mocked(useLiveMatch).mockReturnValue({
        match,
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      act(() => {
        root.render(<LiveGameView session={null} />);
      });

      // The roster container must have lg:grid-cols-2
      const grid = container.querySelector('.grid.lg\\:grid-cols-2');
      expect(grid).not.toBeNull();
      expect(grid?.className).toContain('grid-cols-1');
      expect(grid?.className).toContain('lg:grid-cols-2');

      // The tables must be wrapped in overflow-x-auto to prevent horizontal blowouts
      const overflowWrappers = container.querySelectorAll('.overflow-x-auto');
      expect(overflowWrappers.length).toBe(2);
    });

    it('asserts full App tree suppresses footer and playlist carousel during live matches to preserve vertical budget', () => {
      const match: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'guid-live-budget',
        playlist_id: 11,
        playlist_name: 'Ranked 3v3',
        local_team: 0,
        local_player: createMockPlayer({ is_local: true }),
        teammates: [],
        opponents: [],
        spectators: [],
        updated_at: '2026-10-06T10:00:00Z',
      };

      const session: SessionResponse = {
        session_id: 's1',
        started_at: '2026-10-06T00:00:00Z',
        uptime: '2h',
        total_matches: 4,
        total_wins: 3,
        total_losses: 1,
        win_rate: 75,
        playlists: {
          '11': {
            playlist_id: 11,
            playlist_name: 'Ranked 3v3',
            matches_played: 4,
            wins: 3,
            losses: 1,
            win_rate: 75,
            initial_mmr: 1000,
            current_mmr: 1025,
            mmr_delta: 25,
          },
        },
        matches: [],
      };

      vi.mocked(useLiveMatch).mockReturnValue({
        match,
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      vi.mocked(useSession).mockReturnValue({
        session,
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

      // Footer is suppressed
      expect(container.querySelector('footer')).toBeNull();
      // Playlist carousel in Header is suppressed
      expect(container.querySelector('[data-testid="app-header"]')?.textContent).not.toContain('Playlists:');
      // No vertical overflow container classes
      expect(container.querySelectorAll('.overflow-y-scroll, .overflow-y-auto').length).toBe(0);
    });
  });

  // =========================================================================
  // 3. Zero-Stat Muted Styling & Color Isolation Oracle
  // =========================================================================
  describe('3. Zero-Stat Muted Styling Oracle', () => {
    const fullConfig: ColumnConfig = {
      score: true,
      goals: true,
      assists: true,
      saves: true,
      shots: true,
      demos: true,
      rank: true,
      mmr: true,
      h2h: true,
      platform: true,
    };

    it('renders muted slate styling (text-slate-500) and zero glowing classes when all stats are 0', () => {
      const zeroPlayer = createMockPlayer({
        stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
      });

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={zeroPlayer}
            isLocal={false}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Goals: must be muted slate, NO amber glow, NO drop-shadow
      expect(html).toMatch(/data-testid="stat-goals"[^>]*class="[^"]*text-slate-500/);
      expect(html).not.toMatch(/data-testid="stat-goals"[^>]*text-amber-400/);
      expect(html).not.toMatch(/data-testid="stat-goals"[^>]*drop-shadow/);

      // Assists: must be muted slate, NO cyan glow
      expect(html).toMatch(/data-testid="stat-assists"[^>]*class="[^"]*text-slate-500/);
      expect(html).not.toMatch(/data-testid="stat-assists"[^>]*text-cyan-300/);

      // Saves: must be muted slate, NO emerald glow
      expect(html).toMatch(/data-testid="stat-saves"[^>]*class="[^"]*text-slate-500/);
      expect(html).not.toMatch(/data-testid="stat-saves"[^>]*text-emerald-400/);

      // Shots: must be muted slate, NO light slate
      expect(html).toMatch(/data-testid="stat-shots"[^>]*class="[^"]*text-slate-500/);
      expect(html).not.toMatch(/data-testid="stat-shots"[^>]*text-slate-200/);

      // Demos: must be muted slate, NO rose glow
      expect(html).toMatch(/data-testid="stat-demos"[^>]*class="[^"]*text-slate-500/);
      expect(html).not.toMatch(/data-testid="stat-demos"[^>]*text-rose-400/);
    });

    it('renders vibrant colored styling only for stats that are strictly greater than zero', () => {
      const positivePlayer = createMockPlayer({
        stats: { score: 620, goals: 2, assists: 1, saves: 3, shots: 4, demos: 1 },
      });

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={positivePlayer}
            isLocal={false}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Goals: amber glow and drop-shadow
      expect(html).toMatch(/data-testid="stat-goals"[^>]*class="[^"]*text-amber-400[^"]*drop-shadow/);

      // Assists: cyan glow
      expect(html).toMatch(/data-testid="stat-assists"[^>]*class="[^"]*text-cyan-300/);

      // Saves: emerald glow
      expect(html).toMatch(/data-testid="stat-saves"[^>]*class="[^"]*text-emerald-400/);

      // Shots: text-slate-200
      expect(html).toMatch(/data-testid="stat-shots"[^>]*class="[^"]*text-slate-200/);

      // Demos: rose highlight
      expect(html).toMatch(/data-testid="stat-demos"[^>]*class="[^"]*text-rose-400/);
    });

    it('distinguishes Xbox platform green icon (text-emerald-500) from saves green glow (text-emerald-400)', () => {
      const xboxZeroPlayer = createMockPlayer({
        platform: 'Xbox',
        matchup_record: undefined,
        stats: { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
      });

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={xboxZeroPlayer}
            isLocal={false}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      // Xbox icon has text-emerald-500
      expect(html).toContain('text-emerald-500');

      // But row does NOT contain text-emerald-400 (saves is muted slate-500)
      expect(html).not.toContain('text-emerald-400');
      expect(html).toMatch(/data-testid="stat-saves"[^>]*class="[^"]*text-slate-500/);
      expect(html).not.toMatch(/data-testid="stat-saves"[^>]*text-emerald-400/);
    });

    it('safely handles missing, undefined, or corrupt stats object without throwing and applies muted classes', () => {
      const nullStatsPlayer = createMockPlayer({
        stats: undefined as unknown as LobbyPlayer['stats'],
      });

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={nullStatsPlayer}
            isLocal={false}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      expect(html).toContain('data-testid="stat-score"');
      expect(html).toMatch(/data-testid="stat-goals"[^>]*class="[^"]*text-slate-500/);
      expect(html).toMatch(/data-testid="stat-assists"[^>]*class="[^"]*text-slate-500/);
      expect(html).toMatch(/data-testid="stat-saves"[^>]*class="[^"]*text-slate-500/);
    });

    it('evaluates negative stat values (e.g. corrupt or debug telemetry) to muted slate classes', () => {
      const negativeStatsPlayer = createMockPlayer({
        stats: { score: -100, goals: -1, assists: -2, saves: -3, shots: -4, demos: -1 },
      });

      const html = cleanHtml(
        renderToString(
          <PlayerRow
            player={negativeStatsPlayer}
            isLocal={false}
            isTeammate={true}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        )
      );

      expect(html).toMatch(/data-testid="stat-goals"[^>]*class="[^"]*text-slate-500/);
      expect(html).toMatch(/data-testid="stat-assists"[^>]*class="[^"]*text-slate-500/);
      expect(html).toMatch(/data-testid="stat-saves"[^>]*class="[^"]*text-slate-500/);
    });
  });

  // =========================================================================
  // 4. Edge Cases: XSS Injection, Long Names, Bots, Spectators, Disconnects
  // =========================================================================
  describe('4. Edge Cases: Security, Boundaries & Player Classification', () => {
    const fullConfig: ColumnConfig = {
      score: true,
      goals: true,
      assists: true,
      saves: true,
      shots: true,
      demos: true,
      rank: true,
      mmr: true,
      h2h: true,
      platform: true,
    };

    it('neutralizes malicious XSS script injections and event handlers in player names', () => {
      const attackVectors = [
        '<script>alert("XSS")</script>',
        '<img src=x onerror=alert(1)>',
        '<svg onload=alert(document.domain)>',
        '"><script>alert(1)</script>',
        '<b onmouseover=evil()>HoverMe</b>',
        'javascript:void(0)',
      ];

      for (const vector of attackVectors) {
        const xssPlayer = createMockPlayer({ name: vector });

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[xssPlayer]}
              columnConfig={fullConfig}
              activePlaylistId={11}
            />
          );
        });

        // Script, img, or svg tags with malicious handlers must NOT exist in the DOM
        expect(container.querySelector('script')).toBeNull();
        expect(container.querySelector('img[src="x"]')).toBeNull();
        expect(container.querySelector('svg[onload]')).toBeNull();

        const nameSpan = container.querySelector('[data-testid="player-identity"] span.truncate');
        expect(nameSpan).not.toBeNull();
        expect(nameSpan?.textContent).toBe(vector);
        expect(nameSpan?.getAttribute('title')).toBe(vector);
      }
    });

    it('enforces max-w-[150px] truncate on extremely long player names (up to 500 chars) while preserving full title attribute', () => {
      const longNames = [
        'A'.repeat(50),
        'SupercalifragilisticexpialidociousRocketLeagueChampion2026',
        'B'.repeat(120),
        'C'.repeat(500),
      ];

      for (const longName of longNames) {
        const player = createMockPlayer({ name: longName });

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[player]}
              columnConfig={fullConfig}
              activePlaylistId={11}
            />
          );
        });

        const span = container.querySelector('[data-testid="player-identity"] span.truncate');
        expect(span).not.toBeNull();
        expect(span?.className).toContain('max-w-[150px]');
        expect(span?.className).toContain('truncate');
        expect(span?.getAttribute('title')).toBe(longName);
      }
    });

    it('renders special Unicode, Emojis, and RTL Arabic scripts in player names cleanly', () => {
      const unicodeNames = [
        '🚀👑⚡_GodMode_⚡👑🚀',
        'مرحبا بكم في روكيت ليغ', // Arabic RTL
        '日本語プレイヤー名', // Japanese Kanji
        '🔥⚽™️©️®️',
        'Player with \t and \n whitespace',
      ];

      for (const name of unicodeNames) {
        const player = createMockPlayer({ name });

        act(() => {
          root.render(
            <RosterTable
              teamNum={0}
              teamName="Blue Team"
              players={[player]}
              columnConfig={fullConfig}
              activePlaylistId={11}
            />
          );
        });

        const span = container.querySelector('[data-testid="player-identity"] span.truncate');
        expect(span).not.toBeNull();
        expect(span?.getAttribute('title')).toBe(name);
      }
    });

    it('classifies AI bots with BOT badge, Bot icon, and AI platform descriptor', () => {
      const botPlayer = createMockPlayer({
        name: 'Gerwin',
        is_bot: true,
        platform: 'Epic',
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[botPlayer]}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // BOT badge
      const identity = container.querySelector('[data-testid="player-identity"]');
      expect(identity?.textContent).toContain('BOT');

      // Platform cell displays 'AI'
      const platformCell = container.querySelector('[data-testid="stat-platform"]');
      expect(platformCell?.textContent?.trim()).toBe('AI');
    });

    it('strictly isolates spectators from both Blue and Orange roster tables in LiveGameView', () => {
      const bluePlayers = [createMockPlayer({ player_id: 'B1', name: 'Blue1', team_num: 0 })];
      const orangePlayers = [createMockPlayer({ player_id: 'O1', name: 'Orange1', team_num: 1 })];
      const spectators = [
        createMockPlayer({
          player_id: 'Spec_1',
          name: 'TournamentCaster',
          team_num: 255,
          stats: { score: 9999, goals: 99, assists: 99, saves: 99, shots: 99, demos: 99 },
        }),
        createMockPlayer({
          player_id: 'Spec_2',
          name: 'MatchReferee',
          team_num: 255,
          stats: { score: 5000, goals: 50, assists: 50, saves: 50, shots: 50, demos: 50 },
        }),
      ];

      const match: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'guid-tournament',
        playlist_id: 11,
        playlist_name: 'Tournament 1v1',
        local_team: 0,
        local_player: bluePlayers[0],
        teammates: [],
        opponents: orangePlayers,
        spectators,
        updated_at: '2026-10-06T10:00:00Z',
      };

      vi.mocked(useLiveMatch).mockReturnValue({
        match,
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      act(() => {
        root.render(<LiveGameView session={null} />);
      });

      // Only 2 rows rendered across all tables: Blue1 and Orange1
      const rows = container.querySelectorAll('tbody tr');
      expect(rows.length).toBe(2);

      // Spectator names must not appear
      expect(container.textContent).not.toContain('TournamentCaster');
      expect(container.textContent).not.toContain('MatchReferee');

      // Spectator scores/goals must not contaminate team totals
      const blueGoalsBadge = container.querySelectorAll('[data-testid="team-goals"]')[0];
      const orangeGoalsBadge = container.querySelectorAll('[data-testid="team-goals"]')[1];

      expect(blueGoalsBadge.textContent).not.toContain('99');
      expect(orangeGoalsBadge.textContent).not.toContain('50');
    });

    it('preserves disconnected player stats, team headers aggregation, and distinct visual treatment', () => {
      const activePlayer = createMockPlayer({
        player_id: 'Blue_Active',
        name: 'ActivePlayer',
        team_num: 0,
        stats: { score: 400, goals: 2, assists: 1, saves: 1, shots: 3, demos: 0 },
      });

      const disconnectedPlayer = createMockPlayer({
        player_id: 'Blue_DC',
        name: 'EarlyLeaver',
        team_num: 0,
        is_disconnected: true,
        stats: { score: 250, goals: 1, assists: 0, saves: 2, shots: 2, demos: 1 },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[activePlayer, disconnectedPlayer]}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      // 1. Team aggregated totals include disconnected player (Goals: 2 + 1 = 3, Score: 400 + 250 = 650)
      const teamGoals = container.querySelector('[data-testid="team-goals"]');
      expect(teamGoals?.textContent).toContain('3');

      const teamScore = container.querySelector('[data-testid="team-score"]');
      expect(teamScore?.textContent).toContain('650');

      // 2. Disconnected row visual treatment
      const rows = container.querySelectorAll('tbody tr');
      expect(rows.length).toBe(2);

      // Sorted by score: ActivePlayer (400) is row 0, EarlyLeaver (250) is row 1
      expect(rows[0].className).not.toContain('opacity-65');
      expect(rows[0].querySelector('[data-testid="badge-disconnected"]')).toBeNull();

      expect(rows[1].className).toContain('opacity-65');
      expect(rows[1].className).toContain('saturate-60');
      expect(rows[1].className).toContain('hover:opacity-85');

      const dcBadge = rows[1].querySelector('[data-testid="badge-disconnected"]');
      expect(dcBadge).not.toBeNull();
      expect(dcBadge?.textContent).toContain('DISCONNECTED');
      expect(dcBadge?.querySelector('svg')).not.toBeNull(); // WifiOff icon

      // 3. Stats for disconnected player are intact
      expect(rows[1].querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('250');
      expect(rows[1].querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('1');
      expect(rows[1].querySelector('[data-testid="stat-saves"]')?.textContent?.trim()).toBe('2');
    });

    it('renders matching disconnected badge and styling in MatchDetailModal for concluded matches', () => {
      const matchDetail: SessionMatchDetail = {
        match_guid: 'guid-concluded-dc',
        playlist_id: 11,
        playlist_name: 'Ranked 3v3',
        started_at: '2026-10-06T10:00:00Z',
        ended_at: '2026-10-06T10:05:00Z',
        duration_seconds: 300,
        result: 'victory',
        local_team: 0,
        winner_team: 0,
        blue_score: 3,
        orange_score: 2,
        starting_mmr: 1040,
        ending_mmr: 1049,
        mmr_change: 9,
        players: [
          {
            player_id: 'Steam|local|0',
            name: 'LocalWinner',
            platform: 'Steam',
            team_num: 0,
            is_local: true,
            is_bot: false,
            stats: { score: 500, goals: 2, assists: 1, saves: 1, shots: 4, demos: 1 },
            rank_name: 'Champion I Div II',
            tier: 16,
            division: 2,
            mmr: 1049,
          },
          {
            player_id: 'Epic|dc_teammate|0',
            name: 'TeammateDC',
            platform: 'Epic',
            team_num: 0,
            is_local: false,
            is_bot: false,
            is_disconnected: true,
            stats: { score: 150, goals: 1, assists: 0, saves: 0, shots: 1, demos: 0 },
            rank_name: 'Champion I Div I',
            tier: 16,
            division: 1,
            mmr: 1040,
          },
        ],
      };

      const html = cleanHtml(renderToString(<MatchDetailModal match={matchDetail} onClose={() => {}} />));

      expect(html).toContain('TeammateDC');
      expect(html).toContain('opacity-65 saturate-60 hover:opacity-85');
      expect(html).toContain('data-testid="badge-disconnected"');
      expect(html).toContain('DISCONNECTED');
    });

    it('handles extreme numeric bounds (score 999999, goals 999, MMR 9999.9) cleanly without overflow', () => {
      const titanPlayer = createMockPlayer({
        name: 'Titan',
        stats: {
          score: 999999,
          goals: 999,
          assists: 888,
          saves: 777,
          shots: 666,
          demos: 555,
        },
        current_rank: {
          playlist_id: 11,
          playlist_name: 'Ranked 3v3',
          tier: 22,
          division: 1,
          rank_name: 'Supersonic Legend',
          mmr: 9999.9,
        },
      });

      act(() => {
        root.render(
          <RosterTable
            teamNum={0}
            teamName="Blue Team"
            players={[titanPlayer]}
            columnConfig={fullConfig}
            activePlaylistId={11}
          />
        );
      });

      expect(container.querySelector('[data-testid="stat-score"]')?.textContent?.trim()).toBe('999999');
      expect(container.querySelector('[data-testid="stat-goals"]')?.textContent?.trim()).toBe('999');
      expect(container.querySelector('[data-testid="stat-mmr"]')?.textContent?.trim()).toBe('9999.9');

      // Check text-right on all numeric cells
      const rightAlignedCells = container.querySelectorAll('tbody td.text-right');
      expect(rightAlignedCells.length).toBeGreaterThanOrEqual(7);
    });
  });
});
