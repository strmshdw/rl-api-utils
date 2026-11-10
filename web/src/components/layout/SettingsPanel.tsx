import React from 'react';
import {
  ReplayLimitPreviewState,
  THEME_STYLES,
  ThemeStyleKey,
  useDashboard,
} from '../../context/DashboardContext';
import { Modal } from '../common/Modal';
import { SectionLabel } from '../common/ui';

export const THEME_OPTIONS: ReadonlyArray<{
  id: ThemeStyleKey;
  label: string;
  shortName: string;
  core?: boolean;
}> = [
  { id: 'broadcast', label: '01 · Deco Monolith: Woodblock (Default)', shortName: '01 · WOODBLOCK', core: true },
  { id: 'tactical', label: '02 · Deco Monolith: Lacquer & Brass', shortName: '02 · LACQUER', core: true },
  { id: 'swiss', label: '03 · Deco Monolith: Carved Inkstone', shortName: '03 · INKSTONE', core: true },
  { id: 'arashi', label: '04 · Deco Monolith: Warm Parchment (Light)', shortName: '04 · PARCHMENT', core: true },
  { id: 'classic', label: '05 · Classic Broadcast (Original RL/SYNC)', shortName: '05 · CLASSIC', core: true },
  { id: 'arena', label: '06 · Glassmorphic Arena (Frosted HUD)', shortName: '06 · ARENA' },
  { id: 'carbon', label: '07 · Carbon Paddock (Motorsport Telemetry)', shortName: '07 · CARBON' },
  { id: 'synthwave', label: '08 · Mannfield Night (Neon Violet)', shortName: '08 · MANNFIELD' },
  { id: 'editorial', label: '09 · Editorial Gazette (Fraunces Serif)', shortName: '09 · GAZETTE' },
  { id: 'nordic', label: '10 · Nordic Frost (Arctic Slate & Ice)', shortName: '10 · NORDIC' },
  { id: 'terminal', label: '11 · Retro CRT Phosphor (Monospace)', shortName: '11 · RETRO CRT' },
  { id: 'solar', label: '12 · Utopia Daylight (Warm Sand & Blue)', shortName: '12 · UTOPIA' },
  { id: 'industrial', label: '13 · Cobalt Blueprint (CAD Grid)', shortName: '13 · BLUEPRINT' },
  { id: 'oled', label: '14 · Pure OLED Pitch (True Zero Black)', shortName: '14 · PURE OLED' },
];

interface SettingsPanelProps {
  isOpen: boolean;
  onClose: () => void;
  inMatch: boolean;
  onSwitchToLive: () => void;
  onOpenColumns?: () => void;
}

