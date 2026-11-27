import React, { useState } from 'react';
import { CurrentMatchResponse, HistoricalMatchRecord } from '../../types/api';
import { COLUMN_HEADER, PANEL_LABEL, SectionLabel } from '../common/ui';
import { AreaFilterBar } from '../common/AreaFilterBar';
import { PlatformBadge } from '../common/PlatformBadge';
import { Sparkline } from '../common/Sparkline';
import { HistoricalMatchDetailModal, TelemetryTab } from '../historical/HistoricalMatchDetailModal';
import { PlayerNotesModal } from './PlayerNotesModal';
import { useDashboard } from '../../context/DashboardContext';
import {
  compactRank,
  formatAgo,
  formatMmr,
  formatShortDate,
  playlistLabels,
  shortRank,
} from '../../utils/broadcast';
import { STAT_LABEL_CLASS } from '../../utils/statColors';
import {
  CompactRoleSplitCard,
  RecordWithPct,
} from './PlayerRoleSplitCard';
import { PlayerMatchHistoryList } from './PlayerMatchHistoryList';
import { PlayerDossierDataState, usePlayerDossierData } from './usePlayerDossierData';

interface PlayerDossierViewProps {
  playerId: string;
  playerName?: string;
  variant?: 'dialog' | 'page';
  localPlayerIds?: Set<string>;
  dossier?: PlayerDossierDataState;
  liveMatch?: CurrentMatchResponse | null;
  onSelectPlayer?: (playerId: string, playerName?: string) => void;
}

const PLAYLIST_GRID = 'minmax(150px,1.2fr) minmax(110px,1fr) minmax(110px,1fr) 70px 120px';
const NO_IDS = new Set<string>();

const EXTRA_MODE_SPECS: ReadonlyArray<{ id: number; label: string }> = [
  { id: 27, label: 'HOOPS (2V2)' },
  { id: 28, label: 'RUMBLE (3V3)' },
  { id: 29, label: 'DROPSHOT (3V3)' },
  { id: 30, label: 'SNOW DAY (3V3)' },
  { id: 34, label: 'TOURNAMENTS (3V3)' },
];

