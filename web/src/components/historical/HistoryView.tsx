import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CurrentMatchResponse, HistoricalMatchRecord, MatchesResponse } from '../../types/api';
import { isDemoMode, SAMPLE_HISTORICAL_MATCHES } from '../../utils/sampleData';
import { COLUMN_HEADER, GhostButton, PageHeader, PrimaryButton, SearchInput, Segmented } from '../common/ui';
import { HistoricalMatchDetailModal } from './HistoricalMatchDetailModal';
import { HISTORY_ROW_GRID, HistoryRow } from './HistoryRow';
import { historyOutcome, isLocal, replayStatus } from './historyData';
import { PlayerProfileDialog } from '../players/PlayerProfileDialog';
import { useDashboard } from '../../context/DashboardContext';
import { useBallchasingSync } from '../../hooks/useBallchasingSync';

type PlaylistFilter = 'all' | 'duel' | 'doubles' | 'standard';
type StatusFilter = 'all' | 'BALLCHASING' | 'LOCAL';
type OutcomeFilter = 'all' | 'W' | 'L';
type HistorySortOption =
  | 'date-desc'
  | 'date-asc'
  | 'pts-desc'
  | 'goals-desc'
  | 'saves-desc'
  | 'demos-desc'
  | 'duration-desc';

const PLAYLIST_OPTIONS = [
  ['all', 'ALL MODES'],
  ['duel', '1V1'],
  ['doubles', '2V2'],
  ['standard', '3V3'],
] as const;
const PLAYLIST_IDS: Record<PlaylistFilter, number> = { all: 0, duel: 10, doubles: 11, standard: 13 };

const STATUS_OPTIONS = [
  ['all', 'ALL STATUS'],
  ['BALLCHASING', 'BALLCHASING'],
  ['LOCAL', 'LOCAL'],
] as const;

const OUTCOME_OPTIONS = [
  ['all', 'ALL'],
  ['W', 'WINS'],
  ['L', 'LOSSES'],
] as const;

const PAGE_SIZE = 20;
const EMPTY_IDS = new Set<string>();

interface HistoryViewProps {
  /** Lower-cased online IDs of the local player, used to orient W/L and highlight "you". */
  localPlayerIds?: Set<string>;
  liveMatch?: CurrentMatchResponse | null;
  onOpenFullProfile?: (playerId: string, playerName?: string) => void;
}

