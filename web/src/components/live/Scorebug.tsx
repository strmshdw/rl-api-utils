import React from 'react';
import { CurrentMatchResponse, LobbyPlayer } from '../../types/api';
import { playlistLabels } from '../../utils/broadcast';

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

const ScoreTile: React.FC<{ value: number; testId: string }> = ({ value, testId }) => (
  <div
    data-testid={testId}
    className="w-[110px] shrink-0 bg-ink text-page flex items-center justify-center font-extrabold text-[76px] leading-none"
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
  }

  return (
    <div data-testid="scorebug" className="flex justify-center pt-9 pb-7 font-display">
      <div className="flex items-stretch h-[92px] w-full justify-center">
        <div className="slab-left flex-[1_1_300px] max-w-[300px] min-w-0 bg-blue-slab text-ink flex flex-col justify-center items-end pr-6 pl-8 overflow-hidden">
          <div className="font-extrabold text-[30px] tracking-t6 leading-none">BLUE</div>
          <div className="font-semibold text-[14px] tracking-t8 opacity-[.85] max-w-full truncate">{sub(blueAvg, 0)}</div>
        </div>
        <ScoreTile value={goals(bluePlayers)} testId="score-blue" />
        <div className="w-[150px] shrink-0 bg-tile flex flex-col items-center justify-center gap-1 px-2 text-center">
          <div className="font-bold text-[13px] tracking-t14">{status}</div>
          <div className="font-semibold text-[15px] tracking-t6 text-ink-2 max-w-full truncate">
            {playlistLabels(match.playlist_id, match.playlist_name).mode}
          </div>
          {parity && <div className="font-medium text-[12px] tracking-t8 text-muted-2">{parity}</div>}
        </div>
        <ScoreTile value={goals(orangePlayers)} testId="score-orange" />
        <div className="slab-right flex-[1_1_300px] max-w-[300px] min-w-0 bg-orange-slab text-page flex flex-col justify-center items-start pl-6 pr-8 overflow-hidden">
          <div className="font-extrabold text-[30px] tracking-t6 leading-none">ORANGE</div>
          <div className="font-semibold text-[14px] tracking-t8 opacity-80 max-w-full truncate">{sub(orangeAvg, 1)}</div>
        </div>
      </div>
    </div>
  );
};
