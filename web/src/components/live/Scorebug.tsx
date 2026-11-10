import React from 'react';
import { CurrentMatchResponse, LobbyPlayer } from '../../types/api';
import { formatMatchClock, playlistLabels } from '../../utils/broadcast';

interface ScorebugProps {
  match: CurrentMatchResponse;
  bluePlayers: LobbyPlayer[];
  orangePlayers: LobbyPlayer[];
  localTeam: number;
}

function averageMmr(players: LobbyPlayer[]): number | null {
  const mmrs = players.map((p) => p.current_rank?.mmr).filter((m): m is number => typeof m === 'number' && m > 0);
  return mmrs.length ? mmrs.reduce((a, b) => a + b, 0) / mmrs.length : null;
}

const goals = (players: LobbyPlayer[]) => players.reduce((sum, p) => sum + (p.stats?.goals || 0), 0);

/** Parity rule (unchanged): |avg diff| < 5 is an even match. */
export function parityLabel(blueAvg: number | null, orangeAvg: number | null): string | null {
  if (blueAvg === null || orangeAvg === null) return null;
  const diff = blueAvg - orangeAvg;
  if (Math.abs(diff) < 5) return 'EVEN MATCH';
  return diff > 0 ? `+${Math.round(diff)} BLUE` : `+${Math.round(-diff)} ORANGE`;
}

const ScoreTile: React.FC<{ value: number; testId: string; isRival?: boolean }> = ({ value, testId, isRival }) => (
  <div
    data-testid={testId}
    className={`w-[108px] shrink-0 surface-plaque border-x border-line flex items-center justify-center font-extrabold text-[68px] leading-none t-font-mono ${
      isRival ? 'text-orange-ink' : 't-text-strong text-ink-strong'
    }`}
  >
    {value}
  </div>
);

export const Scorebug: React.FC<ScorebugProps> = ({ match, bluePlayers, orangePlayers, localTeam }) => {
  const blueAvg = averageMmr(bluePlayers);
  const orangeAvg = averageMmr(orangePlayers);
  const parity = parityLabel(blueAvg, orangeAvg);
  const sub = (avg: number | null, team: number) =>
    [avg !== null ? `AVG ${Math.round(avg)}` : null, team === localTeam ? 'YOUR TEAM' : null].filter(Boolean).join(' · ');

  let status: React.ReactNode = <span className="text-loss">● LIVE</span>;
  if (match.match_ended) {
    status =
      match.result === 'victory' ? <span className="text-win">WIN</span>
      : match.result === 'defeat' ? <span className="text-loss">LOSS</span>
      : <span className="text-muted">FINAL</span>;
  } else if (match.time_remaining !== undefined && match.time_remaining !== null) {
    status = (
      <div className="flex items-center gap-1.5">
        <span className="text-loss text-[11px] leading-none">● LIVE</span>
        <span
          data-testid="scorebug-clock"
          className={`font-mono font-extrabold text-[17px] tracking-tight leading-none ${match.is_overtime ? 'text-loss' : 'text-ink-strong'}`}
        >
          {formatMatchClock(match.time_remaining, match.is_overtime)}
        </span>
        {match.is_overtime && (
          <span className="text-[10px] font-sans font-bold text-loss px-1 bg-loss/15">OT</span>
        )}
      </div>
    );
  }

  return (
    <div data-testid="scorebug" className="w-full font-display">
      <div className="w-full surface-plaque border border-outline overflow-hidden">
        <div className="team-split-rule" />
        <div className="flex items-stretch h-[82px] w-full justify-between">
          <div className="slab-left relative flex-1 min-w-0 t-bg-blue-slab bg-blue-slab t-on-slab flex flex-col justify-center items-end pr-6 pl-8 overflow-hidden">
            <span aria-hidden="true" className="f-serif-jp font-black text-[64px] leading-none opacity-10 absolute left-4 select-none pointer-events-none">
              嵐
            </span>
            <div className="font-bold text-[26px] tracking-widest leading-none z-10 uppercase">
              <span className="sr-only">BLUE </span>嵐 STORM
            </div>
            <div className="font-mono font-bold text-[12px] tracking-wider opacity-85 max-w-full truncate z-10 mt-1">
              {sub(blueAvg, 0)}
            </div>
          </div>
          <ScoreTile value={goals(bluePlayers)} testId="score-blue" />
          <div className="w-[186px] shrink-0 surface-plaque flex flex-col items-center justify-center gap-1 px-3 text-center">
            <div className="font-bold text-[12px] tracking-t14">{status}</div>
            <div className="font-bold text-[14px] tracking-wider text-ink-strong max-w-full truncate">
              {playlistLabels(match.playlist_id, match.playlist_name).mode}
            </div>
            {parity && (
              <div className="font-mono font-bold text-[11px] tracking-wider px-2 py-0.5 cartouche-mono">
                {parity}
              </div>
            )}
          </div>
          <ScoreTile value={goals(orangePlayers)} testId="score-orange" isRival />
          <div className="slab-right relative flex-1 min-w-0 t-bg-orange-slab bg-orange-slab text-white flex flex-col justify-center items-start pl-6 pr-8 overflow-hidden">
            <span aria-hidden="true" className="f-serif-jp font-black text-[64px] leading-none opacity-15 absolute right-4 select-none pointer-events-none">
              影
            </span>
            <div className="font-bold text-[26px] tracking-widest leading-none z-10 uppercase">
              <span className="sr-only">ORANGE </span>影 RIVAL
            </div>
            <div className="font-mono font-bold text-[12px] tracking-wider opacity-90 max-w-full truncate z-10 mt-1">
              {sub(orangeAvg, 1)}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
