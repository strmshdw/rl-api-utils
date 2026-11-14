import { useEffect, useMemo, useState } from 'react';
import { PlayerDetailResponse, PlayerMatchDetail, PlayerMatchesResponse, PlayerPlaylistRank, PlayerRoleStatsSummary } from '../../types/api';
import { computeDynamicRelationshipTag, computeLiveThreatBadges, emptyRoleStats, formatMmr, playlistLabels, shortRank, trackerUrl } from '../../utils/broadcast';
import { getSamplePlayerMatches, getSamplePlayerProfile, isDemoMode, SAMPLE_CURRENT_MATCH } from '../../utils/sampleData';
import { useDashboard } from '../../context/DashboardContext';
import { parseRanks, sumMatchups } from './playerData';
import { PLAYLIST_SCOPE_IDS, ProfilePlaylistScope, ProfileResultFilter, ProfileRoleFilter, ProfileSortOption, resolveLocalOutcome } from './PlayerRoleSplitCard';

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
  totals: ReturnType<typeof sumMatchups>;
  withStats: PlayerRoleStatsSummary;
  vsStats: PlayerRoleStatsSummary;
  mmrSparkline: number[];
  matches: PlayerMatchDetail[];
  filteredAndSortedMatches: PlayerMatchDetail[];
  totalMatches: number;
  totalPages: number;
  isLoadingMatches: boolean;
  pageSize: number;
  statPlaylist: ProfilePlaylistScope;
  setStatPlaylist: (next: ProfilePlaylistScope) => void;
  activePlaylistId: number;
  setPlaylistById: (playlistId: number) => void;
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
  if (playlistId === 11) return '2v2';
  if (playlistId === 13) return '3v3';
  if (playlistId === 10) return '1v1';
  return 'all';
}

