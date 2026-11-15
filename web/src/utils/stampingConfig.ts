import type { StampingConfig } from '../contracts/generated_dto';

export type { StampingConfig };

export const DEFAULT_STAMPING_CONFIG: StampingConfig = {
  alt_detection_enabled: true,
  alt_min_tier: 12,
  alt_high_sigma: 3.35,
  alt_high_sigma_max_games: 35,
  alt_streak_sigma: 2.75,
  alt_streak_max_games: 45,
  alt_min_win_streak: 4,
  show_off_mode_peak: true,
  off_mode_min_games: 15,
  off_mode_self_tier_diff: 2.0,
  off_mode_vs_you_tier_diff: 1.5,
  carry_mmr_diff: 100,
  higher_mmr_diff: 60,
  show_hot_streak: true,
  hot_streak_min_wins: 4,
  show_teammate_off_mode_peak: false,
  relationship_min_games: 3,
};

export interface NumericStampingBounds {
  min: number;
  max: number;
  step: number;
}

export const STAMPING_BOUNDS = {
  alt_min_tier: { min: 1, max: 22, step: 1 },
  alt_high_sigma: { min: 2.5, max: 8.5, step: 0.05 },
  alt_high_sigma_max_games: { min: 5, max: 200, step: 1 },
  alt_streak_sigma: { min: 2.5, max: 8.5, step: 0.05 },
  alt_streak_max_games: { min: 5, max: 250, step: 1 },
  alt_min_win_streak: { min: 2, max: 20, step: 1 },
  off_mode_min_games: { min: 5, max: 200, step: 1 },
  off_mode_self_tier_diff: { min: 0.5, max: 10.0, step: 0.25 },
  off_mode_vs_you_tier_diff: { min: 0.5, max: 10.0, step: 0.25 },
  carry_mmr_diff: { min: 30, max: 500, step: 5 },
  higher_mmr_diff: { min: 20, max: 500, step: 5 },
  hot_streak_min_wins: { min: 2, max: 20, step: 1 },
  relationship_min_games: { min: 1, max: 25, step: 1 },
} as const satisfies Record<string, NumericStampingBounds>;

function clampInt(val: unknown, fallback: number, min: number, max: number): number {
  const n = typeof val === 'number' && Number.isFinite(val) ? Math.round(val) : fallback;
  return Math.max(min, Math.min(max, n));
}

function clampFloat(val: unknown, fallback: number, min: number, max: number): number {
  const n = typeof val === 'number' && Number.isFinite(val) ? val : fallback;
  const clamped = Math.max(min, Math.min(max, n));
  return Math.round(clamped * 100) / 100;
}

function normBool(val: unknown, fallback: boolean): boolean {
  return typeof val === 'boolean' ? val : fallback;
}

export function normalizeStampingConfig(partial?: Partial<StampingConfig> | null): StampingConfig {
  const src = partial ?? {};
  const d = DEFAULT_STAMPING_CONFIG;
  const b = STAMPING_BOUNDS;
  return {
    alt_detection_enabled: normBool(src.alt_detection_enabled, d.alt_detection_enabled),
    alt_min_tier: clampInt(src.alt_min_tier, d.alt_min_tier, b.alt_min_tier.min, b.alt_min_tier.max),
    alt_high_sigma: clampFloat(src.alt_high_sigma, d.alt_high_sigma, b.alt_high_sigma.min, b.alt_high_sigma.max),
    alt_high_sigma_max_games: clampInt(
      src.alt_high_sigma_max_games,
      d.alt_high_sigma_max_games,
      b.alt_high_sigma_max_games.min,
      b.alt_high_sigma_max_games.max,
    ),
    alt_streak_sigma: clampFloat(
      src.alt_streak_sigma,
      d.alt_streak_sigma,
      b.alt_streak_sigma.min,
      b.alt_streak_sigma.max,
    ),
    alt_streak_max_games: clampInt(
      src.alt_streak_max_games,
      d.alt_streak_max_games,
      b.alt_streak_max_games.min,
      b.alt_streak_max_games.max,
    ),
    alt_min_win_streak: clampInt(
      src.alt_min_win_streak,
      d.alt_min_win_streak,
      b.alt_min_win_streak.min,
      b.alt_min_win_streak.max,
    ),
    show_off_mode_peak: normBool(src.show_off_mode_peak, d.show_off_mode_peak),
    off_mode_min_games: clampInt(
      src.off_mode_min_games,
      d.off_mode_min_games,
      b.off_mode_min_games.min,
      b.off_mode_min_games.max,
    ),
    off_mode_self_tier_diff: clampFloat(
      src.off_mode_self_tier_diff,
      d.off_mode_self_tier_diff,
      b.off_mode_self_tier_diff.min,
      b.off_mode_self_tier_diff.max,
    ),
    off_mode_vs_you_tier_diff: clampFloat(
      src.off_mode_vs_you_tier_diff,
      d.off_mode_vs_you_tier_diff,
      b.off_mode_vs_you_tier_diff.min,
      b.off_mode_vs_you_tier_diff.max,
    ),
    carry_mmr_diff: clampInt(src.carry_mmr_diff, d.carry_mmr_diff, b.carry_mmr_diff.min, b.carry_mmr_diff.max),
    higher_mmr_diff: clampInt(
      src.higher_mmr_diff,
      d.higher_mmr_diff,
      b.higher_mmr_diff.min,
      b.higher_mmr_diff.max,
    ),
    show_hot_streak: normBool(src.show_hot_streak, d.show_hot_streak),
    hot_streak_min_wins: clampInt(
      src.hot_streak_min_wins,
      d.hot_streak_min_wins,
      b.hot_streak_min_wins.min,
      b.hot_streak_min_wins.max,
    ),
    show_teammate_off_mode_peak: normBool(
      src.show_teammate_off_mode_peak,
      d.show_teammate_off_mode_peak,
    ),
    relationship_min_games: clampInt(
      src.relationship_min_games,
      d.relationship_min_games,
      b.relationship_min_games.min,
      b.relationship_min_games.max,
    ),
  };
}

export function isStampingConfigPayload(val: unknown): val is Partial<StampingConfig> {
  if (!val || typeof val !== 'object') return false;
  const obj = val as Record<string, unknown>;
  return (
    typeof obj.carry_mmr_diff === 'number' &&
    typeof obj.higher_mmr_diff === 'number' &&
    typeof obj.relationship_min_games === 'number'
  );
}
