import { PLAYLIST_CATALOG } from '../contracts/generated_enums';
import type { LobbyPlayer, PlayerPlaylistRank, StampingConfig } from '../types/api';
import { DEFAULT_STAMPING_CONFIG } from './stampingConfig';

export interface LiveThreatBadge {
  type: 'higher' | 'carry' | 'smurf' | 'peak' | 'streak';
  label: string;
  title: string;
  classes: string;
}

const MODE_SHORT_LABEL: Record<string, string> = {
  '10': '1V1',
  '11': '2V2',
  '13': '3V3',
  '27': 'HOOPS',
  '28': 'RUMBLE',
  '29': 'DROPSHOT',
  '30': 'SNOW DAY',
  '34': 'TOURNAMENT',
};

const CORE_SOCCAR_IDS = new Set<number>([10, 11, 13]);
const EXTRA_MODE_IDS = new Set<number>([27, 28, 29, 30]);

function resolveActiveRank(
  player: LobbyPlayer | undefined | null,
  activePlaylistId: number,
): PlayerPlaylistRank | undefined {
  if (!player) return undefined;
  const activeKey = String(activePlaylistId);
  const byKey = player.ranks?.[activeKey];
  if (byKey && (byKey.mmr > 0 || byKey.tier > 0)) {
    return byKey;
  }
  if (player.current_rank && (!player.current_rank.playlist_id || player.current_rank.playlist_id === activePlaylistId)) {
    return player.current_rank;
  }
  return byKey ?? player.current_rank;
}

function sumAccountMatches(player: LobbyPlayer | undefined | null, fallbackRank?: PlayerPlaylistRank): number {
  let total = 0;
  if (player?.ranks) {
    for (const r of Object.values(player.ranks)) {
      if (r?.matches_played && r.matches_played > 0) {
        total += r.matches_played;
      }
    }
  }
  if (total === 0 && fallbackRank?.matches_played && fallbackRank.matches_played > 0) {
    return fallbackRank.matches_played;
  }
  return total;
}

/**
 * Normalizes a playlist's competitive tier + division onto a continuous Soccar-equivalent
 * tier scale S(p, r), accounting for 1v1's ~1.5-tier compression and Rumble's RNG inflation.
 */
export function soccarEquivalentTier(playlistId: number, rank: PlayerPlaylistRank | undefined): number {
  if (!rank || !rank.tier || rank.tier <= 0) return 0;
  const clampedDiv = Math.max(0, Math.min(3, (rank.division ?? 0) > 3 ? 3 : Math.max(0, rank.division ?? 0)));
  const raw = rank.tier + clampedDiv * 0.25;
  if (playlistId === 10) {
    return raw + (rank.tier >= 10 ? 1.5 : 0.75);
  }
  if (playlistId === 28) {
    return Math.max(1, raw - 1.0);
  }
  return raw;
}

function compactTierCode(rank: PlayerPlaylistRank): string {
  const rawName = (rank.rank_name || rank.short_rank || '').trim();
  const up = rawName.toUpperCase();
  if (up.includes('SUPERSONIC') || up.startsWith('SSL')) return 'SSL';

  const romanMatch = up.match(/\b(III|II|I|[1-3])\b/);
  const subTier =
    romanMatch?.[1] === 'III' || romanMatch?.[1] === '3'
      ? '3'
      : romanMatch?.[1] === 'II' || romanMatch?.[1] === '2'
      ? '2'
      : romanMatch?.[1] === 'I' || romanMatch?.[1] === '1'
      ? '1'
      : '';

  if (up.includes('GRAND CHAMPION') || up.startsWith('GC')) return `GC${subTier || '1'}`;
  if (up.includes('CHAMPION') || up.startsWith('CHAMP') || /^C[1-3]\b/.test(up)) return `C${subTier || '1'}`;
  if (up.includes('DIAMOND') || up.startsWith('DIA') || /^D[1-3]\b/.test(up)) return `D${subTier || '1'}`;
  if (up.includes('PLATINUM') || up.startsWith('PLAT') || /^P[1-3]\b/.test(up)) return `P${subTier || '1'}`;
  if (up.includes('GOLD') || /^G[1-3]\b/.test(up)) return `G${subTier || '1'}`;
  if (up.includes('SILVER') || up.startsWith('SILV') || /^S[1-3]\b/.test(up)) return `S${subTier || '1'}`;
  if (up.includes('BRONZE') || up.startsWith('BRNZ') || /^B[1-3]\b/.test(up)) return `B${subTier || '1'}`;

  if (rank.tier >= 22) return 'SSL';
  if (rank.tier >= 19) return `GC${rank.tier - 18}`;
  if (rank.tier >= 16) return `C${rank.tier - 15}`;
  if (rank.tier >= 13) return `D${rank.tier - 12}`;
  if (rank.tier >= 10) return `P${rank.tier - 9}`;
  if (rank.tier >= 7) return `G${rank.tier - 6}`;
  if (rank.tier >= 4) return `S${rank.tier - 3}`;
  if (rank.tier >= 1) return `B${rank.tier}`;
  return 'UNR';
}

