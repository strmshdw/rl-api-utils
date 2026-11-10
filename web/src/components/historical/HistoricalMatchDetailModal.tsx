import React, { useState, useMemo } from 'react';
import { HistoricalMatchRecord, ReplayPlayerStats } from '../../types/api';
import { Modal } from '../common/Modal';
import { MvpChip, SectionLabel } from '../common/ui';
import { OUTCOME_SLAB, formatAgo, formatMmr, playlistLabels, shortRank } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { historyOutcome, isLocal, replayStatus } from './historyData';

interface HistoricalMatchDetailModalProps {
  match: HistoricalMatchRecord | null;
  onClose: () => void;
  localPlayerIds?: Set<string>;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
  inspectedPlayerId?: string;
  kickerPrefix?: string;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;
const NO_IDS = new Set<string>();

export type TelemetryTab = 'core' | 'boost' | 'movement' | 'positioning' | 'teams' | 'raw_json';

export const TELEMETRY_TABS: ReadonlyArray<readonly [TelemetryTab, string]> = [
  ['core', '1. CORE & MECHANICS'],
  ['boost', '2. BOOST ECONOMY & PADS'],
  ['movement', '3. SPEED & MOVEMENT'],
  ['positioning', '4. POSITIONING & ROTATION'],
  ['teams', '5. TEAM HEAD-TO-HEAD'],
  ['raw_json', 'RAW JSON'],
] as const;

function findReplayMvpKey(players: ReplayPlayerStats[], winnerTeam?: number, blueScore = 0, orangeScore = 0): string | null {
  const winTeam = winnerTeam ?? (blueScore > orangeScore ? 0 : orangeScore > blueScore ? 1 : null);
  if (winTeam === null) return null;
  const winners = players.filter((p) => p.team === winTeam);
  if (winners.length === 0) return null;
  const sorted = [...winners].sort((a, b) => (b.score || 0) - (a.score || 0));
  const best = sorted[0];
  return best ? best.online_id || best.name : null;
}

export const HistoricalMatchDetailModal: React.FC<HistoricalMatchDetailModalProps> = ({
  match,
  onClose,
  localPlayerIds = NO_IDS,
  onPlayerClick,
  inspectedPlayerId,
  kickerPrefix,
}) => {
  const [activeTab, setActiveTab] = useState<TelemetryTab>('core');
  const [copiedGuid, setCopiedGuid] = useState(false);
  const [copiedJson, setCopiedJson] = useState(false);

  const outcome = match ? historyOutcome(match, localPlayerIds) : null;
  const players: ReplayPlayerStats[] = match?.stats?.players ?? [];
  const status = match ? replayStatus(match) : { label: '', ink: '' };

  const bluePlayers = useMemo(
    () => players.filter((p) => p.team === 0).sort((a, b) => (b.score || 0) - (a.score || 0)),
    [players],
  );
  const orangePlayers = useMemo(
    () => players.filter((p) => p.team === 1).sort((a, b) => (b.score || 0) - (a.score || 0)),
    [players],
  );

  const mvpKey = useMemo(
    () => (match ? findReplayMvpKey(players, match.winner_team, match.blue_score, match.orange_score) : null),
    [match, players],
  );

  const rawJsonString = useMemo(() => {
    if (!match) return '';
    if (match.stats?.raw_json) {
      try {
        return JSON.stringify(JSON.parse(match.stats.raw_json), null, 2);
      } catch {
        return match.stats.raw_json;
      }
    }
    if (match.replay_stats_json) {
      try {
        return JSON.stringify(JSON.parse(match.replay_stats_json), null, 2);
      } catch {
        return match.replay_stats_json;
      }
    }
    return JSON.stringify(match, null, 2);
  }, [match]);

  if (!match) return null;

  const copyGuid = () => {
    try {
      navigator.clipboard?.writeText(match.match_guid);
    } catch {}
    setCopiedGuid(true);
    setTimeout(() => setCopiedGuid(false), 2000);
  };

  const copyJson = () => {
    try {
      navigator.clipboard?.writeText(rawJsonString);
    } catch {}
    setCopiedJson(true);
    setTimeout(() => setCopiedJson(false), 2000);
  };

  const hasTelemetry = players.some(
    (p) => p.boost !== undefined || p.movement !== undefined || p.positioning !== undefined,
  );

  const displayPlayerName = (p: ReplayPlayerStats) => {
    const you = isLocal(p, localPlayerIds);
    return you ? 'You' : p.name;
  };

  const isInspected = (p: ReplayPlayerStats) => {
    if (!inspectedPlayerId) return false;
    const lower = inspectedPlayerId.toLowerCase();
    const oid = lower.includes('|') ? lower.split('|')[1] : lower;
    return p.online_id?.toLowerCase() === lower || p.online_id?.toLowerCase() === oid || p.name?.toLowerCase() === lower;
  };

  const renderCoreTeamSection = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const sum = (fn: (p: ReplayPlayerStats) => number) => teamRows.reduce((acc, p) => acc + fn(p), 0);
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
          const pShPct = (p.shots || 0) > 0 ? Math.round(((p.goals || 0) / p.shots) * 100) : 0;
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
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
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

  const renderBoostTeamSection = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const n = teamRows.length || 1;
    const sum = (fn: (p: ReplayPlayerStats) => number) => teamRows.reduce((acc, p) => acc + fn(p), 0);
    const avg = (fn: (p: ReplayPlayerStats) => number) => Math.round(sum(fn) / n);

    const hasTeamBoost = teamRows.some((p) => p.boost !== undefined);
    const avgBpm = hasTeamBoost ? avg((p) => p.boost?.bpm ?? 0) : '—';
    const avgBcpm = hasTeamBoost ? avg((p) => p.boost?.bcpm ?? (p.boost?.bpm ? p.boost.bpm * 0.96 : 0)) : '—';
    const avgBst = hasTeamBoost ? `${avg((p) => p.boost?.avg_amount ?? 0)}%` : '—';
    const sumStolen = hasTeamBoost ? sum((p) => p.boost?.amount_stolen ?? 0) : '—';
    const sumBig = hasTeamBoost ? `${sum((p) => p.boost?.count_collected_big ?? 0)} TOT` : '—';
    const sumSmall = hasTeamBoost ? `${sum((p) => p.boost?.count_collected_small ?? 0)} TOT` : '—';
    const sumOverfill = hasTeamBoost ? sum((p) => p.boost?.amount_overfill ?? 0) : '—';
    const avgZero = hasTeamBoost ? `${avg((p) => p.boost?.percent_zero_boost ?? 0)}%` : '—';
    const avgFull = hasTeamBoost ? `${avg((p) => p.boost?.percent_full_boost ?? 0)}%` : '—';

    return (
      <div>
        {teamRows.map((p, i) => {
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const b = p.boost;
          const bpm = b?.bpm != null ? Math.round(b.bpm) : '—';
          const bcmpm = b?.bcpm != null ? Math.round(b.bcpm) : b?.bpm != null ? Math.round(b.bpm * 0.96) : '—';
          const avgAmt = b?.avg_amount != null ? `${Math.round(b.avg_amount)}%` : '—';
          const stolen = b?.amount_stolen != null ? Math.round(b.amount_stolen) : '—';
          const bigPads = b?.count_collected_big != null ? `${b.count_collected_big} (${b.count_stolen_big ?? 0} ST)` : '—';
          const smallPads =
            b?.count_collected_small != null ? `${b.count_collected_small} (${b.count_stolen_small ?? 0} ST)` : '—';
          const overfill = b?.amount_overfill != null ? Math.round(b.amount_overfill) : '—';
          const zeroPct = b?.percent_zero_boost != null ? `${b.percent_zero_boost.toFixed(1)}%` : '—';
          const fullPct = b?.percent_full_boost != null ? `${b.percent_full_boost.toFixed(1)}%` : '—';

          return (
            <div
              key={`${pKey}-${i}`}
              className="grid grid-cols-[1.3fr_repeat(9,1fr)] py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center"
            >
              <button
                type="button"
                onClick={() => onPlayerClick?.(pKey, p.name)}
                className={`font-sans font-extrabold text-left truncate hover:underline cursor-pointer uppercase ${
                  isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'
                }`}
              >
                {displayPlayerName(p)}
              </button>
              <span className="text-right t-text-strong text-ink-strong">{bpm}</span>
              <span className="text-right t-text-ink2 text-ink-2">{bcmpm}</span>
              <span className="text-right t-text-blue text-blue-ink">{avgAmt}</span>
              <span className="text-right t-text-orange text-orange-ink">{stolen}</span>
              <span className="text-right t-text-strong text-ink-strong">{bigPads}</span>
              <span className="text-right t-text-ink2 text-ink-2">{smallPads}</span>
              <span className="text-right t-text-muted text-muted">{overfill}</span>
              <span className="text-right t-text-loss text-loss">{zeroPct}</span>
              <span className="text-right t-text-win text-win">{fullPct}</span>
            </div>
          );
        })}
        <div className="grid grid-cols-[1.3fr_repeat(9,1fr)] py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center">
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
            {teamLabel} TOTALS / AVG
          </span>
          <span className="text-right t-text-strong text-ink-strong">{avgBpm}</span>
          <span className="text-right t-text-ink2 text-ink-2">{avgBcpm}</span>
          <span className="text-right t-text-blue text-blue-ink">{avgBst}</span>
          <span className="text-right t-text-orange text-orange-ink">{sumStolen}</span>
          <span className="text-right t-text-strong text-ink-strong">{sumBig}</span>
          <span className="text-right t-text-ink2 text-ink-2">{sumSmall}</span>
          <span className="text-right t-text-muted text-muted">{sumOverfill}</span>
          <span className="text-right t-text-loss text-loss">{avgZero}</span>
          <span className="text-right t-text-win text-win">{avgFull}</span>
        </div>
      </div>
    );
  };

