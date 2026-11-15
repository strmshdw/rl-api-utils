import React from 'react';
import { ReplayPlayerStats } from '../../types/api';
import { positioningRoleLabels } from '../../utils/broadcast';
import { SectionLabel } from '../common/ui';
import { TelemetryPendingBanner } from './MatchCoreOverviewTab';

interface TeamTelemetryTabProps {
  bluePlayers: ReplayPlayerStats[];
  orangePlayers: ReplayPlayerStats[];
  hasTelemetry: boolean;
  displayPlayerName: (p: ReplayPlayerStats) => string;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
}

interface MatchPositioningTabProps extends TeamTelemetryTabProps {
  playlist?: number | string | null;
}

const sumBy = (rows: ReplayPlayerStats[], fn: (p: ReplayPlayerStats) => number) =>
  rows.reduce((acc, p) => acc + fn(p), 0);

const meanBy = (rows: ReplayPlayerStats[], fn: (p: ReplayPlayerStats) => number) =>
  rows.length ? sumBy(rows, fn) / rows.length : 0;

const avgBy = (rows: ReplayPlayerStats[], fn: (p: ReplayPlayerStats) => number) =>
  Math.round(meanBy(rows, fn));

const formatTrimmedTenths = (value: number): string =>
  value.toFixed(1).replace(/\.0$/, '');

const BOOST_GRID_COLS = 'grid-cols-[minmax(176px,1.7fr)_repeat(9,minmax(64px,1fr))]';
const MOVEMENT_GRID_COLS = 'grid-cols-[minmax(176px,1.7fr)_repeat(8,minmax(68px,1fr))]';

