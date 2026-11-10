import React, { useEffect, useMemo, useState } from 'react';
import { PlayerSummary } from '../../types/api';
import { usePlayerSearch, PlayerSortField } from '../../hooks/usePlayerSearch';
import {
  computeDynamicRelationshipTag,
  formatAgo,
  formatMmr,
  formatPct,
  formatRecord,
  platformLabel,
  recordInk,
  shortRank,
  synthesizeRoleStats,
} from '../../utils/broadcast';
import {
  EmptyLine,
  GhostButton,
  PageHeader,
  RecordCells,
  SearchInput,
  Segmented,
  COLUMN_HEADER,
} from '../common/ui';
import { PlayerProfilePage } from './PlayerProfilePage';
import { Spotlight, matchesRole, parseRanks, primaryRank, spotlights } from './playerData';
import { useDashboard } from '../../context/DashboardContext';

const PLAYLIST_OPTIONS = [
  ['all', 'ALL MODES'],
  ['1v1', '1V1'],
  ['2v2', '2V2'],
  ['3v3', '3V3'],
] as const;

const PLAYLIST_ID_MAP: Record<string, number> = {
  all: 0,
  '1v1': 10,
  '2v2': 11,
  '3v3': 13,
};

const ROLE_OPTIONS = [
  ['all', 'ALL'],
  ['frequent', 'FREQUENT ≥2'],
  ['teammate', 'TEAMMATES (WITH YOU)'],
  ['opponent', 'OPPONENTS (AGAINST)'],
  ['synergy', 'SYNERGY ≥50%'],
] as const;

const PLATFORM_OPTIONS = [
  ['all', 'ALL'],
  ['steam', 'STEAM'],
  ['epic', 'EPIC'],
  ['playstation', 'PSN'],
  ['xbox', 'XBOX'],
] as const;

const ROW_GRID = '28px minmax(0,1.35fr) 58px 72px minmax(0,1fr) 124px 124px 54px 112px 112px 90px';

const PinnedPlayerCard: React.FC<{
  slotIndex: number;
  player: PlayerSummary | null;
  spot?: Spotlight;
  isPinned: boolean;
  isAvg: boolean;
  onTogglePin: (playerId: string) => void;
  onOpen: (player: PlayerSummary) => void;
}> = ({ slotIndex, player, spot, isPinned, isAvg, onTogglePin, onOpen }) => {
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
        {spot && <span className="text-sm text-faint">{spot.label} · {spot.sub}</span>}
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
  const wStats = player.with_you_stats || synthesizeRoleStats(wWins, wLosses, player.player_id);
  const aStats = player.against_you_stats || synthesizeRoleStats(aWins, aLosses, `${player.player_id}-vs`);

  return (
    <div
      data-testid={testId}
      onClick={() => onOpen(player)}
      className={`t-bg-panel bg-panel hover:bg-panel-hover border-t-4 ${rel.border} border-x border-b border-line p-4 cursor-pointer transition-colors flex flex-col justify-between gap-3 t-num t-radius`}
    >
      <div className="flex justify-between items-start gap-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className={`px-2 py-0.5 t-bg-page bg-page border border-line text-sm font-extrabold ${rel.color}`}>
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
            <span className="t-text-blue text-blue-ink t-font-mono">{formatMmr(rank?.mmr)} MMR</span> ·{' '}
            {rank?.rank_name ? shortRank(rank.rank_name, rank.division) : 'UNRANKED'} ·{' '}
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
            {wGames > 0 ? (isAvg ? Math.round(wStats.avg_score) : (wStats.score ?? wStats.total_score ?? 0)).toLocaleString() : '—'}
          </span>
          <span className="text-right text-ink-strong">
            {wGames > 0 ? (isAvg ? wStats.avg_goals.toFixed(1) : (wStats.goals ?? wStats.total_goals ?? 0)) : '—'}
          </span>
          <span className="text-right text-ink-2">
            {wGames > 0 ? (isAvg ? wStats.avg_assists.toFixed(1) : (wStats.assists ?? wStats.total_assists ?? 0)) : '—'}
          </span>
          <span className="text-right text-ink-2">
            {wGames > 0 ? (isAvg ? wStats.avg_saves.toFixed(1) : (wStats.saves ?? wStats.total_saves ?? 0)) : '—'}
          </span>
          <span className="text-right text-orange-ink">
            {wGames > 0 ? (isAvg ? (wStats.avg_demos ?? wStats.avg_demos_inflicted ?? 0).toFixed(1) : (wStats.demos ?? wStats.total_demos_inflicted ?? 0)) : '—'}
          </span>
        </div>
        <div className="grid grid-cols-[64px_42px_repeat(5,1fr)] items-center px-2.5 py-1.5 border-t border-line">
          <span className="text-orange-ink font-sans">AGAINST</span>
          <span className="text-right text-muted">{aGames}G</span>
          <span className="text-right text-ink-strong">
            {aGames > 0 ? (isAvg ? Math.round(aStats.avg_score) : (aStats.score ?? aStats.total_score ?? 0)).toLocaleString() : '—'}
          </span>
          <span className="text-right text-ink-strong">
            {aGames > 0 ? (isAvg ? aStats.avg_goals.toFixed(1) : (aStats.goals ?? aStats.total_goals ?? 0)) : '—'}
          </span>
          <span className="text-right text-ink-2">
            {aGames > 0 ? (isAvg ? aStats.avg_assists.toFixed(1) : (aStats.assists ?? aStats.total_assists ?? 0)) : '—'}
          </span>
          <span className="text-right text-ink-2">
            {aGames > 0 ? (isAvg ? aStats.avg_saves.toFixed(1) : (aStats.saves ?? aStats.total_saves ?? 0)) : '—'}
          </span>
          <span className="text-right text-orange-ink">
            {aGames > 0 ? (isAvg ? (aStats.avg_demos ?? aStats.avg_demos_inflicted ?? 0).toFixed(1) : (aStats.demos ?? aStats.total_demos_inflicted ?? 0)) : '—'}
          </span>
        </div>
      </div>
    </div>
  );
};

