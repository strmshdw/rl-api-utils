import React from 'react';
import { HistoricalMatchRecord } from '../../types/api';
import { formatAgo, playlistLabels } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { TeamBoxScore } from '../common/BoxScore';
import { ResultSlab } from '../common/ui';
import { historyOutcome, playerNames, replayStatus, teamRows } from './historyData';

export const HISTORY_ROW_GRID =
  '72px 120px minmax(0,1.2fr) minmax(0,1fr) 90px 150px 140px';

interface HistoryRowProps {
  match: HistoricalMatchRecord;
  localIds: Set<string>;
  open: boolean;
  onToggle: () => void;
  onDetail: () => void;
  onRetry?: (guid: string, e: React.MouseEvent) => void;
  isRetrying?: boolean;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
}

export const HistoryRow: React.FC<HistoryRowProps> = ({
  match,
  localIds,
  open,
  onToggle,
  onDetail,
  onRetry,
  isRetrying = false,
  onPlayerClick,
}) => {
  const players = match.stats?.players ?? [];
  const expandable = players.length > 0;
  const status = replayStatus(match);
  const duration = match.stats?.duration;
  const isFailed =
    (match.download_status || '').toUpperCase() === 'FAILED' ||
    (match.upload_status || '').toUpperCase() === 'FAILED';

  return (
    <div
      data-testid="history-row"
      data-guid={match.match_guid}
      className="flex flex-col bg-panel t-border border border-line"
    >
      <div
        role={expandable ? 'button' : undefined}
        tabIndex={expandable ? 0 : undefined}
        aria-expanded={expandable ? open : undefined}
        onClick={expandable ? onToggle : undefined}
        onKeyDown={
          expandable
            ? (e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onToggle();
                }
              }
            : undefined
        }
        className={`grid items-center h-16 transition-colors duration-120 outline-none ${
          expandable
            ? 'cursor-pointer hover:bg-panel-hover focus-visible:bg-panel-hover'
            : 'cursor-default'
        }`}
        style={{ gridTemplateColumns: HISTORY_ROW_GRID }}
      >
        <ResultSlab outcome={historyOutcome(match, localIds)} className="text-[20px]" />
        <div className="flex items-center gap-2 pl-[22px] font-extrabold text-[34px] leading-none t-font-mono">
          <span className="text-blue-ink">{match.blue_score}</span>
          <span className="text-faint text-[24px]">–</span>
          <span className="text-orange-ink">{match.orange_score}</span>
        </div>
        <div className="flex flex-col gap-0.5 min-w-0 pr-4">
          <span className="font-bold text-lg tracking-t6 truncate">
            {playlistLabels(match.playlist).name}
          </span>
          <span className="font-medium text-sm tracking-t6 text-muted truncate">
            {match.map_name || match.stats?.map_name || 'Unknown arena'}
          </span>
        </div>
        <span className="font-medium text-sm tracking-t4 text-muted truncate pr-4">
          {playerNames(players, localIds) || '—'}
        </span>
        <span className="text-right font-bold text-lg tracking-t4 text-ink-2 whitespace-nowrap t-font-mono">
          {duration ? formatDuration(duration) : '—'}
          {match.stats?.overtime && (
            <span className="text-loss text-sm tracking-t12"> OT</span>
          )}
        </span>
        <div className="flex items-center justify-end gap-2 pr-2">
          <span
            className={`text-right font-bold text-sm tracking-t10 whitespace-nowrap ${status.ink}`}
            title={status.title}
          >
            {status.href ? (
              <a
                href={status.href}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(e) => e.stopPropagation()}
                className="hover:text-blue-ink transition-colors duration-120"
              >
                {status.label}
              </a>
            ) : isRetrying ? (
              'RETRYING…'
            ) : (
              status.label
            )}
          </span>
          {isFailed && (
            <button
              type="button"
              title="Retry replay download & upload"
              disabled={isRetrying}
              onClick={(e) => onRetry?.(match.match_guid, e)}
              className="px-2 py-0.5 bg-loss/15 hover:bg-loss/25 text-loss border border-loss/30 text-sm font-bold rounded tracking-wider uppercase transition-colors disabled:opacity-50"
            >
              {isRetrying ? '…' : 'RETRY'}
            </button>
          )}
        </div>
        <span className="text-right pr-[22px] font-semibold text-sm tracking-t8 text-muted whitespace-nowrap">
          {formatAgo(match.stats?.date || match.created_at)}
        </span>
      </div>

      {open && expandable && (
        <div
          data-testid="history-box-score"
          className="border-t border-page p-4 t-bg-page bg-page flex flex-col gap-3"
        >
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-[2px]">
            {[0, 1].map((team) => (
              <TeamBoxScore
                key={team}
                team={team as 0 | 1}
                label={team === 0 ? 'BLUE' : 'ORANGE'}
                rows={teamRows(players, team, localIds)}
                withRanks
                onPlayerClick={onPlayerClick}
              />
            ))}
          </div>
          <div className="flex flex-wrap justify-between items-center gap-2">
            <span className="text-sm font-extrabold text-muted">
              CLICK ANY PLAYER IN BOX SCORE FOR QUICK MODAL PROFILE (MMR SHOWN BEFORE RANK/DIV)
            </span>
            <button
              type="button"
              onClick={onDetail}
              className="px-4 py-2 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-sm uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
            >
              FULL 5-TAB BALLCHASING TELEMETRY →
            </button>
          </div>
        </div>
      )}
    </div>
  );
};
