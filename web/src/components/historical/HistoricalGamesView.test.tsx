// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { HistoricalGamesView } from './HistoricalGamesView';
import { HistoricalMatchCard } from './HistoricalMatchCard';
import { HistoricalMatchDetailModal } from './HistoricalMatchDetailModal';
import { isDemoMode } from '../../utils/sampleData';
import { LiveGameView } from '../live/LiveGameView';
import { SessionHistoryView } from '../session/SessionHistoryView';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useSession } from '../../hooks/useSession';
import { MatchesResponse, HistoricalMatchRecord, CurrentMatchResponse } from '../../types/api';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

vi.mock('../../hooks/useLiveMatch', () => ({
  useLiveMatch: vi.fn(),
}));

vi.mock('../../hooks/useSession', () => ({
  useSession: vi.fn(),
}));

describe('Historical Games & Preview / Demo Mode Test Suite', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  const originalLocation = window.location;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    vi.restoreAllMocks();
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
    // Reset window.location
    delete (window as { location?: unknown }).location;
    window.location = originalLocation;
  });

  // Helper to set URL query string
  const setQueryString = (query: string) => {
    delete (window as { location?: unknown }).location;
    window.location = new URL(`http://localhost:3000/${query}`) as unknown as Location;
  };

  describe('1. Demo / Preview Mode Detection', () => {
    it('detects demo=true correctly', () => {
      setQueryString('?demo=true');
      expect(isDemoMode()).toBe(true);
    });

    it('detects test=true correctly', () => {
      setQueryString('?test=true');
      expect(isDemoMode()).toBe(true);
    });

    it('detects preview=true correctly', () => {
      setQueryString('?preview=true');
      expect(isDemoMode()).toBe(true);
    });

    it('returns false when no preview query param is present', () => {
      setQueryString('');
      expect(isDemoMode()).toBe(false);
    });

    it('returns false when param is not true', () => {
      setQueryString('?demo=false');
      expect(isDemoMode()).toBe(false);
    });
  });

  describe('2. HistoricalMatchCard and Modal Rendering', () => {
    const mockMatch: HistoricalMatchRecord = {
      match_guid: 'test-guid-123',
      record_start_timestamp: 1700000000,
      map_name: 'DFH Stadium',
      playlist: 11,
      replay_url: '',
      download_status: 'DOWNLOADED',
      local_file_path: 'replays/test.replay',
      upload_status: 'UPLOADED',
      ballchasing_id: 'bc-123',
      ballchasing_url: 'https://ballchasing.com/replay/bc-123',
      retry_count: 0,
      last_error: '',
      blue_score: 3,
      orange_score: 1,
      replay_stats_json: '',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      result: 'victory',
      stats: {
        match_guid: 'test-guid-123',
        map_name: 'DFH Stadium',
        playlist: 11,
        blue_score: 3,
        orange_score: 1,
        duration: 320,
        overtime: false,
        date: new Date().toISOString(),
        ballchasing_url: 'https://ballchasing.com/replay/bc-123',
        players: [
          { name: 'AcePlayer', platform: 'Epic', online_id: 'p1', team: 0, score: 500, goals: 2, assists: 1, saves: 2, shots: 4, demos: 1 },
          { name: 'Wingman', platform: 'Steam', online_id: 'p2', team: 0, score: 350, goals: 1, assists: 2, saves: 1, shots: 3, demos: 0 },
          { name: 'Rival1', platform: 'PlayStation', online_id: 'p3', team: 1, score: 400, goals: 1, assists: 0, saves: 3, shots: 5, demos: 2 },
          { name: 'Rival2', platform: 'Xbox', online_id: 'p4', team: 1, score: 280, goals: 0, assists: 1, saves: 1, shots: 2, demos: 0 },
        ],
      },
    };

    it('renders match card with scores, map, and win badge', () => {
      const handleClick = vi.fn();
      act(() => {
        root.render(<HistoricalMatchCard match={mockMatch} onClick={handleClick} />);
      });

      expect(container.textContent).toContain('DFH Stadium');
      expect(container.textContent).toContain('WIN');
      expect(container.textContent).toContain('3');
      expect(container.textContent).toContain('1');
      expect(container.textContent).toContain('Ranked Doubles 2v2');
      expect(container.textContent).toContain('AcePlayer');
    });

    it('renders detail modal with complete player box scores and MVP badge', () => {
      const handleClose = vi.fn();
      act(() => {
        root.render(<HistoricalMatchDetailModal match={mockMatch} onClose={handleClose} />);
      });

      expect(container.textContent).toContain('Historical Match Details');
      expect(container.textContent).toContain('Blue Team');
      expect(container.textContent).toContain('Orange Team');
      expect(container.textContent).toContain('AcePlayer');
      expect(container.textContent).toContain('MVP');
      expect(container.textContent).toContain('View on Ballchasing.com');
      expect(container.textContent).toContain('test-guid-123');
    });
  });

  describe('3. HistoricalGamesView Fetching, Syncing & Filtering', () => {
    it('fetches matches from /api/matches and displays them', async () => {
      const mockMatchesResp: MatchesResponse = {
        matches: [
          {
            match_guid: 'guid-001',
            record_start_timestamp: 1700000000,
            map_name: 'Neo Tokyo',
            playlist: 13,
            replay_url: '',
            download_status: 'DOWNLOADED',
            local_file_path: '',
            upload_status: 'UPLOADED',
            ballchasing_id: 'bc-tokyo',
            ballchasing_url: '',
            retry_count: 0,
            last_error: '',
            blue_score: 4,
            orange_score: 2,
            replay_stats_json: '',
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
            result: 'victory',
          },
        ],
        total: 1,
        limit: 20,
        offset: 0,
      };

      global.fetch = vi.fn().mockImplementation((url: string) => {
        if (url.includes('/api/sync/ballchasing/status')) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve({ running: false, total_replays: 0, synced_matches: 0, synced_players: 0 }),
          });
        }
        if (url.includes('/api/matches')) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve(mockMatchesResp),
          });
        }
        return Promise.reject(new Error(`Unhandled URL: ${url}`));
      });

      await act(async () => {
        root.render(<HistoricalGamesView />);
      });

      expect(container.textContent).toContain('Historical Matches Database');
      expect(container.textContent).toContain('Neo Tokyo');
      expect(container.textContent).toContain('Ranked Standard (3v3)');
    });

    it('triggers sync via POST /api/sync/ballchasing on button click', async () => {
      let syncTriggered = false;
      global.fetch = vi.fn().mockImplementation((url: string, opts?: { method?: string }) => {
        if (url.includes('/api/sync/ballchasing') && opts?.method === 'POST') {
          syncTriggered = true;
          return Promise.resolve({
            ok: true,
            status: 202,
            json: () => Promise.resolve({ status: 'started' }),
          });
        }
        if (url.includes('/api/sync/ballchasing/status')) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve({ running: syncTriggered, total_replays: 50, synced_matches: 10, synced_players: 25 }),
          });
        }
        if (url.includes('/api/matches')) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve({ matches: [], total: 0, limit: 20, offset: 0 }),
          });
        }
        return Promise.reject(new Error(`Unhandled URL: ${url}`));
      });

      await act(async () => {
        root.render(<HistoricalGamesView />);
      });

      const allButtons = Array.from(container.querySelectorAll('button'));
      const triggerBtn = allButtons.find((b) => b.textContent?.includes('Sync from Ballchasing'));
      expect(triggerBtn).toBeDefined();

      await act(async () => {
        triggerBtn?.click();
      });

      expect(syncTriggered).toBe(true);
    });

    it('falls back to demo historical matches when in demo mode and store is empty', async () => {
      setQueryString('?preview=true');

      global.fetch = vi.fn().mockImplementation((url: string) => {
        if (url.includes('/api/sync/ballchasing/status')) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve({ running: false }),
          });
        }
        if (url.includes('/api/matches')) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve({ matches: [], total: 0, limit: 20, offset: 0 }),
          });
        }
        return Promise.reject(new Error(`Unhandled URL: ${url}`));
      });

      await act(async () => {
        root.render(<HistoricalGamesView />);
      });

      expect(container.textContent).toContain('SIMULATED PREVIEW / DEMO MODE ACTIVE');
      expect(container.textContent).toContain('DFH Stadium');
      expect(container.textContent).toContain('Champions Field');
      expect(container.textContent).toContain('Utopia Coliseum');
    });
  });

  describe('4. LiveGameView & SessionHistoryView Demo Mode Integration', () => {
    it('LiveGameView renders simulated scoreboard and GC rosters when ?preview=true is active and no match is playing', () => {
      setQueryString('?preview=true');

      const mockInactive: CurrentMatchResponse = {
        active_match: false,
        match_ended: false,
        match_guid: '',
        playlist_id: 0,
        playlist_name: '',
        teammates: [],
        opponents: [],
        updated_at: new Date().toISOString(),
      };

      vi.mocked(useLiveMatch).mockReturnValue({
        match: mockInactive,
        status: 'connected',
        lastUpdated: new Date(),
        error: null,
      });

      act(() => {
        root.render(<LiveGameView session={null} />);
      });

      expect(container.textContent).toContain('SIMULATED PREVIEW / DEMO MODE ACTIVE');
      expect(container.textContent).toContain('Ranked Doubles');
      expect(container.textContent).toContain('You (Demo Player)');
      expect(container.textContent).toContain('AeroPulse');
      expect(container.textContent).toContain('VortexRL');
      expect(container.textContent).toContain('Zenith_99');
      expect(container.textContent).toContain('Grand Champion I');
    });

    it('SessionHistoryView renders simulated session matches and KPI cards when ?preview=true is active', () => {
      setQueryString('?preview=true');

      vi.mocked(useSession).mockReturnValue({
        session: {
          session_id: 'empty',
          started_at: new Date().toISOString(),
          uptime: '0m',
          total_matches: 0,
          total_wins: 0,
          total_losses: 0,
          win_rate: 0,
          playlists: {},
          matches: [],
        },
        isLoading: false,
        error: null,
        refreshSession: vi.fn(),
        resetSession: vi.fn(),
        selectedMatch: null,
        setSelectedMatch: vi.fn(),
      });

      act(() => {
        root.render(<SessionHistoryView />);
      });

      expect(container.textContent).toContain('SIMULATED PREVIEW / DEMO MODE ACTIVE');
      expect(container.textContent).toContain('Session Match Analysis (5)');
      expect(container.textContent).toContain('Session Performance Metrics');
      expect(container.textContent).toContain('Ranked Doubles');
    });
  });
});
