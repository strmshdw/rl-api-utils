import { useCallback, useEffect, useMemo, useState, type SetStateAction } from 'react';
import {
  CurrentMatchResponse,
  PlayerDetailResponse,
  PlayerMatchDetail,
  PlayerMatchesResponse,
  PlayerMatchup,
  PlayerPlaylistRank,
  PlayerRoleStatsSummary,
} from '../../types/api';
import {
  computeDynamicRelationshipTag,
  computeLiveThreatBadges,
  emptyRoleStats,
  formatArenaName,
  formatMmr,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  normalizeActiveModeKey,
  playlistFilterToParam,
  playlistLabels,
  shortRank,
  trackerUrl,
} from '../../utils/broadcast';
import {
  getSamplePlayerMatches,
  getSamplePlayerProfile,
  isDemoMode,
  SAMPLE_CURRENT_MATCH,
} from '../../utils/sampleData';
import {
  DateRangePreset,
  StatMode,
  buildDateRangeQueryParams,
  useAreaFilters,
  useDashboard,
} from '../../context/DashboardContext';
import { DEFAULT_CUSTOM_FROM, DEFAULT_CUSTOM_TO } from '../../context/dateRange';
import { parseRanks, sumMatchups } from './playerData';
import {
  PLAYLIST_SCOPE_IDS,
  ProfilePlaylistScope,
  ProfileResultFilter,
  ProfileRoleFilter,
  ProfileSortOption,
  resolveLocalOutcome,
} from './PlayerRoleSplitCard';

export interface CoreRankCard {
  playlistId: number;
  label: string;
  rank: string;
  mmr: string;
  games: number;
}

export interface PlayerDossierDataState {
  profile: PlayerDetailResponse | null;
  isLoadingProfile: boolean;
  error: string | null;
  id: string;
  name: string;
  platform: string;
  tracker: string | null;
  isOwnProfile: boolean;
  relTag: { tag: string; color: string };
  threatBadges: ReturnType<typeof computeLiveThreatBadges>;
  allRanks: PlayerPlaylistRank[];
  coreRankCards: CoreRankCard[];
  extraRanks: PlayerPlaylistRank[];
  playlistBreakdown: PlayerMatchup[];
  totals: ReturnType<typeof sumMatchups>;
  withStats: PlayerRoleStatsSummary;
  vsStats: PlayerRoleStatsSummary;
  mmrSparkline: number[];
  matches: PlayerMatchDetail[];
  filteredAndSortedMatches: PlayerMatchDetail[];
  isDemoMatches: boolean;
  totalMatches: number;
  totalPages: number;
  isLoadingMatches: boolean;
  pageSize: number;
  statPlaylist: ProfilePlaylistScope;
  setStatPlaylist: (next: ProfilePlaylistScope) => void;
  activePlaylistId: number;
  activePlaylistParam: string;
  setPlaylistById: (playlistId: number) => void;
  setPlaylistByModeKey: (modeKey: string) => void;
  dateRange: DateRangePreset;
  setDateRange: (range: DateRangePreset) => void;
  customFrom: string;
  setCustomFrom: (date: string) => void;
  customTo: string;
  setCustomTo: (date: string) => void;
  showCustomCalendar: boolean;
  setShowCustomCalendar: React.Dispatch<React.SetStateAction<boolean>>;
  statMode: StatMode;
  setStatMode: (mode: StatMode) => void;
  resetScopeFilters: () => void;
  roleFilter: ProfileRoleFilter;
  setRoleFilter: React.Dispatch<React.SetStateAction<ProfileRoleFilter>>;
  resultFilter: ProfileResultFilter;
  setResultFilter: React.Dispatch<React.SetStateAction<ProfileResultFilter>>;
  searchQuery: string;
  setSearchQuery: (q: string) => void;
  sortOption: ProfileSortOption;
  setSortOption: (opt: ProfileSortOption) => void;
  matchPage: number;
  setMatchPage: React.Dispatch<React.SetStateAction<number>>;
  clearFilters: () => void;
  saveNotes: (newNotes: string, newTags: string[]) => Promise<void>;
}