function isRelevantOffPlaylist(activePlaylistId: number, candidatePlaylistId: number): boolean {
  if (candidatePlaylistId === activePlaylistId) return false;
  if (CORE_SOCCAR_IDS.has(activePlaylistId) || activePlaylistId === 34) {
    // Core Soccar & Tournaments only compare against Core Soccar (never Extra Modes or Casual)
    return CORE_SOCCAR_IDS.has(candidatePlaylistId);
  }
  if (EXTRA_MODE_IDS.has(activePlaylistId)) {
    return CORE_SOCCAR_IDS.has(candidatePlaylistId) || EXTRA_MODE_IDS.has(candidatePlaylistId);
  }
  return false;
}

function computeLocalSkillBaseline(
  localPlayer: LobbyPlayer | undefined | null,
  activePlaylistId: number,
  minGames: number,
): number {
  const localActiveRank = resolveActiveRank(localPlayer, activePlaylistId);
  let baseline = soccarEquivalentTier(activePlaylistId, localActiveRank);
  if (localPlayer?.ranks) {
    for (const [pidStr, r] of Object.entries(localPlayer.ranks)) {
      const pid = Number(pidStr);
      if (!CORE_SOCCAR_IDS.has(pid) || !r || (r.matches_played ?? 0) < minGames) continue;
      baseline = Math.max(baseline, soccarEquivalentTier(pid, r));
    }
  }
  return baseline;
}

/**
 * Option 5 Unified Multi-Signal Stamping Engine.
 * Evaluates true fresh alt uncertainty (Sigma + account volume + win streak + manual overrides),
 * calibrated & playlist-relevant Off-Mode Peaks, same-team Party Carries, active Higher MMR,
 * and Hot Streaks using user-configurable StampingConfig thresholds.
 */