const DirectoryRow: React.FC<{
  player: PlayerSummary;
  isPinned: boolean;
  isAvg: boolean;
  playlistScopeId: number;
  onTogglePin: (playerId: string) => void;
  onOpen: () => void;
}> = ({ player, isPinned, isAvg, playlistScopeId, onTogglePin, onOpen }) => {
  const ranks = parseRanks(player.ranks_json);
  const rank =
    playlistScopeId > 0
      ? ranks.find((r) => r.playlist_id === playlistScopeId) || primaryRank(player.ranks_json)
      : primaryRank(player.ranks_json);

  const mate = { wins: player.total_wins_as_teammate, losses: player.total_losses_as_teammate };
  const vs = { wins: player.total_wins_as_opponent, losses: player.total_losses_as_opponent };
  const wGames = mate.wins + mate.losses;
  const aGames = vs.wins + vs.losses;

  const rel = computeDynamicRelationshipTag(mate.wins, mate.losses, vs.wins, vs.losses, false);
  const wStats = player.with_you_stats || synthesizeRoleStats(mate.wins, mate.losses, player.player_id);
  const aStats = player.against_you_stats || synthesizeRoleStats(vs.wins, vs.losses, `${player.player_id}-vs`);

  const withPtsVal = wGames > 0 ? (isAvg ? Math.round(wStats.avg_score) : (wStats.score ?? wStats.total_score ?? 0).toLocaleString()) : '—';
  const vsPtsVal = aGames > 0 ? (isAvg ? Math.round(aStats.avg_score) : (aStats.score ?? aStats.total_score ?? 0).toLocaleString()) : '—';

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
        className={`text-left text-base cursor-pointer ${isPinned ? 't-text-blue text-blue-ink' : 'text-faint hover:text-ink'}`}
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
        <div className={`text-sm font-extrabold ${rel.color} truncate mt-0.5`}>{rel.tag}</div>
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
            <span className={`font-extrabold text-base t-font-mono ${recordInk(mate.wins, mate.losses)}`}>
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
            <span className={`font-extrabold text-base t-font-mono ${recordInk(vs.wins, vs.losses)}`}>
              {formatRecord(vs.wins, vs.losses)}
            </span>
            <span className="sr-only">{formatPct(vs.wins, vs.losses)}</span>
          </>
        ) : (
          <span className="text-faint">—</span>
        )}
      </div>

      <span className="text-right font-extrabold text-base text-ink-strong t-font-mono">{player.total_matches}</span>

      <span className="text-right font-extrabold text-base text-win t-font-mono">{withPtsVal}</span>

      <span className="text-right font-extrabold text-base text-orange-ink t-font-mono">{vsPtsVal}</span>

      <span className="text-right font-bold text-sm tracking-t8 text-muted whitespace-nowrap">
        {formatAgo(player.last_seen_at)}
      </span>
    </div>
  );
};

interface PlayersViewProps {
  localPlayerIds?: Set<string>;
  initialInspectedPlayer?: { id: string; name?: string } | null;
  onClearInitialPlayer?: () => void;
}

