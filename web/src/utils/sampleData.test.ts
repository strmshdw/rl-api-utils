import { describe, expect, it } from 'vitest';
import { PREVIEW_FIXTURE_SENTINEL as PREVIEW_SENTINEL } from '../preview';
import {
  EMPTY_CURRENT_MATCH,
  EMPTY_SESSION_RESPONSE,
  PREVIEW_FIXTURE_SENTINEL,
  SAMPLE_CURRENT_MATCH,
  SAMPLE_HISTORICAL_MATCHES,
  SAMPLE_PLAYERS,
  SAMPLE_SESSION_RESPONSE,
  getSamplePlayerMatches,
  getSamplePlayerProfile,
  isPreviewBuildAllowed,
} from './sampleData';

describe('sampleData compile-time preview facade', () => {
  it('exposes fixtures synchronously in vitest preview-enabled mode', () => {
    expect(isPreviewBuildAllowed()).toBe(true);
    expect(PREVIEW_FIXTURE_SENTINEL).toBe('__RL_SYNC_DEV_PREVIEW_FIXTURE__');
    expect(PREVIEW_SENTINEL).toBe('__RL_SYNC_DEV_PREVIEW_FIXTURE__');
    expect(SAMPLE_CURRENT_MATCH.active_match).toBe(true);
    expect(SAMPLE_SESSION_RESPONSE.total_matches).toBe(5);
    expect(SAMPLE_HISTORICAL_MATCHES.length).toBe(3);
    expect(SAMPLE_PLAYERS.length).toBe(5);
    expect(SAMPLE_CURRENT_MATCH.teammates[0].matchups_by_playlist?.['11']?.wins_as_teammate).toBe(4);
    expect(SAMPLE_CURRENT_MATCH.opponents[0].matchups_by_playlist?.['10']).toBeDefined();
    const aeroProfile = getSamplePlayerProfile('Steam|76561198000000010|0');
    expect(aeroProfile.player_name).toBe('AeroPulse');
    expect(aeroProfile.playlist_breakdown?.length).toBeGreaterThanOrEqual(2);
    expect(getSamplePlayerMatches('Steam|76561198000000010|0').length).toBe(3);
  });

  it('defines safe empty fallbacks', () => {
    expect(EMPTY_CURRENT_MATCH.active_match).toBe(false);
    expect(EMPTY_CURRENT_MATCH.teammates).toEqual([]);
    expect(EMPTY_SESSION_RESPONSE.total_matches).toBe(0);
    expect(EMPTY_SESSION_RESPONSE.matches).toEqual([]);
  });
});