export const SettingsPanel: React.FC<SettingsPanelProps> = ({
  isOpen,
  onClose,
  inMatch,
  onSwitchToLive,
  onOpenColumns,
}) => {
  const {
    themeStyle,
    colorMode,
    setTheme,
    fontScale,
    setFontScale,
    statMode,
    setStatMode,
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

  const applyThemeSelection = (next: ThemeStyleKey) => {
    if (!THEME_STYLES[next]) return;
    if (next === 'arashi') {
      setTheme(next, 'light');
    } else if (next === 'broadcast' || next === 'tactical' || next === 'swiss') {
      setTheme(next, 'dark');
    } else {
      setTheme(next, colorMode);
    }
  };

  const coreThemes = THEME_OPTIONS.filter((t) => t.core);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      kicker="STUDIO & TELEMETRY CONFIGURATION"
      title="SETTINGS"
      meta={
        <span>
          RL/SYNC v{appVersion}
          {isDevBuild ? '-dev' : ''} · PREFERENCES SAVED TO LOCAL STORAGE
        </span>
      }
      maxWidth={880}
    >
      <div
        id="settings-panel-body"
        data-testid="settings-panel"
        className="p-6 flex flex-col gap-6 font-sans text-sm"
      >
        {/* ==================== SECTION 01: DESIGN SYSTEM & APPEARANCE ==================== */}
        <section className="flex flex-col gap-3.5">
          <SectionLabel>01 · DESIGN SYSTEM &amp; APPEARANCE</SectionLabel>

          {/* 5 Core Theme Quick-Select Swatch Cards */}
          <div className="grid grid-cols-1 sm:grid-cols-5 gap-2">
            {coreThemes.map((opt) => {
              const def = THEME_STYLES[opt.id];
              const pal = opt.id === 'arashi' ? def.light : def.dark;
              const active = themeStyle === opt.id;
              return (
                <button
                  key={opt.id}
                  type="button"
                  onClick={() => applyThemeSelection(opt.id)}
                  className={`p-2.5 text-left surface-plaque border transition-colors cursor-pointer flex flex-col gap-2 t-radius ${
                    active
                      ? 'border-blue-ink ring-1 ring-blue-ink t-bg-panel-hover'
                      : 'border-line hover:border-outline'
                  }`}
                >
                  <div className="flex items-center justify-between gap-1">
                    <span className="t-font-display font-bold text-xs uppercase tracking-wider t-text-strong truncate">
                      {opt.shortName}
                    </span>
                    {active && (
                      <span className="text-[10px] t-font-mono font-extrabold t-text-blue">●</span>
                    )}
                  </div>
                  <div className="flex items-center gap-1 h-3 w-full overflow-hidden border border-line">
                    <span className="flex-1 h-full" style={{ backgroundColor: pal.page }} title="Surface" />
                    <span className="flex-1 h-full" style={{ backgroundColor: pal.blueSlab }} title="Blue Team" />
                    <span className="flex-1 h-full" style={{ backgroundColor: pal.orangeSlab }} title="Orange Team" />
                  </div>
                </button>
              );
            })}
          </div>

          {/* Full 14-Theme Selector + Dark/Light Polarity + Font Scale */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-1">
            {/* Theme Dropdown */}
            <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
              <label
                htmlFor="theme-select"
                className="t-font-display font-bold text-xs uppercase tracking-wider text-muted"
              >
                ALL DESIGN EXPRESSIONS (14)
              </label>
              <select
                id="theme-select"
                value={themeStyle}
                onChange={(e) => applyThemeSelection(e.target.value as ThemeStyleKey)}
                className="t-bg-page t-text-strong border border-outline px-2.5 py-1.5 text-xs font-semibold t-radius focus:outline-none cursor-pointer w-full"
              >
                {THEME_OPTIONS.map((opt) => (
                  <option key={opt.id} value={opt.id}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>

            {/* Dark / Light Polarity */}
            <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                COLOR POLARITY
              </span>
              <div className="grid grid-cols-2 t-border t-radius overflow-hidden">
                <button
                  type="button"
                  id="btn-color-dark"
                  onClick={() => setTheme(themeStyle === 'arashi' ? 'broadcast' : themeStyle, 'dark')}
                  className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                    colorMode === 'dark'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  DARK MODE
                </button>
                <button
                  type="button"
                  id="btn-color-light"
                  onClick={() => setTheme(themeStyle, 'light')}
                  className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                    colorMode === 'light'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  LIGHT MODE
                </button>
              </div>
            </div>

            {/* Anti-Squint Text Scale */}
            <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                TYPOGRAPHY SCALE
              </span>
              <div className="grid grid-cols-2 t-border t-radius overflow-hidden">
                <button
                  type="button"
                  id="btn-scale-normal"
                  onClick={() => setFontScale('normal')}
                  className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                    fontScale === 'normal'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                  title="Default Large Readable Scale (16px base)"
                >
                  TEXT: LARGE (DEFAULT)
                </button>
                <button
                  type="button"
                  id="btn-scale-xl"
                  onClick={() => setFontScale('xl')}
                  className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                    fontScale === 'xl'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                  title="Extra-Large Scale (+15% for 1440p/4K monitors)"
                >
                  TEXT: XL (+15%)
                </button>
              </div>
            </div>
          </div>
        </section>

        {/* ==================== SECTION 02: TELEMETRY DISPLAY & STREAMING ==================== */}
        <section className="flex flex-col gap-3.5 pt-2 border-t border-line">
          <SectionLabel>02 · TELEMETRY DISPLAY &amp; BROADCAST</SectionLabel>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            {/* Stat Aggregation Mode */}
            <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                HISTORICAL STAT AGGREGATION
              </span>
              <div className="grid grid-cols-2 t-border t-radius overflow-hidden">
                <button
                  type="button"
                  onClick={() => setStatMode('avg')}
                  className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                    statMode === 'avg'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  AVG / G
                </button>
                <button
                  type="button"
                  onClick={() => setStatMode('total')}
                  className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                    statMode === 'total'
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  TOTALS
                </button>
              </div>
            </div>

            {/* Replay Queue Sync Banner Visibility */}
            <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                REPLAY QUEUE SYNC BANNER
              </span>
              <button
                type="button"
                id="btn-restore-sync-banner"
                data-testid="restore-sync-banner-btn"
                onClick={() => setReplayBannerDismissed(!replayBannerDismissed)}
                className={`w-full py-1.5 px-3 border t-font-display font-bold text-xs uppercase tracking-wider t-radius cursor-pointer ${
                  replayBannerDismissed
                    ? 't-bg-page border-outline t-text-strong hover:border-ink'
                    : 't-bg-panel-hover border-blue-ink t-text-strong'
                }`}
              >
                {replayBannerDismissed ? 'SHOW SYNC BANNER (CURRENTLY HIDDEN)' : 'SYNC BANNER: ENABLED (CLICK TO HIDE)'}
              </button>
            </div>

            {/* OBS Streaming Overlay & Table Columns */}
            <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                STREAMING &amp; TABLE LAYOUT
              </span>
              <div className="flex items-center gap-2">
                {onOpenColumns && (
                  <button
                    type="button"
                    onClick={() => {
                      onClose();
                      onOpenColumns();
                    }}
                    className="flex-1 py-1.5 px-2.5 t-bg-page border border-outline t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:border-ink t-radius cursor-pointer text-center whitespace-nowrap"
                  >
                    COLUMNS
                  </button>
                )}
                <a
                  href="/?mode=overlay"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex-1 py-1.5 px-2.5 t-bg-page border border-outline t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:border-ink t-radius text-center whitespace-nowrap"
                >
                  OBS OVERLAY ↗
                </a>
              </div>
            </div>
          </div>
        </section>

        {/* ==================== SECTION 03: DEVELOPER & DIAGNOSTICS (DEV BUILD ONLY) ==================== */}
        {isDevBuild && (
          <section className="flex flex-col gap-3.5 pt-2 border-t border-line">
            <SectionLabel>03 · DEVELOPER &amp; TELEMETRY SIMULATOR (DEV BUILD ONLY)</SectionLabel>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              {/* Preview Mode Toggle */}
              <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
                <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                  SIMULATED PREVIEW TELEMETRY
                </span>
                <button
                  type="button"
                  id="btn-preview-toggle"
                  data-testid="preview-mode-toggle"
                  onClick={() => {
                    const next = !previewMode;
                    setPreviewMode(next);
                    setLiveModeOverride(next ? 'match' : 'auto');
                  }}
                  className={`w-full py-1.5 px-3 t-border t-radius t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                    previewMode
                      ? 'cartouche-gold'
                      : 't-bg-page t-text-muted hover:text-ink'
                  }`}
                >
                  PREVIEW: {previewMode ? 'ON' : 'OFF'}
                </button>
              </div>

              {/* In-Match vs Idle View Override */}
              <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
                <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                  LIVE VIEW OVERRIDE
                </span>
                <div className="grid grid-cols-2 t-border t-radius overflow-hidden" role="group" aria-label="Live or Idle view mode">
                  <button
                    type="button"
                    id="btn-mode-match"
                    onClick={() => {
                      if (!previewMode) setPreviewMode(true);
                      setLiveModeOverride('match');
                      onSwitchToLive();
                    }}
                    className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
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
                      onSwitchToLive();
                    }}
                    className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                      !inMatch
                        ? 't-bg-blue-ink t-text-on-accent'
                        : 't-bg-page t-text-muted hover:opacity-80'
                    }`}
                  >
                    IDLE VIEW
                  </button>
                </div>
              </div>

              {/* Replay Buffer State Simulator */}
              <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
                <label
                  htmlFor="limit-state-select"
                  className="t-font-display font-bold text-xs uppercase tracking-wider text-muted"
                >
                  REPLAY BUFFER SIMULATOR
                </label>
                <select
                  id="limit-state-select"
                  value={replayLimitPreview}
                  onChange={(e) => setReplayLimitPreview(e.target.value as ReplayLimitPreviewState)}
                  className="t-bg-page t-text-strong border border-outline px-2.5 py-1.5 text-xs font-semibold t-radius focus:outline-none cursor-pointer w-full"
                >
                  <option value="auto">BUFFER: AUTO (5/20)</option>
                  <option value="warn">BUFFER: WARN (16/20)</option>
                  <option value="error">BUFFER: FULL (20/20)</option>
                  <option value="ok">BUFFER: OK (HIDDEN)</option>
                </select>
              </div>
            </div>
          </section>
        )}
      </div>
    </Modal>
  );
};
