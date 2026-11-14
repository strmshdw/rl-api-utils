import React from 'react';
import { PlayerSummary } from '../../types/api';
import {
  computeDynamicRelationshipTag,
  emptyRoleStats,
  formatMmr,
  hasSyncedRoleTelemetry,
  platformLabel,
  shortRank,
} from '../../utils/broadcast';
import { Spotlight, primaryRank } from './playerData';

export interface PinnedSlot {
  idx: number;
  player: PlayerSummary | null;
  spot?: Spotlight;
  isPinned: boolean;
}

interface PinnedPlayerCardProps {
  slotIndex: number;
  player: PlayerSummary | null;
  spot?: Spotlight;
  isPinned: boolean;
  isAvg: boolean;
  onTogglePin: (playerId: string) => void;
  onOpen: (player: PlayerSummary) => void;
}

export const PinnedPlayerCard: React.FC<PinnedPlayerCardProps> = ({
  slotIndex,
  player,
  spot,
  isPinned,
  isAvg,
  onTogglePin,
  onOpen,
}) => {
  const testId =
    spot?.key === 'mate'
      ? 'spotlight-mate'
      : spot?.key === 'faced'
      ? 'spotlight-faced'
      : spot?.key === 'rival'
      ? 'spotlight-rival'
      : `pinned-slot-${slotIndex}`;

  if (!player) {
    return (
      <div
        data-testid={testId}
        className="t-bg-panel bg-panel border border-dashed border-outline p-5 flex flex-col items-center justify-center gap-1 text-sm font-extrabold text-muted t-radius"
      >
        <span>EMPTY PIN SLOT ({slotIndex + 1}/3)</span>
        {spot && (
          <span className="text-sm text-faint">
            {spot.label} · {spot.sub}
          </span>
        )}
      </div>
    );
  }

  const rank = primaryRank(player.ranks_json);
  const wWins = player.total_wins_as_teammate;
  const wLosses = player.total_losses_as_teammate;
  const wGames = wWins + wLosses;
  const wWr = wGames > 0 ? Math.round((wWins / wGames) * 100) : 0;

  const aWins = player.total_wins_as_opponent;
  const aLosses = player.total_losses_as_opponent;
  const aGames = aWins + aLosses;
  const aWr = aGames > 0 ? Math.round((aWins / aGames) * 100) : 0;

  const rel = computeDynamicRelationshipTag(wWins, wLosses, aWins, aLosses, false);
  const wStats = player.with_you_stats || emptyRoleStats(wGames, wWins, wLosses);
  const aStats = player.against_you_stats || emptyRoleStats(aGames, aWins, aLosses);
  const wSynced = wGames > 0 && hasSyncedRoleTelemetry(wStats);
  const aSynced = aGames > 0 && hasSyncedRoleTelemetry(aStats);

  return (
    <div
      data-testid={testId}
      onClick={() => onOpen(player)}
      className={`t-bg-panel bg-panel hover:bg-panel-hover border-t-4 ${rel.border} border-x border-b border-line p-4 cursor-pointer transition-colors flex flex-col justify-between gap-3 t-num t-radius`}
    >
      <div className="flex justify-between items-start gap-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span
              className={`px-2 py-0.5 t-bg-page bg-page border border-line text-sm font-extrabold ${rel.color}`}
            >
              {rel.tag}
            </span>
            {spot && (
              <span className="text-sm font-bold text-muted uppercase truncate">
                {spot.label} · {spot.sub}
              </span>
            )}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onTogglePin(player.player_id);
              }}
              className="text-sm text-muted hover:text-loss font-extrabold cursor-pointer"
            >
              {isPinned ? '★ UNPIN' : '☆ PIN'}
            </button>
          </div>
          <div
            title={player.player_name}
            className="t-font-display font-extrabold text-[28px] t-text-strong text-ink-strong leading-tight mt-1 truncate uppercase"
          >
            {player.player_name}
          </div>
          <div className="text-sm font-extrabold text-muted">
            <span className="t-text-blue text-blue-ink t-font-mono">
              {formatMmr(rank?.mmr)} MMR
            </span>{' '}
            · {rank?.rank_name ? shortRank(rank.rank_name, rank.division) : 'UNRANKED'} ·{' '}
            {platformLabel(player.platform || player.player_id)}
          </div>
        </div>
        <div className="text-right text-sm font-extrabold t-font-mono shrink-0">
          <div className={wGames > 0 ? 'text-win' : 'text-faint'}>
            WITH: {wGames > 0 ? `${wWins}–${wLosses} (${wWr}%)` : '—'}
          </div>
          <div className={`${aGames > 0 ? 'text-orange-ink' : 'text-faint'} mt-0.5`}>
            VS: {aGames > 0 ? `${aWins}–${aLosses} (${aWr}%)` : '—'}
          </div>
        </div>
      </div>

      {/* Shared Header Mini Table inside Pinned Card */}
      <div className="t-bg-page bg-page border border-line text-sm font-extrabold t-font-mono">
        <div className="grid grid-cols-[64px_42px_repeat(5,1fr)] items-center px-2.5 py-1 text-muted font-sans border-b border-line">
          <span>ROLE</span>
          <span className="text-right">GMS</span>
          <span className="text-right">PTS</span>
          <span className="text-right">G</span>
          <span className="text-right">A</span>
          <span className="text-right">SV</span>
          <span className="text-right">DEM</span>
        </div>
        <div className="grid grid-cols-[64px_42px_repeat(5,1fr)] items-center px-2.5 py-1.5">
          <span className="text-win font-sans">WITH</span>
          <span className="text-right text-muted">{wGames}G</span>
          <span className="text-right text-ink-strong">
            {wSynced
              ? (isAvg
                  ? Math.round(wStats.avg_score)
                  : wStats.score || wStats.total_score || 0
                ).toLocaleString()
              : '—'}
          </span>
          <span className="text-right text-ink-strong">
            {wSynced
              ? isAvg
                ? wStats.avg_goals.toFixed(1)
                : wStats.goals || wStats.total_goals || 0
              : '—'}
          </span>
          <span className="text-right text-ink-2">
            {wSynced
              ? isAvg
                ? wStats.avg_assists.toFixed(1)
                : wStats.assists || wStats.total_assists || 0
              : '—'}
          </span>
          <span className="text-right text-ink-2">
            {wSynced
              ? isAvg
                ? wStats.avg_saves.toFixed(1)
                : wStats.saves || wStats.total_saves || 0
              : '—'}
          </span>
          <span className="text-right text-orange-ink">
            {wSynced
              ? isAvg
                ? (wStats.avg_demos || wStats.avg_demos_inflicted || 0).toFixed(1)
                : wStats.demos || wStats.total_demos_inflicted || 0
              : '—'}
          </span>
        </div>
        <div className="grid grid-cols-[64px_42px_repeat(5,1fr)] items-center px-2.5 py-1.5 border-t border-line">
          <span className="text-orange-ink font-sans">AGAINST</span>
          <span className="text-right text-muted">{aGames}G</span>
          <span className="text-right text-ink-strong">
            {aSynced
              ? (isAvg
                  ? Math.round(aStats.avg_score)
                  : aStats.score || aStats.total_score || 0
                ).toLocaleString()
              : '—'}
          </span>
          <span className="text-right text-ink-strong">
            {aSynced
              ? isAvg
                ? aStats.avg_goals.toFixed(1)
                : aStats.goals || aStats.total_goals || 0
              : '—'}
          </span>
          <span className="text-right text-ink-2">
            {aSynced
              ? isAvg
                ? aStats.avg_assists.toFixed(1)
                : aStats.assists || aStats.total_assists || 0
              : '—'}
          </span>
          <span className="text-right text-ink-2">
            {aSynced
              ? isAvg
                ? aStats.avg_saves.toFixed(1)
                : aStats.saves || aStats.total_saves || 0
              : '—'}
          </span>
          <span className="text-right text-orange-ink">
            {aSynced
              ? isAvg
                ? (aStats.avg_demos || aStats.avg_demos_inflicted || 0).toFixed(1)
                : aStats.demos || aStats.total_demos_inflicted || 0
              : '—'}
          </span>
        </div>
      </div>
    </div>
  );
};

interface PlayersSpotlightGridProps {
  pinnedSlots: PinnedSlot[];
  isAvg: boolean;
  onTogglePin: (playerId: string) => void;
  onOpenPlayer: (player: PlayerSummary) => void;
}

export const PlayersSpotlightGrid: React.FC<PlayersSpotlightGridProps> = ({
  pinnedSlots,
  isAvg,
  onTogglePin,
  onOpenPlayer,
}) => (
  <>
    <div className="mb-2 flex items-center justify-between">
      <span className="text-sm font-extrabold tracking-wider text-muted uppercase">
        PINNED & SPOTLIGHT PLAYERS ({pinnedSlots.filter((s) => s.player).length}/3) · CLICK ANY CARD FOR FULL-PAGE PROFILE
      </span>
    </div>
    <div id="pinned-players-grid" className="grid grid-cols-1 lg:grid-cols-3 gap-3 pb-7">
      {pinnedSlots.map((s) => (
        <PinnedPlayerCard
          key={s.idx}
          slotIndex={s.idx}
          player={s.player}
          spot={s.spot}
          isPinned={s.isPinned}
          isAvg={isAvg}
          onTogglePin={onTogglePin}
          onOpen={onOpenPlayer}
        />
      ))}
    </div>
  </>
);
