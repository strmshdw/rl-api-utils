import React, { useState } from 'react';
import { PlayerMatchDetail } from '../../types/api';
import { TeamBoxScore } from '../common/BoxScore';
import { SectionLabel } from '../common/ui';
import { teamRows } from '../historical/historyData';
import { formatAgo, OUTCOME_SLAB, playlistLabels } from '../../utils/broadcast';
import {
  ProfileResultFilter,
  ProfileRoleFilter,
  ProfileSortOption,
  resolveLocalOutcome,
} from './PlayerRoleSplitCard';

interface PlayerMatchHistoryListProps {
  playerId: string;
  playerName: string;
  matches: PlayerMatchDetail[];
  totalMatches: number;
  isLoadingMatches: boolean;
  matchPage: number;
  totalPages: number;
  setMatchPage: React.Dispatch<React.SetStateAction<number>>;
  roleFilter: ProfileRoleFilter;
  setRoleFilter: React.Dispatch<React.SetStateAction<ProfileRoleFilter>>;
  resultFilter: ProfileResultFilter;
  setResultFilter: React.Dispatch<React.SetStateAction<ProfileResultFilter>>;
  searchQuery: string;
  setSearchQuery: (q: string) => void;
  sortOption: ProfileSortOption;
  setSortOption: (opt: ProfileSortOption) => void;
  localPlayerIds: Set<string>;
  variant: 'dialog' | 'page';
  onSelectPlayer: (playerId: string, playerName?: string) => void;
  onSelectTelemetry: (match: PlayerMatchDetail) => void;
}

