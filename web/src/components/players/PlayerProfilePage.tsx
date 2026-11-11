import React, { useEffect, useState, useMemo } from 'react';
import {
  PlayerDetailResponse,
  PlayerMatchDetail,
  PlayerMatchesResponse,
  HistoricalMatchRecord,
} from '../../types/api';
import { COLUMN_HEADER, SectionLabel } from '../common/ui';
import {
  OUTCOME_SLAB,
  computeDynamicRelationshipTag,
  formatAgo,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  platformLabel,
  playlistLabels,
  recordInk,
  shortRank,
  synthesizeRoleStats,
  trackerUrl,
} from '../../utils/broadcast';
import { parseRanks, sumMatchups } from './playerData';
import { teamRows } from '../historical/historyData';
import { TeamBoxScore } from '../common/BoxScore';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { PlayerNotesModal } from './PlayerNotesModal';
import { CompactRoleSplitCard, PlayerProfileDialog } from './PlayerProfileDialog';
import { useDashboard } from '../../context/DashboardContext';
import {
  getSamplePlayerMatches,
  getSamplePlayerProfile,
  isDemoMode,
  SAMPLE_HISTORICAL_MATCHES,
} from '../../utils/sampleData';

interface PlayerProfilePageProps {
  playerId: string;
  playerName?: string;
  onBack: () => void;
  localPlayerIds?: Set<string>;
  onOpenFullProfile?: (playerId: string, playerName?: string) => void;
}

const PLAYLIST_GRID = 'minmax(0,1.2fr) minmax(0,1fr) minmax(0,1fr) 70px 120px';
const MATCH_PAGE_SIZE = 20;
const NO_IDS = new Set<string>();

type ProfilePlaylistScope = 'all' | '2v2' | '3v3' | '1v1';
type ProfileRoleFilter = 'all' | 'teammate' | 'opponent';
type ProfileResultFilter = 'all' | 'W' | 'L';
type ProfileSortOption = 'date-desc' | 'date-asc' | 'pts-desc' | 'goals-desc' | 'saves-desc' | 'demos-desc';

const PLAYLIST_SCOPE_IDS: Record<ProfilePlaylistScope, number> = {
  all: 0,
  '2v2': 11,
  '3v3': 13,
  '1v1': 10,
};

const RecordWithPct: React.FC<{ wins: number; losses: number }> = ({ wins, losses }) => (
  <span className={`font-bold text-lg t-font-mono ${recordInk(wins, losses)}`}>
    {formatRecord(wins, losses)} <span className="text-sm text-muted font-semibold">{formatPct(wins, losses)}</span>
  </span>
);

