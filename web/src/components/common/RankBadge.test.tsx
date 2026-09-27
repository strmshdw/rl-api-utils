// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { RankBadge } from './RankBadge';

describe('RankBadge Component', () => {
  it('renders custom rank name when provided', () => {
    const html = renderToString(<RankBadge rankName="Champion II" tier={17} division={3} />);
    expect(html).toContain('Champion II');
  });

  it('renders fallback name Unranked when rankName is empty, null, or undefined', () => {
    expect(renderToString(<RankBadge rankName="" tier={0} />)).toContain('Unranked');
    expect(renderToString(<RankBadge rankName={null as unknown as string} tier={0} />)).toContain('Unranked');
    expect(renderToString(<RankBadge rankName={undefined as unknown as string} tier={0} />)).toContain('Unranked');
  });

  it('renders unranked neutral slate styling for tier 0 and negative tiers', () => {
    const html0 = renderToString(<RankBadge rankName="Unranked" tier={0} />);
    expect(html0).toContain('text-slate-400');
    expect(html0).toContain('bg-slate-800/60');
    expect(html0).toContain('border-slate-700');
    expect(html0).not.toContain('animate-pulse');

    const htmlNeg = renderToString(<RankBadge rankName="Negative" tier={-5} />);
    expect(htmlNeg).toContain('text-slate-400');
    expect(htmlNeg).not.toContain('animate-pulse');
  });

  it('renders correct theme classes across standard competitive tiers 1 to 21', () => {
    expect(renderToString(<RankBadge rankName="Bronze I" tier={1} />)).toContain('text-amber-500');
    expect(renderToString(<RankBadge rankName="Silver II" tier={5} />)).toContain('text-slate-200');
    expect(renderToString(<RankBadge rankName="Gold III" tier={9} />)).toContain('text-yellow-400');
    expect(renderToString(<RankBadge rankName="Platinum I" tier={10} />)).toContain('text-cyan-400');
    expect(renderToString(<RankBadge rankName="Diamond II" tier={14} />)).toContain('text-sky-400');
    expect(renderToString(<RankBadge rankName="Champion III" tier={18} />)).toContain('text-purple-400');
    expect(renderToString(<RankBadge rankName="Grand Champion I" tier={19} />)).toContain('text-rose-400');
  });

  it('strictly requires tier === 22 for Supersonic Legend pulsating theme', () => {
    const ssl = renderToString(<RankBadge rankName="Supersonic Legend" tier={22} />);
    expect(ssl).toContain('text-fuchsia-300');
    expect(ssl).toContain('animate-pulse');
    expect(ssl).toContain('from-fuchsia-950/50');
  });

  it('falls back to unranked neutral slate styling for unknown tiers (> 22, NaN, undefined, null)', () => {
    // Tier 23
    const html23 = renderToString(<RankBadge rankName="Tier 23" tier={23} />);
    expect(html23).toContain('text-slate-400');
    expect(html23).toContain('border-slate-700');
    expect(html23).not.toContain('animate-pulse');

    // Tier 99
    const html99 = renderToString(<RankBadge rankName="Tier 99" tier={99} />);
    expect(html99).toContain('text-slate-400');
    expect(html99).not.toContain('animate-pulse');

    // NaN
    const htmlNaN = renderToString(<RankBadge rankName="NaN Tier" tier={NaN} />);
    expect(htmlNaN).toContain('text-slate-400');
    expect(htmlNaN).not.toContain('animate-pulse');

    // undefined
    const htmlUndef = renderToString(<RankBadge rankName="Undef Tier" tier={undefined} />);
    expect(htmlUndef).toContain('text-slate-400');
    expect(htmlUndef).not.toContain('animate-pulse');

    // null
    const htmlNull = renderToString(<RankBadge rankName="Null Tier" tier={null} />);
    expect(htmlNull).toContain('text-slate-400');
    expect(htmlNull).not.toContain('animate-pulse');
  });
});
