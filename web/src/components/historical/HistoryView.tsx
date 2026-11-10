import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { BallchasingSyncStatus, HistoricalMatchRecord, MatchesResponse } from '../../types/api';
import { isDemoMode, SAMPLE_HISTORICAL_MATCHES } from '../../utils/sampleData';
import { formatAgo, playlistLabels } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { TeamBoxScore } from '../common/BoxScore';
import { COLUMN_HEADER, GhostButton, PageHeader, PrimaryButton, ResultSlab, SearchInput, Segmented } from '../common/ui';
import { HistoricalMatchDetailModal } from './HistoricalMatchDetailModal';
import { historyOutcome, isLocal, playerNames, replayStatus, teamRows } from './historyData';
import { PlayerProfileDialog } from '../players/PlayerProfileDialog';
import { useDashboard } from '../../context/DashboardContext';

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
const ROW_GRID = '72px 120px minmax(0,1.2fr) minmax(0,1fr) 90px 150px 140px';

interface HistoryViewProps {
  /** Lower-cased online IDs of the local player, used to orient W/L and highlight "you". */
  localPlayerIds?: Set<string>;
  onOpenFullProfile?: (playerId: string, playerName?: string) => void;
}

interface HistoryRowProps {
  match: HistoricalMatchRecord;
  localIds: Set<string>;
  open: boolean;
  onToggle: () => void;
  onDetail: () => void;
  onRetry?: (guid: string, e: React.MouseEvent) => void;
  isRetrying?: boolean;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
}

const HistoryRow: React.FC<HistoryRowProps> = ({
  match,
  localIds,
  open,
  onToggle,
  onDetail,
  onRetry,
  isRetrying = false,
  onPlayerClick,
}) => {
  const players = match.stats?.players ?? [];
  const expandable = players.length > 0;
  const status = replayStatus(match);
  const duration = match.stats?.duration;
  const isFailed =
    (match.download_status || '').toUpperCase() === 'FAILED' || (match.upload_status || '').toUpperCase() === 'FAILED';

  return (
    <div data-testid="history-row" data-guid={match.match_guid} className="flex flex-col bg-panel t-border border border-line">
      <div
        role={expandable ? 'button' : undefined}
        tabIndex={expandable ? 0 : undefined}
        aria-expanded={expandable ? open : undefined}
        onClick={expandable ? onToggle : undefined}
        onKeyDown={
          expandable
            ? (e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onToggle();
                }
              }
            : undefined
        }
        className={`grid items-center h-16 transition-colors duration-120 outline-none ${
          expandable ? 'cursor-pointer hover:bg-panel-hover focus-visible:bg-panel-hover' : 'cursor-default'
        }`}
        style={{ gridTemplateColumns: ROW_GRID }}
      >
        <ResultSlab outcome={historyOutcome(match, localIds)} className="text-[20px]" />
        <div className="flex items-center gap-2 pl-[22px] font-extrabold text-[34px] leading-none t-font-mono">
          <span className="text-blue-ink">{match.blue_score}</span>
          <span className="text-faint text-[24px]">–</span>
          <span className="text-orange-ink">{match.orange_score}</span>
        </div>
        <div className="flex flex-col gap-0.5 min-w-0 pr-4">
          <span className="font-bold text-lg tracking-t6 truncate">{playlistLabels(match.playlist).name}</span>
          <span className="font-medium text-sm tracking-t6 text-muted truncate">
            {match.map_name || match.stats?.map_name || 'Unknown arena'}
          </span>
        </div>
        <span className="font-medium text-sm tracking-t4 text-muted truncate pr-4">
          {playerNames(players, localIds) || '—'}
        </span>
        <span className="text-right font-bold text-lg tracking-t4 text-ink-2 whitespace-nowrap t-font-mono">
          {duration ? formatDuration(duration) : '—'}
          {match.stats?.overtime && <span className="text-loss text-sm tracking-t12"> OT</span>}
        </span>
        <div className="flex items-center justify-end gap-2 pr-2">
          <span className={`text-right font-bold text-sm tracking-t10 whitespace-nowrap ${status.ink}`} title={status.title}>
            {status.href ? (
              <a
                href={status.href}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(e) => e.stopPropagation()}
                className="hover:text-blue-ink transition-colors duration-120"
              >
                {status.label}
              </a>
            ) : isRetrying ? (
              'RETRYING…'
            ) : (
              status.label
            )}
          </span>
          {isFailed && (
            <button
              type="button"
              title="Retry replay download & upload"
              disabled={isRetrying}
              onClick={(e) => onRetry?.(match.match_guid, e)}
              className="px-2 py-0.5 bg-loss/15 hover:bg-loss/25 text-loss border border-loss/30 text-sm font-bold rounded tracking-wider uppercase transition-colors disabled:opacity-50"
            >
              {isRetrying ? '…' : 'RETRY'}
            </button>
          )}
        </div>
        <span className="text-right pr-[22px] font-semibold text-sm tracking-t8 text-muted whitespace-nowrap">
          {formatAgo(match.stats?.date || match.created_at)}
        </span>
      </div>

      {open && expandable && (
        <div data-testid="history-box-score" className="border-t border-page p-4 t-bg-page bg-page flex flex-col gap-3">
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-[2px]">
            {[0, 1].map((team) => (
              <TeamBoxScore
                key={team}
                team={team as 0 | 1}
                label={team === 0 ? 'BLUE' : 'ORANGE'}
                rows={teamRows(players, team, localIds)}
                withRanks
                onPlayerClick={onPlayerClick}
              />
            ))}
          </div>
          <div className="flex flex-wrap justify-between items-center gap-2">
            <span className="text-sm font-extrabold text-muted">
              CLICK ANY PLAYER IN BOX SCORE FOR QUICK MODAL PROFILE (MMR SHOWN BEFORE RANK/DIV)
            </span>
            <button
              type="button"
              onClick={onDetail}
              className="px-4 py-2 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-sm uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
            >
              FULL 5-TAB BALLCHASING TELEMETRY →
            </button>
          </div>
        </div>
      )}
    </div>
  );
};

