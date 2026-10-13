import React, { useState } from 'react';
import { HistoricalMatchRecord } from '../../types/api';
import { Modal } from '../common/Modal';
import { TeamBoxScore } from '../common/BoxScore';
import { SectionLabel } from '../common/ui';
import { OUTCOME_SLAB, formatAgo, playlistLabels } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { historyOutcome, replayStatus, teamRows } from './historyData';

interface HistoricalMatchDetailModalProps {
  match: HistoricalMatchRecord | null;
  onClose: () => void;
  localPlayerIds?: Set<string>;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;
const NO_IDS = new Set<string>();

export const HistoricalMatchDetailModal: React.FC<HistoricalMatchDetailModalProps> = ({ match, onClose, localPlayerIds = NO_IDS }) => {
  const [copied, setCopied] = useState(false);
  if (!match) return null;

  const outcome = historyOutcome(match, localPlayerIds);
  const players = match.stats?.players ?? [];
  const status = replayStatus(match);

  const copyGuid = () => {
    try {
      navigator.clipboard?.writeText(match.match_guid);
    } catch {
      // Clipboard unavailable (insecure context); the label still confirms the attempt.
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Modal
      isOpen
      onClose={onClose}
      kicker={`${playlistLabels(match.playlist).name} · ${formatAgo(match.stats?.date || match.created_at)}`}
      title={match.map_name || match.stats?.map_name || 'Unknown arena'}
      meta={
        <>
          {outcome && (
            <span className={`font-extrabold text-[13px] tracking-t12 text-page px-2 py-px ${OUTCOME_SLAB[outcome]}`}>
              {OUTCOME_WORD[outcome]}
            </span>
          )}
          <span className="font-extrabold text-[20px] leading-none">
            <span className="text-blue-ink">{match.blue_score}</span>
            <span className="text-faint"> – </span>
            <span className="text-orange-ink">{match.orange_score}</span>
          </span>
          {match.stats?.duration ? (
            <span>
              {formatDuration(match.stats.duration)}
              {match.stats.overtime && <span className="text-loss"> OT</span>}
            </span>
          ) : null}
          <span className="text-ink-2 normal-case truncate max-w-[320px]" title={match.match_guid}>
            {match.match_guid}
          </span>
          <button type="button" onClick={copyGuid} className="text-muted hover:text-ink transition-colors duration-120">
            {copied ? 'COPIED ✓' : 'COPY GUID'}
          </button>
          {status.href ? (
            <a href={status.href} target="_blank" rel="noopener noreferrer" className="text-ink hover:text-blue-ink transition-colors duration-120">
              BALLCHASING ↗
            </a>
          ) : (
            <span className={status.ink} title={status.title}>
              {status.label}
            </span>
          )}
        </>
      }
    >
      <div className="px-8 pt-6 pb-7 flex flex-col gap-3">
        <SectionLabel>BOX SCORE</SectionLabel>
        {players.length === 0 ? (
          <p className="font-semibold text-[15px] tracking-t8 text-muted">NO PARSED STATS FOR THIS REPLAY</p>
        ) : (
          <div className="grid gap-[2px] bg-page">
            {[0, 1].map((team) => (
              <TeamBoxScore key={team} team={team as 0 | 1} label={team === 0 ? 'BLUE' : 'ORANGE'} rows={teamRows(players, team, localPlayerIds)} />
            ))}
          </div>
        )}
      </div>
    </Modal>
  );
};
