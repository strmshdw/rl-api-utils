import { useState, useEffect, useCallback } from 'react';
import { SessionResponse, SessionMatchDetail } from '../types/api';

export function useSession() {
  const [session, setSession] = useState<SessionResponse | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedMatch, setSelectedMatch] = useState<SessionMatchDetail | null>(null);

  const fetchSession = useCallback(async () => {
    try {
      const res = await fetch('/api/session');
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`);
      const data: SessionResponse = await res.json();
      setSession(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load session');
    } finally {
      setIsLoading(false);
    }
  }, []);

  const resetSession = useCallback(async () => {
    try {
      const res = await fetch('/api/session/reset', { method: 'POST' });
      if (!res.ok) throw new Error(`Failed to reset session: HTTP ${res.status}`);
      await fetchSession();
    } catch (err) {
      console.error('Session reset error:', err);
    }
  }, [fetchSession]);

  useEffect(() => {
    fetchSession();

    // Listen to SSE /api/events for instant updates
    let eventSource: EventSource | null = null;
    try {
      eventSource = new EventSource('/api/events');
      eventSource.addEventListener('session_update', () => {
        fetchSession();
      });
      eventSource.addEventListener('match_ended', () => {
        fetchSession();
      });
      eventSource.onerror = () => {
        // SSE reconnect handles itself
      };
    } catch (e) {
      console.warn('SSE not supported or failed to connect:', e);
    }

    return () => {
      if (eventSource) eventSource.close();
    };
  }, [fetchSession]);

  return {
    session,
    isLoading,
    error,
    refreshSession: fetchSession,
    resetSession,
    selectedMatch,
    setSelectedMatch,
  };
}
