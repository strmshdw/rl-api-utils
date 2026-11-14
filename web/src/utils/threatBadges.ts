import type { LobbyPlayer } from '../types/api';
import { shortRank } from './broadcastLabels';

export interface LiveThreatBadge {
  type: 'higher' | 'carry' | 'smurf';
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
};

/**
 * Computes Live Match Threat, Carry & Smurf Detection Badges for a player.
 * 1. HIGHER MMR (+X VS YOU): opponent active-playlist MMR > local player active-playlist MMR.
 * 2. CARRYING (+X MMR VS MATE): opponent active-playlist MMR >= +80 MMR above their teammate(s).
 * 3. SMURF · <MODE> <SHORT_RANK> (<MMR> · +<DIFF> VS YOU): off-mode peak >= +120 vs you & >= +80 vs active MMR.
 */
export function computeLiveThreatBadges(
  player: LobbyPlayer,
  localPlayer: LobbyPlayer | undefined | null,
  opposingTeamPlayers: LobbyPlayer[],
  activePlaylistId = 11,
): LiveThreatBadge[] {
  if (!player || player.is_local || player.is_bot) return [];
  const badges: LiveThreatBadge[] = [];

  const activeKey = String(activePlaylistId || 11);
  const playerActiveMmr = player.current_rank?.mmr || player.ranks?.[activeKey]?.mmr || 0;
  const localActiveMmr = localPlayer?.current_rank?.mmr || localPlayer?.ranks?.[activeKey]?.mmr || 1515;

  const isOpponent = localPlayer != null ? player.team_num !== localPlayer.team_num : player.team_num === 1;

  if (isOpponent && playerActiveMmr > 0 && localActiveMmr > 0 && playerActiveMmr > localActiveMmr) {
    const diff = Math.round(playerActiveMmr - localActiveMmr);
    if (diff > 0) {
      badges.push({
        type: 'higher',
        label: `◆ HIGHER MMR (+${diff} VS YOU)`,
        title: `Active playlist MMR is +${diff} higher than yours (${Math.round(playerActiveMmr)} vs ${Math.round(localActiveMmr)})`,
        classes: 'cartouche-gold',
      });
    }
  }

  if (isOpponent && playerActiveMmr > 0 && opposingTeamPlayers.length > 1) {
    const mates = opposingTeamPlayers.filter((m) => m.player_id !== player.player_id && !m.is_bot);
    let maxMateDiff = 0;
    for (const mate of mates) {
      const mateMmr = mate.current_rank?.mmr || mate.ranks?.[activeKey]?.mmr || 0;
      if (mateMmr > 0) {
        maxMateDiff = Math.max(maxMateDiff, Math.round(playerActiveMmr - mateMmr));
      }
    }
    if (maxMateDiff >= 80) {
      badges.push({
        type: 'carry',
        label: `◆ CARRYING (+${maxMateDiff} MMR VS MATE)`,
        title: `Significantly higher MMR (+${maxMateDiff}) than their teammate on the opposing team`,
        classes: 'cartouche-carry',
      });
    }
  }

  if (player.ranks) {
    let bestSmurf: {
      modeLabel: string;
      rankShort: string;
      mmr: number;
      diffVsYou: number;
    } | null = null;

    for (const [pid, r] of Object.entries(player.ranks)) {
      if (pid === activeKey || !r || !r.mmr) continue;
      const localModeMmr = localPlayer?.ranks?.[pid]?.mmr || localActiveMmr || 1450;
      const diffVsYou = Math.round(r.mmr - localModeMmr);
      const diffVsOwnActive = playerActiveMmr > 0 ? Math.round(r.mmr - playerActiveMmr) : 999;

      if (diffVsYou >= 120 && diffVsOwnActive >= 80) {
        if (!bestSmurf || diffVsYou > bestSmurf.diffVsYou) {
          const modeLabel = MODE_SHORT_LABEL[pid] || r.playlist_name?.toUpperCase() || `MODE ${pid}`;
          const rankShort = shortRank(r.rank_name, r.division).split('·')[0].trim();
          bestSmurf = {
            modeLabel,
            rankShort,
            mmr: Math.round(r.mmr),
            diffVsYou,
          };
        }
      }
    }

    if (bestSmurf) {
      badges.push({
        type: 'smurf',
        label: `◆ SMURF · ${bestSmurf.modeLabel} ${bestSmurf.rankShort} (${bestSmurf.mmr} · +${bestSmurf.diffVsYou} VS YOU)`,
        title: `Off-playlist peak: ${bestSmurf.modeLabel} ${bestSmurf.rankShort} (${bestSmurf.mmr} MMR, +${bestSmurf.diffVsYou} above you in that playlist)`,
        classes: 'hanko-seal-vermilion',
      });
    }
  }

  return badges;
}
