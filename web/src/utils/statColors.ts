export type StatColorKey =
  | 'score'
  | 'goals'
  | 'assists'
  | 'saves'
  | 'shots'
  | 'shotpct'
  | 'demos'
  | 'demod'
  | 'mmr'
  | 'rank'
  | 'bpm'
  | 'boost'
  | 'speed'
  | 'games'
  | 'h2h';

export interface StatColorPalette {
  score: string;
  goals: string;
  assists: string;
  saves: string;
  shots: string;
  shotpct: string;
  demos: string;
  demod: string;
  mmr: string;
  rank: string;
  bpm: string;
  boost: string;
  speed: string;
  games: string;
  h2h: string;
}

/**
 * Hypercolor, ultra-high-contrast semantic colors for box-score and telemetry stat labels & values.
 * Every token is calibrated for electric saturation, strong perceptual separation (CIELAB ΔE),
 * and >= 4.7:1 WCAG 2.1 contrast across all 14 dark and light theme surfaces
 * (page, panel, plaque, and panelHover — reaching 7:1–18:1 on core dark themes).
 */
export const STAT_COLOR_PALETTES: Record<'dark' | 'light', StatColorPalette> = {
  dark: {
    score: '#fff500',   // Hyper Laser Yellow — SCORE / PTS
    goals: '#00ff59',   // Hyper Neon Green — GOALS / G
    assists: '#00f7ff', // Hyper Electric Cyan — ASSISTS / A
    saves: '#78a6ff',   // Hyper Electric Cobalt — SAVES / SV
    shots: '#ff66ff',   // Hyper Electric Magenta — SHOTS / SH
    shotpct: '#b8ff00', // Hyper Volt Lime — SHOT % / SH%
    demos: '#ff8519',   // Hyper Blaze Orange — DEMOS / D / DEM
    demod: '#ff7084',   // Hyper Laser Crimson — DEMO'D
    mmr: '#00ccff',     // Hyper Neon Azure — MMR
    rank: '#ffc400',    // Hyper Solar Gold — RANK · DIV
    bpm: '#c785ff',     // Hyper Ultraviolet — BPM
    boost: '#00ffb7',   // Hyper Neon Mint — BOOST / BST
    speed: '#ff6ec4',   // Hyper Hot Pink — SUPER % / SPEED
    games: '#ffffff',   // Pure Hyper White — GAMES / GMS
    h2h: '#ffb07c',     // Hyper Neon Coral — H2H
  },
  light: {
    score: '#755600',   // Hyper Vivid Gold — SCORE / PTS
    goals: '#006b22',   // Hyper Vivid Emerald — GOALS / G
    assists: '#006175', // Hyper Electric Cyan — ASSISTS / A
    saves: '#0035c7',   // Hyper Electric Cobalt — SAVES / SV
    shots: '#a600a0',   // Hyper Electric Magenta — SHOTS / SH
    shotpct: '#446b00', // Hyper Volt Lime — SHOT % / SH%
    demos: '#b03200',   // Hyper Blaze Orange — DEMOS / D / DEM
    demod: '#c4002a',   // Hyper Laser Crimson — DEMO'D
    mmr: '#004ea8',     // Hyper Electric Azure — MMR
    rank: '#8c2b56',    // Hyper Royal Ruby — RANK · DIV
    bpm: '#7300db',     // Hyper Ultraviolet — BPM
    boost: '#006b50',   // Hyper Electric Teal-Mint — BOOST / BST
    speed: '#bd0068',   // Hyper Hot Pink — SUPER % / SPEED
    games: '#1e293b',   // Crisp Ink Slate — GAMES / GMS
    h2h: '#43209c',     // Hyper Electric Indigo — H2H
  },
};

export const STAT_CSS_VAR: Record<StatColorKey, string> = {
  score: '--t-stat-score',
  goals: '--t-stat-goals',
  assists: '--t-stat-assists',
  saves: '--t-stat-saves',
  shots: '--t-stat-shots',
  shotpct: '--t-stat-shotpct',
  demos: '--t-stat-demos',
  demod: '--t-stat-demod',
  mmr: '--t-stat-mmr',
  rank: '--t-stat-rank',
  bpm: '--t-stat-bpm',
  boost: '--t-stat-boost',
  speed: '--t-stat-speed',
  games: '--t-stat-games',
  h2h: '--t-stat-h2h',
};

export const STAT_LABEL_CLASS: Record<StatColorKey, string> = {
  score: 'stat-lbl-score',
  goals: 'stat-lbl-goals',
  assists: 'stat-lbl-assists',
  saves: 'stat-lbl-saves',
  shots: 'stat-lbl-shots',
  shotpct: 'stat-lbl-shotpct',
  demos: 'stat-lbl-demos',
  demod: 'stat-lbl-demod',
  mmr: 'stat-lbl-mmr',
  rank: 'stat-lbl-rank',
  bpm: 'stat-lbl-bpm',
  boost: 'stat-lbl-boost',
  speed: 'stat-lbl-speed',
  games: 'stat-lbl-games',
  h2h: 'stat-lbl-h2h',
};
