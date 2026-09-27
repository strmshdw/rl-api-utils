import React, { useEffect, useState } from 'react';
import { PlayerDetailResponse, PlayerPlaylistRank, PlayerRanksSnapshot } from '../../types/api';
import { Modal } from '../common/Modal';
import { parsePlatform } from '../../utils/platforms';
import { formatMMR, formatRelativeTime, formatWinRate, getRankTierColor } from '../../utils/formatters';
import { Shield, Swords, Calendar, AlertCircle } from 'lucide-react';

interface PlayerProfileModalProps {
  playerId: string | null;
  onClose: () => void;
}

export const PlayerProfileModal: React.FC<PlayerProfileModalProps> = ({ playerId, onClose }) => {
  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!playerId) {
      setProfile(null);
      return;
    }

    const abortController = new AbortController();
    setIsLoading(true);
    setError(null);

    fetch(`/api/players/${encodeURIComponent(playerId)}`, { signal: abortController.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}: Failed to load player`);
        return res.json();
      })
      .then((data) => setProfile(data))
      .catch((err) => {
        if (!abortController.signal.aborted) {
          setError(err instanceof Error ? err.message : 'Error fetching profile');
        }
      })
      .finally(() => {
        if (!abortController.signal.aborted) setIsLoading(false);
      });

    return () => abortController.abort();
  }, [playerId]);

  if (!playerId) return null;

  const plat = profile ? parsePlatform(profile.platform || profile.player_id) : null;

  // Parse ranks_json
  let ranksList: PlayerPlaylistRank[] = [];
  try {
    if (profile?.ranks_json && profile.ranks_json !== '{}') {
      const ranksObj: PlayerRanksSnapshot = JSON.parse(profile.ranks_json);
      ranksList = Object.values(ranksObj);
    }
  } catch {
    // Ignore invalid JSON
  }

  return (
    <Modal isOpen={Boolean(playerId)} onClose={onClose} title="Player Profile & Matchup History" maxWidth="max-w-4xl">
      {isLoading ? (
        <div className="flex flex-col items-center justify-center p-12 text-slate-400">
          <div className="w-8 h-8 border-2 border-cyan-500 border-t-transparent rounded-full animate-spin mb-3" />
          <p className="text-sm">Loading player telemetry...</p>
        </div>
      ) : error ? (
        <div className="rounded-xl border border-rose-800/60 bg-rose-950/20 p-4 text-rose-300 flex items-center gap-3">
          <AlertCircle className="w-5 h-5 flex-shrink-0" />
          <p className="text-sm">{error}</p>
        </div>
      ) : profile ? (
        <div className="space-y-6">
          {/* Header Card */}
          <div className="rounded-xl border border-slate-800 bg-slate-950/80 p-5">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
              <div>
                <div className="flex items-center gap-3">
                  <h3 className="text-2xl font-black text-white">{profile.player_name || 'Player Profile'}</h3>
                  {plat && (
                    <span className={`text-xs font-bold px-2.5 py-0.5 rounded border ${plat.badgeClass}`}>
                      {plat.name}
                    </span>
                  )}
                </div>
                <div className="text-xs text-slate-500 font-mono mt-1">{profile.player_id}</div>
              </div>
              <div className="flex items-center gap-4 text-xs text-slate-400">
                <div className="flex items-center gap-1.5">
                  <Calendar className="w-4 h-4 text-slate-500" />
                  <span>First seen: {new Date(profile.first_seen_at).toLocaleDateString()}</span>
                </div>
                <span>•</span>
                <div>Last seen: {formatRelativeTime(profile.last_seen_at)}</div>
              </div>
            </div>
          </div>

          {/* Competitive Ranks Grid */}
          {ranksList.length > 0 && (
            <div>
              <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-3 flex items-center gap-2">
                <Shield className="w-4 h-4 text-cyan-400" />
                Competitive Ranks Snapshot
              </h4>
              <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3">
                {ranksList.map((r) => (
                  <div
                    key={r.playlist_id}
                    className={`rounded-xl border p-3 bg-slate-950/60 ${getRankTierColor(r.tier)}`}
                  >
                    <div className="text-[11px] font-semibold text-slate-300 mb-0.5">{r.playlist_name}</div>
                    <div className="text-sm font-black">{r.rank_name}</div>
                    <div className="text-xs font-mono mt-1 opacity-90">{formatMMR(r.mmr)} MMR</div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Per-Playlist Matchup Table */}
          <div>
            <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-3 flex items-center gap-2">
              <Swords className="w-4 h-4 text-amber-500" />
              Playlist Matchup Breakdown
            </h4>
            {profile.matchups && profile.matchups.length > 0 ? (
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 overflow-hidden">
                <table className="w-full text-left text-xs">
                  <thead className="bg-slate-900 text-slate-400 uppercase tracking-wider border-b border-slate-800">
                    <tr>
                      <th className="py-2.5 px-4">Playlist ID</th>
                      <th className="py-2.5 px-3">As Teammate</th>
                      <th className="py-2.5 px-3">As Opponent</th>
                      <th className="py-2.5 px-3 text-right">Total Encounters</th>
                      <th className="py-2.5 px-4 text-right">Last Encounter</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-800/60">
                    {profile.matchups.map((m) => {
                      const mateRate = formatWinRate(m.wins_as_teammate, m.losses_as_teammate);
                      const oppRate = formatWinRate(m.wins_as_opponent, m.losses_as_opponent);

                      return (
                        <tr key={m.playlist_id} className="hover:bg-slate-900/40">
                          <td className="py-3 px-4 font-semibold text-slate-200">
                            Playlist {m.playlist_id}
                          </td>
                          <td className="py-3 px-3">
                            <span className="font-semibold text-cyan-300">
                              {m.wins_as_teammate}W - {m.losses_as_teammate}L
                            </span>{' '}
                            <span className="text-slate-500 font-mono">({mateRate})</span>
                          </td>
                          <td className="py-3 px-3">
                            <span className="font-semibold text-amber-400">
                              {m.wins_as_opponent}W - {m.losses_as_opponent}L
                            </span>{' '}
                            <span className="text-slate-500 font-mono">({oppRate})</span>
                          </td>
                          <td className="py-3 px-3 text-right font-mono font-bold text-slate-300">
                            {m.total_matches}
                          </td>
                          <td className="py-3 px-4 text-right text-slate-400">
                            {formatRelativeTime(m.last_played_at)}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-6 text-center text-slate-500 text-xs">
                No recorded playlist matchups for this player.
              </div>
            )}
          </div>
        </div>
      ) : null}
    </Modal>
  );
};
