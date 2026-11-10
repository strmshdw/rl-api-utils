import React, { useState } from 'react';
import { SessionResponse } from '../../types/api';
import { deltaInk, formatMmr, formatRecord, formatSigned, playlistLabels } from '../../utils/broadcast';
import { StatusDot } from '../common/ui';
import { DateRangePreset, useDashboard } from '../../context/DashboardContext';
import { ReplayLimitBanner } from './ReplayLimitBanner';
import { SettingsPanel } from './SettingsPanel';

export type DashboardTab = 'live' | 'session' | 'history' | 'players';

interface TopBarProps {
  activeTab: DashboardTab;
  onTabChange: (tab: DashboardTab) => void;
  inMatch: boolean;
  session: SessionResponse | null;
  /** Playlist whose MMR is shown on the right (current match, else latest session match). */
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
    statMode,
    setStatMode,
    dateRange,
    setDateRange,
    customFrom,
    setCustomFrom,
    customTo,
    setCustomTo,
    showCustomCalendar,
    setShowCustomCalendar,
    isDevBuild,
    appVersion,
    previewMode,
  } = useDashboard();

  const [settingsOpen, setSettingsOpen] = useState(false);

  const matchCount = session?.matches?.length ?? 0;
  const playlist = playlistId != null ? session?.playlists?.[String(playlistId)] : undefined;

  return (
    <>
      {/* REPLAY HISTORY LIMIT WARNING / ERROR BANNER */}
      <ReplayLimitBanner session={session} onRefreshSession={onRefreshSession} />

      {/* STICKY ARCHITECTURAL MASTHEAD — Compact Single-Row Broadcast Bar */}
      <header
        data-testid="app-header"
        className="sticky top-0 z-30 t-bg-page bg-page border-b t-border border-line font-display"
      >
        <div className="max-w-[1680px] mx-auto px-4 sm:px-6 py-2 flex flex-wrap xl:flex-nowrap items-center justify-between gap-3">
          {/* Left: Brand Mark + Primary Navigation Tabs */}
          <div className="flex items-center gap-4 min-w-0 shrink-0">
            <div className="flex items-center gap-2 select-none">
              <span
                aria-hidden="true"
                className="inline-flex items-center justify-center w-6 h-6 surface-plaque border border-outline text-xs font-extrabold t-text-blue"
              >
                ◆
              </span>
              <span className="t-font-display font-bold text-xl tracking-widest t-text-strong whitespace-nowrap">
                RL<span className="t-text-orange text-orange-ink">/</span>SYNC
              </span>
              <span className="text-[11px] t-font-mono cartouche-mono px-1.5 py-0.5 hidden sm:inline whitespace-nowrap">
                v{appVersion}
                {isDevBuild ? '-dev' : ''}
              </span>
            </div>

            <nav aria-label="Dashboard" className="flex items-center gap-0.5 font-bold text-sm tracking-wider">
              {TABS.map((tab) => {
                const active = tab.id === activeTab;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    id={`nav-${tab.id}`}
                    aria-current={active ? 'page' : undefined}
                    onClick={() => onTabChange(tab.id)}
                    className={`px-3 py-1 t-font-display font-bold text-sm uppercase tracking-widest flex items-center gap-1.5 border-b-2 transition-colors whitespace-nowrap cursor-pointer ${
                      active
                        ? 't-border-blue border-blue-ink t-text-strong text-ink-strong'
                        : 'border-transparent t-text-muted text-muted hover:text-ink'
                    }`}
                  >
                    {tab.id === 'live' && (
                      <StatusDot className={inMatch ? 'bg-loss animate-pulse' : 'bg-faint'} />
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

          {/* Right: Essential Data Scope, Live Session Summary, Columns, Overlay & Settings Gear Button */}
          <div className="flex flex-wrap xl:flex-nowrap items-center gap-1.5 text-xs shrink-0">
            {/* Global AVG / G vs TOTALS Toggle */}
            <div className="flex t-border t-radius overflow-hidden" role="group" aria-label="Stat aggregation mode">
              <button
                type="button"
                id="btn-stat-avg"
                onClick={() => setStatMode('avg')}
                className={`px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer whitespace-nowrap ${
                  statMode === 'avg'
                    ? 't-bg-blue-ink t-text-on-accent'
                    : 'surface-plaque t-text-muted hover:opacity-80'
                }`}
              >
                AVG / G
              </button>
              <button
                type="button"
                id="btn-stat-total"
                onClick={() => setStatMode('total')}
                className={`px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer whitespace-nowrap ${
                  statMode === 'total'
                    ? 't-bg-blue-ink t-text-on-accent'
                    : 'surface-plaque t-text-muted hover:opacity-80'
                }`}
              >
                TOTALS
              </button>
            </div>

            {/* Global Timeframe Selector */}
            <div className="flex items-center gap-1.5 surface-plaque t-border px-2 py-0.5 t-radius whitespace-nowrap">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider t-text-muted hidden 2xl:inline">
                TIMEFRAME:
              </span>
              <select
                id="global-date-range"
                aria-label="Timeframe"
                value={dateRange}
                onChange={(e) => setDateRange(e.target.value as DateRangePreset)}
                className="t-bg-page t-text-strong text-xs t-font-display font-bold uppercase tracking-wider px-1.5 py-0.5 t-border focus:outline-none cursor-pointer"
              >
                <option value="all">ALL TIME</option>
                <option value="today">TODAY / SESSION</option>
                <option value="7d">LAST 7 DAYS</option>
                <option value="30d">LAST 30 DAYS</option>
                <option value="custom">CUSTOM RANGE...</option>
              </select>
              <button
                type="button"
                id="btn-cal-toggle"
                onClick={() => {
                  if (!showCustomCalendar && dateRange !== 'custom') {
                    setDateRange('custom');
                  } else {
                    setShowCustomCalendar((prev) => !prev);
                  }
                }}
                className="px-1.5 py-0.5 t-bg-page t-border t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:opacity-80 cursor-pointer whitespace-nowrap"
                title="Pick custom start & end dates"
              >
                ◆ DATES
              </button>
            </div>

            {/* Session W-L & Active Playlist MMR Readout */}
            {(session || playlist) && (
              <div className="hidden lg:flex items-center gap-2.5 px-2.5 py-1 surface-plaque t-border font-semibold text-xs tracking-wider text-muted whitespace-nowrap">
                {session && (
                  <span>
                    SESSION{' '}
                    <span className="text-ink-strong t-font-mono">
                      {formatRecord(session.total_wins, session.total_losses)}
                    </span>
                  </span>
                )}
                {playlist && (
                  <span>
                    {playlistLabels(playlist.playlist_id, playlist.playlist_name).short}{' '}
                    <span className="text-ink-strong t-font-mono">{formatMmr(playlist.current_mmr)}</span>{' '}
                    <span className={`${deltaInk(playlist.mmr_delta)} t-font-mono`}>
                      {formatSigned(playlist.mmr_delta)}
                    </span>
                  </span>
                )}
              </div>
            )}

            {onOpenColumns && (
              <button type="button" onClick={onOpenColumns} className={GHOST_LINK}>
                COLUMNS
              </button>
            )}

            <a href="/?mode=overlay" target="_blank" rel="noopener noreferrer" className={GHOST_LINK}>
              OVERLAY ↗
            </a>

            {/* Gear Icon / Settings Button */}
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
        onOpenColumns={onOpenColumns}
      />
    </>
  );
};
