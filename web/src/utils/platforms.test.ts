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

  it('identifies PlayStation platform across PlayStation, PSN, PS4, and PS5', () => {
    for (const input of ['PlayStation|123|0', 'PSN|123|0', 'PS4|123|0', 'PS5|123|0']) {
      const res = parsePlatform(input);
      expect(res.code).toBe('psn');
      expect(res.name).toBe('PlayStation');
      expect(res.shortLabel).toBe('PLAYSTATION');
    }
  });

  it('identifies Xbox, Switch, and PsyNet platforms', () => {
    const xbox = parsePlatform('Xbox|123|0');
    expect(xbox.code).toBe('xbox');
    expect(xbox.name).toBe('Xbox');
    expect(xbox.shortLabel).toBe('XBOX');
    expect(parsePlatform('XboxOne|456|0').code).toBe('xbox');

    const sw = parsePlatform('Switch|789|0');
    expect(sw.code).toBe('switch');
    expect(sw.name).toBe('Nintendo Switch');
    expect(sw.shortLabel).toBe('SWITCH');
    expect(parsePlatform('Nintendo|789|0').code).toBe('switch');

    const psy = parsePlatform('PsyNet|999|0');
    expect(psy.code).toBe('psynet');
    expect(psy.name).toBe('PsyNet');
    expect(psy.shortLabel).toBe('PSYNET');
  });

  it('identifies AI Bot via string or explicit flag', () => {
    const res1 = parsePlatform('bot');
    expect(res1.code).toBe('bot');
    expect(res1.name).toBe('AI Bot');
    expect(res1.shortLabel).toBe('BOT');

    const res2 = parsePlatform('Unknown|0|0');
    expect(res2.code).toBe('bot');
    expect(res2.name).toBe('AI Bot');

    const res3 = parsePlatform('Steam|123|0', true);
    expect(res3.code).toBe('bot');
    expect(res3.shortLabel).toBe('BOT');
  });

  it('handles unknown and empty strings', () => {
    const res1 = parsePlatform('');
    expect(res1.code).toBe('unknown');
    expect(res1.shortLabel).toBe('—');

    const res2 = parsePlatform('OtherNet|999|0');
    expect(res2.code).toBe('unknown');
    expect(res2.shortLabel).toBe('OTHERNET');
  });
});
