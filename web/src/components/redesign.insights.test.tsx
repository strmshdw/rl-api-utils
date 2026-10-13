// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { renderToString } from 'react-dom/server';

import { ScoreboardBanner } from './live/ScoreboardBanner';
import { IdleStateView } from './live/IdleStateView';
import { MatchCard } from './session/MatchCard';
import { MatchDetailModal } from './session/MatchDetailModal';
import { SessionHistoryView } from './session/SessionHistoryView';
import { PlayerCard } from './players/PlayerCard';
import { PlayerProfileModal } from './players/PlayerProfileModal';
import { CurrentMatchResponse, SessionResponse, SessionMatchDetail, LobbyPlayer, PlayerSummary } from '../types/api';
import { useSession } from '../hooks/useSession';

// Tell React 19 act is supported in happy-dom environment
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('../hooks/useSession', () => ({
  useSession: vi.fn(),
}));

const cleanHtml = (s: string) => s.replace(/<!--.*?-->/g, '');

const mockBlueLeader: LobbyPlayer = {
  player_id: 'Steam|blue_lead|0',
  name: 'BlueLeader',
  platform: 'Steam',
  team_num: 0,
  is_local: true,
  is_bot: false,
  stats: { score: 650, goals: 2, assists: 1, saves: 3, shots: 4, demos: 1 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Doubles 2v2',
    tier: 17,
    division: 2,
    rank_name: 'Champion II Div II',
    mmr: 1120.0,
  },
};

const mockBlueTeammate: LobbyPlayer = {
  player_id: 'Epic|blue_mate|0',
  name: 'BlueMate',
  platform: 'Epic',
  team_num: 0,
  is_local: false,
  is_bot: false,
  stats: { score: 400, goals: 1, assists: 2, saves: 1, shots: 3, demos: 0 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Doubles 2v2',
    tier: 17,
    division: 1,
    rank_name: 'Champion II Div I',
    mmr: 1100.0,
  },
};

const mockOrangeOne: LobbyPlayer = {
  player_id: 'Steam|orange_1|0',
  name: 'OrangeOne',
  platform: 'Steam',
  team_num: 1,
  is_local: false,
  is_bot: false,
  stats: { score: 380, goals: 1, assists: 0, saves: 2, shots: 3, demos: 2 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Doubles 2v2',
    tier: 16,
    division: 4,
    rank_name: 'Champion I Div IV',
    mmr: 1070.0,
  },
};

const mockOrangeTwo: LobbyPlayer = {
  player_id: 'Xbox|orange_2|0',
  name: 'OrangeTwo',
  platform: 'Xbox',
  team_num: 1,
  is_local: false,
  is_bot: false,
  stats: { score: 250, goals: 0, assists: 1, saves: 1, shots: 2, demos: 0 },
  current_rank: {
    playlist_id: 11,
    playlist_name: 'Ranked Doubles 2v2',
    tier: 16,
    division: 3,
    rank_name: 'Champion I Div III',
    mmr: 1050.0,
  },
};

const mockLiveMatch: CurrentMatchResponse = {
  active_match: true,
  match_ended: false,
  match_guid: 'guid-live-insight',
  playlist_id: 11,
  playlist_name: 'Ranked Doubles 2v2',
  local_team: 0,
  local_player: mockBlueLeader,
  teammates: [mockBlueTeammate],
  opponents: [mockOrangeOne, mockOrangeTwo],
  updated_at: '2026-10-12T12:00:00Z',
};

const mockConcludedMatch: SessionMatchDetail = {
  match_guid: 'guid-concluded-1',
  playlist_id: 11,
  playlist_name: 'Ranked Doubles 2v2',
  started_at: '2026-10-12T11:50:00Z',
  ended_at: '2026-10-12T11:56:00Z',
  duration_seconds: 360,
  result: 'victory',
  local_team: 0,
  winner_team: 0,
  blue_score: 3,
  orange_score: 1,
  starting_mmr: 1111.0,
  ending_mmr: 1120.0,
  mmr_change: 9.0,
  players: [
    {
      player_id: 'Steam|blue_lead|0',
      name: 'BlueLeader',
      platform: 'Steam',
      team_num: 0,
      is_local: true,
      is_bot: false,
      stats: { score: 650, goals: 2, assists: 1, saves: 3, shots: 4, demos: 1 },
      rank_name: 'Champion II Div II',
      tier: 17,
      division: 2,
      mmr: 1120.0,
    },
    {
      player_id: 'Epic|blue_mate|0',
      name: 'BlueMate',
      platform: 'Epic',
      team_num: 0,
      is_local: false,
      is_bot: false,
      stats: { score: 400, goals: 1, assists: 2, saves: 1, shots: 3, demos: 0 },
      rank_name: 'Champion II Div I',
      tier: 17,
      division: 1,
      mmr: 1100.0,
      matchup_record: {
        player_id: 'Epic|blue_mate|0',
        playlist_id: 11,
        wins_as_teammate: 5,
        losses_as_teammate: 1,
        wins_as_opponent: 1,
        losses_as_opponent: 1,
        total_matches: 8,
        last_played_at: '2026-10-12T11:56:00Z',
      },
    },
    {
      player_id: 'Steam|orange_1|0',
      name: 'OrangeOne',
      platform: 'Steam',
      team_num: 1,
      is_local: false,
      is_bot: false,
      stats: { score: 380, goals: 1, assists: 0, saves: 2, shots: 3, demos: 2 },
      rank_name: 'Champion I Div IV',
      tier: 16,
      division: 4,
      mmr: 1070.0,
    },
    {
      player_id: 'Xbox|orange_2|0',
      name: 'OrangeTwo',
      platform: 'Xbox',
      team_num: 1,
      is_local: false,
      is_bot: false,
      is_disconnected: true,
      stats: { score: 250, goals: 0, assists: 1, saves: 1, shots: 2, demos: 0 },
      rank_name: 'Champion I Div III',
      tier: 16,
      division: 3,
      mmr: 1050.0,
    },
  ],
};

