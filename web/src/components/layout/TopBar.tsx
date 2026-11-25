import React, { useEffect, useState } from 'react';
import { DashboardTab, SessionResponse } from '../../types/api';
import {
  deltaInk,
  filterSessionByPlaylist,
  formatMmr,
  formatRecord,
  formatSigned,
  matchesPlaylistFilter,
  normalizeActiveModeKey,
  playlistLabels,
} from '../../utils/broadcast';
import { isDemoMode } from '../../utils/sampleData';
import { useBallchasingSync } from '../../hooks/useBallchasingSync';
import { StatusDot } from '../common/ui';
import { useDashboard } from '../../context/DashboardContext';
import { ReplayLimitBanner } from './ReplayLimitBanner';
import { SettingsPanel } from './SettingsPanel';

export type { DashboardTab };

interface TopBarProps {
  activeTab: DashboardTab;
  onTabChange: (tab: DashboardTab) => void;
  inMatch: boolean;
  session: SessionResponse | null;
  scopedSession?: SessionResponse | null;
  /** Mode key ('11', '13', '10', etc.) governing the session status pill & count badge. */
  statusMode?: string;
  /** Playlist whose MMR is shown on the right (current match, active mode, else most played). */
  playlistId?: number;
  onRefreshSession?: () => Promise<void> | void;
  onOpenColumns?: () => void;
}

const TABS: ReadonlyArray<{ id: DashboardTab; label: string }> = [
  { id: 'live', label: 'LIVE' },
  { id: 'session', label: 'SESSION' },
  { id: 'history', label: 'HISTORY' },
  { id: 'players', label: 'PLAYERS' },
];

const GHOST_LINK =
  'border border-outline px-2.5 py-1 text-xs font-bold text-muted hover:text-ink hover:border-ink transition-colors duration-120 whitespace-nowrap t-radius';

