import React, { useMemo } from 'react';
import { useColumnConfig } from '../../hooks/useColumnConfig';
import { ConnectionStatus } from '../../hooks/useLiveMatch';
import { CurrentMatchResponse, LobbyPlayer, SessionResponse } from '../../types/api';
import { Scorebug } from './Scorebug';
import { LivePlayerCard, statLeaders } from './LivePlayerCard';
import { IdleStateView } from './IdleStateView';
import { ColumnConfigModal } from './ColumnConfigModal';

interface LiveGameViewProps {
  match: CurrentMatchResponse | null;
  session: SessionResponse | null;
  connection: ConnectionStatus;
  columnsOpen: boolean;
  onCloseColumns: () => void;
  onShowSession: () => void;
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

export const LiveGameView: React.FC<LiveGameViewProps> = ({ match, session, connection, columnsOpen, onCloseColumns, onShowSession }) => {
  const { config, toggleColumn, applyPreset, resetToDefault, activePreset } = useColumnConfig();

  const lobby = useMemo(() => (match?.active_match ? lobbyPlayers(match) : []), [match]);
  const leaders = useMemo(() => statLeaders(lobby), [lobby]);

  if (!match || !match.active_match) {
    return <IdleStateView session={session} connection={connection} onShowSession={onShowSession} />;
  }

  const localId = match.local_player?.player_id;
  const localTeam = match.local_team ?? match.local_player?.team_num ?? 0;
  const teams = [0, 1].map((team) => lobby.filter((p) => p.team_num === team).sort(byScore));
  const sessionRecord = { wins: session?.total_wins ?? 0, losses: session?.total_losses ?? 0 };

  return (
    <div data-testid="live-match">
      <Scorebug match={match} bluePlayers={teams[0]} orangePlayers={teams[1]} localTeam={localTeam} />

      <div className="grid grid-cols-2 gap-7">
        {teams.map((players, team) => (
          <div key={team} data-testid={`team-${team === 0 ? 'blue' : 'orange'}`} className="flex flex-col gap-3 min-w-0">
            <div className={`h-1 ${team === 0 ? 'bg-blue-ink' : 'bg-orange-ink'}`} />
            {players.map((p) => (
              <LivePlayerCard
                key={p.player_id}
                player={p}
                isLocal={p.is_local || p.player_id === localId}
                isTeammate={team === localTeam}
                leaders={leaders}
                columns={config}
                sessionRecord={sessionRecord}
              />
            ))}
          </div>
        ))}
      </div>

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
