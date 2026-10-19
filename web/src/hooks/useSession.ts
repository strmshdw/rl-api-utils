import { useState, useEffect, useCallback } from 'react';
import { SessionResponse, SessionMatchDetail, SessionListResponse } from '../types/api';
import { sharedSSEManager } from '../context/SSEContext';

export function useSession() {
  const [session, setSession] = useState<SessionResponse | null>(null);
  const [pastSessions, setPastSessions] = useState<SessionResponse[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedMatch, setSelectedMatch] = useState<SessionMatchDetail | null>(null);

  const fetchPastSessions = useCallback(async () => {
    try {
      const res = await fetch('/api/sessions?limit=50&offset=0');
      if (!res || !res.ok) return;
      const data: SessionListResponse = await res.json();
      if (data && Array.isArray(data.sessions)) {
        setPastSessions(data.sessions);
      }
    } catch {
      // Ignore past sessions fetch errors so primary session state is unaffected
    }
  }, []);

  const fetchSession = useCallback(async () => {
    try {
      const res = await fetch('/api/session');
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`);
      const data: SessionResponse = await res.json();
      setSession(data);
      setError(null);
      void fetchPastSessions();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load session');
    } finally {
      setIsLoading(false);
    }
  }, [fetchPastSessions]);

  const resetSession = useCallback(async () => {
    try {
      const res = await fetch('/api/session/reset', { method: 'POST' });
      if (!res.ok) throw new Error(`Failed to reset session: HTTP ${res.status}`);
      await fetchSession();
    } catch (err) {
      console.error('Session reset error:', err);
    }
  }, [fetchSession]);

  const continueSession = useCallback(
    async (sessionId?: string): Promise<SessionResponse | null> => {
      try {
        const res = await fetch('/api/session/continue', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(sessionId ? { session_id: sessionId } : {}),
        });
        if (!res.ok) throw new Error(`Failed to continue session: HTTP ${res.status}`);
        const data: SessionResponse = await res.json();
        setSession(data);
        setError(null);
        void fetchPastSessions();
        return data;
      } catch (err) {
        console.error('Session continue error:', err);
        return null;
      }
    },
    [fetchPastSessions],
  );

  const dismissContinueSession = useCallback(
    async (sessionId?: string) => {
      try {
        const res = await fetch('/api/session/dismiss', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(sessionId ? { session_id: sessionId } : {}),
        });
        if (!res.ok) throw new Error(`Failed to dismiss session continuation: HTTP ${res.status}`);
        const data: SessionResponse = await res.json();
        setSession(data);
      } catch (err) {
        console.error('Session dismiss error:', err);
      }
    },
    [],
  );

  useEffect(() => {
    fetchSession();

    // Listen to shared SSE stream for instant updates without extra round-trip HTTP requests
    const unsubSessionUpdate = sharedSSEManager.subscribe('session_update', (data: SessionResponse) => {
      if (data && typeof data === 'object') {
        setSession(data);
        setError(null);
        setIsLoading(false);
        void fetchPastSessions();
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
  }, [fetchSession, fetchPastSessions]);

  return {
    session,
    pastSessions,
    isLoading,
    error,
    refreshSession: fetchSession,
    refreshPastSessions: fetchPastSessions,
    resetSession,
    continueSession,
    dismissContinueSession,
    selectedMatch,
    setSelectedMatch,
  };
}

