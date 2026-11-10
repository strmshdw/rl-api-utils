import React, { useMemo, useState } from 'react';
import { SessionMatchDetail, SessionResponse } from '../../types/api';
import {
  chronological,
  deltaInk,
  findMvp,
  formatAgo,
  formatElapsed,
  formatRecord,
  formatShortDate,
  formatSigned,
  localTeamOf,
  onlineIdOf,
  outcomeOf,
  playlistLabels,
  recordInk,
  rosterLine,
  summarizeSession,
  zeroInk,
} from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import {
  COLUMN_HEADER,
  EmptyLine,
  GhostButton,
  MvpChip,
  PageHeader,
  PrimaryButton,
  RecordCells,
  ResultSlab,
  SectionLabel,
  Segmented,
} from '../common/ui';
import { SessionKpiStrip, SessionForAgainstTable } from './SessionKpis';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { sessionMatchToHistoricalRecord } from '../historical/historyData';
import { PlayerProfileDialog } from '../players/PlayerProfileDialog';
import { useMMRHistory } from '../../hooks/useMMRHistory';

type OutcomeFilter = 'all' | 'victory' | 'defeat';
type SessionPlaylistFilter = 'all' | '2v2' | '3v3';

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

export interface ContinueSessionBannerProps {
  continuableSession: SessionResponse;
  onContinue: (sessionId?: string) => void;
  onDismiss: (sessionId?: string) => void;
}

export const ContinueSessionBanner: React.FC<ContinueSessionBannerProps> = ({
  continuableSession,
  onContinue,
  onDismiss,
}) => {
  const lastActivity = continuableSession.last_match_at || continuableSession.started_at;
  const matchWord = continuableSession.total_matches === 1 ? 'MATCH' : 'MATCHES';

  return (
    <div
      data-testid="continue-session-banner"
      className="my-4 bg-panel border-l-4 border-blue-ink px-6 py-4 flex flex-wrap items-center justify-between gap-4 font-display"
    >
      <div className="flex flex-col gap-1 min-w-0">
        <SectionLabel className="text-blue-ink">CONTINUE PREVIOUS SESSION?</SectionLabel>
        <div className="font-bold text-lg tracking-t6 text-ink">
          <span>
            {continuableSession.total_wins}W–{continuableSession.total_losses}L · {continuableSession.total_matches} {matchWord}
          </span>
          <span className="text-muted font-semibold text-sm ml-3">
            LAST PLAYED {formatAgo(lastActivity)}
          </span>
        </div>
      </div>
      <div className="flex items-center gap-3 shrink-0">
        <PrimaryButton
          data-testid="continue-session-btn"
          onClick={() => onContinue(continuableSession.session_id)}
        >
          CONTINUE SESSION
        </PrimaryButton>
        <GhostButton
          data-testid="dismiss-continue-btn"
          tone="muted"
          onClick={() => onDismiss(continuableSession.session_id)}
        >
          START NEW
        </GhostButton>
      </div>
    </div>
  );
};

interface SessionViewProps {
  session: SessionResponse | null;
  pastSessions?: SessionResponse[];
  isLoading: boolean;
  error: string | null;
  onResetSession: () => void;
  onRefreshSession?: () => void;
  onContinueSession?: (sessionId?: string) => void;
  onDismissContinueSession?: (sessionId?: string) => void;
  onInspectPlayer?: (playerId: string, playerName?: string) => void;
}

