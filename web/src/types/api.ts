// ============================================================================
// Session & Match Telemetry Types
// ============================================================================

export interface PlayerStatsSummary {
  score: number;
  goals: number;
  assists: number;
  saves: number;
  shots: number;
  demos: number;
}

export interface PlayerPlaylistRank {
  playlist_id: number;
  playlist_name: string;
  tier: number;
  division: number;
  rank_name: string;
  mmr: number;
  matches_played?: number;
}

export type PlayerRanksSnapshot = Record<string, PlayerPlaylistRank>;

export interface PlayerMatchup {
  player_id: string;
  playlist_id: number;
  wins_as_teammate: number;
  losses_as_teammate: number;
  wins_as_opponent: number;
  losses_as_opponent: number;
  total_matches: number;
  last_played_at: string;
}

export interface SessionMatchPlayer {
  player_id: string;
  platform: string;
  name: string;
  team_num: number; // 0 = Blue, 1 = Orange, 255 = Spectator
  is_local: boolean;
  is_bot: boolean;
  is_disconnected?: boolean;
  won?: boolean;
  stats: PlayerStatsSummary;
  rank_name: string;
  tier: number;
  division: number;
  mmr: number;
  matchup_record?: PlayerMatchup;
}

export interface SessionMatchDetail {
  match_guid: string;
  playlist_id: number;
  playlist_name: string;
  started_at: string;
  ended_at: string;
  duration_seconds: number;
  result: 'victory' | 'defeat' | 'draw' | 'unknown' | '';
  local_team?: number; // 0 = Blue, 1 = Orange
  winner_team?: number;
  blue_score: number;
  orange_score: number;
  starting_mmr: number;
  ending_mmr: number;
  mmr_change: number; // ending_mmr - starting_mmr
  players: SessionMatchPlayer[];
}

export interface PlaylistSessionStats {
  playlist_id: number;
  playlist_name: string;
  matches_played: number;
  wins: number;
  losses: number;
  win_rate: number; // 0.0 - 100.0
  initial_mmr: number;
  current_mmr: number;
  mmr_delta: number;
}

export interface LobbyPlayer {
  player_id: string;
  platform: string;
  name: string;
  team_num: number;
  is_local: boolean;
  is_bot: boolean;
  is_disconnected?: boolean;
  stats: PlayerStatsSummary;
  current_rank?: PlayerPlaylistRank;
  ranks?: PlayerRanksSnapshot;
  matchup_record?: PlayerMatchup;
}

export interface CurrentMatchResponse {
  active_match: boolean;
  match_ended: boolean;
  match_guid: string;
  playlist_id: number;
  playlist_name: string;
  local_team?: number;
  local_player?: LobbyPlayer;
  teammates: LobbyPlayer[];
  opponents: LobbyPlayer[];
  spectators?: LobbyPlayer[];
  winner_team?: number;
  result?: 'victory' | 'defeat' | 'draw' | '';
  updated_at: string;
}

export interface SessionResponse {
  session_id: string;
  started_at: string;
  uptime: string;
  total_matches: number;
  total_wins: number;
  total_losses: number;
  win_rate: number;
  playlists: Record<string, PlaylistSessionStats>;
  matches: SessionMatchDetail[];
  active_match?: CurrentMatchResponse;
}

// ============================================================================
// Player Directory & Search Types
// ============================================================================

export interface PlayerSummary {
  player_id: string;
  platform: string;
  player_name: string;
  ranks_json: string;
  first_seen_at: string;
  last_seen_at: string;
  total_wins_as_teammate: number;
  total_losses_as_teammate: number;
  total_wins_as_opponent: number;
  total_losses_as_opponent: number;
  total_matches: number;
}

export interface PlayerSearchResponse {
  players: PlayerSummary[];
  total: number;
  limit: number;
  offset: number;
}

export interface PlayerDetailResponse {
  player_id: string;
  platform: string;
  player_name: string;
  ranks_json: string;
  first_seen_at: string;
  last_seen_at: string;
  matchups: PlayerMatchup[];
}

export type ColumnKey =
  | 'score'
  | 'goals'
  | 'assists'
  | 'saves'
  | 'shots'
  | 'demos'
  | 'mmr'
  | 'rank'
  | 'h2h'
  | 'platform';

export type ColumnPreset = 'full' | 'competitive' | 'streamer';
