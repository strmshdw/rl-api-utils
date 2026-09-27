// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { RosterTable } from './RosterTable';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';

describe('RosterTable H2H Orientation', () => {
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

  const orangePlayer: LobbyPlayer = {
    player_id: 'Epic|orange_p1|0',
    name: 'OrangePlayer',
    platform: 'Epic',
    team_num: 1,
    is_local: false,
    is_bot: false,
    stats: { score: 500, goals: 2, assists: 1, saves: 1, shots: 3, demos: 0 },
    matchup_record: {
      player_id: 'Epic|orange_p1|0',
      playlist_id: 11,
      wins_as_teammate: 9,
      losses_as_teammate: 1,
      wins_as_opponent: 2,
      losses_as_opponent: 8,
      total_matches: 20,
      last_played_at: '2026-09-26T00:00:00Z',
    },
  };

  const bluePlayer: LobbyPlayer = {
    player_id: 'Steam|blue_p1|0',
    name: 'BluePlayer',
    platform: 'Steam',
    team_num: 0,
    is_local: false,
    is_bot: false,
    stats: { score: 300, goals: 1, assists: 0, saves: 2, shots: 2, demos: 0 },
    matchup_record: {
      player_id: 'Steam|blue_p1|0',
      playlist_id: 11,
      wins_as_teammate: 7,
      losses_as_teammate: 3,
      wins_as_opponent: 4,
      losses_as_opponent: 6,
      total_matches: 20,
      last_played_at: '2026-09-26T00:00:00Z',
    },
  };

  const clean = (s: string) => s.replace(/<!--.*?-->/g, '');

  it('classifies players on Orange team as teammates when localTeamNum is 1', () => {
    // When local player is on Orange (team 1), team 1 players should show teammate record (9W-1L), not opponent (2W-8L)
    const html = clean(renderToString(
      <RosterTable
        teamNum={1}
        teamName="Orange Team"
        players={[orangePlayer]}
        localPlayerId="Epic|me|0"
        localTeamNum={1}
        columnConfig={fullConfig}
        activePlaylistId={11}
      />
    ));

    expect(html).toContain('<span>9W-1L</span>');
    expect(html).toContain('(90%)');
    expect(html).not.toContain('<span>2W-8L</span>');
    expect(html).not.toContain('(20%)');
  });

  it('classifies players on Blue team as opponents when localTeamNum is 1', () => {
    // When local player is on Orange (team 1), team 0 players should show opponent record (4W-6L), not teammate (7W-3L)
    const html = clean(renderToString(
      <RosterTable
        teamNum={0}
        teamName="Blue Team"
        players={[bluePlayer]}
        localPlayerId="Epic|me|0"
        localTeamNum={1}
        columnConfig={fullConfig}
        activePlaylistId={11}
      />
    ));

    expect(html).toContain('<span>4W-6L</span>');
    expect(html).toContain('(40%)');
    expect(html).not.toContain('<span>7W-3L</span>');
    expect(html).not.toContain('(70%)');
  });

  it('classifies players on Blue team as teammates when localTeamNum is 0', () => {
    const html = clean(renderToString(
      <RosterTable
        teamNum={0}
        teamName="Blue Team"
        players={[bluePlayer]}
        localPlayerId="Steam|me|0"
        localTeamNum={0}
        columnConfig={fullConfig}
        activePlaylistId={11}
      />
    ));

    expect(html).toContain('<span>7W-3L</span>');
    expect(html).toContain('(70%)');
    expect(html).not.toContain('<span>4W-6L</span>');
    expect(html).not.toContain('(40%)');
  });
});