export const PlayersView: React.FC<PlayersViewProps> = ({
  localPlayerIds,
  initialInspectedPlayer,
  onClearInitialPlayer,
}) => {
  const { statMode, pinnedPlayerIds, togglePinPlayer } = useDashboard();
  const isAvg = statMode === 'avg';

  const {
    query,
    setQuery,
    platform,
    setPlatform,
    role,
    setRole,
    playlist,
    setPlaylist,
    sort,
    order,
    toggleSort,
    page,
    setPage,
    players,
    spotlights: serverSpotlights,
    totalPages,
    isLoading,
    error,
  } = usePlayerSearch();

  const [selectedFullPagePlayer, setSelectedFullPagePlayer] = useState<{ id: string; name?: string } | null>(
    initialInspectedPlayer ?? null,
  );
  const [playlistModeFilter, setPlaylistModeFilter] = useState<'all' | '1v1' | '2v2' | '3v3'>('all');

  useEffect(() => {
    if (initialInspectedPlayer) {
      setSelectedFullPagePlayer(initialInspectedPlayer);
      onClearInitialPlayer?.();
    }
  }, [initialInspectedPlayer, onClearInitialPlayer]);

  const playlistIdNum = playlist === 'all' ? 0 : Number(playlist) || 0;

  const handlePlaylistModeChange = (mode: 'all' | '1v1' | '2v2' | '3v3') => {
    setPlaylistModeFilter(mode);
    const id = PLAYLIST_ID_MAP[mode] || 0;
    setPlaylist(id > 0 ? String(id) : 'all');
  };

  const spots = useMemo(() => {
    if (serverSpotlights?.best_teammate || serverSpotlights?.most_faced || serverSpotlights?.toughest_rival) {
      return [
        {
          key: 'mate' as const,
          label: 'BEST TEAMMATE',
          player: serverSpotlights.best_teammate?.player ?? null,
          value: serverSpotlights.best_teammate?.value ?? '—',
          sub: serverSpotlights.best_teammate?.sub ?? 'NOT ENOUGH GAMES YET',
          tone: (serverSpotlights.best_teammate?.tone as 'win' | 'neutral' | 'loss') ?? 'win',
        },
        {
          key: 'faced' as const,
          label: 'MOST FACED',
          player: serverSpotlights.most_faced?.player ?? null,
          value: serverSpotlights.most_faced?.value ?? '—',
          sub: serverSpotlights.most_faced?.sub ?? 'NOT ENOUGH GAMES YET',
          tone: (serverSpotlights.most_faced?.tone as 'win' | 'neutral' | 'loss') ?? 'neutral',
        },
        {
          key: 'rival' as const,
          label: 'TOUGHEST RIVAL',
          player: serverSpotlights.toughest_rival?.player ?? null,
          value: serverSpotlights.toughest_rival?.value ?? '—',
          sub: serverSpotlights.toughest_rival?.sub ?? 'NOT ENOUGH GAMES YET',
          tone: (serverSpotlights.toughest_rival?.tone as 'win' | 'neutral' | 'loss') ?? 'loss',
        },
      ];
    }
    return spotlights(players);
  }, [serverSpotlights, players]);

  // Resolve up to 3 pinned cards (using user-pinned IDs first, falling back to non-duplicate spotlights so all 3 slots are populated)
  const pinnedSlots = useMemo(() => {
    const matchedPinned = pinnedPlayerIds
      .map((id) => players.find((p) => p.player_id === id || p.player_name.toLowerCase() === id.toLowerCase()))
      .filter((p): p is PlayerSummary => Boolean(p));

    const usedIds = new Set<string>(matchedPinned.map((p) => p.player_id));
    const fallbackSpotPlayers = spots
      .map((s) => s?.player ?? null)
      .filter((p): p is PlayerSummary => Boolean(p && !usedIds.has(p.player_id)));

    let fallbackCursor = 0;
    return [0, 1, 2].map((idx) => {
      const spot = spots[idx];
      let player: PlayerSummary | null = null;
      if (idx < matchedPinned.length) {
        player = matchedPinned[idx];
      } else if (spot?.player && !usedIds.has(spot.player.player_id)) {
        player = spot.player;
        usedIds.add(player.player_id);
      } else {
        while (fallbackCursor < fallbackSpotPlayers.length && usedIds.has(fallbackSpotPlayers[fallbackCursor].player_id)) {
          fallbackCursor++;
        }
        player = fallbackSpotPlayers[fallbackCursor] ?? null;
        if (player) usedIds.add(player.player_id);
      }
      const isPinned = player ? pinnedPlayerIds.includes(player.player_id) : false;
      return { idx, player, spot, isPinned };
    });
  }, [pinnedPlayerIds, players, spots]);

  const rows = useMemo(
    () =>
      players
        .filter((p) => {
          if (playlistIdNum > 0 && p.total_matches === 0) return false;
          return matchesRole(p, role);
        })
        .sort((a, b) => {
          if (sort === 'player_name') {
            const comp = (a.player_name || '').localeCompare(b.player_name || '');
            return order === 'asc' ? comp : -comp;
          }
          if (sort === 'platform') {
            const comp = (a.platform || '').localeCompare(b.platform || '');
            return order === 'asc' ? comp : -comp;
          }
          if (sort === 'mmr') {
            const rankA =
              playlistIdNum > 0
                ? parseRanks(a.ranks_json).find((r) => r.playlist_id === playlistIdNum) || primaryRank(a.ranks_json)
                : primaryRank(a.ranks_json);
            const rankB =
              playlistIdNum > 0
                ? parseRanks(b.ranks_json).find((r) => r.playlist_id === playlistIdNum) || primaryRank(b.ranks_json)
                : primaryRank(b.ranks_json);
            const mA = rankA?.mmr ?? 0;
            const mB = rankB?.mmr ?? 0;
            return order === 'asc' ? mA - mB : mB - mA;
          }
          if (sort === 'total_matches') {
            return order === 'asc' ? a.total_matches - b.total_matches : b.total_matches - a.total_matches;
          }
          if (sort === 'win_rate') {
            const wrA =
              a.total_matches > 0
                ? a.total_wins_as_teammate / (a.total_wins_as_teammate + a.total_losses_as_teammate || 1)
                : 0;
            const wrB =
              b.total_matches > 0
                ? b.total_wins_as_teammate / (b.total_wins_as_teammate + b.total_losses_as_teammate || 1)
                : 0;
            return order === 'asc' ? wrA - wrB : wrB - wrA;
          }
          if (sort === 'vs_win_rate' || sort === 'against_wr') {
            const wrA =
              a.total_matches > 0
                ? a.total_wins_as_opponent / (a.total_wins_as_opponent + a.total_losses_as_opponent || 1)
                : 0;
            const wrB =
              b.total_matches > 0
                ? b.total_wins_as_opponent / (b.total_wins_as_opponent + b.total_losses_as_opponent || 1)
                : 0;
            return order === 'asc' ? wrA - wrB : wrB - wrA;
          }
          if (sort === 'with_pts') {
            const wGamesA = a.total_wins_as_teammate + a.total_losses_as_teammate;
            const wGamesB = b.total_wins_as_teammate + b.total_losses_as_teammate;
            const wStatsA = a.with_you_stats || synthesizeRoleStats(a.total_wins_as_teammate, a.total_losses_as_teammate, a.player_id);
            const wStatsB = b.with_you_stats || synthesizeRoleStats(b.total_wins_as_teammate, b.total_losses_as_teammate, b.player_id);
            const ptsA = wGamesA > 0 ? (isAvg ? wStatsA.avg_score : (wStatsA.score ?? wStatsA.total_score ?? 0)) : 0;
            const ptsB = wGamesB > 0 ? (isAvg ? wStatsB.avg_score : (wStatsB.score ?? wStatsB.total_score ?? 0)) : 0;
            return order === 'asc' ? ptsA - ptsB : ptsB - ptsA;
          }
          if (sort === 'vs_pts') {
            const aGamesA = a.total_wins_as_opponent + a.total_losses_as_opponent;
            const aGamesB = b.total_wins_as_opponent + b.total_losses_as_opponent;
            const aStatsA = a.against_you_stats || synthesizeRoleStats(a.total_wins_as_opponent, a.total_losses_as_opponent, `${a.player_id}-vs`);
            const aStatsB = b.against_you_stats || synthesizeRoleStats(b.total_wins_as_opponent, b.total_losses_as_opponent, `${b.player_id}-vs`);
            const ptsA = aGamesA > 0 ? (isAvg ? aStatsA.avg_score : (aStatsA.score ?? aStatsA.total_score ?? 0)) : 0;
            const ptsB = aGamesB > 0 ? (isAvg ? aStatsB.avg_score : (aStatsB.score ?? aStatsB.total_score ?? 0)) : 0;
            return order === 'asc' ? ptsA - ptsB : ptsB - ptsA;
          }
          const timeA = new Date(a.last_seen_at).getTime();
          const timeB = new Date(b.last_seen_at).getTime();
          return order === 'asc' ? timeA - timeB : timeB - timeA;
        }),
    [players, playlistIdNum, role, sort, order, isAvg],
  );

  const sortIndicator = (field: PlayerSortField) => {
    if (sort !== field) return null;
    return <span className="ml-1 text-ink">{order === 'asc' ? '▲' : '▼'}</span>;
  };

  if (selectedFullPagePlayer) {
    return (
      <div data-testid="players-view" className="font-display">
        <PlayerProfilePage
          playerId={selectedFullPagePlayer.id}
          playerName={selectedFullPagePlayer.name}
          onBack={() => setSelectedFullPagePlayer(null)}
          localPlayerIds={localPlayerIds}
        />
      </div>
    );
  }

  return (
    <div data-testid="players-view" className="font-display">
      <PageHeader kicker="EVERYONE YOU'VE PLAYED WITH OR AGAINST" title="PLAYERS" />

      {/* Up to 3 Pinned / Spotlight Player Cards */}
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
            onTogglePin={togglePinPlayer}
            onOpen={(p) => setSelectedFullPagePlayer({ id: p.player_id, name: p.player_name })}
          />
        ))}
      </div>

      {/* Filter & Search Toolbar */}
      <div className="t-bg-panel bg-panel t-border border border-line p-3.5 mb-4 flex flex-wrap gap-3 items-center justify-between">
        <SearchInput
          id="player-search"
          aria-label="Search players"
          placeholder="SEARCH NAME OR PLAYER ID..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="flex flex-wrap items-center gap-3">
          <Segmented
            label="Filter by playlist"
            options={PLAYLIST_OPTIONS}
            value={playlistModeFilter}
            onChange={handlePlaylistModeChange}
          />
          <Segmented label="Filter by role" options={ROLE_OPTIONS} value={role} onChange={setRole} />
          <Segmented
            label="Filter by platform"
            options={PLATFORM_OPTIONS}
            value={platform as (typeof PLATFORM_OPTIONS)[number][0]}
            onChange={setPlatform}
          />
        </div>
      </div>

      {error ? (
        <EmptyLine tone="loss">COULD NOT LOAD PLAYERS · {error}</EmptyLine>
      ) : isLoading && players.length === 0 ? (
        <EmptyLine>SEARCHING PLAYERS…</EmptyLine>
      ) : rows.length === 0 ? (
        <EmptyLine>NO PLAYERS FOUND</EmptyLine>
      ) : (
        <>
          {/* 11-Column Sortable Directory Header */}
          <div
            id="players-table-header"
            className={`grid items-center gap-2.5 px-4 pb-2 ${COLUMN_HEADER}`}
            style={{ gridTemplateColumns: ROW_GRID }}
          >
            <span>★</span>
            <button
              type="button"
              onClick={() => toggleSort('player_name')}
              className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
            >
              PLAYER {sortIndicator('player_name')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('platform')}
              className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
            >
              PLAT {sortIndicator('platform')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('mmr')}
              className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
            >
              MMR {sortIndicator('mmr')}
            </button>
            <span className="whitespace-nowrap truncate">RANK · DIV</span>
            <button
              type="button"
              onClick={() => toggleSort('win_rate')}
              className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
            >
              WITH YOU {sortIndicator('win_rate')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('vs_win_rate')}
              className="text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer uppercase truncate"
            >
              AGAINST {sortIndicator('vs_win_rate')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('total_matches')}
              className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
            >
              GAMES {sortIndicator('total_matches')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('with_pts')}
              className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
            >
              {isAvg ? 'WITH PTS/G' : 'WITH PTS'} {sortIndicator('with_pts')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('vs_pts')}
              className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer uppercase truncate"
            >
              {isAvg ? 'VS PTS/G' : 'VS PTS'} {sortIndicator('vs_pts')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('last_seen')}
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
                playlistScopeId={playlistIdNum}
                onTogglePin={togglePinPlayer}
                onOpen={() => setSelectedFullPagePlayer({ id: p.player_id, name: p.player_name })}
              />
            ))}
          </div>
        </>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-6 pt-6 font-semibold text-sm tracking-t10 text-muted">
          <GhostButton onClick={() => setPage(Math.max(1, page - 1))} disabled={page <= 1}>
            PREV
          </GhostButton>
          <span>
            PAGE <span className="text-ink">{page}</span> OF <span className="text-ink">{totalPages}</span>
          </span>
          <GhostButton onClick={() => setPage(Math.min(totalPages, page + 1))} disabled={page >= totalPages}>
            NEXT
          </GhostButton>
        </div>
      )}
    </div>
  );
};
