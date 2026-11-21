import React, { useEffect, useMemo, useState } from 'react';
import { useColumnConfig } from '../../hooks/useColumnConfig';
import { ConnectionStatus } from '../../hooks/useLiveMatch';
import { useLiveLobbyHistories } from '../../hooks/useLiveLobbyHistories';
import { CurrentMatchResponse, LobbyPlayer, SessionMatchDetail, SessionResponse } from '../../types/api';
import { Scorebug } from './Scorebug';
import { LivePlayerCard, STRIP_STATS, statLeaders } from './LivePlayerCard';
import { IdleStateView } from './IdleStateView';
import { ColumnConfigModal } from './ColumnConfigModal';
import { ContinueSessionBanner } from '../session/SessionView';
import { AreaFilterBar } from '../common/AreaFilterBar';
import { useAreaFilters, useDashboard } from '../../context/DashboardContext';
import {
  filterSessionByPlaylist,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  normalizeActiveModeKey,
} from '../../utils/broadcast';
import { SAMPLE_CURRENT_MATCH } from '../../utils/sampleData';
import { PlayerProfileDialog } from '../players/PlayerProfileDialog';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { sessionMatchToHistoricalRecord } from '../historical/historyData';

interface LiveGameViewProps {
  match: CurrentMatchResponse | null;
  session: SessionResponse | null;
  scopedSession?: SessionResponse | null;
  activePlaylist?: string;
  onSelectPlaylist?: (playlist: string) => void;
  connection: ConnectionStatus;
  columnsOpen?: boolean;
  onOpenColumns?: () => void;
  onCloseColumns?: () => void;
  onShowSession: () => void;
  onContinueSession?: (sessionId?: string) => void;
  onDismissContinueSession?: (sessionId?: string) => void;
  onInspectPlayer?: (playerId: string, playerName?: string) => void;
  onOpenMatchTelemetry?: (match: SessionMatchDetail) => void;
}

/** Local player, teammates and opponents, de-duplicated; spectators are excluded. */
export function lobbyPlayers(match: CurrentMatchResponse): LobbyPlayer[] {
  const all: LobbyPlayer[] = [];
  if (match.local_player) all.push(match.local_player);
  if (match.teammates) all.push(...match.teammates);
  if (match.opponents) all.push(...match.opponents);
  const seen = new Set<string>();
  return all.filter((p) => {
    if (p.team_num !== 0 && p.team_num !== 1) return false;
    if (seen.has(p.player_id)) return false;
    seen.add(p.player_id);
    return true;
  });
}

const byScore = (a: LobbyPlayer, b: LobbyPlayer) => (b.stats?.score || 0) - (a.stats?.score || 0);

function resolveScopedPlaylistId(scope: string): number {
  const param = normalizeActiveModeKey(scope);
  const num = Number(param);
  return Number.isInteger(num) && num > 0 ? num : 0;
}

const LIVE_ACTION_BTN =
  'toolbar-btn border border-outline px-2.5 py-1 t-font-display font-bold text-xs uppercase tracking-wider text-muted hover:text-ink hover:border-ink transition-colors duration-120 whitespace-nowrap t-radius cursor-pointer surface-plaque';

