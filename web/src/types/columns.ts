export type StatColumnKey =
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

export type ColumnConfig = Record<StatColumnKey, boolean>;

export type PresetKey = 'full' | 'competitive' | 'streamer';

export interface ColumnDefinition {
  key: StatColumnKey;
  label: string;
  shortLabel: string;
  description: string;
  category: 'performance' | 'skill' | 'identity';
}

export const STAT_COLUMNS: ColumnDefinition[] = [
  { key: 'score', label: 'Score', shortLabel: 'PTS', description: 'In-game box score points', category: 'performance' },
  { key: 'goals', label: 'Goals', shortLabel: 'G', description: 'Goals scored', category: 'performance' },
  { key: 'assists', label: 'Assists', shortLabel: 'A', description: 'Assists awarded', category: 'performance' },
  { key: 'saves', label: 'Saves', shortLabel: 'SV', description: 'Saves made', category: 'performance' },
  { key: 'shots', label: 'Shots', shortLabel: 'SH', description: 'Shots on goal', category: 'performance' },
  { key: 'demos', label: 'Demolitions', shortLabel: 'DEMO', description: 'Demolitions inflicted', category: 'performance' },
  { key: 'mmr', label: 'Skill Rating (MMR)', shortLabel: 'MMR', description: 'Numerical Matchmaking Rating', category: 'skill' },
  { key: 'rank', label: 'Rank Tier', shortLabel: 'RANK', description: 'Competitive rank tier and division', category: 'skill' },
  { key: 'h2h', label: 'Head-to-Head', shortLabel: 'H2H', description: 'Historical win rate with/against player', category: 'skill' },
  { key: 'platform', label: 'Platform', shortLabel: 'PLAT', description: 'Gaming network (Steam, Epic, etc.)', category: 'identity' },
];

export const PRESETS: Record<PresetKey, { name: string; description: string; columns: StatColumnKey[] }> = {
  full: {
    name: 'Full Stats',
    description: 'All 10 telemetry and skill metrics enabled',
    columns: ['score', 'goals', 'assists', 'saves', 'shots', 'demos', 'mmr', 'rank', 'h2h', 'platform'],
  },
  competitive: {
    name: 'Competitive Focus',
    description: 'Core competitive metrics: Score, Goals, MMR, Rank, and H2H',
    columns: ['score', 'goals', 'mmr', 'rank', 'h2h'],
  },
  streamer: {
    name: 'Streamer Compact',
    description: 'Minimalist box score hiding MMR and ranks for privacy',
    columns: ['score', 'goals', 'assists', 'saves'],
  },
};
