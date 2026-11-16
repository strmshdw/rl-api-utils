import React from 'react';
import { CurrentMatchResponse, LobbyPlayer } from '../../types/api';
import { formatMatchClock, matchesPlaylistFilter, playlistLabels } from '../../utils/broadcast';

interface ScorebugProps {
  match: CurrentMatchResponse;
  bluePlayers: LobbyPlayer[];
  orangePlayers: LobbyPlayer[];
  localTeam: number;
  scopedPlaylistId?: number;
  scopeKey?: string;
}

function resolvePlayerScopedMmr(
  player: LobbyPlayer,
  matchPlaylistId: number,
  scopedPlaylistId?: number,
  scopeKey?: string,
): number | undefined {
  if (
    scopedPlaylistId === 0 &&
    scopeKey &&
    scopeKey !== 'all' &&
    !matchesPlaylistFilter(matchPlaylistId, scopeKey)
  ) {
    return undefined;
  }
  const targetPid = scopedPlaylistId && scopedPlaylistId > 0 ? scopedPlaylistId : matchPlaylistId;
  if (targetPid > 0) {
    const byRankMap = player.ranks?.[String(targetPid)]?.mmr;
    if (typeof byRankMap === 'number' && byRankMap > 0) {
      return byRankMap;
    }
    if (
      player.current_rank &&
      (!player.current_rank.playlist_id || player.current_rank.playlist_id === targetPid)
    ) {
      return player.current_rank.mmr;
    }
    return undefined;
  }
  return player.current_rank?.mmr;
}

function averageMmr(
  players: LobbyPlayer[],
  matchPlaylistId: number,
  scopedPlaylistId?: number,
  scopeKey?: string,
): number | null {
  const mmrs = players
    .map((p) => resolvePlayerScopedMmr(p, matchPlaylistId, scopedPlaylistId, scopeKey))
    .filter((m): m is number => typeof m === 'number' && m > 0);
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

const ScoreTile: React.FC<{ value: number; testId: string; isOrange?: boolean }> = ({
  value,
  testId,
  isOrange,
}) => (
  <div
    data-testid={testId}
    className={`w-[92px] sm:w-[108px] shrink-0 surface-plaque border-x border-line flex items-center justify-center font-extrabold text-[54px] sm:text-[68px] leading-none t-font-mono py-1 ${
      isOrange ? 'text-orange-ink' : 't-text-blue text-blue-ink'
    }`}
  >
    {value}
  </div>
);

export const Scorebug: React.FC<ScorebugProps> = ({
  match,
  bluePlayers,
  orangePlayers,
  localTeam,
  scopedPlaylistId,
  scopeKey,
}) => {
  const blueAvg = averageMmr(bluePlayers, match.playlist_id, scopedPlaylistId, scopeKey);
  const orangeAvg = averageMmr(orangePlayers, match.playlist_id, scopedPlaylistId, scopeKey);
  const parity = parityLabel(blueAvg, orangeAvg);
  const sub = (avg: number | null, team: number) =>
    [avg !== null ? `AVG ${Math.round(avg)}` : null, team === localTeam ? 'YOUR TEAM' : null]
      .filter(Boolean)
      .join(' · ');

  let status: React.ReactNode = <span className="text-loss">● LIVE</span>;
  if (match.match_ended) {
    status =
      match.result === 'victory' ? (
        <span className="text-win">WIN</span>
      ) : match.result === 'defeat' ? (
        <span className="text-loss">LOSS</span>
      ) : (
        <span className="text-muted">FINAL</span>
      );
  } else if (match.time_remaining !== undefined && match.time_remaining !== null) {
    status = (
      <div className="flex items-center gap-1.5">
        <span className="text-loss text-[11px] leading-none">● LIVE</span>
        <span
          data-testid="scorebug-clock"
          className={`font-mono font-extrabold text-[17px] tracking-tight leading-none ${
            match.is_overtime ? 'text-loss' : 'text-ink-strong'
          }`}
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
        <div className="flex items-stretch min-h-[82px] w-full justify-between">
          <div className="slab-left relative flex-1 min-w-0 t-bg-blue-slab bg-blue-slab t-on-slab flex flex-col justify-center items-end pr-4 sm:pr-6 pl-6 sm:pl-8 py-2 overflow-hidden">
            <div className="font-bold text-[22px] sm:text-[26px] tracking-widest leading-none z-10 uppercase">
              BLUE
            </div>
            <div className="font-mono font-bold text-xs tracking-wider opacity-90 max-w-full text-right z-10 mt-1">
              {sub(blueAvg, 0)}
            </div>
          </div>
          <ScoreTile value={goals(bluePlayers)} testId="score-blue" />
          <div className="min-w-[168px] sm:min-w-[204px] shrink-0 surface-plaque flex flex-col items-center justify-center gap-1 px-3 py-1.5 text-center">
            <div className="font-bold text-xs tracking-t14">{status}</div>
            <div className="font-bold text-sm tracking-wider text-ink-strong leading-tight">
              {playlistLabels(match.playlist_id, match.playlist_name).mode}
            </div>
            {parity && (
              <div className="font-mono font-bold text-[11px] tracking-wider px-2 py-0.5 cartouche-mono whitespace-nowrap">
                {parity}
              </div>
            )}
          </div>
          <ScoreTile value={goals(orangePlayers)} testId="score-orange" isOrange />
          <div className="slab-right relative flex-1 min-w-0 t-bg-orange-slab bg-orange-slab text-white flex flex-col justify-center items-start pl-4 sm:pl-6 pr-6 sm:pr-8 py-2 overflow-hidden">
            <div className="font-bold text-[22px] sm:text-[26px] tracking-widest leading-none z-10 uppercase">
              ORANGE
            </div>
            <div className="font-mono font-bold text-xs tracking-wider opacity-90 max-w-full text-left z-10 mt-1">
              {sub(orangeAvg, 1)}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
