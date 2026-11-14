import React from 'react';
import { SessionResponse } from '../../types/api';
import {
  deltaInk,
  formatAgo,
  formatRecord,
  formatShortDate,
  formatSigned,
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
  onSelectSession: (sessionId: string | null) => void;
  onContinueSession?: (sessionId?: string) => void;
}

export const SessionHistoryList: React.FC<SessionHistoryListProps> = ({
  pastSessions,
  currentSessionId,
  activeSessionId,
  onSelectSession,
  onContinueSession,
}) => {
  if (pastSessions.length === 0) return null;

  return (
    <div data-testid="session-history-section" className="pt-8">
      <div className="flex flex-wrap justify-between items-center gap-4 pb-3.5">
        <SectionLabel>
          SESSION HISTORY · SESSIONS OVER TIME ({pastSessions.length})
        </SectionLabel>
      </div>
      <div className="flex flex-col gap-1.5">
        {pastSessions.map((hist) => {
          const isCurrent = hist.session_id === currentSessionId;
          const isSelected = hist.session_id === activeSessionId;
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
              onClick={() => onSelectSession(isCurrent ? null : hist.session_id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelectSession(isCurrent ? null : hist.session_id);
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
                {!isCurrent && hist.can_continue && onContinueSession && (
                  <GhostButton
                    data-testid="continue-history-row-btn"
                    tone="ink"
                    onClick={(e) => {
                      e.stopPropagation();
                      onContinueSession(hist.session_id);
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
