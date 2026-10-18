import React, { useEffect, useState } from 'react';
import { PlayerDetailResponse, PlayerMatchDetail, PlayerMatchesResponse, HistoricalMatchRecord } from '../../types/api';
import { Modal } from '../common/Modal';
import { COLUMN_HEADER, PANEL_LABEL, RecordCells, SectionLabel } from '../common/ui';
import {
  OUTCOME_SLAB,
  formatAgo,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  platformLabel,
  playlistLabels,
  recordFill,
  recordInk,
  trackerUrl,
} from '../../utils/broadcast';
import { h2hTag, parseRanks, sumMatchups } from './playerData';
import { teamRows } from '../historical/historyData';
import { TeamBoxScore } from '../common/BoxScore';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { PlayerNotesModal } from './PlayerNotesModal';

interface PlayerProfileDialogProps {
  playerId: string | null;
  /** Name from the directory row, shown while the profile loads. */
  playerName?: string;
  onClose: () => void;
  localPlayerIds?: Set<string>;
}

const PLAYLIST_GRID = 'minmax(0,1.2fr) minmax(0,1fr) minmax(0,1fr) 70px 120px';
const MATCH_PAGE_SIZE = 10;
const NO_IDS = new Set<string>();

const H2HBlock: React.FC<{
  label: string;
  wins: number;
  losses: number;
  teammate: boolean;
  selected?: boolean;
  onClick?: () => void;
}> = ({ label, wins, losses, teammate, selected, onClick }) => {
  const games = wins + losses;
  const tag = h2hTag(wins, losses, teammate);
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      data-testid={teammate ? 'h2h-with' : 'h2h-vs'}
      className={`bg-panel px-7 py-[22px] flex flex-col gap-3.5 min-w-0 cursor-pointer transition-all duration-150 ${
        selected ? 'ring-2 ring-blue-ink bg-panel-hover' : 'hover:bg-panel-hover'
      }`}
    >
      <div className="flex justify-between items-center gap-3">
        <SectionLabel>
          {label} · {games} {games === 1 ? 'GAME' : 'GAMES'}
        </SectionLabel>
        {tag && (
          <span className={`font-bold text-[12px] tracking-t14 text-page px-2 py-0.5 whitespace-nowrap ${recordFill(wins, losses)}`}>
            {tag}
          </span>
        )}
      </div>
      <div className="flex items-baseline justify-between gap-3">
        <span className={`font-extrabold text-[64px] leading-tightest ${recordInk(wins, losses)}`}>{formatRecord(wins, losses)}</span>
        <span className="font-bold text-[30px] text-muted">{formatPct(wins, losses)}</span>
      </div>
      <RecordCells wins={wins} losses={losses} count={10} cellClass="flex-1 h-[14px]" />
    </div>
  );
};

const RecordWithPct: React.FC<{ wins: number; losses: number }> = ({ wins, losses }) => (
  <span className={`font-bold text-[20px] ${recordInk(wins, losses)}`}>
    {formatRecord(wins, losses)} <span className="text-[14px] text-muted font-semibold">{formatPct(wins, losses)}</span>
  </span>
);

