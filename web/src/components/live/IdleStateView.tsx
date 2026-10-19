import React, { useMemo } from 'react';
import { ConnectionStatus } from '../../hooks/useLiveMatch';
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
  OUTCOME_SLAB,
  playlistLabels,
  rosterLine,
  summarizeSession,
  teamName,
} from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { TeamBoxScore, sessionMatchRows } from '../common/BoxScore';
import { EmptyLine, GhostButton, SectionLabel, StatusDot } from '../common/ui';
import { PlaylistRows, SessionKpiStrip } from '../session/SessionKpis';
import { ContinueSessionBanner } from '../session/SessionView';
import { useMMRHistory } from '../../hooks/useMMRHistory';

interface IdleStateViewProps {
  session: SessionResponse | null;
  connection: ConnectionStatus;
  onShowSession: () => void;
  onContinueSession?: (sessionId?: string) => void;
  onDismissContinueSession?: (sessionId?: string) => void;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;

const LastMatch: React.FC<{ match: SessionMatchDetail; onShowSession: () => void }> = ({ match, onShowSession }) => {
  const outcome = outcomeOf(match.result);
  const localTeam = localTeamOf(match);
  const mvp = findMvp(match.players || [], match.winner_team, match.blue_score, match.orange_score);
  const winner = match.winner_team ?? (match.blue_score > match.orange_score ? 0 : match.orange_score > match.blue_score ? 1 : null);
  const roster = rosterLine(match)
    .split(' vs ')
    .map((side) => side.toUpperCase())
    .join(' vs ');
  const slabText = (team: number) => `${teamName(team)}${team === localTeam ? ' · YOU' : ''}`;

  return (
    <div data-testid="last-match" className="flex flex-col gap-3.5 pt-8">
      <SectionLabel>LAST MATCH · {formatAgo(match.ended_at)}</SectionLabel>
      <div className="flex flex-wrap items-stretch bg-panel">
        <div
          className={`h-[84px] w-[120px] flex items-center justify-center font-extrabold text-[26px] tracking-t6 ${
            outcome ? `${OUTCOME_SLAB[outcome]} text-page` : 'bg-line-2 text-muted'
          }`}
        >
          {outcome ? OUTCOME_WORD[outcome] : 'FINAL'}
        </div>
        <div className="h-[84px] flex-[0_1_220px] min-w-[110px] bg-blue-slab text-ink flex items-center justify-end pr-5 whitespace-nowrap overflow-hidden font-extrabold text-[24px] tracking-t6">
          {slabText(0)}
        </div>
        <div className="h-[84px] w-[84px] bg-ink text-page flex items-center justify-center font-extrabold text-[60px] leading-none">
          {match.blue_score}
        </div>
        <div className="h-[84px] w-[84px] bg-ink text-page flex items-center justify-center font-extrabold text-[60px] leading-none border-l border-ink-2">
          {match.orange_score}
        </div>
        <div className="h-[84px] flex-[0_1_220px] min-w-[110px] bg-orange-slab text-page flex items-center pl-5 whitespace-nowrap overflow-hidden font-extrabold text-[24px] tracking-t6">
          {slabText(1)}
        </div>
        <div className="flex-[1_1_380px] min-h-[84px] flex items-center justify-between gap-6 px-7 py-3.5 min-w-0">
          <div className="flex flex-col gap-0.5 min-w-0 font-semibold text-[15px] tracking-t8 text-muted">
            <span className="text-ink">
              {playlistLabels(match.playlist_id, match.playlist_name).name} · {formatDuration(match.duration_seconds)}
            </span>
            {roster && <span className="truncate">{roster}</span>}
          </div>
          <div className="flex items-center gap-7 shrink-0">
            <span className={`font-extrabold text-[32px] ${deltaInk(match.mmr_change)}`}>{formatSigned(match.mmr_change)}</span>
            <GhostButton tone="ink" onClick={onShowSession}>
              ALL SESSION MATCHES →
            </GhostButton>
          </div>
        </div>
      </div>
      {(match.players?.length ?? 0) > 0 && (
        <div className="grid gap-[2px] bg-page grid-cols-[repeat(auto-fit,minmax(min(540px,100%),1fr))]">
          {[0, 1].map((team) => (
            <TeamBoxScore
              key={team}
              team={team as 0 | 1}
              label={`${teamName(team)}${winner === team ? ' · WIN' : ''}`}
              rows={sessionMatchRows(match, team, mvp?.player_id)}
              withRanks
            />
          ))}
        </div>
      )}
    </div>
  );
};

export const IdleStateView: React.FC<IdleStateViewProps> = ({
  session,
  connection,
  onShowSession,
  onContinueSession,
  onDismissContinueSession,
}) => {
  const { byPlaylist: mmrByPlaylist } = useMMRHistory();
  const summary = useMemo(() => summarizeSession(session), [session]);
  const matches = chronological(session?.matches);
  const lastMatch = matches.length ? matches[matches.length - 1] : null;
  const playlists = session?.playlists ? Object.values(session.playlists) : [];
  const dot = connection === 'connected' ? 'bg-win' : connection === 'offline' ? 'bg-loss' : 'bg-faint';

  return (
    <div data-testid="live-idle" className="font-display">
      <div className="flex flex-wrap justify-between items-end gap-x-6 gap-y-4 pt-12 pb-8 border-b border-line">
        <div className="flex flex-col gap-2.5 min-w-0">
          <SectionLabel className="flex items-center gap-2">
            <StatusDot className="bg-faint" />
            NO ACTIVE MATCH
          </SectionLabel>
          <h1 className="italic font-extrabold text-[72px] leading-tightest tracking-[.01em]">WAITING FOR KICKOFF</h1>
        </div>
        <div data-testid="daemon-status" className="flex gap-6 pb-1.5 font-semibold text-[14px] tracking-t10 text-muted whitespace-nowrap">
          <span className="flex items-center gap-2">
            <StatusDot className={dot} />
            STATS API :49124
          </span>
          <span className="flex items-center gap-2">
            <StatusDot className={dot} />
            RANK QUERIES {connection === 'offline' ? 'OFFLINE' : 'READY'}
          </span>
        </div>
      </div>

      {session?.continuable_session && (
        <ContinueSessionBanner
          continuableSession={session.continuable_session}
          onContinue={(id) => onContinueSession?.(id)}
          onDismiss={(id) => onDismissContinueSession?.(id)}
        />
      )}

      {lastMatch ? <LastMatch match={lastMatch} onShowSession={onShowSession} /> : <EmptyLine>NO MATCHES THIS SESSION YET</EmptyLine>}

      {session && (
        <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)] gap-7 pt-10">
          <div className="flex flex-col gap-3.5 min-w-0">
            <SectionLabel>THIS SESSION · {formatElapsed(session.started_at)}</SectionLabel>
            <SessionKpiStrip kpis={summary.kpis} variant="idle" />
          </div>
          {playlists.length > 0 && (
            <div className="flex flex-col gap-3.5 min-w-0">
              <SectionLabel>PLAYLISTS</SectionLabel>
              <PlaylistRows playlists={playlists} mmrHistory={mmrByPlaylist} />
            </div>
          )}
        </div>
      )}
    </div>
  );
};
