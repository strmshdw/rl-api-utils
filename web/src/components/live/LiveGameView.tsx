import React, { useEffect, useMemo, useState } from 'react';
import { useColumnConfig } from '../../hooks/useColumnConfig';
import { ConnectionStatus } from '../../hooks/useLiveMatch';
import { CurrentMatchResponse, LobbyPlayer, SessionMatchDetail, SessionResponse } from '../../types/api';
import { Scorebug } from './Scorebug';
import { LivePlayerCard, statLeaders } from './LivePlayerCard';
import { IdleStateView } from './IdleStateView';
import { ColumnConfigModal } from './ColumnConfigModal';
import { ContinueSessionBanner } from '../session/SessionView';
import { useDashboard } from '../../context/DashboardContext';
import {
  filterSessionByPlaylist,
  matchesPlaylistFilter,
  modeKeyFromPlaylistId,
  normalizeActiveModeKey,
  playlistLabels,
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
  hideHeaderHistToggle?: boolean;
  connection: ConnectionStatus;
  columnsOpen: boolean;
  onCloseColumns: () => void;
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

const BASE_PLAYLIST_SCOPES: ReadonlyArray<{ id: string; label: string }> = [
  { id: '11', label: '2V2' },
  { id: '13', label: '3V3' },
  { id: '10', label: '1V1' },
];

function scopeLabelForModeKey(modeKey: string, playlistId?: number): string {
  if (modeKey === 'casual') return 'CASUAL';
  if (modeKey === 'private') return 'PRIVATE';
  const pid = Number(modeKey) || playlistId || 0;
  return pid > 0 ? playlistLabels(pid).short : modeKey.toUpperCase();
}

function resolveScopedPlaylistId(scope: string): number {
  const param = normalizeActiveModeKey(scope);
  const num = Number(param);
  return Number.isInteger(num) && num > 0 ? num : 0;
}

export const LiveGameView: React.FC<LiveGameViewProps> = ({
  match,
  session,
  scopedSession,
  activePlaylist,
  onSelectPlaylist,
  hideHeaderHistToggle = false,
  connection,
  columnsOpen,
  onCloseColumns,
  onShowSession,
  onContinueSession,
  onDismissContinueSession,
  onInspectPlayer,
  onOpenMatchTelemetry,
}) => {
  const { config, toggleColumn, applyPreset, resetToDefault, activePreset } = useColumnConfig();
  const {
    activePlaylist: contextActivePlaylist,
    setActivePlaylist,
    liveModeOverride,
    showLiveHistData,
    setShowLiveHistData,
    dateRangeLabel,
    isDevBuild,
    previewMode,
  } = useDashboard();
  const [playlistScope, setPlaylistScope] = useState<string>(() =>
    normalizeActiveModeKey(
      activePlaylist ??
        (match?.playlist_id ? modeKeyFromPlaylistId(match.playlist_id) : contextActivePlaylist),
    ),
  );
  const [fallbackPlayer, setFallbackPlayer] = useState<{ id: string; name?: string } | null>(null);
  const [fallbackMatch, setFallbackMatch] = useState<SessionMatchDetail | null>(null);

  useEffect(() => {
    if (activePlaylist !== undefined) {
      setPlaylistScope(normalizeActiveModeKey(activePlaylist));
    }
  }, [activePlaylist]);

  const handleScopeSelect = (nextScope: string) => {
    const normalized = normalizeActiveModeKey(nextScope);
    setPlaylistScope(normalized);
    onSelectPlaylist?.(normalized);
    setActivePlaylist(normalized);
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

  const scopedContinuableSession = useMemo(() => {
    if (!session?.continuable_session) return null;
    const scoped = filterSessionByPlaylist(session.continuable_session, normalizedScopeKey);
    return scoped && scoped.total_matches > 0 ? scoped : null;
  }, [session?.continuable_session, normalizedScopeKey]);

  const playlistScopes = useMemo(() => {
    const items = [...BASE_PLAYLIST_SCOPES];
    const matchModeKey = effectiveMatch?.playlist_id
      ? modeKeyFromPlaylistId(effectiveMatch.playlist_id)
      : '';
    for (const candidate of [matchModeKey, normalizedScopeKey]) {
      if (candidate && !items.some((s) => s.id === candidate)) {
        items.push({
          id: candidate,
          label: scopeLabelForModeKey(candidate, effectiveMatch?.playlist_id),
        });
      }
    }
    return items;
  }, [effectiveMatch?.playlist_id, normalizedScopeKey]);

  if (!shouldShowMatch || !effectiveMatch) {
    return (
      <>
        <IdleStateView
          session={session}
          scopedSession={scopedSession}
          activePlaylist={activePlaylist ?? normalizedScopeKey}
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

  const localId = effectiveMatch.local_player?.player_id;
  const localTeam = effectiveMatch.local_team ?? effectiveMatch.local_player?.team_num ?? 0;
  const teams = [0, 1].map((team) => lobby.filter((p) => p.team_num === team).sort(byScore));
  const opposingTeam = localTeam === 0 ? teams[1] : teams[0];

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
          className="t-bg-panel bg-panel border-l-4 border-blue-ink px-6 py-3 flex items-center justify-between t-border t-radius"
        >
          <div className="flex items-center gap-3 font-display">
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

      {/* IN-MATCH Top Telemetry Strip */}
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-1.5 surface-plaque border border-line t-radius font-display">
        <div className="flex items-center gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 t-bg-loss bg-loss animate-pulse" />
            <span className="t-font-display font-bold text-xs tracking-widest uppercase t-text-loss">
              LIVE TELEMETRY STREAM
            </span>
          </div>
          <span className="t-text-faint">|</span>
          <span className="text-xs t-text-muted font-semibold uppercase">
            CLICK ANY PLAYER CARD FOR QUICK SCOUTING PROFILE
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {!hideHeaderHistToggle && (
            <button
              type="button"
              id="btn-toggle-live-hist"
              aria-pressed={showLiveHistData}
              onClick={() => setShowLiveHistData((prev) => !prev)}
              className={`px-2.5 py-0.5 t-border t-radius t-font-display font-bold text-xs uppercase tracking-wider transition-colors ${
                showLiveHistData
                  ? 't-bg-page t-text-strong border-outline'
                  : 't-bg-page t-text-muted'
              }`}
              title="Show or hide historical WITH YOU / AGAINST YOU split tables on live player cards"
            >
              {showLiveHistData
                ? 'HISTORICAL DATA: SHOWN (CLICK TO HIDE)'
                : 'HISTORICAL DATA: HIDDEN (CLICK TO SHOW)'}
            </button>
          )}

          <span className="text-xs t-font-display font-bold uppercase tracking-wider t-text-muted">
            SCOPE ({dateRangeLabel}):
          </span>
          <div className="flex t-border t-radius overflow-hidden" role="group" aria-label="Live player card playlist scope">
            {playlistScopes.map((sc, i) => {
              const active = normalizedScopeKey === sc.id;
              return (
                <button
                  key={sc.id}
                  type="button"
                  aria-pressed={active}
                  onClick={() => handleScopeSelect(sc.id)}
                  className={`live-pl-btn px-2 py-0.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                    i > 0 ? 'border-l t-border' : ''
                  } ${
                    active
                      ? 't-bg-blue-ink t-text-on-accent'
                      : 't-bg-page t-text-muted hover:opacity-80'
                  }`}
                >
                  {sc.label}
                </button>
              );
            })}
          </div>
        </div>
      </div>

      <Scorebug
        match={effectiveMatch}
        bluePlayers={teams[0]}
        orangePlayers={teams[1]}
        localTeam={localTeam}
      />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
        {teams.map((players, team) => (
          <div
            key={team}
            data-testid={`team-${team === 0 ? 'blue' : 'orange'}`}
            className="flex flex-col gap-3.5 min-w-0"
          >
            {players.map((p) => (
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
                onInspectPlayer={handleInspectPlayer}
              />
            ))}
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
        isOpen={columnsOpen}
        onClose={onCloseColumns}
        config={config}
        toggleColumn={toggleColumn}
        applyPreset={applyPreset}
        resetToDefault={resetToDefault}
        activePreset={activePreset}
      />
    </div>
  );
};
