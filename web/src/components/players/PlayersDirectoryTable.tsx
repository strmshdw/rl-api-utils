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
  platformLabel,
  recordInk,
  shortRank,
} from '../../utils/broadcast';
import { COLUMN_HEADER, RecordCells } from '../common/ui';
import { parseRanks, primaryRank } from './playerData';

const ROW_GRID =
  '28px minmax(0,1.35fr) 58px 72px minmax(0,1fr) 124px 124px 54px 112px 112px 90px';

interface DirectoryRowProps {
  player: PlayerSummary;
  isPinned: boolean;
  isAvg: boolean;
  playlistScopeId: number;
  onTogglePin: (playerId: string) => void;
  onOpen: () => void;
}

const DirectoryRow: React.FC<DirectoryRowProps> = ({
  player,
  isPinned,
  isAvg,
  playlistScopeId,
  onTogglePin,
  onOpen,
}) => {
  const ranks = parseRanks(player.ranks_json);
  const rank =
    playlistScopeId > 0
      ? ranks.find((r) => r.playlist_id === playlistScopeId) || primaryRank(player.ranks_json)
      : primaryRank(player.ranks_json);

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

  const rel = computeDynamicRelationshipTag(mate.wins, mate.losses, vs.wins, vs.losses, false);
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
    <div
      data-testid="player-row"
      data-player-id={player.player_id}
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onOpen();
        }
      }}
      className="grid items-center gap-2.5 h-[64px] px-4 bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 outline-none focus-visible:bg-panel-hover t-border border border-line t-num"
      style={{ gridTemplateColumns: ROW_GRID }}
    >
      <button
        type="button"
        aria-label={isPinned ? `Unpin ${player.player_name}` : `Pin ${player.player_name}`}
        onClick={(e) => {
          e.stopPropagation();
          onTogglePin(player.player_id);
        }}
        className={`text-left text-base cursor-pointer ${
          isPinned ? 't-text-blue text-blue-ink' : 'text-faint hover:text-ink'
        }`}
      >
        {isPinned ? '★' : '☆'}
      </button>

      <div className="min-w-0 pr-1">
        <div
          title={player.player_name}
          className="t-font-display font-extrabold text-[22px] t-text-strong text-ink-strong truncate uppercase leading-none"
        >
          {player.player_name || 'UNKNOWN'}
        </div>
        <div className={`text-sm font-extrabold ${rel.color} truncate mt-0.5`}>
          {rel.tag}
        </div>
      </div>

      <span className="font-extrabold text-sm tracking-t10 text-muted">
        {platformLabel(player.platform || player.player_id)}
      </span>

      <span className="text-right font-extrabold text-base t-text-blue text-blue-ink t-font-mono">
        {formatMmr(rank?.mmr)}
      </span>

      <span className="font-extrabold text-sm text-ink-2 uppercase truncate t-font-mono">
        {rank ? shortRank(rank.rank_name || 'UNRANKED', rank.division) : '—'}
        {rank?.rank_name && <span className="sr-only"> {rank.rank_name}</span>}
      </span>

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

      <span className="text-right font-extrabold text-base text-ink-strong t-font-mono">
        {player.total_matches}
      </span>

      <span className="text-right font-extrabold text-base text-win t-font-mono">
        {withPtsVal}
      </span>

      <span className="text-right font-extrabold text-base text-orange-ink t-font-mono">
        {vsPtsVal}
      </span>

      <span className="text-right font-bold text-sm tracking-t8 text-muted whitespace-nowrap">
        {formatAgo(player.last_seen_at)}
      </span>
    </div>
  );
};

interface PlayersDirectoryTableProps {
  rows: PlayerSummary[];
  pinnedPlayerIds: string[];
  isAvg: boolean;
  playlistScopeId: number;
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
  playlistScopeId,
  sort,
  order,
  onToggleSort,
  onTogglePin,
  onSelectPlayer,
}) => {
  const sortIndicator = (field: PlayerSortField) => {
    if (sort !== field) return null;
    return <span className="ml-1 text-ink">{order === 'asc' ? '▲' : '▼'}</span>;
  };

  return (
    <>
      <div
        id="players-table-header"
        className={`grid items-center gap-2.5 px-4 pb-2 ${COLUMN_HEADER}`}
        style={{ gridTemplateColumns: ROW_GRID }}
      >
        <span>★</span>
        <button
          type="button"
          onClick={() => onToggleSort('player_name')}
          className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
        >
          PLAYER {sortIndicator('player_name')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('platform')}
          className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
        >
          PLAT {sortIndicator('platform')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('mmr')}
          className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
        >
          MMR {sortIndicator('mmr')}
        </button>
        <span className="whitespace-nowrap truncate">RANK · DIV</span>
        <button
          type="button"
          onClick={() => onToggleSort('win_rate')}
          className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
        >
          WITH YOU {sortIndicator('win_rate')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('vs_win_rate')}
          className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
        >
          AGAINST {sortIndicator('vs_win_rate')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('total_matches')}
          className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
        >
          GAMES {sortIndicator('total_matches')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('with_pts')}
          className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
        >
          {isAvg ? 'WITH PTS/G' : 'WITH PTS'} {sortIndicator('with_pts')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('vs_pts')}
          className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
        >
          {isAvg ? 'VS PTS/G' : 'VS PTS'} {sortIndicator('vs_pts')}
        </button>
        <button
          type="button"
          onClick={() => onToggleSort('last_seen')}
          className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
        >
          LAST SEEN {sortIndicator('last_seen')}
        </button>
      </div>

      <div id="player-rows" className="flex flex-col gap-1.5">
        {rows.map((p) => (
          <DirectoryRow
            key={p.player_id}
            player={p}
            isPinned={pinnedPlayerIds.includes(p.player_id)}
            isAvg={isAvg}
            playlistScopeId={playlistScopeId}
            onTogglePin={onTogglePin}
            onOpen={() => onSelectPlayer({ id: p.player_id, name: p.player_name })}
          />
        ))}
      </div>
    </>
  );
};
