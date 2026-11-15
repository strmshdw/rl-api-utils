import React, { useState, useEffect, useCallback } from 'react';
import { SessionResponse, SessionMatchDetail, SessionListResponse } from '../types/api';
import { sharedSSEManager } from '../context/SSEContext';
import { useDashboard } from '../context/DashboardContext';
import { filterSessionByPlaylist, playlistFilterToParam } from '../utils/broadcast';
import { isDemoMode, SAMPLE_SESSION_RESPONSE } from '../utils/sampleData';

export interface UseSessionResult {
  session: SessionResponse | null;
  unfilteredSession?: SessionResponse | null;
  pastSessions: SessionResponse[];
  isLoading: boolean;
  error: string | null;
  refreshSession: () => Promise<void>;
  refreshPastSessions: () => Promise<void>;
  resetSession: () => Promise<void>;
  continueSession: (sessionId?: string) => Promise<SessionResponse | null>;
  dismissContinueSession: (sessionId?: string) => Promise<void>;
  selectedMatch: SessionMatchDetail | null;
  setSelectedMatch: React.Dispatch<React.SetStateAction<SessionMatchDetail | null>>;
}

export function useSession(playlistFilter?: string): UseSessionResult {
  const { dateRangeQueryParams, previewMode, isDevBuild } = useDashboard();
  const [session, setSession] = useState<SessionResponse | null>(null);
  const [unfilteredSession, setUnfilteredSession] = useState<SessionResponse | null>(null);
  const [pastSessions, setPastSessions] = useState<SessionResponse[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedMatch, setSelectedMatch] = useState<SessionMatchDetail | null>(null);

  const playlistParam = playlistFilter ? playlistFilterToParam(playlistFilter) : '';
  const demoFallbackEnabled = isDevBuild && (previewMode || isDemoMode());

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
      const baseParams = new URLSearchParams();
      if (dateRangeQueryParams.from) baseParams.set('from', dateRangeQueryParams.from);
      if (dateRangeQueryParams.to) baseParams.set('to', dateRangeQueryParams.to);
      const baseQs = baseParams.toString();
      const baseUrl = baseQs ? `/api/session?${baseQs}` : '/api/session';

      if (!playlistParam) {
        const res = await fetch(baseUrl);
        if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`);
        const data: SessionResponse = await res.json();
        const resolved =
          demoFallbackEnabled && (!data.matches || data.matches.length === 0)
            ? SAMPLE_SESSION_RESPONSE
            : data;
        setUnfilteredSession(resolved);
        setSession(resolved);
      } else {
        const filteredParams = new URLSearchParams(baseParams);
        filteredParams.set('playlist', playlistParam);
        const [baseRes, filteredRes] = await Promise.all([
          fetch(baseUrl),
          fetch(`/api/session?${filteredParams.toString()}`),
        ]);
        if (!baseRes.ok && !filteredRes.ok) {
          const failed = !filteredRes.ok ? filteredRes : baseRes;
          throw new Error(`HTTP ${failed.status}: ${failed.statusText}`);
        }
        const baseData: SessionResponse | null = baseRes.ok ? await baseRes.json() : null;
        const filteredData: SessionResponse | null = filteredRes.ok ? await filteredRes.json() : null;

        if (demoFallbackEnabled && (!baseData?.matches || baseData.matches.length === 0)) {
          setUnfilteredSession(SAMPLE_SESSION_RESPONSE);
          setSession(filterSessionByPlaylist(SAMPLE_SESSION_RESPONSE, playlistFilter));
        } else {
          if (baseData) setUnfilteredSession(baseData);
          const source = filteredData ?? baseData;
          setSession(filterSessionByPlaylist(source, playlistFilter));
        }
      }

      setError(null);
      void fetchPastSessions();
    } catch (err) {
      if (demoFallbackEnabled) {
        setUnfilteredSession(SAMPLE_SESSION_RESPONSE);
        setSession(
          playlistParam
            ? filterSessionByPlaylist(SAMPLE_SESSION_RESPONSE, playlistFilter)
            : SAMPLE_SESSION_RESPONSE,
        );
        setError(null);
      } else {
        setError(err instanceof Error ? err.message : 'Failed to load session');
      }
    } finally {
      setIsLoading(false);
    }
  }, [
    fetchPastSessions,
    dateRangeQueryParams.from,
    dateRangeQueryParams.to,
    playlistParam,
    playlistFilter,
    demoFallbackEnabled,
  ]);

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
        setUnfilteredSession(data);
        setSession(playlistParam ? filterSessionByPlaylist(data, playlistFilter) : data);
        setError(null);
        void fetchPastSessions();
        return data;
      } catch (err) {
        console.error('Session continue error:', err);
        return null;
      }
    },
    [fetchPastSessions, playlistParam, playlistFilter],
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
        setUnfilteredSession(data);
        setSession(playlistParam ? filterSessionByPlaylist(data, playlistFilter) : data);
      } catch (err) {
        console.error('Session dismiss error:', err);
      }
    },
    [playlistParam, playlistFilter],
  );

  useEffect(() => {
    fetchSession();

    // Listen to shared SSE stream for instant updates without extra round-trip HTTP requests
    const unsubSessionUpdate = sharedSSEManager.subscribe('session_update', (data: SessionResponse) => {
      if (data && typeof data === 'object') {
        const isEmpty = !data.matches || data.matches.length === 0;
        if (demoFallbackEnabled && isEmpty) {
          setUnfilteredSession(SAMPLE_SESSION_RESPONSE);
          setSession(
            playlistParam
              ? filterSessionByPlaylist(SAMPLE_SESSION_RESPONSE, playlistFilter)
              : SAMPLE_SESSION_RESPONSE,
          );
        } else {
          setUnfilteredSession(data);
          setSession(playlistParam ? filterSessionByPlaylist(data, playlistFilter) : data);
          if (playlistParam && !isEmpty) {
            void fetchSession();
          }
        }
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
  }, [fetchSession, fetchPastSessions, playlistParam, playlistFilter]);

  return {
    session,
    unfilteredSession,
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

