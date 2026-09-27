import { describe, it, expect } from 'vitest';
import { parsePlatform } from './platforms';

describe('platforms', () => {
  it('identifies Steam platform', () => {
    const res = parsePlatform('Steam|76561198000000000|0');
    expect(res.code).toBe('steam');
    expect(res.name).toBe('Steam');
  });

  it('identifies Epic platform', () => {
    const res = parsePlatform('Epic|1234567890abcdef|0');
    expect(res.code).toBe('epic');
    expect(res.name).toBe('Epic Games');
  });

  it('identifies PlayStation platform', () => {
    const res = parsePlatform('PlayStation|123|0');
    expect(res.code).toBe('psn');
    expect(res.name).toBe('PlayStation');
  });

  it('identifies Xbox platform', () => {
    const res = parsePlatform('Xbox|123|0');
    expect(res.code).toBe('xbox');
    expect(res.name).toBe('Xbox');
  });

  it('identifies AI Bot', () => {
    const res1 = parsePlatform('bot');
    expect(res1.code).toBe('bot');
    expect(res1.name).toBe('AI Bot');

    const res2 = parsePlatform('Unknown|0|0');
    expect(res2.code).toBe('bot');
    expect(res2.name).toBe('AI Bot');
  });

  it('handles unknown and empty strings', () => {
    const res1 = parsePlatform('');
    expect(res1.code).toBe('unknown');

    const res2 = parsePlatform('OtherNet|999|0');
    expect(res2.code).toBe('unknown');
  });
});