export const TopBar: React.FC<TopBarProps> = ({
  activeTab,
  onTabChange,
  inMatch,
  session,
  scopedSession,
  statusMode,
  playlistId,
  onRefreshSession,
  onOpenColumns,
}) => {
  const {
    areaFilters,
    replayLimitPreview,
    setReplayLimitPreview,
    isDevBuild,
    previewMode,
  } = useDashboard();

  const [settingsOpen, setSettingsOpen] = useState(false);
  const [clearedPendingSnapshot, setClearedPendingSnapshot] = useState<{
    sessionId: string | undefined;
    count: number;
    preview: string;
  } | null>(null);

  useEffect(() => {
    if (
      clearedPendingSnapshot !== null &&
      (session?.session_id !== clearedPendingSnapshot.sessionId ||
        (session?.pending_count ?? 0) !== clearedPendingSnapshot.count ||
        replayLimitPreview !== clearedPendingSnapshot.preview)
    ) {
      setClearedPendingSnapshot(null);
    }
  }, [clearedPendingSnapshot, session?.session_id, session?.pending_count, replayLimitPreview]);

  const markPendingClearedOptimistically = () => {
    setClearedPendingSnapshot({
      sessionId: session?.session_id,
      count: session?.pending_count ?? 0,
      preview: replayLimitPreview,
    });
  };

  const { isSyncing, triggerSync } = useBallchasingSync({
    onSyncComplete: () => {
      markPendingClearedOptimistically();
      void onRefreshSession?.();
    },
  });

  const maxHostedReplays = session?.max_hosted_replays || 20;
  const triggerThreshold =
    session?.trigger_threshold && session.trigger_threshold > 0 ? session.trigger_threshold : 15;
  const isOptimisticallyCleared =
    clearedPendingSnapshot !== null &&
    session?.session_id === clearedPendingSnapshot.sessionId &&
    (session?.pending_count ?? 0) === clearedPendingSnapshot.count &&
    replayLimitPreview === clearedPendingSnapshot.preview;

  const activePreview = isDevBuild ? replayLimitPreview : 'auto';
  let pendingCount = isOptimisticallyCleared ? 0 : (session?.pending_count ?? 0);
  if (!isOptimisticallyCleared) {
    if (activePreview === 'warn') pendingCount = Math.max(triggerThreshold, 16);
    else if (activePreview === 'error') pendingCount = maxHostedReplays;
    else if (activePreview === 'ok') pendingCount = 0;
  }
  const isOverSyncWarning = pendingCount >= triggerThreshold;
  const isOverSyncLimit = pendingCount >= maxHostedReplays;

  const handleHeaderSyncNow = async () => {
    if (isSyncing) return;
    await triggerSync({ includeReplaySync: true });
    markPendingClearedOptimistically();
    if (isDevBuild && replayLimitPreview !== 'auto') {
      setReplayLimitPreview('ok');
    }
    await onRefreshSession?.();
  };

  const fallbackMode =
    activeTab === 'live' ? areaFilters.live.playlist : areaFilters.session.playlist;
  const activeParam = normalizeActiveModeKey(statusMode ?? fallbackMode);
  const effectiveScopedSession = scopedSession ?? filterSessionByPlaylist(session, activeParam);
  const matchCount =
    effectiveScopedSession?.matches?.length ?? effectiveScopedSession?.total_matches ?? 0;
  const numericActiveId = Number(activeParam);
  const targetPlaylistId =
    playlistId != null && matchesPlaylistFilter(playlistId, activeParam)
      ? playlistId
      : Number.isInteger(numericActiveId) && numericActiveId > 0
      ? numericActiveId
      : undefined;
  const playlist =
    targetPlaylistId != null
      ? effectiveScopedSession?.playlists?.[String(targetPlaylistId)] ??
        session?.playlists?.[String(targetPlaylistId)]
      : undefined;
  const groupPlaylists = Object.values(
    effectiveScopedSession?.playlists ?? session?.playlists ?? {},
  ).filter((p) => matchesPlaylistFilter(p.playlist_id, activeParam));
  const groupWins = groupPlaylists.reduce((s, p) => s + p.wins, 0);
  const groupLosses = groupPlaylists.reduce((s, p) => s + p.losses, 0);
  const modeShortLabel =
    Number.isInteger(numericActiveId) && numericActiveId > 0
      ? playlistLabels(numericActiveId).short
      : activeParam === 'casual'
      ? 'CASUAL'
      : activeParam === 'private'
      ? 'PRIVATE'
      : activeParam.toUpperCase();
  const activeDemo = isDevBuild && (previewMode || isDemoMode());
  const overlayHref = activeDemo ? '/?mode=overlay&demo=true' : '/?mode=overlay';

  return (
    <>
      {/* REPLAY HISTORY LIMIT WARNING / ERROR BANNER */}
      <ReplayLimitBanner session={session} onRefreshSession={onRefreshSession} />

      {/* SLIM SINGLE-TIER STICKY MASTHEAD (Per-Area Filters Live Locally Inside Each View) */}
      <header
        data-testid="app-header"
        className="sticky top-0 z-30 t-bg-page bg-page border-b t-border border-line font-display"
      >
        <div className="max-w-[1680px] mx-auto px-4 sm:px-6 py-2 flex flex-wrap 2xl:flex-nowrap items-center justify-between gap-x-3 gap-y-1.5">
          {/* Left: Brand Mark + Single-Row Primary Navigation Tabs */}
          <div className="flex items-center gap-3 sm:gap-4 min-w-0 max-w-full">
            <div className="flex items-center gap-2 select-none shrink-0">
              <span
                aria-hidden="true"
                className="inline-flex items-center justify-center w-6 h-6 surface-plaque border border-outline text-xs font-extrabold t-text-blue"
              >
                ◆
              </span>
              <span className="t-font-display font-bold text-xl tracking-widest t-text-strong whitespace-nowrap">
                RL<span className="t-text-orange text-orange-ink">/</span>SYNC
              </span>
            </div>

            <nav
              aria-label="Dashboard"
              className="dashboard-nav-scroll flex flex-nowrap overflow-x-auto items-center gap-0.5 font-bold text-sm tracking-wider min-w-0"
            >
              {TABS.map((tab) => {
                const active = tab.id === activeTab;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    id={`nav-${tab.id}`}
                    aria-current={active ? 'page' : undefined}
                    onClick={() => onTabChange(tab.id)}
                    className={`px-2.5 sm:px-3 py-1 flex-none t-font-display font-bold text-sm uppercase tracking-widest flex items-center gap-1.5 border-b-2 transition-colors whitespace-nowrap cursor-pointer ${
                      active
                        ? 't-border-blue border-blue-ink t-text-strong text-ink-strong'
                        : 'border-transparent t-text-muted text-muted hover:text-ink'
                    }`}
                  >
                    {tab.id === 'live' && inMatch && (
                      <StatusDot className="bg-loss animate-pulse" />
                    )}
                    {tab.label}
                    {tab.id === 'session' && matchCount > 0 && (
                      <span className="text-xs t-font-mono px-1.5 py-0.5 surface-plaque t-border t-text-ink2 text-count">
                        {matchCount}
                      </span>
                    )}
                  </button>
                );
              })}
            </nav>
          </div>

          {/* Right: Active-Mode Session Status Pill + Sync Now, Overlay Link & Settings Button */}
          <div className="flex flex-wrap items-center gap-1.5 text-xs shrink-0 ml-auto">
            {(session || playlist) && (
              <div
                data-testid="header-session-pill"
                data-sync-warning={isOverSyncWarning ? 'true' : 'false'}
                className={`hidden sm:flex items-center gap-2 px-2.5 py-1 surface-plaque font-semibold text-xs tracking-wider text-muted whitespace-nowrap t-radius transition-colors ${
                  isOverSyncWarning
                    ? 'border-2 border-[#ff3b4e] shadow-[0_0_0_1px_rgba(255,59,78,0.35)]'
                    : 't-border'
                }`}
              >
                {playlist &&
                (Number.isInteger(numericActiveId) && numericActiveId > 0
                  ? true
                  : groupPlaylists.length === 1) ? (
                  <span>
                    {playlistLabels(playlist.playlist_id, playlist.playlist_name).short}{' '}
                    <span className="text-ink-strong t-font-mono">
                      {formatMmr(playlist.current_mmr)}
                    </span>{' '}
                    <span className={`${deltaInk(playlist.mmr_delta)} t-font-mono`}>
                      {formatSigned(playlist.mmr_delta)}
                    </span>
                    <span className="text-muted t-font-mono">
                      {' '}
                      ·{' '}
                      {playlist.wins + playlist.losses > 0
                        ? formatRecord(playlist.wins, playlist.losses)
                        : '0–0'}
                    </span>
                  </span>
                ) : (
                  <span>
                    {modeShortLabel}{' '}
                    <span className="text-muted t-font-mono">
                      ·{' '}
                      {groupWins + groupLosses > 0
                        ? formatRecord(groupWins, groupLosses)
                        : '0–0'}
                    </span>
                  </span>
                )}

                {isOverSyncWarning && (
                  <span
                    data-testid="header-sync-warning"
                    role="status"
                    className="px-1.5 py-0.5 bg-[#c81624] text-white font-extrabold text-[11px] t-font-mono uppercase tracking-wider leading-none"
                    title={`${pendingCount} of ${maxHostedReplays} undownloaded Rocket League replays (warning threshold: ${triggerThreshold})`}
                  >
                    {isOverSyncLimit
                      ? `⚠ SYNC CAP ${pendingCount}/${maxHostedReplays}`
                      : `⚠ SYNC WARN ${pendingCount}/${maxHostedReplays}`}
                  </span>
                )}

                <button
                  type="button"
                  data-testid="header-sync-now-btn"
                  disabled={isSyncing}
                  onClick={handleHeaderSyncNow}
                  className={`px-2 py-0.5 t-font-display font-extrabold text-[11px] uppercase tracking-wider transition-colors cursor-pointer disabled:opacity-60 t-radius ${
                    isOverSyncWarning
                      ? 'bg-[#c81624] text-white border border-[#ff6b78] hover:bg-[#a8111d]'
                      : 't-bg-page border border-outline t-text-strong hover:border-blue-ink hover:t-text-blue'
                  }`}
                  title="Download pending Rocket League replays and sync with Ballchasing now"
                >
                  {isSyncing ? '⟳ SYNCING…' : '⟳ SYNC NOW'}
                </button>
              </div>
            )}

            <a href={overlayHref} target="_blank" rel="noopener noreferrer" className={GHOST_LINK}>
              OVERLAY ↗
            </a>

            <button
              type="button"
              id="btn-settings"
              data-testid="settings-btn"
              aria-label="Open Settings"
              aria-expanded={settingsOpen}
              onClick={() => setSettingsOpen(true)}
              className={`inline-flex items-center gap-1.5 px-2.5 py-1 border t-radius t-font-display font-bold text-xs uppercase tracking-wider transition-colors duration-120 cursor-pointer whitespace-nowrap ${
                settingsOpen || (isDevBuild && previewMode)
                  ? 'border-blue-ink t-text-strong surface-plaque'
                  : 'border-outline text-muted hover:text-ink hover:border-ink surface-plaque'
              }`}
              title="Open Studio & Telemetry Settings (Themes, Dark/Light, Text Scale, Sync Banner & Diagnostics)"
            >
              <svg
                aria-hidden="true"
                viewBox="0 0 20 20"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.75"
                className="w-3.5 h-3.5 shrink-0"
              >
                <circle cx="10" cy="10" r="3" />
                <path
                  strokeLinecap="square"
                  d="M10 1.75v2M10 16.25v2M1.75 10h2M16.25 10h2M4.17 4.17l1.41 1.41M14.42 14.42l1.41 1.41M15.83 4.17l-1.41 1.41M5.58 14.42l-1.41 1.41"
                />
              </svg>
              <span>SETTINGS</span>
              {isDevBuild && previewMode && (
                <span
                  aria-label="Preview Mode Active"
                  className="inline-block w-2 h-2 rounded-full bg-[#deb052] shrink-0"
                />
              )}
            </button>
          </div>
        </div>
      </header>

      {/* DEDICATED SETTINGS PANEL MODAL */}
      <SettingsPanel
        isOpen={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        inMatch={inMatch}
        onSwitchToLive={() => {
          if (activeTab !== 'live') onTabChange('live');
        }}
        onOpenColumns={onOpenColumns}
      />
    </>
  );
};
