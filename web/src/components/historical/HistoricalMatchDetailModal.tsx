import React, { useState, useMemo } from 'react';
import { HistoricalMatchRecord, ReplayPlayerStats } from '../../types/api';
import { Modal } from '../common/Modal';
import { TeamBoxScore } from '../common/BoxScore';
import { SectionLabel, Segmented } from '../common/ui';
import { OUTCOME_SLAB, formatAgo, playlistLabels } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { historyOutcome, replayStatus, teamRows } from './historyData';

interface HistoricalMatchDetailModalProps {
  match: HistoricalMatchRecord | null;
  onClose: () => void;
  localPlayerIds?: Set<string>;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
  inspectedPlayerId?: string;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;
const NO_IDS = new Set<string>();

type TelemetryTab = 'overview' | 'boost_movement' | 'positioning' | 'demos_combat' | 'raw_json';

const TABS = [
  ['overview', 'OVERVIEW'],
  ['boost_movement', 'BOOST & MOVEMENT'],
  ['positioning', 'POSITIONING'],
  ['demos_combat', 'DEMOLITIONS & COMBAT'],
  ['raw_json', 'RAW JSON'],
] as const;

const CompareBar: React.FC<{ label: string; blue: number; orange: number; suffix?: string }> = ({
  label,
  blue,
  orange,
  suffix = '',
}) => {
  const total = blue + orange;
  const bluePct = total > 0 ? (blue / total) * 100 : 50;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex justify-between items-baseline font-display">
        <span className="font-bold text-[20px] text-blue-ink">
          {blue}
          {suffix}
        </span>
        <span className="font-semibold text-[12px] tracking-t14 text-muted-2 uppercase">{label}</span>
        <span className="font-bold text-[20px] text-orange-ink">
          {orange}
          {suffix}
        </span>
      </div>
      <div className="flex h-1.5 bg-line-2 gap-[2px]">
        <div className="bg-blue-ink" style={{ width: `${bluePct}%` }} />
        <div className="bg-orange-ink flex-1" />
      </div>
    </div>
  );
};

