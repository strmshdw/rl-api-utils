import React from 'react';
import { SessionMatchDetail } from '../../types/api';
import {
  deltaInk,
  findMvp,
  formatAgo,
  formatSigned,
  localTeamOf,
  outcomeOf,
  playlistLabels,
  rosterLine,
  zeroInk,
} from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import {
  COLUMN_HEADER,
  EmptyLine,
  MvpChip,
  ResultSlab,
  SectionLabel,
  Segmented,
} from '../common/ui';

export type OutcomeFilter = 'all' | 'victory' | 'defeat';
export type SessionPlaylistFilter = 'all' | '2v2' | '3v3';

const OUTCOME_OPTIONS = [
  ['all', 'ALL'],
  ['victory', 'WINS'],
  ['defeat', 'LOSSES'],
] as const;

const PLAYLIST_OPTIONS = [
  ['all', 'ALL MODES'],
  ['2v2', '2V2'],
  ['3v3', '3V3'],
] as const;

const ROW_GRID = '72px 120px minmax(0,1fr) repeat(4,64px) 90px 90px';

const SessionMatchRow: React.FC<{ match: SessionMatchDetail; onOpen: () => void }> = ({
  match,
  onOpen,
}) => {
  const localTeam = localTeamOf(match);
  const us = localTeam === 0 ? match.blue_score : match.orange_score;
  const them = localTeam === 0 ? match.orange_score : match.blue_score;
  const local = match.players?.find((p) => p.is_local);
  const mvp = findMvp(match.players || [], match.winner_team, match.blue_score, match.orange_score);
  const isLocalMvp = !!local && mvp?.player_id === local.player_id;
  const stat = (v: number | undefined) =>
    local?.stats ? (
      <span className={zeroInk(v || 0)}>{v || 0}</span>
    ) : (
      <span className="text-faint">—</span>
    );

  return (
    <div
      role="button"
      tabIndex={0}
      data-testid="session-match-row"
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onOpen();
        }
      }}
      className="grid items-center h-[72px] bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 outline-none focus-visible:bg-panel-hover t-border border border-line"
      style={{ gridTemplateColumns: ROW_GRID }}
    >
      <ResultSlab outcome={outcomeOf(match.result)} className="text-[22px]" />
      <div className="flex items-center gap-2 pl-[22px] font-extrabold text-[38px] leading-none t-font-mono">
        <span className={localTeam === 0 ? 'text-blue-ink' : 'text-orange-ink'}>{us}</span>
        <span className="text-faint text-[26px]">–</span>
        <span className={localTeam === 0 ? 'text-orange-ink' : 'text-blue-ink'}>{them}</span>
      </div>
      <div className="flex flex-col gap-[3px] min-w-0 pr-4">
        <div className="flex items-center gap-2.5 font-bold text-lg tracking-t6 min-w-0">
          <span className="truncate">
            {playlistLabels(match.playlist_id, match.playlist_name).name}
          </span>
          <span className="text-sm text-muted font-semibold t-font-mono">
            {formatDuration(match.duration_seconds)}
          </span>
          {isLocalMvp && <MvpChip large />}
        </div>
        <div className="font-medium text-sm tracking-t6 text-muted truncate">
          {rosterLine(match)}
        </div>
      </div>
      <span className="text-right font-bold text-[26px] t-font-mono">
        {stat(local?.stats?.goals)}
      </span>
      <span className="text-right font-bold text-[26px] t-font-mono">
        {stat(local?.stats?.assists)}
      </span>
      <span className="text-right font-bold text-[26px] t-font-mono">
        {stat(local?.stats?.saves)}
      </span>
      <span className="text-right font-bold text-[26px] t-font-mono">
        {local?.stats ? (
          <span className="text-ink">{local.stats.score}</span>
        ) : (
          <span className="text-faint">—</span>
        )}
      </span>
      <span
        className={`text-right font-extrabold text-[26px] t-font-mono ${deltaInk(
          match.mmr_change,
        )}`}
      >
        {formatSigned(match.mmr_change)}
      </span>
      <span className="text-right pr-[22px] font-semibold text-sm tracking-t8 text-muted whitespace-nowrap">
        {formatAgo(match.ended_at, false)}
      </span>
    </div>
  );
};

interface SessionMatchesSectionProps {
  totalMatchCount: number;
  rows: SessionMatchDetail[];
  outcomeFilter: OutcomeFilter;
  onOutcomeChange: (next: OutcomeFilter) => void;
  playlistFilter: SessionPlaylistFilter;
  onPlaylistChange: (next: SessionPlaylistFilter) => void;
  onOpenMatch: (match: SessionMatchDetail) => void;
}

export const SessionMatchesSection: React.FC<SessionMatchesSectionProps> = ({
  totalMatchCount,
  rows,
  outcomeFilter,
  onOutcomeChange,
  playlistFilter,
  onPlaylistChange,
  onOpenMatch,
}) => (
  <>
    <div className="flex flex-wrap justify-between items-center gap-4 pt-4 pb-2">
      <SectionLabel>
        SESSION MATCHES ({rows.length}) · CLICK ANY ROW FOR 5-TAB TELEMETRY
      </SectionLabel>
      <div className="flex flex-wrap items-center gap-3">
        <Segmented
          label="Filter by result"
          options={OUTCOME_OPTIONS}
          value={outcomeFilter}
          onChange={onOutcomeChange}
        />
        <Segmented
          label="Filter by playlist"
          options={PLAYLIST_OPTIONS}
          value={playlistFilter}
          onChange={onPlaylistChange}
        />
      </div>
    </div>

    {totalMatchCount === 0 ? (
      <EmptyLine>NO MATCHES THIS SESSION YET</EmptyLine>
    ) : rows.length === 0 ? (
      <EmptyLine>
        NO{' '}
        {outcomeFilter === 'victory'
          ? 'WINS'
          : outcomeFilter === 'defeat'
          ? 'LOSSES'
          : 'MATCHES'}{' '}
        THIS SESSION YET
      </EmptyLine>
    ) : (
      <div>
        <div
          className={`grid pb-2 ${COLUMN_HEADER}`}
          style={{ gridTemplateColumns: ROW_GRID }}
        >
          <span />
          <span>SCORE</span>
          <span>MATCH</span>
          <span className="text-right">G</span>
          <span className="text-right">A</span>
          <span className="text-right">SV</span>
          <span className="text-right">PTS</span>
          <span className="text-right">MMR</span>
          <span className="text-right pr-[22px]">WHEN</span>
        </div>
        <div className="flex flex-col gap-1.5">
          {rows.map((m) => (
            <SessionMatchRow
              key={m.match_guid}
              match={m}
              onOpen={() => onOpenMatch(m)}
            />
          ))}
        </div>
      </div>
    )}
  </>
);
