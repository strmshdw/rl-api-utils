import { useState, useEffect } from 'react';
import { PlayerSummary, PlayerSearchResponse, GlobalSpotlights } from '../types/api';

export type PlayerSortField = 'last_seen' | 'total_matches' | 'win_rate' | 'player_name';
export type SortOrder = 'asc' | 'desc';
export type RoleFilter = 'all' | 'frequent' | 'teammate' | 'opponent' | 'synergy';

export interface UsePlayerSearchOptions {
  initialRole?: RoleFilter;
  initialSort?: PlayerSortField;
  initialOrder?: SortOrder;
  limit?: number;
}

export function usePlayerSearch(options: UsePlayerSearchOptions = {}) {
  const [query, setQuery] = useState<string>('');
  const [debouncedQuery, setDebouncedQuery] = useState<string>('');
  const [platform, setPlatform] = useState<string>('all');
  const [role, setRole] = useState<RoleFilter>(options.initialRole ?? 'all');
  const [sort, setSort] = useState<PlayerSortField>(options.initialSort ?? 'last_seen');
  const [order, setOrder] = useState<SortOrder>(options.initialOrder ?? 'desc');
  const [page, setPage] = useState<number>(1);
  const [limit] = useState<number>(options.limit ?? 24);

  const [players, setPlayers] = useState<PlayerSummary[]>([]);
  const [total, setTotal] = useState<number>(0);
  const [spotlights, setSpotlights] = useState<GlobalSpotlights | null>(null);
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

  // Reset page when platform, role, sort, or order changes
  useEffect(() => {
    setPage(1);
  }, [platform, role, sort, order]);

  useEffect(() => {
    const abortController = new AbortController();
    const offset = (page - 1) * limit;

    // Map UI sort field to backend query parameter
    let backendSort = 'last_seen';
    if (sort === 'total_matches') backendSort = 'match_count';
    else if (sort === 'player_name') backendSort = 'name';
    else if (sort === 'win_rate') backendSort = 'win_rate';

    async function executeSearch() {
      setIsLoading(true);
      setError(null);
      try {
        const params = new URLSearchParams({
          limit: limit.toString(),
          offset: offset.toString(),
        });
        if (debouncedQuery.trim()) params.set('query', debouncedQuery.trim());
        if (platform !== 'all') params.set('platform', platform);
        if (role !== 'all') params.set('role', role);
        if (backendSort) params.set('sort', backendSort);
        if (order) params.set('order', order);

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
          if (resp.spotlights) {
            setSpotlights(resp.spotlights);
          }
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
  }, [debouncedQuery, platform, role, sort, order, page, limit]);

  const toggleSort = (field: PlayerSortField) => {
    if (sort === field) {
      setOrder((prev) => (prev === 'asc' ? 'desc' : 'asc'));
    } else {
      setSort(field);
      setOrder(field === 'player_name' ? 'asc' : 'desc');
    }
  };

  return {
    query,
    setQuery,
    platform,
    setPlatform,
    role,
    setRole,
    sort,
    setSort,
    order,
    setOrder,
    toggleSort,
    page,
    setPage,
    limit,
    players,
    total,
    spotlights,
    isLoading,
    error,
    totalPages: Math.max(1, Math.ceil(total / limit)),
  };
}
