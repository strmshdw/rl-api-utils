import { describe, expect, it } from 'vitest';
import { emptyRoleStats, hasSyncedRoleTelemetry } from './roleStats';

describe('emptyRoleStats', () => {
  it('computes honest win_rate and zeros all unmeasured telemetry fields', () => {
    const stats = emptyRoleStats(10, 6, 4);
    expect(stats.games).toBe(10);
    expect(stats.wins).toBe(6);
    expect(stats.losses).toBe(4);
    expect(stats.win_rate).toBe(60);
    expect(stats.total_score).toBe(0);
    expect(stats.avg_score).toBe(0);
    expect(stats.avg_bpm).toBe(0);
    expect(stats.avg_boost).toBe(0);
    expect(stats.avg_dist_to_ball).toBe(0);
    expect(hasSyncedRoleTelemetry(stats)).toBe(false);
  });

  it('merges optional partial telemetry cleanly without fabricating unmeasured fields', () => {
    const stats = emptyRoleStats(4, 3, 1, { total_score: 1800, avg_score: 450, avg_bpm: 392 });
    expect(stats.win_rate).toBe(75);
    expect(stats.total_score).toBe(1800);
    expect(stats.avg_score).toBe(450);
    expect(stats.avg_bpm).toBe(392);
    expect(stats.avg_boost).toBe(0);
    expect(hasSyncedRoleTelemetry(stats)).toBe(true);
  });

  it('handles zero games safely without NaN', () => {
    const stats = emptyRoleStats();
    expect(stats.games).toBe(0);
    expect(stats.win_rate).toBe(0);
    expect(hasSyncedRoleTelemetry(null)).toBe(false);
    expect(hasSyncedRoleTelemetry(stats)).toBe(false);
  });
});
