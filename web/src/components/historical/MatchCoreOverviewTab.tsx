import React from 'react';
import { ReplayPlayerStats } from '../../types/api';
import { MvpChip } from '../common/ui';
import { formatMmr, shortRank } from '../../utils/broadcast';

interface MatchCoreOverviewTabProps {
  players: ReplayPlayerStats[];
  bluePlayers: ReplayPlayerStats[];
  orangePlayers: ReplayPlayerStats[];
  mvpKey: string | null;
  inspectedPlayerId?: string;
  displayPlayerName: (p: ReplayPlayerStats) => string;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
}

export const TelemetryPendingBanner: React.FC = () => (
  <div className="p-3.5 bg-panel border-l-4 border-amber-500 text-amber-300 font-bold text-sm tracking-wider uppercase">
    TELEMETRY PENDING · SYNC FROM BALLCHASING TO POPULATE ADVANCED METRICS
  </div>
);

export const MatchCoreOverviewTab: React.FC<MatchCoreOverviewTabProps> = ({
  players,
  bluePlayers,
  orangePlayers,
  mvpKey,
  inspectedPlayerId,
  displayPlayerName,
  onPlayerClick,
}) => {
  const isInspected = (p: ReplayPlayerStats) => {
    if (!inspectedPlayerId) return false;
    const lower = inspectedPlayerId.toLowerCase();
    const oid = lower.includes('|') ? lower.split('|')[1] : lower;
    return (
      p.online_id?.toLowerCase() === lower ||
      p.online_id?.toLowerCase() === oid ||
      p.name?.toLowerCase() === lower
    );
  };

  const renderCoreTeamSection = (
    teamRows: ReplayPlayerStats[],
    teamLabel: string,
    isBlue: boolean,
  ) => {
    const sum = (fn: (p: ReplayPlayerStats) => number) =>
      teamRows.reduce((acc, p) => acc + fn(p), 0);
    const totPts = sum((p) => p.score || 0);
    const totG = sum((p) => p.goals || 0);
    const totA = sum((p) => p.assists || 0);
    const totSv = sum((p) => p.saves || 0);
    const totSh = sum((p) => p.shots || 0);
    const totD = sum((p) => p.demo?.inflicted ?? p.demos ?? 0);
    const totTk = sum((p) => p.demo?.taken ?? 0);
    const shPct = totSh > 0 ? Math.round((totG / totSh) * 100) : 0;

    return (
      <div data-testid={isBlue ? 'box-score-blue' : 'box-score-orange'}>
        {teamRows.map((p, i) => {
          const pShPct =
            (p.shots || 0) > 0 ? Math.round(((p.goals || 0) / p.shots) * 100) : 0;
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const isMvp = mvpKey !== null && (p.online_id === mvpKey || p.name === mvpKey);
          const inspected = isInspected(p);
          const demosInflicted = p.demo?.inflicted ?? p.demos ?? 0;
          const demosTaken = p.demo?.taken ?? 0;

          return (
            <div
              key={`${pKey}-${i}`}
              className={`grid grid-cols-[1.35fr_70px_1fr_repeat(8,1fr)] py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center ${
                inspected ? 'bg-panel-hover' : ''
              }`}
            >
              <div className="flex items-center gap-2 min-w-0 font-sans font-extrabold">
                <button
                  type="button"
                  onClick={() => onPlayerClick?.(pKey, p.name)}
                  className={`truncate text-left hover:underline cursor-pointer uppercase ${
                    isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'
                  }`}
                >
                  {displayPlayerName(p)}
                </button>
                {isMvp && <MvpChip />}
              </div>
              <span className="text-right t-text-blue text-blue-ink">{formatMmr(p.mmr)}</span>
              <span className="pl-2 text-sm t-text-ink2 text-ink-2 truncate">
                {p.rank_name ? shortRank(p.rank_name) : 'UNRANKED'}
              </span>
              <span className="text-right t-text-strong text-ink-strong">{p.score || 0}</span>
              <span className="text-right t-text-strong text-ink-strong">{p.goals || 0}</span>
              <span className="text-right t-text-ink2 text-ink-2">{p.assists || 0}</span>
              <span className="text-right t-text-ink2 text-ink-2">{p.saves || 0}</span>
              <span className="text-right t-text-ink2 text-ink-2">{p.shots || 0}</span>
              <span className="text-right t-text-win text-win">{pShPct}%</span>
              <span className="text-right t-text-orange text-orange-ink">{demosInflicted}</span>
              <span className="text-right t-text-muted text-muted">{demosTaken}</span>
            </div>
          );
        })}
        <div className="grid grid-cols-[1.35fr_70px_1fr_repeat(8,1fr)] py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center">
          <span
            className={`font-sans ${
              isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'
            }`}
          >
            {teamLabel} TOTALS / AVG
          </span>
          <span className="text-right t-text-muted text-muted">—</span>
          <span className="pl-2 t-text-muted text-muted">—</span>
          <span className="text-right t-text-strong text-ink-strong">{totPts}</span>
          <span className="text-right t-text-strong text-ink-strong">{totG}</span>
          <span className="text-right t-text-ink2 text-ink-2">{totA}</span>
          <span className="text-right t-text-ink2 text-ink-2">{totSv}</span>
          <span className="text-right t-text-ink2 text-ink-2">{totSh}</span>
          <span className="text-right t-text-win text-win">{shPct}%</span>
          <span className="text-right t-text-orange text-orange-ink">{totD}</span>
          <span className="text-right t-text-muted text-muted">{totTk}</span>
        </div>
      </div>
    );
  };

  return (
    <div className="p-7 flex flex-col gap-5">
      {players.length === 0 ? (
        <p className="font-semibold text-base tracking-t8 text-muted">
          NO PARSED STATS FOR THIS REPLAY
        </p>
      ) : (
        <div className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto">
          <div className="grid grid-cols-[1.35fr_70px_1fr_repeat(8,1fr)] text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase">
            <span>PLAYER</span>
            <span className="text-right">MMR</span>
            <span className="pl-2">RANK · DIV</span>
            <span className="text-right">SCORE</span>
            <span className="text-right">GOALS</span>
            <span className="text-right">ASSISTS</span>
            <span className="text-right">SAVES</span>
            <span className="text-right">SHOTS</span>
            <span className="text-right">SHOT %</span>
            <span className="text-right">DEMOS</span>
            <span className="text-right">DEMO&apos;D</span>
          </div>
          {renderCoreTeamSection(bluePlayers, 'BLUE TEAM', true)}
          {renderCoreTeamSection(orangePlayers, 'ORANGE TEAM', false)}
        </div>
      )}
    </div>
  );
};
