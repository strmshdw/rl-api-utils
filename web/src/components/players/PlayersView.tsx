import React, { useMemo, useState } from 'react';
import { PlayerSummary } from '../../types/api';
import { usePlayerSearch, PlayerSortField } from '../../hooks/usePlayerSearch';
import { formatAgo, formatMmr, formatPct, formatRecord, platformLabel, recordInk } from '../../utils/broadcast';
import { EmptyLine, GhostButton, PageHeader, SearchInput, Segmented, COLUMN_HEADER } from '../common/ui';
import { PlayerProfileDialog } from './PlayerProfileDialog';
import { Spotlight, matchesRole, primaryRank, spotlights } from './playerData';

const ROLE_OPTIONS = [
  ['all', 'ALL'],
  ['frequent', 'FREQUENT'],
  ['teammate', 'TEAMMATES'],
  ['opponent', 'OPPONENTS'],
  ['synergy', 'SYNERGY ≥50%'],
] as const;

const PLATFORM_OPTIONS = [
  ['all', 'ALL'],
  ['steam', 'STEAM'],
  ['epic', 'EPIC'],
  ['playstation', 'PSN'],
  ['xbox', 'XBOX'],
] as const;

const ROW_GRID = 'minmax(0,1.4fr) 110px minmax(0,1.2fr) 68px 120px 120px 72px 130px';

const RecordCell: React.FC<{ wins: number; losses: number }> = ({ wins, losses }) => {
  const games = wins + losses;
  return (
    <div className="flex flex-col pl-4">
      <span className={`font-bold text-[20px] ${recordInk(wins, losses)}`}>
        {formatRecord(wins, losses)}
      </span>
      {games > 0 && <span className="font-semibold text-[13px] text-muted">{formatPct(wins, losses)}</span>}
    </div>
  );
};

const SpotlightCard: React.FC<{ spot: Spotlight; onOpen: (p: PlayerSummary) => void }> = ({ spot, onOpen }) => {
  const p = spot.player;
  return (
    <div
      data-testid={`spotlight-${spot.key}`}
      onClick={() => p && onOpen(p)}
      className={`bg-panel px-7 py-5 flex flex-col gap-2 min-w-0 transition-colors duration-120 ${
        p ? 'hover:bg-panel-hover cursor-pointer' : 'cursor-default'
      }`}
    >
      <div className="flex justify-between items-baseline gap-2">
        <span className="font-semibold text-[13px] tracking-t12 text-muted uppercase truncate">{spot.label}</span>
        {p && (
          <span className="font-semibold text-[12px] tracking-t10 text-muted uppercase shrink-0">
            {platformLabel(p.platform || p.player_id)}
          </span>
        )}
      </div>
      <div className="flex items-baseline justify-between gap-4">
        <span title={p?.player_name} className="font-extrabold text-[34px] tracking-t3 uppercase leading-none truncate">
          {p?.player_name || '—'}
        </span>
        <span className={`font-extrabold text-[34px] tracking-t3 leading-none shrink-0 ${spot.tone === 'win' ? 'text-win' : spot.tone === 'loss' ? 'text-loss' : 'text-ink-strong'}`}>
          {spot.value}
        </span>
      </div>
      <div className="font-semibold text-[13px] tracking-t10 text-muted truncate">{spot.sub}</div>
    </div>
  );
};

const DirectoryRow: React.FC<{ player: PlayerSummary; onOpen: () => void }> = ({ player, onOpen }) => {
  const rank = primaryRank(player.ranks_json);
  const size = rank?.matches_played ? `${rank.matches_played}G` : '';
  const mate = { wins: player.total_wins_as_teammate, losses: player.total_losses_as_teammate };
  const vs = { wins: player.total_wins_as_opponent, losses: player.total_losses_as_opponent };

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
      className="grid items-center h-[60px] bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 outline-none focus-visible:bg-panel-hover"
      style={{ gridTemplateColumns: ROW_GRID }}
    >
      <span title={player.player_name} className="pl-[22px] font-bold text-[22px] tracking-t3 uppercase truncate">
        {player.player_name || 'UNKNOWN'}
      </span>
      <span className="font-semibold text-[14px] tracking-t10 text-muted">{platformLabel(player.platform || player.player_id)}</span>
      <span className="font-semibold text-[16px] tracking-t6 text-ink-2 uppercase truncate pr-2">
        {rank ? rank.rank_name || 'UNRANKED' : '—'}
        {size && <span className="text-muted"> · {size}</span>}
      </span>
      <span className="text-right font-bold text-[20px] text-ink-2">{formatMmr(rank?.mmr)}</span>
      <RecordCell wins={mate.wins} losses={mate.losses} />
      <RecordCell wins={vs.wins} losses={vs.losses} />
      <span className="text-right font-bold text-[20px]">{player.total_matches}</span>
      <span className="text-right pr-[22px] font-semibold text-[14px] tracking-t8 text-muted whitespace-nowrap">{formatAgo(player.last_seen_at)}</span>
    </div>
  );
};

