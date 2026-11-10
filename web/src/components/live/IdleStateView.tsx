import React, { useMemo, useState } from 'react';
import { ConnectionStatus } from '../../hooks/useLiveMatch';
import { SessionMatchDetail, SessionResponse } from '../../types/api';
import {
  chronological,
  deltaInk,
  findMvp,
  formatAgo,
  formatElapsed,
  formatMmr,
  formatSigned,
  localTeamOf,
  outcomeOf,
  playlistLabels,
  rosterLine,
  shortRank,
  summarizeSession,
  teamName,
} from '../../utils/broadcast';
import { formatDuration } from '../../utils/formatters';
import { TeamBoxScore, sessionMatchRows } from '../common/BoxScore';
import { EmptyLine, GhostButton, SectionLabel, StatusDot } from '../common/ui';
import { PlaylistRows, SessionForAgainstTable, SessionKpiStrip } from '../session/SessionKpis';
import { ContinueSessionBanner } from '../session/SessionView';
import { useMMRHistory } from '../../hooks/useMMRHistory';
import { useDashboard } from '../../context/DashboardContext';

interface IdleStateViewProps {
  session: SessionResponse | null;
  connection: ConnectionStatus;
  onShowSession: () => void;
  onContinueSession?: (sessionId?: string) => void;
  onDismissContinueSession?: (sessionId?: string) => void;
  onInspectPlayer?: (playerId: string, playerName?: string) => void;
  onOpenMatchTelemetry?: (match: SessionMatchDetail) => void;
}

const OUTCOME_WORD = { W: 'WIN', L: 'LOSS', D: 'DRAW' } as const;

const LastMatch: React.FC<{
  match: SessionMatchDetail;
  onShowSession: () => void;
  onInspectPlayer?: (playerId: string, playerName?: string) => void;
  onOpenMatchTelemetry?: (match: SessionMatchDetail) => void;
}> = ({ match, onShowSession, onInspectPlayer, onOpenMatchTelemetry }) => {
  const outcome = outcomeOf(match.result);
  const localTeam = localTeamOf(match);
  const mvp = findMvp(match.players || [], match.winner_team, match.blue_score, match.orange_score);
  const winner =
    match.winner_team ??
    (match.blue_score > match.orange_score ? 0 : match.orange_score > match.blue_score ? 1 : null);
  const roster = rosterLine(match)
    .split(' vs ')
    .map((side) => side.toUpperCase())
    .join(' vs ');
  const slabText = (team: number) => `${teamName(team)}${team === localTeam ? ' · YOU' : ''}`;

  return (
    <div data-testid="last-match" className="flex flex-col gap-3 pt-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <SectionLabel>LAST MATCH · {formatAgo(match.ended_at)}</SectionLabel>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => onOpenMatchTelemetry?.(match)}
            className="text-sm t-font-display font-bold uppercase tracking-wider t-text-blue hover:underline cursor-pointer"
          >
            OPEN FULL 5-TAB BALLCHASING TELEMETRY →
          </button>
        </div>
      </div>

      <div className="flex flex-wrap items-stretch surface-plaque border border-outline t-radius overflow-hidden">
        <div className="min-h-[64px] flex-[0_1_250px] min-w-[160px] t-bg-blue-slab bg-blue-slab t-on-slab flex items-center justify-between px-4 whitespace-nowrap overflow-hidden font-bold text-lg tracking-wider">
          <span
            className={`px-2 py-0.5 text-xs t-font-mono font-extrabold border ${
              outcome === 'L' ? 'hanko-seal-vermilion' : 'border-current'
            }`}
          >
            {outcome ? OUTCOME_WORD[outcome] : 'FINAL'}
          </span>
          <span>{slabText(0)}</span>
        </div>
        <div className="min-h-[64px] w-[68px] surface-plaque border-x border-line t-text-blue text-blue-ink flex items-center justify-center font-extrabold text-4xl leading-none t-font-mono">
          {match.blue_score}
        </div>
        <div className="min-h-[64px] w-[68px] surface-plaque border-r border-line text-orange-ink flex items-center justify-center font-extrabold text-4xl leading-none t-font-mono">
          {match.orange_score}
        </div>
        <div className="min-h-[64px] flex-[0_1_210px] min-w-[130px] t-bg-orange-slab bg-orange-slab text-white flex items-center pl-4 whitespace-nowrap overflow-hidden font-bold text-lg tracking-wider">
          {slabText(1)}
        </div>
        <div className="flex-[1_1_360px] min-h-[68px] flex flex-wrap items-center justify-between gap-4 px-5 py-2.5 min-w-0">
          <div className="flex flex-col gap-0.5 min-w-0 font-semibold text-sm tracking-t8 text-muted">
            <span className="text-ink font-bold text-base">
              {playlistLabels(match.playlist_id, match.playlist_name).name} ·{' '}
              {formatDuration(match.duration_seconds)}
            </span>
            {roster && <span className="truncate">{roster}</span>}
          </div>
          <div className="flex items-center gap-4 shrink-0">
            <span className={`font-extrabold text-2xl sm:text-3xl t-font-mono ${deltaInk(match.mmr_change)}`}>
              {formatSigned(match.mmr_change)}
            </span>
            <GhostButton tone="ink" onClick={onShowSession}>
              ALL SESSION MATCHES →
            </GhostButton>
          </div>
        </div>
      </div>

      {(match.players?.length ?? 0) > 0 && (
        <div className="grid gap-3 grid-cols-1 lg:grid-cols-2">
          {[0, 1].map((team) => (
            <TeamBoxScore
              key={team}
              team={team as 0 | 1}
              label={`${teamName(team)}${winner === team ? ' · WIN' : ''}`}
              rows={sessionMatchRows(match, team, mvp?.player_id)}
              withRanks
              onPlayerClick={onInspectPlayer}
            />
          ))}
        </div>
      )}
    </div>
  );
};

