import React, { useEffect, useMemo, useState } from 'react';
import { PlayerSummary } from '../../types/api';
import { usePlayerSearch } from '../../hooks/usePlayerSearch';
import { emptyRoleStats, hasSyncedRoleTelemetry } from '../../utils/broadcast';
import { EmptyLine, GhostButton, PageHeader, SearchInput, Segmented } from '../common/ui';
import { PlayerProfilePage } from './PlayerProfilePage';
import { PlayersSpotlightGrid } from './PlayersSpotlightGrid';
import { PlayersDirectoryTable } from './PlayersDirectoryTable';
import { matchesRole, parseRanks, primaryRank, spotlights } from './playerData';
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

interface PlayersViewProps {
  localPlayerIds?: Set<string>;
  initialInspectedPlayer?: { id: string; name?: string } | null;
  inspectedPlayer?: { id: string; name?: string } | null;
  onInspectPlayerChange?: (player: { id: string; name?: string } | null) => void;
  onClearInitialPlayer?: () => void;
}

export const PlayersView: React.FC<PlayersViewProps> = ({
  localPlayerIds,
  initialInspectedPlayer,
  inspectedPlayer,
  onInspectPlayerChange,
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

  const [selectedFullPagePlayer, setSelectedFullPagePlayer] = useState<{
    id: string;
    name?: string;
  } | null>(inspectedPlayer ?? initialInspectedPlayer ?? null);
  const [playlistModeFilter, setPlaylistModeFilter] = useState<'all' | '1v1' | '2v2' | '3v3'>('all');

  useEffect(() => {
    if (inspectedPlayer !== undefined) {
      setSelectedFullPagePlayer(inspectedPlayer);
      return;
    }
    if (initialInspectedPlayer) {
      setSelectedFullPagePlayer(initialInspectedPlayer);
      onClearInitialPlayer?.();
    }
  }, [inspectedPlayer, initialInspectedPlayer, onClearInitialPlayer]);

  const handleSelectPlayer = (player: { id: string; name?: string } | null) => {
    setSelectedFullPagePlayer(player);
    onInspectPlayerChange?.(player);
  };

  const playlistIdNum = playlist === 'all' ? 0 : Number(playlist) || 0;

  const handlePlaylistModeChange = (mode: 'all' | '1v1' | '2v2' | '3v3') => {
    setPlaylistModeFilter(mode);
    const id = PLAYLIST_ID_MAP[mode] || 0;
    setPlaylist(id > 0 ? String(id) : 'all');
  };

  const spots = useMemo(() => {
    if (
      serverSpotlights?.best_teammate ||
      serverSpotlights?.most_faced ||
      serverSpotlights?.toughest_rival
    ) {
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

  const pinnedSlots = useMemo(() => {
    const matchedPinned = pinnedPlayerIds
      .map((id) =>
        players.find(
          (p) =>
            p.player_id === id || p.player_name.toLowerCase() === id.toLowerCase(),
        ),
      )
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
        while (
          fallbackCursor < fallbackSpotPlayers.length &&
          usedIds.has(fallbackSpotPlayers[fallbackCursor].player_id)
        ) {
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
                ? parseRanks(a.ranks_json).find((r) => r.playlist_id === playlistIdNum) ||
                  primaryRank(a.ranks_json)
                : primaryRank(a.ranks_json);
            const rankB =
              playlistIdNum > 0
                ? parseRanks(b.ranks_json).find((r) => r.playlist_id === playlistIdNum) ||
                  primaryRank(b.ranks_json)
                : primaryRank(b.ranks_json);
            const mA = rankA?.mmr ?? 0;
            const mB = rankB?.mmr ?? 0;
            return order === 'asc' ? mA - mB : mB - mA;
          }
          if (sort === 'total_matches') {
            return order === 'asc'
              ? a.total_matches - b.total_matches
              : b.total_matches - a.total_matches;
          }
          if (sort === 'win_rate') {
            const wrA =
              a.total_matches > 0
                ? a.total_wins_as_teammate /
                  (a.total_wins_as_teammate + a.total_losses_as_teammate || 1)
                : 0;
            const wrB =
              b.total_matches > 0
                ? b.total_wins_as_teammate /
                  (b.total_wins_as_teammate + b.total_losses_as_teammate || 1)
                : 0;
            return order === 'asc' ? wrA - wrB : wrB - wrA;
          }
          if (sort === 'vs_win_rate' || sort === 'against_wr') {
            const wrA =
              a.total_matches > 0
                ? a.total_wins_as_opponent /
                  (a.total_wins_as_opponent + a.total_losses_as_opponent || 1)
                : 0;
            const wrB =
              b.total_matches > 0
                ? b.total_wins_as_opponent /
                  (b.total_wins_as_opponent + b.total_losses_as_opponent || 1)
                : 0;
            return order === 'asc' ? wrA - wrB : wrB - wrA;
          }
          if (sort === 'with_pts') {
            const wGamesA = a.total_wins_as_teammate + a.total_losses_as_teammate;
            const wGamesB = b.total_wins_as_teammate + b.total_losses_as_teammate;
            const wStatsA =
              a.with_you_stats ||
              emptyRoleStats(wGamesA, a.total_wins_as_teammate, a.total_losses_as_teammate);
            const wStatsB =
              b.with_you_stats ||
              emptyRoleStats(wGamesB, b.total_wins_as_teammate, b.total_losses_as_teammate);
            const ptsA =
              wGamesA > 0 && hasSyncedRoleTelemetry(wStatsA)
                ? isAvg
                  ? wStatsA.avg_score
                  : wStatsA.score || wStatsA.total_score || 0
                : 0;
            const ptsB =
              wGamesB > 0 && hasSyncedRoleTelemetry(wStatsB)
                ? isAvg
                  ? wStatsB.avg_score
                  : wStatsB.score || wStatsB.total_score || 0
                : 0;
            return order === 'asc' ? ptsA - ptsB : ptsB - ptsA;
          }
          if (sort === 'vs_pts') {
            const aGamesA = a.total_wins_as_opponent + a.total_losses_as_opponent;
            const aGamesB = b.total_wins_as_opponent + b.total_losses_as_opponent;
            const aStatsA =
              a.against_you_stats ||
              emptyRoleStats(aGamesA, a.total_wins_as_opponent, a.total_losses_as_opponent);
            const aStatsB =
              b.against_you_stats ||
              emptyRoleStats(aGamesB, b.total_wins_as_opponent, b.total_losses_as_opponent);
            const ptsA =
              aGamesA > 0 && hasSyncedRoleTelemetry(aStatsA)
                ? isAvg
                  ? aStatsA.avg_score
                  : aStatsA.score || aStatsA.total_score || 0
                : 0;
            const ptsB =
              aGamesB > 0 && hasSyncedRoleTelemetry(aStatsB)
                ? isAvg
                  ? aStatsB.avg_score
                  : aStatsB.score || aStatsB.total_score || 0
                : 0;
            return order === 'asc' ? ptsA - ptsB : ptsB - ptsA;
          }
          const timeA = new Date(a.last_seen_at).getTime();
          const timeB = new Date(b.last_seen_at).getTime();
          return order === 'asc' ? timeA - timeB : timeB - timeA;
        }),
    [players, playlistIdNum, role, sort, order, isAvg],
  );

  if (selectedFullPagePlayer) {
    return (
      <div data-testid="players-view" className="font-display">
        <PlayerProfilePage
          playerId={selectedFullPagePlayer.id}
          playerName={selectedFullPagePlayer.name}
          onBack={() => handleSelectPlayer(null)}
          localPlayerIds={localPlayerIds}
          onOpenFullProfile={(id, name) => handleSelectPlayer({ id, name })}
        />
      </div>
    );
  }

  return (
    <div data-testid="players-view" className="font-display">
      <PageHeader
        kicker="EVERYONE YOU'VE PLAYED WITH OR AGAINST"
        title="PLAYERS"
        right={
          <button
            type="button"
            data-testid="view-own-profile-btn"
            onClick={() => {
              const firstLocal =
                localPlayerIds && localPlayerIds.size > 0
                  ? Array.from(localPlayerIds)[0]
                  : 'me';
              handleSelectPlayer({ id: firstLocal, name: 'You' });
            }}
            className="px-4 py-2 t-bg-blue-slab bg-blue-slab text-white font-display font-extrabold text-sm uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
          >
            VIEW MY PROFILE →
          </button>
        }
      />

      <PlayersSpotlightGrid
        pinnedSlots={pinnedSlots}
        isAvg={isAvg}
        onTogglePin={togglePinPlayer}
        onOpenPlayer={(p) =>
          handleSelectPlayer({ id: p.player_id, name: p.player_name })
        }
      />

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
          <Segmented
            label="Filter by role"
            options={ROLE_OPTIONS}
            value={role}
            onChange={setRole}
          />
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
        <PlayersDirectoryTable
          rows={rows}
          pinnedPlayerIds={pinnedPlayerIds}
          isAvg={isAvg}
          playlistScopeId={playlistIdNum}
          sort={sort}
          order={order}
          onToggleSort={toggleSort}
          onTogglePin={togglePinPlayer}
          onSelectPlayer={handleSelectPlayer}
        />
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-6 pt-6 font-semibold text-sm tracking-t10 text-muted">
          <GhostButton
            onClick={() => setPage(Math.max(1, page - 1))}
            disabled={page <= 1}
          >
            PREV
          </GhostButton>
          <span>
            PAGE <span className="text-ink">{page}</span> OF{' '}
            <span className="text-ink">{totalPages}</span>
          </span>
          <GhostButton
            onClick={() => setPage(Math.min(totalPages, page + 1))}
            disabled={page >= totalPages}
          >
            NEXT
          </GhostButton>
        </div>
      )}
    </div>
  );
};
