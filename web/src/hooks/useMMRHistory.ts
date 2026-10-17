import { useCallback, useEffect, useState } from 'react';
import { MMRHistoryEntry, MMRHistoryResponse } from '../types/api';
import { useSSEEvent } from '../context/SSEContext';

export interface UseMMRHistoryResult {
  history: MMRHistoryEntry[];
  byPlaylist: Record<number, MMRHistoryEntry[]>;
  loading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useMMRHistory(playlistId?: number, limit = 50): UseMMRHistoryResult {
  const [history, setHistory] = useState<MMRHistoryEntry[]>([]);
  const [byPlaylist, setByPlaylist] = useState<Record<number, MMRHistoryEntry[]>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchHistory = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const url = playlistId
        ? `/api/mmr-history?playlist_id=${playlistId}&limit=${limit}&order=asc`
        : `/api/mmr-history?limit=${limit}&order=asc`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data: MMRHistoryResponse = await res.json();
      const entries = data.history || [];
      setHistory(entries);

      const map: Record<number, MMRHistoryEntry[]> = {};
      for (const entry of entries) {
        if (!map[entry.playlist_id]) {
          map[entry.playlist_id] = [];
        }
        map[entry.playlist_id].push(entry);
      }
      setByPlaylist(map);
    } catch (err: any) {
      setError(err?.message || 'Failed to load MMR history');
    } finally {
      setLoading(false);
    }
  }, [playlistId, limit]);

  useEffect(() => {
    fetchHistory();
  }, [fetchHistory]);

  // Refresh MMR history when match ends or ranks update
  useSSEEvent('match_ended', fetchHistory);
  useSSEEvent('ranks_updated', fetchHistory);

  return { history, byPlaylist, loading, error, refresh: fetchHistory };
}
