import React, { useState } from 'react';
import { SessionMatchDetail, SessionMatchPlayer } from '../../types/api';
import { Modal } from '../common/Modal';
import { TeamBoxScore, sessionMatchRows } from '../common/BoxScore';
import { SectionLabel } from '../common/ui';
import {
  OUTCOME_SLAB,
  deltaInk,
  findMvp,
  formatAgo,
  formatSigned,
  outcomeOf,
  playlistLabels,
  teamName,
} from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';

interface MatchDetailModalProps {
  match: SessionMatchDetail | null;
  onClose: () => void;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;

function teamTotals(players: SessionMatchPlayer[]) {
  const sum = (k: keyof SessionMatchPlayer['stats']) => players.reduce((acc, p) => acc + (p.stats?.[k] || 0), 0);
  const goals = sum('goals');
  const shots = sum('shots');
  return {
    score: sum('score'),
    goals,
    shots,
    accuracy: shots > 0 ? Math.round((goals / shots) * 100) : 0,
    saves: sum('saves'),
    demos: sum('demos'),
  };
}

const CompareBar: React.FC<{ label: string; blue: number; orange: number; suffix?: string }> = ({ label, blue, orange, suffix = '' }) => {
  const total = blue + orange;
  const bluePct = total > 0 ? (blue / total) * 100 : 50;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex justify-between items-baseline">
        <span className="font-bold text-[20px] text-blue-ink">{blue}{suffix}</span>
        <span className="font-semibold text-[12px] tracking-t14 text-muted-2">{label}</span>
        <span className="font-bold text-[20px] text-orange-ink">{orange}{suffix}</span>
      </div>
      <div className="flex h-1.5 bg-line-2 gap-[2px]">
        <div className="bg-blue-ink" style={{ width: `${bluePct}%` }} />
        <div className="bg-orange-ink flex-1" />
      </div>
    </div>
  );
};

export const MatchDetailModal: React.FC<MatchDetailModalProps> = ({ match, onClose }) => {
  const [copied, setCopied] = useState(false);
  if (!match) return null;

  const outcome = outcomeOf(match.result);
  const blue = teamTotals(match.players.filter((p) => p.team_num === 0));
  const orange = teamTotals(match.players.filter((p) => p.team_num === 1));
  const mvp = findMvp(match.players, match.winner_team, match.blue_score, match.orange_score);
  const winner = match.winner_team ?? (match.blue_score > match.orange_score ? 0 : match.orange_score > match.blue_score ? 1 : null);

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
      kicker={`SESSION MATCH · ${formatAgo(match.ended_at)}`}
      title={playlistLabels(match.playlist_id, match.playlist_name).name}
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
          <span>{formatDuration(match.duration_seconds)}</span>
          <span className={deltaInk(match.mmr_change)}>{formatSigned(match.mmr_change)} MMR</span>
          <span className="text-ink-2 normal-case truncate max-w-[320px]" title={match.match_guid}>
            {match.match_guid}
          </span>
          <button type="button" onClick={copyGuid} className="text-muted hover:text-ink transition-colors duration-120">
            {copied ? 'COPIED ✓' : 'COPY GUID'}
          </button>
        </>
      }
    >
      <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
        <SectionLabel>TEAMS</SectionLabel>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-4 bg-panel px-6 py-5">
          <CompareBar label="SCORE" blue={blue.score} orange={orange.score} />
          <CompareBar label="GOALS" blue={blue.goals} orange={orange.goals} />
          <CompareBar label="SHOTS" blue={blue.shots} orange={orange.shots} />
          <CompareBar label="SHOT ACCURACY" blue={blue.accuracy} orange={orange.accuracy} suffix="%" />
          <CompareBar label="SAVES" blue={blue.saves} orange={orange.saves} />
          <CompareBar label="DEMOS" blue={blue.demos} orange={orange.demos} />
        </div>
      </div>
      <div className="px-8 pt-6 pb-7 flex flex-col gap-3">
        <SectionLabel>BOX SCORE</SectionLabel>
        <div className="grid gap-[2px] bg-page">
          {[0, 1].map((team) => (
            <TeamBoxScore
              key={team}
              team={team as 0 | 1}
              label={`${teamName(team)}${winner === team ? ' · WIN' : ''}`}
              rows={sessionMatchRows(match, team, mvp?.player_id)}
              withRanks
              withH2H
            />
          ))}
        </div>
      </div>
    </Modal>
  );
};