export const PlayerDossierView: React.FC<PlayerDossierViewProps> = ({
  playerId,
  playerName,
  variant = 'page',
  localPlayerIds = NO_IDS,
  dossier: externalDossier,
  liveMatch,
  onSelectPlayer = () => {},
}) => {
  const {
    pinnedPlayerIds,
    togglePinPlayer,
  } = useDashboard();

  const internalDossier = usePlayerDossierData(playerId, playerName, localPlayerIds, {
    enabled: !externalDossier,
    pageSize: variant === 'dialog' ? 10 : 20,
    liveMatch,
  });
  const d = externalDossier ?? internalDossier;
  const isAvg = d.statMode === 'avg';

  const [showAllRanks, setShowAllRanks] = useState(false);
  const [telemetryMatch, setTelemetryMatch] = useState<HistoricalMatchRecord | null>(null);
  const [telemetryInitialTab, setTelemetryInitialTab] = useState<TelemetryTab>('core');
  const [isNotesModalOpen, setIsNotesModalOpen] = useState(false);

  const isDialog = variant === 'dialog';
  const padX = isDialog ? 'px-8' : '';
  const isPinned = pinnedPlayerIds.includes(d.id);
  const hasSecondaryFilter =
    d.roleFilter !== 'all' || d.resultFilter !== 'all' || d.searchQuery.trim() !== '';
  const showHistory =
    !isDialog ||
    d.matches.length > 0 ||
    d.isLoadingMatches ||
    hasSecondaryFilter;

  const handleSelectPlaylistRow = (playlistId: number) => {
    if (playlistId <= 0) return;
    d.setPlaylistById(playlistId);
  };

  const handleClearFilters = () => {
    d.clearFilters();
  };

  const activeModeKey = d.activePlaylistParam || '11';
  const showScopeReset =
    activeModeKey !== '11' ||
    d.dateRange !== 'all' ||
    d.showCustomCalendar ||
    d.statMode !== 'avg';

  const activePlaylistLabel =
    d.activePlaylistId > 0
      ? playlistLabels(d.activePlaylistId).name
      : d.statPlaylist === 'casual'
      ? 'CASUAL'
      : '';

  const displayTotalMatches = d.isDemoMatches
    ? d.filteredAndSortedMatches.length
    : d.totalMatches;

  return (
    <>
      {d.error ? (
        <div className={`${padX} py-6 font-semibold text-base tracking-t8 text-loss uppercase`}>
          COULD NOT LOAD PROFILE · {d.error}
        </div>
      ) : !d.profile && d.isLoadingProfile ? (
        <div className={`${padX} py-6 font-semibold text-base tracking-t8 text-muted`}>
          LOADING PROFILE…
        </div>
      ) : !d.profile && isDialog ? null : (
        <div className="flex flex-col font-display pb-4 gap-4">
          {/* Independent Dossier Scope Controls: Mode, Timeframe, AVG/TOTALS, MMR Trend & Pin */}
          <div className={`${padX} ${isDialog ? 'pt-4' : ''}`}>
            <AreaFilterBar
              area="dossier"
              ariaLabel="Player profile scope filters"
              playlist={activeModeKey}
              onPlaylistChange={(mode) => d.setPlaylistByModeKey(mode)}
              dateRange={d.dateRange}
              onDateRangeChange={d.setDateRange}
              customFrom={d.customFrom}
              onCustomFromChange={d.setCustomFrom}
              customTo={d.customTo}
              onCustomToChange={d.setCustomTo}
              showCustomCalendar={d.showCustomCalendar}
              onToggleCustomCalendar={() => {
                if (!d.showCustomCalendar && d.dateRange !== 'custom') {
                  d.setDateRange('custom');
                } else {
                  d.setShowCustomCalendar((prev) => !prev);
                }
              }}
              statMode={d.statMode}
              onStatModeChange={d.setStatMode}
              showReset={showScopeReset}
              onReset={d.resetScopeFilters}
              leadingSlot={
                d.mmrSparkline.length > 0 ? (
                  <div
                    data-testid="dossier-mmr-sparkline"
                    className="flex items-center gap-2 px-2 py-0.5 t-bg-page bg-page t-border border border-line t-radius"
                  >
                    <span className="text-[11px] text-muted font-bold uppercase">MMR TREND</span>
                    <Sparkline data={d.mmrSparkline} width={76} height={18} />
                  </div>
                ) : undefined
              }
              trailingSlot={
                <button
                  type="button"
                  data-testid="pin-player-btn"
                  onClick={() => togglePinPlayer(d.id)}
                  className={`toolbar-btn px-2.5 py-1 t-border border border-outline font-bold text-xs uppercase tracking-wider t-radius cursor-pointer whitespace-nowrap ${
                    isPinned
                      ? 't-bg-ink bg-ink text-page'
                      : 't-bg-page bg-page t-text-blue text-blue-ink hover:border-ink'
                  }`}
                >
                  {isPinned ? '★ PINNED' : '☆ PIN PLAYER'}
                </button>
              }
            />
          </div>

          {/* Interactive WITH YOU & AGAINST YOU H2H Role Split Cards */}
          <div className={`${padX} grid grid-cols-1 lg:grid-cols-2 gap-3.5`}>
            <CompactRoleSplitCard
              label={d.isOwnProfile ? 'YOUR STATS' : 'WITH YOU'}
              roleTitle={d.isOwnProfile ? 'SELECTED MODE (YOU)' : 'WITH YOU (TEAMMATE)'}
              wins={d.totals.mate.wins}
              losses={d.totals.mate.losses}
              teammate
              stats={d.withStats}
              isAvg={isAvg}
              emptyMessage="NO MATCHES PLAYED AS TEAMMATE IN SELECTED RANGE"
              selected={d.roleFilter === 'teammate'}
              onClick={() => {
                d.setRoleFilter((cur) => (cur === 'teammate' ? 'all' : 'teammate'));
                d.setMatchPage(1);
              }}
            />
            <CompactRoleSplitCard
              label="VS YOU"
              roleTitle="AGAINST YOU (OPPONENT)"
              wins={d.totals.vs.wins}
              losses={d.totals.vs.losses}
              teammate={false}
              stats={d.vsStats}
              isAvg={isAvg}
              emptyMessage="NO MATCHES PLAYED AS OPPONENT IN SELECTED RANGE"
              selected={d.roleFilter === 'opponent'}
              onClick={() => {
                d.setRoleFilter((cur) => (cur === 'opponent' ? 'all' : 'opponent'));
                d.setMatchPage(1);
              }}
            />
          </div>

          {/* Active Secondary Filter Summary Bar */}
          {hasSecondaryFilter && (
            <div
              data-testid="active-filter-summary"
              className={`${
                isDialog ? 'mx-8' : ''
              } bg-panel border-l-4 border-blue-ink px-6 py-2.5 flex items-center justify-between text-sm font-bold`}
            >
              <div className="flex items-center gap-2">
                <span className="text-muted">ACTIVE FILTER:</span>
                <span className="text-ink uppercase">
                  {d.roleFilter === 'teammate'
                    ? 'WITH YOU (TEAMMATE)'
                    : d.roleFilter === 'opponent'
                    ? 'VS YOU (OPPONENT)'
                    : d.resultFilter === 'W'
                    ? 'WINS'
                    : d.resultFilter === 'L'
                    ? 'LOSSES'
                    : d.searchQuery.trim()}
                  {activePlaylistLabel ? ` · ${activePlaylistLabel}` : ''}
                </span>
                <span className="text-muted">
                  ({displayTotalMatches} {displayTotalMatches === 1 ? 'match' : 'matches'})
                </span>
              </div>
              <button
                type="button"
                onClick={handleClearFilters}
                className="text-muted hover:text-loss text-sm uppercase underline cursor-pointer"
              >
                CLEAR FILTERS
              </button>
            </div>
          )}

          {/* Competitive Ranks Snapshot + Extra Modes */}
          <div className={`${padX} pt-2 flex flex-col gap-3`}>
            <div className="flex flex-wrap justify-between items-center gap-2">
              <SectionLabel>
                KNOWN PLAYLIST RANKS (CORE 3V3 · 2V2 · 1V1)
              </SectionLabel>
              <button
                type="button"
                data-testid="toggle-extra-ranks-btn"
                onClick={() => setShowAllRanks((v) => !v)}
                className="text-sm font-extrabold t-text-blue text-blue-ink hover:underline uppercase cursor-pointer"
              >
                {showAllRanks
                  ? 'HIDE EXTRA MODES ▲'
                  : 'VIEW FULL PLAYLIST STATS & EXTRA MODES →'}
              </button>
            </div>

            {d.allRanks.length === 0 && isDialog ? (
              <p className="font-semibold text-base tracking-t8 text-muted">
                NO RANK DATA RECORDED
              </p>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 t-num">
                {d.coreRankCards.map((c) => (
                  <div
                    key={c.playlistId}
                    className="t-bg-panel bg-panel t-border border border-line px-4 py-3 flex justify-between items-center"
                  >
                    <div>
                      <div className={PANEL_LABEL}>{c.label}</div>
                      <div className="t-font-display font-extrabold text-[22px] t-text-strong text-ink-strong uppercase">
                        {c.rank}
                      </div>
                    </div>
                    <div className="text-right">
                      <div className="t-font-mono font-extrabold text-[20px] t-text-blue text-blue-ink">
                        {c.mmr} MMR
                      </div>
                      <div className="text-sm font-extrabold text-muted t-font-mono">
                        {c.games} GAMES
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}

            {showAllRanks && (
              <div className="grid gap-2 grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 pt-1 t-num">
                {EXTRA_MODE_SPECS.map((em) => {
                  const found = d.allRanks.find((r) => r.playlist_id === em.id);
                  const fullRank = found?.rank_name
                    ? shortRank(found.rank_name, found.division, found.short_rank, found.tier)
                    : 'UNRANKED';
                  const badgeRank = found?.rank_name
                    ? compactRank(found.rank_name, found.division, found.short_rank, found.tier)
                    : 'UNRANKED';
                  return (
                    <div
                      key={em.id}
                      className="bg-panel px-3.5 py-2.5 flex justify-between items-center gap-2 min-w-0 t-border border border-line"
                    >
                      <div className="flex flex-col min-w-0">
                        <span className="text-xs font-bold text-muted uppercase whitespace-nowrap">
                          {em.label}
                        </span>
                        <span
                          title={fullRank}
                          className="font-bold text-base uppercase whitespace-nowrap"
                        >
                          {badgeRank}
                        </span>
                      </div>
                      <span className="font-bold text-lg text-ink-2 t-font-mono whitespace-nowrap shrink-0">
                        {found?.mmr ? formatMmr(found.mmr) : '—'}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Scouting Notes & Tags */}
          <div className={`${padX} flex flex-col gap-2`}>
            <div className="t-bg-panel bg-panel t-border border border-line p-4 flex flex-col gap-2">
              <div className="flex justify-between items-center gap-2">
                <SectionLabel>SCOUTING NOTES & TAGS</SectionLabel>
                <button
                  type="button"
                  data-testid="edit-notes-btn"
                  onClick={() => setIsNotesModalOpen(true)}
                  className="px-3 py-1 t-bg-page bg-page t-border border border-outline hover:border-ink text-sm font-bold text-ink-2 hover:text-ink tracking-wider uppercase t-radius transition-colors cursor-pointer"
                >
                  EDIT NOTES & TAGS ✎
                </button>
              </div>
              {d.profile?.tags && d.profile.tags.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                  {d.profile.tags.map((t) => (
                    <span
                      key={t}
                      className="t-bg-page bg-page border border-line-2 px-2.5 py-1 text-sm font-bold text-ink tracking-wider uppercase rounded"
                    >
                      {t}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-muted italic">No custom tags assigned</p>
              )}
              {d.profile?.notes ? (
                <div className="t-bg-page bg-page p-3 border-l-2 border-blue-ink text-sm text-ink-2 whitespace-pre-wrap font-sans">
                  {d.profile.notes}
                </div>
              ) : (
                <p className="text-sm text-muted italic">No player notes recorded</p>
              )}
            </div>
          </div>

          {/* Interactive Breakdown By Playlist */}
          <div className={`${padX} flex flex-col gap-2`}>
            <SectionLabel className="pb-1">BY PLAYLIST</SectionLabel>
            {d.playlistBreakdown.length ? (
              <div
                role="region"
                aria-label="Player breakdown by playlist"
                tabIndex={0}
                className="overflow-x-auto"
              >
                <div className="min-w-[580px] flex flex-col gap-2">
                  <div
                    className={`grid px-[18px] ${COLUMN_HEADER}`}
                    style={{ gridTemplateColumns: PLAYLIST_GRID }}
                  >
                    <span>PLAYLIST</span>
                    <span className={STAT_LABEL_CLASS.goals}>WITH YOU</span>
                    <span className={STAT_LABEL_CLASS.demos}>VS YOU</span>
                    <span className={`text-right ${STAT_LABEL_CLASS.games}`}>GAMES</span>
                    <span className="text-right">LAST MET</span>
                  </div>
                  {d.playlistBreakdown.map((m) => {
                    const isSelected = d.activePlaylistId === m.playlist_id;
                    return (
                      <div
                        key={m.playlist_id}
                        data-testid="profile-playlist-row"
                        role="button"
                        tabIndex={0}
                        onClick={() => handleSelectPlaylistRow(m.playlist_id)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            handleSelectPlaylistRow(m.playlist_id);
                          }
                        }}
                        className={`grid items-center min-h-[52px] py-2 bg-panel px-[18px] cursor-pointer transition-all duration-120 ${
                          isSelected
                            ? 'ring-2 ring-blue-ink bg-panel-hover'
                            : 'hover:bg-panel-hover'
                        }`}
                        style={{ gridTemplateColumns: PLAYLIST_GRID }}
                      >
                        <span className="font-bold text-lg tracking-t6 truncate">
                          {playlistLabels(m.playlist_id).name}
                        </span>
                        <RecordWithPct
                          wins={m.wins_as_teammate}
                          losses={m.losses_as_teammate}
                        />
                        <RecordWithPct
                          wins={m.wins_as_opponent}
                          losses={m.losses_as_opponent}
                        />
                        <span className={`text-right font-bold text-xl t-font-mono ${STAT_LABEL_CLASS.games}`}>
                          {m.total_matches}
                        </span>
                        <span className="text-right font-semibold text-sm tracking-t8 text-muted">
                          {formatAgo(m.last_played_at)}
                        </span>
                      </div>
                    );
                  })}
                </div>
              </div>
            ) : (
              <p className="font-semibold text-base tracking-t8 text-muted">
                NO RECORDED MATCHUPS
              </p>
            )}
          </div>

          {/* Paginated Match History */}
          {showHistory && (
            <PlayerMatchHistoryList
              playerId={d.id}
              playerName={d.name}
              matches={d.filteredAndSortedMatches}
              totalMatches={displayTotalMatches}
              isLoadingMatches={d.isLoadingMatches}
              matchPage={d.matchPage}
              totalPages={d.totalPages}
              setMatchPage={d.setMatchPage}
              roleFilter={d.roleFilter}
              setRoleFilter={d.setRoleFilter}
              resultFilter={d.resultFilter}
              setResultFilter={d.setResultFilter}
              searchQuery={d.searchQuery}
              setSearchQuery={d.setSearchQuery}
              sortOption={d.sortOption}
              setSortOption={d.setSortOption}
              localPlayerIds={localPlayerIds}
              variant={variant}
              onSelectPlayer={onSelectPlayer}
              onSelectTelemetry={(m) => {
                setTelemetryInitialTab('core');
                setTelemetryMatch(m);
              }}
              onWatchReplay={(m) => {
                setTelemetryInitialTab('replay');
                setTelemetryMatch(m);
              }}
            />
          )}

          {/* Profile Timestamps Footer */}
          {d.profile && (
            <div
              className={`flex justify-between gap-4 ${
                isDialog ? 'px-8 py-3.5' : 'px-4 py-3'
              } border-t border-line font-semibold text-sm tracking-t12 text-muted-2 mt-2`}
            >
              <span>FIRST SEEN {formatShortDate(d.profile.first_seen_at)}</span>
              <span className="inline-flex items-center gap-1.5">
                <PlatformBadge platform={d.platform} size="sm" /> · LAST SEEN {formatAgo(d.profile.last_seen_at)}
              </span>
            </div>
          )}
        </div>
      )}

      {telemetryMatch && (
        <HistoricalMatchDetailModal
          match={telemetryMatch}
          initialTab={telemetryInitialTab}
          onClose={() => setTelemetryMatch(null)}
          localPlayerIds={localPlayerIds}
          onPlayerClick={onSelectPlayer}
          inspectedPlayerId={d.id}
        />
      )}

      {d.profile && (
        <PlayerNotesModal
          isOpen={isNotesModalOpen}
          initialNotes={d.profile.notes || ''}
          initialTags={d.profile.tags || []}
          onClose={() => setIsNotesModalOpen(false)}
          onSave={d.saveNotes}
        />
      )}
    </>
  );
};
