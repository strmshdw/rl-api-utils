import { useCallback, useEffect, useRef, useState } from 'react';
import type { ReplaySyncStatus } from '../types/api';

export interface UseReplaySyncOptions {
  onSyncComplete?: (status: ReplaySyncStatus | null) => void;
  pollIntervalMs?: number;
}

export interface UseReplaySyncResult {
  syncStatus: ReplaySyncStatus | null;
  isSyncing: boolean;
  syncMessage: string | null;
  error: string | null;
  triggerReplaySync: () => Promise<boolean>;
  refreshStatus: () => Promise<ReplaySyncStatus | null>;
  dismissSyncMessage: () => void;
  clearError: () => void;
}

function buildReplaySyncMessage(status: ReplaySyncStatus | null): string {
  if (!status) {
    return 'PSYNET REPLAY SYNC COMPLETE';
  }
  if (status.last_error) {
    return `SYNC FAILED · ${status.last_error}`;
  }
  if (status.downloaded_count > 0) {
    const noun = status.downloaded_count === 1 ? 'REPLAY' : 'REPLAYS';
    return status.uploaded_count > 0
      ? `DOWNLOADED ${status.downloaded_count} NEW ${noun} · ${status.uploaded_count} UPLOADED`
      : `DOWNLOADED ${status.downloaded_count} NEW ${noun} FROM PSYNET`;
  }
  if (status.uploaded_count > 0) {
    return `UPLOADED ${status.uploaded_count} PENDING ${status.uploaded_count === 1 ? 'REPLAY' : 'REPLAYS'}`;
  }
  if (status.discovered_count > 0) {
    return `ALL PSYNET REPLAYS UP TO DATE (${status.discovered_count} CHECKED)`;
  }
  return 'PSYNET REPLAY SYNC COMPLETE';
}

/**
 * Manages manual PsyNet replay syncs (POST /api/sync/now + GET /api/sync/now/status).
 * Strictly downloads/uploads undownloaded replays from Rocket League's 20-game PsyNet buffer
 * without triggering a full Ballchasing historical archive sync.
 */
export function useReplaySync(options: UseReplaySyncOptions = {}): UseReplaySyncResult {
  const { onSyncComplete, pollIntervalMs = 500 } = options;

  const [syncStatus, setSyncStatus] = useState<ReplaySyncStatus | null>(null);
  const [isSyncing, setIsSyncing] = useState(false);
  const [syncMessage, setSyncMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const mountedRef = useRef(true);
  const syncingRef = useRef(false);
  const pollInFlightRef = useRef(false);
  const onCompleteRef = useRef(onSyncComplete);
  onCompleteRef.current = onSyncComplete;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const finalizeSync = useCallback((data: ReplaySyncStatus | null) => {
    if (!mountedRef.current || !syncingRef.current) return;
    syncingRef.current = false;
    setIsSyncing(false);
    setSyncStatus(data);
    setSyncMessage(buildReplaySyncMessage(data));
    onCompleteRef.current?.(data);
  }, []);

  const refreshStatus = useCallback(async (): Promise<ReplaySyncStatus | null> => {
    try {
      const res = await fetch('/api/sync/now/status');
      if (!res.ok) return null;
      const data: Partial<ReplaySyncStatus> = await res.json();
      if (!mountedRef.current || typeof data?.running !== 'boolean') return null;
      const full = data as ReplaySyncStatus;
      setSyncStatus(full);
      if (full.running) {
        syncingRef.current = true;
        setIsSyncing(true);
      } else if (syncingRef.current) {
        finalizeSync(full);
      }
      return full;
    } catch {
      return null;
    }
  }, [finalizeSync]);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  useEffect(() => {
    if (!isSyncing) return;
    syncingRef.current = true;
    let cancelled = false;

    const poll = async () => {
      if (cancelled || pollInFlightRef.current || !syncingRef.current) return;
      pollInFlightRef.current = true;
      try {
        const res = await fetch('/api/sync/now/status');
        if (cancelled || !mountedRef.current) return;
        if (!res.ok) {
          clearInterval(interval);
          finalizeSync(null);
          return;
        }
        const data: Partial<ReplaySyncStatus> = await res.json();
        if (cancelled || !mountedRef.current) return;
        if (typeof data?.running !== 'boolean') {
          clearInterval(interval);
          finalizeSync(null);
          return;
        }
        const full = data as ReplaySyncStatus;
        setSyncStatus(full);
        if (!full.running) {
          clearInterval(interval);
          finalizeSync(full);
        }
      } catch {
        // keep polling on transient network hiccup
      } finally {
        pollInFlightRef.current = false;
      }
    };

    const interval = setInterval(() => {
      void poll();
    }, pollIntervalMs);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [isSyncing, pollIntervalMs, finalizeSync]);

  const triggerReplaySync = useCallback(async (): Promise<boolean> => {
    if (syncingRef.current) return false;
    setError(null);
    setSyncMessage(null);
    setSyncStatus(null);
    syncingRef.current = true;
    setIsSyncing(true);
    try {
      const res = await fetch('/api/sync/now', { method: 'POST' });
      if (!res.ok && res.status !== 409) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      const body: { running?: boolean } = await res.json().catch(() => ({}));
      if (!mountedRef.current) return true;
      if (body.running !== true) {
        finalizeSync(null);
      }
      return true;
    } catch (err) {
      if (!mountedRef.current) return false;
      syncingRef.current = false;
      setIsSyncing(false);
      setError(err instanceof Error ? err.message : 'Failed to trigger PsyNet replay sync');
      return false;
    }
  }, [finalizeSync]);

  const dismissSyncMessage = useCallback(() => setSyncMessage(null), []);
  const clearError = useCallback(() => setError(null), []);

  return {
    syncStatus,
    isSyncing,
    syncMessage,
    error,
    triggerReplaySync,
    refreshStatus,
    dismissSyncMessage,
    clearError,
  };
}
