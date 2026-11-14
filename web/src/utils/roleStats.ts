import type { PlayerRoleStatsSummary } from '../types/api';

/**
 * Builds an honest zero-initialized PlayerRoleStatsSummary with derived W/L and win_rate.
 * Never fabricates fake Ballchasing averages — unmeasured telemetry metrics stay at 0.
 */
export function emptyRoleStats(
  games = 0,
  wins = 0,
  losses = 0,
  partial?: Partial<PlayerRoleStatsSummary> | null,
): PlayerRoleStatsSummary {
  const g = Math.max(0, Math.round(games || 0));
  const w = Math.max(0, Math.round(wins || 0));
  const l = Math.max(0, Math.round(losses || 0));

  const base: PlayerRoleStatsSummary = {
    games: g,
    wins: w,
    losses: l,
    win_rate: g > 0 ? (w / g) * 100 : 0,
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

  if (!partial) return base;

  for (const [key, value] of Object.entries(partial) as Array<
    [keyof PlayerRoleStatsSummary, number | undefined]
  >) {
    if (value !== undefined) {
      base[key] = value;
    }
  }
  return base;
}

/**
 * Returns true only when real synced Ballchasing telemetry metrics exist on the summary.
 */
export function hasSyncedRoleTelemetry(stats?: PlayerRoleStatsSummary | null): boolean {
  return Boolean(
    stats &&
      ((stats.total_score || 0) > 0 ||
        (stats.avg_score || 0) > 0 ||
        (stats.total_shots || 0) > 0 ||
        (stats.avg_shots || 0) > 0 ||
        (stats.avg_bpm || 0) > 0 ||
        (stats.avg_boost || 0) > 0),
  );
}
