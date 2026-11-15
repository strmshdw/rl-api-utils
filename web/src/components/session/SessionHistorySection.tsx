import React, { useMemo } from 'react';
import { SessionResponse } from '../../types/api';
import {
  deltaInk,
  filterSessionByPlaylist,
  formatAgo,
  formatRecord,
  formatShortDate,
  formatSigned,
  normalizeActiveModeKey,
  playlistLabels,
  recordInk,
  summarizeSession,
} from '../../utils/broadcast';
import { GhostButton, PrimaryButton, RecordCells, SectionLabel } from '../common/ui';

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
            {continuableSession.total_wins}W–{continuableSession.total_losses}L ·{' '}
            {continuableSession.total_matches} {matchWord}
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

interface SessionHistoryListProps {
  pastSessions: SessionResponse[];
  currentSessionId: string;
  activeSessionId?: string;
  activePlaylist?: string;
  onSelectSession: (sessionId: string | null) => void;
  onContinueSession?: (sessionId?: string) => void;
}

export const SessionHistoryList: React.FC<SessionHistoryListProps> = ({
  pastSessions,
  currentSessionId,
  activeSessionId,
  activePlaylist,
  onSelectSession,
  onContinueSession,
}) => {
  const modeKey = normalizeActiveModeKey(activePlaylist);

  const visibleEntries = useMemo(() => {
    return pastSessions
      .map((raw) => {
        const scoped = filterSessionByPlaylist(raw, modeKey) ?? raw;
        const isCurrent = raw.session_id === currentSessionId;
        const isSelected = raw.session_id === activeSessionId;
        return { raw, scoped, isCurrent, isSelected };
      })
      .filter(({ scoped, isCurrent, isSelected }) => isCurrent || isSelected || scoped.total_matches > 0);
  }, [pastSessions, modeKey, currentSessionId, activeSessionId]);

  if (visibleEntries.length === 0) return null;

  return (
    <div data-testid="session-history-section" className="pt-8">
      <div className="flex flex-wrap justify-between items-center gap-4 pb-3.5">
        <SectionLabel>
          SESSION HISTORY · SESSIONS OVER TIME ({visibleEntries.length})
        </SectionLabel>
      </div>
      <div className="flex flex-col gap-1.5">
        {visibleEntries.map(({ raw, scoped, isCurrent, isSelected }) => {
          const histSummary = summarizeSession(scoped, modeKey);
          const histPlaylists = scoped.playlists
            ? Object.values(scoped.playlists)
                .map((p) => playlistLabels(p.playlist_id, p.playlist_name).short)
                .join(', ')
            : '';
          const lastPlayed = raw.last_match_at || raw.ended_at || raw.started_at;

          return (
            <div
              key={raw.session_id}
              role="button"
              tabIndex={0}
              data-testid="session-history-row"
              onClick={() => onSelectSession(isCurrent ? null : raw.session_id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelectSession(isCurrent ? null : raw.session_id);
                }
              }}
              className={`flex flex-wrap items-center justify-between gap-4 px-6 py-4 bg-panel hover:bg-panel-hover cursor-pointer transition-colors duration-120 border-l-4 ${
                isSelected ? 'border-blue-ink' : 'border-transparent'
              }`}
            >
              <div className="flex items-center gap-5 min-w-0">
                <RecordCells wins={scoped.total_wins} losses={scoped.total_losses} />
                <span
                  className={`font-extrabold text-[26px] leading-none min-w-[64px] t-font-mono ${recordInk(
                    scoped.total_wins,
                    scoped.total_losses,
                  )}`}
                >
                  {formatRecord(scoped.total_wins, scoped.total_losses)}
                </span>
                <div className="flex flex-col gap-0.5 min-w-0">
                  <div className="flex flex-wrap items-center gap-2.5 font-bold text-base tracking-t6 text-ink">
                    <span>{formatShortDate(raw.started_at)}</span>
                    <span className="text-muted text-sm font-semibold">
                      · {scoped.total_matches} {scoped.total_matches === 1 ? 'MATCH' : 'MATCHES'}
                    </span>
                    {isCurrent && (
                      <span className="bg-blue-ink/20 text-blue-ink text-sm font-extrabold px-2 py-0.5 tracking-t10 uppercase">
                        CURRENT
                      </span>
                    )}
                    {!isCurrent && raw.can_continue && (
                      <span className="bg-win/20 text-win text-sm font-extrabold px-2 py-0.5 tracking-t10 uppercase">
                        CONTINUABLE
                      </span>
                    )}
                  </div>
                  <div className="font-semibold text-sm tracking-t8 text-muted uppercase break-words">
                    LAST ACTIVITY {formatAgo(lastPlayed)}
                    {histPlaylists ? ` · ${histPlaylists}` : ''}
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-5 shrink-0">
                <div className="text-right">
                  <div
                    className={`font-extrabold text-[22px] leading-none t-font-mono ${deltaInk(
                      histSummary.netMmr,
                    )}`}
                  >
                    {formatSigned(histSummary.netMmr)}
                  </div>
                  <div className="font-semibold text-sm tracking-t10 text-faint uppercase">
                    NET MMR
                  </div>
                </div>
                {!isCurrent && raw.can_continue && onContinueSession && (
                  <GhostButton
                    data-testid="continue-history-row-btn"
                    tone="ink"
                    onClick={(e) => {
                      e.stopPropagation();
                      onContinueSession(raw.session_id);
                      onSelectSession(null);
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
  );
};
