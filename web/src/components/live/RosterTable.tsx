import React from 'react';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { PlayerRow } from './PlayerRow';
import { Shield, Flame } from 'lucide-react';

interface RosterTableProps {
  teamNum: 0 | 1;
  teamName: string;
  players: LobbyPlayer[];
  localPlayerId?: string;
  localTeamNum?: number;
  columnConfig: ColumnConfig;
  activePlaylistId: number;
}

export const RosterTable: React.FC<RosterTableProps> = ({
  teamNum,
  teamName,
  players,
  localPlayerId,
  localTeamNum,
  columnConfig,
  activePlaylistId,
}) => {
  const isTeammateTeam = localTeamNum !== undefined ? teamNum === localTeamNum : teamNum === 0;
  const isBlue = teamNum === 0;
  const themeBorder = isBlue ? 'border-[#00a2ff]/30' : 'border-[#ff7b00]/30';
  const themeText = isBlue ? 'text-[#00a2ff]' : 'text-[#ff7b00]';
  const TeamIcon = isBlue ? Shield : Flame;

  // Sort players: Score DESC
  const sortedPlayers = [...players].sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0));

  // Calculate team total goals and score
  const teamGoals = players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  const teamScore = players.reduce((sum, p) => sum + (p.stats?.score || 0), 0);

  return (
    <div
      className={`rounded-2xl border ${themeBorder} bg-slate-900/70 overflow-hidden shadow-xl flex flex-col`}
      style={{
        contentVisibility: 'auto',
        containIntrinsicSize: 'auto 600px auto 350px',
      }}
    >
      {/* Team Header Banner */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-slate-800/40 border-b border-slate-800">
        <div className="flex items-center gap-2.5">
          <TeamIcon className={`w-5 h-5 ${themeText}`} />
          <h3 className={`font-bold text-sm uppercase tracking-wider ${themeText}`}>
            {teamName}
          </h3>
          <span className="text-xs text-slate-400 font-medium">({players.length})</span>
        </div>
        <div className="flex items-center gap-4 text-xs font-semibold text-slate-300">
          <span>Goals: <strong className="text-slate-100">{teamGoals}</strong></span>
          <span>Score: <strong className="text-slate-100">{teamScore}</strong></span>
        </div>
      </div>

      {/* Roster Table Content */}
      <div className="overflow-x-auto">
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 font-bold uppercase tracking-wider text-[11px] bg-slate-950/30">
              <th data-testid="th-player" className="py-2.5 px-4">Player</th>
              {columnConfig.score && <th data-testid="th-score" className="py-2.5 px-3 text-right">Score</th>}
              {columnConfig.goals && <th data-testid="th-goals" className="py-2.5 px-3 text-right">Goals</th>}
              {columnConfig.assists && <th data-testid="th-assists" className="py-2.5 px-3 text-right">Assists</th>}
              {columnConfig.saves && <th data-testid="th-saves" className="py-2.5 px-3 text-right">Saves</th>}
              {columnConfig.shots && <th data-testid="th-shots" className="py-2.5 px-3 text-right">Shots</th>}
              {columnConfig.demos && <th data-testid="th-demos" className="py-2.5 px-3 text-right">Demos</th>}
              {columnConfig.rank && <th data-testid="th-rank" className="py-2.5 px-3">Rank</th>}
              {columnConfig.mmr && <th data-testid="th-mmr" className="py-2.5 px-3 text-right">MMR</th>}
              {columnConfig.h2h && <th data-testid="th-h2h" className="py-2.5 px-4 text-right">H2H Record</th>}
              {columnConfig.platform && <th data-testid="th-platform" className="py-2.5 px-3">Platform</th>}
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800/60">
            {sortedPlayers.length === 0 ? (
              <tr>
                <td colSpan={12} className="py-8 text-center text-slate-500 italic">
                  No players currently loaded for this team.
                </td>
              </tr>
            ) : (
              sortedPlayers.map(player => (
                <PlayerRow
                  key={player.player_id}
                  player={player}
                  isLocal={player.is_local || player.player_id === localPlayerId}
                  isTeammate={isTeammateTeam}
                  columnConfig={columnConfig}
                  activePlaylistId={activePlaylistId}
                />
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
