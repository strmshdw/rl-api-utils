import type {
  LobbyPlayer,
  MostPlayedPlaylistSummary,
  PlayerMatchup,
  PlayerRoleStatsSummary,
  PlayerStatsSummary,
} from './playerTypes';

export type {
  MatchClock,
  PendingMatch,
  RoleStatsBreakdown,
  StatsAPIStatus,
  SyncConfig,
  SyncProgress,
} from '../contracts/generated_dto';

export {
  MATCH_STATUS,
  PLAYLIST_CATALOG,
  PLAYER_CLASSIFICATION,
  REPLAY_STATS_STATUS,
  SSE_EVENT_TYPE,
  SYNC_MODE,
  type MatchStatus,
  type PlayerClassification,
  type PlaylistMeta,
  type ReplayStatsStatus,
  type SSEEventType,
  type SyncMode,
} from '../contracts/generated_enums';

// ============================================================================
// Navigation, Live Match & Session Telemetry Types
// ============================================================================

export type DashboardTab = 'live' | 'session' | 'history' | 'players';

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

export interface CurrentMatchResponse {
  active_match: boolean;
  match_ended: boolean;
  match_guid: string;
  playlist_id: number;
  playlist_name: string;
  time_remaining?: number;
  is_overtime?: boolean;
  local_team?: number;
  local_player?: LobbyPlayer;
  local_player_ids?: string[];
  teammates: LobbyPlayer[];
  opponents: LobbyPlayer[];
  spectators?: LobbyPlayer[];
  winner_team?: number;
  result?: 'victory' | 'defeat' | 'draw' | 'unknown' | '';
  updated_at: string;
}

export interface SessionResponse {
  session_id: string;
  started_at: string;
  ended_at?: string;
  last_match_at?: string;
  uptime: string;
  total_matches: number;
  total_wins: number;
  total_losses: number;
  win_rate: number;
  playlists: Record<string, PlaylistSessionStats>;
  matches: SessionMatchDetail[];
  active_match?: CurrentMatchResponse;
  local_player_ids?: string[];
  continuable_session?: SessionResponse | null;
  continue_window_seconds?: number;
  can_continue?: boolean;
  most_played_playlist?: MostPlayedPlaylistSummary;
  for_stats?: PlayerRoleStatsSummary;
  against_stats?: PlayerRoleStatsSummary;
  stats_api_ready?: boolean;
  rank_api_ready?: boolean;
  mmr_poller_state?: string;
  game_connected?: boolean;
  pending_count?: number;
  trigger_threshold?: number;
  force_sync_on_trigger?: boolean;
  enable_toast?: boolean;
  auto_sync_on_exit?: boolean;
  max_hosted_replays?: number;
  replay_count?: number;
  replay_limit?: number;
  replay_usage_pct?: number;
  replay_limit_status?: 'ok' | 'warning' | 'exceeded' | string;
}

export interface SessionListResponse {
  sessions: SessionResponse[];
  total: number;
  limit: number;
  offset: number;
}
