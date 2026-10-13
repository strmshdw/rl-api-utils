import React from 'react';
import { LobbyPlayer, PlayerStatsSummary } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { formatMmr, formatRecord, platformLabel, recordInk } from '../../utils/broadcast';
import { RecordCells } from '../common/ui';

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
  sessionRecord: { wins: number; losses: number };
}

export const LivePlayerCard: React.FC<LivePlayerCardProps> = ({ player, isLocal, isTeammate, leaders, columns, sessionRecord }) => {
  const stats = player.stats || EMPTY_STATS;
  const rank = player.current_rank;

  let h2hLabel = 'VS YOU';
  let wins = player.matchup_record?.wins_as_opponent ?? 0;
  let losses = player.matchup_record?.losses_as_opponent ?? 0;
  if (isLocal) {
    h2hLabel = 'THIS SESSION';
    wins = sessionRecord.wins;
    losses = sessionRecord.losses;
  } else if (isTeammate) {
    h2hLabel = 'WITH YOU';
    wins = player.matchup_record?.wins_as_teammate ?? 0;
    losses = player.matchup_record?.losses_as_teammate ?? 0;
  }
  const recordColor = isLocal && wins + losses > 0 ? 'text-ink' : recordInk(wins, losses);

  const meta: React.ReactNode[] = [];
  if (columns.platform) meta.push(<span key="platform">{platformLabel(player.platform, player.is_bot)}</span>);
  if (columns.rank) meta.push(<span key="rank" className="text-ink-2">{rank?.rank_name || 'UNRANKED'}</span>);
  if (columns.mmr) meta.push(<span key="mmr">{formatMmr(rank?.mmr)}</span>);
  if (player.is_disconnected) {
    meta.push(
      <span key="dc" data-testid="badge-disconnected" className="text-loss" title="Player disconnected early; stats and match outcomes are preserved">
        DISCONNECTED
      </span>,
    );
  }

  const strip = STRIP_STATS.filter((s) => columns[s.key]);

  return (
    <div
      data-testid="live-player-card"
      data-player-id={player.player_id}
      className={`bg-panel px-[26px] py-[22px] flex flex-col gap-[18px] border-l-[3px] font-display ${
        isLocal ? 'border-blue-ink' : 'border-transparent'
      } ${player.is_disconnected ? 'opacity-60' : ''}`}
    >
      <div className="flex justify-between items-start gap-4">
        <div className="flex flex-col gap-1 min-w-0">
          <div title={player.name} className="font-bold text-[34px] tracking-t2 leading-none uppercase truncate">
            {isLocal ? 'You' : player.name}
          </div>
          {meta.length > 0 && (
            <div data-testid="player-meta" className="font-semibold text-[14px] tracking-t10 text-muted uppercase truncate">
              {meta.map((m, i) => (
                <React.Fragment key={i}>
                  {i > 0 && ' · '}
                  {m}
                </React.Fragment>
              ))}
            </div>
          )}
        </div>
        {columns.h2h && (
          <div data-testid="player-h2h" className="flex flex-col items-end gap-1.5 shrink-0">
            <div className="font-semibold text-[13px] tracking-t12 text-muted">{h2hLabel}</div>
            <div className="flex items-center gap-2.5">
              <RecordCells wins={wins} losses={losses} />
              <div className={`font-bold text-[24px] leading-none ${recordColor}`}>{formatRecord(wins, losses)}</div>
            </div>
          </div>
        )}
      </div>
      {strip.length > 0 && (
        <div
          data-testid="stat-strip"
          className="grid border-t border-line-2 pt-3.5"
          style={{ gridTemplateColumns: strip.map((s) => (s.key === 'score' ? 'minmax(0,1.6fr)' : 'minmax(0,1fr)')).join(' ') }}
        >
          {strip.map(({ key, label }) => (
            <div key={key} data-stat={key} className="flex flex-col min-w-0">
              <span className="font-semibold text-[12px] tracking-t14 text-muted-2">{label}</span>
              <span className={`font-extrabold text-[44px] leading-none ${statInk(stats[key] || 0, leaders[key])}`}>{stats[key] || 0}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
