import React, { useEffect, useState } from 'react';
import { PlayerDetailResponse } from '../../types/api';
import { Modal } from '../common/Modal';
import { COLUMN_HEADER, PANEL_LABEL, RecordCells, SectionLabel } from '../common/ui';
import {
  formatAgo,
  formatMmr,
  formatPct,
  formatRecord,
  formatShortDate,
  platformLabel,
  playlistLabels,
  recordFill,
  recordInk,
  trackerUrl,
} from '../../utils/broadcast';
import { h2hTag, parseRanks, sumMatchups } from './playerData';

interface PlayerProfileDialogProps {
  playerId: string | null;
  /** Name from the directory row, shown while the profile loads. */
  playerName?: string;
  onClose: () => void;
}

const PLAYLIST_GRID = 'minmax(0,1.2fr) minmax(0,1fr) minmax(0,1fr) 70px 120px';

const H2HBlock: React.FC<{ label: string; wins: number; losses: number; teammate: boolean }> = ({ label, wins, losses, teammate }) => {
  const games = wins + losses;
  const tag = h2hTag(wins, losses, teammate);
  return (
    <div data-testid={teammate ? 'h2h-with' : 'h2h-vs'} className="bg-panel px-7 py-[22px] flex flex-col gap-3.5 min-w-0">
      <div className="flex justify-between items-center gap-3">
        <SectionLabel>
          {label} · {games} {games === 1 ? 'GAME' : 'GAMES'}
        </SectionLabel>
        {tag && (
          <span className={`font-bold text-[12px] tracking-t14 text-page px-2 py-0.5 whitespace-nowrap ${recordFill(wins, losses)}`}>{tag}</span>
        )}
      </div>
      <div className="flex items-baseline justify-between gap-3">
        <span className={`font-extrabold text-[64px] leading-tightest ${recordInk(wins, losses)}`}>{formatRecord(wins, losses)}</span>
        <span className="font-bold text-[30px] text-muted">{formatPct(wins, losses)}</span>
      </div>
      <RecordCells wins={wins} losses={losses} count={10} cellClass="flex-1 h-[14px]" />
    </div>
  );
};

const RecordWithPct: React.FC<{ wins: number; losses: number }> = ({ wins, losses }) => (
  <span className={`font-bold text-[20px] ${recordInk(wins, losses)}`}>
    {formatRecord(wins, losses)} <span className="text-[14px] text-muted font-semibold">{formatPct(wins, losses)}</span>
  </span>
);

