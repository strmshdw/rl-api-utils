import React from 'react';
import { Gamepad2, History, Users, ExternalLink, Database, Eye } from 'lucide-react';

export type DashboardTab = 'live' | 'session' | 'history' | 'historical' | 'players';

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
            onClick={() => onTabChange('session')}
            className={`flex items-center gap-2 px-4 py-3 text-xs font-bold uppercase tracking-wider border-b-2 transition-all ${
              activeTab === 'session' || activeTab === 'history'
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

          {/* Historical Games Tab */}
          <button
            onClick={() => onTabChange('historical')}
            className={`flex items-center gap-2 px-4 py-3 text-xs font-bold uppercase tracking-wider border-b-2 transition-all ${
              activeTab === 'historical'
                ? 'border-cyan-400 text-cyan-300 bg-cyan-500/5'
                : 'border-transparent text-slate-400 hover:text-slate-200 hover:border-slate-700'
            }`}
          >
            <Database className="w-4 h-4" />
            Historical Games
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

        {/* Right Action Buttons */}
        <div className="flex items-center gap-2">
          {/* Demo Preview Toggle Button */}
          <button
            onClick={() => {
              const url = new URL(window.location.href);
              const isCurrentlyPreview =
                url.searchParams.get('preview') === 'true' ||
                url.searchParams.get('demo') === 'true' ||
                url.searchParams.get('test') === 'true';
              if (isCurrentlyPreview) {
                url.searchParams.delete('preview');
                url.searchParams.delete('demo');
                url.searchParams.delete('test');
              } else {
                url.searchParams.set('preview', 'true');
              }
              window.location.href = url.toString();
            }}
            title="Toggle simulated preview of live match and session history"
            className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold border transition-colors ${
              typeof window !== 'undefined' &&
              (new URLSearchParams(window.location.search).get('preview') === 'true' ||
                new URLSearchParams(window.location.search).get('demo') === 'true' ||
                new URLSearchParams(window.location.search).get('test') === 'true')
                ? 'bg-amber-500/20 text-amber-300 border-amber-500/40 hover:bg-amber-500/30 shadow-[0_0_10px_rgba(245,158,11,0.2)]'
                : 'text-slate-400 hover:text-amber-300 hover:bg-slate-900 border-slate-800 hover:border-slate-700'
            }`}
          >
            <Eye className="w-3.5 h-3.5 text-amber-400" />
            <span>
              {typeof window !== 'undefined' &&
              (new URLSearchParams(window.location.search).get('preview') === 'true' ||
                new URLSearchParams(window.location.search).get('demo') === 'true' ||
                new URLSearchParams(window.location.search).get('test') === 'true')
                ? 'Exit Preview'
                : 'Preview UI'}
            </span>
          </button>

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
      </div>
    </nav>
  );
};