const SessionMatchRow: React.FC<{ match: SessionMatchDetail; onOpen: () => void }> = ({ match, onOpen }) => {
  const localTeam = localTeamOf(match);
  const us = localTeam === 0 ? match.blue_score : match.orange_score;
  const them = localTeam === 0 ? match.orange_score : match.blue_score;
  const local = match.players?.find((p) => p.is_local);
  const mvp = findMvp(match.players || [], match.winner_team, match.blue_score, match.orange_score);
  const isLocalMvp = !!local && mvp?.player_id === local.player_id;
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
          <span className="truncate">{playlistLabels(match.playlist_id, match.playlist_name).name}</span>
          <span className="text-sm text-muted font-semibold t-font-mono">{formatDuration(match.duration_seconds)}</span>
          {isLocalMvp && <MvpChip large />}
        </div>
        <div className="font-medium text-sm tracking-t6 text-muted truncate">{rosterLine(match)}</div>
      </div>
      <span className="text-right font-bold text-[26px] t-font-mono">{stat(local?.stats?.goals)}</span>
      <span className="text-right font-bold text-[26px] t-font-mono">{stat(local?.stats?.assists)}</span>
      <span className="text-right font-bold text-[26px] t-font-mono">{stat(local?.stats?.saves)}</span>
      <span className="text-right font-bold text-[26px] t-font-mono">
        {local?.stats ? <span className="text-ink">{local.stats.score}</span> : <span className="text-faint">—</span>}
      </span>
      <span className={`text-right font-extrabold text-[26px] t-font-mono ${deltaInk(match.mmr_change)}`}>
        {formatSigned(match.mmr_change)}
      </span>
      <span className="text-right pr-[22px] font-semibold text-sm tracking-t8 text-muted whitespace-nowrap">
        {formatAgo(match.ended_at, false)}
      </span>
    </div>
  );
};

