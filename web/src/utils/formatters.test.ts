import { describe, it, expect } from 'vitest';
import {
  formatMMR,
  formatMMRDelta,
  formatWinRate,
  formatDuration,
  formatRelativeTime,
  getRankTierColor,
} from './formatters';

describe('formatters', () => {
  it('formats MMR correctly', () => {
    expect(formatMMR(0)).toBe('---');
    expect(formatMMR(NaN)).toBe('---');
    expect(formatMMR(1234.56)).toBe('1234.6');
    expect(formatMMR(1000)).toBe('1000.0');
  });

  it('formats MMR Delta correctly', () => {
    expect(formatMMRDelta(0)).toBe('±0.0');
    expect(formatMMRDelta(NaN)).toBe('±0.0');
    expect(formatMMRDelta(18.5)).toBe('+18.5');
    expect(formatMMRDelta(-9.0)).toBe('-9.0');
  });

  it('formats Win Rate correctly', () => {
    expect(formatWinRate(0, 0)).toBe('0.0%');
    expect(formatWinRate(5, 5)).toBe('50.0%');
    expect(formatWinRate(7, 3)).toBe('70.0%');
  });

  it('formats duration in mm:ss', () => {
    expect(formatDuration(0)).toBe('0:00');
    expect(formatDuration(65)).toBe('1:05');
    expect(formatDuration(300)).toBe('5:00');
    expect(formatDuration(-10)).toBe('0:00');
  });

  it('formats relative time', () => {
    const now = new Date().toISOString();
    expect(formatRelativeTime(now)).toBe('just now');
    expect(formatRelativeTime('')).toBe('');
  });

  it('returns rank tier colors', () => {
    expect(getRankTierColor(1)).toContain('amber');
    expect(getRankTierColor(5)).toContain('slate');
    expect(getRankTierColor(8)).toContain('yellow');
    expect(getRankTierColor(11)).toContain('teal');
    expect(getRankTierColor(14)).toContain('sky');
    expect(getRankTierColor(17)).toContain('purple');
    expect(getRankTierColor(20)).toContain('rose');
    expect(getRankTierColor(22)).toContain('cyan');
    expect(getRankTierColor(0)).toContain('slate-400');
  });
});
