import React, { useEffect, useState, useMemo } from 'react';
import {
  PlayerDetailResponse,
  PlayerMatchDetail,
  PlayerMatchesResponse,
  HistoricalMatchRecord,
  PlayerRoleStatsSummary,
} from '../../types/api';
import { Modal } from '../common/Modal';
import { COLUMN_HEADER, PANEL_LABEL, RecordCells, SectionLabel } from '../common/ui';
import {
  OUTCOME_SLAB,
  computeDynamicRelationshipTag,
  computeLiveThreatBadges,
  formatAgo,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  platformLabel,
  playlistLabels,
  recordFill,
  recordInk,
  shortRank,
  synthesizeRoleStats,
  trackerUrl,
} from '../../utils/broadcast';
import { h2hTag, parseRanks, sumMatchups } from './playerData';
import { teamRows } from '../historical/historyData';
import { TeamBoxScore } from '../common/BoxScore';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { PlayerNotesModal } from './PlayerNotesModal';
import { useDashboard } from '../../context/DashboardContext';
import {
  getSamplePlayerMatches,
  getSamplePlayerProfile,
  isDemoMode,
  SAMPLE_CURRENT_MATCH,
} from '../../utils/sampleData';

interface PlayerProfileDialogProps {
  playerId: string | null;
  /** Name from the directory row, shown while the profile loads. */
  playerName?: string;
  onClose: () => void;
  localPlayerIds?: Set<string>;
  onOpenFullProfile?: (playerId: string, playerName?: string) => void;
}

const PLAYLIST_GRID = 'minmax(0,1.2fr) minmax(0,1fr) minmax(0,1fr) 70px 120px';
const MATCH_PAGE_SIZE = 10;
const NO_IDS = new Set<string>();

export const CompactRoleSplitCard: React.FC<{
  label: string;
  roleTitle: string;
  wins: number;
  losses: number;
  teammate: boolean;
  stats: PlayerRoleStatsSummary;
  isAvg: boolean;
  emptyMessage: string;
  selected?: boolean;
  onClick?: () => void;
}> = ({ label, roleTitle, wins, losses, teammate, stats, isAvg, emptyMessage, selected, onClick }) => {
  const games = wins + losses;
  const tag = h2hTag(wins, losses, teammate);
  const gameWord = games === 1 ? 'GAME' : 'GAMES';
  const wr = games > 0 ? Math.round((wins / games) * 100) : 0;

  const pts = isAvg ? Math.round(stats.avg_score) : (stats.score ?? stats.total_score ?? 0);
  const g = isAvg ? Number(stats.avg_goals.toFixed(1)) : (stats.goals ?? stats.total_goals ?? 0);
  const a = isAvg ? Number(stats.avg_assists.toFixed(1)) : (stats.assists ?? stats.total_assists ?? 0);
  const sv = isAvg ? Number(stats.avg_saves.toFixed(1)) : (stats.saves ?? stats.total_saves ?? 0);
  const shPct = Math.round(stats.shot_pct ?? stats.shooting_pct ?? 0);
  const dem = isAvg ? Number((stats.avg_demos ?? stats.avg_demos_inflicted ?? 0).toFixed(1)) : (stats.demos ?? stats.total_demos_inflicted ?? 0);
  const bpm = Math.round(stats.avg_bpm || 0);
  const bst = Math.round(stats.avg_boost || 0);
  const sup = Math.round(stats.avg_supersonic_pct || 0);

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      data-testid={teammate ? 'h2h-with' : 'h2h-vs'}
      className={`t-bg-panel bg-panel t-border border border-line p-4 flex flex-col gap-3 min-w-0 cursor-pointer transition-all duration-150 t-num ${
        selected ? 'ring-2 ring-blue-ink bg-panel-hover' : 'hover:bg-panel-hover'
      }`}
    >
      <div className="flex items-center justify-between gap-2 border-b t-border border-line pb-2.5">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={`px-2.5 py-1 text-sm font-extrabold uppercase shrink-0 ${
              teammate ? 't-bg-blue-slab bg-blue-slab text-white' : 't-bg-orange-slab bg-orange-slab t-text-on-slab text-page'
            }`}
          >
            {roleTitle}
          </span>
          <SectionLabel className="truncate">
            {label} · {games} {gameWord} IN RANGE
          </SectionLabel>
          {tag && (
            <span className={`font-bold text-sm tracking-t14 text-page px-2 py-0.5 whitespace-nowrap ${recordFill(wins, losses)}`}>
              {tag}
            </span>
          )}
        </div>
        <div className="flex items-baseline gap-2 whitespace-nowrap shrink-0">
          <span className={`font-extrabold text-[28px] leading-none ${recordInk(wins, losses)}`}>
            {formatRecord(wins, losses)}
          </span>
          <span className="t-font-mono font-extrabold text-sm text-muted">({wr}% · {formatPct(wins, losses)})</span>
        </div>
      </div>

      <RecordCells wins={wins} losses={losses} count={10} cellClass="flex-1 h-[10px]" />

      {games === 0 ? (
        <div className="t-bg-page bg-page t-border border border-line py-4 px-4 text-center text-sm font-extrabold text-muted uppercase">
          {emptyMessage}
        </div>
      ) : (
        <div className="t-bg-page bg-page t-border border border-line">
          <div className="grid grid-cols-9 items-center py-1.5 px-2 text-right text-sm font-extrabold text-muted border-b t-border border-line uppercase">
            <span>PTS</span>
            <span>G</span>
            <span>A</span>
            <span>SV</span>
            <span>SH%</span>
            <span>DEM</span>
            <span>BPM</span>
            <span>BST</span>
            <span>SUP%</span>
          </div>
          <div className="grid grid-cols-9 items-center py-2 px-2 text-right text-base font-extrabold t-font-mono">
            <span className="t-text-strong text-ink-strong">{pts.toLocaleString()}</span>
            <span className="t-text-strong text-ink-strong">{g}</span>
            <span className="t-text-strong text-ink-strong">{a}</span>
            <span className="t-text-strong text-ink-strong">{sv}</span>
            <span className="t-text-win text-win">{shPct}%</span>
            <span className="t-text-orange text-orange-ink">{dem}</span>
            <span className="t-text-blue text-blue-ink">{bpm}</span>
            <span className="t-text-ink2 text-ink-2">{bst}%</span>
            <span className="t-text-ink2 text-ink-2">{sup}%</span>
          </div>
        </div>
      )}
    </div>
  );
};

