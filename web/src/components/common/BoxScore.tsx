import React from 'react';
import { MvpChip, COLUMN_HEADER } from './ui';
import { SessionMatchDetail } from '../../types/api';
import { TEAM_SLAB_BORDER, formatMmr, formatRecord, localTeamOf, recordInk, zeroInk } from '../../utils/broadcast';

export interface BoxScoreRow {
  key: string;
  name: string;
  you?: boolean;
  mvp?: boolean;
  disconnected?: boolean;
  rank?: string;
  mmr?: number;
  score: number;
  goals: number;
  assists: number;
  saves: number;
  shots: number;
  demos: number;
  /** Head-to-head record with/against the local player. */
  h2h?: { wins: number; losses: number };
}

interface TeamBoxScoreProps {
  team: 0 | 1;
  label: string;
  rows: BoxScoreRow[];
  /** Adds RANK and MMR columns (used for the last-match box score). */
  withRanks?: boolean;
  /** Adds an H2H record column. */
  withH2H?: boolean;
}

/** Box score rows for one team of a session match, top scorer first. */
export function sessionMatchRows(match: SessionMatchDetail, team: number, mvpId?: string): BoxScoreRow[] {
  const localTeam = localTeamOf(match);
  return (match.players || [])
    .filter((p) => p.team_num === team)
    .sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0))
    .map((p) => {
      const rec = p.matchup_record;
      const h2h =
        rec && !p.is_local
          ? team === localTeam
            ? { wins: rec.wins_as_teammate, losses: rec.losses_as_teammate }
            : { wins: rec.wins_as_opponent, losses: rec.losses_as_opponent }
          : undefined;
      return {
        key: p.player_id,
        name: p.is_local ? 'You' : p.name,
        you: p.is_local,
        mvp: p.player_id === mvpId,
        disconnected: p.is_disconnected,
        rank: p.rank_name,
        mmr: p.mmr,
        score: p.stats?.score || 0,
        goals: p.stats?.goals || 0,
        assists: p.stats?.assists || 0,
        saves: p.stats?.saves || 0,
        shots: p.stats?.shots || 0,
        demos: p.stats?.demos || 0,
        h2h,
      };
    });
}

const WITH_RANKS = 'minmax(90px,1fr) minmax(0,.9fr) 52px repeat(6,36px)';
const STATS_ONLY = 'minmax(90px,1fr) repeat(6,36px)';

/** One team's box score panel with a 3px rule in the team slab colour. */
export const TeamBoxScore: React.FC<TeamBoxScoreProps> = ({ team, label, rows, withRanks = false, withH2H = false }) => {
  const gridTemplateColumns = `${withRanks ? WITH_RANKS : STATS_ONLY}${withH2H ? ' 64px' : ''}`;
  return (
    <div
      data-testid={`box-score-${team === 0 ? 'blue' : 'orange'}`}
      className={`bg-panel px-[22px] pt-4 pb-[18px] border-t-[3px] ${TEAM_SLAB_BORDER[team]} flex flex-col min-w-0 overflow-hidden font-display`}
    >
      <div className={`grid gap-x-2 pb-1.5 ${COLUMN_HEADER}`} style={{ gridTemplateColumns }}>
        <span>{label}</span>
        {withRanks && <span>RANK</span>}
        {withRanks && <span className="text-right">MMR</span>}
        <span className="text-right">PTS</span>
        <span className="text-right">G</span>
        <span className="text-right">A</span>
        <span className="text-right">SV</span>
        <span className="text-right">SH</span>
        <span className="text-right">D</span>
        {withH2H && <span className="text-right">H2H</span>}
      </div>
      {rows.map((p) => (
        <div
          key={p.key}
          className={`grid gap-x-2 border-t border-line font-bold text-[20px] ${withRanks ? 'items-center py-2' : 'items-baseline py-1.5'} ${
            p.disconnected ? 'opacity-60' : ''
          }`}
          style={{ gridTemplateColumns }}
        >
          <span className="flex items-center gap-2 min-w-0">
            <span
              title={p.name}
              className={`tracking-t4 uppercase whitespace-nowrap overflow-hidden text-ellipsis ${p.you ? 'text-blue-ink' : 'text-ink'}`}
            >
              {p.name}
            </span>
            {p.mvp && <MvpChip />}
          </span>
          {withRanks && (
            <span className="font-semibold text-[14px] tracking-t6 text-muted uppercase whitespace-nowrap overflow-hidden text-ellipsis">
              {p.rank || 'UNRANKED'}
            </span>
          )}
          {withRanks && <span className="text-right text-ink-2 text-[17px]">{formatMmr(p.mmr)}</span>}
          <span className="text-right">{p.score}</span>
          <span className={`text-right ${zeroInk(p.goals)}`}>{p.goals}</span>
          <span className={`text-right ${zeroInk(p.assists)}`}>{p.assists}</span>
          <span className={`text-right ${zeroInk(p.saves)}`}>{p.saves}</span>
          <span className="text-right text-ink-3">{p.shots}</span>
          <span className={`text-right ${zeroInk(p.demos)}`}>{p.demos}</span>
          {withH2H && (
            <span className={`text-right text-[17px] ${p.h2h ? recordInk(p.h2h.wins, p.h2h.losses) : 'text-faint'}`}>
              {p.h2h ? formatRecord(p.h2h.wins, p.h2h.losses) : '—'}
            </span>
          )}
        </div>
      ))}
    </div>
  );
};
