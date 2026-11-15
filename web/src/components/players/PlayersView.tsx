import React, { useEffect, useMemo, useState } from 'react';
import { CurrentMatchResponse, PlayerSummary } from '../../types/api';
import { usePlayerSearch } from '../../hooks/usePlayerSearch';
import { EmptyLine, GhostButton, PageHeader, SearchInput, Segmented } from '../common/ui';
import { PlayerProfilePage } from './PlayerProfilePage';
import { PlayersSpotlightGrid } from './PlayersSpotlightGrid';
import { PlayersDirectoryTable } from './PlayersDirectoryTable';
import { isSoloDuelPlaylist, matchesRole, spotlights } from './playerData';
import { useDashboard } from '../../context/DashboardContext';

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
  liveMatch?: CurrentMatchResponse | null;
  initialInspectedPlayer?: { id: string; name?: string } | null;
  inspectedPlayer?: { id: string; name?: string } | null;
  onInspectPlayerChange?: (player: { id: string; name?: string } | null) => void;
  onClearInitialPlayer?: () => void;
}

export const PlayersView: React.FC<PlayersViewProps> = ({
  localPlayerIds,
  liveMatch,
  initialInspectedPlayer,
  inspectedPlayer,
  onInspectPlayerChange,
  onClearInitialPlayer,
}) => {
  const { statMode, activePlaylist, pinnedPlayerIds, togglePinPlayer } = useDashboard();
  const isAvg = statMode === 'avg';

  const {
    query,
    setQuery,
    platform,
    setPlatform,
    role,
    setRole,
    playlist,
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

  const soloDuel = isSoloDuelPlaylist(activePlaylist);
  const visibleRoleOptions = useMemo(
    () =>
      soloDuel
        ? ROLE_OPTIONS.filter(([key]) => key !== 'teammate' && key !== 'synergy')
        : ROLE_OPTIONS,
    [soloDuel],
  );

  useEffect(() => {
    if (soloDuel && (role === 'teammate' || role === 'synergy')) {
      setRole('all');
    }
  }, [soloDuel, role, setRole]);

  const spots = useMemo(() => {
    if (
      serverSpotlights?.best_teammate ||
      serverSpotlights?.most_faced ||
      serverSpotlights?.toughest_rival
    ) {
      return [
        soloDuel
          ? {
              key: 'mate' as const,
              label: 'SOLO DUEL MODE',
              player: null,
              value: '1V1',
              sub: 'NO TEAMMATES IN 1V1',
              tone: 'neutral' as const,
            }
          : {
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
    return spotlights(players, activePlaylist);
  }, [serverSpotlights, players, activePlaylist, soloDuel]);

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
      } else if (soloDuel && spot?.key === 'mate') {
        player = null;
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
  }, [pinnedPlayerIds, players, spots, soloDuel]);

  const rows = useMemo(
    () =>
      players.filter((p) => {
        if (playlist !== 'all' && p.total_matches === 0) return false;
        return matchesRole(p, role);
      }),
    [players, playlist, role],
  );

  if (selectedFullPagePlayer) {
    return (
      <div data-testid="players-view" className="font-display">
        <PlayerProfilePage
          playerId={selectedFullPagePlayer.id}
          playerName={selectedFullPagePlayer.name}
          onBack={() => handleSelectPlayer(null)}
          localPlayerIds={localPlayerIds}
          liveMatch={liveMatch}
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
        activePlaylist={activePlaylist}
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
            label="Filter by role"
            options={visibleRoleOptions}
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
          activePlaylist={activePlaylist}
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
