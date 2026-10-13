import React, { useState, useMemo } from 'react';
import { useLiveMatch } from '../../hooks/useLiveMatch';
import { useColumnConfig } from '../../hooks/useColumnConfig';
import { ScoreboardBanner } from './ScoreboardBanner';
import { RosterTable } from './RosterTable';
import { IdleStateView } from './IdleStateView';
import { ColumnConfigModal } from './ColumnConfigModal';
import { MatchDetailModal } from '../session/MatchDetailModal';
import { SessionResponse, LobbyPlayer, SessionMatchDetail } from '../../types/api';
import { isDemoMode, SAMPLE_CURRENT_MATCH } from '../../utils/sampleData';

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
  const [selectedMatch, setSelectedMatch] = useState<SessionMatchDetail | null>(null);

  // Fallback to sample match in preview / demo mode if no real match is active
  const activeMatch = useMemo(() => {
    if (match && match.active_match) return match;
    if (isDemoMode()) return SAMPLE_CURRENT_MATCH;
    return match;
  }, [match]);

  // Group players by team (0 = Blue, 1 = Orange)
  const { bluePlayers, orangePlayers } = useMemo(() => {
    if (!activeMatch || !activeMatch.active_match) {
      return { bluePlayers: [], orangePlayers: [] };
    }

    const all: LobbyPlayer[] = [];
    if (activeMatch.local_player) all.push(activeMatch.local_player);
    if (activeMatch.teammates) all.push(...activeMatch.teammates);
    if (activeMatch.opponents) all.push(...activeMatch.opponents);
    if (activeMatch.spectators) all.push(...activeMatch.spectators);

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
  }, [activeMatch]);

  // Current playlist MMR delta from session
  const playlistDelta = useMemo(() => {
    if (!session || !activeMatch?.playlist_id) return undefined;
    const stats = session.playlists[activeMatch.playlist_id.toString()] || session.playlists[activeMatch.playlist_id];
    return stats ? stats.mmr_delta : undefined;
  }, [session, activeMatch]);

  // If outside a match, display IdleStateView
  if (!activeMatch || !activeMatch.active_match) {
    return (
      <div className="py-2">
        <IdleStateView session={session} onSelectMatch={setSelectedMatch} />
        <MatchDetailModal match={selectedMatch} onClose={() => setSelectedMatch(null)} />
      </div>
    );
  }

  return (
    <div className="py-1 space-y-3">
      {/* Demo Mode Notice */}
      {isDemoMode() && (!match || !match.active_match) && (
        <div className="flex items-center justify-between px-4 py-2 rounded-xl bg-cyan-950/40 border border-cyan-500/30 text-xs text-cyan-300">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 rounded-full bg-cyan-400 animate-pulse" />
            <span className="font-semibold tracking-wide">SIMULATED PREVIEW / DEMO MODE ACTIVE</span>
            <span className="text-cyan-400/70">— Showing simulated Grand Champion doubles match telemetry.</span>
          </div>
          <a href={window.location.pathname} className="text-cyan-400 hover:text-white underline font-mono text-xs">
            Exit Preview
          </a>
        </div>
      )}

      {/* Stadium Scoreboard Banner */}
      <ScoreboardBanner
        match={activeMatch}
        playlistMmrDelta={playlistDelta}
        onOpenColumnConfig={() => setIsModalOpen(true)}
      />

      {/* Dual Team Rosters (Blue on Left/Top, Orange on Right/Bottom) */}
      <div className="grid grid-cols-1 gap-4">
        <RosterTable
          teamNum={0}
          teamName="Blue Team"
          players={bluePlayers}
          localPlayerId={activeMatch.local_player?.player_id}
          localTeamNum={activeMatch.local_team}
          columnConfig={config}
          activePlaylistId={activeMatch.playlist_id}
        />
        <RosterTable
          teamNum={1}
          teamName="Orange Team"
          players={orangePlayers}
          localPlayerId={activeMatch.local_player?.player_id}
          localTeamNum={activeMatch.local_team}
          columnConfig={config}
          activePlaylistId={activeMatch.playlist_id}
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
