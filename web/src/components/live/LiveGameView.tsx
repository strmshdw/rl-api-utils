import React, { useMemo, useState } from 'react';
import { useColumnConfig } from '../../hooks/useColumnConfig';
import { ConnectionStatus } from '../../hooks/useLiveMatch';
import { CurrentMatchResponse, LobbyPlayer, SessionMatchDetail, SessionResponse } from '../../types/api';
import { Scorebug } from './Scorebug';
import { LivePlayerCard, statLeaders } from './LivePlayerCard';
import { IdleStateView } from './IdleStateView';
import { ColumnConfigModal } from './ColumnConfigModal';
import { ContinueSessionBanner } from '../session/SessionView';
import { useDashboard } from '../../context/DashboardContext';
import { SAMPLE_CURRENT_MATCH } from '../../utils/sampleData';
import { PlayerProfileDialog } from '../players/PlayerProfileDialog';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { sessionMatchToHistoricalRecord } from '../historical/historyData';

interface LiveGameViewProps {
  match: CurrentMatchResponse | null;
  session: SessionResponse | null;
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

const PLAYLIST_SCOPES: ReadonlyArray<{ id: string; label: string }> = [
  { id: 'all', label: 'ALL' },
  { id: '11', label: '2V2' },
  { id: '13', label: '3V3' },
  { id: '10', label: '1V1' },
];

export const LiveGameView: React.FC<LiveGameViewProps> = ({
  match,
  session,
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
    liveModeOverride,
    showLiveHistData,
    setShowLiveHistData,
    dateRangeLabel,
  } = useDashboard();
  const [playlistScope, setPlaylistScope] = useState<string>('all');
  const [fallbackPlayer, setFallbackPlayer] = useState<{ id: string; name?: string } | null>(null);
  const [fallbackMatch, setFallbackMatch] = useState<SessionMatchDetail | null>(null);

  const handleInspectPlayer = (playerId: string, playerName?: string) => {
    if (onInspectPlayer) onInspectPlayer(playerId, playerName);
    else setFallbackPlayer({ id: playerId, name: playerName });
  };

  const handleOpenMatchTelemetry = (m: SessionMatchDetail) => {
    if (onOpenMatchTelemetry) onOpenMatchTelemetry(m);
    else setFallbackMatch(m);
  };

  const realDisplayable = !!match && (match.active_match || match.match_ended);
  const shouldShowMatch =
    liveModeOverride === 'match' ? true : liveModeOverride === 'idle' ? false : realDisplayable;

  const effectiveMatch = useMemo<CurrentMatchResponse | null>(() => {
    if (!shouldShowMatch) return null;
    if (realDisplayable && match) return match;
    return SAMPLE_CURRENT_MATCH;
  }, [shouldShowMatch, realDisplayable, match]);

  const lobby = useMemo(
    () => (effectiveMatch ? lobbyPlayers(effectiveMatch) : []),
    [effectiveMatch]
  );
  const leaders = useMemo(() => statLeaders(lobby), [lobby]);

  if (!shouldShowMatch || !effectiveMatch) {
    return (
      <>
        <IdleStateView
          session={session}
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
  const sessionRecord = {
    wins: session?.total_wins ?? 0,
    losses: session?.total_losses ?? 0,
    netMmr: session?.most_played_playlist?.net_mmr,
  };

  return (
    <div data-testid="live-match" className="flex flex-col gap-4">
      {session?.continuable_session && (
        <div>
          <ContinueSessionBanner
            continuableSession={session.continuable_session}
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

      {/* IN-MATCH Top Control Bar: Historical Data Toggle & Playlist Scope */}
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-1.5 surface-plaque border border-line t-radius font-display">
        <div className="flex items-center gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 t-bg-loss bg-loss animate-pulse" />
            <span className="t-font-display font-bold text-xs tracking-widest uppercase t-text-loss">
              印 · LIVE TELEMETRY STREAM
            </span>
          </div>
          <span className="t-text-faint">|</span>
          <span className="text-xs t-text-muted font-semibold uppercase">
            CLICK ANY PLAYER MONOLITH FOR QUICK SCOUTING SCROLL
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Hideable Historical Data Toggle */}
          <button
            type="button"
            id="btn-toggle-live-hist"
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

          <span className="text-xs t-font-display font-bold uppercase tracking-wider t-text-muted">
            SCOPE ({dateRangeLabel}):
          </span>
          <div className="flex t-border t-radius overflow-hidden">
            {PLAYLIST_SCOPES.map((sc, i) => (
              <button
                key={sc.id}
                type="button"
                onClick={() => setPlaylistScope(sc.id)}
                className={`live-pl-btn px-2 py-0.5 t-font-display font-bold text-xs uppercase tracking-wider ${
                  i > 0 ? 'border-l t-border' : ''
                } ${
                  playlistScope === sc.id
                    ? 't-bg-blue-ink t-text-on-accent'
                    : 't-bg-page t-text-muted hover:opacity-80'
                }`}
              >
                {sc.label}
              </button>
            ))}
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
                playlistScope={playlistScope}
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