function teamTotals(players: ReplayPlayerStats[]) {
  const sum = (k: keyof ReplayPlayerStats) => players.reduce((acc, p) => acc + (Number(p[k]) || 0), 0);
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

export const HistoricalMatchDetailModal: React.FC<HistoricalMatchDetailModalProps> = ({
  match,
  onClose,
  localPlayerIds = NO_IDS,
  onPlayerClick,
  inspectedPlayerId,
}) => {
  const [activeTab, setActiveTab] = useState<TelemetryTab>('overview');
  const [copiedGuid, setCopiedGuid] = useState(false);
  const [copiedJson, setCopiedJson] = useState(false);

  const outcome = match ? historyOutcome(match, localPlayerIds) : null;
  const players: ReplayPlayerStats[] = match?.stats?.players ?? [];
  const status = match ? replayStatus(match) : { label: '', ink: '' };

  const blueTotals = useMemo(() => teamTotals(players.filter((p) => p.team === 0)), [players]);
  const orangeTotals = useMemo(() => teamTotals(players.filter((p) => p.team === 1)), [players]);

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

  const hasTelemetry = players.some((p) => p.boost !== undefined || p.movement !== undefined || p.positioning !== undefined);

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
          <span className="text-ink-2 normal-case truncate max-w-[280px]" title={match.match_guid}>
            {match.match_guid}
          </span>
          <button type="button" onClick={copyGuid} className="text-muted hover:text-ink transition-colors duration-120">
            {copiedGuid ? 'COPIED ✓' : 'COPY GUID'}
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
      <div className="flex flex-col font-display">
        {/* Navigation Tabs */}
        <div className="px-8 pt-4 pb-2 border-b border-line flex gap-2">
          <Segmented
            label="Telemetry tabs"
            options={TABS}
            value={activeTab}
            onChange={(tab) => setActiveTab(tab)}
          />
        </div>

        {/* Tab 1: Overview */}
        {activeTab === 'overview' && (
          <div className="px-8 py-6 flex flex-col gap-6">
            <div className="flex flex-col gap-3">
              <SectionLabel>TEAM COMPARISON</SectionLabel>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-4 bg-panel px-6 py-5">
                <CompareBar label="SCORE" blue={blueTotals.score} orange={orangeTotals.score} />
                <CompareBar label="GOALS" blue={blueTotals.goals} orange={orangeTotals.goals} />
                <CompareBar label="SHOTS" blue={blueTotals.shots} orange={orangeTotals.shots} />
                <CompareBar label="SHOT ACCURACY" blue={blueTotals.accuracy} orange={orangeTotals.accuracy} suffix="%" />
                <CompareBar label="SAVES" blue={blueTotals.saves} orange={orangeTotals.saves} />
                <CompareBar label="DEMOS" blue={blueTotals.demos} orange={orangeTotals.demos} />
              </div>
            </div>

            <div className="flex flex-col gap-3">
              <SectionLabel>BOX SCORE</SectionLabel>
              {players.length === 0 ? (
                <p className="font-semibold text-[15px] tracking-t8 text-muted">NO PARSED STATS FOR THIS REPLAY</p>
              ) : (
                <div className="grid gap-[2px] bg-page">
                  {[0, 1].map((team) => (
                    <TeamBoxScore
                      key={team}
                      team={team as 0 | 1}
                      label={team === 0 ? 'BLUE' : 'ORANGE'}
                      rows={teamRows(players, team, localPlayerIds, inspectedPlayerId)}
                      onPlayerClick={onPlayerClick}
                    />
                  ))}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Tab 2: Boost & Movement */}
        {activeTab === 'boost_movement' && (
          <div className="px-8 py-6 flex flex-col gap-6">
            {!hasTelemetry && (
              <div className="p-3.5 bg-panel border-l-4 border-amber-500 text-amber-300 font-bold text-[13px] tracking-wider uppercase">
                TELEMETRY PENDING · SYNC FROM BALLCHASING TO POPULATE ADVANCED METRICS
              </div>
            )}

            <div className="flex flex-col gap-3">
              <SectionLabel>BOOST TELEMETRY</SectionLabel>
              <div className="bg-panel overflow-x-auto">
                <table className="w-full text-left border-collapse text-[14px]">
                  <thead>
                    <tr className="border-b border-line text-muted font-bold text-[11px] tracking-wider uppercase">
                      <th className="py-2.5 px-4">PLAYER</th>
                      <th className="py-2.5 px-3 text-right">BPM</th>
                      <th className="py-2.5 px-3 text-right">AVG BOOST</th>
                      <th className="py-2.5 px-3 text-right">SMALL PADS</th>
                      <th className="py-2.5 px-3 text-right">BIG PADS</th>
                      <th className="py-2.5 px-3 text-right">STOLEN BIG</th>
                      <th className="py-2.5 px-3 text-right">% 0 BOOST</th>
                      <th className="py-2.5 px-4 text-right">% 100 BOOST</th>
                    </tr>
                  </thead>
                  <tbody>
                    {players.map((p, idx) => (
                      <tr key={idx} className="border-t border-line hover:bg-panel-hover">
                        <td className="py-2 px-4 font-bold">
                          <button
                            type="button"
                            onClick={() => onPlayerClick?.(p.online_id || p.name, p.name)}
                            className={`hover:underline uppercase text-left ${p.team === 0 ? 'text-blue-ink' : 'text-orange-ink'}`}
                          >
                            {p.name}
                          </button>
                        </td>
                        <td className="py-2 px-3 text-right font-mono">{p.boost?.bpm?.toFixed(0) ?? '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.boost?.avg_amount?.toFixed(0) ?? '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.boost?.count_collected_small ?? '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.boost?.count_collected_big ?? '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.boost?.count_stolen_big ?? '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.boost?.percent_zero_boost != null ? `${p.boost.percent_zero_boost.toFixed(1)}%` : '—'}</td>
                        <td className="py-2 px-4 text-right font-mono">{p.boost?.percent_full_boost != null ? `${p.boost.percent_full_boost.toFixed(1)}%` : '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            <div className="flex flex-col gap-3">
              <SectionLabel>MOVEMENT & SPEED</SectionLabel>
              <div className="bg-panel overflow-x-auto">
                <table className="w-full text-left border-collapse text-[14px]">
                  <thead>
                    <tr className="border-b border-line text-muted font-bold text-[11px] tracking-wider uppercase">
                      <th className="py-2.5 px-4">PLAYER</th>
                      <th className="py-2.5 px-3 text-right">AVG SPEED</th>
                      <th className="py-2.5 px-3 text-right">% SUPERSONIC</th>
                      <th className="py-2.5 px-3 text-right">% BOOST SPEED</th>
                      <th className="py-2.5 px-3 text-right">% SLOW</th>
                      <th className="py-2.5 px-3 text-right">% GROUND</th>
                      <th className="py-2.5 px-3 text-right">% AIR</th>
                      <th className="py-2.5 px-4 text-right">POWERSLIDES</th>
                    </tr>
                  </thead>
                  <tbody>
                    {players.map((p, idx) => (
                      <tr key={idx} className="border-t border-line hover:bg-panel-hover">
                        <td className="py-2 px-4 font-bold">
                          <button
                            type="button"
                            onClick={() => onPlayerClick?.(p.online_id || p.name, p.name)}
                            className={`hover:underline uppercase text-left ${p.team === 0 ? 'text-blue-ink' : 'text-orange-ink'}`}
                          >
                            {p.name}
                          </button>
                        </td>
                        <td className="py-2 px-3 text-right font-mono">{p.movement?.avg_speed?.toFixed(0) ?? '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.movement?.percent_supersonic_speed != null ? `${p.movement.percent_supersonic_speed.toFixed(1)}%` : '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.movement?.percent_boost_speed != null ? `${p.movement.percent_boost_speed.toFixed(1)}%` : '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.movement?.percent_slow_speed != null ? `${p.movement.percent_slow_speed.toFixed(1)}%` : '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">{p.movement?.percent_ground != null ? `${p.movement.percent_ground.toFixed(1)}%` : '—'}</td>
                        <td className="py-2 px-3 text-right font-mono">
                          {p.movement?.percent_low_air != null && p.movement?.percent_high_air != null
                            ? `${(p.movement.percent_low_air + p.movement.percent_high_air).toFixed(1)}%`
                            : '—'}
                        </td>
                        <td className="py-2 px-4 text-right font-mono">{p.movement?.count_powerslide ?? '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}

        {/* Tab 3: Positioning */}
        {activeTab === 'positioning' && (
          <div className="px-8 py-6 flex flex-col gap-6">
            {!hasTelemetry && (
              <div className="p-3.5 bg-panel border-l-4 border-amber-500 text-amber-300 font-bold text-[13px] tracking-wider uppercase">
                TELEMETRY PENDING · SYNC FROM BALLCHASING TO POPULATE ADVANCED METRICS
              </div>
            )}

            <div className="flex flex-col gap-3">
              <SectionLabel>FIELD THIRDS & SPACING</SectionLabel>
              <div className="bg-panel overflow-x-auto">
                <table className="w-full text-left border-collapse text-[14px]">
                  <thead>
                    <tr className="border-b border-line text-muted font-bold text-[11px] tracking-wider uppercase">
                      <th className="py-2.5 px-4">PLAYER</th>
                      <th className="py-2.5 px-3">DEF / NEUTRAL / OFF THIRD</th>
                      <th className="py-2.5 px-3 text-right">% BEHIND BALL</th>
                      <th className="py-2.5 px-3 text-right">% IN FRONT</th>
                      <th className="py-2.5 px-3 text-right">% LAST DEFENDER</th>
                      <th className="py-2.5 px-4 text-right">AVG BALL DIST</th>
                    </tr>
                  </thead>
                  <tbody>
                    {players.map((p, idx) => {
                      const def = p.positioning?.percent_defensive_third ?? 33;
                      const neu = p.positioning?.percent_neutral_third ?? 33;
                      const off = p.positioning?.percent_offensive_third ?? 34;
                      return (
                        <tr key={idx} className="border-t border-line hover:bg-panel-hover">
                          <td className="py-2.5 px-4 font-bold">
                            <button
                              type="button"
                              onClick={() => onPlayerClick?.(p.online_id || p.name, p.name)}
                              className={`hover:underline uppercase text-left ${p.team === 0 ? 'text-blue-ink' : 'text-orange-ink'}`}
                            >
                              {p.name}
                            </button>
                          </td>
                          <td className="py-2.5 px-3">
                            <div className="flex flex-col gap-1 w-44">
                              <div className="flex h-2 bg-line-2 overflow-hidden rounded">
                                <div className="bg-blue-ink" style={{ width: `${def}%` }} title={`Defensive: ${def.toFixed(1)}%`} />
                                <div className="bg-muted-2" style={{ width: `${neu}%` }} title={`Neutral: ${neu.toFixed(1)}%`} />
                                <div className="bg-orange-ink" style={{ width: `${off}%` }} title={`Offensive: ${off.toFixed(1)}%`} />
                              </div>
                              <span className="text-[11px] font-mono text-muted">
                                {def.toFixed(0)}% · {neu.toFixed(0)}% · {off.toFixed(0)}%
                              </span>
                            </div>
                          </td>
                          <td className="py-2.5 px-3 text-right font-mono">{p.positioning?.percent_behind_ball != null ? `${p.positioning.percent_behind_ball.toFixed(1)}%` : '—'}</td>
                          <td className="py-2.5 px-3 text-right font-mono">{p.positioning?.percent_infront_ball != null ? `${p.positioning.percent_infront_ball.toFixed(1)}%` : '—'}</td>
                          <td className="py-2.5 px-3 text-right font-mono">{p.positioning?.percent_most_back != null ? `${p.positioning.percent_most_back.toFixed(1)}%` : '—'}</td>
                          <td className="py-2.5 px-4 text-right font-mono">{p.positioning?.avg_distance_to_ball?.toFixed(0) ?? '—'}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}

        {/* Tab 4: Demolitions & Combat */}
        {activeTab === 'demos_combat' && (
          <div className="px-8 py-6 flex flex-col gap-6">
            <div className="flex flex-col gap-3">
              <SectionLabel>COMBAT & DEMOLITIONS</SectionLabel>
              <div className="bg-panel overflow-x-auto">
                <table className="w-full text-left border-collapse text-[14px]">
                  <thead>
                    <tr className="border-b border-line text-muted font-bold text-[11px] tracking-wider uppercase">
                      <th className="py-2.5 px-4">PLAYER</th>
                      <th className="py-2.5 px-3 text-right">DEMOS INFLICTED</th>
                      <th className="py-2.5 px-3 text-right">IN OFFENSIVE HALF</th>
                      <th className="py-2.5 px-3 text-right">IN DEFENSIVE HALF</th>
                      <th className="py-2.5 px-3 text-right">DEMOS TAKEN</th>
                      <th className="py-2.5 px-4 text-right">NET COMBAT +/-</th>
                    </tr>
                  </thead>
                  <tbody>
                    {players.map((p, idx) => {
                      const inflicted = p.demo?.inflicted ?? p.demos ?? 0;
                      const taken = p.demo?.taken ?? 0;
                      const net = inflicted - taken;
                      return (
                        <tr key={idx} className="border-t border-line hover:bg-panel-hover">
                          <td className="py-2 px-4 font-bold">
                            <button
                              type="button"
                              onClick={() => onPlayerClick?.(p.online_id || p.name, p.name)}
                              className={`hover:underline uppercase text-left ${p.team === 0 ? 'text-blue-ink' : 'text-orange-ink'}`}
                            >
                              {p.name}
                            </button>
                          </td>
                          <td className="py-2 px-3 text-right font-mono text-win font-bold">{inflicted}</td>
                          <td className="py-2 px-3 text-right font-mono">{p.demo?.inflicted_in_offensive_half ?? '—'}</td>
                          <td className="py-2 px-3 text-right font-mono">{p.demo?.inflicted_in_defensive_half ?? '—'}</td>
                          <td className="py-2 px-3 text-right font-mono text-loss font-bold">{taken}</td>
                          <td className={`py-2 px-4 text-right font-mono font-bold ${net > 0 ? 'text-win' : net < 0 ? 'text-loss' : 'text-muted'}`}>
                            {net > 0 ? `+${net}` : net}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}

        {/* Tab 5: Raw JSON */}
        {activeTab === 'raw_json' && (
          <div className="px-8 py-6 flex flex-col gap-3">
            <div className="flex justify-between items-center">
              <SectionLabel>RAW TELEMETRY JSON ({rawJsonString.length} BYTES)</SectionLabel>
              <button
                type="button"
                data-testid="copy-json-btn"
                onClick={copyJson}
                className="px-3 py-1 bg-panel border border-line-2 hover:border-ink text-ink font-bold text-[12px] rounded uppercase transition-colors"
              >
                {copiedJson ? 'COPIED ✓' : 'COPY JSON'}
              </button>
            </div>
            <pre
              data-testid="raw-json-viewer"
              className="bg-panel p-4 font-mono text-[12px] text-ink overflow-auto max-h-[500px] border border-line select-text whitespace-pre"
            >
              {rawJsonString}
            </pre>
          </div>
        )}
      </div>
    </Modal>
  );
};
