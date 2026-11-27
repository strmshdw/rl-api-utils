import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { HistoricalMatchRecord, ReplayPlayerStats } from '../../types/api';
import { Modal } from '../common/Modal';
import { OUTCOME_SLAB, formatAgo, formatArenaName, playlistLabels } from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { dedupeTeamPlayers, historyOutcome, isLocal, replayStatus } from './historyData';
import { MatchCoreOverviewTab } from './MatchCoreOverviewTab';
import {
  MatchBoostTab,
  MatchH2HTab,
  MatchMovementTab,
  MatchPositioningTab,
  MatchRawJsonTab,
} from './MatchTelemetryTabs';
import { MatchReplayTab } from '../replay/MatchReplayTab';

interface HistoricalMatchDetailModalProps {
  match: HistoricalMatchRecord | null;
  onClose: () => void;
  localPlayerIds?: Set<string>;
  onPlayerClick?: (playerId: string, playerName?: string) => void;
  inspectedPlayerId?: string;
  kickerPrefix?: string;
  initialTab?: TelemetryTab;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;
const NO_IDS = new Set<string>();

export type TelemetryTab =
  | 'core'
  | 'boost'
  | 'movement'
  | 'positioning'
  | 'teams'
  | 'replay'
  | 'raw_json';

export const TELEMETRY_TABS: ReadonlyArray<readonly [TelemetryTab, string]> = [
  ['core', '1. CORE & MECHANICS'],
  ['boost', '2. BOOST ECONOMY & PADS'],
  ['movement', '3. SPEED & MOVEMENT'],
  ['positioning', '4. POSITIONING & ROTATION'],
  ['teams', '5. TEAM HEAD-TO-HEAD'],
  ['replay', '▶ 3D REPLAY'],
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
  initialTab = 'core',
}) => {
  const [activeTab, setActiveTab] = useState<TelemetryTab>(initialTab);
  const [theaterMode, setTheaterMode] = useState(false);
  const [copiedGuid, setCopiedGuid] = useState(false);
  const [copiedJson, setCopiedJson] = useState(false);
  const [fetchedMatch, setFetchedMatch] = useState<HistoricalMatchRecord | null>(null);
  const replayEscapeGuardRef = useRef<((event: Event) => boolean) | null>(null);
  const tabButtonRefs = useRef<Partial<Record<TelemetryTab, HTMLButtonElement | null>>>({});
  const fetchedGuidRef = useRef<string | null>(null);
  const syncedGuidRef = useRef<string | null>(match?.match_guid ?? null);

  useEffect(() => {
    setActiveTab(initialTab);
    setTheaterMode(false);
    fetchedGuidRef.current = null;
    syncedGuidRef.current = match?.match_guid ?? null;
  }, [match?.match_guid, initialTab]);

  useEffect(() => {
    if (activeTab !== 'replay' && theaterMode) {
      setTheaterMode(false);
    }
  }, [activeTab, theaterMode]);

  // Keep the active tab button scrolled cleanly into view (especially at 1280x720 XL).
  useEffect(() => {
    const btn = tabButtonRefs.current[activeTab];
    if (btn && typeof btn.scrollIntoView === 'function') {
      btn.scrollIntoView({ inline: 'nearest', block: 'nearest' });
    }
  }, [activeTab, match?.match_guid, theaterMode]);

  const handleRegisterEscapeGuard = useCallback((guard: ((event: Event) => boolean) | null) => {
    replayEscapeGuardRef.current = guard;
  }, []);

  const handleModalEscape = useCallback(
    (e: Event) => {
      if (activeTab !== 'replay') return false;
      if (replayEscapeGuardRef.current?.(e)) {
        return true;
      }
      if (theaterMode) {
        e.preventDefault();
        e.stopPropagation();
        setTheaterMode(false);
        return true;
      }
      return false;
    },
    [activeTab, theaterMode],
  );

  useEffect(() => {
    if (!match?.match_guid) {
      setFetchedMatch(null);
      return;
    }
    // Defer match-stats hydration while viewing the 3D Replay tab directly so
    // session-only matches opened straight to 3D Replay don't trigger a 404.
    const effectiveInitialTab =
      syncedGuidRef.current !== match.match_guid ? initialTab : activeTab;
    if (effectiveInitialTab === 'replay') {
      return;
    }
    if (fetchedGuidRef.current === match.match_guid) {
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
    fetchedGuidRef.current = match.match_guid;
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
  }, [match, activeTab]);

  const effectiveMatch =
    fetchedMatch && fetchedMatch.match_guid === match?.match_guid ? fetchedMatch : match;
  const outcome = effectiveMatch ? historyOutcome(effectiveMatch, localPlayerIds) : null;
  const players: ReplayPlayerStats[] = effectiveMatch?.stats?.players ?? [];
  const status = effectiveMatch ? replayStatus(effectiveMatch) : { label: '', ink: '' };

  const bluePlayers = useMemo(
    () => dedupeTeamPlayers(players, 0, localPlayerIds),
    [players, localPlayerIds],
  );
  const orangePlayers = useMemo(
    () => dedupeTeamPlayers(players, 1, localPlayerIds),
    [players, localPlayerIds],
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

  const rawArena = effectiveMatch.map_name || effectiveMatch.stats?.map_name;
  const arenaTitle = rawArena ? formatArenaName(rawArena) : 'Unknown arena';
  const matchTitle = `${arenaTitle} (BLUE ${effectiveMatch.blue_score} – ${effectiveMatch.orange_score} ORANGE)`;
  const isReplayTab = activeTab === 'replay';
  const isTheater = isReplayTab && theaterMode;

  return (
    <Modal
      isOpen
      onClose={onClose}
      kicker={kickerText}
      title={matchTitle}
      maxWidth={isReplayTab ? 1560 : 1180}
      fullViewport={isTheater}
      compactHeader={isTheater}
      tightPadding={isReplayTab && !isTheater}
      viewportBounded={isReplayTab && !isTheater}
      onEscape={handleModalEscape}
      headerActions={
        !isReplayTab ? (
          <button
            type="button"
            data-testid="modal-watch-replay-btn"
            onClick={() => setActiveTab('replay')}
            className="px-3.5 py-1.5 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-sm uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer shrink-0"
          >
            ▶ WATCH 3D REPLAY
          </button>
        ) : undefined
      }
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
      <div className={`flex flex-col font-display ${isReplayTab ? 'flex-1 min-h-0 overflow-hidden' : ''}`}>
        {!isTheater && (
          <div
            className={
              isReplayTab
                ? 'dashboard-nav-scroll px-4 pt-1.5 border-b t-border border-line flex flex-nowrap overflow-x-auto gap-3 lg:gap-4 text-[12px] leading-tight tracking-wide font-extrabold shrink-0 whitespace-nowrap'
                : 'px-7 pt-3.5 border-b t-border border-line flex flex-wrap gap-6 text-sm font-extrabold'
            }
          >
            {TELEMETRY_TABS.map(([id, label]) => (
              <button
                key={id}
                ref={(el) => {
                  tabButtonRefs.current[id] = el;
                }}
                type="button"
                data-testid={`telemetry-tab-${id}`}
                onClick={() => setActiveTab(id)}
                className={`${
                  isReplayTab ? 'pb-1.5 shrink-0 whitespace-nowrap' : 'pb-3'
                } border-b-[3px] uppercase transition-colors cursor-pointer ${
                  activeTab === id
                    ? 'border-current t-text-strong text-ink-strong'
                    : id === 'replay'
                    ? 'border-transparent text-blue-ink hover:text-ink-strong'
                    : 'border-transparent t-text-muted text-muted hover:text-ink'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
        )}

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
            playlist={effectiveMatch.playlist}
          />
        )}

        {activeTab === 'teams' && (
          <MatchH2HTab bluePlayers={bluePlayers} orangePlayers={orangePlayers} />
        )}

        {activeTab === 'replay' && (
          <MatchReplayTab
            match={effectiveMatch}
            matchTitle={matchTitle}
            theaterMode={theaterMode}
            onToggleTheaterMode={() => setTheaterMode((v) => !v)}
            onRegisterEscapeGuard={handleRegisterEscapeGuard}
          />
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
