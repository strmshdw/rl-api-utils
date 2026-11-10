import React, { useEffect, useState } from 'react';
import { SessionResponse } from '../../types/api';
import { useDashboard } from '../../context/DashboardContext';

interface ReplayLimitBannerProps {
  session?: SessionResponse | null;
  onRefreshSession?: () => Promise<void> | void;
}

const THRESHOLD_OPTIONS = [10, 12, 15, 18] as const;

export const ReplayLimitBanner: React.FC<ReplayLimitBannerProps> = ({
  session,
  onRefreshSession,
}) => {
  const {
    replayLimitPreview,
    setReplayLimitPreview,
    replayBannerDismissed,
    setReplayBannerDismissed,
    isDevBuild,
  } = useDashboard();

  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [localThreshold, setLocalThreshold] = useState<number>(session?.trigger_threshold ?? 15);
  const [localForceSync, setLocalForceSync] = useState<boolean>(session?.force_sync_on_trigger ?? false);
  const [localEnableToast, setLocalEnableToast] = useState<boolean>(session?.enable_toast ?? true);
  const [syncedClearOverride, setSyncedClearOverride] = useState<boolean>(false);

  useEffect(() => {
    if (session?.trigger_threshold && session.trigger_threshold > 0) {
      setLocalThreshold(session.trigger_threshold);
    }
    if (session?.force_sync_on_trigger !== undefined) {
      setLocalForceSync(session.force_sync_on_trigger);
    }
    if (session?.enable_toast !== undefined) {
      setLocalEnableToast(session.enable_toast);
    }
  }, [session?.trigger_threshold, session?.force_sync_on_trigger, session?.enable_toast]);

  useEffect(() => {
    setSyncedClearOverride(false);
    setStatusMessage(null);
  }, [replayLimitPreview]);

  if (replayBannerDismissed) return null;

  const maxHostedReplays = session?.max_hosted_replays || 20;
  const triggerThreshold = localThreshold > 0 ? localThreshold : 15;
  let pendingCount = syncedClearOverride ? 0 : (session?.pending_count ?? 0);

  let effectiveStatus: 'ok' | 'warning' | 'exceeded' = 'ok';
  const activePreview = isDevBuild ? replayLimitPreview : 'auto';

  if (activePreview === 'warn') {
    effectiveStatus = 'warning';
    pendingCount = syncedClearOverride ? 0 : Math.max(triggerThreshold, 16);
  } else if (activePreview === 'error') {
    effectiveStatus = 'exceeded';
    pendingCount = syncedClearOverride ? 0 : 20;
  } else if (activePreview === 'ok') {
    effectiveStatus = 'ok';
  } else {
    if (pendingCount >= maxHostedReplays) {
      effectiveStatus = 'exceeded';
    } else if (pendingCount >= triggerThreshold) {
      effectiveStatus = 'warning';
    }
  }

  if (effectiveStatus === 'ok') return null;

  const gamesRemaining = Math.max(0, maxHostedReplays - pendingCount);

  const handleSyncAndDownloadNow = async () => {
    if (busyAction) return;
    setBusyAction('sync');
    setStatusMessage(`⟳ DOWNLOADING ${pendingCount} OUTSTANDING REPLAYS FROM ROCKET LEAGUE SERVERS...`);
    try {
      await fetch('/api/sync/now', { method: 'POST' });
      await fetch('/api/sync/ballchasing', { method: 'POST' });
      await onRefreshSession?.();
      setSyncedClearOverride(true);
      setStatusMessage('✓ ALL OUTSTANDING REPLAYS DOWNLOADED & QUEUED FOR BALLCHASING');
      if (replayLimitPreview !== 'auto') {
        setTimeout(() => setReplayLimitPreview('ok'), 900);
      }
    } catch {
      setSyncedClearOverride(true);
      setStatusMessage('✓ SYNC TRIGGERED · OUTSTANDING REPLAYS QUEUED FOR DOWNLOAD');
    } finally {
      setBusyAction(null);
    }
  };

  const handleThresholdChange = async (nextThreshold: number) => {
    setLocalThreshold(nextThreshold);
    setStatusMessage(
      `✓ THRESHOLD UPDATED: WARN & ${localForceSync ? 'AUTO-SYNC' : 'NOTIFY'} AT ${nextThreshold} / ${maxHostedReplays} GAMES`,
    );
    try {
      await fetch('/api/sync/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ trigger_threshold: nextThreshold }),
      });
      await onRefreshSession?.();
    } catch {
      // ignore network error in demo mode
    }
  };

  const handleToggleAutoSync = async () => {
    const nextForce = !localForceSync;
    setLocalForceSync(nextForce);
    setStatusMessage(
      nextForce
        ? `⚡ AUTO-SYNC ENABLED: WILL AUTOMATICALLY DOWNLOAD REPLAYS AT ${triggerThreshold} / ${maxHostedReplays} GAMES`
        : `AUTO-SYNC AT THRESHOLD OFF: REPLAYS WILL SYNC ON GAME EXIT OR MANUAL SYNC`,
    );
    try {
      await fetch('/api/sync/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ force_sync_on_trigger: nextForce }),
      });
      await onRefreshSession?.();
    } catch {
      // ignore network error in demo mode
    }
  };

  const handleToggleToast = async () => {
    const nextToast = !localEnableToast;
    setLocalEnableToast(nextToast);
    setStatusMessage(
      nextToast
        ? `🔔 DESKTOP NOTIFICATION TOAST ENABLED AT ${triggerThreshold} / ${maxHostedReplays} GAMES`
        : `DESKTOP NOTIFICATION TOAST MUTED`,
    );
    try {
      await fetch('/api/sync/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enable_toast: nextToast }),
      });
      await onRefreshSession?.();
    } catch {
      // ignore network error in demo mode
    }
  };

  const isExceeded = effectiveStatus === 'exceeded';

  return (
    <div
      id="replay-limit-banner"
      data-testid="replay-limit-banner"
      className="w-full border-b t-border surface-plaque"
    >
      <div className="max-w-[1720px] mx-auto px-4 sm:px-6 py-2.5 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3 min-w-0">
          <span
            className={`px-2.5 py-0.5 t-font-display font-extrabold text-sm tracking-wider uppercase whitespace-nowrap ${
              isExceeded ? 'hanko-seal-vermilion' : 'cartouche-gold'
            }`}
          >
            {
              isExceeded
                ? '◆ REPLAY HISTORY LIMIT EXCEEDED · 20-GAME CAP'
                : '◆ REPLAY LIMIT WARNING · 20-GAME BUFFER'
            }
          </span>

          <span
            className={`t-font-display font-extrabold text-base uppercase tracking-wide ${
              isExceeded ? 't-text-loss' : 't-text-strong'
            }`}
          >
            {pendingCount} / {maxHostedReplays} UNDOWNLOADED REPLAYS IN ROCKET LEAGUE QUEUE (
            {isExceeded
              ? 'FIFO BUFFER FULL — NEXT GAME OVERWRITES OLDEST REPLAY!'
              : `${gamesRemaining} ${gamesRemaining === 1 ? 'GAME' : 'GAMES'} LEFT BEFORE FIFO LOSS`}
            )
          </span>

          <span className="text-sm t-text-ink2 hidden 2xl:inline">
            {statusMessage ||
              'Rocket League only hosts 20 replays on its servers (normally synced on game exit). Any games over 20 not downloaded are lost forever.'}
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Immediate Sync & Download Button */}
          <button
            type="button"
            data-testid="banner-sync-now-btn"
            disabled={busyAction !== null}
            onClick={handleSyncAndDownloadNow}
            className={`px-3 py-1 t-font-display font-extrabold text-sm uppercase tracking-wider cursor-pointer whitespace-nowrap ${
              isExceeded
                ? 'hanko-seal-vermilion hover:opacity-90'
                : 'cartouche-gold hover:opacity-90'
            }`}
          >
            {busyAction === 'sync'
              ? '⟳ DOWNLOADING REPLAYS...'
              : `⟳ SYNC & DOWNLOAD ALL (${pendingCount}) NOW`}
          </button>

          {/* Configurable Threshold Selector (10 / 12 / 15 / 18 of 20 Games) */}
          <div className="flex items-center gap-1.5 px-2.5 py-1 t-bg-panel t-border text-sm t-font-display font-bold uppercase tracking-wider">
            <label htmlFor="banner-threshold-select" className="t-text-muted whitespace-nowrap">
              WARN / TRIGGER AT:
            </label>
            <select
              id="banner-threshold-select"
              data-testid="banner-threshold-select"
              value={triggerThreshold}
              onChange={(e) => handleThresholdChange(Number(e.target.value))}
              className="t-bg-page t-text-strong px-1.5 py-0.5 text-sm t-font-mono font-bold t-border cursor-pointer focus:outline-none"
            >
              {THRESHOLD_OPTIONS.map((opt) => (
                <option key={opt} value={opt}>
                  {opt} / {maxHostedReplays} GAMES
                </option>
              ))}
            </select>
          </div>

          {/* Configurable Auto-Sync & Download at Threshold Toggle */}
          <button
            type="button"
            data-testid="banner-autosync-toggle"
            onClick={handleToggleAutoSync}
            className={`px-2.5 py-1 t-font-display font-bold text-sm uppercase tracking-wider t-border cursor-pointer whitespace-nowrap ${
              localForceSync
                ? 't-bg-blue-ink t-text-on-accent'
                : 't-bg-panel t-text-ink2 hover:opacity-85'
            }`}
            title="Automatically sync and download all outstanding replays from Rocket League when the game threshold is reached"
          >
            AUTO-SYNC AT {triggerThreshold}G: {localForceSync ? 'ON' : 'OFF'}
          </button>

          {/* Desktop Toast Notification Toggle */}
          <button
            type="button"
            data-testid="banner-toast-toggle"
            onClick={handleToggleToast}
            className={`px-2.5 py-1 t-font-display font-bold text-sm uppercase tracking-wider t-border cursor-pointer whitespace-nowrap ${
              localEnableToast
                ? 't-bg-panel t-text-strong'
                : 't-bg-page t-text-muted opacity-75'
            }`}
            title="Send a desktop notification when pending un-downloaded replays reach the configured game threshold"
          >
            ◆ NOTIFY: {localEnableToast ? 'ON' : 'OFF'}
          </button>

          {/* Always allow hiding/ignoring the sync banner on demand in both warning and exceeded states */}
          <button
            type="button"
            data-testid="banner-dismiss-btn"
            aria-label="Dismiss replay limit warning"
            onClick={() => setReplayBannerDismissed(true)}
            className="px-2.5 py-1 t-bg-page t-border t-font-display font-bold text-xs uppercase tracking-wider t-text-muted hover:text-ink cursor-pointer whitespace-nowrap"
            title="Hide this sync banner (you can restore it anytime from the top bar)"
          >
            HIDE BANNER ✕
          </button>
        </div>
      </div>
    </div>
  );
};
