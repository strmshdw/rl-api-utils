import React from 'react';
import { LobbyPlayer, PlayerRoleStatsSummary, PlayerStatsSummary } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import {
  computeLiveThreatBadges,
  emptyRoleStats,
  formatMmr,
  formatRecord,
  formatSigned,
  hasSyncedRoleTelemetry,
  matchesPlaylistFilter,
  playlistFilterToParam,
  recordInk,
  shortRank,
} from '../../utils/broadcast';
import { PlatformBadge } from '../common/PlatformBadge';
import { RecordCells } from '../common/ui';
import { StatMode, useDashboard } from '../../context/DashboardContext';
import type { ScopedLivePlayerHistory } from '../../hooks/useLiveLobbyHistories';
import { STAT_LABEL_CLASS } from '../../utils/statColors';

export type StatKey = keyof PlayerStatsSummary;
export type StatLeaders = Record<StatKey, number>;

export const STRIP_STATS: ReadonlyArray<{ key: StatKey; label: string; colorClass: string }> = [
  { key: 'score', label: 'SCORE', colorClass: STAT_LABEL_CLASS.score },
  { key: 'goals', label: 'G', colorClass: STAT_LABEL_CLASS.goals },
  { key: 'assists', label: 'A', colorClass: STAT_LABEL_CLASS.assists },
  { key: 'saves', label: 'SV', colorClass: STAT_LABEL_CLASS.saves },
  { key: 'shots', label: 'SH', colorClass: STAT_LABEL_CLASS.shots },
  { key: 'demos', label: 'D', colorClass: STAT_LABEL_CLASS.demos },
];

const EMPTY_STATS: PlayerStatsSummary = { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 };

const HIST_GRID_COLS =
  'grid grid-cols-[minmax(96px,1.95fr)_minmax(34px,0.9fr)_minmax(54px,1.35fr)_repeat(3,minmax(26px,0.82fr))_minmax(38px,1fr)_repeat(2,minmax(26px,0.82fr))] gap-x-1.5 min-w-[430px] px-3 py-1 items-center min-h-[28px]';

const STAMP_PILL_BASE = 'inline-flex items-center leading-tight min-h-[22px] text-[11px] px-2 py-0.5';

/** Colors non-zero stat values to match their attribute color (bolding lobby leaders); zeros stay faint. */
export function statInk(value: number, leader: number, colorClass?: string): string {
  if (!value) return 'text-faint';
  if (colorClass) {
    return value === leader ? `${colorClass} font-black` : `${colorClass} font-bold`;
  }
  return value === leader ? 'text-ink-strong' : 'text-ink-3';
}

export function statLeaders(players: LobbyPlayer[]): StatLeaders {
  const leaders = { ...EMPTY_STATS };
  for (const p of players) {
    for (const { key } of STRIP_STATS) leaders[key] = Math.max(leaders[key], p.stats?.[key] || 0);
  }
  return leaders;
}

export interface LiveCardSubgridPlacement {
  col: number;
  row: string;
  fallbackRows: string;
}

interface LivePlayerCardProps {
  player: LobbyPlayer;
  isLocal: boolean;
  isTeammate: boolean;
  leaders: StatLeaders;
  columns: ColumnConfig;
  /** Playlist-scoped Session W–L, shown on the local player's own card. */
  sessionRecord: { wins: number; losses: number; netMmr?: number };
  localPlayer?: LobbyPlayer | null;
  opposingTeamPlayers?: LobbyPlayer[];
  activePlaylistId?: number;
  playlistScope?: string;
  statMode?: StatMode;
  scopedHistory?: ScopedLivePlayerHistory;
  subgridPlacement?: LiveCardSubgridPlacement;
  onInspectPlayer?: (playerId: string, playerName?: string) => void;
}

