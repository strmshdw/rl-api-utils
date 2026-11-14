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
  platformLabel,
  recordInk,
  shortRank,
} from '../../utils/broadcast';
import { RecordCells } from '../common/ui';
import { useDashboard } from '../../context/DashboardContext';

export type StatKey = keyof PlayerStatsSummary;
export type StatLeaders = Record<StatKey, number>;

export const STRIP_STATS: ReadonlyArray<{ key: StatKey; label: string }> = [
  { key: 'score', label: 'SCORE' },
  { key: 'goals', label: 'G' },
  { key: 'assists', label: 'A' },
  { key: 'saves', label: 'SV' },
  { key: 'shots', label: 'SH' },
  { key: 'demos', label: 'D' },
];

const EMPTY_STATS: PlayerStatsSummary = { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 };

/** Lobby leader (value > 0) is white, other non-zero values ink-3, zeros faint. */
export function statInk(value: number, leader: number): string {
  if (!value) return 'text-faint';
  return value === leader ? 'text-ink-strong' : 'text-ink-3';
}

export function statLeaders(players: LobbyPlayer[]): StatLeaders {
  const leaders = { ...EMPTY_STATS };
  for (const p of players) {
    for (const { key } of STRIP_STATS) leaders[key] = Math.max(leaders[key], p.stats?.[key] || 0);
  }
  return leaders;
}