export const IdleStateView: React.FC<IdleStateViewProps> = ({
  session,
  connection,
  onShowSession,
  onContinueSession,
  onDismissContinueSession,
  onInspectPlayer,
  onOpenMatchTelemetry,
}) => {
  const { setLiveModeOverride, isDevBuild, previewMode } = useDashboard();
  const { byPlaylist: mmrByPlaylist } = useMMRHistory();
  const [showAllModes, setShowAllModes] = useState(false);
  const summary = useMemo(() => summarizeSession(session), [session]);
  const matches = chronological(session?.matches);
  const lastMatch = matches.length ? matches[matches.length - 1] : null;
  const playlists = session?.playlists ? Object.values(session.playlists) : [];
  const dot = connection === 'connected' ? 'bg-win' : connection === 'offline' ? 'bg-loss' : 'bg-faint';
  const mmrPollerState = session?.mmr_poller_state || (connection === 'offline' ? 'OFFLINE' : 'READY');

  const corePlaylists = useMemo(() => {
    const localRanks = session?.active_match?.local_player?.ranks || {};
    const useSampleFallbacks = isDevBuild && previewMode;
    const specs = [
      { id: 13, label: 'RANKED 3V3 (STANDARD)', defaultMmr: 1460, defaultRank: 'Grand Champion I', defaultDiv: 1 },
      { id: 11, label: 'RANKED 2V2 (DOUBLES)', defaultMmr: 1485, defaultRank: 'Grand Champion I', defaultDiv: 2 },
      { id: 10, label: 'RANKED 1V1 (DUEL)', defaultMmr: 1120, defaultRank: 'Champion II', defaultDiv: 2 },
    ];
    return specs.map((spec) => {
      const plStat = session?.playlists?.[String(spec.id)];
      const rInfo = localRanks[String(spec.id)];
      const mmr = plStat?.current_mmr || rInfo?.mmr || (useSampleFallbacks ? spec.defaultMmr : 0);
      const rankName = rInfo?.rank_name || (useSampleFallbacks ? spec.defaultRank : mmr > 0 ? 'RANKED' : 'UNRANKED');
      const div = rInfo?.division ?? (useSampleFallbacks ? spec.defaultDiv : undefined);
      const delta = plStat?.mmr_delta ?? 0;
      const games = plStat?.matches_played ?? rInfo?.matches_played ?? 0;
      return {
        id: spec.id,
        label: spec.label,
        shortRank: shortRank(rankName, div),
        mmr: formatMmr(mmr),
        delta,
        games,
      };
    });
  }, [session, isDevBuild, previewMode]);

  const extraPlaylists = useMemo(() => {
    const localRanks = session?.active_match?.local_player?.ranks || {};
    const useSampleFallbacks = isDevBuild && previewMode;
    const specs = [
      { id: 27, label: 'HOOPS (2V2)', sampleRank: 'CHAMPION II · DIV III', sampleMmr: '1165' },
      { id: 28, label: 'RUMBLE (3V3)', sampleRank: 'GRAND CHAMPION I · DIV I', sampleMmr: '1290' },
      { id: 29, label: 'DROPSHOT (3V3)', sampleRank: 'DIAMOND III · DIV IV', sampleMmr: '985' },
      { id: 30, label: 'SNOW DAY (3V3)', sampleRank: 'CHAMPION I · DIV II', sampleMmr: '1040' },
      { id: 34, label: 'TOURNAMENTS (3V3)', sampleRank: 'GRAND CHAMPION I · DIV III', sampleMmr: '1510' },
    ];
    return specs.map((spec) => {
      const plStat = session?.playlists?.[String(spec.id)];
      const rInfo = localRanks[String(spec.id)];
      const mmr = plStat?.current_mmr || rInfo?.mmr;
      if (mmr && mmr > 0) {
        return {
          id: spec.id,
          label: spec.label,
          rank: shortRank(rInfo?.rank_name || 'RANKED', rInfo?.division),
          mmr: formatMmr(mmr),
        };
      }
      return {
        id: spec.id,
        label: spec.label,
        rank: useSampleFallbacks ? spec.sampleRank : 'UNRANKED',
        mmr: useSampleFallbacks ? spec.sampleMmr : '—',
      };
    });
  }, [session, isDevBuild, previewMode]);

  return (
    <div data-testid="live-idle" className="font-display flex flex-col gap-4">
      {/* Compact Status Bar (No giant WAITING FOR KICKOFF banner) */}
      <div
        data-testid="daemon-status"
        className="surface-plaque border border-line t-radius px-4 py-2 flex flex-wrap items-center justify-between gap-3"
      >
        <div className="flex flex-wrap items-center gap-3 text-xs font-semibold tracking-wider text-muted">
          <div className="flex items-center gap-2">
            <StatusDot className={dot} size={8} />
            <span className="t-font-display font-bold text-xs uppercase tracking-widest t-text-strong">
              IDLE · QUEUE READY
            </span>
          </div>
          <span className="t-text-faint">|</span>
          <span className="flex items-center gap-1.5">
            <StatusDot className={dot} />
            STATS API :49124
          </span>
          <span className="t-text-faint">|</span>
          <span className="flex items-center gap-1.5">
            <StatusDot className={dot} />
            RANK QUERIES {connection === 'offline' ? 'OFFLINE' : 'READY'}
          </span>
          <span className="t-text-faint hidden sm:inline">|</span>
          <span className="hidden sm:flex items-center gap-1.5">
            <StatusDot className={dot} />
            POST-GAME MMR POLLER: <strong className="t-text-strong">{mmrPollerState}</strong>
          </span>
        </div>

        {isDevBuild && previewMode && (
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setLiveModeOverride('match')}
              className="px-2.5 py-0.5 t-bg-page border border-outline t-font-display font-bold text-xs uppercase tracking-wider t-text-strong hover:opacity-80 t-radius"
            >
              PREVIEW IN-MATCH TELEMETRY →
            </button>
          </div>
        )}
      </div>

      {session?.continuable_session && (
        <ContinueSessionBanner
          continuableSession={session.continuable_session}
          onContinue={(id) => onContinueSession?.(id)}
          onDismiss={(id) => onDismissContinueSession?.(id)}
        />
      )}

      {/* Shared Session KPI Strip + Shared-Header FOR / AGAINST Historical Split Table */}
      {session && (
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)] gap-4">
            <div className="flex flex-col gap-2 min-w-0">
              <SectionLabel>THIS SESSION · {formatElapsed(session.started_at)}</SectionLabel>
              <SessionKpiStrip kpis={summary.kpis} variant="idle" session={session} />
            </div>
            {playlists.length > 0 && (
              <div className="flex flex-col gap-2 min-w-0">
                <SectionLabel>PLAYLIST MMR PROGRESSION</SectionLabel>
                <PlaylistRows playlists={playlists} mmrHistory={mmrByPlaylist} />
              </div>
            )}
          </div>
          <SessionForAgainstTable session={session} />

          {/* Top-3 Core Playlist Rank Cards (3v3, 2v2, 1v1) + ALL 8 MODES Drawer */}
          <div data-testid="idle-core-ranks" className="surface-plaque border border-outline p-4 flex flex-col gap-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <SectionLabel>KNOWN PLAYLIST RANKS (CORE 3V3 · 2V2 · 1V1)</SectionLabel>
              <button
                type="button"
                data-testid="toggle-all-8-modes"
                onClick={() => setShowAllModes((v) => !v)}
                className="text-xs font-extrabold t-text-strong hover:underline uppercase cursor-pointer"
              >
                {showAllModes ? 'HIDE EXTRA MODES ▲' : 'VIEW FULL PLAYLIST STATS & EXTRA MODES (8) ↗'}
              </button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3 t-num">
              {corePlaylists.map((cp) => (
                <div
                  key={cp.id}
                  className="t-bg-page bg-page border border-line px-4 py-3 flex items-center justify-between gap-3"
                >
                  <div className="min-w-0">
                    <div className="text-sm font-extrabold text-muted uppercase">{cp.label}</div>
                    <div className="t-font-display font-extrabold text-xl t-text-strong text-ink-strong uppercase truncate">
                      {cp.shortRank}
                    </div>
                  </div>
                  <div className="text-right shrink-0">
                    <div className="t-font-mono font-extrabold text-xl t-text-blue text-blue-ink">
                      {cp.mmr} MMR
                    </div>
                    <div className={`text-sm font-extrabold t-font-mono ${deltaInk(cp.delta)}`}>
                      {cp.delta !== 0 ? `${formatSigned(cp.delta)} SESSION` : `${cp.games}G IN SESSION`}
                    </div>
                  </div>
                </div>
              ))}
            </div>
            {showAllModes && (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-2.5 pt-2 border-t t-border border-line t-num">
                {extraPlaylists.map((ep) => (
                  <div
                    key={ep.id}
                    className="t-bg-page bg-page t-border border border-line px-3.5 py-2.5 flex items-center justify-between gap-2"
                  >
                    <div className="min-w-0">
                      <div className="text-sm font-extrabold text-muted uppercase">{ep.label}</div>
                      <div className="font-bold text-sm t-text-strong text-ink-strong uppercase truncate">
                        {ep.rank}
                      </div>
                    </div>
                    <div className="t-font-mono font-extrabold text-base t-text-blue text-blue-ink shrink-0">
                      {ep.mmr}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Last Match Strip + 2-Team Box Score */}
      {lastMatch ? (
        <LastMatch
          match={lastMatch}
          onShowSession={onShowSession}
          onInspectPlayer={onInspectPlayer}
          onOpenMatchTelemetry={onOpenMatchTelemetry}
        />
      ) : (
        <EmptyLine>NO MATCHES THIS SESSION YET</EmptyLine>
      )}
    </div>
  );
};

