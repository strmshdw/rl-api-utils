import React from 'react';
import { SessionMatchDetail, SessionMatchPlayer } from '../../types/api';
import { Modal } from '../common/Modal';
import { formatDuration, formatMMR, formatMMRDelta } from '../../utils/formatters';
import { parsePlatform } from '../../utils/platforms';
import { Copy, Check, Users } from 'lucide-react';

interface MatchDetailModalProps {
  match: SessionMatchDetail | null;
  onClose: () => void;
}

export const MatchDetailModal: React.FC<MatchDetailModalProps> = ({ match, onClose }) => {
  const [copied, setCopied] = React.useState(false);

  if (!match) return null;

  const handleCopyGUID = () => {
    navigator.clipboard.writeText(match.match_guid);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const bluePlayers = match.players.filter((p) => p.team_num === 0);
  const orangePlayers = match.players.filter((p) => p.team_num === 1);

  const renderRosterTable = (title: string, players: SessionMatchPlayer[], isBlue: boolean) => {
    const headerBorder = isBlue ? 'border-[#00a2ff]/40 text-[#00a2ff]' : 'border-[#ff7b00]/40 text-[#ff7b00]';
    const headerBg = isBlue ? 'bg-[#00a2ff]/10' : 'bg-[#ff7b00]/10';

    return (
      <div className={`rounded-xl border ${isBlue ? 'border-[#00a2ff]/30' : 'border-[#ff7b00]/30'} overflow-hidden mb-6`}>
        <div className={`px-4 py-2.5 flex items-center justify-between border-b ${headerBorder} ${headerBg}`}>
          <span className="font-bold text-sm tracking-wide flex items-center gap-2">
            <Users className="w-4 h-4" />
            {title} ({players.length})
          </span>
          <span className="text-xl font-black">
            {isBlue ? match.blue_score : match.orange_score}
          </span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="bg-slate-950/80 text-slate-400 uppercase tracking-wider border-b border-slate-800">
              <tr>
                <th className="py-2.5 px-4">Player</th>
                <th className="py-2.5 px-2 text-right">Score</th>
                <th className="py-2.5 px-2 text-right">Goals</th>
                <th className="py-2.5 px-2 text-right">Assists</th>
                <th className="py-2.5 px-2 text-right">Saves</th>
                <th className="py-2.5 px-2 text-right">Shots</th>
                <th className="py-2.5 px-2 text-right">Demos</th>
                <th className="py-2.5 px-3 text-right">MMR</th>
                <th className="py-2.5 px-3">H2H Record</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60 bg-slate-900/50">
              {players.map((p) => {
                const plat = parsePlatform(p.platform || p.player_id);
                const isLocalTeam = match.local_team !== undefined && p.team_num === match.local_team;
                const h2h = p.matchup_record;

                return (
                  <tr
                    key={p.player_id}
                    className={`hover:bg-slate-800/50 transition-colors ${
                      p.is_local ? 'bg-cyan-950/25 font-semibold text-white' : 'text-slate-200'
                    }`}
                  >
                    <td className="py-2.5 px-4 flex items-center gap-2">
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded border ${plat.badgeClass}`}>
                        {plat.name}
                      </span>
                      <span className="truncate max-w-[150px]">{p.name}</span>
                      {p.is_local && (
                        <span className="px-1.5 py-0.2 text-[9px] font-black uppercase rounded bg-cyan-500 text-slate-950">
                          YOU
                        </span>
                      )}
                    </td>
                    <td className="py-2.5 px-2 text-right font-bold text-white">{p.stats?.score ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-emerald-400">{p.stats?.goals ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-sky-400">{p.stats?.assists ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-indigo-400">{p.stats?.saves ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-yellow-400">{p.stats?.shots ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-rose-400">{p.stats?.demos ?? 0}</td>
                    <td className="py-2.5 px-3 text-right font-mono text-slate-300">
                      {formatMMR(p.mmr)}
                    </td>
                    <td className="py-2.5 px-3">
                      {h2h && !p.is_local ? (
                        isLocalTeam ? (
                          <span className="text-[11px] text-emerald-300">
                            {h2h.wins_as_teammate}W - {h2h.losses_as_teammate}L (mate)
                          </span>
                        ) : (
                          <span className="text-[11px] text-amber-300">
                            {h2h.wins_as_opponent}W - {h2h.losses_as_opponent}L (opp)
                          </span>
                        )
                      ) : (
                        <span className="text-slate-500 text-[11px]">---</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    );
  };

  return (
    <Modal isOpen={Boolean(match)} onClose={onClose} title="Match Summary & Box Scores" maxWidth="max-w-4xl">
      {/* Overview Banner */}
      <div className="rounded-xl border border-slate-800 bg-slate-950/70 p-4 mb-6">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 mb-1">
              <span className="text-lg font-bold text-white">{match.playlist_name}</span>
              <span
                className={`text-xs font-black uppercase px-2 py-0.5 rounded border ${
                  match.result === 'victory'
                    ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40'
                    : match.result === 'defeat'
                    ? 'bg-rose-500/20 text-rose-400 border-rose-500/40'
                    : 'bg-slate-700/30 text-slate-300 border-slate-600'
                }`}
              >
                {match.result || 'concluded'}
              </span>
            </div>
            <div className="flex items-center gap-3 text-xs text-slate-400">
              <span>Duration: {formatDuration(match.duration_seconds)}</span>
              <span>•</span>
              <span>{new Date(match.ended_at).toLocaleString()}</span>
            </div>
          </div>

          <div className="flex items-center gap-6">
            <div className="flex items-center gap-3 bg-slate-900 border border-slate-800 px-4 py-2 rounded-xl">
              <span className="text-2xl font-black text-[#00a2ff]">{match.blue_score}</span>
              <span className="text-slate-500 font-bold">:</span>
              <span className="text-2xl font-black text-[#ff7b00]">{match.orange_score}</span>
            </div>
            <div className="text-right">
              <div className="text-xs text-slate-400 font-medium">MMR Change</div>
              <div
                className={`text-base font-bold ${
                  match.mmr_change > 0
                    ? 'text-emerald-400'
                    : match.mmr_change < 0
                    ? 'text-rose-400'
                    : 'text-slate-400'
                }`}
              >
                {formatMMRDelta(match.mmr_change)}
              </div>
            </div>
          </div>
        </div>

        {/* Match GUID Copy Pill */}
        <div className="mt-3 pt-3 border-t border-slate-800/80 flex items-center justify-between text-xs text-slate-500">
          <span className="font-mono truncate max-w-sm">GUID: {match.match_guid}</span>
          <button
            onClick={handleCopyGUID}
            className="flex items-center gap-1 text-slate-400 hover:text-cyan-300 transition-colors"
          >
            {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
            {copied ? 'Copied!' : 'Copy GUID'}
          </button>
        </div>
      </div>

      {/* Roster Tables */}
      {renderRosterTable('Blue Team', bluePlayers, true)}
      {renderRosterTable('Orange Team', orangePlayers, false)}
    </Modal>
  );
};
