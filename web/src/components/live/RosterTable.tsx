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

  // Design Tokens & Theming (Esports Broadcast Matchup)
  const themeBorder = isBlue ? 'border-[#00a2ff]/40' : 'border-[#ff7b00]/40';
  const themeBg = isBlue
    ? 'bg-gradient-to-b from-[#00a2ff]/10 via-slate-900/90 to-slate-950/95'
    : 'bg-gradient-to-b from-[#ff7b00]/10 via-slate-900/90 to-slate-950/95';
  const themeShadow = isBlue
    ? 'shadow-[0_0_20px_rgba(0,162,255,0.2)]'
    : 'shadow-[0_0_20px_rgba(255,123,0,0.2)]';
  const ambientGlowGradient = isBlue
    ? 'from-[#00a2ff] to-[#00a2ff]/20'
    : 'from-[#ff7b00] to-[#ff7b00]/20';
  const themeText = isBlue ? 'text-[#00a2ff]' : 'text-[#ff7b00]';
  const themeGlowDrop = isBlue
    ? 'drop-shadow-[0_0_10px_rgba(0,162,255,0.4)]'
    : 'drop-shadow-[0_0_10px_rgba(255,123,0,0.4)]';
  const TeamIcon = isBlue ? Shield : Flame;

  // Sort players: Score DESC
  const sortedPlayers = [...players].sort((a, b) => (b.stats?.score || 0) - (a.stats?.score || 0));

  // Calculate team total goals, score, shots, and demos
  const teamGoals = players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  const teamScore = players.reduce((sum, p) => sum + (p.stats?.score || 0), 0);
  const teamShots = players.reduce((sum, p) => sum + (p.stats?.shots || 0), 0);
  const shotAccuracy = teamShots > 0 ? ((teamGoals / teamShots) * 100).toFixed(0) : '0';
  const teamDemos = players.reduce((sum, p) => sum + (p.stats?.demos || 0), 0);

  return (
    <div
      className={`rounded-2xl border ${themeBorder} ${themeBg} ${themeShadow} overflow-hidden shadow-xl flex flex-col`}
      style={{
        contentVisibility: 'auto',
        containIntrinsicSize: 'auto 600px auto 350px',
      }}
    >
      {/* Top ambient team glow stripe */}
      <div className={`h-1 w-full bg-gradient-to-r ${ambientGlowGradient}`} />

      {/* Glowing Team Header Banner */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-slate-800/40 border-b border-slate-800 backdrop-blur-sm">
        <div className="flex items-center gap-2.5">
          <TeamIcon className={`w-5 h-5 ${themeText} ${themeGlowDrop}`} />
          <h3 className={`font-bold text-sm uppercase tracking-wider ${themeText} ${themeGlowDrop}`}>
            {teamName}
          </h3>
          <span className="text-xs text-slate-400 font-medium">({players.length})</span>
        </div>
        <div className="flex items-center gap-2 sm:gap-3">
          {/* Team Shots / Conversion badge (responsive) */}
          {teamShots > 0 && (
            <div
              className="hidden sm:inline-flex items-center gap-1.5 px-2 py-1 rounded-lg bg-slate-900/60 border border-slate-700/60 text-slate-300 shadow-sm"
              title="Team shots and shot conversion accuracy"
            >
              <span className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Shots:</span>
              <strong className="text-xs font-black font-mono text-white">{teamShots}</strong>
              <span className="text-[10px] text-slate-400 font-mono">({shotAccuracy}%)</span>
            </div>
          )}
          {/* Team Demos badge (if > 0) */}
          {teamDemos > 0 && (
            <div
              className="hidden sm:inline-flex items-center gap-1.5 px-2 py-1 rounded-lg bg-rose-950/25 border border-rose-800/40 text-rose-300 shadow-sm"
              title="Team demolitions"
            >
              <span className="text-[10px] uppercase font-bold tracking-wider text-rose-400/80">Demos:</span>
              <strong className="text-xs font-black font-mono text-white">{teamDemos}</strong>
            </div>
          )}
          {/* Aggregated Goals Badge */}
          <div
            data-testid="team-goals"
            className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-amber-500/15 border border-amber-500/40 text-amber-300 shadow-[0_0_10px_rgba(245,158,11,0.25)]"
          >
            <span className="text-[10px] uppercase font-bold tracking-wider text-amber-400/80">Goals:</span>
            <strong className="text-xs font-black font-mono text-white">{teamGoals}</strong>
          </div>
          {/* Aggregated Score Badge */}
          <div
            data-testid="team-score"
            className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg ${isBlue
                ? 'bg-[#00a2ff]/15 border border-[#00a2ff]/40 text-[#00a2ff] shadow-[0_0_10px_rgba(0,162,255,0.25)]'
                : 'bg-[#ff7b00]/15 border border-[#ff7b00]/40 text-[#ff7b00] shadow-[0_0_10px_rgba(255,123,0,0.25)]'
              }`}
          >
            <span className="text-[10px] uppercase font-bold tracking-wider opacity-80">Score:</span>
            <strong className="text-xs font-black font-mono text-white">{teamScore}</strong>
          </div>
        </div>
      </div>

      {/* Roster Table Content */}
      <div className="overflow-x-auto">
        <table className="w-full text-left border-collapse text-xs">
          <thead>
            <tr className="border-b border-slate-800 text-slate-400 font-bold uppercase tracking-wider text-[11px] bg-slate-950/30">
              <th data-testid="th-player" className="py-2.5 px-4" style={{ width: "200px" }}>Player</th>
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
                  teamNum={teamNum}
                />
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