const RecordWithPct: React.FC<{ wins: number; losses: number }> = ({ wins, losses }) => (
  <span className={`font-bold text-lg t-font-mono ${recordInk(wins, losses)}`}>
    {formatRecord(wins, losses)} <span className="text-sm text-muted font-semibold">{formatPct(wins, losses)}</span>
  </span>
);

export const PlayerProfileDialog: React.FC<PlayerProfileDialogProps> = ({
  playerId,
  playerName,
  onClose,
  localPlayerIds = NO_IDS,
  onOpenFullProfile,
}) => {
  const { statMode, dateRangeQueryParams } = useDashboard();
  const isAvg = statMode === 'avg';

  // Cross-player navigation history stack
  const [historyStack, setHistoryStack] = useState<Array<{ id: string; name?: string }>>([]);

  const activeEntry = historyStack[historyStack.length - 1];
  const activeId = activeEntry?.id ?? playerId;
  const activeName = activeEntry?.name ?? playerName;

  useEffect(() => {
    if (playerId) {
      const initialStack = [{ id: playerId, name: playerName }];
      setHistoryStack(initialStack);
    } else {
      setHistoryStack([]);
    }
  }, [playerId, playerName]);

  useEffect(() => {
    const handlePopState = (event: PopStateEvent) => {
      const dialogStack = (event.state as { rlsyncDialogStack?: Array<{ id: string; name?: string }> } | null)
        ?.rlsyncDialogStack;
      if (Array.isArray(dialogStack) && dialogStack.length > 0) {
        setHistoryStack(dialogStack);
      } else if (playerId) {
        setHistoryStack([{ id: playerId, name: playerName }]);
      }
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, [playerId, playerName]);

  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoadingProfile, setIsLoadingProfile] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [showAllRanks, setShowAllRanks] = useState(false);

  // Filters for match history
  const [roleFilter, setRoleFilter] = useState<'all' | 'teammate' | 'opponent'>('all');
  const [playlistFilter, setPlaylistFilter] = useState<number>(0);
  const [matchPage, setMatchPage] = useState<number>(1);
  const [matches, setMatches] = useState<PlayerMatchDetail[]>([]);
  const [totalMatches, setTotalMatches] = useState<number>(0);
  const [isLoadingMatches, setIsLoadingMatches] = useState(false);

  // Level 1 accordion and Level 2 telemetry modal state
  const [openMatchGuid, setOpenMatchGuid] = useState<string | null>(null);
  const [telemetryMatch, setTelemetryMatch] = useState<HistoricalMatchRecord | null>(null);

  // Notes & tags modal state
  const [isNotesModalOpen, setIsNotesModalOpen] = useState(false);

  // Fetch player profile details
  useEffect(() => {
    setProfile(null);
    setCopied(false);
    setRoleFilter('all');
    setPlaylistFilter(0);
    setMatchPage(1);
    setOpenMatchGuid(null);

    if (!activeId) return;

    const controller = new AbortController();
    setIsLoadingProfile(true);
    setError(null);
    const params = new URLSearchParams();
    if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
    if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);
    const qs = params.toString();
    const url = `/api/players/${encodeURIComponent(activeId)}${qs ? `?${qs}` : ''}`;

    fetch(url, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to load player`);
        return res.json();
      })
      .then((data: PlayerDetailResponse) => setProfile(data))
      .catch((err) => {
        if (!controller.signal.aborted) {
          if (isDemoMode()) {
            setProfile(getSamplePlayerProfile(activeId, activeName));
          } else {
            setError(err instanceof Error ? err.message : 'Error fetching profile');
          }
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoadingProfile(false);
      });
    return () => controller.abort();
  }, [activeId, activeName, dateRangeQueryParams.from, dateRangeQueryParams.to]);

  // Fetch paginated match history
  useEffect(() => {
    if (!activeId) return;
    const controller = new AbortController();
    setIsLoadingMatches(true);

    const params = new URLSearchParams({
      limit: String(MATCH_PAGE_SIZE),
      offset: String((matchPage - 1) * MATCH_PAGE_SIZE),
    });
    if (roleFilter !== 'all') params.set('role', roleFilter);
    if (playlistFilter > 0) params.set('playlist', String(playlistFilter));
    if (dateRangeQueryParams.from) params.set('from', dateRangeQueryParams.from);
    if (dateRangeQueryParams.to) params.set('to', dateRangeQueryParams.to);

    fetch(`/api/players/${encodeURIComponent(activeId)}/matches?${params.toString()}`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data: PlayerMatchesResponse) => {
        if ((!data.matches || data.matches.length === 0) && isDemoMode()) {
          const demoRows = getSamplePlayerMatches(activeId, activeName);
          setMatches(demoRows);
          setTotalMatches(demoRows.length);
        } else {
          setMatches(data.matches || []);
          setTotalMatches(data.total || 0);
        }
      })
      .catch(() => {
        if (isDemoMode()) {
          const demoRows = getSamplePlayerMatches(activeId, activeName);
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
  }, [activeId, activeName, roleFilter, playlistFilter, matchPage, dateRangeQueryParams.from, dateRangeQueryParams.to]);

  const id = profile?.player_id ?? activeId ?? '';
  const name = profile?.player_name || activeName || 'PLAYER';
  const platform = profile?.platform || id;
  const tracker = id ? trackerUrl(id, platform, name) : null;
  const ranks = useMemo(() => parseRanks(profile?.ranks_json), [profile?.ranks_json]);
  const scopedMatchups = useMemo(() => {
    const list = profile?.matchups ?? [];
    if (playlistFilter <= 0) return list;
    return list.filter((m) => m.playlist_id === playlistFilter);
  }, [profile?.matchups, playlistFilter]);
  const totals = useMemo(() => sumMatchups(scopedMatchups), [scopedMatchups]);

  const withStats = useMemo(
    () =>
      playlistFilter <= 0 && profile?.with_you_stats
        ? profile.with_you_stats
        : synthesizeRoleStats(totals.mate.wins, totals.mate.losses, `${id}-${playlistFilter}`),
    [playlistFilter, profile?.with_you_stats, totals.mate.wins, totals.mate.losses, id],
  );
  const vsStats = useMemo(
    () =>
      playlistFilter <= 0 && profile?.against_you_stats
        ? profile.against_you_stats
        : synthesizeRoleStats(totals.vs.wins, totals.vs.losses, `${id}-${playlistFilter}-vs`),
    [playlistFilter, profile?.against_you_stats, totals.vs.wins, totals.vs.losses, id],
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

  if (!activeId) return null;

  const copyId = () => {
    try {
      navigator.clipboard?.writeText(id);
    } catch {}
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handlePlayerClick = (nextPlayerId: string, nextPlayerName?: string) => {
    if (!nextPlayerId || nextPlayerId === id) return;
    setHistoryStack((prev) => {
      const nextStack = [...prev, { id: nextPlayerId, name: nextPlayerName }];
      try {
        const curState =
          window.history.state && typeof window.history.state === 'object' ? window.history.state : {};
        window.history.pushState(
          { ...curState, rlsyncDialogStack: nextStack },
          '',
          window.location.pathname + window.location.search,
        );
      } catch {}
      return nextStack;
    });
  };

  const handleBack = () => {
    setHistoryStack((prev) => prev.slice(0, prev.length - 1));
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

  const totalPages = Math.max(1, Math.ceil(totalMatches / MATCH_PAGE_SIZE));

  return (
    <>
      <Modal
        isOpen
        onClose={onClose}
        kicker={
          historyStack.length > 1 ? (
            <div className="flex flex-wrap items-center gap-3">
              <button
                type="button"
                onClick={handleBack}
                className="hover:underline flex items-center gap-1.5 text-blue-ink font-bold uppercase cursor-pointer"
              >
                <span>← BACK TO {historyStack[historyStack.length - 2]?.name || 'PREVIOUS PLAYER'}</span>
              </button>
              <span className="text-muted text-sm">
                ({historyStack.map((h) => h.name || h.id).join(' → ')})
              </span>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <span>PLAYER PROFILE · {platformLabel(platform)}</span>
              <span className={`px-2 py-0.5 t-bg-page bg-page t-border border border-line text-sm font-extrabold ${relTag.color}`}>
                {relTag.tag}
              </span>
              {threatBadges.map((b, idx) => (
                <span key={idx} className={`px-2 py-0.5 text-sm font-extrabold uppercase tracking-wide ${b.classes}`}>
                  {b.label}
                </span>
              ))}
            </div>
          )
        }
        title={name}
        headerActions={
          onOpenFullProfile && (
            <button
              type="button"
              onClick={() => {
                onClose();
                onOpenFullProfile(id, name);
              }}
              className="px-3.5 py-2 t-bg-blue-slab bg-blue-slab text-white text-sm font-extrabold uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
            >
              OPEN FULL-PAGE PROFILE →
            </button>
          )
        }
        meta={
          <>
            <span className="text-ink-2 normal-case break-all t-font-mono">{id}</span>
            <button type="button" onClick={copyId} className="text-muted hover:text-ink transition-colors duration-120">
              {copied ? 'COPIED ✓' : 'COPY ID'}
            </button>
            {tracker && (
              <a
                href={tracker}
                target="_blank"
                rel="noopener noreferrer"
                className="text-ink hover:text-blue-ink transition-colors duration-120"
              >
                RL TRACKER ↗
              </a>
            )}
          </>
        }
      >
        {error ? (
          <p className="px-8 py-6 font-semibold text-base tracking-t8 text-loss uppercase">
            COULD NOT LOAD PROFILE · {error}
          </p>
        ) : !profile && isLoadingProfile ? (
          <p className="px-8 py-6 font-semibold text-base tracking-t8 text-muted">LOADING PROFILE…</p>
        ) : !profile ? null : (
          <div className="flex flex-col font-display pb-4">
            {/* Compact Non-Wrapping WITH YOU & AGAINST YOU Role Split Cards */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-3.5 px-8 pt-5">
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

            {/* Active Filter Summary Bar */}
            {(roleFilter !== 'all' || playlistFilter > 0) && (
              <div
                data-testid="active-filter-summary"
                className="mx-8 mt-4 bg-panel border-l-4 border-blue-ink px-6 py-2.5 flex items-center justify-between text-sm font-bold"
              >
                <div className="flex items-center gap-2">
                  <span className="text-muted">ACTIVE FILTER:</span>
                  <span className="text-ink uppercase">
                    {roleFilter === 'teammate'
                      ? 'WITH YOU (TEAMMATE)'
                      : roleFilter === 'opponent'
                      ? 'VS YOU (OPPONENT)'
                      : ''}
                    {roleFilter !== 'all' && playlistFilter > 0 ? ' · ' : ''}
                    {playlistFilter > 0 ? playlistLabels(playlistFilter).name : ''}
                  </span>
                  <span className="text-muted">
                    ({totalMatches} {totalMatches === 1 ? 'match' : 'matches'})
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    setRoleFilter('all');
                    setPlaylistFilter(0);
                    setMatchPage(1);
                  }}
                  className="text-muted hover:text-loss text-sm uppercase underline cursor-pointer"
                >
                  CLEAR FILTERS
                </button>
              </div>
            )}

            {/* Scouting Notes & Tags Section */}
            <div className="px-8 pt-5 pb-2 flex flex-col gap-3">
              <div className="flex justify-between items-center">
                <SectionLabel>SCOUTING NOTES & TAGS</SectionLabel>
                <button
                  type="button"
                  data-testid="edit-notes-btn"
                  onClick={() => setIsNotesModalOpen(true)}
                  className="text-sm font-bold text-ink-2 hover:text-ink tracking-wider uppercase transition-colors"
                >
                  EDIT NOTES & TAGS ✎
                </button>
              </div>

              {profile.tags && profile.tags.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                  {profile.tags.map((t) => (
                    <span
                      key={t}
                      className="bg-panel border border-line-2 px-2.5 py-1 text-sm font-bold text-ink tracking-wider uppercase rounded"
                    >
                      {t}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted italic">No custom tags assigned</p>
              )}

              {profile.notes ? (
                <div className="bg-panel p-3 border-l-2 border-blue-ink text-sm text-ink-2 whitespace-pre-wrap font-sans">
                  {profile.notes}
                </div>
              ) : (
                <p className="text-sm text-muted italic">No player notes recorded</p>
              )}
            </div>

            {/* Competitive Ranks Snapshot */}
            <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
              <div className="flex justify-between items-center">
                <SectionLabel>RANKS · KNOWN PLAYLIST RANKS (CORE 3V3 · 2V2 · 1V1)</SectionLabel>
                {ranks.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setShowAllRanks((v) => !v)}
                    className="text-sm font-extrabold t-text-blue text-blue-ink hover:underline uppercase cursor-pointer"
                  >
                    {showAllRanks ? 'HIDE EXTRA MODES ▲' : 'VIEW FULL PLAYLIST STATS & EXTRA MODES →'}
                  </button>
                )}
              </div>
              {ranks.length === 0 ? (
                <p className="font-semibold text-base tracking-t8 text-muted">NO RANK DATA RECORDED</p>
              ) : (
                <>
                  <div className="grid gap-2 grid-cols-[repeat(auto-fill,minmax(200px,1fr))]">
                    {ranks.slice(0, 3).map((r) => (
                      <div
                        key={r.playlist_id}
                        className="bg-panel px-[18px] py-3.5 flex justify-between items-end gap-3 min-w-0 t-border border border-line"
                      >
                        <div className="flex flex-col gap-0.5 min-w-0">
                          <span className={PANEL_LABEL}>{playlistLabels(r.playlist_id, r.playlist_name).mode}</span>
                          <span className="font-bold text-[22px] tracking-t3 uppercase truncate">
                            {r.rank_name ? shortRank(r.rank_name, r.division) : 'UNRANKED'}
                          </span>
                        </div>
                        <span className="font-bold text-[22px] text-ink-2 t-font-mono">{formatMmr(r.mmr)}</span>
                      </div>
                    ))}
                  </div>
                  {showAllRanks && (
                    <div className="grid gap-2 grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 pt-1">
                      {[
                        { id: 27, label: 'HOOPS (2V2)' },
                        { id: 28, label: 'RUMBLE (3V3)' },
                        { id: 29, label: 'DROPSHOT (3V3)' },
                        { id: 30, label: 'SNOW DAY (3V3)' },
                      ].map((em) => {
                        const found = ranks.find((r) => r.playlist_id === em.id);
                        return (
                          <div
                            key={em.id}
                            className="bg-panel px-4 py-2.5 flex justify-between items-center gap-2 min-w-0 t-border border border-line"
                          >
                            <div className="flex flex-col min-w-0">
                              <span className="text-sm font-bold text-muted uppercase">{em.label}</span>
                              <span className="font-bold text-base uppercase truncate">
                                {found?.rank_name ? shortRank(found.rank_name, found.division) : 'UNRANKED'}
                              </span>
                            </div>
                            <span className="font-bold text-lg text-ink-2 t-font-mono">
                              {found?.mmr ? formatMmr(found.mmr) : '—'}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Breakdown By Playlist */}
            <div className="px-8 pt-6 pb-2 flex flex-col gap-2">
              <SectionLabel className="pb-1">BY PLAYLIST</SectionLabel>
              {profile.matchups?.length ? (
                <>
                  <div className={`grid px-[18px] ${COLUMN_HEADER}`} style={{ gridTemplateColumns: PLAYLIST_GRID }}>
                    <span>PLAYLIST</span>
                    <span>WITH YOU</span>
                    <span>VS YOU</span>
                    <span className="text-right">GAMES</span>
                    <span className="text-right">LAST MET</span>
                  </div>
                  {profile.matchups.map((m) => {
                    const isSelected = playlistFilter === m.playlist_id;
                    return (
                      <div
                        key={m.playlist_id}
                        data-testid="profile-playlist-row"
                        role="button"
                        tabIndex={0}
                        onClick={() => {
                          setPlaylistFilter((cur) => (cur === m.playlist_id ? 0 : m.playlist_id));
                          setMatchPage(1);
                        }}
                        className={`grid items-center h-[52px] bg-panel px-[18px] cursor-pointer transition-all duration-120 ${
                          isSelected ? 'ring-2 ring-blue-ink bg-panel-hover' : 'hover:bg-panel-hover'
                        }`}
                        style={{ gridTemplateColumns: PLAYLIST_GRID }}
                      >
                        <span className="font-bold text-lg tracking-t6 truncate">
                          {playlistLabels(m.playlist_id).name}
                        </span>
                        <RecordWithPct wins={m.wins_as_teammate} losses={m.losses_as_teammate} />
                        <RecordWithPct wins={m.wins_as_opponent} losses={m.losses_as_opponent} />
                        <span className="text-right font-bold text-xl t-font-mono">{m.total_matches}</span>
                        <span className="text-right font-semibold text-sm tracking-t8 text-muted">
                          {formatAgo(m.last_played_at)}
                        </span>
                      </div>
                    );
                  })}
                </>
              ) : (
                <p className="font-semibold text-base tracking-t8 text-muted">NO RECORDED MATCHUPS</p>
              )}
            </div>

            {/* Paginated Chronological Match History with Level 1 Accordion */}
            {(matches.length > 0 || isLoadingMatches || roleFilter !== 'all' || playlistFilter !== 0) && (
              <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
                <div className="flex justify-between items-center">
                  <SectionLabel>MATCH HISTORY ({totalMatches})</SectionLabel>
                  {totalPages > 1 && (
                    <div className="flex items-center gap-2 text-sm font-bold">
                      <button
                        type="button"
                        disabled={matchPage <= 1}
                        onClick={() => setMatchPage((p) => Math.max(1, p - 1))}
                        className="px-2 py-0.5 bg-panel border border-line-2 disabled:opacity-40 uppercase"
                      >
                        PREV
                      </button>
                      <span className="text-muted">
                        PAGE {matchPage} OF {totalPages}
                      </span>
                      <button
                        type="button"
                        disabled={matchPage >= totalPages}
                        onClick={() => setMatchPage((p) => Math.min(totalPages, p + 1))}
                        className="px-2 py-0.5 bg-panel border border-line-2 disabled:opacity-40 uppercase"
                      >
                        NEXT
                      </button>
                    </div>
                  )}
                </div>

                <div className="flex flex-col gap-2">
                  {isLoadingMatches && matches.length === 0 ? (
                    <div className="p-4 text-center text-muted font-bold text-sm">LOADING MATCHES…</div>
                  ) : matches.length === 0 ? (
                    <div className="p-4 text-center text-muted font-bold text-sm">NO RECORDED MATCHES FOUND</div>
                  ) : (
                    matches.map((m) => {
                      const isExpanded = openMatchGuid === m.match_guid;
                      const localOutcome = resolveLocalOutcome(m);
                      const outcome =
                        localOutcome === 'victory'
                          ? 'W'
                          : localOutcome === 'defeat'
                          ? 'L'
                          : null;
                      return (
                        <div key={m.match_guid} className="bg-panel border border-line flex flex-col">
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
                                  className={`font-extrabold text-sm px-2 py-0.5 text-page rounded ${OUTCOME_SLAB[outcome]}`}
                                >
                                  {outcome === 'W' ? 'WIN' : 'LOSS'}
                                </span>
                              )}
                              <span className="font-extrabold text-lg t-font-mono">
                                <span className="text-blue-ink">{m.blue_score}</span>
                                <span className="text-muted"> – </span>
                                <span className="text-orange-ink">{m.orange_score}</span>
                              </span>
                              <span className="font-bold text-base truncate">
                                {playlistLabels(m.playlist).name} · {m.map_name || 'Arena'}
                              </span>
                            </div>

                            <div className="flex items-center gap-3">
                              <span
                                className={`text-sm font-bold px-2 py-0.5 rounded ${
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

                          {/* Level 1 Box Score Accordion */}
                          {isExpanded && (
                            <div className="px-4 pb-4 pt-2 border-t border-line flex flex-col gap-3">
                              <div className="grid grid-cols-1 xl:grid-cols-2 gap-[2px] bg-page">
                                {[0, 1].map((team) => (
                                  <TeamBoxScore
                                    key={team}
                                    team={team as 0 | 1}
                                    label={team === 0 ? 'BLUE' : 'ORANGE'}
                                    rows={teamRows(m.stats?.players || [], team, localPlayerIds, id)}
                                    withRanks
                                    onPlayerClick={handlePlayerClick}
                                  />
                                ))}
                              </div>
                              <div className="flex justify-end">
                                <button
                                  type="button"
                                  onClick={() => setTelemetryMatch(m)}
                                  className="px-3 py-1.5 bg-blue-slab text-white font-bold text-sm tracking-wider rounded uppercase hover:opacity-90 transition-colors cursor-pointer"
                                >
                                  FULL TELEMETRY →
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
            )}

            {/* Profile Timestamps Footer */}
            <div className="flex justify-between gap-4 px-8 py-3.5 border-t border-line font-semibold text-sm tracking-t12 text-muted-2 mt-4">
              <span>FIRST SEEN {formatShortDate(profile.first_seen_at)}</span>
              <span>LAST SEEN {formatAgo(profile.last_seen_at)}</span>
            </div>
          </div>
        )}
      </Modal>

      {/* Level 2 Telemetry Modal */}
      {telemetryMatch && (
        <HistoricalMatchDetailModal
          match={telemetryMatch}
          onClose={() => setTelemetryMatch(null)}
          localPlayerIds={localPlayerIds}
          onPlayerClick={handlePlayerClick}
          inspectedPlayerId={id}
        />
      )}

      {/* Player Scouting Notes & Tags Modal */}
      {profile && (
        <PlayerNotesModal
          isOpen={isNotesModalOpen}
          initialNotes={profile.notes || ''}
          initialTags={profile.tags || []}
          onClose={() => setIsNotesModalOpen(false)}
          onSave={handleSaveNotes}
        />
      )}
    </>
  );
};
