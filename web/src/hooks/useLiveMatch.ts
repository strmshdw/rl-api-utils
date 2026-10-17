import { useState, useEffect, useRef } from 'react';
import { CurrentMatchResponse, SessionMatchDetail } from '../types/api';
import { sharedSSEManager, ConnectionStatus, ClockUpdatePayload } from '../context/SSEContext';

export type { ConnectionStatus };

export function useLiveMatch() {
  const [match, setMatch] = useState<CurrentMatchResponse | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>(() => sharedSSEManager.getStatus());
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const [error, setError] = useState<string | null>(null);
  const pollIntervalRef = useRef<number | null>(null);

  // Initial HTTP poll to populate state immediately before SSE connects
  useEffect(() => {
    let mounted = true;

    const fetchCurrent = async () => {
      try {
        const res = await fetch('/api/current-match');
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data: CurrentMatchResponse = await res.json();
        if (mounted) {
          setMatch(data);
          setLastUpdated(new Date());
          setError(null);
        }
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : 'Failed to fetch current match');
        }
      }
    };

    fetchCurrent();

    return () => {
      mounted = false;
    };
  }, []);

  // Shared SSE connection integration
  useEffect(() => {
    const unsubStatus = sharedSSEManager.onStatusChange((newStatus) => {
      setStatus(newStatus);
      if (newStatus === 'offline' || newStatus === 'reconnecting') {
        if (!pollIntervalRef.current) {
          pollIntervalRef.current = window.setInterval(async () => {
            try {
              const res = await fetch('/api/current-match');
              if (res.ok) {
                const data: CurrentMatchResponse = await res.json();
                setMatch(data);
                setLastUpdated(new Date());
              }
            } catch {}
          }, 4000);
        }
      } else {
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current);
          pollIntervalRef.current = null;
        }
      }
    });

    const unsubMatchUpdate = sharedSSEManager.subscribe('match_update', (data: CurrentMatchResponse) => {
      setMatch(data);
      setLastUpdated(new Date());
      setError(null);
    });

    const unsubMatchEnded = sharedSSEManager.subscribe('match_ended', (detail: SessionMatchDetail) => {
      setMatch((prev) => {
        if (!prev) {
          return {
            active_match: false,
            match_ended: true,
            match_guid: detail.match_guid,
            playlist_id: detail.playlist_id,
            playlist_name: detail.playlist_name,
            winner_team: detail.winner_team,
            result: (detail.result as any) || '',
            local_team: detail.local_team,
            teammates: [],
            opponents: [],
            updated_at: new Date().toISOString(),
          };
        }
        return {
          ...prev,
          active_match: false,
          match_ended: true,
          winner_team: detail.winner_team !== undefined ? detail.winner_team : prev.winner_team,
          result: (detail.result as any) || prev.result,
          local_team: detail.local_team !== undefined ? detail.local_team : prev.local_team,
          updated_at: new Date().toISOString(),
        };
      });
      setLastUpdated(new Date());
    });

    const unsubClock = sharedSSEManager.subscribe('clock_updated', (data: ClockUpdatePayload) => {
      setMatch((prev) => {
        if (!prev) return prev;
        return {
          ...prev,
          time_remaining: data.time_remaining !== undefined ? data.time_remaining : prev.time_remaining,
          is_overtime: data.is_overtime !== undefined ? data.is_overtime : prev.is_overtime,
          updated_at: new Date().toISOString(),
        };
      });
    });

    return () => {
      unsubStatus();
      unsubMatchUpdate();
      unsubMatchEnded();
      unsubClock();
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };
  }, []);

  return {
    match,
    status,
    lastUpdated,
    error,
  };
}
