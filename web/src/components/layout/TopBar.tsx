import React from 'react';
import { SessionResponse } from '../../types/api';
import { deltaInk, formatMmr, formatRecord, formatSigned, playlistLabels } from '../../utils/broadcast';
import { StatusDot } from '../common/ui';
import {
  DateRangePreset,
  ReplayLimitPreviewState,
  THEME_STYLES,
  ThemeStyleKey,
  useDashboard,
} from '../../context/DashboardContext';
import { ReplayLimitBanner } from './ReplayLimitBanner';

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

const THEME_OPTIONS: ReadonlyArray<{ id: ThemeStyleKey; label: string }> = [
  { id: 'broadcast', label: '01 · Deco Monolith: Woodblock (Default)' },
  { id: 'tactical', label: '02 · Deco Monolith: Lacquer & Brass' },
  { id: 'swiss', label: '03 · Deco Monolith: Carved Inkstone' },
  { id: 'arashi', label: '04 · Deco Monolith: Warm Parchment (Light)' },
  { id: 'arena', label: '05 · Glassmorphic Arena (Frosted HUD)' },
  { id: 'carbon', label: '06 · Carbon Paddock (Motorsport Telemetry)' },
  { id: 'synthwave', label: '07 · Mannfield Night (Neon Violet)' },
  { id: 'editorial', label: '08 · Editorial Gazette (Fraunces Serif)' },
  { id: 'nordic', label: '09 · Nordic Frost (Arctic Slate & Ice)' },
  { id: 'terminal', label: '10 · Retro CRT Phosphor (Monospace)' },
  { id: 'solar', label: '11 · Utopia Daylight (Warm Sand & Blue)' },
  { id: 'industrial', label: '12 · Cobalt Blueprint (CAD Grid)' },
  { id: 'oled', label: '13 · Pure OLED Pitch (True Zero Black)' },
];