const EMPTY_IDS = new Set<string>();

export const HistoryView: React.FC<HistoryViewProps> = ({ localPlayerIds = EMPTY_IDS, onOpenFullProfile }) => {
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

  const [syncStatus, setSyncStatus] = useState<BallchasingSyncStatus | null>(null);
  const [isSyncing, setIsSyncing] = useState(false);
  const [syncMessage, setSyncMessage] = useState<string | null>(null);

  const [retryingGuids, setRetryingGuids] = useState<Set<string>>(new Set());
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
      const params = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((page - 1) * PAGE_SIZE) });
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

  useEffect(() => {
    fetch('/api/sync/ballchasing/status')
      .then((res) => (res.ok ? res.json() : null))
      .then((data: BallchasingSyncStatus | null) => {
        if (!data) return;
        setSyncStatus(data);
        if (data.running) setIsSyncing(true);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!isSyncing) return;
    const interval = setInterval(async () => {
      try {
        const res = await fetch('/api/sync/ballchasing/status');
        if (!res.ok) return;
        const data: BallchasingSyncStatus = await res.json();
        setSyncStatus(data);
        if (!data.running) {
          setIsSyncing(false);
          setSyncMessage(
            data.last_error
              ? `SYNC FAILED · ${data.last_error}`
              : `SYNCED ${data.synced_matches} MATCHES · ${data.synced_players} PLAYERS FROM BALLCHASING`,
          );
          fetchMatches();
        }
      } catch {
        // keep polling
      }
    }, 1000);
    return () => clearInterval(interval);
  }, [isSyncing, fetchMatches]);

  const triggerSync = async () => {
    setError(null);
    setSyncMessage(null);
    setSyncStatus(null);
    setIsSyncing(true);
    try {
      const res = await fetch('/api/sync/ballchasing', { method: 'POST' });
      if (!res.ok && res.status !== 409) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
    } catch (err) {
      setIsSyncing(false);
      setError(err instanceof Error ? err.message : 'Failed to trigger Ballchasing sync');
    }
  };

  const handleRetryMatch = async (guid: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (retryingGuids.has(guid)) return;
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
            ? { ...m, download_status: 'PENDING', upload_status: 'PENDING', retry_count: 0, last_error: '' }
            : m,
        ),
      );
      setTimeout(() => fetchMatches(), 1500);
    } catch (err) {
      console.error('Match retry failed:', err);
    } finally {
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
      const p = m.stats?.players?.find((pl) => isLocal(pl, effectiveLocalIds)) || m.stats?.players?.[0];
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
    debouncedQuery !== '' || playlistFilter !== 'all' || outcomeFilter !== 'all' || statusFilter !== 'all';

  return (
    <div data-testid="history-view" className="font-display">
      <PageHeader
        kicker="LOCAL STORE · REPLAYS + BALLCHASING"
        title="HISTORY"
        right={
          <PrimaryButton
            onClick={triggerSync}
            disabled={isSyncing}
            className={!isSyncing ? "before:content-['⟳_']" : undefined}
          >
            {isSyncing ? `SYNCING · ${syncStatus?.synced_matches ?? 0} MATCHES` : 'SYNC FROM BALLCHASING'}
          </PrimaryButton>
        }
      />

      {syncMessage && (
        <div className="mb-4 bg-panel border-l-4 border-blue-ink px-4 py-2 text-sm text-ink font-semibold flex items-center justify-between">
          <span>{syncMessage}</span>
          <button
            type="button"
            onClick={() => setSyncMessage(null)}
            className="text-muted hover:text-ink text-sm uppercase ml-4"
          >
            DISMISS
          </button>
        </div>
      )}

      {/* Filter & Sort Toolbar */}
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

      {error ? (
        <div className="bg-panel p-8 text-center text-loss font-semibold">{error}</div>
      ) : isLoading && matches.length === 0 ? (
        <div className="bg-panel p-8 text-center text-muted font-semibold">LOADING REPLAYS…</div>
      ) : rows.length === 0 ? (
        <div className="bg-panel p-8 text-center text-muted font-semibold">
          {hasFilters ? 'NO MATCHES FOUND MATCHING FILTERS' : 'NO MATCHES IN THE LOCAL STORE YET'}
        </div>
      ) : (
        <div className="flex flex-col gap-[2px]">
          <div className={`grid px-0 pb-2 ${COLUMN_HEADER}`} style={{ gridTemplateColumns: ROW_GRID }}>
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
              onToggle={() => setOpenMatchGuid((cur) => (cur === m.match_guid ? null : m.match_guid))}
              onDetail={() => setDetailMatch(m)}
              onRetry={handleRetryMatch}
              isRetrying={retryingGuids.has(m.match_guid)}
              onPlayerClick={(pId, pName) => {
                setProfilePlayerId(pId);
                setProfilePlayerName(pName);
              }}
            />
          ))}

          {/* Pagination */}
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
                <GhostButton onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={page >= totalPages}>
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
          onOpenFullProfile={onOpenFullProfile}
        />
      )}
    </div>
  );
};
