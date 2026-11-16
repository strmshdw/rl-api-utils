import { useEffect, useMemo, useRef, useState } from 'react';
import type { LobbyPlayer, PlayerDetailResponse, PlayerRoleStatsSummary } from '../types/api';
import { normalizeActiveModeKey } from '../utils/broadcast';

export interface ScopedLivePlayerHistory {
  withYouStats?: PlayerRoleStatsSummary;
  againstYouStats?: PlayerRoleStatsSummary;
  teammateWins: number;
  teammateLosses: number;
  opponentWins: number;
  opponentLosses: number;
  hasDateFilter: boolean;
}

interface ScopedHistoryEntry {
  scopeSig: string;
  byPlayerId: Record<string, ScopedLivePlayerHistory>;
}

const EMPTY_HISTORY_MAP: Record<string, ScopedLivePlayerHistory> = {};
const DEMO_LIVE_MATCH_GUID = 'simulated-demo-live-001';

export function useLiveLobbyHistories(params: {
  matchGuid: string | undefined;
  lobby: LobbyPlayer[];
  localId: string | undefined;
  scopeKey: string;
  dateParams: { from?: string; to?: string };
  enabled: boolean;
  previewMode: boolean;
}): Record<string, ScopedLivePlayerHistory> {
  const { matchGuid, lobby, localId, scopeKey, dateParams, enabled, previewMode } = params;
  const normalizedScope = normalizeActiveModeKey(scopeKey);
  const from = dateParams.from || '';
  const to = dateParams.to || '';

  const targetPlayers = useMemo(
    () =>
      lobby
        .filter((p) => !p.is_bot && Boolean(p.player_id))
        .map((p) => ({
          playerId: p.player_id,
          isLocal: Boolean(p.is_local || (localId && p.player_id === localId)),
        })),
    [lobby, localId],
  );

  const playersKey = useMemo(
    () => targetPlayers.map((p) => `${p.isLocal ? '1' : '0'}:${p.playerId}`).join(';'),
    [targetPlayers],
  );

  const scopeSig = `${matchGuid || ''}|${normalizedScope}|${from}|${to}|${playersKey}`;
  const [entry, setEntry] = useState<ScopedHistoryEntry | null>(null);
  const seqRef = useRef(0);

  useEffect(() => {
    if (
      !enabled ||
      previewMode ||
      !matchGuid ||
      matchGuid === DEMO_LIVE_MATCH_GUID ||
      targetPlayers.length === 0
    ) {
      return;
    }

    const seq = ++seqRef.current;
    const controller = new AbortController();
    const qs = new URLSearchParams({ playlist: normalizedScope });
    if (from) qs.set('from', from);
    if (to) qs.set('to', to);
    const queryStr = qs.toString();
    const hasDateFilter = Boolean(from || to);

    Promise.all(
      targetPlayers.map(async ({ playerId, isLocal }) => {
        const routeId = isLocal ? 'me' : encodeURIComponent(playerId);
        try {
          const res = await fetch(`/api/players/${routeId}?${queryStr}`, {
            signal: controller.signal,
          });
          if (!res.ok) return null;
          const detail: PlayerDetailResponse = await res.json();
          const record: ScopedLivePlayerHistory = {
            withYouStats: detail.with_you_stats,
            againstYouStats: detail.against_you_stats,
            teammateWins: detail.teammate_wins ?? detail.with_you_stats?.wins ?? 0,
            teammateLosses: detail.teammate_losses ?? detail.with_you_stats?.losses ?? 0,
            opponentWins: detail.opponent_wins ?? detail.against_you_stats?.wins ?? 0,
            opponentLosses: detail.opponent_losses ?? detail.against_you_stats?.losses ?? 0,
            hasDateFilter,
          };
          return [playerId, record] as const;
        } catch {
          return null;
        }
      }),
    ).then((pairs) => {
      if (controller.signal.aborted || seq !== seqRef.current) return;
      const byPlayerId: Record<string, ScopedLivePlayerHistory> = {};
      for (const pair of pairs) {
        if (pair) {
          byPlayerId[pair[0]] = pair[1];
        }
      }
      setEntry({ scopeSig, byPlayerId });
    });

    return () => {
      controller.abort();
    };
  }, [enabled, previewMode, matchGuid, normalizedScope, from, to, scopeSig, targetPlayers]);

  return entry && entry.scopeSig === scopeSig ? entry.byPlayerId : EMPTY_HISTORY_MAP;
}