export const MatchBoostTab: React.FC<TeamTelemetryTabProps> = ({
  bluePlayers,
  orangePlayers,
  hasTelemetry,
  displayPlayerName,
  onPlayerClick,
}) => {
  const renderTeam = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const hasTeamBoost = teamRows.some((p) => p.boost !== undefined);
    const bpmRows = teamRows.filter((p) => p.boost?.bpm != null);
    const bcpmRows = teamRows.filter((p) => p.boost?.bcpm != null);
    const avgBpm = bpmRows.length > 0 ? avgBy(bpmRows, (p) => p.boost!.bpm!) : '—';
    const avgBcpm = bcpmRows.length > 0 ? avgBy(bcpmRows, (p) => p.boost!.bcpm!) : '—';
    const avgBst = hasTeamBoost ? `${avgBy(teamRows, (p) => p.boost?.avg_amount ?? 0)}%` : '—';
    const sumStolen = hasTeamBoost ? sumBy(teamRows, (p) => p.boost?.amount_stolen ?? 0) : '—';
    const sumBig = hasTeamBoost ? `${sumBy(teamRows, (p) => p.boost?.count_collected_big ?? 0)} TOT` : '—';
    const sumSmall = hasTeamBoost ? `${sumBy(teamRows, (p) => p.boost?.count_collected_small ?? 0)} TOT` : '—';
    const sumOverfill = hasTeamBoost ? sumBy(teamRows, (p) => p.boost?.amount_overfill ?? 0) : '—';
    const avgZero = hasTeamBoost ? `${avgBy(teamRows, (p) => p.boost?.percent_zero_boost ?? 0)}%` : '—';
    const avgFull = hasTeamBoost ? `${avgBy(teamRows, (p) => p.boost?.percent_full_boost ?? 0)}%` : '—';

    return (
      <div>
        {teamRows.map((p, i) => {
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const b = p.boost;
          const bpm = b?.bpm != null ? Math.round(b.bpm) : '—';
          const bcpm = b?.bcpm != null ? Math.round(b.bcpm) : '—';
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
              className={`grid ${BOOST_GRID_COLS} py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center`}
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
              <span className="text-right t-text-ink2 text-ink-2">{bcpm}</span>
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
        <div
          className={`grid ${BOOST_GRID_COLS} py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center`}
        >
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
            {teamLabel} TOTALS / AVG
          </span>
          <span title="Team average" className="text-right t-text-strong text-ink-strong">{avgBpm}</span>
          <span title="Team average" className="text-right t-text-ink2 text-ink-2">{avgBcpm}</span>
          <span title="Team average" className="text-right t-text-blue text-blue-ink">{avgBst}</span>
          <span title="Team total" className="text-right t-text-orange text-orange-ink">{sumStolen}</span>
          <span title="Team total" className="text-right t-text-strong text-ink-strong">{sumBig}</span>
          <span title="Team total" className="text-right t-text-ink2 text-ink-2">{sumSmall}</span>
          <span title="Team total" className="text-right t-text-muted text-muted">{sumOverfill}</span>
          <span title="Team average" className="text-right t-text-loss text-loss">{avgZero}</span>
          <span title="Team average" className="text-right t-text-win text-win">{avgFull}</span>
        </div>
      </div>
    );
  };

  return (
    <div className="p-7 flex flex-col gap-4">
      {!hasTelemetry && <TelemetryPendingBanner />}
      <div
        role="region"
        aria-label="Boost economy telemetry"
        tabIndex={0}
        className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto"
      >
        <div className="min-w-[880px]">
          <div
            className={`grid ${BOOST_GRID_COLS} text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase`}
          >
            <span>PLAYER</span>
            <span className="text-right" title="Boost Used Per Minute">BPM</span>
            <span className="text-right" title="Boost Collected Per Minute">BCPM</span>
            <span className="text-right">AVG BST</span>
            <span className="text-right">STOLEN</span>
            <span className="text-right">100 PADS</span>
            <span className="text-right">SML PADS</span>
            <span className="text-right">OVERFILL</span>
            <span className="text-right">0% TIME</span>
            <span className="text-right">100% TIME</span>
          </div>
          {renderTeam(bluePlayers, 'BLUE TEAM', true)}
          {renderTeam(orangePlayers, 'ORANGE TEAM', false)}
        </div>
      </div>
    </div>
  );
};

export const MatchMovementTab: React.FC<TeamTelemetryTabProps> = ({
  bluePlayers,
  orangePlayers,
  hasTelemetry,
  displayPlayerName,
  onPlayerClick,
}) => {
  const renderTeam = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const hasTeamMove = teamRows.some((p) => p.movement !== undefined);
    const speedRows = teamRows.filter((p) => p.movement?.avg_speed != null);
    const teamAvgSpeedUu = speedRows.length > 0 ? meanBy(speedRows, (p) => p.movement!.avg_speed!) : null;
    return (
      <div>
        {teamRows.map((p, i) => {
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const mv = p.movement;
          const pslides =
            mv?.count_powerslide != null
              ? `${mv.count_powerslide}${mv.time_powerslide != null ? ` (${mv.time_powerslide.toFixed(1)}s)` : ''}`
              : '—';
          return (
            <div
              key={`${pKey}-${i}`}
              className={`grid ${MOVEMENT_GRID_COLS} py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center`}
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
              <span className="text-right t-text-strong text-ink-strong">
                {mv?.avg_speed != null ? `${Math.round(mv.avg_speed)} uu/s` : '—'}
              </span>
              <span className="text-right t-text-win text-win">{mv?.percent_supersonic_speed != null ? `${mv.percent_supersonic_speed.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-ink2 text-ink-2">{mv?.percent_boost_speed != null ? `${mv.percent_boost_speed.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-muted text-muted">{mv?.percent_slow_speed != null ? `${mv.percent_slow_speed.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-ink2 text-ink-2">{mv?.percent_ground != null ? `${mv.percent_ground.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-ink2 text-ink-2">{mv?.percent_low_air != null ? `${mv.percent_low_air.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-blue text-blue-ink">{mv?.percent_high_air != null ? `${mv.percent_high_air.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-strong text-ink-strong">{pslides}</span>
            </div>
          );
        })}
        <div
          className={`grid ${MOVEMENT_GRID_COLS} py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center`}
        >
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
            {teamLabel} TOTALS / AVG
          </span>
          <span title="Team average" className="text-right t-text-strong text-ink-strong">
            {teamAvgSpeedUu != null ? `${Math.round(teamAvgSpeedUu)} uu/s` : '—'}
          </span>
          <span className="text-right t-text-win text-win">{hasTeamMove ? `${avgBy(teamRows, (p) => p.movement?.percent_supersonic_speed ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-ink2 text-ink-2">{hasTeamMove ? `${avgBy(teamRows, (p) => p.movement?.percent_boost_speed ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-muted text-muted">{hasTeamMove ? `${avgBy(teamRows, (p) => p.movement?.percent_slow_speed ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-ink2 text-ink-2">{hasTeamMove ? `${avgBy(teamRows, (p) => p.movement?.percent_ground ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-ink2 text-ink-2">{hasTeamMove ? `${avgBy(teamRows, (p) => p.movement?.percent_low_air ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-blue text-blue-ink">{hasTeamMove ? `${avgBy(teamRows, (p) => p.movement?.percent_high_air ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-strong text-ink-strong">{hasTeamMove ? `${sumBy(teamRows, (p) => p.movement?.count_powerslide ?? 0)} TOT` : '—'}</span>
        </div>
      </div>
    );
  };

  return (
    <div className="p-7 flex flex-col gap-4">
      {!hasTelemetry && <TelemetryPendingBanner />}
      <div
        role="region"
        aria-label="Speed and movement telemetry"
        tabIndex={0}
        className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto"
      >
        <div className="min-w-[860px]">
          <div
            className={`grid ${MOVEMENT_GRID_COLS} text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase`}
          >
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
          {renderTeam(bluePlayers, 'BLUE TEAM', true)}
          {renderTeam(orangePlayers, 'ORANGE TEAM', false)}
        </div>
      </div>
    </div>
  );
};

export const MatchPositioningTab: React.FC<MatchPositioningTabProps> = ({
  bluePlayers,
  orangePlayers,
  hasTelemetry,
  displayPlayerName,
  onPlayerClick,
  playlist,
}) => {
  const { showRotation, forwardLabel, backLabel } = positioningRoleLabels(playlist);
  const gridColsClass = showRotation
    ? 'grid-cols-[minmax(176px,1.7fr)_repeat(8,minmax(68px,1fr))]'
    : 'grid-cols-[minmax(176px,1.7fr)_repeat(6,minmax(76px,1fr))]';

  const renderTeam = (teamRows: ReplayPlayerStats[], teamLabel: string, isBlue: boolean) => {
    const hasTeamPos = teamRows.some((p) => p.positioning !== undefined);
    const distRows = teamRows.filter((p) => p.positioning?.avg_distance_to_ball != null);
    const teamAvgDistUu =
      distRows.length > 0 ? meanBy(distRows, (p) => p.positioning!.avg_distance_to_ball!) : null;
    return (
      <div>
        {teamRows.map((p, i) => {
          const pKey = p.online_id && p.online_id !== '0' ? p.online_id : p.name;
          const pos = p.positioning;
          return (
            <div
              key={`${pKey}-${i}`}
              className={`grid ${gridColsClass} py-2.5 px-2 border-b t-border border-line text-base font-extrabold t-panel-hover hover:bg-panel-hover t-font-mono items-center`}
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
              <span className="text-right t-text-ink2 text-ink-2">{pos?.percent_defensive_third != null ? `${pos.percent_defensive_third.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-ink2 text-ink-2">{pos?.percent_neutral_third != null ? `${pos.percent_neutral_third.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-win text-win">{pos?.percent_offensive_third != null ? `${pos.percent_offensive_third.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-blue text-blue-ink">{pos?.percent_behind_ball != null ? `${pos.percent_behind_ball.toFixed(1)}%` : '—'}</span>
              <span className="text-right t-text-muted text-muted">{pos?.percent_infront_ball != null ? `${pos.percent_infront_ball.toFixed(1)}%` : '—'}</span>
              {showRotation && (
                <>
                  <span className="text-right t-text-ink2 text-ink-2">{pos?.percent_most_forward != null ? `${pos.percent_most_forward.toFixed(1)}%` : '—'}</span>
                  <span className="text-right t-text-ink2 text-ink-2">{pos?.percent_most_back != null ? `${pos.percent_most_back.toFixed(1)}%` : '—'}</span>
                </>
              )}
              <span className="text-right t-text-muted text-muted">
                {pos?.avg_distance_to_ball != null ? `${Math.round(pos.avg_distance_to_ball)} uu` : '—'}
              </span>
            </div>
          );
        })}
        <div className={`grid ${gridColsClass} py-2 px-2 t-bg-page bg-page border-b-2 t-border-2 border-line-2 text-base font-extrabold t-font-mono items-center`}>
          <span className={`font-sans ${isBlue ? 't-text-blue text-blue-ink' : 't-text-orange text-orange-ink'}`}>
            {teamLabel} TOTALS / AVG
          </span>
          <span className="text-right t-text-ink2 text-ink-2">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_defensive_third ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-ink2 text-ink-2">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_neutral_third ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-win text-win">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_offensive_third ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-blue text-blue-ink">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_behind_ball ?? 0)}%` : '—'}</span>
          <span className="text-right t-text-muted text-muted">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_infront_ball ?? 0)}%` : '—'}</span>
          {showRotation && (
            <>
              <span className="text-right t-text-ink2 text-ink-2">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_most_forward ?? 0)}%` : '—'}</span>
              <span className="text-right t-text-ink2 text-ink-2">{hasTeamPos ? `${avgBy(teamRows, (p) => p.positioning?.percent_most_back ?? 0)}%` : '—'}</span>
            </>
          )}
          <span title="Team average" className="text-right t-text-muted text-muted">
            {teamAvgDistUu != null ? `${Math.round(teamAvgDistUu)} uu` : '—'}
          </span>
        </div>
      </div>
    );
  };

  return (
    <div className="p-7 flex flex-col gap-4">
      {!hasTelemetry && <TelemetryPendingBanner />}
      <div
        role="region"
        aria-label="Positioning and rotation telemetry"
        tabIndex={0}
        className="t-bg-panel bg-panel t-border border border-line p-4 overflow-x-auto"
      >
        <div className="min-w-[860px]">
          <div
            className={`grid ${gridColsClass} text-sm font-extrabold t-text-muted text-muted pb-2 border-b t-border border-line uppercase`}
          >
            <span>PLAYER</span>
            <span className="text-right">DEF 1/3</span>
            <span className="text-right">NEUT 1/3</span>
            <span className="text-right">OFF 1/3</span>
            <span className="text-right">BEHIND BALL</span>
            <span className="text-right">IN FRONT</span>
            {showRotation && (
              <>
                <span className="text-right">{forwardLabel}</span>
                <span className="text-right">{backLabel}</span>
              </>
            )}
            <span className="text-right">DIST BALL</span>
          </div>
          {renderTeam(bluePlayers, 'BLUE TEAM', true)}
          {renderTeam(orangePlayers, 'ORANGE TEAM', false)}
        </div>
      </div>
    </div>
  );
};

interface H2HRowMetric {
  label: string;
  blueRaw: number;
  orangeRaw: number;
  blueText: string;
  orangeText: string;
}

export const MatchH2HTab: React.FC<{
  bluePlayers: ReplayPlayerStats[];
  orangePlayers: ReplayPlayerStats[];
}> = ({ bluePlayers, orangePlayers }) => {
  const blueSpeedRows = bluePlayers.filter((p) => p.movement?.avg_speed != null);
  const orangeSpeedRows = orangePlayers.filter((p) => p.movement?.avg_speed != null);
  const blueSpeedUu = blueSpeedRows.length > 0 ? meanBy(blueSpeedRows, (p) => p.movement!.avg_speed!) : 0;
  const orangeSpeedUu = orangeSpeedRows.length > 0 ? meanBy(orangeSpeedRows, (p) => p.movement!.avg_speed!) : 0;
  const blueSpeedRounded = Math.round(blueSpeedUu);
  const orangeSpeedRounded = Math.round(orangeSpeedUu);

  const blueSupRows = bluePlayers.filter((p) => p.movement?.percent_supersonic_speed != null);
  const orangeSupRows = orangePlayers.filter((p) => p.movement?.percent_supersonic_speed != null);
  const blueSupPct = blueSupRows.length > 0 ? meanBy(blueSupRows, (p) => p.movement!.percent_supersonic_speed!) : 0;
  const orangeSupPct = orangeSupRows.length > 0 ? meanBy(orangeSupRows, (p) => p.movement!.percent_supersonic_speed!) : 0;

  const makeShareMetric = (label: string, bVal: number, oVal: number): H2HRowMetric => {
    const total = bVal + oVal;
    const pct = total > 0 ? Math.round((bVal / total) * 100) : 50;
    return {
      label,
      blueRaw: bVal,
      orangeRaw: oVal,
      blueText: `BLUE ${bVal} (${pct}%)`,
      orangeText: `(${100 - pct}%) ${oVal} ORANGE`,
    };
  };

  const h2hMetrics: H2HRowMetric[] = [
    makeShareMetric('TEAM SCORE (PTS)', sumBy(bluePlayers, (p) => p.score || 0), sumBy(orangePlayers, (p) => p.score || 0)),
    makeShareMetric('SHOTS ON TARGET', sumBy(bluePlayers, (p) => p.shots || 0), sumBy(orangePlayers, (p) => p.shots || 0)),
    makeShareMetric(
      'TOTAL BOOST COLLECTED',
      sumBy(bluePlayers, (p) => p.boost?.amount_collected ?? 0),
      sumBy(orangePlayers, (p) => p.boost?.amount_collected ?? 0),
    ),
    makeShareMetric(
      'STOLEN BOOST',
      sumBy(bluePlayers, (p) => p.boost?.amount_stolen ?? 0),
      sumBy(orangePlayers, (p) => p.boost?.amount_stolen ?? 0),
    ),
    {
      label: 'AVG SPEED',
      blueRaw: blueSpeedUu,
      orangeRaw: orangeSpeedUu,
      blueText: `BLUE ${blueSpeedRounded} uu/s`,
      orangeText: `${orangeSpeedRounded} uu/s ORANGE`,
    },
    {
      label: 'AVG SUPERSONIC %',
      blueRaw: blueSupPct,
      orangeRaw: orangeSupPct,
      blueText: `BLUE ${formatTrimmedTenths(blueSupPct)}%`,
      orangeText: `${formatTrimmedTenths(orangeSupPct)}% ORANGE`,
    },
    makeShareMetric(
      'DEMOLITIONS INFLICTED',
      sumBy(bluePlayers, (p) => p.demo?.inflicted ?? p.demos ?? 0),
      sumBy(orangePlayers, (p) => p.demo?.inflicted ?? p.demos ?? 0),
    ),
  ];

  return (
    <div className="p-7">
      <div className="t-bg-panel bg-panel t-border border border-line p-5 flex flex-col gap-4 text-sm font-extrabold t-num">
        {h2hMetrics.map((m) => {
          const total = m.blueRaw + m.orangeRaw;
          const pct = total > 0 ? Math.round((m.blueRaw / total) * 100) : 50;
          return (
            <div key={m.label}>
              <div className="flex flex-wrap justify-between items-baseline gap-2 mb-1.5 text-base">
                <span className="t-text-blue text-blue-ink t-font-mono">
                  {m.blueText}
                </span>
                <span className="t-text-strong text-ink-strong tracking-wider">{m.label}</span>
                <span className="t-text-orange text-orange-ink t-font-mono">
                  {m.orangeText}
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
  );
};

export const MatchRawJsonTab: React.FC<{
  rawJsonString: string;
  copiedJson: boolean;
  onCopyJson: () => void;
}> = ({ rawJsonString, copiedJson, onCopyJson }) => (
  <div className="p-7 flex flex-col gap-3">
    <div className="flex justify-between items-center">
      <SectionLabel>RAW TELEMETRY JSON ({rawJsonString.length} BYTES)</SectionLabel>
      <button
        type="button"
        data-testid="copy-json-btn"
        onClick={onCopyJson}
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
);
