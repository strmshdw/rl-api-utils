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
 * Distinct, high-contrast semantic colors for box-score and telemetry stat labels.
 * Every token is calibrated to exceed WCAG 2.1 AA (>= 4.5:1) across all 14 dark
 * and light theme surfaces (page, panel, plaque, and panelHover).
 */
export const STAT_COLOR_PALETTES: Record<'dark' | 'light', StatColorPalette> = {
  dark: {
    score: '#ffd166',   // Gold — SCORE / PTS
    goals: '#5ce69a',   // Emerald — GOALS / G
    assists: '#52daf5', // Electric Cyan — ASSISTS / A
    saves: '#9abaff',   // Periwinkle Blue — SAVES / SV
    shots: '#d6a6ff',   // Lavender Violet — SHOTS / SH
    shotpct: '#4de8cc', // Aquamarine Mint — SHOT % / SH%
    demos: '#ff9966',   // Bright Tangerine — DEMOS / D / DEM
    demod: '#ff85a1',   // Crimson Rose — DEMO'D
    mmr: '#7cc7ff',     // Ice Blue — MMR
    rank: '#e3cfa6',    // Champagne Sand — RANK · DIV
    bpm: '#c5ed6b',     // Chartreuse Lime — BPM
    boost: '#c9a6ff',   // Soft Violet — BOOST / BST
    speed: '#ff8cd9',   // Magenta Orchid — SUPER % / SPEED
    games: '#dde6f2',   // Crisp Platinum — GAMES / GMS
    h2h: '#b7f06e',     // Bright Lime — H2H
  },
  light: {
    score: '#784800',   // Deep Amber — SCORE / PTS
    goals: '#0a5e32',   // Deep Emerald — GOALS / G
    assists: '#065970', // Deep Cyan — ASSISTS / A
    saves: '#1d3da0',   // Royal Cobalt — SAVES / SV
    shots: '#5c229c',   // Deep Violet — SHOTS / SH
    shotpct: '#065e50', // Deep Teal — SHOT % / SH%
    demos: '#942e04',   // Burnt Tangerine — DEMOS / D / DEM
    demod: '#991230',   // Deep Crimson — DEMO'D
    mmr: '#0c4a85',     // Deep Navy Blue — MMR
    rank: '#594524',    // Warm Bronze — RANK · DIV
    bpm: '#3e5900',     // Deep Olive Lime — BPM
    boost: '#582499',   // Deep Plum — BOOST / BST
    speed: '#8f125d',   // Deep Magenta — SUPER % / SPEED
    games: '#2f3d52',   // Deep Slate — GAMES / GMS
    h2h: '#395700',     // Deep Moss Lime — H2H
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