export const PlayerProfileDialog: React.FC<PlayerProfileDialogProps> = ({ playerId, playerName, onClose }) => {
  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    setProfile(null);
    setCopied(false);
    if (!playerId) return;

    const controller = new AbortController();
    setIsLoading(true);
    setError(null);
    fetch(`/api/players/${encodeURIComponent(playerId)}`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to load player`);
        return res.json();
      })
      .then((data: PlayerDetailResponse) => setProfile(data))
      .catch((err) => {
        if (!controller.signal.aborted) setError(err instanceof Error ? err.message : 'Error fetching profile');
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoading(false);
      });
    return () => controller.abort();
  }, [playerId]);

  if (!playerId) return null;

  const id = profile?.player_id ?? playerId;
  const name = profile?.player_name || playerName || 'PLAYER';
  const platform = profile?.platform || id;
  const tracker = trackerUrl(id, platform, name);
  const ranks = parseRanks(profile?.ranks_json);
  const totals = sumMatchups(profile?.matchups);

  const copyId = () => {
    try {
      navigator.clipboard?.writeText(id);
    } catch {
      // Clipboard unavailable (insecure context); the label still confirms the attempt.
    }
    setCopied(true);
  };

  return (
    <Modal
      isOpen
      onClose={onClose}
      kicker={`PLAYER PROFILE · ${platformLabel(platform)}`}
      title={name}
      meta={
        <>
          <span className="text-ink-2 normal-case break-all">{id}</span>
          <button type="button" onClick={copyId} className="text-muted hover:text-ink transition-colors duration-120">
            {copied ? 'COPIED ✓' : 'COPY ID'}
          </button>
          {tracker && (
            <a href={tracker} target="_blank" rel="noopener noreferrer" className="text-ink hover:text-blue-ink transition-colors duration-120">
              RL TRACKER ↗
            </a>
          )}
        </>
      }
    >
      {error ? (
        <p className="px-8 py-6 font-semibold text-[15px] tracking-t8 text-loss uppercase">COULD NOT LOAD PROFILE · {error}</p>
      ) : !profile ? (
        <p className="px-8 py-6 font-semibold text-[15px] tracking-t8 text-muted">{isLoading ? 'LOADING PROFILE…' : ''}</p>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-[2px] bg-page">
            <H2HBlock label="WITH YOU" wins={totals.mate.wins} losses={totals.mate.losses} teammate />
            <H2HBlock label="VS YOU" wins={totals.vs.wins} losses={totals.vs.losses} teammate={false} />
          </div>

          <div className="px-8 pt-6 pb-2 flex flex-col gap-3">
            <SectionLabel>RANKS</SectionLabel>
            {ranks.length === 0 ? (
              <p className="font-semibold text-[15px] tracking-t8 text-muted">NO RANK DATA RECORDED</p>
            ) : (
              <div className="grid gap-2 grid-cols-[repeat(auto-fill,minmax(200px,1fr))]">
                {ranks.map((r) => (
                  <div key={r.playlist_id} className="bg-panel px-[18px] py-3.5 flex justify-between items-end gap-3 min-w-0">
                    <div className="flex flex-col gap-0.5 min-w-0">
                      <span className={PANEL_LABEL}>{playlistLabels(r.playlist_id, r.playlist_name).mode}</span>
                      <span className="font-bold text-[22px] tracking-t3 uppercase truncate">{r.rank_name || 'UNRANKED'}</span>
                    </div>
                    <span className="font-bold text-[22px] text-ink-2">{formatMmr(r.mmr)}</span>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="px-8 pt-6 pb-7 flex flex-col gap-2">
            <SectionLabel className="pb-1">BY PLAYLIST</SectionLabel>
            {profile.matchups?.length ? (
              <>
                <div className={`grid px-[18px] ${COLUMN_HEADER}`} style={{ gridTemplateColumns: PLAYLIST_GRID }}>
                  <span>PLAYLIST</span>
                  <span>WITH YOU</span>
                  <span>VS YOU</span>
                  <span className="text-right">GAMES</span>
                  <span className="text-right">LAST MET</span>
                </div>
                {profile.matchups.map((m) => (
                  <div
                    key={m.playlist_id}
                    data-testid="profile-playlist-row"
                    className="grid items-center h-[52px] bg-panel px-[18px]"
                    style={{ gridTemplateColumns: PLAYLIST_GRID }}
                  >
                    <span className="font-bold text-[18px] tracking-t6 truncate">{playlistLabels(m.playlist_id).name}</span>
                    <RecordWithPct wins={m.wins_as_teammate} losses={m.losses_as_teammate} />
                    <RecordWithPct wins={m.wins_as_opponent} losses={m.losses_as_opponent} />
                    <span className="text-right font-bold text-[20px]">{m.total_matches}</span>
                    <span className="text-right font-semibold text-[14px] tracking-t8 text-muted">{formatAgo(m.last_played_at)}</span>
                  </div>
                ))}
              </>
            ) : (
              <p className="font-semibold text-[15px] tracking-t8 text-muted">NO RECORDED MATCHUPS</p>
            )}
          </div>

          <div className="flex justify-between gap-4 px-8 py-3.5 border-t border-line font-semibold text-[13px] tracking-t12 text-muted-2">
            <span>FIRST SEEN {formatShortDate(profile.first_seen_at)}</span>
            <span>LAST SEEN {formatAgo(profile.last_seen_at)}</span>
          </div>
        </>
      )}
    </Modal>
  );
};
