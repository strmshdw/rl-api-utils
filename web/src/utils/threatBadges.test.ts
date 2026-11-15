import { describe, expect, it } from 'vitest';
import type { LobbyPlayer } from '../types/api';
import { computeDynamicRelationshipTag } from './broadcastLabels';
import { DEFAULT_STAMPING_CONFIG, normalizeStampingConfig } from './stampingConfig';
import { computeLiveThreatBadges } from './threatBadges';

function makeLocal(over: Partial<LobbyPlayer> = {}): LobbyPlayer {
  return {
    player_id: 'Epic|local_me|0',
    platform: 'Epic',
    name: 'You',
    team_num: 0,
    is_local: true,
    is_bot: false,
    stats: { score: 400, goals: 1, assists: 1, saves: 2, shots: 4, demos: 0 },
    current_rank: {
      playlist_id: 11,
      playlist_name: 'Ranked Doubles',
      tier: 16,
      division: 1,
      rank_name: 'Grand Champion I',
      mmr: 1485,
      matches_played: 160,
      sigma: 2.5,
      win_streak: 1,
    },
    ranks: {
      '11': {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 16,
        division: 1,
        rank_name: 'Grand Champion I',
        mmr: 1485,
        matches_played: 160,
        sigma: 2.5,
      },
    },
    ...over,
  };
}

function makeOpponent(id: string, over: Partial<LobbyPlayer> = {}): LobbyPlayer {
  return {
    player_id: id,
    platform: 'Steam',
    name: id,
    team_num: 1,
    is_local: false,
    is_bot: false,
    stats: { score: 350, goals: 1, assists: 0, saves: 2, shots: 3, demos: 1 },
    ...over,
  };
}

