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
  notes?: string;
  tags?: string[];
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
  result?: 'victory' | 'defeat' | 'draw' | '';
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
}

export interface SessionListResponse {
  sessions: SessionResponse[];
  total: number;
  limit: number;
  offset: number;
}

// ============================================================================
// MMR History Snapshot Models (SPEC-08)
// ============================================================================

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

// ============================================================================
// Player Directory, Spotlights & Notes/Tags (SPEC-05, SPEC-07)
// ============================================================================

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
}

export interface PlayerSearchResponse {
  players: PlayerSummary[];
  total: number;
  limit: number;
  offset: number;
  spotlights?: GlobalSpotlights;
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
}

export interface PlayerPatchPayload {
  notes?: string;
  tags?: string[];
}

export interface PlayerPatchResponse {
  status: string;
  player: PlayerSummary;
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

// ============================================================================
// Relational Match Players & Chronological History (SPEC-02)
// ============================================================================

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

// ============================================================================
// Comprehensive Ballchasing Telemetry Models (SPEC-01)
// ============================================================================

export interface CameraSettings {
  fov: number;
  height: number;
  pitch: number;
  distance: number;
  stiffness: number;
  swivel_speed: number;
  transition_speed: number;
}

export interface CoreStats {
  shots?: number;
  shots_against?: number;
  goals?: number;
  goals_against?: number;
  saves?: number;
  assists?: number;
  score?: number;
  mvp?: boolean;
  shooting_percentage?: number;
  epic_saves?: number;
  clears?: number;
  centers?: number;
  aerial_hits?: number;
  first_touches?: number;
  flip_resets?: number;
  crossbar_hits?: number;
  bicycle_hits?: number;
  juggle_hits?: number;
}

export interface BoostStats {
  bpm?: number;
  bcpm?: number;
  avg_amount?: number;
  amount_collected?: number;
  amount_stolen?: number;
  amount_collected_big?: number;
  amount_stolen_big?: number;
  amount_collected_small?: number;
  amount_stolen_small?: number;
  count_collected_big?: number;
  count_stolen_big?: number;
  count_collected_small?: number;
  count_stolen_small?: number;
  amount_overfill?: number;
  amount_overfill_stolen?: number;
  amount_used_while_supersonic?: number;
  time_zero_boost?: number;
  percent_zero_boost?: number;
  time_full_boost?: number;
  percent_full_boost?: number;
  time_boost_0_25?: number;
  time_boost_25_50?: number;
  time_boost_50_75?: number;
  time_boost_75_100?: number;
  percent_boost_0_25?: number;
  percent_boost_25_50?: number;
  percent_boost_50_75?: number;
  percent_boost_75_100?: number;
}

export interface MovementStats {
  avg_speed?: number;
  total_distance?: number;
  time_supersonic_speed?: number;
  time_boost_speed?: number;
  time_slow_speed?: number;
  time_ground?: number;
  time_low_air?: number;
  time_high_air?: number;
  time_powerslide?: number;
  count_powerslide?: number;
  avg_powerslide_duration?: number;
  avg_speed_percentage?: number;
  percent_slow_speed?: number;
  percent_boost_speed?: number;
  percent_supersonic_speed?: number;
  percent_ground?: number;
  percent_low_air?: number;
  percent_high_air?: number;
}

export interface PositioningStats {
  avg_distance_to_ball?: number;
  avg_distance_to_ball_possession?: number;
  avg_distance_to_ball_no_possession?: number;
  avg_distance_to_mates?: number;
  time_defensive_third?: number;
  time_neutral_third?: number;
  time_offensive_third?: number;
  time_defensive_half?: number;
  time_offensive_half?: number;
  time_behind_ball?: number;
  time_infront_ball?: number;
  time_most_back?: number;
  time_most_forward?: number;
  goals_against_while_last_defender?: number;
  time_closest_to_ball?: number;
  time_farthest_from_ball?: number;
  percent_defensive_third?: number;
  percent_offensive_third?: number;
  percent_neutral_third?: number;
  percent_defensive_half?: number;
  percent_offensive_half?: number;
  percent_behind_ball?: number;
  percent_infront_ball?: number;
  percent_most_back?: number;
  percent_most_forward?: number;
  percent_closest_to_ball?: number;
  percent_farthest_from_ball?: number;
}

export interface DemoStats {
  inflicted?: number;
  inflicted_in_defensive_half?: number;
  inflicted_in_offensive_half?: number;
  taken?: number;
  taken_in_defensive_half?: number;
  taken_in_offensive_half?: number;
}

export interface BallStats {
  possession_time?: number;
  time_in_side?: number;
}

export interface PlayerTelemetry {
  core?: CoreStats;
  boost?: BoostStats;
  movement?: MovementStats;
  positioning?: PositioningStats;
  demo?: DemoStats;
  ball?: BallStats;
}

export interface ServerInfo {
  name?: string;
}

export interface UploaderInfo {
  steam_id?: string;
  name?: string;
  profile_url?: string;
  avatar?: string;
}

export interface ReplayPlayer {
  start_time?: number;
  end_time?: number;
  name: string;
  id: { platform: string; id: string };
  car_id?: number;
  car_name?: string;
  camera?: CameraSettings;
  steering_sensitivity?: number;
  rank?: { id?: string; tier?: number; division?: number; name?: string; mmr?: number };
  mvp?: boolean;
  score: number;
  goals: number;
  assists: number;
  saves: number;
  shots: number;
  demos: { inflicted: number; taken: number };
  stats?: PlayerTelemetry;
  core?: CoreStats;
  boost?: BoostStats;
  movement?: MovementStats;
  positioning?: PositioningStats;
  demo?: DemoStats;
  ball?: BallStats;
}

export interface TeamStats {
  ball?: BallStats;
  core?: CoreStats;
  boost?: BoostStats;
  movement?: MovementStats;
  positioning?: PositioningStats;
  demo?: DemoStats;
}

export interface ReplayTeam {
  color: string;
  name: string;
  goals: number;
  players: ReplayPlayer[];
  stats?: TeamStats;
}

export interface BallchasingGroup {
  id: string;
  name: string;
  link: string;
}

export interface ReplayDetail {
  id: string;
  link: string;
  created: string;
  date: string;
  date_has_timezone?: boolean;
  status?: string;
  rocket_league_id?: string;
  match_guid?: string;
  title?: string;
  replay_title?: string;
  map_code?: string;
  map_name?: string;
  match_type?: string;
  team_size?: number;
  playlist_id?: string;
  playlist_name?: string;
  duration?: number;
  play_duration?: number;
  overtime: boolean;
  overtime_seconds?: number;
  season?: number;
  season_type?: string;
  visibility?: string;
  server?: ServerInfo;
  uploader?: UploaderInfo;
  blue: ReplayTeam;
  orange: ReplayTeam;
  min_rank?: { id?: string; tier?: number; division?: number; name?: string; mmr?: number };
  max_rank?: { id?: string; tier?: number; division?: number; name?: string; mmr?: number };
  groups?: BallchasingGroup[];
}

// ============================================================================
// Historical Games & Ballchasing Sync Types
// ============================================================================

export interface ReplayPlayerStats {
  name: string;
  platform: string;
  online_id: string;
  team: number;
  score: number;
  goals: number;
  assists: number;
  saves: number;
  shots: number;
  demos: number;
  is_bot?: boolean;
  car_id?: number;
  car_name?: string;
  mvp?: boolean;
  camera?: CameraSettings;
  steering_sensitivity?: number;
  rank?: { id?: string; tier?: number; division?: number; name?: string; mmr?: number };
  core?: CoreStats;
  boost?: BoostStats;
  movement?: MovementStats;
  positioning?: PositioningStats;
  demo?: DemoStats;
  ball?: BallStats;
}

export interface ReplayMatchStats {
  match_guid: string;
  map_name: string;
  playlist: number;
  blue_score: number;
  orange_score: number;
  winner_team?: number;
  duration: number;
  overtime: boolean;
  date: string;
  players: ReplayPlayerStats[];
  ballchasing_id?: string;
  ballchasing_url?: string;
  raw_json?: string;
}

export interface HistoricalMatchRecord {
  match_guid: string;
  record_start_timestamp: number;
  map_name: string;
  playlist: number;
  replay_url: string;
  download_status: string;
  local_file_path: string;
  downloaded_at?: string;
  upload_status: string;
  ballchasing_id: string;
  ballchasing_url: string;
  uploaded_at?: string;
  retry_count: number;
  last_error: string;
  blue_score: number;
  orange_score: number;
  winner_team?: number;
  replay_stats_json: string;
  synced_at?: string;
  created_at: string;
  updated_at: string;
  stats?: ReplayMatchStats;
  result?: 'victory' | 'defeat' | 'draw' | 'unknown' | '';
  stats_sync_status?: string;
  stats_retry_count?: number;
}

export interface MatchesResponse {
  matches: HistoricalMatchRecord[];
  total: number;
  limit: number;
  offset: number;
  local_player_ids?: string[];
}

export interface BallchasingSyncStatus {
  running: boolean;
  total_replays: number;
  synced_matches: number;
  synced_players: number;
  started_at?: string;
  ended_at?: string;
  last_error?: string;
}
