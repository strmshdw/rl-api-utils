import React, { useMemo, useState } from 'react';
import { SessionMatchDetail, SessionResponse } from '../../types/api';
import {
  chronological,
  deltaInk,
  findMvp,
  formatAgo,
  formatElapsed,
  formatSigned,
  localTeamOf,
  outcomeOf,
  playlistLabels,
  rosterLine,
  summarizeSession,
  zeroInk,
} from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { COLUMN_HEADER, EmptyLine, GhostButton, MvpChip, PageHeader, ResultSlab, SectionLabel, Segmented } from '../common/ui';
import { SessionKpiStrip } from './SessionKpis';
import { MatchDetailModal } from './MatchDetailModal';
import { useMMRHistory } from '../../hooks/useMMRHistory';

type OutcomeFilter = 'all' | 'victory' | 'defeat';

const OUTCOME_OPTIONS = [
  ['all', 'ALL'],
  ['victory', 'WINS'],
  ['defeat', 'LOSSES'],
] as const;

const ROW_GRID = '72px 120px minmax(0,1fr) repeat(4,64px) 90px 90px';

interface SessionViewProps {
  session: SessionResponse | null;
  isLoading: boolean;
  error: string | null;
  onResetSession: () => void;
}

const SessionMatchRow: React.FC<{ match: SessionMatchDetail; onOpen: () => void }> = ({ match, onOpen }) => {
  const localTeam = localTeamOf(match);
  const us = localTeam === 0 ? match.blue_score : match.orange_score;
  const them = localTeam === 0 ? match.orange_score : match.blue_score;
  const local = match.players?.find((p) => p.is_local);
  const mvp = findMvp(match.players || [], match.winner_team, match.blue_score, match.orange_score);
  const isLocalMvp = !!local && mvp?.player_id === local.player_id;
  // Matches recorded without your box score show dashes rather than zeros.
  const stat = (v: number | undefined) =>
    local?.stats ? <span className={zeroInk(v || 0)}>{v || 0}</span> : <span className="text-faint">—</span>;

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
      className="grid items-center h-[72px] bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 outline-none focus-visible:bg-panel-hover"
      style={{ gridTemplateColumns: ROW_GRID }}
    >
      <ResultSlab outcome={outcomeOf(match.result)} className="text-[22px]" />
      <div className="flex items-center gap-2 pl-[22px] font-extrabold text-[38px] leading-none">
        <span className={localTeam === 0 ? 'text-blue-ink' : 'text-orange-ink'}>{us}</span>
        <span className="text-faint text-[26px]">–</span>
        <span className={localTeam === 0 ? 'text-orange-ink' : 'text-blue-ink'}>{them}</span>
      </div>
      <div className="flex flex-col gap-[3px] min-w-0 pr-4">
        <div className="flex items-center gap-2.5 font-bold text-[18px] tracking-t6 min-w-0">
          <span className="truncate">{playlistLabels(match.playlist_id, match.playlist_name).name}</span>
          <span className="text-[14px] text-muted font-semibold">{formatDuration(match.duration_seconds)}</span>
          {isLocalMvp && <MvpChip large />}
        </div>
        <div className="font-medium text-[14px] tracking-t6 text-muted truncate">{rosterLine(match)}</div>
      </div>
      <span className="text-right font-bold text-[26px]">{stat(local?.stats?.goals)}</span>
      <span className="text-right font-bold text-[26px]">{stat(local?.stats?.assists)}</span>
      <span className="text-right font-bold text-[26px]">{stat(local?.stats?.saves)}</span>
      <span className="text-right font-bold text-[26px]">
        {local?.stats ? <span className="text-ink">{local.stats.score}</span> : <span className="text-faint">—</span>}
      </span>
      <span className={`text-right font-extrabold text-[26px] ${deltaInk(match.mmr_change)}`}>{formatSigned(match.mmr_change)}</span>
      <span className="text-right pr-[22px] font-semibold text-[14px] tracking-t8 text-muted whitespace-nowrap">
        {formatAgo(match.ended_at, false)}
      </span>
    </div>
  );
};

export const SessionView: React.FC<SessionViewProps> = ({ session, isLoading, error, onResetSession }) => {
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>('all');
  const [selectedMatch, setSelectedMatch] = useState<SessionMatchDetail | null>(null);
  const { byPlaylist: mmrByPlaylist } = useMMRHistory();

  const summary = useMemo(() => summarizeSession(session), [session]);
  const matches = session?.matches ?? [];
  const playlists = session?.playlists ? Object.values(session.playlists) : [];

  const rows = useMemo(() => {
    const newestFirst = chronological(session?.matches).reverse();
    return outcomeFilter === 'all' ? newestFirst : newestFirst.filter((m) => m.result === outcomeFilter);
  }, [session, outcomeFilter]);

  const kicker = session
    ? `STARTED ${formatElapsed(session.started_at)} AGO · ${matches.length} ${matches.length === 1 ? 'MATCH' : 'MATCHES'}`
    : 'CURRENT SESSION';

  const header = (
    <PageHeader
      kicker={kicker}
      title="SESSION"
      right={
        session && (
          <GhostButton
            tone="danger"
            onClick={() => {
              if (window.confirm('Reset all session stats and match history?')) onResetSession();
            }}
          >
            RESET SESSION
          </GhostButton>
        )
      }
    />
  );

  if (!session) {
    return (
      <div className="font-display">
        {header}
        {isLoading ? <EmptyLine>LOADING SESSION…</EmptyLine> : <EmptyLine tone="loss">{error ? `SESSION UNAVAILABLE · ${error}` : 'SESSION UNAVAILABLE'}</EmptyLine>}
      </div>
    );
  }

  return (
    <div data-testid="session-view" className="font-display">
      {header}
      <SessionKpiStrip kpis={summary.kpis} variant="page" playlists={playlists} mmrHistory={mmrByPlaylist} />

      <div className="flex flex-wrap justify-between items-center gap-4 pt-9 pb-3.5">
        <SectionLabel>MATCHES · NEWEST FIRST</SectionLabel>
        <Segmented label="Filter by result" options={OUTCOME_OPTIONS} value={outcomeFilter} onChange={setOutcomeFilter} />
      </div>

      {matches.length === 0 ? (
        <EmptyLine>NO MATCHES THIS SESSION YET</EmptyLine>
      ) : rows.length === 0 ? (
        <EmptyLine>NO {outcomeFilter === 'victory' ? 'WINS' : 'LOSSES'} THIS SESSION YET</EmptyLine>
      ) : (
        <>
          <div className={`grid pb-2 ${COLUMN_HEADER}`} style={{ gridTemplateColumns: ROW_GRID }}>
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
              <SessionMatchRow key={m.match_guid} match={m} onOpen={() => setSelectedMatch(m)} />
            ))}
          </div>
        </>
      )}

      <MatchDetailModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />
    </div>
  );
};
