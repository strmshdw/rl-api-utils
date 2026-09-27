import React, { useMemo } from 'react';
import { CurrentMatchResponse, LobbyPlayer } from '../../types/api';
import { Shield, Flame, SlidersHorizontal } from 'lucide-react';

interface ScoreboardBannerProps {
  match: CurrentMatchResponse;
  playlistMmrDelta?: number;
  onOpenColumnConfig: () => void;
}

export const ScoreboardBanner: React.FC<ScoreboardBannerProps> = ({
  match,
  playlistMmrDelta,
  onOpenColumnConfig,
}) => {
  // Aggregate all players across teammates, opponents, spectators, and local player
  const allPlayers = useMemo(() => {
    const list: LobbyPlayer[] = [];
    if (match.local_player) list.push(match.local_player);
    if (match.teammates) list.push(...match.teammates);
    if (match.opponents) list.push(...match.opponents);
    if (match.spectators) list.push(...match.spectators);
    // Deduplicate by player_id
    const seen = new Set<string>();
    return list.filter(p => {
      if (seen.has(p.player_id)) return false;
      seen.add(p.player_id);
      return true;
    });
  }, [match]);

  // Derive scores from player goals
  const blueScore = useMemo(() => {
    return allPlayers
      .filter(p => p.team_num === 0)
      .reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  }, [allPlayers]);

  const orangeScore = useMemo(() => {
    return allPlayers
      .filter(p => p.team_num === 1)
      .reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  }, [allPlayers]);

  const isLocalOnBlue = match.local_team === 0;
  const isLocalOnOrange = match.local_team === 1;

  // Status badge resolution
  const renderStatusBadge = () => {
    if (match.match_ended) {
      if (match.result === 'victory') {
        return (
          <span className="inline-flex items-center px-3 py-1 rounded-full text-xs font-black tracking-wider uppercase bg-emerald-500/20 text-emerald-300 border border-emerald-500/50 shadow-[0_0_12px_rgba(16,185,129,0.3)]">
            VICTORY
          </span>
        );
      }
      if (match.result === 'defeat') {
        return (
          <span className="inline-flex items-center px-3 py-1 rounded-full text-xs font-black tracking-wider uppercase bg-rose-500/20 text-rose-300 border border-rose-500/50 shadow-[0_0_12px_rgba(244,63,94,0.3)]">
            DEFEAT
          </span>
        );
      }
      return (
        <span className="inline-flex items-center px-3 py-1 rounded-full text-xs font-black tracking-wider uppercase bg-slate-800 text-slate-300 border border-slate-700">
          MATCH CONCLUDED
        </span>
      );
    }

    return (
      <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-bold tracking-wider uppercase bg-cyan-500/10 text-cyan-400 border border-cyan-500/30">
        <span className="w-2 h-2 rounded-full bg-cyan-400 animate-ping" />
        LIVE MATCH
      </span>
    );
  };

  return (
    <div className="relative overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/90 shadow-2xl backdrop-blur-md mb-8">
      {/* Top Ambient Glow Bar */}
      <div className="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-[#00a2ff] via-slate-700 to-[#ff7b00]" />

      <div className="grid grid-cols-1 md:grid-cols-3 items-center p-6 gap-6">
        {/* Blue Team Score (Left) */}
        <div className="flex items-center justify-between md:justify-start gap-4">
          <div className="flex items-center gap-3">
            <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-[#00a2ff]/10 border border-[#00a2ff]/30 text-[#00a2ff] shadow-[0_0_15px_rgba(0,162,255,0.25)]">
              <Shield className="w-6 h-6" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="text-sm font-bold tracking-wider uppercase text-[#00a2ff]">
                  BLUE TEAM
                </span>
                {isLocalOnBlue && (
                  <span className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase bg-[#00a2ff]/20 text-[#00a2ff] border border-[#00a2ff]/40">
                    YOU
                  </span>
                )}
              </div>
              <span className="text-xs text-slate-400">
                {allPlayers.filter(p => p.team_num === 0).length} Players
              </span>
            </div>
          </div>
          <div className="text-5xl font-black text-[#00a2ff] drop-shadow-[0_0_15px_rgba(0,162,255,0.5)]">
            {blueScore}
          </div>
        </div>

        {/* Center Match Telemetry & Stadium Status */}
        <div className="flex flex-col items-center justify-center text-center">
          {renderStatusBadge()}

          <div className="mt-2 text-base font-semibold text-slate-200">
            {match.playlist_name || 'Rocket League Match'}
          </div>

          {/* MMR Delta Pill */}
          {playlistMmrDelta !== undefined && (
            <div className="mt-1 flex items-center gap-1.5 text-xs font-medium">
              <span className="text-slate-400">Session Δ:</span>
              <span
                className={`font-bold ${
                  playlistMmrDelta > 0
                    ? 'text-emerald-400'
                    : playlistMmrDelta < 0
                    ? 'text-rose-400'
                    : 'text-slate-400'
                }`}
              >
                {playlistMmrDelta > 0 ? `+${playlistMmrDelta.toFixed(1)}` : playlistMmrDelta.toFixed(1)} MMR
              </span>
            </div>
          )}

          {/* Action Button: Open Column Customizer */}
          <button
            onClick={onOpenColumnConfig}
            className="mt-3 inline-flex items-center gap-1.5 px-3 py-1 rounded-lg text-xs font-medium text-slate-300 bg-slate-800/80 hover:bg-slate-750 border border-slate-700 hover:border-slate-600 transition-colors"
          >
            <SlidersHorizontal className="w-3.5 h-3.5 text-cyan-400" />
            Customize Columns
          </button>
        </div>

        {/* Orange Team Score (Right) */}
        <div className="flex items-center justify-between md:justify-end gap-4">
          <div className="text-5xl font-black text-[#ff7b00] drop-shadow-[0_0_15px_rgba(255,123,0,0.5)] order-2 md:order-1">
            {orangeScore}
          </div>
          <div className="flex items-center gap-3 order-1 md:order-2">
            <div className="text-right">
              <div className="flex items-center justify-end gap-2">
                {isLocalOnOrange && (
                  <span className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase bg-[#ff7b00]/20 text-[#ff7b00] border border-[#ff7b00]/40">
                    YOU
                  </span>
                )}
                <span className="text-sm font-bold tracking-wider uppercase text-[#ff7b00]">
                  ORANGE TEAM
                </span>
              </div>
              <span className="text-xs text-slate-400">
                {allPlayers.filter(p => p.team_num === 1).length} Players
              </span>
            </div>
            <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-[#ff7b00]/10 border border-[#ff7b00]/30 text-[#ff7b00] shadow-[0_0_15px_rgba(255,123,0,0.25)]">
              <Flame className="w-6 h-6" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
