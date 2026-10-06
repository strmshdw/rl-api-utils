import React, { useState, useMemo } from 'react';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useColumnConfig } from '../../hooks/useColumnConfig';
import { ScoreboardBanner } from './ScoreboardBanner';
import { RosterTable } from './RosterTable';
import { IdleStateView } from './IdleStateView';
import { ColumnConfigModal } from './ColumnConfigModal';
import { SessionResponse, LobbyPlayer } from '../../types/api';

interface LiveGameViewProps {
  session: SessionResponse | null;
}

export const LiveGameView: React.FC<LiveGameViewProps> = ({ session }) => {
  const { match } = useLiveMatch();
  const {
    config,
    toggleColumn,
    applyPreset,
    resetToDefault,
    activePreset,
  } = useColumnConfig();

  const [isModalOpen, setIsModalOpen] = useState(false);

  // Group players by team (0 = Blue, 1 = Orange)
  const { bluePlayers, orangePlayers } = useMemo(() => {
    if (!match || !match.active_match) {
      return { bluePlayers: [], orangePlayers: [] };
    }

    const all: LobbyPlayer[] = [];
    if (match.local_player) all.push(match.local_player);
    if (match.teammates) all.push(...match.teammates);
    if (match.opponents) all.push(...match.opponents);
    if (match.spectators) all.push(...match.spectators);

    const seen = new Set<string>();
    const deduped = all.filter(p => {
      if (seen.has(p.player_id)) return false;
      seen.add(p.player_id);
      return true;
    });

    return {
      bluePlayers: deduped.filter(p => p.team_num === 0),
      orangePlayers: deduped.filter(p => p.team_num === 1),
    };
  }, [match]);

  // Current playlist MMR delta from session
  const playlistDelta = useMemo(() => {
    if (!session || !match?.playlist_id) return undefined;
    const stats = session.playlists[match.playlist_id.toString()] || session.playlists[match.playlist_id];
    return stats ? stats.mmr_delta : undefined;
  }, [session, match]);

  // If outside a match, display IdleStateView
  if (!match || !match.active_match) {
    return (
      <div className="py-2">
        <IdleStateView session={session} />
      </div>
    );
  }

  return (
    <div className="py-1 space-y-3">
      {/* Stadium Scoreboard Banner */}
      <ScoreboardBanner
        match={match}
        playlistMmrDelta={playlistDelta}
        onOpenColumnConfig={() => setIsModalOpen(true)}
      />

      {/* Dual Team Rosters (Blue on Left/Top, Orange on Right/Bottom) */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <RosterTable
          teamNum={0}
          teamName="Blue Team"
          players={bluePlayers}
          localPlayerId={match.local_player?.player_id}
          localTeamNum={match.local_team}
          columnConfig={config}
          activePlaylistId={match.playlist_id}
        />
        <RosterTable
          teamNum={1}
          teamName="Orange Team"
          players={orangePlayers}
          localPlayerId={match.local_player?.player_id}
          localTeamNum={match.local_team}
          columnConfig={config}
          activePlaylistId={match.playlist_id}
        />
      </div>

      {/* Column Configurator Modal */}
      <ColumnConfigModal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        config={config}
        toggleColumn={toggleColumn}
        applyPreset={applyPreset}
        resetToDefault={resetToDefault}
        activePreset={activePreset}
      />
    </div>
  );
};