const GHOST_LINK =
  'border border-outline px-2.5 py-1 text-sm font-bold text-muted hover:text-ink hover:border-ink transition-colors duration-120 whitespace-nowrap t-radius';

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
    themeStyle,
    colorMode,
    setTheme,
    fontScale,
    setFontScale,
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
    replayLimitPreview,
    setReplayLimitPreview,
    replayBannerDismissed,
    setReplayBannerDismissed,
    liveModeOverride,
    setLiveModeOverride,
    isDevBuild,
    appVersion,
    previewMode,
    setPreviewMode,
  } = useDashboard();

  const matchCount = session?.matches?.length ?? 0;
  const playlist = playlistId != null ? session?.playlists?.[String(playlistId)] : undefined;

  return (
    <>
      {/* REPLAY HISTORY LIMIT WARNING / ERROR BANNER */}
      <ReplayLimitBanner session={session} onRefreshSession={onRefreshSession} />

      {/* STICKY ARCHITECTURAL MASTHEAD (Quiet Chrome on --t-page) */}
      <header
        data-testid="app-header"
        className="sticky top-0 z-30 t-bg-page bg-page border-b t-border border-line font-display"
      >
        {/* Row 1: Primary Brand, Navigation Tabs & Studio Theme Controls on a Single Balanced Row */}
        <div className="max-w-[1680px] mx-auto px-4 sm:px-6 py-1.5 flex flex-wrap xl:flex-nowrap items-center justify-between gap-2">
          {/* Left: Brand Mark + Primary Navigation */}
          <div className="flex items-center gap-3.5 min-w-0 shrink-0">
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
                    className={`px-2.5 py-1 t-font-display font-bold text-sm uppercase tracking-widest flex items-center gap-1.5 border-b-2 transition-colors whitespace-nowrap ${
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

          {/* Right: Theme Expression Selector, Dark/Light Polarity, Scale, Sync Banner Restore & Dev Preview Controls */}
          <div className="flex flex-wrap xl:flex-nowrap items-center gap-1.5 text-xs shrink-0">
            {/* Design Style Switcher */}
            <div className="flex items-center gap-1">
              <label
                htmlFor="theme-select"
                className="t-text-muted t-font-display font-bold uppercase tracking-wider text-xs hidden 2xl:inline"
              >
                STYLE:
              </label>
              <select
                id="theme-select"
                value={themeStyle}
                onChange={(e) => {
                  const next = e.target.value as ThemeStyleKey;
                  if (!THEME_STYLES[next]) return;
                  if (next === 'arashi') {
                    setTheme(next, 'light');
                  } else if (next === 'broadcast' || next === 'tactical' || next === 'swiss') {
                    setTheme(next, 'dark');
                  } else {
                    setTheme(next, colorMode);
                  }
                }}
                className="surface-plaque t-text-strong t-border px-2 py-1 text-xs font-semibold t-radius focus:outline-none cursor-pointer max-w-[240px]"
              >
                {THEME_OPTIONS.map((opt) => (
                  <option key={opt.id} value={opt.id}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>

            {/* Dark / Light Mode Toggle */}
            <div className="flex t-border t-radius overflow-hidden">
              <button
                type="button"
                id="btn-color-dark"
                onClick={() => setTheme(themeStyle === 'arashi' ? 'broadcast' : themeStyle, 'dark')}
                className={`px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider ${
                  colorMode === 'dark'
                    ? 't-bg-blue-ink t-text-on-accent'
                    : 'surface-plaque t-text-muted hover:opacity-80'
                }`}
              >
                DARK
              </button>
              <button
                type="button"
                id="btn-color-light"
                onClick={() => setTheme(themeStyle, 'light')}
                className={`px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border ${
                  colorMode === 'light'
                    ? 't-bg-blue-ink t-text-on-accent'
                    : 'surface-plaque t-text-muted hover:opacity-80'
                }`}
              >
                LIGHT
              </button>
            </div>

            {/* Font Scale Switcher */}
            <div className="flex t-border t-radius overflow-hidden">
              <button
                type="button"
                id="btn-scale-normal"
                onClick={() => setFontScale('normal')}
                className={`px-2 py-1 t-font-display font-bold text-xs uppercase tracking-wider ${
                  fontScale === 'normal'
                    ? 't-bg-panel-hover t-text-strong border-b-2 t-border-blue'
                    : 'surface-plaque t-text-muted hover:opacity-80'
                }`}
                title="Default Large Readable Scale (16px base)"
              >
                TEXT: LARGE (DEFAULT)
              </button>
              <button
                type="button"
                id="btn-scale-xl"
                onClick={() => setFontScale('xl')}
                className={`px-2 py-1 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border ${
                  fontScale === 'xl'
                    ? 't-bg-panel-hover t-text-strong border-b-2 t-border-blue'
                    : 'surface-plaque t-text-muted hover:opacity-80'
                }`}
                title="Extra-Large Scale (+15% for 1440p/4K monitors)"
              >
                TEXT: XL (+15%)
              </button>
            </div>

            {/* Restore Sync Banner button when hidden */}
            {replayBannerDismissed && (
              <button
                type="button"
                id="btn-restore-sync-banner"
                data-testid="restore-sync-banner-btn"
                onClick={() => setReplayBannerDismissed(false)}
                className="px-2 py-1 surface-plaque t-border t-font-display font-bold text-xs uppercase tracking-wider t-text-muted hover:text-ink t-radius cursor-pointer whitespace-nowrap"
                title="Restore the replay queue sync banner if any warning is active"
              >
                SHOW SYNC BANNER
              </button>
            )}

            {/* Dev/Debug-Only Preview Mode Toggle & Buffer Simulator */}
            {isDevBuild && (
              <>
                <button
                  type="button"
                  id="btn-preview-toggle"
                  data-testid="preview-mode-toggle"
                  onClick={() => {
                    const next = !previewMode;
                    setPreviewMode(next);
                    setLiveModeOverride(next ? 'match' : 'auto');
                  }}
                  className={`px-2.5 py-1 t-border t-radius t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer whitespace-nowrap ${
                    previewMode
                      ? 'cartouche-gold'
                      : 'surface-plaque t-text-muted hover:text-ink'
                  }`}
                  title="Dev/Debug build only: Toggle simulated demo/preview telemetry ON or OFF"
                >
                  PREVIEW: {previewMode ? 'ON' : 'OFF'}
                </button>

                <div className="flex items-center gap-1">
                  <label
                    htmlFor="limit-state-select"
                    className="t-text-muted t-font-display font-bold uppercase tracking-wider text-xs hidden 2xl:inline"
                  >
                    BUFFER:
                  </label>
                  <select
                    id="limit-state-select"
                    value={replayLimitPreview}
                    onChange={(e) => setReplayLimitPreview(e.target.value as ReplayLimitPreviewState)}
                    className="surface-plaque t-text-strong t-border px-2 py-1 text-xs font-semibold t-radius focus:outline-none cursor-pointer"
                  >
                    <option value="auto">BUFFER: AUTO (5/20)</option>
                    <option value="warn">BUFFER: WARN (16/20)</option>
                    <option value="error">BUFFER: FULL (20/20)</option>
                    <option value="ok">BUFFER: OK (HIDDEN)</option>
                  </select>
                </div>
              </>
            )}

            <a href="/?mode=overlay" target="_blank" rel="noopener noreferrer" className={GHOST_LINK}>
              OVERLAY ↗
            </a>
          </div>
        </div>

        {/* Row 2: Compact Ledger Control Strip */}
        <div className="border-t border-line surface-plaque">
          <div className="max-w-[1680px] mx-auto px-4 sm:px-6 py-1 flex flex-wrap items-center justify-between gap-2 text-xs">
            <div className="flex flex-wrap items-center gap-2">
              {/* Dev/Debug-Only Live / Idle View Switch */}
              {isDevBuild && (
                <div className="flex t-border t-radius overflow-hidden" role="group" aria-label="Live or Idle view mode">
                  <button
                    type="button"
                    id="btn-mode-match"
                    onClick={() => {
                      if (!previewMode) setPreviewMode(true);
                      setLiveModeOverride('match');
                      if (activeTab !== 'live') onTabChange('live');
                    }}
                    className={`px-2.5 py-0.5 t-font-display font-bold text-xs uppercase tracking-wider ${
                      inMatch
                        ? 't-bg-blue-ink t-text-on-accent'
                        : 't-bg-page t-text-muted hover:opacity-80'
                    }`}
                  >
                    IN-MATCH
                  </button>
                  <button
                    type="button"
                    id="btn-mode-idle"
                    onClick={() => {
                      setLiveModeOverride(liveModeOverride === 'idle' ? 'auto' : 'idle');
                      if (activeTab !== 'live') onTabChange('live');
                    }}
                    className={`px-2.5 py-0.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border ${
                      !inMatch
                        ? 't-bg-blue-ink t-text-on-accent'
                        : 't-bg-page t-text-muted hover:opacity-80'
                    }`}
                  >
                    IDLE VIEW
                  </button>
                </div>
              )}

              {/* Global AVG / G vs TOTALS Toggle */}
              <div className="flex t-border t-radius overflow-hidden" role="group" aria-label="Stat aggregation mode">
                <button
                  type="button"
                  id="btn-stat-avg"
                  onClick={() => setStatMode('avg')}
                  className={`px-2.5 py-0.5 t-font-display font-bold text-xs uppercase tracking-wider ${
                    statMode === 'avg'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  AVG / G
                </button>
                <button
                  type="button"
                  id="btn-stat-total"
                  onClick={() => setStatMode('total')}
                  className={`px-2.5 py-0.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border ${
                    statMode === 'total'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  TOTALS
                </button>
              </div>

              {/* Global Timeframe Selector + Custom Calendar Picker */}
              <div className="flex flex-wrap items-center gap-1.5 t-bg-page t-border px-2 py-0.5 t-radius">
                <span className="t-font-display font-bold text-xs uppercase tracking-wider t-text-muted">
                  TIMEFRAME:
                </span>
                <select
                  id="global-date-range"
                  aria-label="Timeframe"
                  value={dateRange}
                  onChange={(e) => setDateRange(e.target.value as DateRangePreset)}
                  className="surface-plaque t-text-strong text-xs t-font-display font-bold uppercase tracking-wider px-1.5 py-0.5 t-border focus:outline-none cursor-pointer"
                >
                  <option value="all">ALL TIME</option>
                  <option value="today">TODAY / SESSION</option>
                  <option value="7d">LAST 7 DAYS</option>
                  <option value="30d">LAST 30 DAYS</option>
                  <option value="custom">CUSTOM CALENDAR RANGE...</option>
                </select>

                {(showCustomCalendar || dateRange === 'custom') && (
                  <div
                    id="custom-calendar-bar"
                    data-testid="custom-calendar-bar"
                    className="flex items-center gap-1.5 pl-1.5 border-l t-border"
                  >
                    <label
                      htmlFor="custom-date-from"
                      className="text-xs t-font-display font-bold t-text-muted uppercase"
                    >
                      FROM
                    </label>
                    <input
                      id="custom-date-from"
                      type="date"
                      value={customFrom}
                      onChange={(e) => setCustomFrom(e.target.value)}
                      className="surface-plaque t-text-strong t-border px-1.5 py-0.5 text-xs t-font-mono t-radius focus:outline-none cursor-pointer"
                    />
                    <span className="t-text-muted font-bold">→</span>
                    <label
                      htmlFor="custom-date-to"
                      className="text-xs t-font-display font-bold t-text-muted uppercase"
                    >
                      TO
                    </label>
                    <input
                      id="custom-date-to"
                      type="date"
                      value={customTo}
                      onChange={(e) => setCustomTo(e.target.value)}
                      className="surface-plaque t-text-strong t-border px-1.5 py-0.5 text-xs t-font-mono t-radius focus:outline-none cursor-pointer"
                    />
                  </div>
                )}

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
                  className="px-2 py-0.5 surface-plaque t-border t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:opacity-80"
                  title="Pick custom start & end dates"
                >
                  ◆ DATES
                </button>
              </div>
            </div>

            <div className="flex items-center gap-3 font-semibold text-xs tracking-wider text-muted">
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
              {onOpenColumns && (
                <button type="button" onClick={onOpenColumns} className={GHOST_LINK}>
                  COLUMNS
                </button>
              )}
            </div>
          </div>
        </div>
      </header>
    </>
  );
};