interface LivePlayerCardProps {
  player: LobbyPlayer;
  isLocal: boolean;
  isTeammate: boolean;
  leaders: StatLeaders;
  columns: ColumnConfig;
  /** Session W–L, shown on the local player's own card. */
  sessionRecord: { wins: number; losses: number; netMmr?: number };
  localPlayer?: LobbyPlayer | null;
  opposingTeamPlayers?: LobbyPlayer[];
  activePlaylistId?: number;
  playlistScope?: string;
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
  playlistScope = 'all',
  onInspectPlayer,
}) => {
  const { statMode, showLiveHistData } = useDashboard();
  const isAvg = statMode === 'avg';

  const scopedPlaylistId =
    playlistScope === '2v2' || playlistScope === '11'
      ? 11
      : playlistScope === '3v3' || playlistScope === '13'
      ? 13
      : playlistScope === '1v1' || playlistScope === '10'
      ? 10
      : 0;

  const stats = player.stats || EMPTY_STATS;
  const rank =
    scopedPlaylistId > 0
      ? player.ranks?.[String(scopedPlaylistId)] ??
        (player.current_rank?.playlist_id === scopedPlaylistId ? player.current_rank : undefined)
      : player.current_rank;

  const playlistApplies =
    scopedPlaylistId === 0 ||
    scopedPlaylistId === activePlaylistId ||
    player.matchup_record?.playlist_id === scopedPlaylistId;

  let h2hLabel = 'VS YOU';
  let wins = playlistApplies ? player.matchup_record?.wins_as_opponent ?? 0 : 0;
  let losses = playlistApplies ? player.matchup_record?.losses_as_opponent ?? 0 : 0;
  if (isLocal) {
    h2hLabel = 'THIS SESSION';
    wins = playlistApplies ? sessionRecord.wins : 0;
    losses = playlistApplies ? sessionRecord.losses : 0;
  } else if (isTeammate) {
    h2hLabel = 'WITH YOU';
    wins = playlistApplies ? player.matchup_record?.wins_as_teammate ?? 0 : 0;
    losses = playlistApplies ? player.matchup_record?.losses_as_teammate ?? 0 : 0;
  }
  const recordColor = isLocal && wins + losses > 0 ? 'text-ink' : recordInk(wins, losses);

  const meta: React.ReactNode[] = [];
  if (columns.platform) {
    meta.push(<span key="platform">{platformLabel(player.platform, player.is_bot)}</span>);
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
      </span>
    );
  }

  const strip = STRIP_STATS.filter((s) => columns[s.key]);

  // Compute Live Threat, Carry & Smurf Badges
  const threatBadges = computeLiveThreatBadges(
    player,
    localPlayer,
    opposingTeamPlayers,
    scopedPlaylistId > 0 ? scopedPlaylistId : activePlaylistId
  );

  // Build historical split rows without fabricated telemetry fallbacks
  const tw = playlistApplies ? player.matchup_record?.wins_as_teammate ?? 0 : 0;
  const tl = playlistApplies ? player.matchup_record?.losses_as_teammate ?? 0 : 0;
  const ow = playlistApplies ? player.matchup_record?.wins_as_opponent ?? 0 : 0;
  const ol = playlistApplies ? player.matchup_record?.losses_as_opponent ?? 0 : 0;

  const withStats: PlayerRoleStatsSummary = !playlistApplies
    ? emptyRoleStats(0, 0, 0)
    : isLocal
    ? player.in_wins_stats ?? emptyRoleStats(Math.max(0, sessionRecord.wins), sessionRecord.wins, 0)
    : player.with_you_stats ?? emptyRoleStats(tw + tl, tw, tl);

  const againstStats: PlayerRoleStatsSummary = !playlistApplies
    ? emptyRoleStats(0, 0, 0)
    : isLocal
    ? player.in_losses_stats ?? emptyRoleStats(Math.max(0, sessionRecord.losses), 0, sessionRecord.losses)
    : player.against_you_stats ?? emptyRoleStats(ow + ol, ow, ol);

  const renderHistRow = (
    roleLabel: string,
    roleColor: string,
    s: PlayerRoleStatsSummary
  ) => {
    if (!s || s.games === 0) {
      return (
        <div
          key={roleLabel}
          className="grid grid-cols-10 px-3 py-1 border-t border-line items-center text-xs t-font-mono opacity-45"
        >
          <div className="col-span-2 font-bold t-text-muted uppercase whitespace-nowrap">{roleLabel}</div>
          <div className="col-span-1 text-right t-text-faint">0G</div>
          <div className="col-span-7 text-center text-xs t-font-display tracking-wider t-text-faint uppercase">
            — NO MATCHES IN SELECTED RANGE —
          </div>
        </div>
      );
    }
    if (!hasSyncedRoleTelemetry(s)) {
      return (
        <div
          key={roleLabel}
          className="grid grid-cols-10 px-3 py-1 border-t border-line items-center text-xs t-font-mono opacity-70"
        >
          <div className={`col-span-2 t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap ${roleColor}`}>
            {roleLabel}
          </div>
          <div className="col-span-1 text-right font-bold t-text-ink2">{s.games}G</div>
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
        className="grid grid-cols-10 px-3 py-1 border-t border-line items-center text-sm t-font-mono"
      >
        <div className={`col-span-2 t-font-display font-bold text-xs uppercase tracking-wider whitespace-nowrap ${roleColor}`}>
          {roleLabel}
        </div>
        <div className="col-span-1 text-right font-bold t-text-ink2">{s.games}G</div>
        <div className="col-span-1 text-right font-bold t-text-strong">{pts}</div>
        <div className="col-span-1 text-right font-bold t-text-win">{g}</div>
        <div className="col-span-1 text-right t-text-ink">{a}</div>
        <div className="col-span-1 text-right t-text-ink">{sv}</div>
        <div className="col-span-1 text-right t-text-ink2">{shPct}</div>
        <div className="col-span-1 text-right t-text-orange">{dem}</div>
        <div className="col-span-1 text-right t-text-muted">{bst}</div>
      </div>
    );
  };

  const isAlly = player.team_num === 0;

  return (
    <div
      data-testid="live-player-card"
      data-player-id={player.player_id}
      onClick={() => {
        if (onInspectPlayer && !player.is_bot) {
          onInspectPlayer(player.player_id, isLocal ? 'You' : player.name);
        }
      }}
      className={`surface-plaque flex flex-col border font-display t-radius overflow-hidden transition-colors ${
        isLocal
          ? 'border-blue-ink border-outline'
          : 'border-outline'
      } ${player.is_disconnected ? 'opacity-60' : ''} ${
        onInspectPlayer && !player.is_bot ? 'cursor-pointer t-hover-panel' : ''
      }`}
    >
      {/* Full-Bleed Team Monolith Header Slab */}
      <div
        className={`px-4 py-2.5 flex items-center justify-between gap-3 border-b border-line ${
          isAlly ? 't-bg-blue-slab bg-blue-slab t-on-slab' : 't-bg-orange-slab bg-orange-slab text-white'
        }`}
      >
        <div className="flex flex-col min-w-0">
          <span className="t-font-mono text-[10px] font-bold tracking-widest uppercase opacity-80">
            {isAlly ? 'BLUE TEAM // PLAYER' : 'ORANGE TEAM // PLAYER'}
          </span>
          <div className="flex items-baseline gap-2.5 flex-wrap">
            <div
              title={player.name}
              className="font-bold text-2xl sm:text-[26px] tracking-wider leading-none uppercase truncate"
            >
              {isLocal ? 'You' : player.name}
            </div>
            {meta.length > 0 && (
              <div
                data-testid="player-meta"
                className="t-font-mono font-bold text-[11px] tracking-wider uppercase opacity-85 truncate"
              >
                {meta.map((m, i) => (
                  <React.Fragment key={i}>
                    {i > 0 && ' · '}
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
              {shortRank(rank.rank_name, rank.division)}
            </div>
          )}
        </div>
      </div>

      {/* Sub-Header Seal & H2H Ledger Row */}
      <div className="px-4 py-2 border-b border-line flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-1.5 min-w-0">
          {isLocal && (
            <>
              <span className="text-[11px] t-font-display font-bold px-2 py-0.5 cartouche-monolith uppercase tracking-wider">
                ◆ YOU · ACTIVE SESSION
              </span>
              <span className="text-[11px] t-font-mono font-bold px-2 py-0.5 cartouche-mono uppercase">
                IN RANGE {wins}W–{losses}L
              </span>
              {sessionRecord.netMmr !== undefined && (
                <span
                  className={`text-[11px] t-font-mono font-extrabold px-2 py-0.5 uppercase ${
                    sessionRecord.netMmr >= 0 ? 'cartouche-celadon text-win' : 'cartouche-gold text-loss'
                  }`}
                >
                  NET MMR {formatSigned(sessionRecord.netMmr)}
                </span>
              )}
            </>
          )}
          {!isLocal && threatBadges.length === 0 && (!player.tags || player.tags.length === 0) && !player.notes && (
            <span className="text-[11px] t-font-mono font-bold px-2 py-0.5 cartouche-mono uppercase">
              {isTeammate ? '◆ TEAMMATE' : '◆ OPPONENT'}
            </span>
          )}

          {/* Live Threat, Carry & Smurf Detection Badges + User Scouting Tags */}
          {(threatBadges.length > 0 || (player.tags && player.tags.length > 0) || player.notes) && (
            <div
              data-testid="live-player-badges"
              className="flex flex-wrap items-center gap-1.5"
            >
              {threatBadges.map((b, idx) => {
                const badgeStyle =
                  b.type === 'smurf'
                    ? 'hanko-seal-vermilion font-extrabold'
                    : b.type === 'carry'
                    ? 'cartouche-carry font-bold'
                    : 'cartouche-gold font-bold';
                return (
                  <span
                    key={`${b.type}-${idx}`}
                    data-testid={`badge-threat-${b.type}`}
                    title={b.title}
                    className={`text-[11px] t-font-display px-2 py-0.5 uppercase tracking-wider ${badgeStyle}`}
                  >
                    {b.label}
                  </span>
                );
              })}
              {player.tags?.map((tag) => (
                <span
                  key={tag}
                  data-testid={`badge-tag-${tag}`}
                  className="cartouche-mono text-[11px] font-bold px-2 py-0.5 tracking-wider uppercase"
                >
                  {tag}
                </span>
              ))}
              {player.notes && (
                <span
                  data-testid="badge-player-note"
                  title={player.notes}
                  className="inline-flex items-center gap-1 text-[11px] font-bold cartouche-carry px-2 py-0.5 tracking-wider uppercase cursor-help"
                >
                  <span>◆</span> NOTE
                </span>
              )}
            </div>
          )}
        </div>

        {columns.h2h && (
          <div data-testid="player-h2h" className="flex items-center gap-2 shrink-0">
            <span className="font-bold text-xs tracking-widest text-muted uppercase">{h2hLabel}</span>
            <RecordCells wins={wins} losses={losses} />
            <span className={`font-extrabold text-lg leading-none t-font-mono ${recordColor}`}>
              {formatRecord(wins, losses)}
            </span>
          </div>
        )}
      </div>

      {/* Compact Architectural Live Match Scoreboard Strip */}
      {strip.length > 0 && (
        <div
          data-testid="stat-strip"
          className="grid divide-x divide-line border-b border-line"
          style={{
            gridTemplateColumns: strip
              .map((s) => (s.key === 'score' ? 'minmax(0,1.4fr)' : 'minmax(0,1fr)'))
              .join(' '),
          }}
        >
          {strip.map(({ key, label }) => (
            <div key={key} data-stat={key} className="px-3 py-1.5 flex items-baseline justify-between gap-1 min-w-0">
              <span className="font-bold text-[11px] tracking-widest text-muted uppercase">{label}</span>
              <span
                className={`font-extrabold text-2xl leading-none t-font-mono ${statInk(
                  stats[key] || 0,
                  leaders[key]
                )}`}
              >
                {stats[key] || 0}
              </span>
            </div>
          ))}
        </div>
      )}

      {/* Collapsible Shared-Header Historical Split Table */}
      {showLiveHistData && (
        <div
          data-testid="live-player-hist-split"
          className="live-hist-section surface-plaque overflow-hidden"
        >
          <div className="grid grid-cols-10 px-3 py-1 text-[11px] t-font-display font-bold uppercase tracking-wider t-text-muted">
            <div className="col-span-2 whitespace-nowrap">HIST ({isAvg ? 'AVG/G' : 'TOTAL'})</div>
            <div className="col-span-1 text-right">GMS</div>
            <div className="col-span-1 text-right">PTS</div>
            <div className="col-span-1 text-right">G</div>
            <div className="col-span-1 text-right">A</div>
            <div className="col-span-1 text-right">SV</div>
            <div className="col-span-1 text-right">SH%</div>
            <div className="col-span-1 text-right">DEM</div>
            <div className="col-span-1 text-right">BST</div>
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
    </div>
  );
};