const CORE_SPECS: ReadonlyArray<{ id: number; label: string }> = [
  { id: 13, label: 'RANKED 3V3 (STANDARD)' },
  { id: 11, label: 'RANKED 2V2 (DOUBLES)' },
  { id: 10, label: 'RANKED 1V1 (DUEL)' },
];

export function playlistIdToScope(playlistId: number): ProfilePlaylistScope {
  const modeKey = modeKeyFromPlaylistId(playlistId);
  if (modeKey === '11') return '2v2';
  if (modeKey === '13') return '3v3';
  if (modeKey === '10') return '1v1';
  if (
    modeKey === '27' ||
    modeKey === '28' ||
    modeKey === '29' ||
    modeKey === '30' ||
    modeKey === '34'
  ) {
    return modeKey;
  }
  if (modeKey === 'casual') return 'casual';
  return '2v2';
}

export function parseDashboardPlaylist(activePlaylist: string): {
  scope: ProfilePlaylistScope;
  customId: number | null;
} {
  const norm = normalizeActiveModeKey(activePlaylist);
  if (norm === '11') return { scope: '2v2', customId: null };
  if (norm === '13') return { scope: '3v3', customId: null };
  if (norm === '10') return { scope: '1v1', customId: null };
  if (
    norm === '27' ||
    norm === '28' ||
    norm === '29' ||
    norm === '30' ||
    norm === '34'
  ) {
    return { scope: norm, customId: null };
  }
  if (norm === 'casual') return { scope: 'casual', customId: null };
  const num = Number(norm);
  if (Number.isInteger(num) && num > 0) {
    return { scope: '2v2', customId: num };
  }
  return { scope: '2v2', customId: null };
}

function mapSortOptionToQuery(opt: ProfileSortOption): { sortBy: string; sortDir: 'asc' | 'desc' } {
  switch (opt) {
    case 'date-asc':
      return { sortBy: 'date', sortDir: 'asc' };
    case 'pts-desc':
      return { sortBy: 'score', sortDir: 'desc' };
    case 'goals-desc':
      return { sortBy: 'goals', sortDir: 'desc' };
    case 'saves-desc':
      return { sortBy: 'saves', sortDir: 'desc' };
    case 'demos-desc':
      return { sortBy: 'demos', sortDir: 'desc' };
    case 'date-desc':
    default:
      return { sortBy: 'date', sortDir: 'desc' };
  }
}