const mockConcludedMatch2: SessionMatchDetail = {
  match_guid: 'guid-concluded-2',
  playlist_id: 13,
  playlist_name: 'Ranked Standard 3v3',
  started_at: '2026-10-12T11:40:00Z',
  ended_at: '2026-10-12T11:45:00Z',
  duration_seconds: 300,
  result: 'defeat',
  local_team: 0,
  winner_team: 1,
  blue_score: 1,
  orange_score: 4,
  starting_mmr: 1050.0,
  ending_mmr: 1041.5,
  mmr_change: -8.5,
  players: [
    {
      player_id: 'Steam|blue_lead|0',
      name: 'BlueLeader',
      platform: 'Steam',
      team_num: 0,
      is_local: true,
      is_bot: false,
      stats: { score: 310, goals: 1, assists: 0, saves: 1, shots: 2, demos: 0 },
      rank_name: 'Champion I Div II',
      tier: 16,
      division: 2,
      mmr: 1041.5,
    },
  ],
};

const mockSession: SessionResponse = {
  session_id: 'sess-test',
  started_at: '2026-10-12T11:30:00Z',
  uptime: '45m',
  total_matches: 2,
  total_wins: 1,
  total_losses: 1,
  win_rate: 50.0,
  playlists: {
    '11': {
      playlist_id: 11,
      playlist_name: 'Ranked Doubles 2v2',
      matches_played: 1,
      wins: 1,
      losses: 0,
      win_rate: 100.0,
      initial_mmr: 1111.0,
      current_mmr: 1120.0,
      mmr_delta: 9.0,
    },
    '13': {
      playlist_id: 13,
      playlist_name: 'Ranked Standard 3v3',
      matches_played: 1,
      wins: 0,
      losses: 1,
      win_rate: 0.0,
      initial_mmr: 1050.0,
      current_mmr: 1041.5,
      mmr_delta: -8.5,
    },
  },
  matches: [mockConcludedMatch2, mockConcludedMatch],
};