export const PlayerProfileDialog: React.FC<PlayerProfileDialogProps> = ({
  playerId,
  playerName,
  onClose,
  localPlayerIds = NO_IDS,
}) => {
  // Cross-player navigation history stack
  const [historyStack, setHistoryStack] = useState<Array<{ id: string; name?: string }>>([]);

  const activeEntry = historyStack[historyStack.length - 1];
  const activeId = activeEntry?.id ?? playerId;
  const activeName = activeEntry?.name ?? playerName;

  // Initialize or reset history stack when root playerId prop changes
  useEffect(() => {
    if (playerId) {
      setHistoryStack([{ id: playerId, name: playerName }]);
    } else {
      setHistoryStack([]);
    }
  }, [playerId, playerName]);

  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoadingProfile, setIsLoadingProfile] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

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
    fetch(`/api/players/${encodeURIComponent(activeId)}`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to load player`);
        return res.json();
      })
      .then((data: PlayerDetailResponse) => setProfile(data))
      .catch((err) => {
        if (!controller.signal.aborted) setError(err instanceof Error ? err.message : 'Error fetching profile');
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoadingProfile(false);
      });
    return () => controller.abort();
  }, [activeId]);

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

    fetch(`/api/players/${encodeURIComponent(activeId)}/matches?${params.toString()}`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data: PlayerMatchesResponse) => {
        setMatches(data.matches || []);
        setTotalMatches(data.total || 0);
      })
      .catch(() => {
        // Fallback gracefully (e.g. In unit tests where /matches is unmocked)
        setMatches([]);
        setTotalMatches(0);
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoadingMatches(false);
      });

    return () => controller.abort();
  }, [activeId, roleFilter, playlistFilter, matchPage]);

  if (!activeId) return null;

  const id = profile?.player_id ?? activeId;
  const name = profile?.player_name || activeName || 'PLAYER';
  const platform = profile?.platform || id;
  const tracker = trackerUrl(id, platform, name);
  const ranks = parseRanks(profile?.ranks_json);
  const totals = sumMatchups(profile?.matchups);

  const copyId = () => {
    try {
      navigator.clipboard?.writeText(id);
    } catch {}
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handlePlayerClick = (nextPlayerId: string, nextPlayerName?: string) => {
    if (!nextPlayerId || nextPlayerId === id) return;
    setHistoryStack((prev) => [...prev, { id: nextPlayerId, name: nextPlayerName }]);
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
            <button
              type="button"
              onClick={handleBack}
              className="hover:underline flex items-center gap-1.5 text-blue-ink font-bold uppercase cursor-pointer"
            >
              <span>← BACK TO {historyStack[historyStack.length - 2]?.name || 'PREVIOUS PLAYER'}</span>
            </button>
          ) : (
            `PLAYER PROFILE · ${platformLabel(platform)}`
          )
        }
        title={name}
        meta={
          <>
            <span className="text-ink-2 normal-case break-all">{id}</span>
            <button type="button" onClick={copyId} className="text-muted hover:text-ink transition-colors duration-120">
              {copied ? 'COPIED ✓' : 'COPY ID'}
            </button>
            {tracker && (
              <a href={tracker} target="_blank" rel="noopener noreferrer" className="text-ink hover:text-blue-ink transition-colors duration-120">
                RL TRACKER ↗
              </a>
            )}
          </>
        }
      >
        {error ? (
          <p className="px-8 py-6 font-semibold text-[15px] tracking-t8 text-loss uppercase">COULD NOT LOAD PROFILE · {error}</p>
        ) : !profile && isLoadingProfile ? (
          <p className="px-8 py-6 font-semibold text-[15px] tracking-t8 text-muted">LOADING PROFILE…</p>
        ) : !profile ? null : (
          <div className="flex flex-col font-display pb-4">
            {/* Interactive Head-to-Head Filter Cards */}
            <div className="grid grid-cols-2 gap-[2px] bg-page">
              <H2HBlock
                label="WITH YOU"
                wins={totals.mate.wins}
                losses={totals.mate.losses}
                teammate
                selected={roleFilter === 'teammate'}
                onClick={() => {
                  setRoleFilter((cur) => (cur === 'teammate' ? 'all' : 'teammate'));
                  setMatchPage(1);
                }}
              />
              <H2HBlock
                label="VS YOU"
                wins={totals.vs.wins}
                losses={totals.vs.losses}
                teammate={false}
                selected={roleFilter === 'opponent'}
                onClick={() => {
                  setRoleFilter((cur) => (cur === 'opponent' ? 'all' : 'opponent'));
                  setMatchPage(1);
                }}
              />
            </div>

            {/* Active Filter Summary Bar */}
            {(roleFilter !== 'all' || playlistFilter > 0) && (
              <div data-testid="active-filter-summary" className="mx-8 mt-4 bg-panel border-l-4 border-blue-ink px-6 py-2.5 flex items-center justify-between text-[13px] font-bold">
                <div className="flex items-center gap-2">
                  <span className="text-muted">ACTIVE FILTER:</span>
                  <span className="text-ink uppercase">
                    {roleFilter === 'teammate' ? 'WITH YOU (TEAMMATE)' : roleFilter === 'opponent' ? 'VS YOU (OPPONENT)' : ''}
                    {roleFilter !== 'all' && playlistFilter > 0 ? ' · ' : ''}
                    {playlistFilter > 0 ? playlistLabels(playlistFilter).name : ''}
                  </span>
                  <span className="text-muted">({totalMatches} {totalMatches === 1 ? 'match' : 'matches'})</span>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    setRoleFilter('all');
                    setPlaylistFilter(0);
                    setMatchPage(1);
                  }}
                  className="text-muted hover:text-loss text-[11px] uppercase underline cursor-pointer"
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
                  className="text-[12px] font-bold text-ink-2 hover:text-ink tracking-wider uppercase transition-colors"
                >
                  EDIT NOTES & TAGS ✎
                </button>
              </div>

              {profile.tags && profile.tags.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                  {profile.tags.map((t) => (
                    <span key={t} className="bg-panel border border-line-2 px-2.5 py-1 text-[13px] font-bold text-ink tracking-wider uppercase rounded">
                      {t}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="text-[13px] text-muted italic">No custom tags assigned</p>
              )}

              {profile.notes ? (
                <div className="bg-panel p-3 border-l-2 border-blue-ink text-[14px] text-ink-2 whitespace-pre-wrap font-sans">
                  {profile.notes}
                </div>
              ) : (
                <p className="text-[13px] text-muted italic">No player notes recorded</p>
              )}
            </div>

            {/* Competitive Ranks Snapshot */}
            <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
              <SectionLabel>RANKS</SectionLabel>
              {ranks.length === 0 ? (
                <p className="font-semibold text-[15px] tracking-t8 text-muted">NO RANK DATA RECORDED</p>
              ) : (
                <div className="grid gap-2 grid-cols-[repeat(auto-fill,minmax(200px,1fr))]">
                  {ranks.map((r) => (
                    <div key={r.playlist_id} className="bg-panel px-[18px] py-3.5 flex justify-between items-end gap-3 min-w-0">
                      <div className="flex flex-col gap-0.5 min-w-0">
                        <span className={PANEL_LABEL}>{playlistLabels(r.playlist_id, r.playlist_name).mode}</span>
                        <span className="font-bold text-[22px] tracking-t3 uppercase truncate">{r.rank_name || 'UNRANKED'}</span>
                      </div>
                      <span className="font-bold text-[22px] text-ink-2">{formatMmr(r.mmr)}</span>
                    </div>
                  ))}
                </div>
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
                        <span className="font-bold text-[18px] tracking-t6 truncate">{playlistLabels(m.playlist_id).name}</span>
                        <RecordWithPct wins={m.wins_as_teammate} losses={m.losses_as_teammate} />
                        <RecordWithPct wins={m.wins_as_opponent} losses={m.losses_as_opponent} />
                        <span className="text-right font-bold text-[20px]">{m.total_matches}</span>
                        <span className="text-right font-semibold text-[14px] tracking-t8 text-muted">{formatAgo(m.last_played_at)}</span>
                      </div>
                    );
                  })}
                </>
              ) : (
                <p className="font-semibold text-[15px] tracking-t8 text-muted">NO RECORDED MATCHUPS</p>
              )}
            </div>

            {/* Paginated Chronological Match History with Level 1 Accordion */}
            {(matches.length > 0 || isLoadingMatches || roleFilter !== 'all' || playlistFilter !== 0) && (
              <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
                <div className="flex justify-between items-center">
                  <SectionLabel>MATCH HISTORY ({totalMatches})</SectionLabel>
                  {totalPages > 1 && (
                    <div className="flex items-center gap-2 text-[12px] font-bold">
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
                    <div className="p-4 text-center text-muted font-bold text-[14px]">LOADING MATCHES…</div>
                  ) : matches.length === 0 ? (
                    <div className="p-4 text-center text-muted font-bold text-[14px]">NO RECORDED MATCHES FOUND</div>
                  ) : (
                    matches.map((m) => {
                    const isExpanded = openMatchGuid === m.match_guid;
                    const isOpponent = m.role === 'opponent';
                    const outcome =
                      m.result === 'victory'
                        ? isOpponent
                          ? 'L'
                          : 'W'
                        : m.result === 'defeat'
                          ? isOpponent
                            ? 'W'
                            : 'L'
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
                              <span className={`font-extrabold text-[12px] px-2 py-0.5 text-page rounded ${OUTCOME_SLAB[outcome]}`}>
                                {outcome === 'W' ? 'WIN' : 'LOSS'}
                              </span>
                            )}
                            <span className="font-extrabold text-[18px]">
                              <span className="text-blue-ink">{m.blue_score}</span>
                              <span className="text-muted"> – </span>
                              <span className="text-orange-ink">{m.orange_score}</span>
                            </span>
                            <span className="font-bold text-[15px] truncate">
                              {playlistLabels(m.playlist).name} · {m.map_name || 'Arena'}
                            </span>
                          </div>

                          <div className="flex items-center gap-3">
                            <span className={`text-[12px] font-bold px-2 py-0.5 rounded ${m.role === 'teammate' ? 'bg-win/15 text-win' : 'bg-orange-slab/20 text-orange-ink'}`}>
                              {m.role === 'teammate' ? 'WITH YOU' : 'VS YOU'}
                            </span>
                            <span className="text-muted text-[13px]">{formatAgo(m.stats?.date || m.created_at)}</span>
                            <span className="text-muted text-[14px]">{isExpanded ? '▲' : '▼'}</span>
                          </div>
                        </div>

                        {/* Level 1 Box Score Accordion */}
                        {isExpanded && (
                          <div className="px-4 pb-4 pt-2 border-t border-line flex flex-col gap-3">
                            <div className="grid gap-[2px] bg-page">
                              {[0, 1].map((team) => (
                                <TeamBoxScore
                                  key={team}
                                  team={team as 0 | 1}
                                  label={team === 0 ? 'BLUE' : 'ORANGE'}
                                  rows={teamRows(m.stats?.players || [], team, localPlayerIds, id)}
                                  onPlayerClick={handlePlayerClick}
                                />
                              ))}
                            </div>
                            <div className="flex justify-end">
                              <button
                                type="button"
                                onClick={() => setTelemetryMatch(m)}
                                className="px-3 py-1.5 bg-blue-slab text-page font-bold text-[12px] tracking-wider rounded uppercase hover:bg-blue-ink transition-colors"
                              >
                                FULL TELEMETRY →
                              </button>
                            </div>
                          </div>
                        )}
                      </div>
                    );
                  }))}
                </div>
              </div>
            )}

            {/* Profile Timestamps Footer */}
            <div className="flex justify-between gap-4 px-8 py-3.5 border-t border-line font-semibold text-[13px] tracking-t12 text-muted-2 mt-4">
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
