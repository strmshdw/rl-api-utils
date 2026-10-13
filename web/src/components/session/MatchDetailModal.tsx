import React, { useMemo } from 'react';
import { SessionMatchDetail, SessionMatchPlayer } from '../../types/api';
import { Modal } from '../common/Modal';
import { formatDuration, formatMMR, formatMMRDelta } from '../../utils/formatters';
import { parsePlatform } from '../../utils/platforms';
import { Copy, Check, Users, WifiOff, Crown, Swords, Shield } from 'lucide-react';

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

  // Compute team comparative totals
  const blueStats = useMemo(() => {
    const score = bluePlayers.reduce((sum, p) => sum + (p.stats?.score || 0), 0);
    const goals = bluePlayers.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
    const assists = bluePlayers.reduce((sum, p) => sum + (p.stats?.assists || 0), 0);
    const saves = bluePlayers.reduce((sum, p) => sum + (p.stats?.saves || 0), 0);
    const shots = bluePlayers.reduce((sum, p) => sum + (p.stats?.shots || 0), 0);
    const demos = bluePlayers.reduce((sum, p) => sum + (p.stats?.demos || 0), 0);
    const mmrs = bluePlayers.map((p) => p.mmr).filter((m) => m > 0);
    const avgMmr = mmrs.length > 0 ? mmrs.reduce((a, b) => a + b, 0) / mmrs.length : null;
    const shotAcc = shots > 0 ? Math.round((goals / shots) * 100) : 0;
    return { score, goals, assists, saves, shots, demos, avgMmr, shotAcc };
  }, [bluePlayers]);

  const orangeStats = useMemo(() => {
    const score = orangePlayers.reduce((sum, p) => sum + (p.stats?.score || 0), 0);
    const goals = orangePlayers.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
    const assists = orangePlayers.reduce((sum, p) => sum + (p.stats?.assists || 0), 0);
    const saves = orangePlayers.reduce((sum, p) => sum + (p.stats?.saves || 0), 0);
    const shots = orangePlayers.reduce((sum, p) => sum + (p.stats?.shots || 0), 0);
    const demos = orangePlayers.reduce((sum, p) => sum + (p.stats?.demos || 0), 0);
    const mmrs = orangePlayers.map((p) => p.mmr).filter((m) => m > 0);
    const avgMmr = mmrs.length > 0 ? mmrs.reduce((a, b) => a + b, 0) / mmrs.length : null;
    const shotAcc = shots > 0 ? Math.round((goals / shots) * 100) : 0;
    return { score, goals, assists, saves, shots, demos, avgMmr, shotAcc };
  }, [orangePlayers]);

  // Identify Match MVP (highest score on the winning team, or highest overall score)
  const winningTeam = match.winner_team !== undefined ? match.winner_team : (match.blue_score > match.orange_score ? 0 : match.orange_score > match.blue_score ? 1 : null);
  const eligibleMvpPool = winningTeam !== null ? match.players.filter((p) => p.team_num === winningTeam) : match.players;
  const mvpPlayer = eligibleMvpPool.reduce<SessionMatchPlayer | null>((top, p) => {
    if (!top || (p.stats?.score || 0) > (top.stats?.score || 0)) {
      return p;
    }
    return top;
  }, null);

  const renderComparativeBar = (label: string, blueVal: number, orangeVal: number, formatVal?: (v: number) => string) => {
    const total = blueVal + orangeVal;
    const bluePct = total > 0 ? (blueVal / total) * 100 : 50;
    const orangePct = total > 0 ? (orangeVal / total) * 100 : 50;
    const displayBlue = formatVal ? formatVal(blueVal) : blueVal.toString();
    const displayOrange = formatVal ? formatVal(orangeVal) : orangeVal.toString();

    return (
      <div className="space-y-1">
        <div className="flex items-center justify-between text-xs font-mono">
          <span className="font-bold text-[#00a2ff]">{displayBlue}</span>
          <span className="text-[11px] font-sans font-semibold text-slate-400 uppercase tracking-wider">{label}</span>
          <span className="font-bold text-[#ff7b00]">{displayOrange}</span>
        </div>
        <div className="w-full bg-slate-950 rounded-full h-2 overflow-hidden flex border border-slate-800">
          <div
            className="bg-[#00a2ff] h-full transition-all duration-300"
            style={{ width: `${bluePct}%` }}
          />
          <div
            className="bg-[#ff7b00] h-full transition-all duration-300"
            style={{ width: `${orangePct}%` }}
          />
        </div>
      </div>
    );
  };

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
                <th className="py-2.5 px-2 text-right">Shot Acc</th>
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
                const disconnectedClasses = p.is_disconnected
                  ? 'opacity-65 saturate-60 hover:opacity-85'
                  : '';
                const isMvp = mvpPlayer && p.player_id === mvpPlayer.player_id && (p.stats?.score || 0) > 0;
                const shotAcc = (p.stats?.shots || 0) > 0 ? `${Math.round(((p.stats?.goals || 0) / p.stats.shots) * 100)}%` : '---';

                return (
                  <tr
                    key={p.player_id}
                    className={`hover:bg-slate-800/50 transition-colors ${
                      p.is_local ? 'bg-cyan-950/25 font-semibold text-white' : 'text-slate-200'
                    } ${disconnectedClasses}`}
                  >
                    <td className="py-2.5 px-4 flex items-center gap-2">
                      <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded border ${plat.badgeClass}`}>
                        {plat.name}
                      </span>
                      <span className="truncate max-w-[150px]">{p.name}</span>
                      {isMvp && (
                        <span className="inline-flex items-center gap-0.5 px-1.5 py-0.2 text-[9px] font-black uppercase rounded bg-amber-500/20 text-amber-300 border border-amber-500/40" title="Match MVP">
                          <Crown className="w-3 h-3 text-amber-400" />
                          MVP
                        </span>
                      )}
                      {p.is_local && (
                        <span className="px-1.5 py-0.2 text-[9px] font-black uppercase rounded bg-cyan-500 text-slate-950">
                          YOU
                        </span>
                      )}
                      {p.is_disconnected && (
                        <span
                          data-testid="badge-disconnected"
                          className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase tracking-wider bg-rose-950/60 text-rose-300 border border-rose-800/60 inline-flex items-center gap-1 shadow-sm"
                          title="Player disconnected early; stats and match outcomes are preserved"
                        >
                          <WifiOff className="w-2.5 h-2.5 text-rose-400" />
                          DISCONNECTED
                        </span>
                      )}
                    </td>
                    <td className="py-2.5 px-2 text-right font-bold text-white">{p.stats?.score ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-emerald-400 font-mono font-bold">{p.stats?.goals ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-sky-400 font-mono font-bold">{p.stats?.assists ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-indigo-400 font-mono font-bold">{p.stats?.saves ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-yellow-400 font-mono">{p.stats?.shots ?? 0}</td>
                    <td className="py-2.5 px-2 text-right text-slate-400 font-mono text-[11px]">{shotAcc}</td>
                    <td className="py-2.5 px-2 text-right text-rose-400 font-mono">{p.stats?.demos ?? 0}</td>
                    <td className="py-2.5 px-3 text-right font-mono text-slate-300">
                      {formatMMR(p.mmr)}
                    </td>
                    <td className="py-2.5 px-3">
                      {h2h && !p.is_local ? (
                        isLocalTeam ? (
                          <span className="text-[11px] text-emerald-300 font-medium">
                            {h2h.wins_as_teammate}W - {h2h.losses_as_teammate}L (mate)
                          </span>
                        ) : (
                          <span className="text-[11px] text-amber-300 font-medium">
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
      <div className="rounded-xl border border-slate-800 bg-slate-950/70 p-4 mb-5">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2 mb-1">
              <span className="text-lg font-bold text-white flex items-center gap-2">
                <Swords className="w-4 h-4 text-cyan-400" />
                {match.playlist_name}
              </span>
              <span
                className={`text-xs font-black uppercase px-2.5 py-0.5 rounded-full border ${
                  match.result === 'victory'
                    ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40 shadow-[0_0_10px_rgba(16,185,129,0.2)]'
                    : match.result === 'defeat'
                    ? 'bg-rose-500/20 text-rose-400 border-rose-500/40 shadow-[0_0_10px_rgba(244,63,94,0.2)]'
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
              <span>•</span>
              <span>
                Your Team:{' '}
                <strong className={match.local_team === 0 ? 'text-[#00a2ff]' : 'text-[#ff7b00]'}>
                  {match.local_team === 0 ? 'Blue Team' : 'Orange Team'}
                </strong>
              </span>
            </div>
          </div>

          <div className="flex items-center gap-5">
            <div className="flex items-center gap-3 bg-slate-900 border border-slate-800 px-4 py-2 rounded-xl shadow-inner">
              <span className="text-2xl font-black text-[#00a2ff]">{match.blue_score}</span>
              <span className="text-slate-500 font-bold">:</span>
              <span className="text-2xl font-black text-[#ff7b00]">{match.orange_score}</span>
            </div>
            <div className="text-right">
              <div className="text-[10px] text-slate-400 font-semibold uppercase">MMR Change</div>
              <div
                className={`text-base font-black font-mono ${
                  match.mmr_change > 0
                    ? 'text-emerald-400'
                    : match.mmr_change < 0
                    ? 'text-rose-400'
                    : 'text-slate-400'
                }`}
              >
                {formatMMRDelta(match.mmr_change)} MMR
              </div>
            </div>
          </div>
        </div>

        {/* Match GUID Copy Pill */}
        <div className="mt-3 pt-3 border-t border-slate-800/80 flex items-center justify-between text-xs text-slate-500">
          <span className="font-mono truncate max-w-sm">GUID: {match.match_guid}</span>
          <button
            onClick={handleCopyGUID}
            className="flex items-center gap-1.5 text-slate-400 hover:text-cyan-300 transition-colors"
          >
            {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
            {copied ? 'Copied to Clipboard!' : 'Copy GUID'}
          </button>
        </div>
      </div>

      {/* Team Comparative Stats Battle Section */}
      <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 mb-6">
        <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-3.5 flex items-center gap-1.5">
          <Shield className="w-3.5 h-3.5 text-cyan-400" />
          Team Head-to-Head Comparison
        </h4>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-3">
          {renderComparativeBar('Total Score', blueStats.score, orangeStats.score)}
          {renderComparativeBar('Goals Scored', blueStats.goals, orangeStats.goals)}
          {renderComparativeBar('Total Shots', blueStats.shots, orangeStats.shots)}
          {renderComparativeBar('Shot Accuracy', blueStats.shotAcc, orangeStats.shotAcc, (v) => `${v}%`)}
          {renderComparativeBar('Saves (Defense)', blueStats.saves, orangeStats.saves)}
          {renderComparativeBar('Demolitions', blueStats.demos, orangeStats.demos)}
        </div>
      </div>

      {/* Roster Tables */}
      {renderRosterTable('Blue Team', bluePlayers, true)}
      {renderRosterTable('Orange Team', orangePlayers, false)}
    </Modal>
  );
};