export function computeLiveThreatBadges(
  player: LobbyPlayer,
  localPlayer: LobbyPlayer | undefined | null,
  lobbyOrTeamPlayers: LobbyPlayer[],
  activePlaylistId = 11,
  config: StampingConfig = DEFAULT_STAMPING_CONFIG,
): LiveThreatBadge[] {
  if (!player || player.is_local || player.is_bot) return [];
  if (activePlaylistId === 0) return [];
  const playlistMeta = PLAYLIST_CATALOG[activePlaylistId];
  if (playlistMeta && !playlistMeta.isRanked) return [];

  const badges: LiveThreatBadge[] = [];
  const isOpponent =
    localPlayer != null ? player.team_num !== localPlayer.team_num : player.team_num === 1;

  const activeRank = resolveActiveRank(player, activePlaylistId);
  const localActiveRank = resolveActiveRank(localPlayer, activePlaylistId);
  const playerActiveMmr = activeRank?.mmr ?? 0;
  const localActiveMmr = localActiveRank?.mmr ?? 0;

  const lowerTags = new Set((player.tags ?? []).map((t) => t.trim().toLowerCase()));
  const isManuallyVerified = lowerTags.has('verified') || lowerTags.has('not-smurf');
  const isManualSmurf = lowerTags.has('smurf') || lowerTags.has('alt');

  // 1. LIKELY ALT / TAGGED ALT (Opponent-only vermilion Hanko seal)
  if (isOpponent) {
    if (isManualSmurf && !isManuallyVerified) {
      badges.push({
        type: 'smurf',
        label: '◆ TAGGED ALT',
        title: 'Manually tagged as a known alt/smurf account in your scouting notes',
        classes: 'hanko-seal-vermilion',
      });
    } else if (config.alt_detection_enabled && !isManuallyVerified && activeRank) {
      const localSigma = localActiveRank?.sigma ?? 0;
      const localGames = sumAccountMatches(localPlayer, localActiveRank);
      const localUnsettled = localSigma > 2.9 && localGames > 0 && localGames <= 40;
      const sigma = activeRank.sigma ?? 0;
      const totalGames = sumAccountMatches(player, activeRank);
      const streak = activeRank.win_streak ?? 0;
      const tier = activeRank.tier ?? 0;

      if (!localUnsettled && tier >= config.alt_min_tier && sigma > 0 && totalGames > 0) {
        const highSigmaAlt =
          sigma >= config.alt_high_sigma && totalGames <= config.alt_high_sigma_max_games;
        const streakAlt =
          sigma >= config.alt_streak_sigma &&
          totalGames <= config.alt_streak_max_games &&
          streak >= config.alt_min_win_streak;

        if (highSigmaAlt || streakAlt) {
          badges.push({
            type: 'smurf',
            label: `◆ LIKELY ALT (σ ${sigma.toFixed(2)} · ${totalGames}G)`,
            title: `High skill uncertainty (Sigma ${sigma.toFixed(2)}) with only ${totalGames} tracked ranked games${
              streak >= 2 ? ` and ${streak}W streak` : ''
            }`,
            classes: 'hanko-seal-vermilion',
          });
        }
      }
    }
  }

  // 2. Active Lobby MMR Differential (Opponent-only, strictly real active MMR — zero hardcoded fallbacks)
  if (isOpponent && playerActiveMmr > 0 && localActiveMmr > 0) {
    const diff = Math.round(playerActiveMmr - localActiveMmr);
    if (diff >= config.higher_mmr_diff) {
      badges.push({
        type: 'higher',
        label: `◆ HIGHER MMR (+${diff} VS YOU)`,
        title: `Active playlist MMR is +${diff} higher than yours (${Math.round(playerActiveMmr)} vs ${Math.round(localActiveMmr)})`,
        classes: 'cartouche-gold',
      });
    }
  }

  // 3. Same-Team Party Carry (Opponent-only, strictly filtered to player.team_num)
  if (isOpponent && playerActiveMmr > 0 && lobbyOrTeamPlayers.length > 1) {
    const sameTeamMates = lobbyOrTeamPlayers.filter(
      (m) => m.team_num === player.team_num && m.player_id !== player.player_id && !m.is_bot,
    );
    let maxMateDiff = 0;
    for (const mate of sameTeamMates) {
      const mateRank = resolveActiveRank(mate, activePlaylistId);
      const mateMmr = mateRank?.mmr ?? 0;
      if (mateMmr > 0) {
        maxMateDiff = Math.max(maxMateDiff, Math.round(playerActiveMmr - mateMmr));
      }
    }
    if (maxMateDiff >= config.carry_mmr_diff) {
      badges.push({
        type: 'carry',
        label: `◆ PARTY CARRY (+${maxMateDiff} MMR VS MATE)`,
        title: `Active playlist MMR is +${maxMateDiff} above their teammate on the opposing team`,
        classes: 'cartouche-carry',
      });
    }
  }

  // 4. Calibrated Cross-Playlist OFF-MODE PEAK / TEAMMATE PEAK
  const allowPeakForRole = isOpponent ? config.show_off_mode_peak : config.show_teammate_off_mode_peak;
  if (allowPeakForRole && player.ranks) {
    const playerActiveEqTier = soccarEquivalentTier(activePlaylistId, activeRank);
    const localBaselineEqTier = computeLocalSkillBaseline(
      localPlayer,
      activePlaylistId,
      config.off_mode_min_games,
    );

    let bestPeak: {
      modeLabel: string;
      tierCode: string;
      mmr: number;
      eqTier: number;
      games: number;
    } | null = null;

    for (const [pidStr, r] of Object.entries(player.ranks)) {
      const pid = Number(pidStr);
      if (!Number.isInteger(pid) || !r || !r.tier || !r.mmr) continue;
      if (!isRelevantOffPlaylist(activePlaylistId, pid)) continue;
      const games = r.matches_played ?? 0;
      if (games < config.off_mode_min_games) continue;

      const candidateEqTier = soccarEquivalentTier(pid, r);
      const selfGap = playerActiveEqTier > 0 ? candidateEqTier - playerActiveEqTier : 99;
      const vsYouGap =
        localBaselineEqTier > 0 ? candidateEqTier - localBaselineEqTier : config.off_mode_vs_you_tier_diff;

      const passesThresholds = isOpponent
        ? selfGap >= config.off_mode_self_tier_diff && vsYouGap >= config.off_mode_vs_you_tier_diff
        : selfGap >= config.off_mode_self_tier_diff;

      if (passesThresholds && (!bestPeak || candidateEqTier > bestPeak.eqTier)) {
        bestPeak = {
          modeLabel: MODE_SHORT_LABEL[pidStr] || r.playlist_name?.toUpperCase() || `MODE ${pidStr}`,
          tierCode: compactTierCode(r),
          mmr: Math.round(r.mmr),
          eqTier: candidateEqTier,
          games,
        };
      }
    }

    if (bestPeak) {
      const prefix = isOpponent ? 'OFF-MODE PEAK' : 'TEAMMATE PEAK';
      badges.push({
        type: 'peak',
        label: `◆ ${prefix} · ${bestPeak.modeLabel} ${bestPeak.tierCode} (${bestPeak.mmr} MMR)`,
        title: `Calibrated peak in ${bestPeak.modeLabel}: ${bestPeak.tierCode} (${bestPeak.mmr} MMR across ${bestPeak.games} games)`,
        classes: isOpponent ? 'cartouche-carry' : 'cartouche-mono',
      });
    }
  }

  // 5. Active Playlist Win-Streak Momentum Stamp
  const streak = activeRank?.win_streak ?? 0;
  if (config.show_hot_streak && streak >= config.hot_streak_min_wins) {
    badges.push({
      type: 'streak',
      label: `◆ HOT STREAK (${streak}W)`,
      title: `Currently riding a ${streak}-game win streak in this playlist`,
      classes: isOpponent ? 'cartouche-gold' : 'cartouche-celadon text-win',
    });
  }

  return badges;
}
