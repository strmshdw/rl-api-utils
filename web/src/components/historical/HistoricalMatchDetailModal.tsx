import React, { useState } from 'react';
import { HistoricalMatchRecord, ReplayPlayerStats } from '../../types/api';
import { Modal } from '../common/Modal';
import { formatDuration, formatPlaylistName, formatRelativeTime } from '../../utils/formatters';
import { parsePlatform } from '../../utils/platforms';
import { Copy, Check, Users, ExternalLink, Trophy, ShieldAlert, Award } from 'lucide-react';

interface HistoricalMatchDetailModalProps {
  match: HistoricalMatchRecord | null;
  onClose: () => void;
}

export const HistoricalMatchDetailModal: React.FC<HistoricalMatchDetailModalProps> = ({ match, onClose }) => {
  const [copied, setCopied] = useState(false);

  if (!match) return null;

  const handleCopyGUID = () => {
    navigator.clipboard.writeText(match.match_guid);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const bluePlayers = match.stats?.players?.filter((p) => p.team === 0) || [];
  const orangePlayers = match.stats?.players?.filter((p) => p.team === 1) || [];

  const result = match.result || (
    match.blue_score > match.orange_score ? 'victory' :
    match.orange_score > match.blue_score ? 'defeat' :
    match.blue_score === match.orange_score && match.blue_score > 0 ? 'draw' : ''
  );

  const isVictory = result === 'victory';
  const isDefeat = result === 'defeat';

  // Find MVP (highest score)
  const allPlayers = match.stats?.players || [];
  const mvp = allPlayers.length > 0
    ? allPlayers.reduce((top, p) => (p.score > (top?.score || 0) ? p : top), allPlayers[0])
    : null;

  const ballchasingURL = match.ballchasing_url || match.stats?.ballchasing_url;

  const renderRoster = (title: string, players: ReplayPlayerStats[], isBlue: boolean) => {
    const borderColor = isBlue ? 'border-[#00a2ff]/30' : 'border-[#ff7b00]/30';
    const headerBg = isBlue ? 'bg-[#00a2ff]/10 text-[#00a2ff]' : 'bg-[#ff7b00]/10 text-[#ff7b00]';

    return (
      <div className={`rounded-xl border ${borderColor} overflow-hidden mb-5`}>
        <div className={`px-4 py-2.5 flex items-center justify-between font-bold text-sm ${headerBg}`}>
          <div className="flex items-center gap-2">
            <Users className="w-4 h-4" />
            <span>{title}</span>
          </div>
          <span className="font-mono text-xs">
            {players.length} {players.length === 1 ? 'Player' : 'Players'}
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="bg-slate-950/80 text-[10px] text-slate-400 uppercase tracking-wider border-b border-slate-800">
              <tr>
                <th className="py-2.5 px-3">Player</th>
                <th className="py-2.5 px-2 text-center">Platform</th>
                <th className="py-2.5 px-2 text-right">Score</th>
                <th className="py-2.5 px-2 text-right">Goals</th>
                <th className="py-2.5 px-2 text-right">Assists</th>
                <th className="py-2.5 px-2 text-right">Saves</th>
                <th className="py-2.5 px-2 text-right">Shots</th>
                <th className="py-2.5 px-2 text-right">Demos</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60 bg-slate-900/40">
              {players.map((p, idx) => {
                const isMvp = mvp && p.name === mvp.name && p.score === mvp.score;
                const platformInfo = parsePlatform(p.platform);

                return (
                  <tr key={`${p.online_id || p.name}-${idx}`} className="hover:bg-slate-800/40 transition-colors">
                    <td className="py-2.5 px-3">
                      <div className="flex items-center gap-2 font-bold text-slate-200">
                        <span>{p.name}</span>
                        {isMvp && (
                          <span className="inline-flex items-center gap-0.5 px-1.5 py-0.2 rounded bg-amber-500/20 text-amber-300 text-[10px] font-bold border border-amber-500/30">
                            <Award className="w-3 h-3" /> MVP
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="py-2.5 px-2 text-center">
                      <span className="text-[11px] font-semibold text-slate-400">
                        {platformInfo.name}
                      </span>
                    </td>
                    <td className="py-2.5 px-2 text-right font-mono font-bold text-slate-200">{p.score}</td>
                    <td className="py-2.5 px-2 text-right font-mono text-slate-300">{p.goals}</td>
                    <td className="py-2.5 px-2 text-right font-mono text-slate-300">{p.assists}</td>
                    <td className="py-2.5 px-2 text-right font-mono text-slate-300">{p.saves}</td>
                    <td className="py-2.5 px-2 text-right font-mono text-slate-300">{p.shots}</td>
                    <td className="py-2.5 px-2 text-right font-mono text-slate-300">{p.demos}</td>
                  </tr>
                );
              })}
              {players.length === 0 && (
                <tr>
                  <td colSpan={8} className="py-4 text-center text-slate-500 italic">
                    No player stats recorded for this team
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    );
  };

  return (
    <Modal isOpen={!!match} onClose={onClose} title="Historical Match Details">
      <div className="space-y-6">
        {/* Match Result & Score Banner */}
        <div className="flex flex-col sm:flex-row items-center justify-between p-4 rounded-xl bg-slate-950/70 border border-slate-800 gap-4">
          <div className="flex items-center gap-3">
            <div
              className={`flex items-center justify-center w-12 h-12 rounded-xl font-black text-sm uppercase tracking-wider border ${
                isVictory
                  ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40 shadow-[0_0_15px_rgba(16,185,129,0.2)]'
                  : isDefeat
                  ? 'bg-rose-500/20 text-rose-400 border-rose-500/40 shadow-[0_0_15px_rgba(244,63,94,0.2)]'
                  : 'bg-slate-800 text-slate-300 border-slate-700'
              }`}
            >
              {isVictory ? <Trophy className="w-6 h-6" /> : isDefeat ? <ShieldAlert className="w-6 h-6" /> : 'DRAW'}
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="font-black text-lg text-white">
                  {match.map_name || match.stats?.map_name || 'Standard Arena'}
                </h3>
                {match.stats?.overtime && (
                  <span className="px-2 py-0.5 text-[10px] font-mono font-bold rounded bg-amber-500/20 text-amber-300 border border-amber-500/30">
                    OVERTIME
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2 text-xs text-slate-400 mt-1">
                <span className="text-cyan-400 font-semibold">{formatPlaylistName(match.playlist)}</span>
                <span>•</span>
                <span>{match.stats?.date ? formatRelativeTime(match.stats.date) : 'Unknown Date'}</span>
                {match.stats?.duration ? (
                  <>
                    <span>•</span>
                    <span>{formatDuration(match.stats.duration)}</span>
                  </>
                ) : null}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-4 px-5 py-2.5 rounded-xl bg-slate-900 border border-slate-800">
            <div className="text-center">
              <div className="text-[10px] font-bold text-[#00a2ff] uppercase">Blue</div>
              <div className="text-2xl font-black font-mono text-[#00a2ff]">{match.blue_score}</div>
            </div>
            <div className="text-lg font-black text-slate-600">:</div>
            <div className="text-center">
              <div className="text-[10px] font-bold text-[#ff7b00] uppercase">Orange</div>
              <div className="text-2xl font-black font-mono text-[#ff7b00]">{match.orange_score}</div>
            </div>
          </div>
        </div>

        {/* Dual Rosters */}
        <div>
          {renderRoster('Blue Team', bluePlayers, true)}
          {renderRoster('Orange Team', orangePlayers, false)}
        </div>

        {/* Footer Details: GUID & External Links */}
        <div className="pt-4 border-t border-slate-800 flex flex-col sm:flex-row items-center justify-between gap-3 text-xs">
          <div className="flex items-center gap-2 text-slate-400 font-mono">
            <span className="text-slate-500 text-[11px]">GUID:</span>
            <span className="truncate max-w-[200px] sm:max-w-xs">{match.match_guid}</span>
            <button
              onClick={handleCopyGUID}
              className="p-1 rounded hover:bg-slate-800 text-slate-400 hover:text-slate-200 transition-colors"
              title="Copy match GUID"
            >
              {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
            </button>
          </div>

          {ballchasingURL && (
            <a
              href={ballchasingURL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-cyan-500/10 hover:bg-cyan-500/20 text-cyan-300 border border-cyan-500/30 font-semibold transition-colors"
            >
              <span>View on Ballchasing.com</span>
              <ExternalLink className="w-3.5 h-3.5" />
            </a>
          )}
        </div>
      </div>
    </Modal>
  );
};
