import React from 'react';
import { MvpChip, COLUMN_HEADER } from './ui';
import { SessionMatchDetail } from '../../types/api';
import {
  TEAM_SLAB_BORDER,
  formatMmr,
  formatRecord,
  localTeamOf,
  onlineIdOf,
  recordInk,
  shortRank,
  zeroInk,
} from '../../utils/broadcast';
import { STAT_LABEL_CLASS } from '../../utils/statColors';

export interface BoxScoreRow {
  key: string;
  name: string;
  playerId?: string;
  you?: boolean;
  inspected?: boolean;
  mvp?: boolean;
  disconnected?: boolean;
  rank?: string;
  shortRank?: string;
  tier?: number;
  division?: number;
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

export interface TeamBoxScoreProps {
  team: 0 | 1;
  label: string;
  rows: BoxScoreRow[];
  /** Adds MMR and RANK · DIV columns (defaults to true in Phase 3). */
  withRanks?: boolean;
  /** Adds an H2H record column. */
  withH2H?: boolean;
  /** Optional click callback for player navigation */
  onPlayerClick?: (playerId: string, playerName?: string) => void;
}

/** Box score rows for one team of a session match, top scorer first. */
export function sessionMatchRows(match: SessionMatchDetail, team: number, mvpId?: string): BoxScoreRow[] {
  const localTeam = localTeamOf(match);
  const sorted = (match.players || [])
    .filter((p) => p.team_num === team)
    .sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0));

  let seenLocal = false;
  const seenKeys = new Set<string>();
  const deduped = sorted.filter((p) => {
    if (p.is_local) {
      if (seenLocal) return false;
      seenLocal = true;
    }
    const idKey = onlineIdOf(p.player_id) || (p.player_id || '').trim().toLowerCase();
    const nameKey = (p.name || '').trim().toLowerCase();
    if ((idKey && seenKeys.has(`id:${idKey}`)) || (nameKey && seenKeys.has(`name:${nameKey}`))) {
      return false;
    }
    if (idKey) seenKeys.add(`id:${idKey}`);
    if (nameKey) seenKeys.add(`name:${nameKey}`);
    return true;
  });

  return deduped.map((p) => {
    const rec = p.matchup_record;
    const h2h =
      rec && !p.is_local
        ? team === localTeam
          ? { wins: rec.wins_as_teammate, losses: rec.losses_as_teammate }
          : { wins: rec.wins_as_opponent, losses: rec.losses_as_opponent }
        : undefined;
    return {
      key: p.player_id,
      playerId: p.player_id,
      name: p.is_local ? 'You' : p.name,
      you: p.is_local,
      mvp: p.player_id === mvpId,
      disconnected: p.is_disconnected,
      rank: p.rank_name,
      shortRank: p.short_rank,
      tier: p.tier,
      division: p.division,
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

const WITH_RANKS =
  'minmax(112px,1.45fr) minmax(44px,0.52fr) minmax(112px,1.18fr) minmax(42px,0.52fr) repeat(5,minmax(26px,0.36fr))';
const STATS_ONLY = 'minmax(110px,1fr) minmax(44px,0.55fr) repeat(5,minmax(28px,0.42fr))';

/** One team's box score panel with a 3px rule in the team slab colour. */
export const TeamBoxScore: React.FC<TeamBoxScoreProps> = ({
  team,
  label,
  rows,
  withRanks = true,
  withH2H = false,
  onPlayerClick,
}) => {
  const gridTemplateColumns = `${withRanks ? WITH_RANKS : STATS_ONLY}${
    withH2H ? ' minmax(54px,0.65fr)' : ''
  }`;
  const minGridWidth = withRanks ? (withH2H ? 'min-w-[490px]' : 'min-w-[432px]') : 'min-w-[320px]';
  return (
    <div
      data-testid={`box-score-${team === 0 ? 'blue' : 'orange'}`}
      role="region"
      aria-label={`${label} box score`}
      tabIndex={0}
      className={`surface-plaque border border-outline px-4 sm:px-5 pt-3.5 pb-4 border-t-[3px] ${TEAM_SLAB_BORDER[team]} flex flex-col min-w-0 overflow-x-auto font-display t-radius`}
    >
      <div className={`grid gap-x-2 pb-1.5 ${minGridWidth} ${COLUMN_HEADER}`} style={{ gridTemplateColumns }}>
        <span className={team === 0 ? 't-text-blue' : 't-text-orange'}>
          {label}
        </span>
        {withRanks && <span className={`text-right ${STAT_LABEL_CLASS.mmr}`}>MMR</span>}
        {withRanks && <span className={`pl-1 ${STAT_LABEL_CLASS.rank}`}>RANK · DIV</span>}
        <span className={`text-right ${STAT_LABEL_CLASS.score}`}>PTS</span>
        <span className={`text-right ${STAT_LABEL_CLASS.goals}`}>G</span>
        <span className={`text-right ${STAT_LABEL_CLASS.assists}`}>A</span>
        <span className={`text-right ${STAT_LABEL_CLASS.saves}`}>SV</span>
        <span className={`text-right ${STAT_LABEL_CLASS.shots}`}>SH</span>
        <span className={`text-right ${STAT_LABEL_CLASS.demos}`}>D</span>
        {withH2H && <span className={`text-right ${STAT_LABEL_CLASS.h2h}`}>H2H</span>}
      </div>
      {rows.map((p) => {
        const formattedRank = shortRank(p.rank, p.division, p.shortRank, p.tier);
        return (
          <div
            key={p.key}
            className={`grid gap-x-2 ${minGridWidth} border-t border-line font-bold text-base sm:text-[18px] items-center py-2 t-hover-panel ${
              p.disconnected ? 'opacity-60' : ''
            }`}
            style={{ gridTemplateColumns }}
          >
            <span className="flex items-center gap-2 min-w-0">
              {onPlayerClick && (p.playerId || p.key) ? (
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onPlayerClick(p.playerId || p.key, p.name);
                  }}
                  title={`View profile for ${p.name}`}
                  className={`tracking-t4 uppercase whitespace-nowrap overflow-hidden text-ellipsis hover:underline text-left cursor-pointer transition-colors duration-120 ${
                    p.inspected
                      ? 'text-amber-400 font-extrabold'
                      : p.you
                      ? 'text-blue-ink font-extrabold'
                      : 'text-ink hover:opacity-80'
                  }`}
                >
                  {p.name}
                </button>
              ) : (
                <span
                  title={p.name}
                  className={`tracking-t4 uppercase whitespace-nowrap overflow-hidden text-ellipsis ${
                    p.inspected ? 'text-amber-400 font-extrabold' : p.you ? 'text-blue-ink font-extrabold' : 'text-ink'
                  }`}
                >
                  {p.name}
                </span>
              )}
              {p.inspected && (
                <span className="font-display font-bold text-xs tracking-wider bg-amber-500/20 text-amber-300 px-1.5 py-0.5 border border-amber-500/30">
                  INSPECTED
                </span>
              )}
              {p.mvp && <MvpChip />}
            </span>
            {withRanks && (
              <span className={`text-right t-font-mono font-bold text-base ${p.mmr ? STAT_LABEL_CLASS.mmr : 'text-faint'}`}>
                {formatMmr(p.mmr)}
              </span>
            )}
            {withRanks && (
              <span
                title={formattedRank}
                className={`pl-1 font-bold text-sm tracking-wider uppercase whitespace-nowrap overflow-hidden text-ellipsis ${
                  formattedRank && formattedRank !== '—' ? STAT_LABEL_CLASS.rank : 'text-faint'
                }`}
              >
                {formattedRank}
              </span>
            )}
            <span className={`text-right t-font-mono font-bold ${STAT_LABEL_CLASS.score}`}>{p.score}</span>
            <span className={`text-right t-font-mono ${zeroInk(p.goals, `${STAT_LABEL_CLASS.goals} font-extrabold`)}`}>{p.goals}</span>
            <span className={`text-right t-font-mono ${zeroInk(p.assists, STAT_LABEL_CLASS.assists)}`}>{p.assists}</span>
            <span className={`text-right t-font-mono ${zeroInk(p.saves, STAT_LABEL_CLASS.saves)}`}>{p.saves}</span>
            <span className={`text-right t-font-mono ${zeroInk(p.shots, STAT_LABEL_CLASS.shots)}`}>{p.shots}</span>
            <span className={`text-right t-font-mono ${zeroInk(p.demos, `${STAT_LABEL_CLASS.demos} font-extrabold`)}`}>
              {p.demos}
            </span>
            {withH2H && (
              <span
                className={`text-right t-font-mono text-base ${
                  p.h2h ? recordInk(p.h2h.wins, p.h2h.losses) : 'text-faint'
                }`}
              >
                {p.h2h ? formatRecord(p.h2h.wins, p.h2h.losses) : '—'}
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
};