export const LivePlayerCard: React.FC<LivePlayerCardProps> = ({
  player,
  isLocal,
  isTeammate,
  leaders,
  columns,
  sessionRecord,
  localPlayer,
  opposingTeamPlayers = [],
  activePlaylistId = 11,
  playlistScope = '11',
  statMode: statModeProp,
  scopedHistory,
  subgridPlacement,
  onInspectPlayer,
}) => {
  const { statMode: contextStatMode, showLiveHistData, stampingConfig } = useDashboard();
  const isAvg = (statModeProp ?? contextStatMode) === 'avg';

  const param = playlistFilterToParam(playlistScope);
  const parsedId = Number(param);
  const scopedPlaylistId = Number.isInteger(parsedId) && parsedId > 0 ? parsedId : 0;
  const nonNumericMismatch =
    scopedPlaylistId === 0 &&
    Boolean(playlistScope) &&
    playlistScope !== 'all' &&
    !matchesPlaylistFilter(activePlaylistId, playlistScope);

  const stats = player.stats || EMPTY_STATS;
  const rank = nonNumericMismatch
    ? undefined
    : scopedPlaylistId > 0
    ? player.ranks?.[String(scopedPlaylistId)] ??
      (player.current_rank?.playlist_id === scopedPlaylistId ? player.current_rank : undefined)
    : player.current_rank;

  const scopedMatchup = nonNumericMismatch
    ? undefined
    : scopedPlaylistId > 0
    ? player.matchups_by_playlist?.[String(scopedPlaylistId)] ??
      (player.matchup_record?.playlist_id === scopedPlaylistId ? player.matchup_record : undefined)
    : player.matchup_record;

  const tw = scopedHistory?.hasDateFilter
    ? scopedHistory.teammateWins
    : scopedMatchup?.wins_as_teammate ?? scopedHistory?.teammateWins ?? 0;
  const tl = scopedHistory?.hasDateFilter
    ? scopedHistory.teammateLosses
    : scopedMatchup?.losses_as_teammate ?? scopedHistory?.teammateLosses ?? 0;
  const ow = scopedHistory?.hasDateFilter
    ? scopedHistory.opponentWins
    : scopedMatchup?.wins_as_opponent ?? scopedHistory?.opponentWins ?? 0;
  const ol = scopedHistory?.hasDateFilter
    ? scopedHistory.opponentLosses
    : scopedMatchup?.losses_as_opponent ?? scopedHistory?.opponentLosses ?? 0;

  let h2hLabel = 'VS YOU';
  let wins = ow;
  let losses = ol;
  if (isLocal) {
    h2hLabel = 'THIS SESSION';
    wins = sessionRecord.wins;
    losses = sessionRecord.losses;
  } else if (isTeammate) {
    h2hLabel = 'WITH YOU';
    wins = tw;
    losses = tl;
  }
  const recordColor = isLocal && wins + losses > 0 ? 'text-ink' : recordInk(wins, losses);

  const meta: React.ReactNode[] = [];
  if (columns.platform) {
    meta.push(
      <PlatformBadge
        key="platform"
        platform={player.platform}
        isBot={player.is_bot}
        size="sm"
      />,
    );
  } else {
    if (columns.rank) meta.push(<span key="rank">{rank?.rank_name || 'UNRANKED'}</span>);
    if (columns.mmr) meta.push(<span key="mmr">{formatMmr(rank?.mmr)}</span>);
  }
  if (player.is_disconnected) {
    meta.push(
      <span
        key="dc"
        data-testid="badge-disconnected"
        className="underline font-extrabold"
        title="Player disconnected early; stats and match outcomes are preserved"
      >
        DISCONNECTED
      </span>,
    );
  }

  const strip = STRIP_STATS.filter((s) => columns[s.key]);

  // Compute Live Threat, Carry, Off-Mode Peak & Streak Badges
  const threatBadges = computeLiveThreatBadges(
    player,
    localPlayer,
    opposingTeamPlayers,
    scopedPlaylistId > 0 ? scopedPlaylistId : activePlaylistId,
    stampingConfig,
  );

  // Build historical split rows using scoped SQLite role telemetry first, falling back to active-mode inline stats
  const canUseLocalRoleStats =
    !nonNumericMismatch && (scopedPlaylistId === 0 || scopedPlaylistId === activePlaylistId);

  const canUsePlayerRoleStats =
    !nonNumericMismatch &&
    (scopedPlaylistId === 0 ||
      (scopedPlaylistId === activePlaylistId &&
        Boolean(scopedMatchup) &&
        scopedMatchup?.playlist_id === activePlaylistId));

  const withStats: PlayerRoleStatsSummary = scopedHistory?.withYouStats
    ? scopedHistory.withYouStats
    : isLocal
    ? canUseLocalRoleStats && player.in_wins_stats
      ? player.in_wins_stats
      : emptyRoleStats(Math.max(0, sessionRecord.wins), sessionRecord.wins, 0)
    : canUsePlayerRoleStats && player.with_you_stats
    ? player.with_you_stats
    : emptyRoleStats(tw + tl, tw, tl);

  const againstStats: PlayerRoleStatsSummary = scopedHistory?.againstYouStats
    ? scopedHistory.againstYouStats
    : isLocal
    ? canUseLocalRoleStats && player.in_losses_stats
      ? player.in_losses_stats
      : emptyRoleStats(Math.max(0, sessionRecord.losses), 0, sessionRecord.losses)
    : canUsePlayerRoleStats && player.against_you_stats
    ? player.against_you_stats
    : emptyRoleStats(ow + ol, ow + ol > 0 ? ow : 0, ow + ol > 0 ? ol : 0);

  const renderHistRow = (
    roleLabel: string,
    roleColor: string,
    s: PlayerRoleStatsSummary,
  ) => {
    if (!s || s.games === 0) {
      return (
        <div
          key={roleLabel}
          className={`${HIST_GRID_COLS} border-t border-line text-xs t-font-mono`}
        >
          <div className="font-bold t-text-muted uppercase whitespace-nowrap">{roleLabel}</div>
          <div className="text-right t-text-faint tabular-nums">0G</div>
          <div className="col-span-7 text-center text-xs t-font-display tracking-wider t-text-muted2 uppercase">
            — NO MATCHES IN SELECTED RANGE —
          </div>
        </div>
      );
    }
    if (!hasSyncedRoleTelemetry(s)) {
      return (
        <div
          key={roleLabel}
          className={`${HIST_GRID_COLS} border-t border-line text-xs t-font-mono opacity-70`}
        >
          <div className={`t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap ${roleColor}`}>
            {roleLabel}
          </div>
          <div className="text-right font-bold t-text-ink2 tabular-nums">{s.games}G</div>
          <div className="col-span-7 text-center text-xs t-font-display tracking-wider t-text-faint">
            No synced role telemetry · —
          </div>
        </div>
      );
    }
    const pts = isAvg ? Math.round(s.avg_score) : Math.round(s.total_score);
    const g = isAvg ? s.avg_goals.toFixed(1) : Math.round(s.total_goals);
    const a = isAvg ? s.avg_assists.toFixed(1) : Math.round(s.total_assists);
    const sv = isAvg ? s.avg_saves.toFixed(1) : Math.round(s.total_saves);
    const shPct = `${Math.round(s.shooting_pct)}%`;
    const dem = isAvg ? s.avg_demos_inflicted.toFixed(1) : Math.round(s.total_demos_inflicted);
    const bst = Math.round(s.avg_boost);

    return (
      <div
        key={roleLabel}
        className={`${HIST_GRID_COLS} border-t border-line text-sm t-font-mono`}
      >
        <div className={`t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap ${roleColor}`}>
          {roleLabel}
        </div>
        <div className={`text-right font-bold tabular-nums ${STAT_LABEL_CLASS.games}`}>{s.games}G</div>
        <div className={`text-right font-bold tabular-nums ${STAT_LABEL_CLASS.score}`}>{pts}</div>
        <div className={`text-right font-bold tabular-nums ${STAT_LABEL_CLASS.goals}`}>{g}</div>
        <div className={`text-right tabular-nums ${STAT_LABEL_CLASS.assists}`}>{a}</div>
        <div className={`text-right tabular-nums ${STAT_LABEL_CLASS.saves}`}>{sv}</div>
        <div className={`text-right tabular-nums ${STAT_LABEL_CLASS.shotpct}`}>{shPct}</div>
        <div className={`text-right tabular-nums ${STAT_LABEL_CLASS.demos}`}>{dem}</div>
        <div className={`text-right tabular-nums ${STAT_LABEL_CLASS.boost}`}>{bst}</div>
      </div>
    );
  };

  const isBlueTeam = player.team_num === 0;
  const subgridStyle = subgridPlacement
    ? ({
        '--live-card-col': String(subgridPlacement.col),
        '--live-card-row': subgridPlacement.row,
        '--live-card-fallback-rows': subgridPlacement.fallbackRows,
      } as React.CSSProperties)
    : undefined;

  return (
    <article
      data-testid="live-player-card"
      data-player-id={player.player_id}
      style={subgridStyle}
      onClick={() => {
        if (onInspectPlayer && !player.is_bot) {
          onInspectPlayer(player.player_id, isLocal ? 'You' : player.name);
        }
      }}
      className={`live-player-subgrid surface-plaque flex flex-col border font-display t-radius overflow-hidden transition-colors min-w-0 ${
        isLocal ? 'border-blue-ink border-outline' : 'border-outline'
      } ${player.is_disconnected ? 'opacity-60' : ''} ${
        onInspectPlayer && !player.is_bot ? 'cursor-pointer t-hover-panel' : ''
      }`}
    >
      {/* Band 1: Full-Bleed Team Monolith Header Slab */}
      <div
        className={`px-4 py-2.5 flex items-center justify-between gap-3 border-b border-line min-w-0 ${
          isBlueTeam ? 't-bg-blue-slab bg-blue-slab t-on-slab' : 't-bg-orange-slab bg-orange-slab text-white'
        }`}
      >
        <div className="flex flex-col min-w-0 flex-1">
          <span className="t-font-mono text-[10px] font-bold tracking-widest uppercase opacity-80">
            {isBlueTeam ? 'BLUE TEAM // PLAYER' : 'ORANGE TEAM // PLAYER'}
          </span>
          <div className="flex items-center gap-2.5 flex-wrap min-w-0">
            <div
              title={player.name}
              className="font-bold text-2xl sm:text-[26px] tracking-wider leading-none uppercase max-w-full min-w-0 truncate"
            >
              {isLocal ? 'You' : player.name}
            </div>
            {meta.length > 0 && (
              <div
                data-testid="player-meta"
                className="inline-flex items-center gap-1.5 t-font-mono font-bold text-[11px] tracking-wider uppercase opacity-90 max-w-full min-w-0 truncate"
              >
                {meta.map((m, i) => (
                  <React.Fragment key={i}>
                    {i > 0 && <span aria-hidden="true">·</span>}
                    {m}
                  </React.Fragment>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Prominent 4-Digit Tabular MMR + Rank on the Right of the Header Slab */}
        <div className="text-right shrink-0">
          <div className="t-font-mono font-extrabold text-xl leading-none">
            {rank?.mmr ? `${formatMmr(rank.mmr)} MMR` : 'UNRANKED'}
          </div>
          {rank && (
            <div className="t-font-display font-bold text-[11px] tracking-widest uppercase opacity-90 mt-0.5">
              {shortRank(rank.rank_name, rank.division, rank.short_rank, rank.tier)}
            </div>
          )}
        </div>
      </div>

      {/* Band 2: Sub-Header Seal & H2H Ledger Row */}
      <div className="px-4 py-2 border-b border-line flex flex-wrap items-center content-center justify-between gap-x-3 gap-y-1.5 min-w-0">
        <div className="flex flex-wrap items-center gap-1.5 min-w-[180px] flex-1">
          {isLocal && (
            <>
              <span className={`${STAMP_PILL_BASE} uppercase t-font-display font-bold cartouche-monolith tracking-wider`}>
                ◆ YOU · ACTIVE SESSION
              </span>
              <span className={`${STAMP_PILL_BASE} uppercase t-font-mono font-bold cartouche-mono`}>
                IN RANGE {wins}W–{losses}L
              </span>
              {sessionRecord.netMmr !== undefined && (
                <span
                  className={`${STAMP_PILL_BASE} uppercase t-font-mono font-extrabold ${
                    sessionRecord.netMmr >= 0 ? 'cartouche-celadon text-win' : 'cartouche-gold text-loss'
                  }`}
                >
                  NET MMR {formatSigned(sessionRecord.netMmr)}
                </span>
              )}
            </>
          )}
          {!isLocal && threatBadges.length === 0 && (!player.tags || player.tags.length === 0) && !player.notes && (
            <span className={`${STAMP_PILL_BASE} uppercase t-font-mono font-bold cartouche-mono`}>
              {isTeammate ? '◆ TEAMMATE' : '◆ OPPONENT'}
            </span>
          )}

          {/* Live Threat, Carry & Smurf Detection Badges + User Scouting Tags */}
          {(threatBadges.length > 0 || (player.tags && player.tags.length > 0) || player.notes) && (
            <div
              data-testid="live-player-badges"
              className="flex flex-wrap items-center gap-1.5 min-w-0"
            >
              {threatBadges.map((b, idx) => {
                const badgeStyle =
                  b.type === 'smurf'
                    ? `${b.classes} font-extrabold`
                    : `${b.classes} font-bold`;
                return (
                  <span
                    key={`${b.type}-${idx}`}
                    data-testid={`badge-threat-${b.type}`}
                    title={b.title}
                    className={`${STAMP_PILL_BASE} t-font-display tracking-wider ${badgeStyle}`}
                  >
                    {b.label}
                  </span>
                );
              })}
              {player.tags?.map((tag) => (
                <span
                  key={tag}
                  data-testid={`badge-tag-${tag}`}
                  className={`${STAMP_PILL_BASE} uppercase cartouche-mono font-bold tracking-wider`}
                >
                  {tag}
                </span>
              ))}
              {player.notes && (
                <span
                  data-testid="badge-player-note"
                  title={player.notes}
                  className={`${STAMP_PILL_BASE} uppercase gap-1 font-bold cartouche-carry tracking-wider cursor-help`}
                >
                  <span>◆</span> NOTE
                </span>
              )}
            </div>
          )}
        </div>

        {columns.h2h && (
          <div data-testid="player-h2h" className="flex items-center gap-2 shrink-0 ml-auto">
            <span className="font-bold text-xs tracking-widest text-muted uppercase whitespace-nowrap">{h2hLabel}</span>
            <RecordCells wins={wins} losses={losses} />
            <span className={`font-extrabold text-lg leading-none t-font-mono tabular-nums whitespace-nowrap ${recordColor}`}>
              {formatRecord(wins, losses)}
            </span>
          </div>
        )}
      </div>

      {/* Band 3: Compact Architectural Live Match Scoreboard Strip */}
      {strip.length > 0 && (
        <div
          data-testid="stat-strip"
          className="grid divide-x divide-line border-b border-line min-w-0"
          style={{
            gridTemplateColumns: strip
              .map((s) => (s.key === 'score' ? 'minmax(0,1.65fr)' : 'minmax(0,1fr)'))
              .join(' '),
          }}
        >
          {strip.map(({ key, label, colorClass }) => (
            <div key={key} data-stat={key} className="px-2.5 py-1.5 flex items-baseline justify-between gap-1 min-w-0">
              <span className={`font-extrabold text-[10px] sm:text-[11px] tracking-wider uppercase shrink-0 ${colorClass}`}>
                {label}
              </span>
              <span
                className={`text-xl xl:text-2xl leading-none t-font-mono tabular-nums truncate ${statInk(
                  stats[key] || 0,
                  leaders[key],
                  colorClass,
                )}`}
              >
                {stats[key] || 0}
              </span>
            </div>
          ))}
        </div>
      )}

      {/* Band 4: Collapsible Shared-Header Historical Split Table */}
      {showLiveHistData && (
        <div
          data-testid="live-player-hist-split"
          role="region"
          aria-label={`Historical role split for ${isLocal ? 'You' : player.name}`}
          tabIndex={0}
          className="live-hist-section surface-plaque overflow-x-auto min-w-0"
        >
          <div className={`${HIST_GRID_COLS} text-[11px] t-font-display font-bold uppercase tracking-wider t-text-muted`}>
            <div className="whitespace-nowrap">HIST ({isAvg ? 'AVG/G' : 'TOTAL'})</div>
            <div className={`text-right ${STAT_LABEL_CLASS.games}`}>GMS</div>
            <div className={`text-right ${STAT_LABEL_CLASS.score}`}>PTS</div>
            <div className={`text-right ${STAT_LABEL_CLASS.goals}`}>G</div>
            <div className={`text-right ${STAT_LABEL_CLASS.assists}`}>A</div>
            <div className={`text-right ${STAT_LABEL_CLASS.saves}`}>SV</div>
            <div className={`text-right ${STAT_LABEL_CLASS.shotpct}`}>SH%</div>
            <div className={`text-right ${STAT_LABEL_CLASS.demos}`}>DEM</div>
            <div className={`text-right ${STAT_LABEL_CLASS.boost}`}>BST</div>
          </div>
          {isLocal ? (
            <>
              {renderHistRow('IN WINS', 't-text-win', withStats)}
              {renderHistRow('IN LOSSES', 't-text-loss', againstStats)}
            </>
          ) : (
            <>
              {renderHistRow('WITH YOU', 't-text-strong', withStats)}
              {renderHistRow('AGAINST', 't-text-orange', againstStats)}
            </>
          )}
        </div>
      )}
    </article>
  );
};
