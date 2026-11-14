import React, { useEffect, useMemo, useState } from 'react';
import { HistoricalMatchRecord, ReplayPlayerStats } from '../../types/api';
import { Modal } from '../common/Modal';
import { OUTCOME_SLAB, formatAgo, playlistLabels } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { historyOutcome, isLocal, replayStatus } from './historyData';
import { MatchCoreOverviewTab } from './MatchCoreOverviewTab';
import {
  MatchBoostTab,
  MatchH2HTab,
  MatchMovementTab,
  MatchPositioningTab,
  MatchRawJsonTab,
} from './MatchTelemetryTabs';

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

function findReplayMvpKey(
  players: ReplayPlayerStats[],
  winnerTeam?: number,
  blueScore = 0,
  orangeScore = 0,
): string | null {
  const winTeam =
    winnerTeam ?? (blueScore > orangeScore ? 0 : orangeScore > blueScore ? 1 : null);
  if (winTeam === null) return null;
  const winners = players.filter((p) => p.team === winTeam);
  if (winners.length === 0) return null;
  const sorted = [...winners].sort((a, b) => (b.score || 0) - (a.score || 0));
  const best = sorted[0];
  return best ? best.online_id || best.name : null;
}

function copyTextWithFallback(text: string): void {
  try {
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text);
      return;
    }
  } catch {
    // Fall through to execCommand fallback
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    document.execCommand?.('copy');
    document.body.removeChild(ta);
  } catch {
    // Ignore environments without clipboard support
  }
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
  const [fetchedMatch, setFetchedMatch] = useState<HistoricalMatchRecord | null>(null);

  useEffect(() => {
    if (!match?.match_guid) {
      setFetchedMatch(null);
      return;
    }
    const initialHasTelemetry = (match.stats?.players ?? []).some(
      (p) =>
        p.boost !== undefined ||
        p.movement !== undefined ||
        p.positioning !== undefined,
    );
    if (initialHasTelemetry) {
      setFetchedMatch(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch(`/api/matches/${encodeURIComponent(match.match_guid)}`);
        if (!res || !res.ok) return;
        const data = (await res.json()) as HistoricalMatchRecord;
        if (!cancelled && data && data.match_guid) {
          const fetchedHasPlayers = (data.stats?.players?.length ?? 0) > 0;
          if (fetchedHasPlayers) {
            setFetchedMatch(data);
          }
        }
      } catch {
        // Ignore network errors in test/preview environments
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [match]);

  const effectiveMatch =
    fetchedMatch && fetchedMatch.match_guid === match?.match_guid ? fetchedMatch : match;
  const outcome = effectiveMatch ? historyOutcome(effectiveMatch, localPlayerIds) : null;
  const players: ReplayPlayerStats[] = effectiveMatch?.stats?.players ?? [];
  const status = effectiveMatch ? replayStatus(effectiveMatch) : { label: '', ink: '' };

  const bluePlayers = useMemo(
    () => players.filter((p) => p.team === 0).sort((a, b) => (b.score || 0) - (a.score || 0)),
    [players],
  );
  const orangePlayers = useMemo(
    () => players.filter((p) => p.team === 1).sort((a, b) => (b.score || 0) - (a.score || 0)),
    [players],
  );

  const mvpKey = useMemo(
    () =>
      effectiveMatch
        ? findReplayMvpKey(
            players,
            effectiveMatch.winner_team,
            effectiveMatch.blue_score,
            effectiveMatch.orange_score,
          )
        : null,
    [effectiveMatch, players],
  );

  const rawJsonString = useMemo(() => {
    if (!effectiveMatch) return '';
    if (effectiveMatch.stats?.raw_json) {
      try {
        return JSON.stringify(JSON.parse(effectiveMatch.stats.raw_json), null, 2);
      } catch {
        return effectiveMatch.stats.raw_json;
      }
    }
    if (effectiveMatch.replay_stats_json) {
      try {
        return JSON.stringify(JSON.parse(effectiveMatch.replay_stats_json), null, 2);
      } catch {
        return effectiveMatch.replay_stats_json;
      }
    }
    return JSON.stringify(effectiveMatch, null, 2);
  }, [effectiveMatch]);

  if (!effectiveMatch) return null;

  const copyGuid = () => {
    copyTextWithFallback(effectiveMatch.match_guid);
    setCopiedGuid(true);
    setTimeout(() => setCopiedGuid(false), 2000);
  };

  const copyJson = () => {
    copyTextWithFallback(rawJsonString);
    setCopiedJson(true);
    setTimeout(() => setCopiedJson(false), 2000);
  };

  const hasTelemetry = players.some(
    (p) =>
      p.boost !== undefined ||
      p.movement !== undefined ||
      p.positioning !== undefined,
  );

  const displayPlayerName = (p: ReplayPlayerStats) =>
    isLocal(p, localPlayerIds) ? 'You' : p.name;

  const kickerText = kickerPrefix
    ? `${kickerPrefix} · ${playlistLabels(effectiveMatch.playlist).name} · FULL BALLCHASING TELEMETRY`
    : `${playlistLabels(effectiveMatch.playlist).name} · ${formatAgo(
        effectiveMatch.stats?.date || effectiveMatch.created_at,
      )} · FULL BALLCHASING TELEMETRY`;

  return (
    <Modal
      isOpen
      onClose={onClose}
      kicker={kickerText}
      title={`${
        effectiveMatch.map_name || effectiveMatch.stats?.map_name || 'Unknown arena'
      } (BLUE ${effectiveMatch.blue_score} – ${effectiveMatch.orange_score} ORANGE)`}
      meta={
        <>
          {outcome && (
            <span
              className={`font-extrabold text-sm tracking-t12 text-page px-2 py-px ${OUTCOME_SLAB[outcome]}`}
            >
              {OUTCOME_WORD[outcome]}
            </span>
          )}
          <span className="font-extrabold text-xl leading-none t-font-mono">
            <span className="text-blue-ink">{effectiveMatch.blue_score}</span>
            <span className="text-faint"> – </span>
            <span className="text-orange-ink">{effectiveMatch.orange_score}</span>
          </span>
          {effectiveMatch.stats?.duration ? (
            <span>
              {formatDuration(effectiveMatch.stats.duration)}
              {effectiveMatch.stats.overtime && <span className="text-loss"> OT</span>}
            </span>
          ) : null}
          <span
            className="text-ink-2 normal-case truncate max-w-[280px] t-font-mono"
            title={effectiveMatch.match_guid}
          >
            {effectiveMatch.match_guid}
          </span>
          <button
            type="button"
            onClick={copyGuid}
            className="text-muted hover:text-ink transition-colors duration-120"
          >
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

        {activeTab === 'core' && (
          <MatchCoreOverviewTab
            players={players}
            bluePlayers={bluePlayers}
            orangePlayers={orangePlayers}
            mvpKey={mvpKey}
            inspectedPlayerId={inspectedPlayerId}
            displayPlayerName={displayPlayerName}
            onPlayerClick={onPlayerClick}
          />
        )}

        {activeTab === 'boost' && (
          <MatchBoostTab
            bluePlayers={bluePlayers}
            orangePlayers={orangePlayers}
            hasTelemetry={hasTelemetry}
            displayPlayerName={displayPlayerName}
            onPlayerClick={onPlayerClick}
          />
        )}

        {activeTab === 'movement' && (
          <MatchMovementTab
            bluePlayers={bluePlayers}
            orangePlayers={orangePlayers}
            hasTelemetry={hasTelemetry}
            displayPlayerName={displayPlayerName}
            onPlayerClick={onPlayerClick}
          />
        )}

        {activeTab === 'positioning' && (
          <MatchPositioningTab
            bluePlayers={bluePlayers}
            orangePlayers={orangePlayers}
            hasTelemetry={hasTelemetry}
            displayPlayerName={displayPlayerName}
            onPlayerClick={onPlayerClick}
          />
        )}

        {activeTab === 'teams' && (
          <MatchH2HTab bluePlayers={bluePlayers} orangePlayers={orangePlayers} />
        )}

        {activeTab === 'raw_json' && (
          <MatchRawJsonTab
            rawJsonString={rawJsonString}
            copiedJson={copiedJson}
            onCopyJson={copyJson}
          />
        )}
      </div>
    </Modal>
  );
};
