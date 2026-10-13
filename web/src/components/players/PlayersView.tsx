import React, { useMemo, useState } from 'react';
import { usePlayerSearch } from '../../hooks/usePlayerSearch';
import { PlayerSummary } from '../../types/api';
import { formatAgo, formatMmr, formatRecord, platformLabel, playlistLabels, recordInk } from '../../utils/broadcast';
import { COLUMN_HEADER, EmptyLine, GhostButton, PANEL_LABEL, PageHeader, RecordCells, SearchInput, Segmented } from '../common/ui';
import { PlayerProfileDialog } from './PlayerProfileDialog';
import { RoleFilter, Spotlight, matchesRole, primaryRank, spotlights, vsYou, withYou } from './playerData';

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

const ROW_GRID = 'minmax(140px,1.4fr) 90px minmax(0,1fr) 64px 130px 130px 56px 100px';

const TONE = {
  win: { rule: 'border-win', ink: 'text-win' },
  neutral: { rule: 'border-ink-2', ink: 'text-ink-2' },
  loss: { rule: 'border-loss', ink: 'text-loss' },
} as const;

const SpotlightCard: React.FC<{ spot: Spotlight; onOpen: (p: PlayerSummary) => void }> = ({ spot, onOpen }) => {
  const tone = TONE[spot.tone];
  const content = (
    <>
      <div className="flex flex-col gap-1 min-w-0">
        <span className={PANEL_LABEL}>{spot.label}</span>
        <span className="font-bold text-[30px] tracking-t2 uppercase leading-none truncate">{spot.player?.player_name || '—'}</span>
        <span className="font-medium text-[13px] tracking-t8 text-muted">{spot.sub}</span>
      </div>
      <span className={`font-extrabold text-[40px] leading-none shrink-0 ${spot.player ? tone.ink : 'text-faint'}`}>{spot.value}</span>
    </>
  );
  const className = `bg-panel border-t-4 ${tone.rule} px-[22px] py-[18px] flex justify-between items-end gap-3 text-left min-w-0`;
  if (!spot.player) return <div data-testid={`spotlight-${spot.key}`} className={className}>{content}</div>;
  const player = spot.player;
  return (
    <button
      type="button"
      data-testid={`spotlight-${spot.key}`}
      onClick={() => onOpen(player)}
      className={`${className} hover:bg-panel-hover transition-colors duration-120`}
    >
      {content}
    </button>
  );
};

const RecordCell: React.FC<{ wins: number; losses: number }> = ({ wins, losses }) => (
  <div className="flex items-center gap-2.5 pl-4">
    <RecordCells wins={wins} losses={losses} cellClass="w-2 h-4" />
    <span className={`font-bold text-[20px] ${recordInk(wins, losses)}`}>{formatRecord(wins, losses)}</span>
  </div>
);

const DirectoryRow: React.FC<{ player: PlayerSummary; onOpen: () => void }> = ({ player, onOpen }) => {
  const rank = primaryRank(player.ranks_json);
  const mate = withYou(player);
  const vs = vsYou(player);
  const size = rank && rank.playlist_id !== 11 ? playlistLabels(rank.playlist_id, rank.playlist_name).mode.replace(/^RANKED /, '') : null;

  return (
    <div
      role="button"
      tabIndex={0}
      data-testid="player-row"
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
  const { query, setQuery, platform, setPlatform, page, setPage, players, totalPages, isLoading, error } = usePlayerSearch();
  const [roleFilter, setRoleFilter] = useState<RoleFilter>('all');
  const [selected, setSelected] = useState<PlayerSummary | null>(null);

  const spots = useMemo(() => spotlights(players), [players]);
  const rows = useMemo(
    () =>
      players
        .filter((p) => matchesRole(p, roleFilter))
        .sort((a, b) => new Date(b.last_seen_at).getTime() - new Date(a.last_seen_at).getTime()),
    [players, roleFilter],
  );

  return (
    <div data-testid="players-view" className="font-display">
      <PageHeader kicker="EVERYONE YOU'VE PLAYED WITH OR AGAINST" title="PLAYERS" />

      <div className="grid grid-cols-3 gap-3 pb-7">
        {spots.map((s) => (
          <SpotlightCard key={s.key} spot={s} onOpen={setSelected} />
        ))}
      </div>

      <div className="flex flex-wrap gap-3 items-stretch pb-4">
        <SearchInput aria-label="Search players" placeholder="SEARCH NAME OR PLAYER ID" value={query} onChange={(e) => setQuery(e.target.value)} />
        <Segmented label="Filter by role" options={ROLE_OPTIONS} value={roleFilter} onChange={setRoleFilter} />
        <Segmented label="Filter by platform" options={PLATFORM_OPTIONS} value={platform as (typeof PLATFORM_OPTIONS)[number][0]} onChange={setPlatform} />
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
            <span className="pl-[22px]">PLAYER</span>
            <span>PLATFORM</span>
            <span className="whitespace-nowrap">RANK · 2V2</span>
            <span className="text-right">MMR</span>
            <span className="pl-4">WITH YOU</span>
            <span className="pl-4">VS YOU</span>
            <span className="text-right">GAMES</span>
            <span className="text-right pr-[22px]">LAST SEEN</span>
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
