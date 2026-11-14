import React, { useState } from 'react';
import { DashboardTab, SessionResponse } from '../../types/api';
import {
  deltaInk,
  formatMmr,
  formatRecord,
  formatSigned,
  matchesPlaylistFilter,
  playlistFilterToParam,
  playlistLabels,
} from '../../utils/broadcast';
import { isDemoMode } from '../../utils/sampleData';
import { StatusDot } from '../common/ui';
import { useDashboard } from '../../context/DashboardContext';
import { ContextFilterBar } from './ContextFilterBar';
import { ReplayLimitBanner } from './ReplayLimitBanner';
import { SettingsPanel } from './SettingsPanel';

export type { DashboardTab };

interface TopBarProps {
  activeTab: DashboardTab;
  onTabChange: (tab: DashboardTab) => void;
  inMatch: boolean;
  session: SessionResponse | null;
  /** Playlist whose MMR is shown on the right (current match, active mode, else most played). */
  playlistId?: number;
  /** Shown while a live match is on screen; opens the column customiser. */
  onOpenColumns?: () => void;
  onRefreshSession?: () => Promise<void> | void;
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
  playlistId,
  onOpenColumns,
  onRefreshSession,
}) => {
  const {
    activePlaylist,
    dateRange,
    customFrom,
    setCustomFrom,
    customTo,
    setCustomTo,
    showCustomCalendar,
    isDevBuild,
    appVersion,
    previewMode,
  } = useDashboard();

  const [settingsOpen, setSettingsOpen] = useState(false);

  const matchCount = session?.matches?.length ?? session?.total_matches ?? 0;
  const activeParam = playlistFilterToParam(activePlaylist);
  const numericActiveId = Number(activeParam);
  const isAllModes = activeParam === '';
  const playlist =
    !isAllModes && playlistId != null ? session?.playlists?.[String(playlistId)] : undefined;
  const groupPlaylists = !isAllModes
    ? Object.values(session?.playlists ?? {}).filter((p) =>
        matchesPlaylistFilter(p.playlist_id, activeParam),
      )
    : [];
  const groupWins = groupPlaylists.reduce((s, p) => s + p.wins, 0);
  const groupLosses = groupPlaylists.reduce((s, p) => s + p.losses, 0);
  const modeShortLabel =
    Number.isInteger(numericActiveId) && numericActiveId > 0
      ? playlistLabels(numericActiveId).short
      : activeParam === 'extra'
      ? 'EXTRA'
      : activeParam === 'casual'
      ? 'CASUAL'
      : activeParam.toUpperCase();
  const activeDemo = isDevBuild && (previewMode || isDemoMode());
  const overlayHref = activeDemo ? '/?mode=overlay&demo=true' : '/?mode=overlay';

  return (
    <>
      {/* REPLAY HISTORY LIMIT WARNING / ERROR BANNER */}
      <ReplayLimitBanner session={session} onRefreshSession={onRefreshSession} />

      {/* 2-TIER STICKY ARCHITECTURAL MASTHEAD (Tier 1 Nav + Tier 2 Context Filter Toolbar) */}
      <header
        data-testid="app-header"
        className="sticky top-0 z-30 t-bg-page bg-page border-b t-border border-line font-display"
      >
        {/* Tier 1: Primary Navigation, Session/Playlist MMR Readout, Overlay & Settings */}
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
              <span className="text-[11px] t-font-mono cartouche-mono px-1.5 py-0.5 hidden md:inline whitespace-nowrap">
                v{appVersion}
                {isDevBuild ? '-dev' : ''}
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

          {/* Right: Active-Mode Session Status Pill, Overlay Link & Settings Button */}
          <div className="flex flex-wrap items-center gap-1.5 text-xs shrink-0 ml-auto">
            {(session || playlist) && (
              <div className="hidden sm:flex items-center gap-2.5 px-2.5 py-1 surface-plaque t-border font-semibold text-xs tracking-wider text-muted whitespace-nowrap">
                {isAllModes && session ? (
                  <span>
                    ALL MODES · SESSION{' '}
                    <span className="text-ink-strong t-font-mono">
                      {formatRecord(session.total_wins, session.total_losses)}
                    </span>
                  </span>
                ) : playlist &&
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

        {/* Tier 2: Unified Mode Switcher + Contextual Analytics Toolbar */}
        <ContextFilterBar
          activeTab={activeTab}
          inMatch={inMatch}
          onOpenColumns={onOpenColumns}
        />

        {/* On-Demand Custom Date Range Picker Strip (Only visible when Custom Range is active) */}
        {(showCustomCalendar || dateRange === 'custom') && (
          <div
            id="custom-calendar-bar"
            data-testid="custom-calendar-bar"
            className="border-t border-line surface-plaque"
          >
            <div className="max-w-[1680px] mx-auto px-4 sm:px-6 py-1.5 flex flex-wrap items-center gap-2 text-xs">
              <span className="t-font-display font-bold uppercase tracking-wider t-text-muted">
                CUSTOM DATE FILTER:
              </span>
              <label htmlFor="custom-date-from" className="text-xs t-font-display font-bold t-text-muted uppercase">
                FROM
              </label>
              <input
                id="custom-date-from"
                type="date"
                value={customFrom}
                onChange={(e) => setCustomFrom(e.target.value)}
                className="t-bg-page t-text-strong t-border px-2 py-0.5 text-xs t-font-mono t-radius focus:outline-none cursor-pointer"
              />
              <span className="t-text-muted font-bold">→</span>
              <label htmlFor="custom-date-to" className="text-xs t-font-display font-bold t-text-muted uppercase">
                TO
              </label>
              <input
                id="custom-date-to"
                type="date"
                value={customTo}
                onChange={(e) => setCustomTo(e.target.value)}
                className="t-bg-page t-text-strong t-border px-2 py-0.5 text-xs t-font-mono t-radius focus:outline-none cursor-pointer"
              />
            </div>
          </div>
        )}
      </header>

      {/* DEDICATED SETTINGS PANEL MODAL */}
      <SettingsPanel
        isOpen={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        inMatch={inMatch}
        onSwitchToLive={() => {
          if (activeTab !== 'live') onTabChange('live');
        }}
      />
    </>
  );
};
