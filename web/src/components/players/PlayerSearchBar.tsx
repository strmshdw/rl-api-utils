import React from 'react';
import { Search, X } from 'lucide-react';

interface PlayerSearchBarProps {
  query: string;
  onQueryChange: (q: string) => void;
  platform: string;
  onPlatformChange: (p: string) => void;
}

const PLATFORMS = [
  { id: 'all', label: 'All Platforms' },
  { id: 'steam', label: 'Steam' },
  { id: 'epic', label: 'Epic Games' },
  { id: 'playstation', label: 'PlayStation' },
  { id: 'xbox', label: 'Xbox' },
];

export const PlayerSearchBar: React.FC<PlayerSearchBarProps> = ({
  query,
  onQueryChange,
  platform,
  onPlatformChange,
}) => {
  return (
    <div className="space-y-3">
      {/* Search Input Box */}
      <div className="relative">
        <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
        <input
          type="text"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder="Search by player name or platform ID (e.g. Steam|7656... or Octane)..."
          className="w-full rounded-xl border border-slate-700 bg-slate-900/90 pl-10 pr-10 py-2.5 text-sm text-slate-100 placeholder-slate-500 focus:border-cyan-500 focus:outline-none focus:ring-1 focus:ring-cyan-500 transition-colors shadow-inner"
        />
        {query && (
          <button
            onClick={() => onQueryChange('')}
            aria-label="Clear search"
            className="absolute right-3.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-200"
          >
            <X className="w-4 h-4" />
          </button>
        )}
      </div>

      {/* Platform Filter Pills */}
      <div className="flex flex-wrap items-center gap-2">
        {PLATFORMS.map((p) => {
          const isActive = platform === p.id;
          return (
            <button
              key={p.id}
              onClick={() => onPlatformChange(p.id)}
              className={`px-3 py-1 rounded-full text-xs font-semibold border transition-all ${
                isActive
                  ? 'bg-cyan-500/20 text-cyan-300 border-cyan-500/60 shadow-[0_0_10px_rgba(6,182,212,0.2)]'
                  : 'bg-slate-900/60 text-slate-400 border-slate-800 hover:text-slate-200 hover:border-slate-700'
              }`}
            >
              {p.label}
            </button>
          );
        })}
      </div>
    </div>
  );
};
