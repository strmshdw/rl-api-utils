import { useCallback, useEffect, useRef, useState } from 'react';
import { useSSEEvent } from '../context/SSEContext';
import type { BallchasingSyncStatus, SyncProgress } from '../types/api';

export interface UseBallchasingSyncOptions {
  onSyncComplete?: (status: BallchasingSyncStatus) => void;
  pollIntervalMs?: number;
}

export interface TriggerSyncOptions { includeReplaySync?: boolean; }

export interface UseBallchasingSyncResult {
  syncStatus: BallchasingSyncStatus | null;
  progress: SyncProgress | null;
  isSyncing: boolean;
  syncMessage: string | null;
  error: string | null;
  triggerSync: (opts?: TriggerSyncOptions) => Promise<void>;
  refreshStatus: () => Promise<BallchasingSyncStatus | null>;
  dismissSyncMessage: () => void;
  clearError: () => void;
}

function buildCompletionMessage(status: BallchasingSyncStatus): string {
  return status.last_error
    ? `SYNC FAILED · ${status.last_error}`
    : `SYNCED ${status.synced_matches} MATCHES · ${status.synced_players} PLAYERS FROM BALLCHASING`;
}

export function useBallchasingSync(options: UseBallchasingSyncOptions = {}): UseBallchasingSyncResult {
  const { onSyncComplete, pollIntervalMs = 1000 } = options;

  const [syncStatus, setSyncStatus] = useState<BallchasingSyncStatus | null>(null);
  const [progress, setProgress] = useState<SyncProgress | null>(null);
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

  const finalizeSync = useCallback((data: BallchasingSyncStatus) => {
    if (!mountedRef.current || !syncingRef.current) return;
    syncingRef.current = false;
    setIsSyncing(false);
    setSyncStatus(data);
    setSyncMessage(buildCompletionMessage(data));
    onCompleteRef.current?.(data);
  }, []);

  const refreshStatus = useCallback(async (): Promise<BallchasingSyncStatus | null> => {
    try {
      const res = await fetch('/api/sync/ballchasing/status');
      if (!res.ok) return null;
      const data: BallchasingSyncStatus = await res.json();
      if (!mountedRef.current) return null;
      setSyncStatus(data);
      if (data.running) {
        syncingRef.current = true;
        setIsSyncing(true);
      } else if (syncingRef.current) {
        finalizeSync(data);
      }
      return data;
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
        const res = await fetch('/api/sync/ballchasing/status');
        if (!res.ok || cancelled || !mountedRef.current) return;
        const data: BallchasingSyncStatus = await res.json();
        if (cancelled || !mountedRef.current) return;
        setSyncStatus(data);
        if (!data.running) {
          clearInterval(interval);
          finalizeSync(data);
        }
      } catch {
        // keep polling on transient errors
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

  const handleProgressEvent = useCallback(
    (payload: SyncProgress) => {
      if (!mountedRef.current || !payload) return;
      setProgress(payload);
      if (payload.syncing) {
        syncingRef.current = true;
        setIsSyncing(true);
        return;
      }
      if (syncingRef.current && payload.finished_at) {
        void refreshStatus();
      }
    },
    [refreshStatus],
  );

  useSSEEvent<SyncProgress>('sync_progress', handleProgressEvent);

  const triggerSync = useCallback(async (triggerOpts?: TriggerSyncOptions) => {
    setError(null);
    setSyncMessage(null);
    setSyncStatus(null);
    setProgress(null);
    syncingRef.current = true;
    setIsSyncing(true);
    try {
      if (triggerOpts?.includeReplaySync) {
        await fetch('/api/sync/now', { method: 'POST' });
      }
      const res = await fetch('/api/sync/ballchasing', { method: 'POST' });
      if (!res.ok && res.status !== 409) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
    } catch (err) {
      if (!mountedRef.current) return;
      syncingRef.current = false;
      setIsSyncing(false);
      setError(err instanceof Error ? err.message : 'Failed to trigger Ballchasing sync');
    }
  }, []);

  const dismissSyncMessage = useCallback(() => setSyncMessage(null), []);
  const clearError = useCallback(() => setError(null), []);

  return {
    syncStatus,
    progress,
    isSyncing,
    syncMessage,
    error,
    triggerSync,
    refreshStatus,
    dismissSyncMessage,
    clearError,
  };
}
