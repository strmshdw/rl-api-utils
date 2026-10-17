// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act } from 'react';
import { HistoricalMatchDetailModal } from './HistoricalMatchDetailModal';
import { HistoricalMatchRecord } from '../../types/api';
import { cleanup, clickText, mount } from '../../test/dom';

const SAMPLE_HISTORICAL_MATCH: HistoricalMatchRecord = {
  match_guid: 'test-guid-1234',
  record_start_timestamp: 1700000000,
  map_name: 'DFH Stadium',
  playlist: 11,
  replay_url: '',
  download_status: 'COMPLETED',
  local_file_path: '/path/to/replay.replay',
  upload_status: 'COMPLETED',
  ballchasing_id: 'bc-1234',
  ballchasing_url: 'https://ballchasing.com/replay/bc-1234',
  retry_count: 0,
  last_error: '',
  blue_score: 3,
  orange_score: 2,
  winner_team: 0,
  replay_stats_json: JSON.stringify({
    match_guid: 'test-guid-1234',
    map_name: 'DFH Stadium',
    playlist: 11,
    blue_score: 3,
    orange_score: 2,
    duration: 300,
    players: [
      {
        name: 'PlayerOne',
        platform: 'Steam',
        online_id: 'steam_p1',
        team: 0,
        score: 450,
        goals: 2,
        assists: 1,
        saves: 2,
        shots: 4,
        demos: 1,
        boost: { bpm: 550, avg_amount: 45.2, amount_collected: 2400 },
        movement: { avg_speed: 1450, percent_supersonic_speed: 18.5 },
        positioning: { percent_defensive_third: 42.1, percent_neutral_third: 35.0, percent_offensive_third: 22.9 },
        demo: { inflicted: 1, taken: 0 },
      },
      {
        name: 'PlayerTwo',
        platform: 'Epic',
        online_id: 'epic_p2',
        team: 1,
        score: 320,
        goals: 1,
        assists: 0,
        saves: 1,
        shots: 3,
        demos: 0,
        boost: { bpm: 480, avg_amount: 38.0, amount_collected: 1900 },
        movement: { avg_speed: 1380, percent_supersonic_speed: 12.0 },
        positioning: { percent_defensive_third: 50.0, percent_neutral_third: 30.0, percent_offensive_third: 20.0 },
        demo: { inflicted: 0, taken: 1 },
      },
    ],
  }),
  created_at: '2026-10-16T12:00:00Z',
  updated_at: '2026-10-16T12:05:00Z',
  stats: {
    match_guid: 'test-guid-1234',
    map_name: 'DFH Stadium',
    playlist: 11,
    blue_score: 3,
    orange_score: 2,
    winner_team: 0,
    duration: 300,
    overtime: false,
    date: '2026-10-16T12:00:00Z',
    players: [
      {
        name: 'PlayerOne',
        platform: 'Steam',
        online_id: 'steam_p1',
        team: 0,
        score: 450,
        goals: 2,
        assists: 1,
        saves: 2,
        shots: 4,
        demos: 1,
        boost: { bpm: 550, avg_amount: 45.2, amount_collected: 2400 },
        movement: { avg_speed: 1450, percent_supersonic_speed: 18.5 },
        positioning: { percent_defensive_third: 42.1, percent_neutral_third: 35.0, percent_offensive_third: 22.9 },
        demo: { inflicted: 1, taken: 0 },
      },
      {
        name: 'PlayerTwo',
        platform: 'Epic',
        online_id: 'epic_p2',
        team: 1,
        score: 320,
        goals: 1,
        assists: 0,
        saves: 1,
        shots: 3,
        demos: 0,
        boost: { bpm: 480, avg_amount: 38.0, amount_collected: 1900 },
        movement: { avg_speed: 1380, percent_supersonic_speed: 12.0 },
        positioning: { percent_defensive_third: 50.0, percent_neutral_third: 30.0, percent_offensive_third: 20.0 },
        demo: { inflicted: 0, taken: 1 },
      },
    ],
  },
};

describe('HistoricalMatchDetailModal', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders overview tab with score, map, and playlist information', async () => {
    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={SAMPLE_HISTORICAL_MATCH}
        onClose={vi.fn()}
        localPlayerIds={new Set(['steam_p1'])}
      />
    );

    expect(container.textContent).toContain('3');
    expect(container.textContent).toContain('2');
    expect(container.textContent).toContain('DFH Stadium');
    expect(container.textContent).toContain('RANKED DOUBLES');
    expect(container.textContent).toContain('You');
    expect(container.textContent).toContain('PlayerTwo');
  });

  it('switches between telemetry tabs', async () => {
    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={SAMPLE_HISTORICAL_MATCH}
        onClose={vi.fn()}
        localPlayerIds={new Set(['steam_p1'])}
      />
    );

    // Click BOOST & MOVEMENT tab
    await clickText(container, 'BOOST & MOVEMENT');
    expect(container.textContent).toContain('BOOST TELEMETRY');
    expect(container.textContent).toContain('AVG BOOST');

    // Click POSITIONING tab
    await clickText(container, 'POSITIONING');
    expect(container.textContent).toContain('FIELD THIRDS & SPACING');

    // Click DEMOLITIONS & COMBAT tab
    await clickText(container, 'DEMOLITIONS & COMBAT');
    expect(container.textContent).toContain('DEMOLITIONS & COMBAT');

    // Click RAW JSON tab
    await clickText(container, 'RAW JSON');
    expect(container.textContent).toContain('RAW TELEMETRY JSON');
    expect(container.textContent).toContain('COPY JSON');
  });

  it('invokes onPlayerClick when a player row is clicked', async () => {
    const onPlayerClick = vi.fn();
    const { container } = await mount(
      <HistoricalMatchDetailModal
        match={SAMPLE_HISTORICAL_MATCH}
        onClose={vi.fn()}
        onPlayerClick={onPlayerClick}
      />
    );

    const playerBtn = Array.from(container.querySelectorAll('button')).find((el) =>
      el.textContent?.includes('PlayerTwo')
    );
    expect(playerBtn).toBeTruthy();

    await act(async () => {
      playerBtn?.click();
    });

    expect(onPlayerClick).toHaveBeenCalledWith('epic_p2', 'PlayerTwo');
  });
});
