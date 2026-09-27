import { useState, useEffect } from 'react';
import { PlayerSummary, PlayerSearchResponse } from '../types/api';

export function usePlayerSearch() {
  const [query, setQuery] = useState<string>('');
  const [debouncedQuery, setDebouncedQuery] = useState<string>('');
  const [platform, setPlatform] = useState<string>('all');
  const [page, setPage] = useState<number>(1);
  const [limit] = useState<number>(24);

  const [players, setPlayers] = useState<PlayerSummary[]>([]);
  const [total, setTotal] = useState<number>(0);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  // 300ms Debounce effect
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedQuery(query);
      setPage(1); // Reset to page 1 on new search term
    }, 300);
    return () => clearTimeout(timer);
  }, [query]);

  // Reset page when platform filter changes
  useEffect(() => {
    setPage(1);
  }, [platform]);

  useEffect(() => {
    const abortController = new AbortController();
    const offset = (page - 1) * limit;

    async function executeSearch() {
      setIsLoading(true);
      setError(null);
      try {
        const params = new URLSearchParams({
          query: debouncedQuery,
          platform: platform,
          limit: limit.toString(),
          offset: offset.toString(),
        });
        const res = await fetch(`/api/players?${params.toString()}`, {
          signal: abortController.signal,
        });
        if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`);

        const data = await res.json();
        if (Array.isArray(data)) {
          // Backward-compatible fallback if daemon returned raw array
          setPlayers(data);
          setTotal(data.length);
        } else {
          const resp = data as PlayerSearchResponse;
          setPlayers(resp.players || []);
          setTotal(resp.total || 0);
        }
      } catch (err) {
        if (!abortController.signal.aborted) {
          setError(err instanceof Error ? err.message : 'Error fetching players');
        }
      } finally {
        if (!abortController.signal.aborted) {
          setIsLoading(false);
        }
      }
    }

    executeSearch();
    return () => abortController.abort();
  }, [debouncedQuery, platform, page, limit]);

  return {
    query,
    setQuery,
    platform,
    setPlatform,
    page,
    setPage,
    limit,
    players,
    total,
    isLoading,
    error,
    totalPages: Math.max(1, Math.ceil(total / limit)),
  };
}
