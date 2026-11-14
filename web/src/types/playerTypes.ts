import type {
  PlayerPlaylistRank as ContractPlayerPlaylistRank,
  PlayerStatsSummary,
} from '../contracts/generated_dto';
import type { PresetKey, StatColumnKey } from './columns';
import type { HistoricalMatchRecord } from './replayTypes';

export type { PlayerStatsSummary };

// ============================================================================
// Player Directory, Matchups, Spotlights, Role Telemetry & MMR History
// ============================================================================

export interface PlayerPlaylistRank extends Omit<ContractPlayerPlaylistRank, 'matches_played'> {
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

export interface PlayerRoleStatsSummary {
  games: number;
  wins: number;
  losses: number;
  win_rate: number;
  total_score: number;
  total_goals: number;
  total_assists: number;
  total_saves: number;
  total_shots: number;
  total_demos_inflicted: number;
  total_demos_taken: number;
  avg_score: number;
  avg_goals: number;
  avg_assists: number;
  avg_saves: number;
  avg_shots: number;
  shooting_pct: number;
  avg_demos_inflicted: number;
  avg_demos_taken: number;
  avg_bpm: number;
  avg_bcpm: number;
  avg_boost: number;
  total_boost_stolen: number;
  total_big_pads: number;
  total_small_pads: number;
  avg_supersonic_pct: number;
  avg_boost_speed_pct: number;
  avg_slow_speed_pct: number;
  avg_ground_pct: number;
  avg_low_air_pct: number;
  avg_high_air_pct: number;
  avg_powerslide_count: number;
  avg_powerslide_duration: number;
  avg_def_third_pct: number;
  avg_neutral_third_pct: number;
  avg_off_third_pct: number;
  avg_behind_ball_pct: number;
  avg_first_man_pct: number;
  avg_third_man_pct: number;
  avg_dist_to_ball: number;
  score?: number;
  goals?: number;
  assists?: number;
  saves?: number;
  demos?: number;
  shot_pct?: number;
  avg_demos?: number;
}

export interface MostPlayedPlaylistSummary {
  id: number;
  name: string;
  net_mmr: number;
  wins: number;
  losses: number;
  matches_count: number;
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
  with_you_stats?: PlayerRoleStatsSummary;
  against_you_stats?: PlayerRoleStatsSummary;
  in_wins_stats?: PlayerRoleStatsSummary;
  in_losses_stats?: PlayerRoleStatsSummary;
  notes?: string;
  tags?: string[];
}

export interface MMRHistoryEntry {
  id: number;
  recorded_at: string;
  match_guid?: string;
  playlist_id: number;
  mmr: number;
  mmr_change: number;
  tier: number;
  division: number;
}

export interface MMRHistoryResponse {
  playlist_id: number;
  history: MMRHistoryEntry[];
  total: number;
}

export interface SpotlightSummary {
  player: PlayerSummary | null;
  value: string;
  sub: string;
  tone: 'win' | 'neutral' | 'loss';
}

export interface GlobalSpotlights {
  best_teammate?: SpotlightSummary;
  most_faced?: SpotlightSummary;
  toughest_rival?: SpotlightSummary;
}

export interface PlayerSummary {
  player_id: string;
  platform: string;
  player_name: string;
  ranks_json: string;
  notes?: string;
  tags?: string[];
  first_seen_at: string;
  last_seen_at: string;
  total_wins_as_teammate: number;
  total_losses_as_teammate: number;
  total_wins_as_opponent: number;
  total_losses_as_opponent: number;
  total_matches: number;
  teammate_wins?: number;
  teammate_losses?: number;
  opponent_wins?: number;
  opponent_losses?: number;
  encounters_count?: number;
  with_you_stats?: PlayerRoleStatsSummary;
  against_you_stats?: PlayerRoleStatsSummary;
  relationship_tag?: string;
}

export interface PlayerSearchResponse {
  players: PlayerSummary[];
  total: number;
  limit: number;
  offset: number;
  spotlights?: GlobalSpotlights;
}

export interface MatchPlayerRecord {
  match_guid: string;
  player_id: string;
  team_num: number;
  is_local: boolean;
  won?: boolean | null;
  score: number;
  goals: number;
  assists: number;
  saves: number;
  shots: number;
  demos: number;
  car_name?: string;
  stats_json?: string;
}

export interface PlayerMatchDetail extends HistoricalMatchRecord {
  player_stats: MatchPlayerRecord;
  role: 'teammate' | 'opponent';
  result: 'victory' | 'defeat' | 'draw' | 'unknown' | '';
  local_result?: 'victory' | 'defeat' | 'draw' | 'unknown' | '';
}

export interface PlayerMatchesResponse {
  matches: PlayerMatchDetail[];
  total: number;
  limit: number;
  offset: number;
}

export interface MatchRetryResponse {
  status: string;
  message: string;
  match_guid: string;
}

export interface PlayerDetailResponse {
  player_id: string;
  platform: string;
  player_name: string;
  ranks_json: string;
  notes?: string;
  tags?: string[];
  first_seen_at: string;
  last_seen_at: string;
  matchups: PlayerMatchup[];
  recent_matches?: PlayerMatchDetail[];
  teammate_wins?: number;
  teammate_losses?: number;
  opponent_wins?: number;
  opponent_losses?: number;
  encounters_count?: number;
  with_you_stats?: PlayerRoleStatsSummary;
  against_you_stats?: PlayerRoleStatsSummary;
  relationship_tag?: string;
}

export interface PlayerPatchPayload {
  notes?: string;
  tags?: string[];
}

export interface PlayerPatchResponse {
  status: string;
  player: PlayerSummary;
}

export type ColumnKey = StatColumnKey;
export type ColumnPreset = PresetKey;
