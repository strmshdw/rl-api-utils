import React from 'react';
import {
  ReplayLimitPreviewState,
  useDashboard,
} from '../../context/DashboardContext';
import { useBallchasingSync } from '../../hooks/useBallchasingSync';
import { Modal } from '../common/Modal';
import { SectionLabel } from '../common/ui';
import { ThemeAppearanceSection } from './ThemeAppearanceSection';

export { THEME_OPTIONS } from './ThemeAppearanceSection';

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

  const {
    syncStatus,
    progress,
    isSyncing,
    syncMessage,
    error: syncError,
    triggerSync,
    dismissSyncMessage,
  } = useBallchasingSync();

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
        <ThemeAppearanceSection
          themeStyle={themeStyle}
          colorMode={colorMode}
          fontScale={fontScale}
          onSetTheme={setTheme}
          onSetFontScale={setFontScale}
        />

        {/* ==================== SECTION 02: TELEMETRY DISPLAY & STREAMING ==================== */}
        <section className="flex flex-col gap-3.5 pt-2 border-t border-line">
          <SectionLabel>02 · TELEMETRY DISPLAY &amp; BROADCAST</SectionLabel>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
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
                {replayBannerDismissed
                  ? 'SHOW SYNC BANNER (CURRENTLY HIDDEN)'
                  : 'SYNC BANNER: ENABLED (CLICK TO HIDE)'}
              </button>
            </div>

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

          <div className="surface-plaque border border-line p-3 flex flex-wrap items-center justify-between gap-3 t-radius">
            <div className="flex flex-col gap-0.5 min-w-0">
              <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                BALLCHASING TELEMETRY SYNC
              </span>
              <span className="text-xs text-muted truncate">
                {syncError
                  ? syncError
                  : syncMessage
                  ? syncMessage
                  : progress?.syncing
                  ? `SYNCING · ${progress.processed}/${progress.total_found || '?'} (${progress.new_imported} NEW)`
                  : syncStatus
                  ? `LAST STATUS: ${syncStatus.synced_matches} MATCHES · ${syncStatus.synced_players} PLAYERS`
                  : 'Pull match & player telemetry from Ballchasing'}
              </span>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              {syncMessage && (
                <button
                  type="button"
                  onClick={dismissSyncMessage}
                  className="px-2 py-1 text-xs font-bold uppercase text-muted hover:text-ink cursor-pointer"
                >
                  DISMISS
                </button>
              )}
              <button
                type="button"
                data-testid="settings-ballchasing-sync-btn"
                disabled={isSyncing}
                onClick={() => triggerSync()}
                className="px-3 py-1.5 t-bg-blue-ink t-text-on-accent t-font-display font-bold text-xs uppercase tracking-wider t-radius cursor-pointer disabled:opacity-60"
              >
                {isSyncing
                  ? `SYNCING · ${syncStatus?.synced_matches ?? 0} MATCHES`
                  : 'SYNC FROM BALLCHASING'}
              </button>
            </div>
          </div>
        </section>

        {/* ==================== SECTION 03: DEVELOPER & DIAGNOSTICS (DEV BUILD ONLY) ==================== */}
        {isDevBuild && (
          <section className="flex flex-col gap-3.5 pt-2 border-t border-line">
            <SectionLabel>03 · DEVELOPER &amp; TELEMETRY SIMULATOR (DEV BUILD ONLY)</SectionLabel>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
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

              <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
                <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
                  LIVE VIEW OVERRIDE
                </span>
                <div
                  className="grid grid-cols-2 t-border t-radius overflow-hidden"
                  role="group"
                  aria-label="Live or Idle view mode"
                >
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
