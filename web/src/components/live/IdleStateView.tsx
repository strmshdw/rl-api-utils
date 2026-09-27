import React from 'react';
import { Radio, Trophy } from 'lucide-react';
import { SessionResponse } from '../../types/api';

interface IdleStateViewProps {
  session: SessionResponse | null;
  onOpenSessionModal?: () => void;
}

export const IdleStateView: React.FC<IdleStateViewProps> = ({ session }) => {
  return (
    <div className="space-y-8">
      {/* Stadium Waiting Radar Card */}
      <div className="relative overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/60 p-10 text-center shadow-xl backdrop-blur-md">
        <div className="flex justify-center mb-5">
          <div className="relative flex items-center justify-center w-20 h-20 rounded-full bg-cyan-500/10 border border-cyan-500/30 text-cyan-400 shadow-[0_0_25px_rgba(6,182,212,0.25)]">
            <Radio className="w-10 h-10 animate-pulse" />
            <span className="absolute inset-0 rounded-full border border-cyan-400/40 animate-ping" />
          </div>
        </div>

        <h2 className="text-xl font-black tracking-wide text-slate-100 uppercase">
          Waiting for Match...
        </h2>
        <p className="max-w-md mx-auto text-sm text-slate-400 mt-2">
          Launch Rocket League and enter any online match. Live telemetry, team rosters, MMR ratings, and historical head-to-head records will appear automatically.
        </p>

        {/* Telemetry Status Indicators */}
        <div className="mt-6 flex flex-wrap items-center justify-center gap-3 text-xs">
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
            <span className="w-2 h-2 rounded-full bg-emerald-400" />
            Stats API: Listening on :49124
          </span>
          <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
            <span className="w-2 h-2 rounded-full bg-emerald-400" />
            PsyNet Rank Queries: Ready
          </span>
        </div>
      </div>

      {/* Session Progress Ribbon (While In Queue) */}
      {session && (
        <div className="rounded-2xl border border-slate-800 bg-slate-900/50 p-6 shadow-lg backdrop-blur-sm">
          <div className="flex items-center justify-between pb-4 border-b border-slate-800/80 mb-5">
            <div className="flex items-center gap-2">
              <Trophy className="w-5 h-5 text-amber-400" />
              <h3 className="font-bold text-sm text-slate-200 uppercase tracking-wider">
                Current Session Summary
              </h3>
            </div>
            <span className="text-xs text-slate-400">
              Started: {new Date(session.started_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Matches Played</span>
              <div className="text-2xl font-black text-slate-100 mt-1">{session.total_matches}</div>
            </div>
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Record (W - L)</span>
              <div className="text-2xl font-black text-emerald-400 mt-1">
                {session.total_wins}W - {session.total_losses}L
              </div>
            </div>
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Win Rate</span>
              <div className="text-2xl font-black text-cyan-400 mt-1">
                {session.win_rate.toFixed(1)}%
              </div>
            </div>
            <div className="p-3.5 rounded-xl bg-slate-800/30 border border-slate-800">
              <span className="text-xs text-slate-400 font-medium">Playlists Tracked</span>
              <div className="text-2xl font-black text-purple-400 mt-1">
                {Object.keys(session.playlists || {}).length}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
