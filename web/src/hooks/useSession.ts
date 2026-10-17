import { useState, useEffect, useCallback } from 'react';
import { SessionResponse, SessionMatchDetail } from '../types/api';
import { sharedSSEManager } from '../context/SSEContext';

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

    // Listen to shared SSE stream for instant updates without extra round-trip HTTP requests
    const unsubSessionUpdate = sharedSSEManager.subscribe('session_update', (data: SessionResponse) => {
      if (data && typeof data === 'object') {
        setSession(data);
        setError(null);
        setIsLoading(false);
      } else {
        fetchSession();
      }
    });

    const unsubMatchEnded = sharedSSEManager.subscribe('match_ended', () => {
      fetchSession();
    });

    return () => {
      unsubSessionUpdate();
      unsubMatchEnded();
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
