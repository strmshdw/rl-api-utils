import React, { useEffect, useState, useMemo } from 'react';
import { PlayerDetailResponse, PlayerPlaylistRank, PlayerRanksSnapshot } from '../../types/api';
import { Modal } from '../common/Modal';
import { parsePlatform } from '../../utils/platforms';
import { formatMMR, formatRelativeTime, formatWinRate, getRankTierColor } from '../../utils/formatters';
import { Shield, Swords, Calendar, AlertCircle, Copy, Check, ExternalLink, Users } from 'lucide-react';

interface PlayerProfileModalProps {
  playerId: string | null;
  onClose: () => void;
}

// Friendly playlist names dictionary
const PLAYLIST_NAMES: Record<number, string> = {
  1: 'Casual Duel (1v1)',
  2: 'Casual Doubles (2v2)',
  3: 'Casual Standard (3v3)',
  4: 'Casual Chaos (4v4)',
  10: 'Ranked Duel (1v1)',
  11: 'Ranked Doubles (2v2)',
  12: 'Ranked Solo Standard (3v3)',
  13: 'Ranked Standard (3v3)',
  27: 'Hoops',
  28: 'Rumble',
  29: 'Dropshot',
  30: 'Snow Day',
  34: 'Tournament',
};

export const PlayerProfileModal: React.FC<PlayerProfileModalProps> = ({ playerId, onClose }) => {
  const [profile, setProfile] = useState<PlayerDetailResponse | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<boolean>(false);

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

  const handleCopyId = () => {
    if (!profile) return;
    navigator.clipboard.writeText(profile.player_id);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Compute aggregate synergy metrics
  const synergyStats = useMemo(() => {
    if (!profile?.matchups || profile.matchups.length === 0) return null;

    let winsAsMate = 0;
    let lossesAsMate = 0;
    let winsAsOpp = 0;
    let lossesAsOpp = 0;
    let totalEncounters = 0;

    profile.matchups.forEach((m) => {
      winsAsMate += m.wins_as_teammate;
      lossesAsMate += m.losses_as_teammate;
      winsAsOpp += m.wins_as_opponent;
      lossesAsOpp += m.losses_as_opponent;
      totalEncounters += m.total_matches;
    });

    const mateTotal = winsAsMate + lossesAsMate;
    const mateRate = mateTotal > 0 ? (winsAsMate / mateTotal) * 100 : 0;

    const oppTotal = winsAsOpp + lossesAsOpp;
    const oppRate = oppTotal > 0 ? (winsAsOpp / oppTotal) * 100 : 0;

    return {
      winsAsMate,
      lossesAsMate,
      mateTotal,
      mateRate,
      winsAsOpp,
      lossesAsOpp,
      oppTotal,
      oppRate,
      totalEncounters,
    };
  }, [profile]);

  if (!playerId) return null;

  const plat = profile ? parsePlatform(profile.platform || profile.player_id) : null;

  // External tracker URL builder (with direct Steam64 ID support)
  const trackerPlatform = plat?.code === 'epic' ? 'epic' : plat?.code === 'steam' ? 'steam' : plat?.code === 'psn' ? 'psn' : plat?.code === 'xbox' ? 'xbl' : null;
  const trackerIdentifier = plat?.code === 'steam'
    ? (profile?.player_id.split('|')[1] || profile?.player_name)
    : profile?.player_name;
  const trackerUrl = trackerPlatform && trackerIdentifier
    ? `https://rocketleague.tracker.network/rocket-league/profile/${trackerPlatform}/${encodeURIComponent(trackerIdentifier)}/overview`
    : null;

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
    <Modal isOpen={Boolean(playerId)} onClose={onClose} title="Player Scouting & Synergy Dossier" maxWidth="max-w-4xl">
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
          <div className="rounded-xl border border-slate-800 bg-slate-950/80 p-5 shadow-lg">
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
                <div className="flex items-center gap-2 text-xs text-slate-500 font-mono mt-1">
                  <span>{profile.player_id}</span>
                  <button
                    onClick={handleCopyId}
                    className="flex items-center gap-1 text-slate-400 hover:text-cyan-300 transition-colors ml-1"
                    title="Copy full player ID"
                  >
                    {copied ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
                    <span>{copied ? 'Copied' : 'Copy'}</span>
                  </button>
                  {trackerUrl && (
                    <>
                      <span>•</span>
                      <a
                        href={trackerUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="flex items-center gap-1 text-cyan-400 hover:text-cyan-300 transition-colors"
                        title="View profile on Rocket League Tracker Network"
                      >
                        <ExternalLink className="w-3.5 h-3.5" />
                        <span>RL Tracker</span>
                      </a>
                    </>
                  )}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-4 text-xs text-slate-400">
                <div className="flex items-center gap-1.5">
                  <Calendar className="w-4 h-4 text-slate-500" />
                  <span>First seen: {new Date(profile.first_seen_at).toLocaleDateString()}</span>
                </div>
                <span>•</span>
                <div>Last seen: {formatRelativeTime(profile.last_seen_at)}</div>
              </div>
            </div>
          </div>

          {/* Synergy & Head-to-Head Matchup Dossier */}
          {synergyStats && synergyStats.totalEncounters > 0 && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {/* As Teammate Card */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="p-1.5 rounded-lg bg-cyan-500/10 text-cyan-400">
                      <Users className="w-4 h-4" />
                    </div>
                    <div>
                      <h4 className="text-xs font-bold text-slate-200 uppercase tracking-wider">
                        Teammate Synergy
                      </h4>
                      <div className="text-[11px] text-slate-400">
                        {synergyStats.mateTotal} matches played together
                      </div>
                    </div>
                  </div>
                  {synergyStats.mateTotal >= 2 && (
                    <span
                      className={`px-2 py-0.5 rounded text-[10px] font-bold uppercase border ${
                        synergyStats.mateRate >= 60
                          ? 'bg-emerald-950/60 text-emerald-300 border-emerald-800/60'
                          : synergyStats.mateRate >= 40
                          ? 'bg-slate-800 text-slate-300 border-slate-700'
                          : 'bg-rose-950/60 text-rose-300 border-rose-800/60'
                      }`}
                    >
                      {synergyStats.mateRate >= 60 ? 'Strong Synergy' : synergyStats.mateRate >= 40 ? 'Balanced' : 'Struggled'}
                    </span>
                  )}
                </div>

                <div className="flex items-baseline justify-between">
                  <div className="text-2xl font-black text-cyan-300 font-mono">
                    {synergyStats.winsAsMate}W - {synergyStats.lossesAsMate}L
                  </div>
                  <div className="text-lg font-black text-cyan-400">
                    {synergyStats.mateRate.toFixed(1)}%
                  </div>
                </div>

                <div className="w-full bg-slate-900 rounded-full h-2 overflow-hidden flex border border-slate-800">
                  <div className="bg-cyan-500 h-full transition-all duration-300" style={{ width: `${synergyStats.mateRate}%` }} />
                  <div className="bg-slate-800 h-full transition-all duration-300" style={{ width: `${100 - synergyStats.mateRate}%` }} />
                </div>
              </div>

              {/* As Opponent Card */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="p-1.5 rounded-lg bg-amber-500/10 text-amber-400">
                      <Swords className="w-4 h-4" />
                    </div>
                    <div>
                      <h4 className="text-xs font-bold text-slate-200 uppercase tracking-wider">
                        Rivalry Balance
                      </h4>
                      <div className="text-[11px] text-slate-400">
                        {synergyStats.oppTotal} matches played against
                      </div>
                    </div>
                  </div>
                  {synergyStats.oppTotal >= 2 && (
                    <span
                      className={`px-2 py-0.5 rounded text-[10px] font-bold uppercase border ${
                        synergyStats.oppRate >= 60
                          ? 'bg-emerald-950/60 text-emerald-300 border-emerald-800/60'
                          : synergyStats.oppRate >= 40
                          ? 'bg-slate-800 text-slate-300 border-slate-700'
                          : 'bg-rose-950/60 text-rose-300 border-rose-800/60'
                      }`}
                    >
                      {synergyStats.oppRate >= 60 ? 'Dominant Record' : synergyStats.oppRate >= 40 ? 'Even Rivalry' : 'Tough Opponent'}
                    </span>
                  )}
                </div>

                <div className="flex items-baseline justify-between">
                  <div className="text-2xl font-black text-amber-300 font-mono">
                    {synergyStats.winsAsOpp}W - {synergyStats.lossesAsOpp}L
                  </div>
                  <div className="text-lg font-black text-amber-400">
                    {synergyStats.oppRate.toFixed(1)}%
                  </div>
                </div>

                <div className="w-full bg-slate-900 rounded-full h-2 overflow-hidden flex border border-slate-800">
                  <div className="bg-amber-500 h-full transition-all duration-300" style={{ width: `${synergyStats.oppRate}%` }} />
                  <div className="bg-slate-800 h-full transition-all duration-300" style={{ width: `${100 - synergyStats.oppRate}%` }} />
                </div>
              </div>
            </div>
          )}

          {/* Competitive Ranks Grid */}
          <div>
            <h4 className="text-xs font-bold text-slate-400 uppercase tracking-wider mb-3 flex items-center gap-2">
              <Shield className="w-4 h-4 text-cyan-400" />
              Competitive Ranks Snapshot
            </h4>
            {ranksList.length > 0 ? (
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
            ) : (
              <div className="rounded-xl border border-slate-800 bg-slate-900/40 p-4 text-center text-slate-500 text-xs">
                No competitive rank data recorded for this player.
              </div>
            )}
          </div>

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
                      <th className="py-2.5 px-4">Playlist</th>
                      <th className="py-2.5 px-3">As Teammate</th>
                      <th className="py-2.5 px-3">As Opponent</th>
                      <th className="py-2.5 px-3 text-right">Total Encounters</th>
                      <th className="py-2.5 px-4 text-right">Last Encounter</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-800/60">
                    {profile.matchups.map((m) => {
                      const mateTotal = m.wins_as_teammate + m.losses_as_teammate;
                      const oppTotal = m.wins_as_opponent + m.losses_as_opponent;
                      const mateRate = mateTotal > 0 ? formatWinRate(m.wins_as_teammate, m.losses_as_teammate) : '---';
                      const oppRate = oppTotal > 0 ? formatWinRate(m.wins_as_opponent, m.losses_as_opponent) : '---';
                      const friendlyName = PLAYLIST_NAMES[m.playlist_id] || `Playlist ${m.playlist_id}`;

                      return (
                        <tr key={m.playlist_id} className="hover:bg-slate-900/40">
                          <td className="py-3 px-4 font-semibold text-slate-200">
                            {friendlyName}
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