describe('Option 5 Unified Multi-Signal Stamping & UI Config', () => {
  it('flags true fresh alt accounts via high PsyNet Sigma + low account games', () => {
    const local = makeLocal();
    const freshAlt = makeOpponent('FreshAlt', {
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 15,
        division: 3,
        rank_name: 'Champion III',
        mmr: 1410,
        matches_played: 18,
        sigma: 3.55,
        win_streak: 6,
      },
      ranks: {
        '11': {
          playlist_id: 11,
          playlist_name: 'Ranked Doubles',
          tier: 15,
          division: 3,
          rank_name: 'Champion III',
          mmr: 1410,
          matches_played: 18,
          sigma: 3.55,
          win_streak: 6,
        },
      },
    });

    const badges = computeLiveThreatBadges(freshAlt, local, [freshAlt], 11, DEFAULT_STAMPING_CONFIG);
    expect(badges.some((b) => b.type === 'smurf' && b.label.includes('◆ LIKELY ALT'))).toBe(true);
    expect(badges.some((b) => b.type === 'streak' && b.label.includes('HOT STREAK (6W)'))).toBe(true);
  });

  it('suppresses LIKELY ALT during local season soft-reset placements or when tagged verified', () => {
    const unsettledLocal = makeLocal({
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 15,
        division: 2,
        rank_name: 'Champion III',
        mmr: 1390,
        matches_played: 6,
        sigma: 3.1,
      },
      ranks: {
        '11': {
          playlist_id: 11,
          playlist_name: 'Ranked Doubles',
          tier: 15,
          division: 2,
          rank_name: 'Champion III',
          mmr: 1390,
          matches_played: 6,
          sigma: 3.1,
        },
      },
    });
    const highSigmaOpp = makeOpponent('ResetPeer', {
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 15,
        division: 2,
        rank_name: 'Champion III',
        mmr: 1405,
        matches_played: 9,
        sigma: 3.45,
        win_streak: 4,
      },
    });

    const resetBadges = computeLiveThreatBadges(highSigmaOpp, unsettledLocal, [highSigmaOpp], 11);
    expect(resetBadges.some((b) => b.type === 'smurf')).toBe(false);

    // Settled local player, but opponent is manually tagged "verified" -> suppressed
    const verifiedOpp = { ...highSigmaOpp, tags: ['verified'] };
    const verifiedBadges = computeLiveThreatBadges(verifiedOpp, makeLocal(), [verifiedOpp], 11);
    expect(verifiedBadges.some((b) => b.type === 'smurf')).toBe(false);

    // Manually tagged "smurf" forces vermilion alt seal even on settled account
    const manualSmurf = makeOpponent('KnownBoostedAlt', {
      tags: ['smurf'],
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 16,
        division: 1,
        rank_name: 'Grand Champion I',
        mmr: 1480,
        matches_played: 200,
        sigma: 2.5,
      },
    });
    const manualBadges = computeLiveThreatBadges(manualSmurf, makeLocal(), [manualSmurf], 11);
    expect(manualBadges.some((b) => b.type === 'smurf' && b.label.includes('TAGGED ALT'))).toBe(true);
  });

  it('eliminates false-positive smurf stamps from unplayed user playlists, placements, and Extra Modes while surfacing true OFF-MODE PEAKs', () => {
    // Local user ONLY plays 2v2 (GC1 Div II = 16.25), 0 games in 3v3 & 1v1!
    const local = makeLocal();

    // Case A: Peer GC1 in 2v2 who also plays 3v3 at GC1 Div IV -> ZERO stamps!
    const normalPeer = makeOpponent('NormalPeer', {
      current_rank: {
        playlist_id: 11,
        playlist_name: 'Ranked Doubles',
        tier: 16,
        division: 2,
        rank_name: 'Grand Champion I',
        mmr: 1505,
        matches_played: 140,
        sigma: 2.5,
      },
      ranks: {
        '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 16, division: 2, rank_name: 'Grand Champion I', mmr: 1505, matches_played: 140 },
        '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 16, division: 3, rank_name: 'Grand Champion I', mmr: 1530, matches_played: 120 },
        '28': { playlist_id: 28, playlist_name: 'Rumble', tier: 19, division: 0, rank_name: 'Supersonic Legend', mmr: 1380, matches_played: 90 },
      },
    });
    expect(computeLiveThreatBadges(normalPeer, local, [normalPeer], 11)).toEqual([]);

    // Case B: Placement-only 3v3 SSL (4 matches < 15 min games) -> ignored!
    const uncalibratedOffMode = makeOpponent('FourGamesOnly', {
      current_rank: { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 15, division: 3, rank_name: 'Champion III', mmr: 1410, matches_played: 80 },
      ranks: {
        '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 15, division: 3, rank_name: 'Champion III', mmr: 1410, matches_played: 80 },
        '13': { playlist_id: 13, playlist_name: 'Ranked Standard', tier: 19, division: 0, rank_name: 'Supersonic Legend', mmr: 1895, matches_played: 4 },
      },
    });
    expect(computeLiveThreatBadges(uncalibratedOffMode, local, [uncalibratedOffMode], 11)).toEqual([]);

    // Case C: Calibrated 1v1 GC2 (tier 17, 1365 MMR, 60 games) playing in C3 2v2 -> 1v1 tier compression (+1.5 -> 18.5) triggers OFF-MODE PEAK!
    const duelSpecialist = makeOpponent('DuelGod', {
      current_rank: { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 15, division: 1, rank_name: 'Champion III', mmr: 1385, matches_played: 90 },
      ranks: {
        '10': { playlist_id: 10, playlist_name: 'Ranked Duel', tier: 17, division: 0, rank_name: 'Grand Champion II', mmr: 1365, matches_played: 60 },
        '11': { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 15, division: 1, rank_name: 'Champion III', mmr: 1385, matches_played: 90 },
      },
    });
    const duelBadges = computeLiveThreatBadges(duelSpecialist, local, [duelSpecialist], 11);
    expect(duelBadges.some((b) => b.type === 'peak' && b.label.includes('◆ OFF-MODE PEAK · 1V1 GC2'))).toBe(true);
  });

  it('evaluates PARTY CARRY strictly within the same team and respects UI StampingConfig overrides', () => {
    const local = makeLocal();
    const mateLow = makeOpponent('MateLow', {
      current_rank: { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 15, division: 2, rank_name: 'Champion III', mmr: 1400, matches_played: 100 },
    });
    const carryOpp = makeOpponent('CarryOpp', {
      current_rank: { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 17, division: 1, rank_name: 'Grand Champion II', mmr: 1560, matches_played: 150 },
    });
    // Pass mixed lobby including local player (1485) and another hypothetical opponent or ally to ensure team_num isolation
    const lowAlly: LobbyPlayer = {
      ...makeOpponent('LowAlly', {
        team_num: 0,
        current_rank: { playlist_id: 11, playlist_name: 'Ranked Doubles', tier: 13, division: 0, rank_name: 'Diamond I', mmr: 1050, matches_played: 90 },
      }),
    };

    // CarryOpp (1560) vs MateLow (1400) = +160 (NOT +510 vs LowAlly on team 0!)
    const defaultBadges = computeLiveThreatBadges(carryOpp, local, [local, lowAlly, mateLow, carryOpp], 11);
    expect(defaultBadges.some((b) => b.type === 'carry' && b.label === '◆ PARTY CARRY (+160 MMR VS MATE)')).toBe(true);
    expect(defaultBadges.some((b) => b.type === 'higher' && b.label === '◆ HIGHER MMR (+75 VS YOU)')).toBe(true);

    // Raise carry_mmr_diff to 200 and higher_mmr_diff to 100 via UI config -> both badges suppress cleanly!
    const stricterCfg = normalizeStampingConfig({
      ...DEFAULT_STAMPING_CONFIG,
      carry_mmr_diff: 200,
      higher_mmr_diff: 100,
    });
    expect(computeLiveThreatBadges(carryOpp, local, [mateLow, carryOpp], 11, stricterCfg)).toEqual([]);
  });

  it('unifies relationship tag minimum sample floor and active-role priority', () => {
    // Default minGames = 3 -> 1 or 2 games yields RECENT ALLY / RECENT FOE instead of premature NEMESIS
    expect(computeDynamicRelationshipTag(0, 0, 0, 1).tag).toBe('RECENT FOE');
    expect(computeDynamicRelationshipTag(1, 0, 0, 0).tag).toBe('RECENT ALLY');
    expect(computeDynamicRelationshipTag(0, 0, 1, 3).tag).toBe('NEMESIS');
    expect(computeDynamicRelationshipTag(0, 0, 2, 2).tag).toBe('ARCH-RIVAL');

    // Role-aware evaluation: player is 4-1 WITH YOU, 0-3 VS YOU -> as active opponent, surfaces NEMESIS!
    expect(computeDynamicRelationshipTag(4, 1, 0, 3, undefined, { minGames: 3, activeRole: 'opponent' }).tag).toBe(
      'NEMESIS',
    );
    expect(computeDynamicRelationshipTag(4, 1, 0, 3, undefined, { minGames: 3, activeRole: 'teammate' }).tag).toBe(
      'PRIME DUO',
    );
  });
});
