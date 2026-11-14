import React, { useEffect, useMemo, useState } from 'react';
import { CurrentMatchResponse, SessionMatchDetail, SessionResponse } from '../../types/api';
import {
  chronological,
  formatAgo,
  formatElapsed,
  formatShortDate,
  onlineIdOf,
  summarizeSession,
} from '../../utils/broadcast';
import { EmptyLine, GhostButton, PageHeader, PrimaryButton } from '../common/ui';
import { SessionKpiStrip, SessionForAgainstTable } from './SessionKpis';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { sessionMatchToHistoricalRecord } from '../historical/historyData';
import { PlayerProfileDialog } from '../players/PlayerProfileDialog';
import { useMMRHistory } from '../../hooks/useMMRHistory';
import { useBallchasingSync } from '../../hooks/useBallchasingSync';
import { ContinueSessionBanner, SessionHistoryList } from './SessionHistorySection';
import {
  OutcomeFilter,
  SessionMatchesSection,
  SessionPlaylistFilter,
} from './SessionMatchesSection';

export { ContinueSessionBanner, type ContinueSessionBannerProps } from './SessionHistorySection';

interface SessionViewProps {
  session: SessionResponse | null;
  pastSessions?: SessionResponse[];
  liveMatch?: CurrentMatchResponse | null;
  isLoading: boolean;
  error: string | null;
  onResetSession: () => void;
  onRefreshSession?: () => void;
  onContinueSession?: (sessionId?: string) => void;
  onDismissContinueSession?: (sessionId?: string) => void;
  onInspectPlayer?: (playerId: string, playerName?: string) => void;
}

export const SessionView: React.FC<SessionViewProps> = ({
  session,
  pastSessions = [],
  liveMatch,
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
  const [clearedPendingSnapshot, setClearedPendingSnapshot] = useState<{
    sessionId: string | undefined;
    count: number;
  } | null>(null);
  const [localThreshold, setLocalThreshold] = useState<number>(session?.trigger_threshold ?? 15);
  const [localForceSync, setLocalForceSync] = useState<boolean>(
    session?.force_sync_on_trigger ?? false,
  );
  const [profilePlayerId, setProfilePlayerId] = useState<string | null>(null);
  const [profilePlayerName, setProfilePlayerName] = useState<string | undefined>(undefined);
  const { byPlaylist: mmrByPlaylist } = useMMRHistory();

  useEffect(() => {
    if (session?.trigger_threshold && session.trigger_threshold > 0) {
      setLocalThreshold(session.trigger_threshold);
    }
  }, [session?.trigger_threshold]);

  useEffect(() => {
    if (typeof session?.force_sync_on_trigger === 'boolean') {
      setLocalForceSync(session.force_sync_on_trigger);
    }
  }, [session?.force_sync_on_trigger]);

  useEffect(() => {
    if (
      clearedPendingSnapshot !== null &&
      (session?.session_id !== clearedPendingSnapshot.sessionId ||
        (session?.pending_count ?? 0) !== clearedPendingSnapshot.count)
    ) {
      setClearedPendingSnapshot(null);
    }
  }, [clearedPendingSnapshot, session?.session_id, session?.pending_count]);

  const markPendingClearedOptimistically = () => {
    setClearedPendingSnapshot({
      sessionId: session?.session_id,
      count: session?.pending_count ?? 0,
    });
  };

  const { isSyncing, syncStatus, syncMessage, triggerSync } = useBallchasingSync({
    onSyncComplete: () => {
      markPendingClearedOptimistically();
      onRefreshSession?.();
    },
  });

  const maxHostedReplays = session?.max_hosted_replays || 20;
  const isOptimisticallyCleared =
    clearedPendingSnapshot !== null &&
    session?.session_id === clearedPendingSnapshot.sessionId &&
    (session?.pending_count ?? 0) === clearedPendingSnapshot.count;
  const pendingCount = isOptimisticallyCleared ? 0 : (session?.pending_count ?? 0);
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
    await triggerSync({ includeReplaySync: true });
    markPendingClearedOptimistically();
    onRefreshSession?.();
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
    ? `PAST SESSION · ${formatShortDate(inspectedSession.started_at)} · ${matches.length} ${
        matches.length === 1 ? 'MATCH' : 'MATCHES'
      }`
    : session
    ? `STARTED ${formatElapsed(session.started_at)} AGO · ${matches.length} ${
        matches.length === 1 ? 'MATCH' : 'MATCHES'
      }`
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
              <label
                htmlFor="session-threshold-select"
                className="text-muted font-bold uppercase text-sm"
              >
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
              disabled={isSyncing}
              onClick={handleSyncSessionNow}
              className="px-3.5 py-2 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-sm uppercase tracking-wider hover:opacity-90 disabled:opacity-60 t-radius cursor-pointer"
            >
              {isSyncing
                ? `SYNCING · ${syncStatus?.synced_matches ?? 0} MATCHES`
                : syncMessage
                ? '✓ SYNCED · ⟳ SYNC NOW'
                : '⟳ SYNC NOW'}
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
          <EmptyLine tone="loss">
            {error ? `SESSION UNAVAILABLE · ${error}` : 'SESSION UNAVAILABLE'}
          </EmptyLine>
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
              {formatShortDate(inspectedSession.started_at)} · {inspectedSession.total_wins}W–
              {inspectedSession.total_losses}L
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

      <SessionMatchesSection
        totalMatchCount={matches.length}
        rows={rows}
        outcomeFilter={outcomeFilter}
        onOutcomeChange={setOutcomeFilter}
        playlistFilter={playlistFilter}
        onPlaylistChange={setPlaylistFilter}
        onOpenMatch={setSelectedMatch}
      />

      <SessionHistoryList
        pastSessions={pastSessions}
        currentSessionId={session.session_id}
        activeSessionId={activeSession?.session_id}
        onSelectSession={setInspectedSessionId}
        onContinueSession={onContinueSession}
      />

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
          liveMatch={liveMatch}
        />
      )}
    </div>
  );
};
