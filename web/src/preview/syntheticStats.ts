import type { PlayerRoleStatsSummary, ReplayPlayerStats } from '../types/api';

export const PREVIEW_FIXTURE_SENTINEL = '__RL_SYNC_DEV_PREVIEW_FIXTURE__';

export function isoAgo(ms: number): string {
  return new Date(Date.now() - ms).toISOString();
}

export function unixAgo(seconds: number): number {
  return Math.floor(Date.now() / 1000) - seconds;
}

export function serializeRanks(ranks: Record<string, unknown>): string {
  return JSON.stringify(ranks);
}

/**
 * Preview-only role telemetry generator for synthetic fixtures.
 * Never imported by production code paths.
 */
export function synthesizeRoleStats(
  arg1: number,
  arg2: number,
  arg3?: number | string,
  partial?: Partial<PlayerRoleStatsSummary> | null,
  seedPts = 420,
): PlayerRoleStatsSummary & {
  score: number;
  goals: number;
  assists: number;
  saves: number;
  demos: number;
  shot_pct: number;
  avg_demos: number;
} {
  let g: number;
  let w: number;
  let l: number;
  if (typeof arg3 === 'string' || arg3 === undefined) {
    w = Math.max(0, Math.round(arg1 || 0));
    l = Math.max(0, Math.round(arg2 || 0));
    g = w + l;
  } else {
    g = Math.max(0, Math.round(arg1 || 0));
    w = Math.max(0, Math.round(arg2 || 0));
    l = Math.max(0, Math.round(arg3 || 0));
  }

  if (g === 0) {
    return {
      games: 0,
      wins: 0,
      losses: 0,
      win_rate: 0,
      total_score: 0,
      total_goals: 0,
      total_assists: 0,
      total_saves: 0,
      total_shots: 0,
      total_demos_inflicted: 0,
      total_demos_taken: 0,
      avg_score: 0,
      avg_goals: 0,
      avg_assists: 0,
      avg_saves: 0,
      avg_shots: 0,
      shooting_pct: 0,
      avg_demos_inflicted: 0,
      avg_demos_taken: 0,
      avg_bpm: 0,
      avg_bcpm: 0,
      avg_boost: 0,
      total_boost_stolen: 0,
      total_big_pads: 0,
      total_small_pads: 0,
      avg_supersonic_pct: 0,
      avg_boost_speed_pct: 0,
      avg_slow_speed_pct: 0,
      avg_ground_pct: 0,
      avg_low_air_pct: 0,
      avg_high_air_pct: 0,
      avg_powerslide_count: 0,
      avg_powerslide_duration: 0,
      avg_def_third_pct: 0,
      avg_neutral_third_pct: 0,
      avg_off_third_pct: 0,
      avg_behind_ball_pct: 0,
      avg_first_man_pct: 0,
      avg_third_man_pct: 0,
      avg_dist_to_ball: 0,
      score: 0,
      goals: 0,
      assists: 0,
      saves: 0,
      demos: 0,
      shot_pct: 0,
      avg_demos: 0,
    };
  }

  const avgScore = partial?.avg_score ?? seedPts;
  const avgGoals = partial?.avg_goals ?? 1.4;
  const avgAssists = partial?.avg_assists ?? 0.9;
  const avgSaves = partial?.avg_saves ?? 1.6;
  const avgShots = partial?.avg_shots ?? 3.8;
  const shPct = partial?.shooting_pct ?? (avgShots > 0 ? Math.round((avgGoals / avgShots) * 100) : 37);
  const avgDemos = partial?.avg_demos_inflicted ?? 1.2;
  const avgDemosTaken = partial?.avg_demos_taken ?? 1.0;
  const avgBpm = partial?.avg_bpm ?? 415;
  const avgBcpm = partial?.avg_bcpm ?? 430;
  const avgBoost = partial?.avg_boost ?? 46;
  const avgSup = partial?.avg_supersonic_pct ?? 18.2;

  const totalScore = partial?.total_score ?? Math.round(avgScore * g);
  const totalGoals = partial?.total_goals ?? Math.round(avgGoals * g);
  const totalAssists = partial?.total_assists ?? Math.round(avgAssists * g);
  const totalSaves = partial?.total_saves ?? Math.round(avgSaves * g);
  const totalShots = partial?.total_shots ?? Math.round(avgShots * g);
  const totalDemos = partial?.total_demos_inflicted ?? Math.round(avgDemos * g);

  return {
    games: g,
    wins: w,
    losses: l,
    win_rate: g > 0 ? (w / g) * 100 : 0,
    total_score: totalScore,
    total_goals: totalGoals,
    total_assists: totalAssists,
    total_saves: totalSaves,
    total_shots: totalShots,
    total_demos_inflicted: totalDemos,
    total_demos_taken: partial?.total_demos_taken ?? Math.round(avgDemosTaken * g),
    avg_score: avgScore,
    avg_goals: avgGoals,
    avg_assists: avgAssists,
    avg_saves: avgSaves,
    avg_shots: avgShots,
    shooting_pct: shPct,
    avg_demos_inflicted: avgDemos,
    avg_demos_taken: avgDemosTaken,
    avg_bpm: avgBpm,
    avg_bcpm: avgBcpm,
    avg_boost: avgBoost,
    total_boost_stolen: partial?.total_boost_stolen ?? Math.round(520 * g),
    total_big_pads: partial?.total_big_pads ?? Math.round(21 * g),
    total_small_pads: partial?.total_small_pads ?? Math.round(68 * g),
    avg_supersonic_pct: avgSup,
    avg_boost_speed_pct: partial?.avg_boost_speed_pct ?? 39.0,
    avg_slow_speed_pct: partial?.avg_slow_speed_pct ?? 42.8,
    avg_ground_pct: partial?.avg_ground_pct ?? 56.0,
    avg_low_air_pct: partial?.avg_low_air_pct ?? 34.0,
    avg_high_air_pct: partial?.avg_high_air_pct ?? 10.0,
    avg_powerslide_count: partial?.avg_powerslide_count ?? 48,
    avg_powerslide_duration: partial?.avg_powerslide_duration ?? 7.2,
    avg_def_third_pct: partial?.avg_def_third_pct ?? 42,
    avg_neutral_third_pct: partial?.avg_neutral_third_pct ?? 34,
    avg_off_third_pct: partial?.avg_off_third_pct ?? 24,
    avg_behind_ball_pct: partial?.avg_behind_ball_pct ?? 68,
    avg_first_man_pct: partial?.avg_first_man_pct ?? 36,
    avg_third_man_pct: partial?.avg_third_man_pct ?? 32,
    avg_dist_to_ball: partial?.avg_dist_to_ball ?? 2480,
    score: totalScore,
    goals: totalGoals,
    assists: totalAssists,
    saves: totalSaves,
    demos: totalDemos,
    shot_pct: shPct,
    avg_demos: avgDemos,
  };
}