export const PlayersView: React.FC = () => {
  const {
    query,
    setQuery,
    platform,
    setPlatform,
    role,
    setRole,
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

  const [selected, setSelected] = useState<PlayerSummary | null>(null);

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

  const rows = useMemo(
    () =>
      players
        .filter((p) => matchesRole(p, role))
        .sort((a, b) => {
          if (sort === 'player_name') {
            const comp = (a.player_name || '').localeCompare(b.player_name || '');
            return order === 'asc' ? comp : -comp;
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
          const timeA = new Date(a.last_seen_at).getTime();
          const timeB = new Date(b.last_seen_at).getTime();
          return order === 'asc' ? timeA - timeB : timeB - timeA;
        }),
    [players, role, sort, order],
  );

  const sortIndicator = (field: PlayerSortField) => {
    if (sort !== field) return null;
    return <span className="ml-1 text-ink">{order === 'asc' ? '▲' : '▼'}</span>;
  };

  return (
    <div data-testid="players-view" className="font-display">
      <PageHeader kicker="EVERYONE YOU'VE PLAYED WITH OR AGAINST" title="PLAYERS" />

      <div className="grid grid-cols-3 gap-3 pb-7">
        {spots.map((s) => (
          <SpotlightCard key={s.key} spot={s} onOpen={setSelected} />
        ))}
      </div>

      <div className="flex flex-wrap gap-3 items-stretch pb-4">
        <SearchInput
          aria-label="Search players"
          placeholder="SEARCH NAME OR PLAYER ID"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <Segmented label="Filter by role" options={ROLE_OPTIONS} value={role} onChange={setRole} />
        <Segmented
          label="Filter by platform"
          options={PLATFORM_OPTIONS}
          value={platform as (typeof PLATFORM_OPTIONS)[number][0]}
          onChange={setPlatform}
        />
      </div>

      {error ? (
        <EmptyLine tone="loss">COULD NOT LOAD PLAYERS · {error}</EmptyLine>
      ) : isLoading && players.length === 0 ? (
        <EmptyLine>SEARCHING PLAYERS…</EmptyLine>
      ) : rows.length === 0 ? (
        <EmptyLine>NO PLAYERS FOUND</EmptyLine>
      ) : (
        <>
          <div className={`grid pb-2 ${COLUMN_HEADER}`} style={{ gridTemplateColumns: ROW_GRID }}>
            <button
              type="button"
              onClick={() => toggleSort('player_name')}
              className="pl-[22px] text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer"
            >
              PLAYER {sortIndicator('player_name')}
            </button>
            <span>PLATFORM</span>
            <span className="whitespace-nowrap">RANK · 2V2</span>
            <span className="text-right">MMR</span>
            <button
              type="button"
              onClick={() => toggleSort('win_rate')}
              className="pl-4 text-left hover:text-ink transition-colors flex items-center outline-none cursor-pointer"
            >
              WITH YOU {sortIndicator('win_rate')}
            </button>
            <span className="pl-4">VS YOU</span>
            <button
              type="button"
              onClick={() => toggleSort('total_matches')}
              className="text-right hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer"
            >
              GAMES {sortIndicator('total_matches')}
            </button>
            <button
              type="button"
              onClick={() => toggleSort('last_seen')}
              className="text-right pr-[22px] hover:text-ink transition-colors flex items-center justify-end outline-none cursor-pointer"
            >
              LAST SEEN {sortIndicator('last_seen')}
            </button>
          </div>
          <div className="flex flex-col gap-1.5">
            {rows.map((p) => (
              <DirectoryRow key={p.player_id} player={p} onOpen={() => setSelected(p)} />
            ))}
          </div>
        </>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-6 pt-6 font-semibold text-[14px] tracking-t10 text-muted">
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

      <PlayerProfileDialog
        playerId={selected?.player_id ?? null}
        playerName={selected?.player_name}
        onClose={() => setSelected(null)}
      />
    </div>
  );
};
