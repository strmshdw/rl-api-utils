import { useState, useEffect, useRef } from 'react';
import { CurrentMatchResponse } from '../types/api';

export type ConnectionStatus = 'connected' | 'connecting' | 'reconnecting' | 'offline';

export function useLiveMatch() {
  const [match, setMatch] = useState<CurrentMatchResponse | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>('connecting');
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

  // SSE event stream connection
  useEffect(() => {
    let es: EventSource | null = null;
    let reconnectTimeout: number | null = null;

    const startPollingFallback = () => {
      if (pollIntervalRef.current) return;
      pollIntervalRef.current = window.setInterval(async () => {
        try {
          const res = await fetch('/api/current-match');
          if (res.ok) {
            const data: CurrentMatchResponse = await res.json();
            setMatch(data);
            setLastUpdated(new Date());
            setStatus('reconnecting');
          }
        } catch {
          setStatus('offline');
        }
      }, 4000);
    };

    const stopPollingFallback = () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };

    const connectSSE = () => {
      try {
        es = new EventSource('/api/events');

        es.onopen = () => {
          setStatus('connected');
          setError(null);
          stopPollingFallback();
        };

        es.addEventListener('match_update', (event: MessageEvent) => {
          try {
            const data: CurrentMatchResponse = JSON.parse(event.data);
            setMatch(data);
            setLastUpdated(new Date());
          } catch (e) {
            console.error('Failed to parse match_update event', e);
          }
        });

        es.addEventListener('match_ended', (event: MessageEvent) => {
          try {
            const data: CurrentMatchResponse = JSON.parse(event.data);
            setMatch(data);
            setLastUpdated(new Date());
          } catch (e) {
            console.error('Failed to parse match_ended event', e);
          }
        });

        es.onerror = () => {
          setStatus('reconnecting');
          es?.close();
          startPollingFallback();
          reconnectTimeout = window.setTimeout(connectSSE, 5000);
        };
      } catch (e) {
        setStatus('offline');
        startPollingFallback();
      }
    };

    connectSSE();

    return () => {
      es?.close();
      stopPollingFallback();
      if (reconnectTimeout) clearTimeout(reconnectTimeout);
    };
  }, []);

  return {
    match,
    status,
    lastUpdated,
    error,
  };
}