export const HistoryView: React.FC<HistoryViewProps> = ({
  localPlayerIds = EMPTY_IDS,
  liveMatch,
  onOpenFullProfile,
}) => {
  const { dateRangeQueryParams } = useDashboard();
  const [matches, setMatches] = useState<HistoricalMatchRecord[]>([]);
  const [total, setTotal] = useState(0);
  const [serverLocalIds, setServerLocalIds] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [page, setPage] = useState(1);
  const [query, setQuery] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const [playlistFilter, setPlaylistFilter] = useState<PlaylistFilter>('all');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeFilter>('all');
  const [sortOption, setSortOption] = useState<HistorySortOption>('date-desc');
  const [openMatchGuid, setOpenMatchGuid] = useState<string | null>(null);
  const [detailMatch, setDetailMatch] = useState<HistoricalMatchRecord | null>(null);

  const [retryingGuids, setRetryingGuids] = useState<Set<string>>(new Set());
  const retryingRef = useRef<Set<string>>(new Set());
  const [profilePlayerId, setProfilePlayerId] = useState<string | null>(null);
  const [profilePlayerName, setProfilePlayerName] = useState<string | undefined>(undefined);

  const effectiveLocalIds = useMemo(() => {
    if (serverLocalIds.length === 0) return localPlayerIds;
    const merged = new Set<string>(localPlayerIds);
    serverLocalIds.forEach((id) => {
      if (id) {
        const lower = id.toLowerCase();
        merged.add(lower);
        const parts = lower.split('|');
        if (parts.length >= 2 && parts[1]) merged.add(parts[1]);
      }
    });
    return merged;
  }, [localPlayerIds, serverLocalIds]);

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedQuery(query.trim());
      setPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [query]);

  const fetchMatches = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    const playlistId = PLAYLIST_IDS[playlistFilter];
    try {
      const params = new URLSearchParams({
        limit: String(PAGE_SIZE),
        offset: String((page - 1) * PAGE_SIZE),
      });
      if (playlistId > 0) params.set('playlist', String(playlistId));
      if (debouncedQuery) {
        params.set('query', debouncedQuery);
        params.set('search', debouncedQuery);
      }
      if (outcomeFilter === 'W') params.set('result', 'victory');
      if (outcomeFilter === 'L') params.set('result', 'defeat');
      if (statusFilter === 'BALLCHASING') params.set('status', 'ballchasing');
      if (statusFilter === 'LOCAL') params.set('status', 'local');
      if (sortOption !== 'date-desc') {
        const [field, dir] = sortOption.split('-');
        const mapField: Record<string, string> = {
          date: 'date',
          pts: 'score',
          goals: 'goals',
          saves: 'saves',
          demos: 'demos',
          duration: 'duration',
        };
        params.set('sort_by', mapField[field] || 'date');
        params.set('sort_dir', dir || 'desc');
      }
      if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
      if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);

      const res = await fetch(`/api/matches?${params.toString()}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`);
      const data: MatchesResponse = await res.json();

      if (data.local_player_ids?.length) {
        setServerLocalIds(data.local_player_ids);
      }

      if (data.matches.length === 0 && isDemoMode() && !debouncedQuery && playlistId === 0) {
        setMatches(SAMPLE_HISTORICAL_MATCHES);
        setTotal(SAMPLE_HISTORICAL_MATCHES.length);
      } else {
        setMatches(data.matches);
        setTotal(data.total);
      }
    } catch (err) {
      if (isDemoMode()) {
        setMatches(SAMPLE_HISTORICAL_MATCHES);
        setTotal(SAMPLE_HISTORICAL_MATCHES.length);
      } else {
        setError(err instanceof Error ? err.message : 'Failed to fetch historical matches');
      }
    } finally {
      setIsLoading(false);
    }
  }, [
    page,
    playlistFilter,
    debouncedQuery,
    outcomeFilter,
    statusFilter,
    sortOption,
    dateRangeQueryParams.from,
    dateRangeQueryParams.to,
  ]);

  useEffect(() => {
    fetchMatches();
  }, [fetchMatches]);

  const {
    syncStatus,
    isSyncing,
    syncMessage,
    error: syncError,
    triggerSync,
    dismissSyncMessage,
  } = useBallchasingSync({ onSyncComplete: fetchMatches });

  const handleRetryMatch = async (guid: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (retryingRef.current.has(guid) || retryingGuids.has(guid)) return;
    retryingRef.current.add(guid);
    setRetryingGuids((prev) => new Set(prev).add(guid));
    try {
      const res = await fetch(`/api/matches/${encodeURIComponent(guid)}/retry`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      setMatches((prev) =>
        prev.map((m) =>
          m.match_guid === guid
            ? {
                ...m,
                download_status: 'PENDING',
                upload_status: 'PENDING',
                retry_count: 0,
                last_error: '',
              }
            : m,
        ),
      );
      setTimeout(() => fetchMatches(), 1500);
    } catch (err) {
      console.error('Match retry failed:', err);
    } finally {
      retryingRef.current.delete(guid);
      setRetryingGuids((prev) => {
        const next = new Set(prev);
        next.delete(guid);
        return next;
      });
    }
  };

  const rows = useMemo(() => {
    const filtered = matches.filter((m) => {
      if (outcomeFilter !== 'all' && historyOutcome(m, effectiveLocalIds) !== outcomeFilter) return false;
      if (statusFilter !== 'all') {
        const st = replayStatus(m);
        const isBc = st.label.startsWith('BALLCHASING');
        if (statusFilter === 'BALLCHASING' && !isBc) return false;
        if (statusFilter === 'LOCAL' && isBc) return false;
      }
      return true;
    });

    if (sortOption === 'date-desc') return filtered;

    const localStat = (m: HistoricalMatchRecord, k: 'score' | 'goals' | 'saves' | 'demos') => {
      const p =
        m.stats?.players?.find((pl) => isLocal(pl, effectiveLocalIds)) || m.stats?.players?.[0];
      return p ? Number(p[k]) || 0 : 0;
    };

    return [...filtered].sort((a, b) => {
      if (sortOption === 'date-asc') {
        const tA = new Date(a.stats?.date || a.created_at).getTime() || a.record_start_timestamp;
        const tB = new Date(b.stats?.date || b.created_at).getTime() || b.record_start_timestamp;
        return tA - tB;
      }
      if (sortOption === 'pts-desc') return localStat(b, 'score') - localStat(a, 'score');
      if (sortOption === 'goals-desc') return localStat(b, 'goals') - localStat(a, 'goals');
      if (sortOption === 'saves-desc') return localStat(b, 'saves') - localStat(a, 'saves');
      if (sortOption === 'demos-desc') return localStat(b, 'demos') - localStat(a, 'demos');
      if (sortOption === 'duration-desc') return (b.stats?.duration || 0) - (a.stats?.duration || 0);
      return 0;
    });
  }, [matches, outcomeFilter, statusFilter, sortOption, effectiveLocalIds]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const hasFilters =
    debouncedQuery !== '' ||
    playlistFilter !== 'all' ||
    outcomeFilter !== 'all' ||
    statusFilter !== 'all';
  const combinedError = syncError || error;

  return (
    <div data-testid="history-view" className="font-display">
      <PageHeader
        kicker="LOCAL STORE · REPLAYS + BALLCHASING"
        title="HISTORY"
        right={
          <PrimaryButton
            onClick={() => triggerSync()}
            disabled={isSyncing}
            className={!isSyncing ? "before:content-['⟳_']" : undefined}
          >
            {isSyncing
              ? `SYNCING · ${syncStatus?.synced_matches ?? 0} MATCHES`
              : 'SYNC FROM BALLCHASING'}
          </PrimaryButton>
        }
      />

      {syncMessage && (
        <div className="mb-4 bg-panel border-l-4 border-blue-ink px-4 py-2 text-sm text-ink font-semibold flex items-center justify-between">
          <span>{syncMessage}</span>
          <button
            type="button"
            onClick={dismissSyncMessage}
            className="text-muted hover:text-ink text-sm uppercase ml-4"
          >
            DISMISS
          </button>
        </div>
      )}

      <div className="t-bg-panel bg-panel t-border border border-line p-3.5 mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <SearchInput
            id="history-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="SEARCH ARENA, PLAYER OR GUID…"
            className="w-64 sm:w-72"
          />
          <Segmented
            label="Filter by playlist"
            options={PLAYLIST_OPTIONS}
            value={playlistFilter}
            onChange={(v) => {
              setPlaylistFilter(v);
              setPage(1);
            }}
          />
          <Segmented
            label="Filter by status"
            options={STATUS_OPTIONS}
            value={statusFilter}
            onChange={(v) => {
              setStatusFilter(v);
              setPage(1);
            }}
          />
          <Segmented
            label="Filter by result"
            options={OUTCOME_OPTIONS}
            value={outcomeFilter}
            onChange={(v) => {
              setOutcomeFilter(v);
              setPage(1);
            }}
          />
        </div>

        <div className="flex items-center gap-2">
          <select
            id="history-sort"
            aria-label="Sort matches"
            value={sortOption}
            onChange={(e) => setSortOption(e.target.value as HistorySortOption)}
            className="h-[38px] t-bg-page bg-page t-border border border-outline px-3 text-sm font-extrabold t-text-strong text-ink-strong uppercase outline-none t-radius"
          >
            <option value="date-desc">SORT: NEWEST FIRST</option>
            <option value="date-asc">SORT: OLDEST FIRST</option>
            <option value="pts-desc">SORT: YOUR SCORE</option>
            <option value="goals-desc">SORT: YOUR GOALS</option>
            <option value="saves-desc">SORT: YOUR SAVES</option>
            <option value="demos-desc">SORT: YOUR DEMOS</option>
            <option value="duration-desc">SORT: DURATION</option>
          </select>
        </div>
      </div>

      {combinedError ? (
        <div className="bg-panel p-8 text-center text-loss font-semibold">{combinedError}</div>
      ) : isLoading && matches.length === 0 ? (
        <div className="bg-panel p-8 text-center text-muted font-semibold">LOADING REPLAYS…</div>
      ) : rows.length === 0 ? (
        <div className="bg-panel p-8 text-center text-muted font-semibold">
          {hasFilters ? 'NO MATCHES FOUND MATCHING FILTERS' : 'NO MATCHES IN THE LOCAL STORE YET'}
        </div>
      ) : (
        <div className="flex flex-col gap-[2px]">
          <div
            className={`grid px-0 pb-2 ${COLUMN_HEADER}`}
            style={{ gridTemplateColumns: HISTORY_ROW_GRID }}
          >
            <span className="pl-4">RESULT</span>
            <span className="pl-[22px]">SCORE</span>
            <span>PLAYLIST · ARENA</span>
            <span>ROSTER</span>
            <span className="text-right">DURATION</span>
            <span className="text-right pr-2">STATUS</span>
            <span className="text-right pr-[22px]">PLAYED</span>
          </div>

          {rows.map((m) => (
            <HistoryRow
              key={m.match_guid}
              match={m}
              localIds={effectiveLocalIds}
              open={openMatchGuid === m.match_guid}
              onToggle={() =>
                setOpenMatchGuid((cur) => (cur === m.match_guid ? null : m.match_guid))
              }
              onDetail={() => setDetailMatch(m)}
              onRetry={handleRetryMatch}
              isRetrying={retryingGuids.has(m.match_guid)}
              onPlayerClick={(pId, pName) => {
                setProfilePlayerId(pId);
                setProfilePlayerName(pName);
              }}
            />
          ))}

          {totalPages > 1 && (
            <div className="flex items-center justify-between pt-4 px-2">
              <span className="text-muted text-sm font-semibold">
                SHOWING {rows.length} OF {total} MATCHES
              </span>
              <div className="flex items-center gap-2">
                <GhostButton onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page <= 1}>
                  PREV
                </GhostButton>
                <span className="font-semibold text-sm text-ink-2 px-2">
                  PAGE {page} OF {totalPages}
                </span>
                <GhostButton
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  disabled={page >= totalPages}
                >
                  NEXT
                </GhostButton>
              </div>
            </div>
          )}
        </div>
      )}

      {detailMatch && (
        <HistoricalMatchDetailModal
          match={detailMatch}
          onClose={() => setDetailMatch(null)}
          localPlayerIds={effectiveLocalIds}
          onPlayerClick={(pId, pName) => {
            setProfilePlayerId(pId);
            setProfilePlayerName(pName);
          }}
        />
      )}

      {profilePlayerId && (
        <PlayerProfileDialog
          playerId={profilePlayerId}
          playerName={profilePlayerName}
          onClose={() => setProfilePlayerId(null)}
          localPlayerIds={effectiveLocalIds}
          liveMatch={liveMatch}
          onOpenFullProfile={onOpenFullProfile}
        />
      )}
    </div>
  );
};
