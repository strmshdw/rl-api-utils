import React from 'react';
import { PlayerSummary } from '../../types/api';
import { PlayerSortField } from '../../hooks/usePlayerSearch';
import {
  computeDynamicRelationshipTag,
  emptyRoleStats,
  formatAgo,
  formatMmr,
  formatPct,
  formatRecord,
  hasSyncedRoleTelemetry,
  recordInk,
  shortRank,
} from '../../utils/broadcast';
import { useDashboard } from '../../context/DashboardContext';
import { PlatformBadge } from '../common/PlatformBadge';
import { COLUMN_HEADER, RecordCells } from '../common/ui';
import { rankForPlaylist } from './playerData';
import { STAT_LABEL_CLASS } from '../../utils/statColors';

interface DirectoryRowProps {
  player: PlayerSummary;
  isPinned: boolean;
  isAvg: boolean;
  activePlaylist?: string;
  playlistScopeId?: number;
  onTogglePin: (playerId: string) => void;
  onOpen: () => void;
}

const DirectoryRow: React.FC<DirectoryRowProps> = ({
  player,
  isPinned,
  isAvg,
  activePlaylist,
  playlistScopeId,
  onTogglePin,
  onOpen,
}) => {
  const { stampingConfig } = useDashboard();
  const rank = rankForPlaylist(player.ranks_json, activePlaylist || playlistScopeId);

  const mate = {
    wins: player.total_wins_as_teammate,
    losses: player.total_losses_as_teammate,
  };
  const vs = {
    wins: player.total_wins_as_opponent,
    losses: player.total_losses_as_opponent,
  };
  const wGames = mate.wins + mate.losses;
  const aGames = vs.wins + vs.losses;

  const rel = computeDynamicRelationshipTag(mate.wins, mate.losses, vs.wins, vs.losses, false, {
    minGames: stampingConfig.relationship_min_games,
  });
  const wStats = player.with_you_stats || emptyRoleStats(wGames, mate.wins, mate.losses);
  const aStats = player.against_you_stats || emptyRoleStats(aGames, vs.wins, vs.losses);

  const withPtsVal =
    wGames > 0 && hasSyncedRoleTelemetry(wStats)
      ? (isAvg ? Math.round(wStats.avg_score) : wStats.score || wStats.total_score || 0).toLocaleString()
      : '—';
  const vsPtsVal =
    aGames > 0 && hasSyncedRoleTelemetry(aStats)
      ? (isAvg ? Math.round(aStats.avg_score) : aStats.score || aStats.total_score || 0).toLocaleString()
      : '—';

  return (
    <tr
      data-testid="player-row"
      data-player-id={player.player_id}
      onClick={onOpen}
      className="group border-b border-line last:border-b-0 bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 t-num"
    >
      <td className="w-10 px-3 py-2.5 text-center align-middle whitespace-nowrap">
        <button
          type="button"
          aria-label={isPinned ? `Unpin ${player.player_name}` : `Pin ${player.player_name}`}
          onClick={(e) => {
            e.stopPropagation();
            onTogglePin(player.player_id);
          }}
          className={`min-h-[24px] min-w-[24px] inline-flex items-center justify-center text-base cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink ${
            isPinned ? 't-text-blue text-blue-ink' : 'text-faint hover:text-ink'
          }`}
        >
          {isPinned ? '★' : '☆'}
        </button>
      </td>

      <th
        scope="row"
        className="sticky left-0 z-10 t-bg-panel bg-panel group-hover:bg-panel-hover px-3 py-2.5 text-left font-normal align-middle min-w-[180px] max-w-[240px] transition-colors duration-120"
      >
        <div
          title={player.player_name}
          className="t-font-display font-extrabold text-[22px] t-text-strong text-ink-strong truncate uppercase leading-none"
        >
          {player.player_name || 'UNKNOWN'}
        </div>
        <div className={`text-sm font-extrabold ${rel.color} truncate mt-0.5`}>
          {rel.tag}
        </div>
      </th>

      <td className="px-3 py-2.5 align-middle font-extrabold text-sm tracking-t10 text-ink-2 whitespace-nowrap">
        <PlatformBadge platform={player.platform || player.player_id} size="md" />
      </td>

      <td className={`px-3 py-2.5 align-middle text-right font-extrabold text-base t-font-mono whitespace-nowrap ${rank?.mmr ? STAT_LABEL_CLASS.mmr : 'text-faint'}`}>
        {formatMmr(rank?.mmr)}
      </td>

      <td className={`px-3 py-2.5 align-middle font-extrabold text-sm uppercase whitespace-nowrap t-font-mono ${rank ? STAT_LABEL_CLASS.rank : 'text-faint'}`}>
        {rank ? shortRank(rank.rank_name || 'UNRANKED', rank.division, rank.short_rank, rank.tier) : '—'}
        {rank?.rank_name && <span className="sr-only"> {rank.rank_name}</span>}
      </td>

      <td className="px-3 py-2.5 align-middle whitespace-nowrap">
        <div className="flex items-center gap-1.5">
          {wGames > 0 ? (
            <>
              <RecordCells wins={mate.wins} losses={mate.losses} count={5} />
              <span
                className={`font-extrabold text-base t-font-mono ${recordInk(
                  mate.wins,
                  mate.losses,
                )}`}
              >
                {formatRecord(mate.wins, mate.losses)}
              </span>
              <span className="sr-only">{formatPct(mate.wins, mate.losses)}</span>
            </>
          ) : (
            <span className="text-faint">—</span>
          )}
        </div>
      </td>

      <td className="px-3 py-2.5 align-middle whitespace-nowrap">
        <div className="flex items-center gap-1.5">
          {aGames > 0 ? (
            <>
              <RecordCells wins={vs.wins} losses={vs.losses} count={5} />
              <span
                className={`font-extrabold text-base t-font-mono ${recordInk(
                  vs.wins,
                  vs.losses,
                )}`}
              >
                {formatRecord(vs.wins, vs.losses)}
              </span>
              <span className="sr-only">{formatPct(vs.wins, vs.losses)}</span>
            </>
          ) : (
            <span className="text-faint">—</span>
          )}
        </div>
      </td>

      <td className={`px-3 py-2.5 align-middle text-right font-extrabold text-base t-font-mono whitespace-nowrap ${STAT_LABEL_CLASS.games}`}>
        {player.total_matches}
      </td>

      <td className={`px-3 py-2.5 align-middle text-right font-extrabold text-base t-font-mono whitespace-nowrap ${withPtsVal !== '—' ? STAT_LABEL_CLASS.score : 'text-faint'}`}>
        {withPtsVal}
      </td>

      <td className={`px-3 py-2.5 align-middle text-right font-extrabold text-base t-font-mono whitespace-nowrap ${vsPtsVal !== '—' ? STAT_LABEL_CLASS.shots : 'text-faint'}`}>
        {vsPtsVal}
      </td>

      <td className="px-3 py-2.5 align-middle text-right font-bold text-sm tracking-t8 text-muted whitespace-nowrap">
        {formatAgo(player.last_seen_at)}
      </td>

      <td className="px-3 py-2.5 align-middle text-right whitespace-nowrap">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onOpen();
          }}
          className="min-h-[24px] px-2.5 py-1 t-bg-page bg-page t-border border border-outline hover:border-ink text-xs font-extrabold t-text-blue text-blue-ink hover:text-ink-strong uppercase tracking-wider t-radius transition-colors cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink"
        >
          DOSSIER →
        </button>
      </td>
    </tr>
  );
};

