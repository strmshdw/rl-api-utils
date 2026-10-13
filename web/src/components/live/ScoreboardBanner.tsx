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

  // Derive scores and momentum stats from player stats
  const bluePlayers = useMemo(() => allPlayers.filter(p => p.team_num === 0), [allPlayers]);
  const orangePlayers = useMemo(() => allPlayers.filter(p => p.team_num === 1), [allPlayers]);

  const blueScore = useMemo(() => {
    return bluePlayers.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  }, [bluePlayers]);

  const orangeScore = useMemo(() => {
    return orangePlayers.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);
  }, [orangePlayers]);

  // Aggregate pressure & momentum metrics
  const blueShots = useMemo(() => bluePlayers.reduce((sum, p) => sum + (p.stats?.shots || 0), 0), [bluePlayers]);
  const orangeShots = useMemo(() => orangePlayers.reduce((sum, p) => sum + (p.stats?.shots || 0), 0), [orangePlayers]);
  const blueSaves = useMemo(() => bluePlayers.reduce((sum, p) => sum + (p.stats?.saves || 0), 0), [bluePlayers]);
  const orangeSaves = useMemo(() => orangePlayers.reduce((sum, p) => sum + (p.stats?.saves || 0), 0), [orangePlayers]);
  const blueDemos = useMemo(() => bluePlayers.reduce((sum, p) => sum + (p.stats?.demos || 0), 0), [bluePlayers]);
  const orangeDemos = useMemo(() => orangePlayers.reduce((sum, p) => sum + (p.stats?.demos || 0), 0), [orangePlayers]);

  // Team Average MMR & differential calculation
  const { blueAvgMmr, orangeAvgMmr, mmrAdvantage } = useMemo(() => {
    const bMmrs = bluePlayers.map(p => p.current_rank?.mmr).filter((m): m is number => typeof m === 'number' && m > 0);
    const oMmrs = orangePlayers.map(p => p.current_rank?.mmr).filter((m): m is number => typeof m === 'number' && m > 0);
    const bAvg = bMmrs.length > 0 ? bMmrs.reduce((a, b) => a + b, 0) / bMmrs.length : null;
    const oAvg = oMmrs.length > 0 ? oMmrs.reduce((a, b) => a + b, 0) / oMmrs.length : null;
    const adv = bAvg !== null && oAvg !== null ? bAvg - oAvg : null;
    return { blueAvgMmr: bAvg, orangeAvgMmr: oAvg, mmrAdvantage: adv };
  }, [bluePlayers, orangePlayers]);

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
    <div
      data-testid="scoreboard-banner"
      className="relative overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/90 shadow-2xl backdrop-blur-md mb-3"
    >
      {/* Top Ambient Glow Bar */}
      <div className="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-[#00a2ff] via-slate-700 to-[#ff7b00]" />

      <div className="grid grid-cols-1 md:grid-cols-3 items-center py-2.5 px-5 gap-4">
        {/* Blue Team Score (Left) */}
        <div className="flex items-center justify-between md:justify-start gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex items-center justify-center w-9 h-9 rounded-xl bg-[#00a2ff]/10 border border-[#00a2ff]/30 text-[#00a2ff] shadow-[0_0_12px_rgba(0,162,255,0.2)]">
              <Shield className="w-5 h-5" />
            </div>
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-bold tracking-wider uppercase text-[#00a2ff]">
                BLUE TEAM
              </span>
              {isLocalOnBlue && (
                <span className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase bg-[#00a2ff]/20 text-[#00a2ff] border border-[#00a2ff]/40">
                  YOU
                </span>
              )}
            </div>
          </div>
          <div className="text-4xl md:text-5xl font-black text-[#00a2ff] leading-none drop-shadow-[0_0_15px_rgba(0,162,255,0.5)]">
            {blueScore}
          </div>
        </div>

        {/* Center Match Telemetry & Stadium Status */}
        <div className="flex flex-col items-center justify-center text-center">
          {renderStatusBadge()}

          <div className="mt-1 text-sm font-semibold text-slate-200 truncate max-w-[220px]">
            {match.playlist_name || 'Rocket League Match'}
          </div>

          {/* MMR Delta Pill */}
          {playlistMmrDelta !== undefined && (
            <div className="mt-0.5 flex items-center gap-1.5 text-xs font-medium">
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

          {/* At-a-glance Team MMR Comparison */}
          {blueAvgMmr !== null && orangeAvgMmr !== null && (
            <div className="mt-1 flex items-center gap-2 text-[11px] font-mono">
              <span className="text-[#00a2ff] font-bold">{blueAvgMmr.toFixed(0)}</span>
              <span className="text-slate-500 font-sans text-[10px]">Avg MMR</span>
              <span className="text-[#ff7b00] font-bold">{orangeAvgMmr.toFixed(0)}</span>
              {mmrAdvantage !== null && (
                <span
                  className={`px-1.5 py-0.2 rounded text-[9px] font-sans font-bold uppercase ${
                    Math.abs(mmrAdvantage) < 5
                      ? 'bg-slate-800/80 text-slate-400 border border-slate-700/60'
                      : mmrAdvantage > 0
                      ? 'bg-[#00a2ff]/20 text-[#00a2ff] border border-[#00a2ff]/40'
                      : 'bg-[#ff7b00]/20 text-[#ff7b00] border border-[#ff7b00]/40'
                  }`}
                  title={`MMR Advantage: ${Math.abs(mmrAdvantage).toFixed(1)}`}
                >
                  {Math.abs(mmrAdvantage) < 5
                    ? 'Even Match'
                    : mmrAdvantage > 0
                    ? `+${mmrAdvantage.toFixed(0)} Blue`
                    : `+${Math.abs(mmrAdvantage).toFixed(0)} Orange`}
                </span>
              )}
            </div>
          )}

          {/* At-a-glance Live Match Momentum Stats */}
          {(blueShots > 0 || orangeShots > 0 || blueSaves > 0 || orangeSaves > 0) && (
            <div className="mt-1 flex items-center gap-2 text-[10px] text-slate-400">
              <span title="Total Shots (Blue vs Orange)">
                Shots: <strong className="text-[#00a2ff]">{blueShots}</strong> - <strong className="text-[#ff7b00]">{orangeShots}</strong>
              </span>
              <span className="text-slate-600">•</span>
              <span title="Total Saves (Blue vs Orange)">
                Saves: <strong className="text-[#00a2ff]">{blueSaves}</strong> - <strong className="text-[#ff7b00]">{orangeSaves}</strong>
              </span>
              {(blueDemos > 0 || orangeDemos > 0) && (
                <>
                  <span className="text-slate-600">•</span>
                  <span title="Demolitions (Blue vs Orange)">
                    Demos: <strong className="text-[#00a2ff]">{blueDemos}</strong> - <strong className="text-[#ff7b00]">{orangeDemos}</strong>
                  </span>
                </>
              )}
            </div>
          )}

          {/* Action Button: Open Column Customizer */}
          <button
            onClick={onOpenColumnConfig}
            className="mt-1.5 inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-lg text-[11px] font-medium text-slate-300 bg-slate-800/80 hover:bg-slate-750 border border-slate-700 hover:border-slate-600 transition-colors"
          >
            <SlidersHorizontal className="w-3 h-3 text-cyan-400" />
            Customize Columns
          </button>
        </div>

        {/* Orange Team Score (Right) */}
        <div className="flex items-center justify-between md:justify-end gap-3">
          <div className="text-4xl md:text-5xl font-black text-[#ff7b00] leading-none drop-shadow-[0_0_15px_rgba(255,123,0,0.5)] order-2 md:order-1">
            {orangeScore}
          </div>
          <div className="flex items-center gap-2.5 order-1 md:order-2">
            <div className="flex items-center justify-end gap-1.5">
              {isLocalOnOrange && (
                <span className="px-1.5 py-0.5 rounded text-[10px] font-black uppercase bg-[#ff7b00]/20 text-[#ff7b00] border border-[#ff7b00]/40">
                  YOU
                </span>
              )}
              <span className="text-sm font-bold tracking-wider uppercase text-[#ff7b00]">
                ORANGE TEAM
              </span>
            </div>
            <div className="flex items-center justify-center w-9 h-9 rounded-xl bg-[#ff7b00]/10 border border-[#ff7b00]/30 text-[#ff7b00] shadow-[0_0_12px_rgba(255,123,0,0.2)]">
              <Flame className="w-5 h-5" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
