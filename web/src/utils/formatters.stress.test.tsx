// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import {
  formatMMR,
  formatMMRDelta,
  formatWinRate,
  formatDuration,
  formatRelativeTime,
  getRankTierColor,
} from './formatters';
import { RankBadge } from '../components/common/RankBadge';

describe('formatters adversarial stress testing', () => {
  describe('formatMMRDelta edge cases', () => {
    it('handles +0 and -0 consistently', () => {
      expect(formatMMRDelta(0)).toBe('±0.0');
      expect(formatMMRDelta(+0)).toBe('±0.0');
      expect(formatMMRDelta(-0)).toBe('±0.0');
    });

    it('handles NaN and undefined', () => {
      expect(formatMMRDelta(NaN)).toBe('±0.0');
      expect(formatMMRDelta(undefined as unknown as number)).toBe('±0.0');
    });

    it('handles null safely without throwing (returns ±0.0)', () => {
      expect(formatMMRDelta(null as unknown as number)).toBe('±0.0');
    });

    it('handles massive positive and negative values', () => {
      expect(formatMMRDelta(1000000)).toBe('+1000000.0');
      expect(formatMMRDelta(-1000000)).toBe('-1000000.0');
      expect(formatMMRDelta(9999999.9)).toBe('+9999999.9');
      expect(formatMMRDelta(-9999999.9)).toBe('-9999999.9');
    });

    it('handles tiny float values near zero', () => {
      // 0.0001 is > 0, so sign is '+', but .toFixed(1) yields '0.0'
      const posResult = formatMMRDelta(0.0001);
      expect(posResult).toBe('+0.0');

      const negResult = formatMMRDelta(-0.0001);
      expect(negResult).toBe('-0.0');
    });

    it('handles Infinity and -Infinity', () => {
      expect(formatMMRDelta(Infinity)).toBe('+Infinity');
      expect(formatMMRDelta(-Infinity)).toBe('-Infinity');
    });
  });

  describe('formatDuration edge cases', () => {
    it('handles zero and negative durations', () => {
      expect(formatDuration(0)).toBe('0:00');
      expect(formatDuration(-0)).toBe('0:00');
      expect(formatDuration(-1)).toBe('0:00');
      expect(formatDuration(-300)).toBe('0:00');
      expect(formatDuration(-Infinity)).toBe('0:00');
    });

    it('handles non-numeric or falsy inputs', () => {
      expect(formatDuration(NaN)).toBe('0:00');
      expect(formatDuration(undefined as unknown as number)).toBe('0:00');
      expect(formatDuration(null as unknown as number)).toBe('0:00');
    });

    it('handles extreme match durations > 24 hours', () => {
      // 24 hours = 86400 seconds
      expect(formatDuration(86400)).toBe('1440:00');
      // 25 hours = 90000 seconds
      expect(formatDuration(90000)).toBe('1500:00');
      // 100 hours = 360000 seconds
      expect(formatDuration(360000)).toBe('6000:00');
    });

    it('handles fractional seconds', () => {
      expect(formatDuration(65.4)).toBe('1:05');
      expect(formatDuration(65.9)).toBe('1:05');
      expect(formatDuration(0.9)).toBe('0:00');
    });

    it('exposes behavior on Infinity input', () => {
      // Infinity % 60 is NaN, resulting in Infinity:NaN
      expect(formatDuration(Infinity)).toBe('Infinity:NaN');
    });
  });

  describe('formatMMR edge cases', () => {
    it('formats normal, zero, and NaN MMR', () => {
      expect(formatMMR(0)).toBe('---');
      expect(formatMMR(NaN)).toBe('---');
      expect(formatMMR(undefined as unknown as number)).toBe('---');
      expect(formatMMR(1234.5)).toBe('1234.5');
    });

    it('handles null safely without throwing (returns ---)', () => {
      expect(formatMMR(null as unknown as number)).toBe('---');
    });

    it('formats negative MMR', () => {
      expect(formatMMR(-150.2)).toBe('-150.2');
    });
  });

  describe('formatWinRate edge cases', () => {
    it('handles zero total matches', () => {
      expect(formatWinRate(0, 0)).toBe('0.0%');
    });

    it('handles perfect and zero win rates', () => {
      expect(formatWinRate(10, 0)).toBe('100.0%');
      expect(formatWinRate(0, 10)).toBe('0.0%');
    });

    it('handles large match numbers', () => {
      expect(formatWinRate(500000, 500000)).toBe('50.0%');
    });

    it('handles division precision (1/3 win rate)', () => {
      expect(formatWinRate(1, 2)).toBe('33.3%');
    });
  });

  describe('formatRelativeTime edge cases', () => {
    it('handles empty and null date string', () => {
      expect(formatRelativeTime('')).toBe('');
      expect(formatRelativeTime(null as unknown as string)).toBe('');
    });

    it('handles invalid date strings by producing Invalid Date string', () => {
      expect(formatRelativeTime('not-a-valid-date')).toBe('Invalid Date');
    });

    it('handles timestamps in the future by displaying just now', () => {
      const future = new Date(Date.now() + 3600000).toISOString();
      expect(formatRelativeTime(future)).toBe('just now');
    });
  });

  describe('getRankTierColor fallback', () => {
    it('returns appropriate colors for valid tiers 1 to 22', () => {
      expect(getRankTierColor(1)).toContain('amber'); // Bronze
      expect(getRankTierColor(4)).toContain('slate'); // Silver
      expect(getRankTierColor(7)).toContain('yellow'); // Gold
      expect(getRankTierColor(10)).toContain('teal'); // Plat
      expect(getRankTierColor(13)).toContain('sky'); // Diamond
      expect(getRankTierColor(16)).toContain('purple'); // Champ
      expect(getRankTierColor(19)).toContain('rose'); // GC
      expect(getRankTierColor(22)).toContain('cyan'); // SSL
    });

    it('falls back to slate neutral for unknown / out-of-range tiers', () => {
      expect(getRankTierColor(0)).toBe('text-slate-400 border-slate-700 bg-slate-900/30');
      expect(getRankTierColor(-5)).toBe('text-slate-400 border-slate-700 bg-slate-900/30');
      expect(getRankTierColor(23)).toBe('text-slate-400 border-slate-700 bg-slate-900/30');
      expect(getRankTierColor(99)).toBe('text-slate-400 border-slate-700 bg-slate-900/30');
      expect(getRankTierColor(NaN)).toBe('text-slate-400 border-slate-700 bg-slate-900/30');
    });
  });

  describe('RankBadge fallback for unknown tiers & names', () => {
    it('renders fallback name Unranked when rankName is empty or missing', () => {
      const html1 = renderToString(<RankBadge rankName="" tier={0} />);
      expect(html1).toContain('Unranked');

      const html2 = renderToString(<RankBadge rankName={undefined as unknown as string} tier={0} />);
      expect(html2).toContain('Unranked');
    });

    it('renders unranked styling for tier <= 0', () => {
      const html = renderToString(<RankBadge rankName="Unranked" tier={0} />);
      expect(html).toContain('text-slate-400');
      expect(html).toContain('border-slate-700');
    });

    it('falls back to unranked neutral slate styling for unknown tiers (>22) and non-numeric tiers, requiring tier 22 for SSL', () => {
      const html99 = renderToString(<RankBadge rankName="Unknown Tier" tier={99} />);
      expect(html99).not.toContain('animate-pulse');
      expect(html99).not.toContain('from-fuchsia-950/50');
      expect(html99).toContain('text-slate-400');
      expect(html99).toContain('border-slate-700');

      const htmlNaN = renderToString(<RankBadge rankName="Corrupted Tier" tier={NaN} />);
      expect(htmlNaN).not.toContain('animate-pulse');
      expect(htmlNaN).toContain('text-slate-400');

      const htmlUndef = renderToString(<RankBadge rankName="Missing Tier" tier={undefined as unknown as number} />);
      expect(htmlUndef).not.toContain('animate-pulse');
      expect(htmlUndef).toContain('text-slate-400');

      const htmlSSL = renderToString(<RankBadge rankName="Supersonic Legend" tier={22} />);
      expect(htmlSSL).toContain('animate-pulse');
      expect(htmlSSL).toContain('text-fuchsia-300');
      expect(htmlSSL).toContain('from-fuchsia-950/50');
    });
  });
});
