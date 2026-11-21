// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { act } from 'react';
import { LiveGameView, lobbyPlayers } from './LiveGameView';
import { parityLabel } from './Scorebug';
import { statInk } from './LivePlayerCard';
import { CurrentMatchResponse, LobbyPlayer, SessionResponse } from '../../types/api';
import { SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from '../../utils/sampleData';
import { cleanup, mount } from '../../test/dom';

const STORAGE_KEY = 'rl_sync_column_config';

function lobby(over: Partial<LobbyPlayer> & { player_id: string; team_num: number }): LobbyPlayer {
  return {
    platform: 'Steam',
    name: over.player_id,
    is_local: false,
    is_bot: false,
    stats: { score: 100, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 },
    current_rank: { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 16, division: 1, rank_name: 'Grand Champion I', mmr: 1500 },
    ...over,
  };
}

function liveMatch(over: Partial<CurrentMatchResponse> = {}): CurrentMatchResponse {
  return { ...SAMPLE_CURRENT_MATCH, ...over };
}

import { DashboardProvider } from '../../context/DashboardContext';

const noop = () => {};

async function renderLive(match: CurrentMatchResponse | null, session: SessionResponse | null = SAMPLE_SESSION_RESPONSE) {
  return mount(
    <DashboardProvider>
      <LiveGameView match={match} session={session} connection="connected" columnsOpen={false} onCloseColumns={noop} onShowSession={noop} />
    </DashboardProvider>,
  );
}

const cards = (root: ParentNode) => Array.from(root.querySelectorAll<HTMLElement>('[data-testid="live-player-card"]'));
const statEl = (card: HTMLElement, key: string) => card.querySelector<HTMLElement>(`[data-stat="${key}"] span:last-child`)!;

describe('LiveGameView — in match', () => {
  beforeEach(() => localStorage.clear());
  afterEach(cleanup);

  it('renders the scorebug with team goals, playlist and parity', async () => {
    const { container } = await renderLive(liveMatch());
    const bug = container.querySelector('[data-testid="scorebug"]')!;
    expect(bug.querySelector('[data-testid="score-blue"]')?.textContent).toBe('3');
    expect(bug.querySelector('[data-testid="score-orange"]')?.textContent).toBe('2');
    expect(bug.textContent).toContain('RANKED 2V2');
    expect(bug.textContent).toContain('● LIVE');
    expect(bug.textContent).toContain('AVG 1494 · YOUR TEAM');
    expect(bug.textContent).toContain('EVEN MATCH');
  });

  it('applies the parity rule (|avg diff| < 5 is even)', () => {
    expect(parityLabel(1500, 1496)).toBe('EVEN MATCH');
    expect(parityLabel(1520, 1500)).toBe('+20 BLUE');
    expect(parityLabel(1480, 1500)).toBe('+20 ORANGE');
    expect(parityLabel(null, 1500)).toBeNull();
  });

  it('shows the result instead of LIVE once the match has ended', async () => {
    const { container } = await renderLive(liveMatch({ match_ended: true, result: 'defeat' }));
    const bug = container.querySelector('[data-testid="scorebug"]')!;
    expect(bug.textContent).toContain('LOSS');
    expect(bug.textContent).not.toContain('● LIVE');
  });

  it('puts the local player on a "You" card with the playlist-scoped session record', async () => {
    const { container } = await renderLive(liveMatch());
    const you = cards(container).find((c) => c.dataset.playerId === 'Epic|demo_local_player|0')!;
    expect(you.className).toContain('border-blue-ink');
    expect(you.textContent).toContain('You');
    expect(you.querySelector('[data-testid="player-h2h"]')?.textContent).toBe('THIS SESSION3–1');
  });

  it('labels teammates WITH YOU and opponents VS YOU using the matching record', async () => {
    const { container } = await renderLive(liveMatch());
    const h2h = (id: string) => cards(container).find((c) => c.dataset.playerId === id)!.querySelector('[data-testid="player-h2h"]')!.textContent;
    expect(h2h('Steam|76561198000000010|0')).toBe('WITH YOU4–1');
    expect(h2h('Steam|76561198000000020|0')).toBe('VS YOU2–3');
  });

  it('flips teammate/opponent orientation when the local player is on orange', async () => {
    const record = { player_id: '', playlist_id: 11, wins_as_teammate: 5, losses_as_teammate: 0, wins_as_opponent: 0, losses_as_opponent: 4, total_matches: 9, last_played_at: '' };
    const match = liveMatch({
      local_team: 1,
      local_player: lobby({ player_id: 'me', team_num: 1, is_local: true }),
      teammates: [lobby({ player_id: 'mate', team_num: 1, matchup_record: record })],
      opponents: [lobby({ player_id: 'opp', team_num: 0, matchup_record: record })],
    });
    const { container } = await renderLive(match);
    const orange = container.querySelector('[data-testid="team-orange"]')!;
    const blue = container.querySelector('[data-testid="team-blue"]')!;
    expect(orange.textContent).toContain('WITH YOU5–0');
    expect(blue.textContent).toContain('VS YOU0–4');
    expect(container.querySelector('[data-testid="scorebug"]')!.textContent).toMatch(/ORANGE.*YOUR TEAM/);
  });

  it('sorts each team by score and keeps 4v4 rosters separate, excluding spectators', async () => {
    const blue = [1, 2, 3].map((i) => lobby({ player_id: `b${i}`, team_num: 0, stats: { score: i * 100, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 } }));
    const orange = [1, 2, 3, 4].map((i) => lobby({ player_id: `o${i}`, team_num: 1 }));
    const match = liveMatch({
      local_player: lobby({ player_id: 'me', team_num: 0, is_local: true, stats: { score: 250, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 } }),
      teammates: blue,
      opponents: orange,
      spectators: [lobby({ player_id: 'spec', team_num: 255 })],
    });
    expect(lobbyPlayers(match).map((p) => p.player_id)).not.toContain('spec');
    const { container } = await renderLive(match);
    const blueIds = cards(container.querySelector('[data-testid="team-blue"]')!).map((c) => c.dataset.playerId);
    expect(blueIds).toEqual(['b3', 'me', 'b2', 'b1']);
    expect(cards(container.querySelector('[data-testid="team-orange"]')!)).toHaveLength(4);
    expect(container.textContent).not.toContain('spec');
  });

  it('de-duplicates a player reported in more than one list', async () => {
    const dup = lobby({ player_id: 'dup', team_num: 1 });
    const match = liveMatch({ opponents: [dup, dup] });
    const { container } = await renderLive(match);
    expect(cards(container).filter((c) => c.dataset.playerId === 'dup')).toHaveLength(1);
  });

  it('colours stat values to match their semantic stat labels, bolding lobby leaders and dimming zeros', async () => {
    expect(statInk(0, 0)).toBe('text-faint');
    expect(statInk(5, 6)).toBe('text-ink-3');
    expect(statInk(6, 6)).toBe('text-ink-strong');
    expect(statInk(5, 6, 'stat-lbl-goals')).toBe('stat-lbl-goals font-bold');
    expect(statInk(6, 6, 'stat-lbl-goals')).toBe('stat-lbl-goals font-black');

    const { container } = await renderLive(liveMatch());
    const byId = (id: string) => cards(container).find((c) => c.dataset.playerId === id)!;
    const you = byId('Epic|demo_local_player|0');
    const statLabel = (card: HTMLElement, key: string) => card.querySelector<HTMLElement>(`[data-stat="${key}"] span:first-child`)!;
    expect(statLabel(you, 'score').className).toContain('stat-lbl-score');
    expect(statLabel(you, 'goals').className).toContain('stat-lbl-goals');
    expect(statLabel(you, 'assists').className).toContain('stat-lbl-assists');
    expect(statLabel(you, 'saves').className).toContain('stat-lbl-saves');
    expect(statLabel(you, 'shots').className).toContain('stat-lbl-shots');
    expect(statLabel(you, 'demos').className).toContain('stat-lbl-demos');

    expect(statEl(you, 'score').className).toContain('stat-lbl-score'); // 480 is the lobby high
    expect(statEl(you, 'score').className).toContain('font-black');
    expect(statEl(you, 'goals').className).toContain('stat-lbl-goals'); // 2 goals
    expect(statEl(you, 'goals').className).toContain('font-black');
    expect(statEl(byId('Steam|76561198000000020|0'), 'demos').className).toContain('text-faint'); // 0 demos
    expect(statEl(byId('Steam|76561198000000020|0'), 'score').className).toContain('stat-lbl-score');
    expect(statEl(byId('Steam|76561198000000020|0'), 'score').className).toContain('font-bold');
  });

  it('respects the column config for the stat strip and meta line', async () => {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ score: false, goals: true, assists: true, saves: true, shots: false, demos: false, mmr: false, rank: true, h2h: false, platform: false }),
    );
    const { container } = await renderLive(liveMatch());
    const card = cards(container)[0];
    const keys = Array.from(card.querySelectorAll<HTMLElement>('[data-stat]')).map((e) => e.dataset.stat);
    expect(keys).toEqual(['goals', 'assists', 'saves']);
    expect(card.querySelector('[data-testid="player-h2h"]')).toBeNull();
    expect(card.querySelector('[data-testid="player-meta"]')?.textContent).toBe('Grand Champion I');
  });

  it('keeps disconnected players visible with their stats and a DISCONNECTED tag', async () => {
    const dc = lobby({ player_id: 'dc', team_num: 1, is_disconnected: true, stats: { score: 999, goals: 4, assists: 0, saves: 0, shots: 0, demos: 0 } });
    const { container } = await renderLive(liveMatch({ opponents: [dc] }));
    const card = cards(container).find((c) => c.dataset.playerId === 'dc')!;
    expect(card.className).toContain('opacity-60');
    expect(card.querySelector('[data-testid="badge-disconnected"]')?.textContent).toBe('DISCONNECTED');
    expect(statEl(card, 'score').textContent).toBe('999');
    expect(cards(container).find((c) => c.dataset.playerId !== 'dc')!.className).not.toContain('opacity-60');
  });

  it('survives missing stats, ranks and matchup records', async () => {
    const bare = { ...lobby({ player_id: 'bare', team_num: 1 }), stats: undefined, current_rank: undefined } as unknown as LobbyPlayer;
    const { container } = await renderLive(liveMatch({ opponents: [bare] }));
    const card = cards(container).find((c) => c.dataset.playerId === 'bare')!;
    expect(card.textContent).toContain('UNRANKED');
    expect(card.querySelector('[data-testid="player-h2h"]')?.textContent).toBe('VS YOU—');
    expect(statEl(card, 'score').textContent).toBe('0');
  });

  it('renders hostile player names as text', async () => {
    const xss = lobby({ player_id: 'x', team_num: 1, name: '<img src=x onerror=alert(1)>' });
    const { container } = await renderLive(liveMatch({ opponents: [xss] }));
    expect(container.querySelector('img')).toBeNull();
    expect(container.textContent).toContain('<img src=x onerror=alert(1)>');
  });

  it('renders Threat, Carry, and Off-Mode Peak / Streak badges and toggles historical split tables', async () => {
    const { container } = await renderLive(liveMatch());
    expect(container.textContent).toContain('◆ HIGHER MMR');
    expect(container.textContent).toContain('◆ PARTY CARRY');
    expect(container.textContent).toContain('◆ OFF-MODE PEAK');
    expect(container.textContent).toContain('◆ HOT STREAK');

    const youCard = cards(container).find((c) => c.dataset.playerId === 'Epic|demo_local_player|0')!;
    expect(youCard.textContent).toContain('YOU · ACTIVE SESSION');
    expect(youCard.textContent).toContain('IN RANGE 3W–1L');
    expect(youCard.textContent).toContain('NET MMR +29');

    const toggleBtn = container.querySelector<HTMLButtonElement>('#btn-toggle-live-hist')!;
    expect(toggleBtn).not.toBeNull();
    expect(toggleBtn.textContent).toContain('HISTORICAL DATA: SHOWN');
    expect(container.querySelectorAll('.live-hist-section').length).toBeGreaterThan(0);

    await act(async () => {
      toggleBtn.click();
    });
    expect(toggleBtn.textContent).toContain('HISTORICAL DATA: HIDDEN');
  });

  it('scopes player card Rank/MMR, matchups_by_playlist H2H, and THIS SESSION W–L when PLAYLIST SCOPE (3V3 / 1V1) is toggled', async () => {
    const { container } = await renderLive(liveMatch());
    const aeroCard = () => cards(container).find((c) => c.dataset.playerId === 'Steam|76561198000000010|0')!;
    const youCard = () => cards(container).find((c) => c.dataset.playerId === 'Epic|demo_local_player|0')!;

    // Concrete single-mode pills only (no ALL cross-mode pill); default (2V2) shows 2v2 rank (1502), 2v2 H2H (4–1), and 2v2 session record (3–1)
    const scopeLabels = Array.from(container.querySelectorAll<HTMLButtonElement>('.live-pl-btn')).map((b) =>
      b.textContent?.trim(),
    );
    expect(scopeLabels).not.toContain('ALL');
    expect(aeroCard().textContent).toContain('1502');
    expect(aeroCard().querySelector('[data-testid="player-h2h"]')?.textContent).toBe('WITH YOU4–1');
    expect(youCard().querySelector('[data-testid="player-h2h"]')?.textContent).toBe('THIS SESSION3–1');

    // Switch to 3V3 -> AeroPulse's 3v3 rank is Grand Champion III (1795), H2H uses matchups_by_playlist['13'] (2–0),
    // 2v2 mechanical averages (446) are NOT misattributed to 3v3, and You's session record scopes to 3v3 (1–0, +11)
    const btn3v3 = Array.from(container.querySelectorAll<HTMLButtonElement>('.live-pl-btn')).find(
      (b) => b.textContent?.trim() === '3V3',
    )!;
    await act(async () => {
      btn3v3.click();
    });
    expect(aeroCard().textContent).toContain('1795');
    expect(aeroCard().querySelector('[data-testid="player-h2h"]')?.textContent).toBe('WITH YOU2–0');
    expect(aeroCard().textContent).not.toContain('446');
    expect(youCard().querySelector('[data-testid="player-h2h"]')?.textContent).toBe('THIS SESSION1–0');
    expect(youCard().textContent).toContain('NET MMR +11');

    // Switch to 1V1 -> AeroPulse has no 1v1 rank or matches -> UNRANKED and 0G dimmed rows
    const btn1v1 = Array.from(container.querySelectorAll<HTMLButtonElement>('.live-pl-btn')).find(
      (b) => b.textContent?.trim() === '1V1',
    )!;
    await act(async () => {
      btn1v1.click();
    });
    expect(aeroCard().textContent).toContain('UNRANKED');
    expect(aeroCard().textContent).toContain('— NO MATCHES IN SELECTED RANGE —');
  });

  it('locks paired Blue/Orange card bands via CSS subgrid coordinates even under heavy multi-stamp asymmetry and updates Scorebug AVG by scope', async () => {
    const heavyStampOpponent = lobby({
      player_id: 'Steam|heavy_stamp_opp|0',
      name: 'HyperAltCarry99',
      team_num: 1,
      stats: { score: 610, goals: 3, assists: 1, saves: 2, shots: 7, demos: 3 },
      tags: ['smurf-duo', 'watch-air-dribble'],
      notes: 'Queues with lower ranked friend and flip resets from backboard',
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 17,
        division: 3,
        rank_name: 'Grand Champion II',
        mmr: 1645,
        matches_played: 18,
        sigma: 3.35,
        win_streak: 5,
      },
      ranks: {
        '11': {
          playlist_id: 11,
          playlist_name: 'Ranked Doubles',
          tier: 17,
          division: 3,
          rank_name: 'Grand Champion II',
          mmr: 1645,
          matches_played: 18,
          sigma: 3.35,
          win_streak: 5,
        },
        '13': {
          playlist_id: 13,
          playlist_name: 'Ranked Standard',
          tier: 22,
          division: 1,
          rank_name: 'Supersonic Legend',
          mmr: 1910,
          matches_played: 20,
          sigma: 2.5,
        },
      },
    });
    const lowMateOpponent = lobby({
      player_id: 'Steam|low_mate_opp|0',
      name: 'AnchorBuddy',
      team_num: 1,
      stats: { score: 180, goals: 0, assists: 1, saves: 1, shots: 1, demos: 0 },
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 15,
        division: 1,
        rank_name: 'Champion III',
        mmr: 1390,
        matches_played: 210,
        sigma: 2.5,
      },
      ranks: {
        '11': {
          playlist_id: 11,
          playlist_name: 'Ranked Doubles',
          tier: 15,
          division: 1,
          rank_name: 'Champion III',
          mmr: 1390,
          matches_played: 210,
          sigma: 2.5,
        },
        '13': {
          playlist_id: 13,
          playlist_name: 'Ranked Standard',
          tier: 16,
          division: 1,
          rank_name: 'Grand Champion I',
          mmr: 1450,
          matches_played: 120,
          sigma: 2.5,
        },
      },
    });

    const { container } = await renderLive(
      liveMatch({ opponents: [heavyStampOpponent, lowMateOpponent] }),
    );

    const rosterGrid = container.querySelector<HTMLElement>('.live-roster-grid');
    expect(rosterGrid).not.toBeNull();
    expect(rosterGrid?.style.getPropertyValue('--live-roster-rows')).toBe(
      'auto auto auto auto 0.875rem auto auto auto auto',
    );

    const blueCards = cards(container.querySelector('[data-testid="team-blue"]')!);
    const orangeCards = cards(container.querySelector('[data-testid="team-orange"]')!);
    expect(blueCards[0].style.getPropertyValue('--live-card-col')).toBe('1');
    expect(orangeCards[0].style.getPropertyValue('--live-card-col')).toBe('2');
    expect(blueCards[0].style.getPropertyValue('--live-card-row')).toBe('1 / span 4');
    expect(orangeCards[0].style.getPropertyValue('--live-card-row')).toBe('1 / span 4');
    expect(blueCards[1].style.getPropertyValue('--live-card-row')).toBe('6 / span 4');
    expect(orangeCards[1].style.getPropertyValue('--live-card-row')).toBe('6 / span 4');

    // Heavy opponent carries all 5 threat stamps + tags + note without losing H2H ledger
    expect(orangeCards[0].querySelector('[data-testid="badge-threat-smurf"]')).not.toBeNull();
    expect(orangeCards[0].querySelector('[data-testid="badge-threat-higher"]')).not.toBeNull();
    expect(orangeCards[0].querySelector('[data-testid="badge-threat-carry"]')).not.toBeNull();
    expect(orangeCards[0].querySelector('[data-testid="badge-threat-peak"]')).not.toBeNull();
    expect(orangeCards[0].querySelector('[data-testid="badge-threat-streak"]')).not.toBeNull();
    expect(orangeCards[0].querySelector('[data-testid="badge-player-note"]')).not.toBeNull();

    // Switch scope to 3V3 -> Scorebug AVG updates to 3v3 MMRs ((1460+1795)/2 = 1628 vs (1910+1450)/2 = 1680 -> +52 ORANGE)
    const btn3v3 = Array.from(container.querySelectorAll<HTMLButtonElement>('.live-pl-btn')).find(
      (b) => b.textContent?.trim() === '3V3',
    )!;
    await act(async () => {
      btn3v3.click();
    });
    const bug = container.querySelector('[data-testid="scorebug"]')!;
    expect(bug.textContent).toContain('AVG 1628');
    expect(bug.textContent).toContain('AVG 1680');
    expect(bug.textContent).toContain('+53 ORANGE');
  });

  it('hydrates live historical role split telemetry from /api/players/{id} for real live matches and re-scopes on playlist switch', async () => {
    const origFetch = globalThis.fetch;
    const requestedUrls: string[] = [];
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = String(input);
      requestedUrls.push(url);
      const parsed = new URL(url, 'http://localhost');
      const pl = parsed.searchParams.get('playlist');
      if (parsed.pathname === '/api/players/me') {
        return new Response(
          JSON.stringify({
            player_id: 'Epic|real_me|0',
            player_name: 'You',
            with_you_stats:
              pl === '11'
                ? {
                    games: 6,
                    wins: 6,
                    losses: 0,
                    avg_score: 562,
                    total_score: 3372,
                    avg_goals: 2.3,
                    total_goals: 14,
                    avg_assists: 1.1,
                    total_assists: 7,
                    avg_saves: 2.0,
                    total_saves: 12,
                    avg_shots: 5.0,
                    total_shots: 30,
                    shooting_pct: 46,
                    avg_demos_inflicted: 1.5,
                    total_demos_inflicted: 9,
                    avg_boost: 51,
                  }
                : { games: 0, wins: 0, losses: 0 },
            against_you_stats: { games: 0, wins: 0, losses: 0 },
            matchups: [],
            recent_matches: [],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      if (parsed.pathname.includes('real_opp')) {
        return new Response(
          JSON.stringify({
            player_id: 'Steam|real_opp|0',
            player_name: 'RivalAce',
            with_you_stats: { games: 0, wins: 0, losses: 0 },
            against_you_stats:
              pl === '11'
                ? {
                    games: 4,
                    wins: 1,
                    losses: 3,
                    avg_score: 604,
                    total_score: 2416,
                    avg_goals: 2.5,
                    total_goals: 10,
                    avg_assists: 0.8,
                    total_assists: 3,
                    avg_saves: 2.8,
                    total_saves: 11,
                    avg_shots: 5.5,
                    total_shots: 22,
                    shooting_pct: 45,
                    avg_demos_inflicted: 2.2,
                    total_demos_inflicted: 9,
                    avg_boost: 54,
                  }
                : { games: 0, wins: 0, losses: 0 },
            teammate_wins: 0,
            teammate_losses: 0,
            opponent_wins: pl === '11' ? 1 : 0,
            opponent_losses: pl === '11' ? 3 : 0,
            matchups: [],
            recent_matches: [],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      return new Response('{}', { status: 404 });
    }) as typeof fetch;

    try {
      const realMatch: CurrentMatchResponse = {
        active_match: true,
        match_ended: false,
        match_guid: 'real-live-guid-1',
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        local_team: 0,
        updated_at: new Date().toISOString(),
        local_player: lobby({ player_id: 'Epic|real_me|0', name: 'Strmshdw', team_num: 0, is_local: true }),
        teammates: [],
        opponents: [
          lobby({
            player_id: 'Steam|real_opp|0',
            name: 'RivalAce',
            team_num: 1,
            matchup_record: {
              player_id: 'Steam|real_opp|0',
              playlist_id: 11,
              wins_as_teammate: 0,
              losses_as_teammate: 0,
              wins_as_opponent: 1,
              losses_as_opponent: 3,
              total_matches: 4,
              last_played_at: '',
            },
          }),
        ],
        spectators: [],
      };

      const { container } = await renderLive(realMatch);
      await act(async () => {
        await new Promise((r) => setTimeout(r, 25));
      });

      expect(requestedUrls.some((u) => u.includes('/api/players/me?playlist=11'))).toBe(true);
      expect(container.textContent).toContain('562');
      expect(container.textContent).toContain('604');

      // Switch to 3V3 -> 2v2 telemetry (562 / 604) must clear and re-query playlist=13
      const btn3v3 = Array.from(container.querySelectorAll<HTMLButtonElement>('.live-pl-btn')).find(
        (b) => b.textContent?.trim() === '3V3',
      )!;
      await act(async () => {
        btn3v3.click();
        await new Promise((r) => setTimeout(r, 25));
      });
      expect(requestedUrls.some((u) => u.includes('playlist=13'))).toBe(true);
      expect(container.textContent).not.toContain('562');
      expect(container.textContent).not.toContain('604');
    } finally {
      globalThis.fetch = origFetch;
    }
  });
});

describe('LiveGameView — between matches', () => {
  beforeEach(() => localStorage.clear());
  afterEach(cleanup);

  it('shows the compact idle header (no giant WAITING FOR KICKOFF banner), last match strip, box score and session KPIs', async () => {
    const { container } = await renderLive(null);
    expect(container.querySelector('[data-testid="live-match"]')).toBeNull();
    expect(container.textContent).toContain('IDLE · QUEUE READY');
    expect(container.textContent).not.toContain('WAITING FOR KICKOFF');

    const last = container.querySelector('[data-testid="last-match"]')!;
    // Sample data lists newest first; the most recent match (4-2 win, +10) is the one shown.
    expect(last.textContent).toContain('LAST MATCH · 9 MIN AGO');
    expect(last.textContent).toContain('WIN');
    expect(last.textContent).toContain('BLUE · YOU');
    expect(last.textContent).toContain('RANKED DOUBLES · 5:54');
    expect(last.textContent).toContain('YOU + AEROPULSE vs VORTEXRL, ZENITH_99');
    expect(last.textContent).toContain('+10');

    const blue = container.querySelector('[data-testid="box-score-blue"]')!;
    expect(blue.textContent).toContain('BLUE · WIN');
    expect(blue.textContent).toContain('MVP');
    expect(blue.querySelector('.stat-lbl-score')?.textContent).toBe('PTS');
    expect(blue.querySelector('.stat-lbl-goals')?.textContent).toBe('G');
    expect(blue.querySelector('.stat-lbl-assists')?.textContent).toBe('A');
    expect(blue.querySelector('.stat-lbl-saves')?.textContent).toBe('SV');
    expect(blue.querySelector('.stat-lbl-shots')?.textContent).toBe('SH');
    expect(blue.querySelector('.stat-lbl-demos')?.textContent).toBe('D');
    expect(blue.querySelector('.stat-lbl-mmr')?.textContent).toBe('MMR');
    expect(blue.querySelector('.stat-lbl-rank')?.textContent).toBe('RANK · DIV');
    // Stat values in box-score rows also carry matching stat-lbl-* color classes
    expect(blue.querySelectorAll('.stat-lbl-score').length).toBeGreaterThan(1);
    expect(blue.querySelectorAll('.stat-lbl-goals').length).toBeGreaterThan(1);
    expect(blue.querySelectorAll('.stat-lbl-mmr').length).toBeGreaterThan(1);
    expect(blue.querySelectorAll('.stat-lbl-rank').length).toBeGreaterThan(1);
    expect(container.querySelector('[data-testid="box-score-orange"]')!.textContent).not.toContain('MVP');

    const kpis = container.querySelector('[data-testid="session-kpis"]')!;
    expect(kpis.textContent).toContain('3–1');
    expect(kpis.textContent).not.toContain('4–1');
    expect(kpis.textContent).toContain('+29');
    expect(kpis.textContent).toContain('W2');
  });

  it('scopes IdleStateView KPIs, For/Against split, PlaylistRows, and LastMatch strictly to activePlaylist', async () => {
    const { container } = await mount(
      <DashboardProvider>
        <LiveGameView
          match={null}
          session={SAMPLE_SESSION_RESPONSE}
          activePlaylist="13"
          connection="connected"
          columnsOpen={false}
          onCloseColumns={noop}
          onShowSession={noop}
        />
      </DashboardProvider>,
    );
    const kpis = container.querySelector('[data-testid="session-kpis"]')!;
    expect(kpis.textContent).toContain('1–0');
    expect(kpis.textContent).toContain('+11');
    // Playlist progression and LastMatch strictly show only 3v3 (RANKED STANDARD), never Ranked Doubles
    expect(container.textContent).toContain('RANKED STANDARD');
    expect(container.querySelector('[data-testid="last-match"]')?.textContent).toContain('RANKED STANDARD');
    expect(container.querySelector('[data-testid="last-match"]')?.textContent).not.toContain('RANKED DOUBLES');
  });

  it('reflects daemon connectivity in the status dots', async () => {
    const { container } = await mount(
      <LiveGameView match={null} session={null} connection="offline" columnsOpen={false} onCloseColumns={noop} onShowSession={noop} />,
    );
    const status = container.querySelector('[data-testid="daemon-status"]')!;
    expect(status.innerHTML).toContain('bg-loss');
    expect(status.textContent).toContain('RANK QUERIES OFFLINE');
    expect(container.textContent).toContain('NO MATCHES THIS SESSION YET');
  });

  it('renders the continue session banner on the LIVE tab even when match_ended or active_match is true', async () => {
    let continuedId = '';
    let dismissedId = '';
    const sessionWithCont: SessionResponse = {
      ...SAMPLE_SESSION_RESPONSE,
      session_id: 'new-rotated-epoch',
      total_matches: 0,
      total_wins: 0,
      total_losses: 0,
      matches: [],
      continuable_session: {
        ...SAMPLE_SESSION_RESPONSE,
        session_id: 'prev-session-id',
        total_matches: 5,
        total_wins: 4,
        total_losses: 1,
        can_continue: true,
      },
    };

    const { container } = await mount(
      <LiveGameView
        match={liveMatch({ active_match: false, match_ended: true, result: 'victory' })}
        session={sessionWithCont}
        connection="connected"
        columnsOpen={false}
        onCloseColumns={noop}
        onShowSession={noop}
        onContinueSession={(id) => {
          continuedId = id || '';
        }}
        onDismissContinueSession={(id) => {
          dismissedId = id || '';
        }}
      />,
    );

    const banner = container.querySelector('[data-testid="continue-session-banner"]')!;
    expect(banner).not.toBeNull();
    expect(banner.textContent).toContain('3W–1L · 4 MATCHES');

    const continueBtn = container.querySelector<HTMLButtonElement>('[data-testid="continue-session-btn"]')!;
    continueBtn.click();
    expect(continuedId).toBe('prev-session-id');

    const dismissBtn = container.querySelector<HTMLButtonElement>('[data-testid="dismiss-continue-btn"]')!;
    dismissBtn.click();
    expect(dismissedId).toBe('prev-session-id');
  });
});