interface PlayersDirectoryTableProps {
  rows: PlayerSummary[];
  pinnedPlayerIds: string[];
  isAvg: boolean;
  activePlaylist?: string;
  playlistScopeId?: number;
  sort: PlayerSortField;
  order: 'asc' | 'desc';
  onToggleSort: (field: PlayerSortField) => void;
  onTogglePin: (playerId: string) => void;
  onSelectPlayer: (player: { id: string; name?: string }) => void;
}

export const PlayersDirectoryTable: React.FC<PlayersDirectoryTableProps> = ({
  rows,
  pinnedPlayerIds,
  isAvg,
  activePlaylist,
  playlistScopeId,
  sort,
  order,
  onToggleSort,
  onTogglePin,
  onSelectPlayer,
}) => {
  const ariaSort = (field: PlayerSortField): 'ascending' | 'descending' | undefined => {
    if (sort === field || (field === 'vs_win_rate' && sort === 'against_wr')) {
      return order === 'asc' ? 'ascending' : 'descending';
    }
    return undefined;
  };

  const sortIndicator = (field: PlayerSortField) => {
    if (!ariaSort(field)) return null;
    return (
      <span aria-hidden="true" className="ml-1 text-ink">
        {order === 'asc' ? '▲' : '▼'}
      </span>
    );
  };

  const headerBtnClass = (align: 'left' | 'right' = 'left') =>
    `min-h-[24px] w-full px-3 py-2.5 hover:text-ink transition-colors flex items-center ${
      align === 'right' ? 'justify-end text-right' : 'justify-start text-left'
    } focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-ink cursor-pointer uppercase whitespace-nowrap`;

  return (
    <div
      role="region"
      aria-label="Players directory"
      tabIndex={0}
      className="overflow-x-auto t-border border border-line t-bg-panel bg-panel t-radius"
    >
      <table className="w-full min-w-[980px] border-collapse t-num text-left">
        <caption className="sr-only">Players directory</caption>
        <thead
          id="players-table-header"
          className={`border-b border-line t-bg-page bg-page ${COLUMN_HEADER}`}
        >
          <tr>
            <th scope="col" className="w-10 px-3 py-2.5 text-center whitespace-nowrap">
              <span aria-hidden="true">★</span>
              <span className="sr-only">Pinned</span>
            </th>
            <th
              scope="col"
              aria-sort={ariaSort('player_name')}
              className="sticky left-0 z-20 t-bg-page bg-page p-0 whitespace-nowrap"
            >
              <button
                type="button"
                onClick={() => onToggleSort('player_name')}
                className={headerBtnClass('left')}
              >
                PLAYER{sortIndicator('player_name')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('platform')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('platform')}
                className={headerBtnClass('left')}
              >
                PLAT{sortIndicator('platform')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('mmr')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('mmr')}
                className={`${headerBtnClass('right')} ${STAT_LABEL_CLASS.mmr}`}
              >
                MMR{sortIndicator('mmr')}
              </button>
            </th>
            <th scope="col" className={`px-3 py-2.5 whitespace-nowrap ${STAT_LABEL_CLASS.rank}`}>
              RANK · DIV
            </th>
            <th scope="col" aria-sort={ariaSort('win_rate')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('win_rate')}
                className={`${headerBtnClass('left')} ${STAT_LABEL_CLASS.goals}`}
              >
                WITH YOU{sortIndicator('win_rate')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('vs_win_rate')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('vs_win_rate')}
                className={`${headerBtnClass('left')} ${STAT_LABEL_CLASS.demos}`}
              >
                AGAINST{sortIndicator('vs_win_rate')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('total_matches')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('total_matches')}
                className={`${headerBtnClass('right')} ${STAT_LABEL_CLASS.games}`}
              >
                GAMES{sortIndicator('total_matches')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('with_pts')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('with_pts')}
                className={`${headerBtnClass('right')} ${STAT_LABEL_CLASS.score}`}
              >
                {isAvg ? 'WITH PTS/G' : 'WITH PTS'}
                {sortIndicator('with_pts')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('vs_pts')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('vs_pts')}
                className={`${headerBtnClass('right')} ${STAT_LABEL_CLASS.shots}`}
              >
                {isAvg ? 'VS PTS/G' : 'VS PTS'}
                {sortIndicator('vs_pts')}
              </button>
            </th>
            <th scope="col" aria-sort={ariaSort('last_seen')} className="p-0 whitespace-nowrap">
              <button
                type="button"
                onClick={() => onToggleSort('last_seen')}
                className={headerBtnClass('right')}
              >
                LAST SEEN{sortIndicator('last_seen')}
              </button>
            </th>
            <th scope="col" className="px-3 py-2.5 text-right whitespace-nowrap">
              <span className="sr-only">Dossier</span>
            </th>
          </tr>
        </thead>

        <tbody id="player-rows">
          {rows.map((p) => (
            <DirectoryRow
              key={p.player_id}
              player={p}
              isPinned={pinnedPlayerIds.includes(p.player_id)}
              isAvg={isAvg}
              activePlaylist={activePlaylist}
              playlistScopeId={playlistScopeId}
              onTogglePin={onTogglePin}
              onOpen={() => onSelectPlayer({ id: p.player_id, name: p.player_name })}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
};
