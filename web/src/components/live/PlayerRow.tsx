import React from 'react';
import { LobbyPlayer } from '../../types/api';
import { ColumnConfig } from '../../types/columns';
import { RankBadge } from '../common/RankBadge';
import { H2HBadge } from '../common/H2HBadge';
import { Bot, Laptop, Gamepad2 } from 'lucide-react';

interface PlayerRowProps {
  player: LobbyPlayer;
  isLocal: boolean;
  isTeammate: boolean;
  columnConfig: ColumnConfig;
  activePlaylistId: number;
}

export const PlayerRow: React.FC<PlayerRowProps> = ({
  player,
  isLocal,
  isTeammate,
  columnConfig,
}) => {
  const stats = player.stats || { score: 0, goals: 0, assists: 0, saves: 0, shots: 0, demos: 0 };

  const getPlatformIcon = (platform: string) => {
    if (player.is_bot) return <Bot className="w-4 h-4 text-slate-400" />;
    switch (platform.toLowerCase()) {
      case 'steam':
        return <Laptop className="w-4 h-4 text-sky-400" />;
      case 'epic':
        return <Gamepad2 className="w-4 h-4 text-purple-400" />;
      case 'playstation':
        return <Gamepad2 className="w-4 h-4 text-blue-400" />;
      case 'xbox':
        return <Gamepad2 className="w-4 h-4 text-emerald-400" />;
      default:
        return <Gamepad2 className="w-4 h-4 text-slate-400" />;
    }
  };

  return (
    <tr
      className={`transition-colors hover:bg-slate-800/40 ${
        isLocal ? 'bg-cyan-500/10 font-medium' : ''
      }`}
    >
      {/* Player Identity Anchor */}
      <td className="py-2.5 px-4 flex items-center gap-2">
        {getPlatformIcon(player.platform)}
        <span
          className={`font-semibold truncate max-w-[150px] ${
            isLocal ? 'text-cyan-300' : 'text-slate-200'
          }`}
          title={player.name}
        >
          {player.name}
        </span>
        {isLocal && (
          <span className="px-1.5 py-0.5 rounded text-[10px] font-black bg-cyan-500/20 text-cyan-300 border border-cyan-500/40">
            YOU
          </span>
        )}
        {player.is_bot && (
          <span className="px-1.5 py-0.5 rounded text-[10px] font-semibold bg-slate-800 text-slate-400 border border-slate-700">
            BOT
          </span>
        )}
      </td>

      {/* Platform Column */}
      {columnConfig.platform && (
        <td className="py-2.5 px-3 text-slate-400 text-xs">
          {player.is_bot ? 'AI' : player.platform}
        </td>
      )}

      {/* Rank Column */}
      {columnConfig.rank && (
        <td className="py-2.5 px-3">
          <RankBadge
            rankName={player.current_rank?.rank_name || 'Unranked'}
            tier={player.current_rank?.tier ?? 0}
            division={player.current_rank?.division ?? 0}
          />
        </td>
      )}

      {/* MMR Column */}
      {columnConfig.mmr && (
        <td className="py-2.5 px-3 text-right font-mono text-slate-300">
          {player.current_rank?.mmr ? player.current_rank.mmr.toFixed(1) : '--'}
        </td>
      )}

      {/* Box Score Stats Columns */}
      {columnConfig.score && (
        <td className="py-2.5 px-3 text-right font-mono font-bold text-slate-100">
          {stats.score}
        </td>
      )}

      {columnConfig.goals && (
        <td
          className={`py-2.5 px-3 text-right font-mono font-bold ${
            stats.goals > 0 ? 'text-amber-400' : 'text-slate-400'
          }`}
        >
          {stats.goals}
        </td>
      )}

      {columnConfig.assists && (
        <td className="py-2.5 px-3 text-right font-mono text-slate-300">
          {stats.assists}
        </td>
      )}

      {columnConfig.saves && (
        <td className="py-2.5 px-3 text-right font-mono text-slate-300">
          {stats.saves}
        </td>
      )}

      {columnConfig.shots && (
        <td className="py-2.5 px-3 text-right font-mono text-slate-300">
          {stats.shots}
        </td>
      )}

      {columnConfig.demos && (
        <td
          className={`py-2.5 px-3 text-right font-mono ${
            stats.demos > 0 ? 'text-rose-400 font-bold' : 'text-slate-400'
          }`}
        >
          {stats.demos}
        </td>
      )}

      {/* Head-to-Head (H2H) Column */}
      {columnConfig.h2h && (
        <td className="py-2.5 px-4 text-right">
          <H2HBadge
            matchup={player.matchup_record}
            isTeammate={isTeammate}
          />
        </td>
      )}
    </tr>
  );
};