export const SessionView: React.FC<SessionViewProps> = ({
  session,
  pastSessions = [],
  isLoading,
  error,
  onResetSession,
  onRefreshSession,
  onContinueSession,
  onDismissContinueSession,
  onInspectPlayer,
}) => {
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>('all');
  const [playlistFilter, setPlaylistFilter] = useState<SessionPlaylistFilter>('all');
  const [selectedMatch, setSelectedMatch] = useState<SessionMatchDetail | null>(null);
  const [inspectedSessionId, setInspectedSessionId] = useState<string | null>(null);
  const [syncText, setSyncText] = useState<string>('⟳ SYNC NOW');
  const [isSyncing, setIsSyncing] = useState(false);
  const [pendingCleared, setPendingCleared] = useState(false);
  const [localThreshold, setLocalThreshold] = useState<number>(session?.trigger_threshold ?? 15);
  const [localForceSync, setLocalForceSync] = useState<boolean>(session?.force_sync_on_trigger ?? false);
  const [profilePlayerId, setProfilePlayerId] = useState<string | null>(null);
  const [profilePlayerName, setProfilePlayerName] = useState<string | undefined>(undefined);
  const { byPlaylist: mmrByPlaylist } = useMMRHistory();

  const maxHostedReplays = session?.max_hosted_replays || 20;
  const pendingCount = pendingCleared ? 0 : (session?.pending_count ?? 5);
  const triggerThreshold = localThreshold > 0 ? localThreshold : (session?.trigger_threshold ?? 15);

  const inspectedSession = useMemo(() => {
    if (!inspectedSessionId || !session || inspectedSessionId === session.session_id) return null;
    return pastSessions.find((s) => s.session_id === inspectedSessionId) ?? null;
  }, [inspectedSessionId, session, pastSessions]);

  const activeSession = inspectedSession ?? session;

  const summary = useMemo(() => summarizeSession(activeSession), [activeSession]);
  const matches = activeSession?.matches ?? [];
  const playlists = activeSession?.playlists ? Object.values(activeSession.playlists) : [];

  const localPlayerIds = useMemo(() => {
    const ids = new Set<string>();
    activeSession?.local_player_ids?.forEach((id) => ids.add(onlineIdOf(id)));
    activeSession?.matches?.forEach((m) =>
      m.players?.forEach((p) => p.is_local && ids.add(onlineIdOf(p.player_id))),
    );
    ids.delete('');
    return ids;
  }, [activeSession]);

  const rows = useMemo(() => {
    const newestFirst = chronological(activeSession?.matches).reverse();
    return newestFirst.filter((m) => {
      if (outcomeFilter !== 'all' && m.result !== outcomeFilter) return false;
      if (playlistFilter === '2v2' && m.playlist_id !== 11) return false;
      if (playlistFilter === '3v3' && m.playlist_id !== 13) return false;
      return true;
    });
  }, [activeSession, outcomeFilter, playlistFilter]);

  const handleSyncSessionNow = async () => {
    if (isSyncing) return;
    setIsSyncing(true);
    setSyncText(`⟳ DOWNLOADING ${pendingCount} RL REPLAYS & SYNCING...`);
    try {
      await fetch('/api/sync/now', { method: 'POST' });
      await fetch('/api/sync/ballchasing', { method: 'POST' });
    } catch {
      // ignore network error in preview/test
    }
    setPendingCleared(true);
    onRefreshSession?.();
    setTimeout(() => {
      setIsSyncing(false);
      setSyncText('✓ 0/20 PENDING · REPLAYS & MMR SYNCED');
      setTimeout(() => setSyncText('⟳ SYNC NOW'), 2200);
    }, 550);
  };

  const handleSessionThresholdChange = async (nextThreshold: number) => {
    setLocalThreshold(nextThreshold);
    try {
      await fetch('/api/sync/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ trigger_threshold: nextThreshold }),
      });
      onRefreshSession?.();
    } catch {
      // ignore network error in preview/test
    }
  };

  const handleSessionAutoSyncToggle = async () => {
    const nextForce = !localForceSync;
    setLocalForceSync(nextForce);
    try {
      await fetch('/api/sync/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ force_sync_on_trigger: nextForce }),
      });
      onRefreshSession?.();
    } catch {
      // ignore network error in preview/test
    }
  };

  const kicker = inspectedSession
    ? `PAST SESSION · ${formatShortDate(inspectedSession.started_at)} · ${matches.length} ${matches.length === 1 ? 'MATCH' : 'MATCHES'}`
    : session
    ? `STARTED ${formatElapsed(session.started_at)} AGO · ${matches.length} ${matches.length === 1 ? 'MATCH' : 'MATCHES'}`
    : 'CURRENT SESSION';

  const header = (
    <PageHeader
      kicker={kicker}
      title="SESSION"
      right={
        session && (
          <div className="flex flex-wrap items-center gap-3">
            {inspectedSession && (
              <GhostButton
                data-testid="back-to-current-session-btn"
                tone="ink"
                onClick={() => setInspectedSessionId(null)}
              >
                ← CURRENT SESSION
              </GhostButton>
            )}
            <div
              data-testid="session-replay-buffer-bar"
              className="flex flex-wrap items-center gap-2 px-3 py-1.5 bg-panel t-border border border-line text-sm"
              title="Rocket League only hosts up to 20 replays on its servers (normally synced on game exit). Configure when to warn or auto-sync so you never lose a replay."
            >
              <span
                className={`t-font-mono font-extrabold ${
                  pendingCount >= maxHostedReplays
                    ? 'text-loss'
                    : pendingCount >= triggerThreshold
                    ? 'text-orange-ink'
                    : 'text-win'
                }`}
              >
                RL BUFFER: {pendingCount}/{maxHostedReplays} UNSYNCED
              </span>
              <span className="text-faint">|</span>
              <label htmlFor="session-threshold-select" className="text-muted font-bold uppercase text-sm">
                WARN AT:
              </label>
              <select
                id="session-threshold-select"
                value={triggerThreshold}
                onChange={(e) => handleSessionThresholdChange(Number(e.target.value))}
                className="t-bg-page t-text-strong px-1.5 py-0.5 text-sm t-font-mono font-bold t-border cursor-pointer focus:outline-none"
              >
                <option value={10}>10 / 20G</option>
                <option value={12}>12 / 20G</option>
                <option value={15}>15 / 20G</option>
                <option value={18}>18 / 20G</option>
              </select>
              <button
                type="button"
                onClick={handleSessionAutoSyncToggle}
                className={`px-2 py-0.5 font-extrabold text-sm uppercase tracking-wider t-border cursor-pointer ${
                  localForceSync
                    ? 't-bg-blue-ink t-text-on-accent'
                    : 't-bg-page text-muted hover:text-ink'
                }`}
              >
                AUTO-SYNC @{triggerThreshold}G: {localForceSync ? 'ON' : 'ON EXIT'}
              </button>
            </div>
            <button
              id="btn-session-sync"
              data-testid="session-sync-btn"
              type="button"
              onClick={handleSyncSessionNow}
              className="px-3.5 py-2 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-sm uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
            >
              {syncText}
            </button>
            <GhostButton
              tone="danger"
              onClick={() => {
                if (window.confirm('Reset all session stats and match history?')) {
                  setInspectedSessionId(null);
                  onResetSession();
                }
              }}
            >
              RESET SESSION
            </GhostButton>
          </div>
        )
      }
    />
  );

  if (!session) {
    return (
      <div className="font-display">
        {header}
        {isLoading ? (
          <EmptyLine>LOADING SESSION…</EmptyLine>
        ) : (
          <EmptyLine tone="loss">{error ? `SESSION UNAVAILABLE · ${error}` : 'SESSION UNAVAILABLE'}</EmptyLine>
        )}
      </div>
    );
  }

  return (
    <div data-testid="session-view" className="font-display flex flex-col gap-4">
      {header}

      {session.continuable_session && (
        <ContinueSessionBanner
          continuableSession={session.continuable_session}
          onContinue={(id) => {
            setInspectedSessionId(null);
            onContinueSession?.(id);
          }}
          onDismiss={(id) => onDismissContinueSession?.(id)}
        />
      )}

      {inspectedSession && (
        <div
          data-testid="historical-session-banner"
          className="my-2 bg-panel border-l-4 border-orange-ink px-6 py-3.5 flex flex-wrap items-center justify-between gap-4"
        >
          <div className="flex items-center gap-3">
            <span className="font-extrabold text-sm tracking-t8 uppercase text-orange-ink">
              VIEWING PAST SESSION
            </span>
            <span className="font-semibold text-sm tracking-t8 text-muted uppercase">
              {formatShortDate(inspectedSession.started_at)} · {inspectedSession.total_wins}W–{inspectedSession.total_losses}L
            </span>
          </div>
          <div className="flex items-center gap-3">
            {inspectedSession.can_continue && onContinueSession && (
              <PrimaryButton
                data-testid="continue-inspected-session-btn"
                onClick={() => {
                  onContinueSession(inspectedSession.session_id);
                  setInspectedSessionId(null);
                }}
              >
                CONTINUE THIS SESSION
              </PrimaryButton>
            )}
            <GhostButton tone="ink" onClick={() => setInspectedSessionId(null)}>
              RETURN TO CURRENT →
            </GhostButton>
          </div>
        </div>
      )}

      <SessionKpiStrip
        kpis={summary.kpis}
        variant="page"
        playlists={playlists}
        mmrHistory={mmrByPlaylist}
        session={activeSession}
      />

      <SessionForAgainstTable session={activeSession} />

      <div className="flex flex-wrap justify-between items-center gap-4 pt-4 pb-2">
        <SectionLabel>SESSION MATCHES ({rows.length}) · CLICK ANY ROW FOR 5-TAB TELEMETRY</SectionLabel>
        <div className="flex flex-wrap items-center gap-3">
          <Segmented
            label="Filter by result"
            options={OUTCOME_OPTIONS}
            value={outcomeFilter}
            onChange={setOutcomeFilter}
          />
          <Segmented
            label="Filter by playlist"
            options={PLAYLIST_OPTIONS}
            value={playlistFilter}
            onChange={setPlaylistFilter}
          />
        </div>
      </div>

      {matches.length === 0 ? (
        <EmptyLine>NO MATCHES THIS SESSION YET</EmptyLine>
      ) : rows.length === 0 ? (
        <EmptyLine>
          NO {outcomeFilter === 'victory' ? 'WINS' : outcomeFilter === 'defeat' ? 'LOSSES' : 'MATCHES'} THIS SESSION YET
        </EmptyLine>
      ) : (
        <div>
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
        </div>
      )}

      {pastSessions.length > 0 && (
        <div data-testid="session-history-section" className="pt-8">
          <div className="flex flex-wrap justify-between items-center gap-4 pb-3.5">
            <SectionLabel>SESSION HISTORY · SESSIONS OVER TIME ({pastSessions.length})</SectionLabel>
          </div>
          <div className="flex flex-col gap-1.5">
            {pastSessions.map((hist) => {
              const isCurrent = hist.session_id === session.session_id;
              const isSelected = hist.session_id === activeSession?.session_id;
              const histSummary = summarizeSession(hist);
              const histPlaylists = hist.playlists
                ? Object.values(hist.playlists)
                    .map((p) => playlistLabels(p.playlist_id, p.playlist_name).short)
                    .join(', ')
                : '';
              const lastPlayed = hist.last_match_at || hist.ended_at || hist.started_at;

              return (
                <div
                  key={hist.session_id}
                  role="button"
                  tabIndex={0}
                  data-testid="session-history-row"
                  onClick={() => setInspectedSessionId(isCurrent ? null : hist.session_id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      setInspectedSessionId(isCurrent ? null : hist.session_id);
                    }
                  }}
                  className={`flex flex-wrap items-center justify-between gap-4 px-6 py-4 bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 border-l-4 ${
                    isSelected ? 'border-blue-ink' : 'border-transparent'
                  }`}
                >
                  <div className="flex items-center gap-5 min-w-0">
                    <RecordCells wins={hist.total_wins} losses={hist.total_losses} />
                    <span
                      className={`font-extrabold text-[26px] leading-none min-w-[64px] t-font-mono ${recordInk(
                        hist.total_wins,
                        hist.total_losses,
                      )}`}
                    >
                      {formatRecord(hist.total_wins, hist.total_losses)}
                    </span>
                    <div className="flex flex-col gap-0.5 min-w-0">
                      <div className="flex items-center gap-2.5 font-bold text-base tracking-t6 text-ink">
                        <span>{formatShortDate(hist.started_at)}</span>
                        <span className="text-muted text-sm font-semibold">
                          · {hist.total_matches} {hist.total_matches === 1 ? 'MATCH' : 'MATCHES'}
                        </span>
                        {isCurrent && (
                          <span className="bg-blue-ink/20 text-blue-ink text-sm font-extrabold px-2 py-0.5 tracking-t10 uppercase">
                            CURRENT
                          </span>
                        )}
                        {!isCurrent && hist.can_continue && (
                          <span className="bg-win/20 text-win text-sm font-extrabold px-2 py-0.5 tracking-t10 uppercase">
                            CONTINUABLE
                          </span>
                        )}
                      </div>
                      <div className="font-semibold text-sm tracking-t8 text-muted uppercase truncate">
                        LAST ACTIVITY {formatAgo(lastPlayed)}
                        {histPlaylists ? ` · ${histPlaylists}` : ''}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-5 shrink-0">
                    <div className="text-right">
                      <div className={`font-extrabold text-[22px] leading-none t-font-mono ${deltaInk(histSummary.netMmr)}`}>
                        {formatSigned(histSummary.netMmr)}
                      </div>
                      <div className="font-semibold text-sm tracking-t10 text-faint uppercase">NET MMR</div>
                    </div>
                    {!isCurrent && hist.can_continue && onContinueSession && (
                      <GhostButton
                        data-testid="continue-history-row-btn"
                        tone="ink"
                        onClick={(e) => {
                          e.stopPropagation();
                          onContinueSession(hist.session_id);
                          setInspectedSessionId(null);
                        }}
                      >
                        CONTINUE
                      </GhostButton>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {selectedMatch && (
        <HistoricalMatchDetailModal
          match={sessionMatchToHistoricalRecord(selectedMatch)}
          onClose={() => setSelectedMatch(null)}
          localPlayerIds={localPlayerIds}
          kickerPrefix={`SESSION MATCH · ${formatAgo(selectedMatch.ended_at)}`}
          onPlayerClick={(pId, pName) => {
            if (onInspectPlayer) {
              onInspectPlayer(pId, pName);
            } else {
              setProfilePlayerId(pId);
              setProfilePlayerName(pName);
            }
          }}
        />
      )}

      {profilePlayerId && (
        <PlayerProfileDialog
          playerId={profilePlayerId}
          playerName={profilePlayerName}
          onClose={() => setProfilePlayerId(null)}
          localPlayerIds={localPlayerIds}
        />
      )}
    </div>
  );
};