export function usePlayerDossierData(
  playerId: string | null,
  playerName: string | undefined,
  localPlayerIds: Set<string>,
  options: { enabled?: boolean; pageSize?: number } = {},
): PlayerDossierDataState {
  const { enabled = true, pageSize = 20 } = options;
  const { dateRangeQueryParams } = useDashboard();

  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoadingProfile, setIsLoadingProfile] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [statPlaylist, setStatPlaylist] = useState<ProfilePlaylistScope>('all');
  const [customPlaylistId, setCustomPlaylistId] = useState<number | null>(null);
  const [roleFilter, setRoleFilter] = useState<ProfileRoleFilter>('all');
  const [resultFilter, setResultFilter] = useState<ProfileResultFilter>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [sortOption, setSortOption] = useState<ProfileSortOption>('date-desc');
  const [matchPage, setMatchPage] = useState(1);

  const [matches, setMatches] = useState<PlayerMatchDetail[]>([]);
  const [totalMatches, setTotalMatches] = useState(0);
  const [isLoadingMatches, setIsLoadingMatches] = useState(false);

  const activePlaylistId =
    customPlaylistId !== null ? customPlaylistId : PLAYLIST_SCOPE_IDS[statPlaylist];

  const handleSetStatPlaylist = (next: ProfilePlaylistScope) => {
    setCustomPlaylistId(null);
    setStatPlaylist(next);
    setMatchPage(1);
  };

  const setPlaylistById = (playlistId: number) => {
    if (playlistId === 0 || activePlaylistId === playlistId) {
      handleSetStatPlaylist('all');
      return;
    }
    const mapped = playlistIdToScope(playlistId);
    if (mapped !== 'all') {
      handleSetStatPlaylist(mapped);
    } else {
      setStatPlaylist('all');
      setCustomPlaylistId(playlistId);
      setMatchPage(1);
    }
  };

  useEffect(() => {
    setStatPlaylist('all');
    setCustomPlaylistId(null);
    setRoleFilter('all');
    setResultFilter('all');
    setSearchQuery('');
    setSortOption('date-desc');
    setMatchPage(1);
  }, [playerId]);

  useEffect(() => {
    setProfile(null);
    if (!enabled || !playerId) return;

    const controller = new AbortController();
    setIsLoadingProfile(true);
    setError(null);

    const params = new URLSearchParams();
    if (activePlaylistId > 0) params.set('playlist', String(activePlaylistId));
    if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
    if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);
    const qs = params.toString();
    const url = `/api/players/${encodeURIComponent(playerId)}${qs ? `?${qs}` : ''}`;

    fetch(url, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to load player`);
        return res.json();
      })
      .then((data: PlayerDetailResponse) => setProfile(data))
      .catch((err) => {
        if (!controller.signal.aborted) {
          if (isDemoMode()) {
            setProfile(getSamplePlayerProfile(playerId, playerName));
          } else {
            setError(err instanceof Error ? err.message : 'Error fetching profile');
          }
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoadingProfile(false);
      });

    return () => controller.abort();
  }, [enabled, playerId, playerName, activePlaylistId, dateRangeQueryParams.from, dateRangeQueryParams.to]);

  useEffect(() => {
    if (!enabled || !playerId) return;

    const controller = new AbortController();
    setIsLoadingMatches(true);

    const params = new URLSearchParams({
      limit: String(pageSize),
      offset: String((matchPage - 1) * pageSize),
    });
    if (roleFilter !== 'all') params.set('role', roleFilter);
    if (activePlaylistId > 0) params.set('playlist', String(activePlaylistId));
    if (resultFilter === 'W') params.set('result', 'victory');
    if (resultFilter === 'L') params.set('result', 'defeat');
    if (searchQuery.trim()) params.set('search', searchQuery.trim());
    if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
    if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);

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
          setTotalMatches(demoRows.length);
        } else {
          setMatches(data.matches || []);
          setTotalMatches(data.total || 0);
        }
      })
      .catch(() => {
        if (isDemoMode()) {
          const demoRows = getSamplePlayerMatches(playerId, playerName);
          setMatches(demoRows);
          setTotalMatches(demoRows.length);
        } else {
          setMatches([]);
          setTotalMatches(0);
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
    activePlaylistId,
    resultFilter,
    searchQuery,
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
          rank: found?.rank_name ? shortRank(found.rank_name, found.division) : 'UNRANKED',
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

  const scopedMatchups = useMemo(() => {
    const list = profile?.matchups ?? [];
    if (activePlaylistId <= 0) return list;
    return list.filter((m) => m.playlist_id === activePlaylistId);
  }, [profile?.matchups, activePlaylistId]);

  const totals = useMemo(() => sumMatchups(scopedMatchups), [scopedMatchups]);

  const withStats = useMemo(
    () =>
      profile?.with_you_stats ??
      emptyRoleStats(totals.mate.wins + totals.mate.losses, totals.mate.wins, totals.mate.losses),
    [profile?.with_you_stats, totals.mate.wins, totals.mate.losses],
  );

  const vsStats = useMemo(
    () =>
      profile?.against_you_stats ??
      emptyRoleStats(totals.vs.wins + totals.vs.losses, totals.vs.wins, totals.vs.losses),
    [profile?.against_you_stats, totals.vs.wins, totals.vs.losses],
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
      ),
    [totals, isOwnProfile],
  );

  const threatBadges = useMemo(() => {
    const allLobby = [
      ...(SAMPLE_CURRENT_MATCH.local_player ? [SAMPLE_CURRENT_MATCH.local_player] : []),
      ...(SAMPLE_CURRENT_MATCH.teammates || []),
      ...(SAMPLE_CURRENT_MATCH.opponents || []),
    ];
    const target = allLobby.find(
      (lp) => lp.player_id === id || lp.name.toLowerCase() === name.toLowerCase(),
    );
    if (!target) return [];
    return computeLiveThreatBadges(
      target,
      SAMPLE_CURRENT_MATCH.local_player,
      allLobby,
      SAMPLE_CURRENT_MATCH.playlist_id,
    );
  }, [id, name]);

  const filteredAndSortedMatches = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    const list = matches.filter((m) => {
      if (activePlaylistId > 0 && m.playlist !== activePlaylistId) return false;
      if (roleFilter !== 'all' && m.role !== roleFilter) return false;
      const localOutcome = resolveLocalOutcome(m);
      if (resultFilter === 'W' && localOutcome && localOutcome !== 'victory') return false;
      if (resultFilter === 'L' && localOutcome && localOutcome !== 'defeat') return false;
      if (q) {
        const hay = `${m.map_name || ''} ${playlistLabels(m.playlist).name} ${m.match_guid}`.toLowerCase();
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
  }, [matches, activePlaylistId, roleFilter, resultFilter, searchQuery, sortOption]);

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
    if (fromMatches.length >= 2) return fromMatches;
    const rankPoints = allRanks.map((r) => r.mmr).filter((v) => v > 0);
    return rankPoints;
  }, [matches, id, name, allRanks]);

  const saveNotes = async (newNotes: string, newTags: string[]) => {
    const res = await fetch(`/api/players/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ notes: newNotes, tags: newTags }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to update player`);
    setProfile((prev) => (prev ? { ...prev, notes: newNotes, tags: newTags } : null));
  };

  const clearFilters = () => {
    setRoleFilter('all');
    setStatPlaylist('all');
    setCustomPlaylistId(null);
    setResultFilter('all');
    setSearchQuery('');
    setMatchPage(1);
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
    totals,
    withStats,
    vsStats,
    mmrSparkline,
    matches,
    filteredAndSortedMatches,
    totalMatches,
    totalPages: Math.max(1, Math.ceil(totalMatches / pageSize)),
    isLoadingMatches,
    pageSize,
    statPlaylist,
    setStatPlaylist: handleSetStatPlaylist,
    activePlaylistId,
    setPlaylistById,
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