export const LiveGameView: React.FC<LiveGameViewProps> = ({
  match,
  session,
  scopedSession,
  activePlaylist,
  onSelectPlaylist,
  connection,
  columnsOpen: controlledColumnsOpen,
  onOpenColumns,
  onCloseColumns,
  onShowSession,
  onContinueSession,
  onDismissContinueSession,
  onInspectPlayer,
  onOpenMatchTelemetry,
}) => {
  const { config, toggleColumn, applyPreset, resetToDefault, activePreset } = useColumnConfig();
  const {
    liveModeOverride,
    showLiveHistData,
    setShowLiveHistData,
    isDevBuild,
    previewMode,
  } = useDashboard();
  const liveFilters = useAreaFilters('live');
  const [localColumnsOpen, setLocalColumnsOpen] = useState(false);
  const [playlistScope, setPlaylistScope] = useState<string>(() =>
    normalizeActiveModeKey(
      activePlaylist ??
        (match?.playlist_id ? modeKeyFromPlaylistId(match.playlist_id) : liveFilters.playlist),
    ),
  );
  const [fallbackPlayer, setFallbackPlayer] = useState<{ id: string; name?: string } | null>(null);
  const [fallbackMatch, setFallbackMatch] = useState<SessionMatchDetail | null>(null);

  const effectiveColumnsOpen = controlledColumnsOpen ?? localColumnsOpen;
  const openColumnsModal = () => {
    if (onOpenColumns) onOpenColumns();
    else setLocalColumnsOpen(true);
  };
  const closeColumnsModal = () => {
    setLocalColumnsOpen(false);
    onCloseColumns?.();
  };

  useEffect(() => {
    if (activePlaylist !== undefined) {
      setPlaylistScope(normalizeActiveModeKey(activePlaylist));
    } else {
      setPlaylistScope(normalizeActiveModeKey(liveFilters.playlist));
    }
  }, [activePlaylist, liveFilters.playlist]);

  const handleScopeSelect = (nextScope: string) => {
    const normalized = normalizeActiveModeKey(nextScope);
    setPlaylistScope(normalized);
    onSelectPlaylist?.(normalized);
    liveFilters.setPlaylist(normalized);
  };

  const handleInspectPlayer = (playerId: string, playerName?: string) => {
    if (onInspectPlayer) onInspectPlayer(playerId, playerName);
    else setFallbackPlayer({ id: playerId, name: playerName });
  };

  const handleOpenMatchTelemetry = (m: SessionMatchDetail) => {
    if (onOpenMatchTelemetry) onOpenMatchTelemetry(m);
    else setFallbackMatch(m);
  };

  const realDisplayable = !!match && (match.active_match || match.match_ended);
  const canOverride = isDevBuild && previewMode;
  const shouldShowMatch =
    canOverride && liveModeOverride === 'match'
      ? true
      : canOverride && liveModeOverride === 'idle'
      ? false
      : realDisplayable;

  const effectiveMatch = useMemo<CurrentMatchResponse | null>(() => {
    if (!shouldShowMatch) return null;
    if (realDisplayable && match) return match;
    if (canOverride) return SAMPLE_CURRENT_MATCH;
    return null;
  }, [shouldShowMatch, realDisplayable, match, canOverride]);

  const lobby = useMemo(
    () => (effectiveMatch ? lobbyPlayers(effectiveMatch) : []),
    [effectiveMatch],
  );
  const leaders = useMemo(() => statLeaders(lobby), [lobby]);

  const normalizedScopeKey = normalizeActiveModeKey(playlistScope);
  const localId = effectiveMatch?.local_player?.player_id;
  const hasDateFilter = Boolean(liveFilters.dateRangeQueryParams.from || liveFilters.dateRangeQueryParams.to);

  const scopedHistories = useLiveLobbyHistories({
    matchGuid: effectiveMatch?.match_guid,
    lobby,
    localId,
    scopeKey: normalizedScopeKey,
    dateParams: liveFilters.dateRangeQueryParams,
    enabled: Boolean(effectiveMatch) && (showLiveHistData || hasDateFilter),
    previewMode: Boolean(isDevBuild && previewMode),
  });

  const scopedContinuableSession = useMemo(() => {
    if (!session?.continuable_session) return null;
    const scoped = filterSessionByPlaylist(session.continuable_session, normalizedScopeKey);
    return scoped && scoped.total_matches > 0 ? scoped : null;
  }, [session?.continuable_session, normalizedScopeKey]);

  const showLiveReset =
    liveFilters.dateRange !== 'all' ||
    liveFilters.showCustomCalendar ||
    normalizedScopeKey !== (effectiveMatch?.playlist_id ? modeKeyFromPlaylistId(effectiveMatch.playlist_id) : '11');

  if (!shouldShowMatch || !effectiveMatch) {
    return (
      <>
        <IdleStateView
          session={session}
          scopedSession={scopedSession}
          activePlaylist={activePlaylist ?? normalizedScopeKey}
          onSelectPlaylist={handleScopeSelect}
          connection={connection}
          onShowSession={onShowSession}
          onContinueSession={onContinueSession}
          onDismissContinueSession={onDismissContinueSession}
          onInspectPlayer={handleInspectPlayer}
          onOpenMatchTelemetry={handleOpenMatchTelemetry}
        />
        {fallbackPlayer && (
          <PlayerProfileDialog
            playerId={fallbackPlayer.id}
            playerName={fallbackPlayer.name}
            onClose={() => setFallbackPlayer(null)}
          />
        )}
        {fallbackMatch && (
          <HistoricalMatchDetailModal
            match={sessionMatchToHistoricalRecord(fallbackMatch)}
            onClose={() => setFallbackMatch(null)}
            onPlayerClick={handleInspectPlayer}
          />
        )}
      </>
    );
  }

  const localTeam = effectiveMatch.local_team ?? effectiveMatch.local_player?.team_num ?? 0;
  const teams = [0, 1].map((team) => lobby.filter((p) => p.team_num === team).sort(byScore));
  const opposingTeam = localTeam === 0 ? teams[1] : teams[0];

  const hasStatStrip = STRIP_STATS.some((s) => config[s.key]);
  const bandsPerCard = 2 + (hasStatStrip ? 1 : 0) + (showLiveHistData ? 1 : 0);
  const maxSlots = Math.max(1, teams[0].length, teams[1].length);
  const slotTrackDef = Array(bandsPerCard).fill('auto').join(' ');
  const rosterGridRows = Array.from({ length: maxSlots }, () => slotTrackDef).join(' 0.875rem ');
  const fallbackCardRows = `repeat(${bandsPerCard}, auto)`;

  const scopedPlaylistId = resolveScopedPlaylistId(normalizedScopeKey);
  const targetPlId = scopedPlaylistId > 0 ? scopedPlaylistId : effectiveMatch.playlist_id;
  const hasPlaylistBreakdown = !!session?.playlists && Object.keys(session.playlists).length > 0;
  const plSession = hasPlaylistBreakdown ? session?.playlists?.[String(targetPlId)] : undefined;
  const scopeMatchesMatch = matchesPlaylistFilter(effectiveMatch.playlist_id, normalizedScopeKey);

  const sessionRecord = hasPlaylistBreakdown
    ? scopedPlaylistId === 0 && !scopeMatchesMatch
      ? { wins: 0, losses: 0, netMmr: undefined }
      : {
          wins: plSession?.wins ?? 0,
          losses: plSession?.losses ?? 0,
          netMmr: plSession?.mmr_delta,
        }
    : scopeMatchesMatch
    ? {
        wins: session?.total_wins ?? 0,
        losses: session?.total_losses ?? 0,
        netMmr: session?.most_played_playlist?.net_mmr,
      }
    : { wins: 0, losses: 0, netMmr: undefined };

  return (
    <div data-testid="live-match" className="flex flex-col gap-4">
      {scopedContinuableSession && (
        <div>
          <ContinueSessionBanner
            continuableSession={scopedContinuableSession}
            onContinue={(id) => onContinueSession?.(id)}
            onDismiss={(id) => onDismissContinueSession?.(id)}
          />
        </div>
      )}

      {effectiveMatch.match_ended && (
        <div
          data-testid="match-ended-banner"
          className="t-bg-panel bg-panel border-l-4 border-blue-ink px-6 py-3 flex flex-wrap items-center justify-between gap-3 t-border t-radius"
        >
          <div className="flex items-center gap-3 font-display flex-wrap">
            <span className="font-extrabold text-base tracking-wider text-ink uppercase">
              MATCH CONCLUDED · FINAL BOX SCORE
            </span>
            {effectiveMatch.result === 'victory' && (
              <span className="bg-win/20 text-win border border-win/30 text-sm font-bold px-2.5 py-0.5 rounded tracking-wider uppercase">
                VICTORY
              </span>
            )}
            {effectiveMatch.result === 'defeat' && (
              <span className="bg-loss/20 text-loss border border-loss/30 text-sm font-bold px-2.5 py-0.5 rounded tracking-wider uppercase">
                DEFEAT
              </span>
            )}
          </div>
          <button
            type="button"
            onClick={onShowSession}
            className="text-muted hover:text-ink text-sm font-bold tracking-wider uppercase transition-colors"
          >
            VIEW SESSION →
          </button>
        </div>
      )}

      {/* IN-MATCH Local Telemetry & Filter Bar */}
      <AreaFilterBar
        area="live"
        ariaLabel="Live match filters"
        playlist={normalizedScopeKey}
        onPlaylistChange={handleScopeSelect}
        dateRange={liveFilters.dateRange}
        onDateRangeChange={liveFilters.setDateRange}
        customFrom={liveFilters.customFrom}
        onCustomFromChange={liveFilters.setCustomFrom}
        customTo={liveFilters.customTo}
        onCustomToChange={liveFilters.setCustomTo}
        showCustomCalendar={liveFilters.showCustomCalendar}
        onToggleCustomCalendar={() => {
          if (!liveFilters.showCustomCalendar && liveFilters.dateRange !== 'custom') {
            liveFilters.setDateRange('custom');
          } else {
            liveFilters.setShowCustomCalendar((prev) => !prev);
          }
        }}
        statMode={liveFilters.statMode}
        onStatModeChange={liveFilters.setStatMode}
        showReset={showLiveReset}
        onReset={() => {
          const matchMode = effectiveMatch.playlist_id
            ? modeKeyFromPlaylistId(effectiveMatch.playlist_id)
            : '11';
          liveFilters.resetFilters();
          handleScopeSelect(matchMode);
        }}
        leadingSlot={
          <div className="flex items-center gap-1.5 pr-1">
            <span className="w-2 h-2 t-bg-loss bg-loss animate-pulse" />
            <span className="t-font-display font-bold text-xs tracking-widest uppercase t-text-loss whitespace-nowrap">
              LIVE
            </span>
          </div>
        }
        trailingSlot={
          <div role="group" aria-label="Live match controls" className="flex flex-wrap items-center gap-1.5">
            <button
              type="button"
              data-testid="live-columns-btn"
              onClick={openColumnsModal}
              className={LIVE_ACTION_BTN}
            >
              COLUMNS
            </button>
            <button
              type="button"
              id="btn-toggle-live-hist"
              aria-pressed={showLiveHistData}
              onClick={() => setShowLiveHistData((prev) => !prev)}
              className="toolbar-btn px-2.5 py-1 t-border t-radius t-font-display font-bold text-xs uppercase tracking-wider transition-colors cursor-pointer surface-plaque t-text-muted hover:text-ink whitespace-nowrap"
              title="Show or hide historical WITH YOU / AGAINST YOU split tables on live player cards"
            >
              {showLiveHistData ? 'HISTORICAL DATA: SHOWN' : 'HISTORICAL DATA: HIDDEN'}
            </button>
          </div>
        }
      />

      <Scorebug
        match={effectiveMatch}
        bluePlayers={teams[0]}
        orangePlayers={teams[1]}
        localTeam={localTeam}
        scopedPlaylistId={scopedPlaylistId}
        scopeKey={normalizedScopeKey}
      />

      <div
        className="live-roster-grid grid grid-cols-1 lg:grid-cols-2 gap-x-5 gap-y-3.5"
        style={{ '--live-roster-rows': rosterGridRows } as React.CSSProperties}
      >
        {teams.map((players, team) => (
          <div
            key={team}
            data-testid={`team-${team === 0 ? 'blue' : 'orange'}`}
            className="flex flex-col gap-3.5 min-w-0 lg:contents"
          >
            {players.map((p, slotIndex) => {
              const rowStart = 1 + slotIndex * (bandsPerCard + 1);
              return (
                <LivePlayerCard
                  key={p.player_id}
                  player={p}
                  isLocal={p.is_local || p.player_id === localId}
                  isTeammate={team === localTeam}
                  leaders={leaders}
                  columns={config}
                  sessionRecord={sessionRecord}
                  localPlayer={effectiveMatch.local_player}
                  opposingTeamPlayers={opposingTeam}
                  activePlaylistId={effectiveMatch.playlist_id}
                  playlistScope={normalizedScopeKey}
                  statMode={liveFilters.statMode}
                  scopedHistory={scopedHistories[p.player_id]}
                  subgridPlacement={{
                    col: team + 1,
                    row: `${rowStart} / span ${bandsPerCard}`,
                    fallbackRows: fallbackCardRows,
                  }}
                  onInspectPlayer={handleInspectPlayer}
                />
              );
            })}
          </div>
        ))}
      </div>

      {fallbackPlayer && (
        <PlayerProfileDialog
          playerId={fallbackPlayer.id}
          playerName={fallbackPlayer.name}
          onClose={() => setFallbackPlayer(null)}
        />
      )}

      <ColumnConfigModal
        isOpen={effectiveColumnsOpen}
        onClose={closeColumnsModal}
        config={config}
        toggleColumn={toggleColumn}
        applyPreset={applyPreset}
        resetToDefault={resetToDefault}
        activePreset={activePreset}
      />
    </div>
  );
};