export function usePlayerDossierData(
  playerId: string | null,
  playerName: string | undefined,
  localPlayerIds: Set<string>,
  options: { enabled?: boolean; pageSize?: number; liveMatch?: CurrentMatchResponse | null } = {},
): PlayerDossierDataState {
  const { enabled = true, pageSize = 20 } = options;
  const { stampingConfig } = useDashboard();
  const playersArea = useAreaFilters('players');

  const [profileEntry, setProfileEntry] = useState<{
    reqPlayerId: string;
    scopeKey: string;
    data: PlayerDetailResponse;
  } | null>(null);
  const [isLoadingProfile, setIsLoadingProfile] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const initialScope = parseDashboardPlaylist(playersArea.playlist);
  const [scopeTracker, setScopeTracker] = useState<{
    playerId: string | null;
    statPlaylist: ProfilePlaylistScope;
    customPlaylistId: number | null;
  }>({
    playerId,
    statPlaylist: initialScope.scope,
    customPlaylistId: initialScope.customId,
  });

  const [dateRange, setDateRangeState] = useState<DateRangePreset>('all');
  const [customFrom, setCustomFromState] = useState<string>(DEFAULT_CUSTOM_FROM);
  const [customTo, setCustomToState] = useState<string>(DEFAULT_CUSTOM_TO);
  const [showCustomCalendar, setShowCustomCalendar] = useState<boolean>(false);
  const [statMode, setStatMode] = useState<StatMode>('avg');

  const [roleFilter, setRoleFilter] = useState<ProfileRoleFilter>('all');
  const [resultFilter, setResultFilter] = useState<ProfileResultFilter>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [sortOption, setSortOption] = useState<ProfileSortOption>('date-desc');

  if (scopeTracker.playerId !== playerId) {
    const synced = parseDashboardPlaylist(playersArea.playlist);
    setScopeTracker({
      playerId,
      statPlaylist: synced.scope,
      customPlaylistId: synced.customId,
    });
    setRoleFilter('all');
    setResultFilter('all');
    setSearchQuery('');
    setSortOption('date-desc');
  }

  const statPlaylist =
    scopeTracker.playerId === playerId ? scopeTracker.statPlaylist : initialScope.scope;
  const customPlaylistId =
    scopeTracker.playerId === playerId ? scopeTracker.customPlaylistId : initialScope.customId;

  const setDateRange = useCallback((range: DateRangePreset) => {
    setDateRangeState(range);
    setShowCustomCalendar(range === 'custom');
  }, []);

  const setCustomFrom = useCallback(
    (date: string) => {
      if (date && customTo && date > customTo) setCustomToState(date);
      setCustomFromState(date);
      setDateRangeState('custom');
    },
    [customTo],
  );

  const setCustomTo = useCallback(
    (date: string) => {
      if (customFrom && date && customFrom > date) setCustomFromState(date);
      setCustomToState(date);
      setDateRangeState('custom');
    },
    [customFrom],
  );

  const dateRangeQueryParams = useMemo(
    () => buildDateRangeQueryParams(dateRange, customFrom, customTo),
    [dateRange, customFrom, customTo],
  );

  const activePlaylistId =
    customPlaylistId !== null ? customPlaylistId : PLAYLIST_SCOPE_IDS[statPlaylist];
  const activePlaylistParam =
    customPlaylistId !== null && customPlaylistId > 0
      ? String(customPlaylistId)
      : playlistFilterToParam(statPlaylist);

  const matchFilterKey = [
    playerId ?? '',
    activePlaylistParam,
    roleFilter,
    resultFilter,
    searchQuery.trim(),
    sortOption,
    dateRangeQueryParams.from ?? '',
    dateRangeQueryParams.to ?? '',
  ].join('\x1f');

  const [pageState, setPageState] = useState<{ filterKey: string; page: number }>({
    filterKey: matchFilterKey,
    page: 1,
  });
  if (pageState.filterKey !== matchFilterKey) {
    setPageState({ filterKey: matchFilterKey, page: 1 });
  }
  const matchPage = pageState.filterKey === matchFilterKey ? pageState.page : 1;

  const setMatchPage = useCallback(
    (value: SetStateAction<number>) => {
      setPageState((prev) => {
        const currentPage = prev.filterKey === matchFilterKey ? prev.page : 1;
        const nextPage = typeof value === 'function' ? value(currentPage) : value;
        return { filterKey: matchFilterKey, page: nextPage };
      });
    },
    [matchFilterKey],
  );

  const [matches, setMatches] = useState<PlayerMatchDetail[]>([]);
  const [isDemoMatches, setIsDemoMatches] = useState(false);
  const [totalMatches, setTotalMatches] = useState(0);
  const [isLoadingMatches, setIsLoadingMatches] = useState(false);

  const handleSetStatPlaylist = (next: ProfilePlaylistScope) => {
    setScopeTracker({
      playerId,
      statPlaylist: next,
      customPlaylistId: null,
    });
  };

  const setPlaylistById = (playlistId: number) => {
    if (playlistId <= 0) return;
    const modeKey = modeKeyFromPlaylistId(playlistId);
    const mapped = playlistIdToScope(playlistId);
    if (modeKey === 'private' || (mapped === '2v2' && playlistId !== 11)) {
      setScopeTracker({
        playerId,
        statPlaylist: '2v2',
        customPlaylistId: playlistId,
      });
      return;
    }
    handleSetStatPlaylist(mapped);
  };

  const setPlaylistByModeKey = (modeKey: string) => {
    const parsed = parseDashboardPlaylist(modeKey);
    setScopeTracker({
      playerId,
      statPlaylist: parsed.scope,
      customPlaylistId: parsed.customId,
    });
  };

  const resetScopeFilters = () => {
    setScopeTracker({ playerId, statPlaylist: '2v2', customPlaylistId: null });
    setDateRangeState('all');
    setShowCustomCalendar(false);
    setStatMode('avg');
  };

  const profileScopeKey = [
    playerId ?? '',
    activePlaylistParam,
    dateRangeQueryParams.from ?? '',
    dateRangeQueryParams.to ?? '',
  ].join('\x1f');

  const profile =
    enabled && playerId && profileEntry?.reqPlayerId === playerId
      ? profileEntry.data
      : null;
  const isFreshProfileScope = Boolean(profile && profileEntry?.scopeKey === profileScopeKey);

  useEffect(() => {
    if (!enabled || !playerId) return;

    const controller = new AbortController();
    setIsLoadingProfile(true);
    setError(null);

    const params = new URLSearchParams();
    if (activePlaylistParam) params.set('playlist', activePlaylistParam);
    if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
    if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);
    const qs = params.toString();
    const url = `/api/players/${encodeURIComponent(playerId)}${qs ? `?${qs}` : ''}`;

    fetch(url, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to load player`);
        return res.json();
      })
      .then((data: PlayerDetailResponse) =>
        setProfileEntry({ reqPlayerId: playerId, scopeKey: profileScopeKey, data }),
      )
      .catch((err) => {
        if (!controller.signal.aborted) {
          if (isDemoMode()) {
            setProfileEntry({
              reqPlayerId: playerId,
              scopeKey: profileScopeKey,
              data: getSamplePlayerProfile(playerId, playerName),
            });
          } else {
            setError(err instanceof Error ? err.message : 'Error fetching profile');
          }
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoadingProfile(false);
      });

    return () => controller.abort();
  }, [
    enabled,
    playerId,
    playerName,
    activePlaylistParam,
    profileScopeKey,
    dateRangeQueryParams.from,
    dateRangeQueryParams.to,
  ]);

  useEffect(() => {
    if (!enabled || !playerId) return;

    const controller = new AbortController();
    setIsLoadingMatches(true);

    const { sortBy, sortDir } = mapSortOptionToQuery(sortOption);
    const params = new URLSearchParams({
      limit: String(pageSize),
      offset: String((matchPage - 1) * pageSize),
    });
    if (roleFilter !== 'all') params.set('role', roleFilter);
    if (activePlaylistParam) params.set('playlist', activePlaylistParam);
    if (resultFilter === 'W') params.set('result', 'victory');
    if (resultFilter === 'L') params.set('result', 'defeat');
    if (searchQuery.trim()) params.set('search', searchQuery.trim());
    if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
    if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);
    params.set('sort_by', sortBy);
    params.set('sort_dir', sortDir);

    fetch(`/api/players/${encodeURIComponent(playerId)}/matches?${params.toString()}`, {
      signal: controller.signal,
    })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data: PlayerMatchesResponse) => {
        if ((!data.matches || data.matches.length === 0) && isDemoMode()) {
          const demoRows = getSamplePlayerMatches(playerId, playerName);
          setMatches(demoRows);
          setIsDemoMatches(true);
          setTotalMatches(demoRows.length);
        } else {
          setMatches(data.matches || []);
          setIsDemoMatches(false);
          setTotalMatches(data.total || 0);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          if (isDemoMode()) {
            const demoRows = getSamplePlayerMatches(playerId, playerName);
            setMatches(demoRows);
            setIsDemoMatches(true);
            setTotalMatches(demoRows.length);
          } else {
            setMatches([]);
            setIsDemoMatches(false);
            setTotalMatches(0);
          }
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoadingMatches(false);
      });

    return () => controller.abort();
  }, [
    enabled,
    playerId,
    playerName,
    pageSize,
    roleFilter,
    activePlaylistParam,
    resultFilter,
    searchQuery,
    sortOption,
    matchPage,
    dateRangeQueryParams.from,
    dateRangeQueryParams.to,
  ]);

  const id = profile?.player_id ?? playerId ?? '';
  const name = profile?.player_name || playerName || 'PLAYER';
  const platform = profile?.platform || id;
  const tracker = id ? trackerUrl(id, platform, name) : null;
  const allRanks = useMemo(() => parseRanks(profile?.ranks_json), [profile?.ranks_json]);

  const coreRankCards = useMemo(
    () =>
      CORE_SPECS.map((spec) => {
        const found = allRanks.find((r) => r.playlist_id === spec.id);
        return {
          playlistId: spec.id,
          label: spec.label,
          rank: found?.rank_name
            ? shortRank(found.rank_name, found.division, found.short_rank, found.tier)
            : 'UNRANKED',
          mmr: found?.mmr ? formatMmr(found.mmr) : '—',
          games: found?.matches_played ?? 0,
        };
      }),
    [allRanks],
  );

  const extraRanks = useMemo(
    () => allRanks.filter((r) => ![10, 11, 13].includes(r.playlist_id)),
    [allRanks],
  );

  const playlistBreakdown = useMemo(
    () => profile?.playlist_breakdown ?? profile?.matchups ?? [],
    [profile?.playlist_breakdown, profile?.matchups],
  );

  const scopedMatchups = useMemo(() => {
    const list = isFreshProfileScope
      ? profile?.matchups ?? []
      : profile?.playlist_breakdown ?? profile?.matchups ?? [];
    if (!activePlaylistParam) return list;
    return list.filter((m) => matchesPlaylistFilter(m.playlist_id, activePlaylistParam));
  }, [isFreshProfileScope, profile?.matchups, profile?.playlist_breakdown, activePlaylistParam]);

  const totals = useMemo(() => sumMatchups(scopedMatchups), [scopedMatchups]);

  const withStats = useMemo(
    () =>
      (isFreshProfileScope ? profile?.with_you_stats : undefined) ??
      emptyRoleStats(totals.mate.wins + totals.mate.losses, totals.mate.wins, totals.mate.losses),
    [isFreshProfileScope, profile?.with_you_stats, totals.mate.wins, totals.mate.losses],
  );

  const vsStats = useMemo(
    () =>
      (isFreshProfileScope ? profile?.against_you_stats : undefined) ??
      emptyRoleStats(totals.vs.wins + totals.vs.losses, totals.vs.wins, totals.vs.losses),
    [isFreshProfileScope, profile?.against_you_stats, totals.vs.wins, totals.vs.losses],
  );

  const isOwnProfile = useMemo(() => {
    const lowerId = (id || '').toLowerCase();
    return (
      profile?.relationship_tag === 'YOU' ||
      lowerId === 'me' ||
      lowerId === 'you' ||
      localPlayerIds.has(id) ||
      localPlayerIds.has(lowerId) ||
      (id.includes('|') && localPlayerIds.has(id.split('|')[1]))
    );
  }, [profile?.relationship_tag, id, localPlayerIds]);

  const relTag = useMemo(
    () =>
      computeDynamicRelationshipTag(
        totals.mate.wins,
        totals.mate.losses,
        totals.vs.wins,
        totals.vs.losses,
        isOwnProfile ? 'YOU' : false,
        { minGames: stampingConfig.relationship_min_games },
      ),
    [totals, isOwnProfile, stampingConfig.relationship_min_games],
  );

  const threatBadges = useMemo(() => {
    const activeMatch =
      options.liveMatch !== undefined
        ? options.liveMatch
        : isDemoMode()
        ? SAMPLE_CURRENT_MATCH
        : null;
    if (!activeMatch?.active_match) return [];
    const allLobby = [
      ...(activeMatch.local_player ? [activeMatch.local_player] : []),
      ...(activeMatch.teammates || []),
      ...(activeMatch.opponents || []),
    ];
    const target = allLobby.find(
      (lp) => lp.player_id === id || lp.name.toLowerCase() === name.toLowerCase(),
    );
    if (!target) return [];
    return computeLiveThreatBadges(
      target,
      activeMatch.local_player,
      allLobby,
      activeMatch.playlist_id,
      stampingConfig,
    );
  }, [id, name, options.liveMatch, stampingConfig]);

  const filteredAndSortedMatches = useMemo(() => {
    if (!isDemoMatches) {
      return matches;
    }
    const q = searchQuery.trim().toLowerCase();
    const list = matches.filter((m) => {
      if (activePlaylistParam && !matchesPlaylistFilter(m.playlist, activePlaylistParam)) {
        return false;
      }
      if (roleFilter !== 'all' && m.role !== roleFilter) return false;
      const localOutcome = resolveLocalOutcome(m);
      if (resultFilter === 'W' && localOutcome && localOutcome !== 'victory') return false;
      if (resultFilter === 'L' && localOutcome && localOutcome !== 'defeat') return false;
      if (q) {
        const arena = formatArenaName(m.map_name || m.stats?.map_name);
        const rosterHay = (m.stats?.players ?? [])
          .map((p) => `${p.name || ''} ${p.online_id || ''} ${p.car_name || ''}`)
          .join(' ');
        const hay = `${m.map_name || ''} ${arena} ${playlistLabels(m.playlist).name} ${m.match_guid} ${m.player_stats?.car_name || ''} ${rosterHay}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });

    const pStat = (m: PlayerMatchDetail, k: 'score' | 'goals' | 'saves' | 'demos') =>
      Number(m.player_stats?.[k]) || 0;

    return [...list].sort((a, b) => {
      const tA = new Date(a.stats?.date || a.created_at).getTime() || a.record_start_timestamp || 0;
      const tB = new Date(b.stats?.date || b.created_at).getTime() || b.record_start_timestamp || 0;
      if (sortOption === 'date-asc') return tA - tB;
      if (sortOption === 'pts-desc') return pStat(b, 'score') - pStat(a, 'score');
      if (sortOption === 'goals-desc') return pStat(b, 'goals') - pStat(a, 'goals');
      if (sortOption === 'saves-desc') return pStat(b, 'saves') - pStat(a, 'saves');
      if (sortOption === 'demos-desc') return pStat(b, 'demos') - pStat(a, 'demos');
      return tB - tA;
    });
  }, [matches, isDemoMatches, activePlaylistParam, roleFilter, resultFilter, searchQuery, sortOption]);

  const mmrSparkline = useMemo(() => {
    const oid = id.includes('|') ? id.split('|')[1].toLowerCase() : id.toLowerCase();
    const nameLower = name.toLowerCase();
    const fromMatches = [...matches]
      .reverse()
      .map((m) => {
        const p = m.stats?.players?.find(
          (pl) =>
            pl.online_id?.toLowerCase() === oid ||
            pl.name?.toLowerCase() === nameLower,
        );
        return p?.mmr || p?.rank?.mmr || 0;
      })
      .filter((v) => v > 0);
    return fromMatches.length >= 2 ? fromMatches : [];
  }, [matches, id, name]);

  const saveNotes = async (newNotes: string, newTags: string[]) => {
    const res = await fetch(`/api/players/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ notes: newNotes, tags: newTags }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to update player`);
    setProfileEntry((prev) =>
      prev ? { ...prev, data: { ...prev.data, notes: newNotes, tags: newTags } } : null,
    );
  };

  const clearFilters = () => {
    setRoleFilter('all');
    setResultFilter('all');
    setSearchQuery('');
  };

  return {
    profile,
    isLoadingProfile,
    error,
    id,
    name,
    platform,
    tracker,
    isOwnProfile,
    relTag,
    threatBadges,
    allRanks,
    coreRankCards,
    extraRanks,
    playlistBreakdown,
    totals,
    withStats,
    vsStats,
    mmrSparkline,
    matches,
    filteredAndSortedMatches,
    isDemoMatches,
    totalMatches,
    totalPages: Math.max(1, Math.ceil(totalMatches / pageSize)),
    isLoadingMatches,
    pageSize,
    statPlaylist,
    setStatPlaylist: handleSetStatPlaylist,
    activePlaylistId,
    activePlaylistParam,
    setPlaylistById,
    setPlaylistByModeKey,
    dateRange,
    setDateRange,
    customFrom,
    setCustomFrom,
    customTo,
    setCustomTo,
    showCustomCalendar,
    setShowCustomCalendar,
    statMode,
    setStatMode,
    resetScopeFilters,
    roleFilter,
    setRoleFilter,
    resultFilter,
    setResultFilter,
    searchQuery,
    setSearchQuery,
    sortOption,
    setSortOption,
    matchPage,
    setMatchPage,
    clearFilters,
    saveNotes,
  };
}