export function enrichTelemetryPlayer(p: ReplayPlayerStats): ReplayPlayerStats {
  const seed = p.score || 350;
  const g = p.goals || 0;
  const sh = p.shots || 1;
  const d = p.demos || 0;
  return {
    ...p,
    mmr: p.mmr ?? (p.team === 0 ? 1495 : 1505),
    rank_name: p.rank_name ?? 'Grand Champion I',
    short_rank: p.short_rank ?? 'GC I · DIV II',
    core: p.core ?? {
      score: p.score,
      goals: p.goals,
      assists: p.assists,
      saves: p.saves,
      shots: p.shots,
      shooting_percentage: sh > 0 ? Math.round((g / sh) * 100) : 0,
    },
    boost: p.boost ?? {
      bpm: Math.round(370 + (seed % 110)),
      bcpm: Math.round(385 + (seed % 115)),
      avg_amount: Math.round(41 + (seed % 14)),
      amount_collected: Math.round(2150 + seed * 1.4),
      amount_stolen: Math.round(420 + (seed % 260)),
      count_collected_big: Math.round(18 + (seed % 11)),
      count_collected_small: Math.round(58 + (seed % 28)),
      amount_overfill: Math.round(280 + (seed % 160)),
      percent_zero_boost: Number((7.2 + (seed % 6)).toFixed(1)),
      percent_full_boost: Number((10.5 + (seed % 5)).toFixed(1)),
    },
    movement: p.movement ?? {
      avg_speed: Math.round(1465 + (seed % 120)),
      percent_supersonic_speed: Number((16.5 + (seed % 8)).toFixed(1)),
      percent_boost_speed: Number((38.2 + (seed % 6)).toFixed(1)),
      percent_slow_speed: Number((45.3 - (seed % 8)).toFixed(1)),
      percent_ground: Number((54.0 + (seed % 7)).toFixed(1)),
      percent_low_air: Number((34.5 - (seed % 5)).toFixed(1)),
      percent_high_air: Number((11.5 - (seed % 2)).toFixed(1)),
      count_powerslide: Math.round(42 + (seed % 28)),
      time_powerslide: Number((6.4 + (seed % 4) * 0.7).toFixed(1)),
    },
    positioning: p.positioning ?? {
      percent_defensive_third: Number((39 + (seed % 10)).toFixed(0)),
      percent_neutral_third: 35,
      percent_offensive_third: Number((26 - (seed % 10)).toFixed(0)),
      percent_behind_ball: Number((66 + (seed % 11)).toFixed(0)),
      percent_infront_ball: Number((34 - (seed % 11)).toFixed(0)),
      percent_most_forward: Number((34 + (seed % 9)).toFixed(0)),
      percent_most_back: Number((33 + (seed % 8)).toFixed(0)),
      avg_distance_to_ball: Math.round(2380 + (seed % 290)),
    },
    demo: p.demo ?? {
      inflicted: d,
      taken: p.team === 0 ? 1 : 2,
    },
  };
}