export const PlayerMatchHistoryList: React.FC<PlayerMatchHistoryListProps> = ({
  playerId,
  playerName,
  matches,
  totalMatches,
  isLoadingMatches,
  matchPage,
  totalPages,
  setMatchPage,
  roleFilter,
  setRoleFilter,
  resultFilter,
  setResultFilter,
  searchQuery,
  setSearchQuery,
  sortOption,
  setSortOption,
  localPlayerIds,
  variant,
  onSelectPlayer,
  onSelectTelemetry,
}) => {
  const [openMatchGuid, setOpenMatchGuid] = useState<string | null>(null);
  const padClass = variant === 'dialog' ? 'px-8 pt-6 pb-2' : '';

  return (
    <div className={`${padClass} flex flex-col gap-3`}>
      <div className="flex flex-wrap justify-between items-center gap-2.5">
        <SectionLabel>
          {variant === 'page'
            ? `FULL MATCH HISTORY WITH/AGAINST ${playerName} (${totalMatches} ${
                totalMatches === 1 ? 'MATCH' : 'MATCHES'
              })`
            : `MATCH HISTORY (${totalMatches})`}
        </SectionLabel>

        <div className="flex flex-wrap items-center gap-2.5 text-sm font-extrabold">
          {variant === 'page' && (
            <>
              <input
                type="search"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="SEARCH ARENA OR GUID..."
                className="h-[36px] t-bg-panel bg-panel t-border border border-outline px-3 text-sm font-bold t-text-strong text-ink-strong uppercase outline-none t-radius"
              />

              <div className="flex t-border border border-outline t-bg-panel bg-panel t-radius overflow-hidden">
                {(
                  [
                    ['all', 'ALL ROLES'],
                    ['teammate', 'WITH YOU'],
                    ['opponent', 'AGAINST'],
                  ] as const
                ).map(([k, l]) => (
                  <button
                    key={k}
                    type="button"
                    onClick={() => {
                      setRoleFilter(k);
                      setMatchPage(1);
                    }}
                    className={`px-2.5 py-1.5 uppercase cursor-pointer ${
                      roleFilter === k
                        ? 't-bg-ink bg-ink text-page'
                        : 'text-muted hover:text-ink'
                    }`}
                  >
                    {l}
                  </button>
                ))}
              </div>

              <div className="flex t-border border border-outline t-bg-panel bg-panel t-radius overflow-hidden">
                {(
                  [
                    ['all', 'ALL'],
                    ['W', 'WINS'],
                    ['L', 'LOSSES'],
                  ] as const
                ).map(([r, lbl]) => (
                  <button
                    key={r}
                    type="button"
                    onClick={() => {
                      setResultFilter(r);
                      setMatchPage(1);
                    }}
                    className={`px-2.5 py-1.5 uppercase cursor-pointer ${
                      resultFilter === r
                        ? 't-bg-ink bg-ink text-page'
                        : 'text-muted hover:text-ink'
                    }`}
                  >
                    {lbl}
                  </button>
                ))}
              </div>

              <select
                aria-label="Sort player matches"
                value={sortOption}
                onChange={(e) => setSortOption(e.target.value as ProfileSortOption)}
                className="h-[36px] t-bg-panel bg-panel t-border border border-outline px-2.5 t-text-strong text-ink-strong font-extrabold uppercase outline-none t-radius"
              >
                <option value="date-desc">SORT: NEWEST FIRST</option>
                <option value="date-asc">SORT: OLDEST FIRST</option>
                <option value="pts-desc">SORT: SCORE</option>
                <option value="goals-desc">SORT: GOALS</option>
                <option value="saves-desc">SORT: SAVES</option>
                <option value="demos-desc">SORT: DEMOS</option>
              </select>
            </>
          )}

          {totalPages > 1 && (
            <div className="flex items-center gap-2 text-sm font-bold">
              <button
                type="button"
                disabled={matchPage <= 1}
                onClick={() => setMatchPage((p) => Math.max(1, p - 1))}
                className="px-2 py-0.5 bg-panel border border-line-2 disabled:opacity-40 uppercase"
              >
                PREV
              </button>
              <span className="text-muted">
                PAGE {matchPage} OF {totalPages}
              </span>
              <button
                type="button"
                disabled={matchPage >= totalPages}
                onClick={() => setMatchPage((p) => Math.min(totalPages, p + 1))}
                className="px-2 py-0.5 bg-panel border border-line-2 disabled:opacity-40 uppercase"
              >
                NEXT
              </button>
            </div>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-2">
        {isLoadingMatches && matches.length === 0 ? (
          <div className="bg-panel p-4 text-center text-muted font-bold text-sm">
            LOADING MATCHES…
          </div>
        ) : matches.length === 0 ? (
          <div className="bg-panel p-4 text-center text-muted font-bold text-sm">
            NO RECORDED MATCHES FOUND IN SELECTED RANGE
          </div>
        ) : (
          matches.map((m) => {
            const isExpanded = openMatchGuid === m.match_guid;
            const localOutcome = resolveLocalOutcome(m);
            const outcome =
              localOutcome === 'victory'
                ? 'W'
                : localOutcome === 'defeat'
                ? 'L'
                : null;

            return (
              <div
                key={m.match_guid}
                className="t-bg-panel bg-panel t-border border border-line flex flex-col"
              >
                <div
                  role="button"
                  tabIndex={0}
                  data-testid="profile-match-row"
                  onClick={() =>
                    setOpenMatchGuid((cur) => (cur === m.match_guid ? null : m.match_guid))
                  }
                  className="px-4 py-3 flex items-center justify-between cursor-pointer hover:bg-panel-hover transition-colors"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    {outcome && (
                      <span
                        className={`font-extrabold text-sm px-2 py-0.5 text-page rounded ${OUTCOME_SLAB[outcome]}`}
                      >
                        {outcome === 'W' ? 'WIN' : 'LOSS'}
                      </span>
                    )}
                    <span className="font-extrabold text-lg t-font-mono">
                      <span className="text-blue-ink">{m.blue_score}</span>
                      <span className="text-muted"> – </span>
                      <span className="text-orange-ink">{m.orange_score}</span>
                    </span>
                    <span className="font-bold text-base truncate uppercase">
                      {playlistLabels(m.playlist).name} · {m.map_name || 'Arena'}
                    </span>
                  </div>

                  <div className="flex items-center gap-3">
                    <span
                      className={`text-sm font-bold px-2 py-0.5 rounded ${
                        m.role === 'teammate'
                          ? 'bg-win/15 text-win'
                          : 'bg-orange-slab/20 text-orange-ink'
                      }`}
                    >
                      {m.role === 'teammate' ? 'WITH YOU' : 'VS YOU'}
                    </span>
                    <span className="text-muted text-sm">
                      {formatAgo(m.stats?.date || m.created_at)}
                    </span>
                    <span className="text-muted text-sm">{isExpanded ? '▲' : '▼'}</span>
                  </div>
                </div>

                {isExpanded && (
                  <div className="p-4 border-t t-border border-line t-bg-page bg-page flex flex-col gap-3">
                    <div className="grid grid-cols-1 xl:grid-cols-2 gap-[2px]">
                      {[0, 1].map((team) => (
                        <TeamBoxScore
                          key={team}
                          team={team as 0 | 1}
                          label={team === 0 ? 'BLUE' : 'ORANGE'}
                          rows={teamRows(m.stats?.players || [], team, localPlayerIds, playerId)}
                          withRanks
                          onPlayerClick={onSelectPlayer}
                        />
                      ))}
                    </div>
                    <div className="flex flex-wrap justify-between items-center gap-2">
                      {variant === 'page' && (
                        <span className="text-sm font-extrabold text-muted">
                          CLICK ANY PLAYER IN BOX SCORE FOR QUICK MODAL PROFILE (MMR SHOWN BEFORE RANK/DIV)
                        </span>
                      )}
                      <button
                        type="button"
                        onClick={() => onSelectTelemetry(m)}
                        className="ml-auto px-3 py-1.5 bg-blue-slab text-white font-bold text-sm tracking-wider rounded uppercase hover:opacity-90 transition-colors cursor-pointer"
                      >
                        FULL TELEMETRY →
                      </button>
                    </div>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
