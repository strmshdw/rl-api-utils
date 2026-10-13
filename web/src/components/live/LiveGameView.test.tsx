// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
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

const noop = () => {};

async function renderLive(match: CurrentMatchResponse | null, session: SessionResponse | null = SAMPLE_SESSION_RESPONSE) {
  return mount(
    <LiveGameView match={match} session={session} connection="connected" columnsOpen={false} onCloseColumns={noop} onShowSession={noop} />,
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

  it('puts the local player on a "You" card with the session record', async () => {
    const { container } = await renderLive(liveMatch());
    const you = cards(container).find((c) => c.dataset.playerId === 'Epic|demo_local_player|0')!;
    expect(you.className).toContain('border-blue-ink');
    expect(you.textContent).toContain('You');
    expect(you.querySelector('[data-testid="player-h2h"]')?.textContent).toBe('THIS SESSION4–1');
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

  it('colours stat values: lobby leader white, other non-zero ink-3, zero faint', async () => {
    expect(statInk(0, 0)).toBe('text-faint');
    expect(statInk(5, 6)).toBe('text-ink-3');
    expect(statInk(6, 6)).toBe('text-ink-strong');

    const { container } = await renderLive(liveMatch());
    const byId = (id: string) => cards(container).find((c) => c.dataset.playerId === id)!;
    const you = byId('Epic|demo_local_player|0');
    expect(statEl(you, 'score').className).toContain('text-ink-strong'); // 480 is the lobby high
    expect(statEl(you, 'goals').className).toContain('text-ink-strong'); // 2 goals
    expect(statEl(byId('Steam|76561198000000020|0'), 'demos').className).toContain('text-faint'); // 0 demos
    expect(statEl(byId('Steam|76561198000000020|0'), 'score').className).toContain('text-ink-3');
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
});

describe('LiveGameView — between matches', () => {
  afterEach(cleanup);

  it('shows the idle header, last match strip, box score and session KPIs', async () => {
    const { container } = await renderLive(null);
    expect(container.querySelector('[data-testid="live-match"]')).toBeNull();
    expect(container.textContent).toContain('WAITING FOR KICKOFF');

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
    expect(container.querySelector('[data-testid="box-score-orange"]')!.textContent).not.toContain('MVP');

    const kpis = container.querySelector('[data-testid="session-kpis"]')!;
    expect(kpis.textContent).toContain('4–1');
    expect(kpis.textContent).toContain('+31');
    expect(kpis.textContent).toContain('W2');
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
});