describe('Redesigned Frontend UI Insights Suite', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    vi.restoreAllMocks();
  });

  describe('1. Live Match At-A-Glance Insights (ScoreboardBanner)', () => {
    it('calculates and displays team average MMR and advantage pill', () => {
      const html = cleanHtml(
        renderToString(
          <ScoreboardBanner
            match={mockLiveMatch}
            playlistMmrDelta={9.0}
            onOpenColumnConfig={() => {}}
          />
        )
      );

      // Blue average: (1120 + 1100) / 2 = 1110
      // Orange average: (1070 + 1050) / 2 = 1060
      // Differential: +50 Blue advantage
      expect(html).toContain('1110');
      expect(html).toContain('1060');
      expect(html).toContain('+50 Blue');
      expect(html).toContain('Avg MMR');
    });

    it('renders live momentum metrics (shots, saves, demos)', () => {
      const html = cleanHtml(
        renderToString(
          <ScoreboardBanner
            match={mockLiveMatch}
            playlistMmrDelta={9.0}
            onOpenColumnConfig={() => {}}
          />
        )
      );

      // Blue shots: 4 + 3 = 7, Orange shots: 3 + 2 = 5
      expect(html).toContain('Shots:');
      expect(html).toContain('7');
      expect(html).toContain('5');
      // Blue saves: 3 + 1 = 4, Orange saves: 2 + 1 = 3
      expect(html).toContain('Saves:');
      expect(html).toContain('4');
      expect(html).toContain('3');
      // Demos
      expect(html).toContain('Demos:');
    });
  });

  describe('2. Idle State Quick Recap & Session Momentum (IdleStateView)', () => {
    it('renders last match quick recap card with outcome, score, and MMR delta', () => {
      const html = cleanHtml(
        renderToString(<IdleStateView session={mockSession} />)
      );

      expect(html).toContain('Last Match Quick Recap');
      expect(html).toContain('VICTORY');
      expect(html).toContain('Ranked Doubles 2v2');
      expect(html).toContain('+9.0 MMR');
      expect(html).toContain('Your Team:');
      expect(html).toContain('Blue');
    });

    it('renders session streak, goal differential, and playlist trajectory', () => {
      const html = cleanHtml(
        renderToString(<IdleStateView session={mockSession} />)
      );

      expect(html).toContain('Current Streak');
      expect(html).toContain('1 Wins');
      expect(html).toContain('Goal Differential');
      expect(html).toContain('Playlists Trajectory');
      expect(html).toContain('Ranked Doubles 2v2');
      expect(html).toContain('Ranked Standard 3v3');
    });
  });

  describe('3. In-Depth Games Played Analysis (MatchCard & MatchDetailModal)', () => {
    it('renders MVP badge on match top scorer and local player stats snippet in MatchCard', () => {
      const html = cleanHtml(
        renderToString(<MatchCard match={mockConcludedMatch} onClick={() => {}} />)
      );

      expect(html).toContain('MVP');
      expect(html).toContain('G:');
      expect(html).toContain('SV:');
      expect(html).toContain('650');
      expect(html).toContain('Full Box Scores');
    });

    it('renders team head-to-head comparison and shot conversion accuracy in MatchDetailModal', () => {
      const html = cleanHtml(
        renderToString(<MatchDetailModal match={mockConcludedMatch} onClose={() => {}} />)
      );

      // Team Head-to-Head Comparison
      expect(html).toContain('Team Head-to-Head Comparison');
      expect(html).toContain('Total Score');
      expect(html).toContain('Shot Accuracy');
      expect(html).toContain('Saves (Defense)');
      expect(html).toContain('Demolitions');

      // Table contains shot accuracy and MVP badge
      expect(html).toContain('Shot Acc');
      expect(html).toContain('MVP');
      expect(html).toContain('50%'); // 2 goals / 4 shots = 50%

      // Preserves disconnected player contract
      expect(html).toContain('OrangeTwo');
      expect(html).toContain('data-testid="badge-disconnected"');
      expect(html).toContain('DISCONNECTED');
      expect(html).toContain('opacity-65 saturate-60 hover:opacity-85');
    });
  });

  describe('4. Session History View Analytics & Filtering', () => {
    beforeEach(() => {
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

    it('renders session performance metrics and KPI ribbon', () => {
      act(() => {
        root.render(<SessionHistoryView />);
      });

      expect(container.textContent).toContain('Session Performance Metrics');
      expect(container.textContent).toContain('Net Session MMR');
      expect(container.textContent).toContain('+0.5 MMR'); // 9.0 + (-8.5) = +0.5
      expect(container.textContent).toContain('Goal Differential');
      expect(container.textContent).toContain('Win Rate Progress');
      expect(container.textContent).toContain('50.0%');
    });

    it('filters matches dynamically by playlist filter', () => {
      act(() => {
        root.render(<SessionHistoryView />);
      });

      const select = container.querySelector('select') as HTMLSelectElement;
      expect(select).not.toBeNull();

      // Change playlist to '11' (Doubles)
      act(() => {
        select.value = '11';
        select.dispatchEvent(new Event('change', { bubbles: true }));
      });

      const matchCards = container.querySelectorAll('.deferred-card');
      expect(matchCards.length).toBe(1);
      expect(matchCards[0].textContent).toContain('Ranked Doubles 2v2');
      expect(matchCards[0].textContent).not.toContain('Ranked Standard 3v3');
    });
  });

  describe('5. Players Met Scouting & Synergy Analysis', () => {
    const mockAlly: PlayerSummary = {
      player_id: 'Steam|trusted_ally|0',
      platform: 'Steam',
      player_name: 'TrustedAlly',
      ranks_json: JSON.stringify({
        '11': { playlist_id: 11, playlist_name: '2v2', tier: 17, division: 2, rank_name: 'Champion II', mmr: 1130.0 },
      }),
      first_seen_at: '2026-10-01T00:00:00Z',
      last_seen_at: '2026-10-12T10:00:00Z',
      total_wins_as_teammate: 5,
      total_losses_as_teammate: 1, // 5W-1L = 83%
      total_wins_as_opponent: 0,
      total_losses_as_opponent: 0,
      total_matches: 6,
    };

    const mockRival: PlayerSummary = {
      player_id: 'Epic|tough_rival|0',
      platform: 'Epic',
      player_name: 'ToughRival',
      ranks_json: '{}',
      first_seen_at: '2026-10-01T00:00:00Z',
      last_seen_at: '2026-10-12T10:00:00Z',
      total_wins_as_teammate: 0,
      total_losses_as_teammate: 0,
      total_wins_as_opponent: 1,
      total_losses_as_opponent: 4, // 1W-4L = 20%
      total_matches: 5,
    };

    it('renders Trusted Ally badge and visual progress bar on PlayerCard', () => {
      const html = cleanHtml(
        renderToString(<PlayerCard player={mockAlly} onClick={() => {}} />)
      );

      expect(html).toContain('Trusted Ally');
      expect(html).toContain('Frequent');
      expect(html).toContain('5W');
      expect(html).toContain('1L');
      expect(html).toContain('83.3%');
      expect(html).toContain('Champion II');
      expect(html).toContain('Copy Player ID');
    });

    it('renders Tough Rival badge on PlayerCard for nemesis players', () => {
      const html = cleanHtml(
        renderToString(<PlayerCard player={mockRival} onClick={() => {}} />)
      );

      expect(html).toContain('Tough Rival');
      expect(html).toContain('Frequent');
      expect(html).toContain('1W');
      expect(html).toContain('4L');
      expect(html).toContain('20.0%');
    });

    it('renders --- instead of 0.0% on PlayerCard when a player has 0 encounters in that role', () => {
      const html = cleanHtml(
        renderToString(<PlayerCard player={mockAlly} onClick={() => {}} />)
      );
      // mockAlly has 0 opponent matches: opponent win rate must be --- rather than 0.0%
      expect(html).toContain('---');
      expect(html).toContain('0 matches');
    });

    it('renders PlayerProfileModal with clean null state when no playerId is passed', () => {
      const html = cleanHtml(
        renderToString(<PlayerProfileModal playerId={null} onClose={() => {}} />)
      );
      expect(html).toBe('');
    });
  });

  describe('6. Hardened Edge Cases & Adversarial Invariants', () => {
    it('does NOT award MVP to local player on a defeat even if local player had match-high score', () => {
      const mockDefeatMatch: SessionMatchDetail = {
        ...mockConcludedMatch,
        result: 'defeat',
        local_team: 0,
        winner_team: 1,
        blue_score: 1,
        orange_score: 2,
        players: [
          {
            ...mockConcludedMatch.players[0],
            stats: { score: 999, goals: 1, assists: 0, saves: 5, shots: 6, demos: 0 },
          },
          {
            ...mockConcludedMatch.players[1],
            stats: { score: 200, goals: 0, assists: 1, saves: 0, shots: 1, demos: 0 },
          },
          {
            ...mockConcludedMatch.players[2],
            team_num: 1,
            stats: { score: 450, goals: 2, assists: 0, saves: 1, shots: 3, demos: 0 },
          },
        ],
      };

      const html = cleanHtml(
        renderToString(<MatchCard match={mockDefeatMatch} onClick={() => {}} />)
      );

      expect(html).toContain('DEFEAT');
      // Local player has 999 score but team lost: MUST NOT have MVP badge
      expect(html).not.toContain('MVP');
    });

    it('terminates streak calculation immediately when the latest match is a draw or incomplete', () => {
      const sessionWithDraw: SessionResponse = {
        ...mockSession,
        matches: [
          mockConcludedMatch, // victory
          mockConcludedMatch2, // defeat
          mockConcludedMatch, // victory
          {
            ...mockConcludedMatch,
            match_guid: 'guid-draw-last',
            result: 'draw',
            blue_score: 2,
            orange_score: 2,
          },
        ],
      };

      const html = cleanHtml(
        renderToString(<IdleStateView session={sessionWithDraw} />)
      );

      expect(html).toContain('Current Streak');
      // Streak must be None because last match was a draw
      expect(html).toContain('None');
    });

    it('renders Even Match pill on ScoreboardBanner when MMR differential is less than 5 points', () => {
      const balancedMatch: CurrentMatchResponse = {
        ...mockLiveMatch,
        local_player: {
          ...mockBlueLeader,
          current_rank: { ...mockBlueLeader.current_rank!, mmr: 1002.0 },
        },
        teammates: [],
        opponents: [
          {
            ...mockOrangeOne,
            current_rank: { ...mockOrangeOne.current_rank!, mmr: 1000.0 },
          },
        ],
      };

      const html = cleanHtml(
        renderToString(
          <ScoreboardBanner match={balancedMatch} onOpenColumnConfig={() => {}} />
        )
      );

      expect(html).toContain('1002');
      expect(html).toContain('1000');
      expect(html).toContain('Even Match');
    });
  });
});

