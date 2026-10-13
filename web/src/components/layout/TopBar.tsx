import React from 'react';
import { SessionResponse } from '../../types/api';
import { deltaInk, formatMmr, formatRecord, formatSigned, playlistLabels } from '../../utils/broadcast';
import { StatusDot } from '../common/ui';

export type DashboardTab = 'live' | 'session' | 'history' | 'players';

interface TopBarProps {
  activeTab: DashboardTab;
  onTabChange: (tab: DashboardTab) => void;
  inMatch: boolean;
  session: SessionResponse | null;
  /** Playlist whose MMR is shown on the right (current match, else latest session match). */
  playlistId?: number;
  /** Shown while a live match is on screen; opens the column customiser. */
  onOpenColumns?: () => void;
}

const TABS: ReadonlyArray<{ id: DashboardTab; label: string }> = [
  { id: 'live', label: 'LIVE' },
  { id: 'session', label: 'SESSION' },
  { id: 'history', label: 'HISTORY' },
  { id: 'players', label: 'PLAYERS' },
];

const GHOST_LINK =
  'border border-outline px-2.5 py-1 text-muted hover:text-ink hover:border-ink transition-colors duration-120 whitespace-nowrap';

export const TopBar: React.FC<TopBarProps> = ({ activeTab, onTabChange, inMatch, session, playlistId, onOpenColumns }) => {
  const matchCount = session?.matches?.length ?? 0;
  const playlist = playlistId != null ? session?.playlists?.[String(playlistId)] : undefined;

  return (
    <header
      data-testid="app-header"
      className="sticky top-0 z-10 h-14 bg-page border-b border-line px-8 flex items-center justify-between gap-6 font-display"
    >
      <div className="flex items-center gap-10 h-full min-w-0">
        <div className="italic font-extrabold text-[22px] tracking-t4 whitespace-nowrap select-none">
          RL<span className="text-blue-ink">/</span>SYNC
        </div>
        <nav aria-label="Dashboard" className="flex gap-7 h-full font-bold text-[15px] tracking-t8">
          {TABS.map((tab) => {
            const active = tab.id === activeTab;
            return (
              <button
                key={tab.id}
                type="button"
                aria-current={active ? 'page' : undefined}
                onClick={() => onTabChange(tab.id)}
                className={`flex items-center gap-2 h-full whitespace-nowrap transition-colors duration-120 ${
                  active ? 'text-ink-strong shadow-[inset_0_-3px_0_#fff]' : 'text-muted hover:text-ink'
                }`}
              >
                {tab.id === 'live' && <StatusDot className={inMatch ? 'bg-loss' : 'bg-faint'} />}
                {tab.label}
                {tab.id === 'session' && matchCount > 0 && <span className="text-count">{matchCount}</span>}
              </button>
            );
          })}
        </nav>
      </div>

      <div className="flex items-center gap-6 font-semibold text-[15px] tracking-t6 text-muted whitespace-nowrap">
        {session && (
          <span className="hidden lg:inline">
            SESSION <span className="text-ink-strong">{formatRecord(session.total_wins, session.total_losses)}</span>
          </span>
        )}
        {playlist && (
          <span className="hidden md:inline">
            {playlistLabels(playlist.playlist_id, playlist.playlist_name).short}{' '}
            <span className="text-ink-strong">{formatMmr(playlist.current_mmr)}</span>{' '}
            <span className={deltaInk(playlist.mmr_delta)}>{formatSigned(playlist.mmr_delta)}</span>
          </span>
        )}
        {onOpenColumns && (
          <button type="button" onClick={onOpenColumns} className={GHOST_LINK}>
            COLUMNS
          </button>
        )}
        <a href="/?mode=overlay" target="_blank" rel="noopener noreferrer" className={GHOST_LINK}>
          OVERLAY ↗
        </a>
      </div>
    </header>
  );
};