  const renderMovementTeamSection = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const n = teamRows.length || 1;
    const sum = (fn: (p: ReplayPlayerStats) => number) => teamRows.reduce((acc, p) => acc + fn(p), 0);
    const avg = (fn: (p: ReplayPlayerStats) => number) => Math.round(sum(fn) / n);
    const hasTeamMove = teamRows.some((p) => p.movement !== undefined);

    return (
      <div>
        {teamRows.map((p, i) => {
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const mv = p.movement;
          const avgSpd = mv?.avg_speed != null ? `${Math.round(mv.avg_speed)} uu/s` : '—';
          const supPct = mv?.percent_supersonic_speed != null ? `${mv.percent_supersonic_speed.toFixed(1)}%` : '—';
          const bstSpd = mv?.percent_boost_speed != null ? `${mv.percent_boost_speed.toFixed(1)}%` : '—';
          const slwSpd = mv?.percent_slow_speed != null ? `${mv.percent_slow_speed.toFixed(1)}%` : '—';
          const grdPct = mv?.percent_ground != null ? `${mv.percent_ground.toFixed(1)}%` : '—';
          const lowAir = mv?.percent_low_air != null ? `${mv.percent_low_air.toFixed(1)}%` : '—';
          const highAir = mv?.percent_high_air != null ? `${mv.percent_high_air.toFixed(1)}%` : '—';
          const pslides =
            mv?.count_powerslide != null
              ? `${mv.count_powerslide}${mv.time_powerslide != null ? ` (${mv.time_powerslide.toFixed(1)}s)` : ''}`
              : '—';

          return (
            <div
              key={`${pKey}-${i}`}
              className="grid grid-cols-[1.3fr_repeat(8,1fr)] py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center"
            >
              <button
                type="button"
                onClick={() => onPlayerClick?.(pKey, p.name)}
                className={`font-sans font-extrabold text-left truncate hover:underline cursor-pointer uppercase ${
                  isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'
                }`}
              >
                {displayPlayerName(p)}
              </button>
              <span className="text-right t-text-strong text-ink-strong">{avgSpd}</span>
              <span className="text-right t-text-win text-win">{supPct}</span>
              <span className="text-right t-text-ink2 text-ink-2">{bstSpd}</span>
              <span className="text-right t-text-muted text-muted">{slwSpd}</span>
              <span className="text-right t-text-ink2 text-ink-2">{grdPct}</span>
              <span className="text-right t-text-ink2 text-ink-2">{lowAir}</span>
              <span className="text-right t-text-blue text-blue-ink">{highAir}</span>
              <span className="text-right t-text-strong text-ink-strong">{pslides}</span>
            </div>
          );
        })}
        <div className="grid grid-cols-[1.3fr_repeat(8,1fr)] py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center">
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
            {teamLabel} TOTALS / AVG
          </span>
          <span className="text-right t-text-strong text-ink-strong">
            {hasTeamMove ? `${avg((p) => p.movement?.avg_speed ?? 0)} uu/s` : '—'}
          </span>
          <span className="text-right t-text-win text-win">
            {hasTeamMove ? `${avg((p) => p.movement?.percent_supersonic_speed ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamMove ? `${avg((p) => p.movement?.percent_boost_speed ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-muted text-muted">
            {hasTeamMove ? `${avg((p) => p.movement?.percent_slow_speed ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamMove ? `${avg((p) => p.movement?.percent_ground ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamMove ? `${avg((p) => p.movement?.percent_low_air ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-blue text-blue-ink">
            {hasTeamMove ? `${avg((p) => p.movement?.percent_high_air ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-strong text-ink-strong">
            {hasTeamMove ? `${sum((p) => p.movement?.count_powerslide ?? 0)} TOT` : '—'}
          </span>
        </div>
      </div>
    );
  };

  const renderPositioningTeamSection = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const n = teamRows.length || 1;
    const sum = (fn: (p: ReplayPlayerStats) => number) => teamRows.reduce((acc, p) => acc + fn(p), 0);
    const avg = (fn: (p: ReplayPlayerStats) => number) => Math.round(sum(fn) / n);
    const hasTeamPos = teamRows.some((p) => p.positioning !== undefined);

    return (
      <div>
        {teamRows.map((p, i) => {
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const pos = p.positioning;
          return (
            <div
              key={`${pKey}-${i}`}
              className="grid grid-cols-[1.3fr_repeat(8,1fr)] py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center"
            >
              <button
                type="button"
                onClick={() => onPlayerClick?.(pKey, p.name)}
                className={`font-sans font-extrabold text-left truncate hover:underline cursor-pointer uppercase ${
                  isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'
                }`}
              >
                {displayPlayerName(p)}
              </button>
              <span className="text-right t-text-ink2 text-ink-2">
                {pos?.percent_defensive_third != null ? `${pos.percent_defensive_third.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-ink2 text-ink-2">
                {pos?.percent_neutral_third != null ? `${pos.percent_neutral_third.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-win text-win">
                {pos?.percent_offensive_third != null ? `${pos.percent_offensive_third.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-blue text-blue-ink">
                {pos?.percent_behind_ball != null ? `${pos.percent_behind_ball.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-muted text-muted">
                {pos?.percent_infront_ball != null ? `${pos.percent_infront_ball.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-ink2 text-ink-2">
                {pos?.percent_most_forward != null ? `${pos.percent_most_forward.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-ink2 text-ink-2">
                {pos?.percent_most_back != null ? `${pos.percent_most_back.toFixed(1)}%` : '—'}
              </span>
              <span className="text-right t-text-muted text-muted">
                {pos?.avg_distance_to_ball != null ? `${Math.round(pos.avg_distance_to_ball)} uu` : '—'}
              </span>
            </div>
          );
        })}
        <div className="grid grid-cols-[1.3fr_repeat(8,1fr)] py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center">
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
            {teamLabel} TOTALS / AVG
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_defensive_third ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_neutral_third ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-win text-win">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_offensive_third ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-blue text-blue-ink">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_behind_ball ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-muted text-muted">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_infront_ball ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_most_forward ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-ink2 text-ink-2">
            {hasTeamPos ? `${avg((p) => p.positioning?.percent_most_back ?? 0)}%` : '—'}
          </span>
          <span className="text-right t-text-muted text-muted">
            {hasTeamPos ? `${avg((p) => p.positioning?.avg_distance_to_ball ?? 0)} uu` : '—'}
          </span>
        </div>
      </div>
    );
  };

  // Team Head-to-Head aggregates
  const sumTeam = (rows: ReplayPlayerStats[], fn: (p: ReplayPlayerStats) => number) =>
    rows.reduce((acc, p) => acc + fn(p), 0);
  const avgTeam = (rows: ReplayPlayerStats[], fn: (p: ReplayPlayerStats) => number) =>
    rows.length ? Math.round(sumTeam(rows, fn) / rows.length) : 0;

  const h2hMetrics: Array<[string, number, number]> = [
    ['TEAM SCORE (PTS)', sumTeam(bluePlayers, (p) => p.score || 0), sumTeam(orangePlayers, (p) => p.score || 0)],
    ['SHOTS ON TARGET', sumTeam(bluePlayers, (p) => p.shots || 0), sumTeam(orangePlayers, (p) => p.shots || 0)],
    [
      'TOTAL BOOST COLLECTED',
      sumTeam(bluePlayers, (p) => p.boost?.amount_collected ?? 0),
      sumTeam(orangePlayers, (p) => p.boost?.amount_collected ?? 0),
    ],
    [
      'STOLEN BOOST',
      sumTeam(bluePlayers, (p) => p.boost?.amount_stolen ?? 0),
      sumTeam(orangePlayers, (p) => p.boost?.amount_stolen ?? 0),
    ],
    [
      'AVG SUPERSONIC %',
      avgTeam(bluePlayers, (p) => p.movement?.percent_supersonic_speed ?? 0),
      avgTeam(orangePlayers, (p) => p.movement?.percent_supersonic_speed ?? 0),
    ],
    [
      'DEMOLITIONS INFLICTED',
      sumTeam(bluePlayers, (p) => p.demo?.inflicted ?? p.demos ?? 0),
      sumTeam(orangePlayers, (p) => p.demo?.inflicted ?? p.demos ?? 0),
    ],
  ];

  const kickerText = kickerPrefix
    ? `${kickerPrefix} · ${playlistLabels(match.playlist).name} · FULL BALLCHASING TELEMETRY`
    : `${playlistLabels(match.playlist).name} · ${formatAgo(match.stats?.date || match.created_at)} · FULL BALLCHASING TELEMETRY`;

  return (
    <Modal
      isOpen
      onClose={onClose}
      kicker={kickerText}
      title={`${match.map_name || match.stats?.map_name || 'Unknown arena'} (BLUE ${match.blue_score} – ${match.orange_score} ORANGE)`}
      meta={
        <>
          {outcome && (
            <span className={`font-extrabold text-sm tracking-t12 text-page px-2 py-px ${OUTCOME_SLAB[outcome]}`}>
              {OUTCOME_WORD[outcome]}
            </span>
          )}
          <span className="font-extrabold text-xl leading-none t-font-mono">
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
          <span className="text-ink-2 normal-case truncate max-w-[280px] t-font-mono" title={match.match_guid}>
            {match.match_guid}
          </span>
          <button type="button" onClick={copyGuid} className="text-muted hover:text-ink transition-colors duration-120">
            {copiedGuid ? 'COPIED ✓' : 'COPY GUID'}
          </button>
          {status.href ? (
            <a
              href={status.href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-ink hover:text-blue-ink transition-colors duration-120"
            >
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
      <div className="flex flex-col font-display">
        {/* 5-Tab Ballchasing Telemetry Navigation Bar */}
        <div className="px-7 pt-3.5 border-b t-border border-line flex flex-wrap gap-6 text-sm font-extrabold">
          {TELEMETRY_TABS.map(([id, label]) => (
            <button
              key={id}
              type="button"
              onClick={() => setActiveTab(id)}
              className={`pb-3 border-b-[3px] uppercase transition-colors ${
                activeTab === id
                  ? 'border-current t-text-strong text-ink-strong'
                  : 'border-transparent t-text-muted text-muted hover:text-ink'
              }`}
            >
              {label}
            </button>
          ))}
        </div>

        {/* Tab 1: 1. CORE & MECHANICS */}
        {activeTab === 'core' && (
          <div className="p-7 flex flex-col gap-5">
            {players.length === 0 ? (
              <p className="font-semibold text-base tracking-t8 text-muted">NO PARSED STATS FOR THIS REPLAY</p>
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
        )}

        {/* Tab 2: 2. BOOST ECONOMY & PADS */}
        {activeTab === 'boost' && (
          <div className="p-7 flex flex-col gap-4">
            {!hasTelemetry && (
              <div className="p-3.5 bg-panel border-l-4 border-amber-500 text-amber-300 font-bold text-sm tracking-wider uppercase">
                TELEMETRY PENDING · SYNC FROM BALLCHASING TO POPULATE ADVANCED METRICS
              </div>
            )}
            <div className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto">
              <div className="grid grid-cols-[1.3fr_repeat(9,1fr)] text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase">
                <span>PLAYER</span>
                <span className="text-right">BPM</span>
                <span className="text-right">BCMPM</span>
                <span className="text-right">AVG BST</span>
                <span className="text-right">STOLEN</span>
                <span className="text-right">100 PADS</span>
                <span className="text-right">SML PADS</span>
                <span className="text-right">OVERFILL</span>
                <span className="text-right">0% TIME</span>
                <span className="text-right">100% TIME</span>
              </div>
              {renderBoostTeamSection(bluePlayers, 'BLUE TEAM', true)}
              {renderBoostTeamSection(orangePlayers, 'ORANGE TEAM', false)}
            </div>
          </div>
        )}

        {/* Tab 3: 3. SPEED & MOVEMENT */}
        {activeTab === 'movement' && (
          <div className="p-7 flex flex-col gap-4">
            {!hasTelemetry && (
              <div className="p-3.5 bg-panel border-l-4 border-amber-500 text-amber-300 font-bold text-sm tracking-wider uppercase">
                TELEMETRY PENDING · SYNC FROM BALLCHASING TO POPULATE ADVANCED METRICS
              </div>
            )}
            <div className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto">
              <div className="grid grid-cols-[1.3fr_repeat(8,1fr)] text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase">
                <span>PLAYER</span>
                <span className="text-right">AVG SPEED</span>
                <span className="text-right">SUPERSONIC</span>
                <span className="text-right">BOOST SPD</span>
                <span className="text-right">SLOW SPD</span>
                <span className="text-right">GROUND %</span>
                <span className="text-right">LOW AIR</span>
                <span className="text-right">HIGH AIR</span>
                <span className="text-right">POWERSLIDES</span>
              </div>
              {renderMovementTeamSection(bluePlayers, 'BLUE TEAM', true)}
              {renderMovementTeamSection(orangePlayers, 'ORANGE TEAM', false)}
            </div>
          </div>
        )}

        {/* Tab 4: 4. POSITIONING & ROTATION */}
        {activeTab === 'positioning' && (
          <div className="p-7 flex flex-col gap-4">
            {!hasTelemetry && (
              <div className="p-3.5 bg-panel border-l-4 border-amber-500 text-amber-300 font-bold text-sm tracking-wider uppercase">
                TELEMETRY PENDING · SYNC FROM BALLCHASING TO POPULATE ADVANCED METRICS
              </div>
            )}
            <div className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto">
              <div className="grid grid-cols-[1.3fr_repeat(8,1fr)] text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase">
                <span>PLAYER</span>
                <span className="text-right">DEF 1/3</span>
                <span className="text-right">NEUT 1/3</span>
                <span className="text-right">OFF 1/3</span>
                <span className="text-right">BEHIND BALL</span>
                <span className="text-right">IN FRONT</span>
                <span className="text-right">1ST MAN</span>
                <span className="text-right">3RD MAN</span>
                <span className="text-right">DIST BALL</span>
              </div>
              {renderPositioningTeamSection(bluePlayers, 'BLUE TEAM', true)}
              {renderPositioningTeamSection(orangePlayers, 'ORANGE TEAM', false)}
            </div>
          </div>
        )}

        {/* Tab 5: 5. TEAM HEAD-TO-HEAD */}
        {activeTab === 'teams' && (
          <div className="p-7">
            <div className="t-bg-panel bg-panel t-border border border-line p-5 flex flex-col gap-4 text-sm font-extrabold t-num">
              {h2hMetrics.map(([lbl, bVal, oVal]) => {
                const total = bVal + oVal;
                const pct = total > 0 ? Math.round((bVal / total) * 100) : 50;
                return (
                  <div key={lbl}>
                    <div className="flex justify-between mb-1.5 text-base">
                      <span className="t-text-blue text-blue-ink t-font-mono">
                        BLUE {bVal} ({pct}%)
                      </span>
                      <span className="t-text-strong text-ink-strong tracking-wider">{lbl}</span>
                      <span className="t-text-orange text-orange-ink t-font-mono">
                        ({100 - pct}%) {oVal} ORANGE
                      </span>
                    </div>
                    <div className="relative h-4 flex gap-[4px] t-bg-line2 bg-line-2 p-[1px]">
                      <div className="t-bg-blue-slab bg-blue-slab h-full" style={{ width: `${pct}%` }} />
                      <div className="t-bg-orange-slab bg-orange-slab h-full" style={{ width: `${100 - pct}%` }} />
                      <div className="absolute left-1/2 top-0 bottom-0 w-[2px] t-bg-ink bg-ink opacity-40 pointer-events-none" />
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {/* Tab 6: RAW JSON */}
        {activeTab === 'raw_json' && (
          <div className="p-7 flex flex-col gap-3">
            <div className="flex justify-between items-center">
              <SectionLabel>RAW TELEMETRY JSON ({rawJsonString.length} BYTES)</SectionLabel>
              <button
                type="button"
                data-testid="copy-json-btn"
                onClick={copyJson}
                className="px-3 py-1 bg-panel border border-line-2 hover:border-ink text-ink font-bold text-sm rounded uppercase transition-colors"
              >
                {copiedJson ? 'COPIED ✓' : 'COPY JSON'}
              </button>
            </div>
            <pre
              data-testid="raw-json-viewer"
              className="bg-panel p-4 font-mono text-sm text-ink overflow-auto max-h-[500px] border border-line select-text whitespace-pre"
            >
              {rawJsonString}
            </pre>
          </div>
        )}
      </div>
    </Modal>
  );
};
