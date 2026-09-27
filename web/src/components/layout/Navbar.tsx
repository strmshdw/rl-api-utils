import React from 'react';
import { Gamepad2, History, Users, ExternalLink } from 'lucide-react';

export type DashboardTab = 'live' | 'history' | 'players';

interface NavbarProps {
  activeTab: DashboardTab;
  onTabChange: (tab: DashboardTab) => void;
  inMatch: boolean;
  matchCount: number;
}

export const Navbar: React.FC<NavbarProps> = ({
  activeTab,
  onTabChange,
  inMatch,
  matchCount,
}) => {
  return (
    <nav className="border-b border-slate-800 bg-slate-950/80">
      <div className="container mx-auto px-4 flex items-center justify-between">
        <div className="flex items-center gap-1">
          {/* Live Game Tab */}
          <button
            onClick={() => onTabChange('live')}
            className={`flex items-center gap-2 px-4 py-3 text-xs font-bold uppercase tracking-wider border-b-2 transition-all ${
              activeTab === 'live'
                ? 'border-cyan-400 text-cyan-300 bg-cyan-500/5'
                : 'border-transparent text-slate-400 hover:text-slate-200 hover:border-slate-700'
            }`}
          >
            <Gamepad2 className="w-4 h-4" />
            Live Game
            {inMatch ? (
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
              </span>
            ) : (
              <span className="w-1.5 h-1.5 rounded-full bg-slate-600" />
            )}
          </button>

          {/* Session History Tab */}
          <button
            onClick={() => onTabChange('history')}
            className={`flex items-center gap-2 px-4 py-3 text-xs font-bold uppercase tracking-wider border-b-2 transition-all ${
              activeTab === 'history'
                ? 'border-cyan-400 text-cyan-300 bg-cyan-500/5'
                : 'border-transparent text-slate-400 hover:text-slate-200 hover:border-slate-700'
            }`}
          >
            <History className="w-4 h-4" />
            Session History
            {matchCount > 0 && (
              <span className="px-1.5 py-0.2 rounded-full text-[10px] font-mono font-bold bg-slate-800 text-slate-300 border border-slate-700">
                {matchCount}
              </span>
            )}
          </button>

          {/* Player Tracker Tab */}
          <button
            onClick={() => onTabChange('players')}
            className={`flex items-center gap-2 px-4 py-3 text-xs font-bold uppercase tracking-wider border-b-2 transition-all ${
              activeTab === 'players'
                ? 'border-cyan-400 text-cyan-300 bg-cyan-500/5'
                : 'border-transparent text-slate-400 hover:text-slate-200 hover:border-slate-700'
            }`}
          >
            <Users className="w-4 h-4" />
            Player Tracker
          </button>
        </div>

        {/* OBS Overlay Pop-out Link */}
        <a
          href="/?mode=overlay"
          target="_blank"
          rel="noopener noreferrer"
          title="Open transparent streaming overlay for OBS/Streamlabs"
          className="hidden sm:inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold text-slate-400 hover:text-cyan-300 hover:bg-slate-900 border border-slate-800 hover:border-slate-700 transition-colors"
        >
          <span>OBS Overlay</span>
          <ExternalLink className="w-3.5 h-3.5" />
        </a>
      </div>
    </nav>
  );
};
