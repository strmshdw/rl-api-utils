import { describe, it, expect } from 'vitest';
import { STAT_COLUMNS, PRESETS } from './columns';

describe('columns taxonomy & presets', () => {
  it('defines exactly 10 stat columns', () => {
    expect(STAT_COLUMNS).toHaveLength(10);
    const keys = STAT_COLUMNS.map((c) => c.key);
    expect(keys).toEqual([
      'score',
      'goals',
      'assists',
      'saves',
      'shots',
      'demos',
      'mmr',
      'rank',
      'h2h',
      'platform',
    ]);
  });

  it('defines 3 standard presets', () => {
    expect(Object.keys(PRESETS)).toEqual(['full', 'competitive', 'streamer']);
  });

  it('full preset contains all 10 columns', () => {
    expect(PRESETS.full.columns).toHaveLength(10);
    for (const col of STAT_COLUMNS) {
      expect(PRESETS.full.columns).toContain(col.key);
    }
  });

  it('competitive preset focuses on core competitive stats', () => {
    expect(PRESETS.competitive.columns).toEqual([
      'score',
      'goals',
      'mmr',
      'rank',
      'h2h',
    ]);
  });

  it('streamer preset focuses on compact privacy box scores', () => {
    expect(PRESETS.streamer.columns).toEqual([
      'score',
      'goals',
      'assists',
      'saves',
    ]);
    expect(PRESETS.streamer.columns).not.toContain('mmr');
    expect(PRESETS.streamer.columns).not.toContain('rank');
    expect(PRESETS.streamer.columns).not.toContain('h2h');
  });
});