export const PlayerProfilePage: React.FC<PlayerProfilePageProps> = ({
  playerId,
  playerName,
  onBack,
  localPlayerIds = NO_IDS,
  onOpenFullProfile,
}) => {
  const { statMode, dateRangeQueryParams, pinnedPlayerIds, togglePinPlayer } = useDashboard();
  const isAvg = statMode === 'avg';

  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoadingProfile, setIsLoadingProfile] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [showAllRanks, setShowAllRanks] = useState(false);

  // Unified playlist filter & match history filters
  const [statPlaylist, setStatPlaylist] = useState<ProfilePlaylistScope>('all');
  const [roleFilter, setRoleFilter] = useState<ProfileRoleFilter>('all');
  const [resultFilter, setResultFilter] = useState<ProfileResultFilter>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [sortOption, setSortOption] = useState<ProfileSortOption>('date-desc');
  const [matchPage, setMatchPage] = useState(1);

  const [matches, setMatches] = useState<PlayerMatchDetail[]>([]);
  const [, setTotalMatches] = useState(0);
  const [isLoadingMatches, setIsLoadingMatches] = useState(false);

  const [openMatchGuid, setOpenMatchGuid] = useState<string | null>(null);
  const [telemetryMatch, setTelemetryMatch] = useState<HistoricalMatchRecord | null>(null);
  const [quickModalPlayer, setQuickModalPlayer] = useState<{ id: string; name?: string } | null>(null);
  const [isNotesModalOpen, setIsNotesModalOpen] = useState(false);

  const activePlaylistId = PLAYLIST_SCOPE_IDS[statPlaylist];

  // Fetch player profile details
  useEffect(() => {
    setProfile(null);
    setCopied(false);
    setOpenMatchGuid(null);

    if (!playerId) return;

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
  }, [playerId, playerName, activePlaylistId, dateRangeQueryParams.from, dateRangeQueryParams.to]);

  // Fetch match history
  useEffect(() => {
    if (!playerId) return;
    const controller = new AbortController();
    setIsLoadingMatches(true);

    const params = new URLSearchParams({
      limit: String(MATCH_PAGE_SIZE),
      offset: String((matchPage - 1) * MATCH_PAGE_SIZE),
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
          // Synthesize demo matches from SAMPLE_HISTORICAL_MATCHES where this player participated
          const pNameLower = (playerName || '').toLowerCase();
          const oid = playerId.includes('|') ? playerId.split('|')[1].toLowerCase() : playerId.toLowerCase();
          const demoRows: PlayerMatchDetail[] = SAMPLE_HISTORICAL_MATCHES.filter((m) =>
            m.stats?.players?.some(
              (pl) =>
                pl.online_id?.toLowerCase() === oid ||
                pl.name?.toLowerCase() === pNameLower,
            ),
          ).map((m) => {
            const pl = m.stats?.players?.find(
              (x) => x.online_id?.toLowerCase() === oid || x.name?.toLowerCase() === pNameLower,
            );
            const role: 'teammate' | 'opponent' = pl?.team === 0 ? 'teammate' : 'opponent';
            return {
              ...m,
              result: m.result || (m.winner_team === 0 ? 'victory' : 'defeat'),
              role,
              player_stats: {
                match_guid: m.match_guid,
                player_id: playerId,
                team_num: pl?.team ?? 0,
                is_local: false,
                won: m.result === 'victory',
                score: pl?.score ?? 0,
                goals: pl?.goals ?? 0,
                assists: pl?.assists ?? 0,
                saves: pl?.saves ?? 0,
                shots: pl?.shots ?? 0,
                demos: pl?.demos ?? 0,
              },
            };
          });
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
    playerId,
    playerName,
    roleFilter,
    activePlaylistId,
    resultFilter,
    searchQuery,
    matchPage,
    dateRangeQueryParams.from,
    dateRangeQueryParams.to,
  ]);

  const id = profile?.player_id ?? playerId;
  const name = profile?.player_name || playerName || 'PLAYER';
  const platform = profile?.platform || id;
  const tracker = trackerUrl(id, platform, name);
  const isPinned = pinnedPlayerIds.includes(id);

  const allRanks = useMemo(() => parseRanks(profile?.ranks_json), [profile?.ranks_json]);

  // Core 3v3, 2v2, 1v1 ranks (guaranteed 3 cards for Core playlists)
  const coreRankCards = useMemo(() => {
    const coreSpecs = [
      { id: 13, label: 'RANKED 3V3 (STANDARD)' },
      { id: 11, label: 'RANKED 2V2 (DOUBLES)' },
      { id: 10, label: 'RANKED 1V1 (DUEL)' },
    ];
    return coreSpecs.map((spec) => {
      const found = allRanks.find((r) => r.playlist_id === spec.id);
      return {
        playlistId: spec.id,
        label: spec.label,
        rank: found?.rank_name ? shortRank(found.rank_name, found.division) : 'UNRANKED',
        mmr: found?.mmr ? formatMmr(found.mmr) : '—',
        games: found?.matches_played ?? 0,
      };
    });
  }, [allRanks]);

  const extraRanks = useMemo(
    () => allRanks.filter((r) => ![10, 11, 13].includes(r.playlist_id)),
    [allRanks],
  );

  // Scope matchups to statPlaylist
  const scopedMatchups = useMemo(() => {
    const list = profile?.matchups ?? [];
    if (activePlaylistId === 0) return list;
    return list.filter((m) => m.playlist_id === activePlaylistId);
  }, [profile?.matchups, activePlaylistId]);

  const totals = useMemo(() => sumMatchups(scopedMatchups), [scopedMatchups]);

  const withStats = useMemo(
    () =>
      profile?.with_you_stats ??
      synthesizeRoleStats(totals.mate.wins, totals.mate.losses, `${id}-${statPlaylist}-with`),
    [profile?.with_you_stats, totals.mate.wins, totals.mate.losses, id, statPlaylist],
  );
  const vsStats = useMemo(
    () =>
      profile?.against_you_stats ??
      synthesizeRoleStats(totals.vs.wins, totals.vs.losses, `${id}-${statPlaylist}-vs`),
    [profile?.against_you_stats, totals.vs.wins, totals.vs.losses, id, statPlaylist],
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

  const resolveLocalOutcome = (m: PlayerMatchDetail): 'victory' | 'defeat' | 'draw' | '' => {
    if (m.local_result === 'victory' || m.local_result === 'defeat' || m.local_result === 'draw') {
      return m.local_result;
    }
    if (m.result === 'victory') return m.role === 'opponent' ? 'defeat' : 'victory';
    if (m.result === 'defeat') return m.role === 'opponent' ? 'victory' : 'defeat';
    if (m.result === 'draw') return 'draw';
    if (m.player_stats?.won !== undefined && m.player_stats?.won !== null) {
      if (m.role === 'opponent') {
        return m.player_stats.won ? 'defeat' : 'victory';
      }
      return m.player_stats.won ? 'victory' : 'defeat';
    }
    return '';
  };

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

  const copyId = () => {
    try {
      navigator.clipboard?.writeText(id);
    } catch {}
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleSaveNotes = async (newNotes: string, newTags: string[]) => {
    const res = await fetch(`/api/players/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ notes: newNotes, tags: newTags }),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to update player`);
    setProfile((prev) => (prev ? { ...prev, notes: newNotes, tags: newTags } : null));
  };

  return (
    <div data-testid="player-profile-page" className="font-display flex flex-col gap-5 pt-4">
      {/* Top Bar: Back Button, Player Title, Relationship Badge, Pin Toggle, Unified Playlist Filter */}
      <div className="flex flex-wrap justify-between items-center gap-4 pb-4 border-b t-border border-line">
        <div className="flex flex-wrap items-center gap-4">
          <button
            type="button"
            data-testid="back-to-players-btn"
            onClick={onBack}
            className="px-3.5 py-2 t-bg-panel bg-panel t-border border border-outline font-extrabold text-sm t-text-blue text-blue-ink hover:text-ink-strong uppercase t-radius cursor-pointer"
          >
            ← BACK TO PLAYERS DIRECTORY
          </button>
          <div>
            <div className="flex flex-wrap items-center gap-2.5">
              <span className="text-sm font-extrabold tracking-wider text-muted uppercase">
                PLAYER PROFILE · {platformLabel(platform)} · <span className="t-font-mono normal-case">{id}</span>
              </span>
              <span
                className={`px-2.5 py-0.5 t-bg-page bg-page t-border border border-line text-sm font-extrabold ${relTag.color}`}
              >
                {relTag.tag}
              </span>
              <button
                type="button"
                onClick={copyId}
                className="text-sm font-extrabold text-muted hover:text-ink uppercase cursor-pointer"
              >
                {copied ? 'COPIED ✓' : 'COPY ID'}
              </button>
              {tracker && (
                <a
                  href={tracker}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-sm font-extrabold text-ink hover:text-blue-ink uppercase"
                >
                  RL TRACKER ↗
                </a>
              )}
            </div>
            <h1 className="t-font-display italic font-extrabold text-[44px] leading-none t-text-strong text-ink-strong uppercase mt-1">
              {name}
            </h1>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2.5 text-sm font-extrabold">
          <button
            type="button"
            data-testid="pin-player-btn"
            onClick={() => togglePinPlayer(id)}
            className={`px-3.5 py-2 t-border border border-outline uppercase t-radius cursor-pointer ${
              isPinned ? 't-bg-ink bg-ink text-page' : 't-bg-panel bg-panel t-text-blue text-blue-ink'
            }`}
          >
            {isPinned ? '★ PINNED' : '☆ PIN PLAYER'}
          </button>

          <div className="flex t-border border border-outline t-bg-panel bg-panel t-radius overflow-hidden">
            {(['all', '2v2', '3v3', '1v1'] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => {
                  setStatPlaylist(m);
                  setMatchPage(1);
                }}
                className={`prof-pl-btn px-3 py-1.5 uppercase cursor-pointer ${
                  statPlaylist === m ? 't-bg-ink bg-ink text-page' : 'text-muted hover:text-ink'
                }`}
              >
                {m === 'all' ? 'ALL MODES' : m.toUpperCase()}
              </button>
            ))}
          </div>
        </div>
      </div>

      {error ? (
        <div className="bg-panel p-6 text-loss font-bold text-base uppercase">
          COULD NOT LOAD PROFILE · {error}
        </div>
      ) : !profile && isLoadingProfile ? (
        <div className="bg-panel p-6 text-muted font-bold text-base uppercase">LOADING PROFILE…</div>
      ) : (
        <>
          {/* Compact Non-Wrapping WITH YOU & AGAINST YOU Role Split Cards */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-3.5">
            <CompactRoleSplitCard
              label={isOwnProfile ? 'YOUR STATS' : 'WITH YOU'}
              roleTitle={isOwnProfile ? 'ALL MATCHES (YOU)' : 'WITH YOU (TEAMMATE)'}
              wins={totals.mate.wins}
              losses={totals.mate.losses}
              teammate
              stats={withStats}
              isAvg={isAvg}
              emptyMessage="NO MATCHES PLAYED AS TEAMMATE IN SELECTED RANGE"
              selected={roleFilter === 'teammate'}
              onClick={() => {
                setRoleFilter((cur) => (cur === 'teammate' ? 'all' : 'teammate'));
                setMatchPage(1);
              }}
            />
            <CompactRoleSplitCard
              label="VS YOU"
              roleTitle="AGAINST YOU (OPPONENT)"
              wins={totals.vs.wins}
              losses={totals.vs.losses}
              teammate={false}
              stats={vsStats}
              isAvg={isAvg}
              emptyMessage="NO MATCHES PLAYED AS OPPONENT IN SELECTED RANGE"
              selected={roleFilter === 'opponent'}
              onClick={() => {
                setRoleFilter((cur) => (cur === 'opponent' ? 'all' : 'opponent'));
                setMatchPage(1);
              }}
            />
          </div>

          {/* Known Playlist Ranks (Core 3v3 · 2v2 · 1v1 + Extra Modes Toggle) */}
          <div className="t-bg-panel bg-panel t-border border border-line p-4">
            <div className="flex flex-wrap justify-between items-center gap-2 mb-3">
              <span className="text-sm font-extrabold tracking-wider text-muted uppercase">
                KNOWN PLAYLIST RANKS (CORE 3V3 · 2V2 · 1V1)
              </span>
              <button
                type="button"
                data-testid="toggle-extra-ranks-btn"
                onClick={() => setShowAllRanks((v) => !v)}
                className="text-sm font-extrabold t-text-blue text-blue-ink hover:underline uppercase cursor-pointer"
              >
                {showAllRanks ? 'HIDE EXTRA MODES ▲' : 'VIEW FULL PLAYLIST STATS & EXTRA MODES →'}
              </button>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 t-num">
              {coreRankCards.map((c) => (
                <div
                  key={c.playlistId}
                  className="t-bg-page bg-page t-border border border-line px-4 py-3 flex justify-between items-center"
                >
                  <div>
                    <div className="text-sm font-extrabold text-muted">{c.label}</div>
                    <div className="t-font-display font-extrabold text-[22px] t-text-strong text-ink-strong uppercase">
                      {c.rank}
                    </div>
                  </div>
                  <div className="text-right">
                    <div className="t-font-mono font-extrabold text-[20px] t-text-blue text-blue-ink">
                      {c.mmr} MMR
                    </div>
                    <div className="text-sm font-extrabold text-muted t-font-mono">{c.games} GAMES</div>
                  </div>
                </div>
              ))}
            </div>

            {showAllRanks && (
              <div className="mt-3 pt-3 border-t t-border border-line flex flex-col gap-3">
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5 text-sm font-extrabold t-font-mono">
                  {(extraRanks.length
                    ? extraRanks.map((er) => ({
                        playlist: playlistLabels(er.playlist_id, er.playlist_name).name,
                        rank: er.rank_name ? shortRank(er.rank_name, er.division) : 'UNRANKED',
                        mmr: formatMmr(er.mmr),
                        games: er.matches_played ?? 0,
                      }))
                    : [
                        { playlist: 'HOOPS (2V2)', rank: 'UNRANKED', mmr: '—', games: 0 },
                        { playlist: 'RUMBLE (3V3)', rank: 'UNRANKED', mmr: '—', games: 0 },
                        { playlist: 'DROPSHOT (3V3)', rank: 'UNRANKED', mmr: '—', games: 0 },
                      ]
                  ).map((er, i) => (
                    <div
                      key={i}
                      className="t-bg-page bg-page t-border border border-line px-3.5 py-2.5 flex justify-between"
                    >
                      <span className="font-sans">
                        {er.playlist}: <strong>{er.rank}</strong>
                      </span>
                      <span className="t-text-blue text-blue-ink">
                        {er.mmr} MMR ({er.games}G)
                      </span>
                    </div>
                  ))}
                </div>

                {profile?.matchups && profile.matchups.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    <div className={`grid px-[18px] ${COLUMN_HEADER}`} style={{ gridTemplateColumns: PLAYLIST_GRID }}>
                      <span>PLAYLIST</span>
                      <span>WITH YOU</span>
                      <span>VS YOU</span>
                      <span className="text-right">GAMES</span>
                      <span className="text-right">LAST MET</span>
                    </div>
                    {profile.matchups.map((m) => (
                      <div
                        key={m.playlist_id}
                        data-testid="profile-playlist-row"
                        className="grid items-center h-[48px] t-bg-page bg-page px-[18px] t-border border border-line"
                        style={{ gridTemplateColumns: PLAYLIST_GRID }}
                      >
                        <span className="font-bold text-base tracking-t6 truncate">
                          {playlistLabels(m.playlist_id).name}
                        </span>
                        <RecordWithPct wins={m.wins_as_teammate} losses={m.losses_as_teammate} />
                        <RecordWithPct wins={m.wins_as_opponent} losses={m.losses_as_opponent} />
                        <span className="text-right font-bold text-lg t-font-mono">{m.total_matches}</span>
                        <span className="text-right font-semibold text-sm tracking-t8 text-muted">
                          {formatAgo(m.last_played_at)}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Scouting Notes & Tags */}
          <div className="t-bg-panel bg-panel t-border border border-line p-4 flex flex-col gap-2">
            <div className="flex justify-between items-center">
              <SectionLabel>SCOUTING NOTES & TAGS</SectionLabel>
              <button
                type="button"
                data-testid="edit-notes-btn"
                onClick={() => setIsNotesModalOpen(true)}
                className="text-sm font-bold text-ink-2 hover:text-ink tracking-wider uppercase transition-colors cursor-pointer"
              >
                EDIT NOTES & TAGS ✎
              </button>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              {profile?.tags && profile.tags.length > 0 ? (
                profile.tags.map((t) => (
                  <span
                    key={t}
                    className="t-bg-page bg-page border border-line-2 px-2.5 py-1 text-sm font-bold text-ink tracking-wider uppercase rounded"
                  >
                    {t}
                  </span>
                ))
              ) : (
                <span className="text-sm text-muted italic">No custom tags assigned</span>
              )}
              {profile?.notes && (
                <span className="text-sm text-ink-2 font-sans border-l-2 border-blue-ink pl-3">
                  {profile.notes}
                </span>
              )}
            </div>
          </div>

          {/* Full Match History with Search, Role filter, Result filter, Sort by all fields, and Expandable Box Scores */}
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-2.5">
              <span className="text-sm font-extrabold tracking-wider text-muted uppercase">
                FULL MATCH HISTORY WITH/AGAINST {name} ({filteredAndSortedMatches.length}{' '}
                {filteredAndSortedMatches.length === 1 ? 'MATCH' : 'MATCHES'})
              </span>

              <div className="flex flex-wrap items-center gap-2.5 text-sm font-extrabold">
                <input
                  type="search"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="SEARCH ARENA OR GUID..."
                  className="h-[36px] t-bg-panel bg-panel t-border border border-outline px-3 text-sm font-bold t-text-strong text-ink-strong uppercase outline-none t-radius"
                />

                <div className="flex t-border border border-outline t-bg-panel bg-panel t-radius overflow-hidden">
                  {(
                    [
                      ['all', 'ALL ROLES'],
                      ['teammate', 'WITH YOU'],
                      ['opponent', 'AGAINST'],
                    ] as const
                  ).map(([k, l]) => (
                    <button
                      key={k}
                      type="button"
                      onClick={() => {
                        setRoleFilter(k);
                        setMatchPage(1);
                      }}
                      className={`px-2.5 py-1.5 uppercase cursor-pointer ${
                        roleFilter === k ? 't-bg-ink bg-ink text-page' : 'text-muted hover:text-ink'
                      }`}
                    >
                      {l}
                    </button>
                  ))}
                </div>

                <div className="flex t-border border border-outline t-bg-panel bg-panel t-radius overflow-hidden">
                  {(
                    [
                      ['all', 'ALL'],
                      ['W', 'WINS'],
                      ['L', 'LOSSES'],
                    ] as const
                  ).map(([r, lbl]) => (
                    <button
                      key={r}
                      type="button"
                      onClick={() => {
                        setResultFilter(r);
                        setMatchPage(1);
                      }}
                      className={`px-2.5 py-1.5 uppercase cursor-pointer ${
                        resultFilter === r ? 't-bg-ink bg-ink text-page' : 'text-muted hover:text-ink'
                      }`}
                    >
                      {lbl}
                    </button>
                  ))}
                </div>

                <select
                  aria-label="Sort player matches"
                  value={sortOption}
                  onChange={(e) => setSortOption(e.target.value as ProfileSortOption)}
                  className="h-[36px] t-bg-panel bg-panel t-border border border-outline px-2.5 t-text-strong text-ink-strong font-extrabold uppercase outline-none t-radius"
                >
                  <option value="date-desc">SORT: NEWEST FIRST</option>
                  <option value="date-asc">SORT: OLDEST FIRST</option>
                  <option value="pts-desc">SORT: SCORE</option>
                  <option value="goals-desc">SORT: GOALS</option>
                  <option value="saves-desc">SORT: SAVES</option>
                  <option value="demos-desc">SORT: DEMOS</option>
                </select>
              </div>
            </div>

            <div className="flex flex-col gap-2">
              {isLoadingMatches && matches.length === 0 ? (
                <div className="bg-panel p-6 text-center text-muted font-bold text-sm">LOADING MATCHES…</div>
              ) : filteredAndSortedMatches.length === 0 ? (
                <div className="bg-panel p-6 text-center text-muted font-bold text-sm">
                  NO RECORDED MATCHES FOUND IN SELECTED RANGE
                </div>
              ) : (
                filteredAndSortedMatches.map((m) => {
                  const isExpanded = openMatchGuid === m.match_guid;
                  const localOutcome = resolveLocalOutcome(m);
                  const outcome =
                    localOutcome === 'victory'
                      ? 'W'
                      : localOutcome === 'defeat'
                      ? 'L'
                      : null;

                  return (
                    <div key={m.match_guid} className="t-bg-panel bg-panel t-border border border-line flex flex-col">
                      <div
                        role="button"
                        tabIndex={0}
                        data-testid="profile-match-row"
                        onClick={() => setOpenMatchGuid((cur) => (cur === m.match_guid ? null : m.match_guid))}
                        className="px-4 py-3 flex items-center justify-between cursor-pointer hover:bg-panel-hover transition-colors"
                      >
                        <div className="flex items-center gap-3 min-w-0">
                          {outcome && (
                            <span
                              className={`font-extrabold text-sm px-2.5 py-0.5 text-page rounded ${OUTCOME_SLAB[outcome]}`}
                            >
                              {outcome === 'W' ? 'WIN' : 'LOSS'}
                            </span>
                          )}
                          <span className="font-extrabold text-xl t-font-mono">
                            <span className="text-blue-ink">{m.blue_score}</span>
                            <span className="text-muted"> – </span>
                            <span className="text-orange-ink">{m.orange_score}</span>
                          </span>
                          <span className="font-bold text-base truncate uppercase">
                            {playlistLabels(m.playlist).name} · {m.map_name || 'Arena'}
                          </span>
                        </div>

                        <div className="flex items-center gap-3">
                          <span
                            className={`text-sm font-bold px-2.5 py-0.5 rounded ${
                              m.role === 'teammate'
                                ? 'bg-win/15 text-win'
                                : 'bg-orange-slab/20 text-orange-ink'
                            }`}
                          >
                            {m.role === 'teammate' ? 'WITH YOU' : 'VS YOU'}
                          </span>
                          <span className="text-muted text-sm">{formatAgo(m.stats?.date || m.created_at)}</span>
                          <span className="text-muted text-sm">{isExpanded ? '▲' : '▼'}</span>
                        </div>
                      </div>

                      {isExpanded && (
                        <div className="p-4 border-t t-border border-line t-bg-page bg-page flex flex-col gap-3">
                          <div className="grid grid-cols-1 xl:grid-cols-2 gap-[2px]">
                            {[0, 1].map((team) => (
                              <TeamBoxScore
                                key={team}
                                team={team as 0 | 1}
                                label={team === 0 ? 'BLUE' : 'ORANGE'}
                                rows={teamRows(m.stats?.players || [], team, localPlayerIds, id)}
                                withRanks
                                onPlayerClick={(pId, pName) => setQuickModalPlayer({ id: pId, name: pName })}
                              />
                            ))}
                          </div>
                          <div className="flex flex-wrap justify-between items-center gap-2">
                            <span className="text-sm font-extrabold text-muted">
                              CLICK ANY PLAYER IN BOX SCORE FOR QUICK MODAL PROFILE (MMR SHOWN BEFORE RANK/DIV)
                            </span>
                            <button
                              type="button"
                              onClick={() => setTelemetryMatch(m)}
                              className="px-4 py-2 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-sm uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
                            >
                              FULL 5-TAB BALLCHASING TELEMETRY →
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })
              )}
            </div>
          </div>

          {/* Profile Timestamps Footer */}
          {profile && (
            <div className="flex justify-between gap-4 px-4 py-3 border-t border-line font-semibold text-sm tracking-t12 text-muted-2">
              <span>FIRST SEEN {formatShortDate(profile.first_seen_at)}</span>
              <span>LAST SEEN {formatAgo(profile.last_seen_at)}</span>
            </div>
          )}
        </>
      )}

      {telemetryMatch && (
        <HistoricalMatchDetailModal
          match={telemetryMatch}
          onClose={() => setTelemetryMatch(null)}
          localPlayerIds={localPlayerIds}
          onPlayerClick={(pId, pName) => setQuickModalPlayer({ id: pId, name: pName })}
          inspectedPlayerId={id}
        />
      )}

      {quickModalPlayer && (
        <PlayerProfileDialog
          playerId={quickModalPlayer.id}
          playerName={quickModalPlayer.name}
          onClose={() => setQuickModalPlayer(null)}
          localPlayerIds={localPlayerIds}
          onOpenFullProfile={onOpenFullProfile}
        />
      )}

      {profile && (
        <PlayerNotesModal
          isOpen={isNotesModalOpen}
          initialNotes={profile.notes || ''}
          initialTags={profile.tags || []}
          onClose={() => setIsNotesModalOpen(false)}
          onSave={handleSaveNotes}
        />
      )}
    </div>
  );
};
