import { STAT_COLOR_PALETTES, STAT_CSS_VAR, StatColorKey } from '../utils/statColors';

export type ThemeStyleKey =
  | 'broadcast'
  | 'tactical'
  | 'swiss'
  | 'arashi'
  | 'classic'
  | 'arena'
  | 'carbon'
  | 'synthwave'
  | 'editorial'
  | 'nordic'
  | 'terminal'
  | 'solar'
  | 'industrial'
  | 'oled';

export type ColorMode = 'dark' | 'light';
export type FontScale = 'normal' | 'xl';

export interface ThemePalette {
  page: string;
  panel: string;
  plaque?: string;
  panelHover: string;
  modal: string;
  line: string;
  line2: string;
  outline: string;
  frieze?: string;
  ink: string;
  strong: string;
  onAccent: string;
  onSlab?: string;
  ink2: string;
  ink3: string;
  muted: string;
  muted2: string;
  faint: string;
  blueSlab: string;
  orangeSlab: string;
  blueInk: string;
  orangeInk: string;
  vermilionDisplay?: string;
  lossSlab?: string;
  win: string;
  loss: string;
  gold?: string;
  carryInk?: string;
  bgImage: string;
}

export interface ThemeStyleDefinition {
  name: string;
  fontDisplay: string;
  fontSans: string;
  fontMono: string;
  radius: string;
  slant: string;
  bw: string;
  blur: string;
  dark: ThemePalette;
  light: ThemePalette;
}

const F_DECO = "'Josefin Sans', 'Space Grotesk', 'Inter', sans-serif";
const F_COND = "'Barlow Condensed', 'Arial Narrow', sans-serif";
const F_BARLOW = "'Barlow', 'Inter', system-ui, sans-serif";
const F_SANS = "'IBM Plex Sans', 'Inter', system-ui, sans-serif";
const F_GROT = "'Space Grotesk', 'Inter', sans-serif";
const F_MONO = "'JetBrains Mono', monospace";
const F_SERIF = "'Fraunces', Georgia, serif";

const D2_ARASHI_LIGHT_PALETTE: ThemePalette = {
  page: '#ebe5d9', panel: '#f4efe6', plaque: '#fcfaf6', panelHover: '#fcfaf6', modal: '#fcfaf6',
  line: '#cfc7b8', line2: '#dfd8ca', outline: '#1c1917', frieze: '#3d3328',
  ink: '#14110f', strong: '#050403', onAccent: '#fbf9f5', onSlab: '#fbf9f5', ink2: '#2d2620', ink3: '#423930',
  muted: '#574c40', muted2: '#665a4c', faint: '#7d7061',
  blueSlab: '#1d3f58', orangeSlab: '#a8461c', blueInk: '#163d5c', orangeInk: '#8a340f',
  vermilionDisplay: '#94171e', lossSlab: '#94171e',
  win: '#165236', loss: '#9c121b', gold: '#7a4a00', carryInk: '#164561', bgImage: 'none',
};

export const THEME_STYLES: Record<ThemeStyleKey, ThemeStyleDefinition> = {
  broadcast: {
    name: '01 · Deco Monolith: Woodblock (Default)',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: {
      page: '#0a0908', panel: '#181614', plaque: '#0f0d0b', panelHover: '#221f1c', modal: '#181614',
      line: '#453f38', line2: '#302b26', outline: '#e6dfd5', frieze: '#dfd8cd',
      ink: '#f7f4ef', strong: '#ffffff', onAccent: '#12100e', onSlab: '#fbf9f5', ink2: '#dfd8cb', ink3: '#c9c1b2',
      muted: '#afa596', muted2: '#9a9082', faint: '#7c7367',
      blueSlab: '#1e3a52', orangeSlab: '#9e4726', blueInk: '#8ab4d4', orangeInk: '#eb946a',
      vermilionDisplay: '#8f1822', lossSlab: '#8f1822',
      win: '#8ac29d', loss: '#f56c74', gold: '#efc463', carryInk: '#88bad9', bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  tactical: {
    name: '02 · Deco Monolith: Lacquer & Brass',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: {
      page: '#040302', panel: '#120e09', plaque: '#080604', panelHover: '#1d160e', modal: '#120e09',
      line: '#594323', line2: '#3b2b15', outline: '#deb45e', frieze: '#edc36b',
      ink: '#faf2e1', strong: '#ffffff', onAccent: '#120e09', onSlab: '#fffdf9', ink2: '#e0d0b2', ink3: '#cbb795',
      muted: '#b59f7c', muted2: '#a08a68', faint: '#806e50',
      blueSlab: '#13404d', orangeSlab: '#9c4f1a', blueInk: '#74bdd0', orangeInk: '#f0a256',
      vermilionDisplay: '#8c161e', lossSlab: '#8c161e',
      win: '#8ac29d', loss: '#f56972', gold: '#f0c667', carryInk: '#88bad9', bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  swiss: {
    name: '03 · Deco Monolith: Carved Inkstone',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: {
      page: '#0c0b0a', panel: '#1d1916', plaque: '#12100e', panelHover: '#26211d', modal: '#1d1916',
      line: '#4a4139', line2: '#332c26', outline: '#c4b6a3', frieze: '#dbd0c0',
      ink: '#f5efe3', strong: '#ffffff', onAccent: '#12100e', onSlab: '#fbf8f3', ink2: '#d6ccb9', ink3: '#c2b6a2',
      muted: '#aea28f', muted2: '#9b8f7d', faint: '#7f7465',
      blueSlab: '#2b4559', orangeSlab: '#8f492a', blueInk: '#93b8d4', orangeInk: '#e39671',
      vermilionDisplay: '#821d24', lossSlab: '#821d24',
      win: '#8ec4a1', loss: '#f5737b', gold: '#efc463', carryInk: '#8cbcd9', bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  arashi: {
    name: '04 · Deco Monolith: Warm Parchment (Light)',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: D2_ARASHI_LIGHT_PALETTE,
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  classic: {
    name: '05 · Classic Broadcast (Original RL/SYNC)',
    fontDisplay: F_COND, fontSans: F_BARLOW, fontMono: F_MONO, radius: '0px', slant: '20px', bw: '1px', blur: 'none',
    dark: {
      page: '#0a0b0d', panel: '#121418', plaque: '#16181c', panelHover: '#171a1f', modal: '#0f1114',
      line: '#282c34', line2: '#2d323b', outline: '#3e4452', frieze: '#3e4452',
      ink: '#f5f7fa', strong: '#ffffff', onAccent: '#0a0b0d', onSlab: '#ffffff', ink2: '#d5d9e2', ink3: '#b9bfcc',
      muted: '#9ca3b2', muted2: '#8990a0', faint: '#6e7585',
      blueSlab: '#12598c', orangeSlab: '#b85214', blueInk: '#30aff8', orangeInk: '#f67f2f',
      vermilionDisplay: '#9e1c28', lossSlab: '#9e1c28',
      win: '#67d283', loss: '#ff706b', gold: '#f5bf3d', carryInk: '#4ab8fa', bgImage: 'none',
    },
    light: {
      page: '#f1f2f5', panel: '#ffffff', plaque: '#f7f8fa', panelHover: '#f7f8fa', modal: '#ffffff',
      line: '#dcdfe6', line2: '#e5e8ef', outline: '#1c1f26', frieze: '#1c1f26',
      ink: '#121418', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#242933', ink3: '#434957',
      muted: '#4d5566', muted2: '#5c6578', faint: '#768094',
      blueSlab: '#125687', orangeSlab: '#b34e12', blueInk: '#0b4a78', orangeInk: '#8f3b08',
      vermilionDisplay: '#991b26', lossSlab: '#991b26',
      win: '#126e32', loss: '#ba1818', gold: '#7d5200', carryInk: '#0b4a78', bgImage: 'none',
    },
  },
  arena: {
    name: '06 · Glassmorphic Arena (Translucent Frosted HUD)',
    fontDisplay: F_GROT, fontSans: F_SANS, fontMono: F_MONO, radius: '12px', slant: '0px', bw: '1px', blur: 'blur(12px)',
    dark: {
      page: '#070913', panel: 'rgba(19, 25, 43, 0.78)', panelHover: 'rgba(28, 37, 62, 0.88)', modal: '#111626',
      line: 'rgba(148, 163, 184, 0.28)', line2: 'rgba(148, 163, 184, 0.16)', outline: 'rgba(148, 163, 184, 0.45)',
      ink: '#f8fafc', strong: '#ffffff', onAccent: '#070913', onSlab: '#ffffff', ink2: '#e6edf6', ink3: '#d1dce8',
      muted: '#a9b9cf', muted2: '#94a6bf', faint: '#74859e',
      blueSlab: '#155282', orangeSlab: '#b84a18', blueInk: '#66c0f2', orangeInk: '#f79c63',
      lossSlab: '#9e193c', win: '#42e0a5', loss: '#ff708d',
      bgImage: 'radial-gradient(circle at 12% 18%, rgba(86, 180, 233, 0.15), transparent 40%), radial-gradient(circle at 88% 15%, rgba(245, 147, 86, 0.13), transparent 40%)',
    },
    light: {
      page: '#eef2ff', panel: 'rgba(255, 255, 255, 0.82)', panelHover: 'rgba(255, 255, 255, 0.96)', modal: '#ffffff',
      line: 'rgba(99, 102, 241, 0.24)', line2: 'rgba(99, 102, 241, 0.14)', outline: 'rgba(99, 102, 241, 0.42)',
      ink: '#0f172a', strong: '#020617', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#1e293b', ink3: '#334155',
      muted: '#475569', muted2: '#56647a', faint: '#738299',
      blueSlab: '#1c588c', orangeSlab: '#bd4d19', blueInk: '#134e7d', orangeInk: '#9e3a0e',
      lossSlab: '#9e193c', win: '#046c4e', loss: '#be123c',
      bgImage: 'radial-gradient(circle at 12% 18%, rgba(28, 88, 140, 0.12), transparent 40%), radial-gradient(circle at 88% 15%, rgba(189, 77, 25, 0.10), transparent 40%)',
    },
  },
  carbon: {
    name: '07 · Carbon Paddock (Motorsport Telemetry)',
    fontDisplay: F_SANS, fontSans: F_SANS, fontMono: F_MONO, radius: '5px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#09090b', panel: '#121215', panelHover: '#18181c', modal: '#141418',
      line: '#2a2a32', line2: '#1e1e24', outline: '#3e3e4a',
      ink: '#fafafa', strong: '#ffffff', onAccent: '#09090b', onSlab: '#ffffff', ink2: '#e7e7eb', ink3: '#d4d4dc',
      muted: '#adadb8', muted2: '#9595a3', faint: '#757584',
      blueSlab: '#1e4478', orangeSlab: '#ba5618', blueInk: '#78ace8', orangeInk: '#f7a154',
      lossSlab: '#99152c', win: '#28c991', loss: '#ff667e', bgImage: 'none',
    },
    light: {
      page: '#fafafa', panel: '#ffffff', panelHover: '#f4f4f5', modal: '#ffffff',
      line: '#e4e4e7', line2: '#f4f4f5', outline: '#a1a1aa',
      ink: '#09090b', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#18181b', ink3: '#27272a',
      muted: '#4d4d57', muted2: '#60606b', faint: '#7c7c87',
      blueSlab: '#1b3f70', orangeSlab: '#b35014', blueInk: '#163763', orangeInk: '#8f3c0a',
      lossSlab: '#99152c', win: '#046e4f', loss: '#b81430', bgImage: 'none',
    },
  },
  synthwave: {
    name: '08 · Mannfield Night (Neon Violet & Laser)',
    fontDisplay: F_GROT, fontSans: F_GROT, fontMono: F_MONO, radius: '4px', slant: '14px', bw: '1px', blur: 'none',
    dark: {
      page: '#0d061a', panel: '#160b2b', panelHover: '#21113e', modal: '#190d30',
      line: '#3e236e', line2: '#2b1652', outline: '#6138ab',
      ink: '#f7f2ff', strong: '#ffffff', onAccent: '#0d061a', onSlab: '#ffffff', ink2: '#eadcff', ink3: '#d8c2ff',
      muted: '#b99eff', muted2: '#a485fa', faint: '#8465d9',
      blueSlab: '#263c91', orangeSlab: '#bd3b28', blueInk: '#64e8ed', orangeInk: '#ff9469',
      lossSlab: '#96124b', win: '#2bf7ab', loss: '#ff699c',
      bgImage: 'radial-gradient(circle at 50% 0%, rgba(189, 59, 40, 0.15), transparent 55%)',
    },
    light: {
      page: '#f8f5ff', panel: '#ffffff', panelHover: '#f3edff', modal: '#ffffff',
      line: '#dfd2fc', line2: '#efe8ff', outline: '#b494f7',
      ink: '#1e0b3b', strong: '#0d031c', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#2e1459', ink3: '#43207a',
      muted: '#583494', muted2: '#6b47a8', faint: '#8666bd',
      blueSlab: '#293d8c', orangeSlab: '#b83725', blueInk: '#1f3175', orangeInk: '#942718',
      lossSlab: '#96124b', win: '#046e4f', loss: '#bd1358', bgImage: 'none',
    },
  },
  editorial: {
    name: '09 · Editorial Gazette (Fraunces Serif & Warm Press)',
    fontDisplay: F_SERIF, fontSans: F_SANS, fontMono: F_MONO, radius: '2px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#141210', panel: '#1d1a17', panelHover: '#26221e', modal: '#211d1a',
      line: '#3d362f', line2: '#2d2822', outline: '#5c5247',
      ink: '#f8f3eb', strong: '#ffffff', onAccent: '#141210', onSlab: '#ffffff', ink2: '#ebe3ce', ink3: '#dad0c5',
      muted: '#bab0a2', muted2: '#a69a8b', faint: '#877c6e',
      blueSlab: '#233d54', orangeSlab: '#9c4322', blueInk: '#8fb4d6', orangeInk: '#eb9365',
      lossSlab: '#801820', win: '#45deaa', loss: '#f5737b', bgImage: 'none',
    },
    light: {
      page: '#f7f3eb', panel: '#fffdf9', panelHover: '#f2ece1', modal: '#fffdf9',
      line: '#dcd3c2', line2: '#eae3d5', outline: '#a69882',
      ink: '#1c1814', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#332c25', ink3: '#4a4036',
      muted: '#5c5043', muted2: '#6e6051', faint: '#87796a',
      blueSlab: '#20384d', orangeSlab: '#963f1e', blueInk: '#1a3347', orangeInk: '#7d3114',
      lossSlab: '#801820', win: '#116931', loss: '#9e1c26', bgImage: 'none',
    },
  },
  nordic: {
    name: '10 · Nordic Frost (Clean Arctic Slate & Ice)',
    fontDisplay: F_GROT, fontSans: F_SANS, fontMono: F_MONO, radius: '8px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#1e222a', panel: '#262b36', panelHover: '#2e3440', modal: '#292f3b',
      line: '#434c5e', line2: '#353c4a', outline: '#5e6b82',
      ink: '#f2f5fa', strong: '#ffffff', onAccent: '#1b1f26', onSlab: '#ffffff', ink2: '#e5eaf2', ink3: '#d8e0ec',
      muted: '#b3bfd1', muted2: '#9eabc0', faint: '#8290a8',
      blueSlab: '#2e506e', orangeSlab: '#9e4c2e', blueInk: '#93cce0', orangeInk: '#e89d84',
      lossSlab: '#822933', win: '#b3d19a', loss: '#f28590', bgImage: 'none',
    },
    light: {
      page: '#eceff4', panel: '#ffffff', panelHover: '#e5e9f0', modal: '#ffffff',
      line: '#d8dee9', line2: '#e5e9f0', outline: '#9aa7bf',
      ink: '#242933', strong: '#14171d', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#323947', ink3: '#3f4759',
      muted: '#485266', muted2: '#566178', faint: '#738099',
      blueSlab: '#2c4d69', orangeSlab: '#99482b', blueInk: '#234159', orangeInk: '#7d371e',
      lossSlab: '#822933', win: '#3b6625', loss: '#9e2634', bgImage: 'none',
    },
  },
  terminal: {
    name: '11 · Retro CRT Phosphor (Monospace Mainframe)',
    fontDisplay: F_MONO, fontSans: F_MONO, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#040a06', panel: '#08140c', panelHover: '#0d2114', modal: '#09170e',
      line: '#1c472c', line2: '#13301e', outline: '#2d7546',
      ink: '#dcfce7', strong: '#f0fdf4', onAccent: '#040a06', onSlab: '#ecfdf5', ink2: '#b2f7d4', ink3: '#80edb9',
      muted: '#4ade9f', muted2: '#2bc48a', faint: '#199466',
      blueSlab: '#0f4c5c', orangeSlab: '#964b12', blueInk: '#5ee0d4', orangeInk: '#ffb866',
      lossSlab: '#821c26', win: '#52e686', loss: '#ff7878',
      bgImage: 'repeating-linear-gradient(0deg, rgba(16, 185, 129, 0.03) 0px, rgba(16, 185, 129, 0.03) 1px, transparent 1px, transparent 3px)',
    },
    light: {
      page: '#e6f0ea', panel: '#f4faf6', panelHover: '#ffffff', modal: '#ffffff',
      line: '#a7cbb6', line2: '#cce3d5', outline: '#558c6d',
      ink: '#062413', strong: '#021208', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#0c381f', ink3: '#144d2c',
      muted: '#1e5c37', muted2: '#296e46', faint: '#4c8564',
      blueSlab: '#125161', orangeSlab: '#914710', blueInk: '#0d4452', orangeInk: '#783808',
      lossSlab: '#821c26', win: '#04694a', loss: '#9b1c28', bgImage: 'none',
    },
  },
  solar: {
    name: '12 · Utopia Daylight (Warm Sand, Blue & Terracotta)',
    fontDisplay: F_COND, fontSans: F_SANS, fontMono: F_MONO, radius: '4px', slant: '10px', bw: '1px', blur: 'none',
    dark: {
      page: '#0f1416', panel: '#171f22', panelHover: '#1f2a2e', modal: '#192226',
      line: '#33444a', line2: '#243035', outline: '#4b636c',
      ink: '#f7f4ee', strong: '#ffffff', onAccent: '#0f1416', onSlab: '#ffffff', ink2: '#e8e3d9', ink3: '#d6cfc2',
      muted: '#aebbc0', muted2: '#98a7ac', faint: '#78888e',
      blueSlab: '#16536e', orangeSlab: '#ad4822', blueInk: '#69cfcd', orangeInk: '#f79972',
      lossSlab: '#8f1928', win: '#38d973', loss: '#ff707e', bgImage: 'none',
    },
    light: {
      page: '#f5efe6', panel: '#fffdf9', panelHover: '#efe7da', modal: '#fffdf9',
      line: '#dcd2c0', line2: '#ebe3d5', outline: '#a69881',
      ink: '#182226', strong: '#0a1012', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#29383d', ink3: '#384a52',
      muted: '#485a61', muted2: '#556870', faint: '#73858c',
      blueSlab: '#195670', orangeSlab: '#a84520', blueInk: '#12465c', orangeInk: '#873314',
      lossSlab: '#8f1928', win: '#0f6630', loss: '#b01b2e', bgImage: 'none',
    },
  },
  industrial: {
    name: '13 · Cobalt Blueprint (CAD Engineering Grid)',
    fontDisplay: F_COND, fontSans: F_MONO, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '2px', blur: 'none',
    dark: {
      page: '#091936', panel: '#0e234a', panelHover: '#142f61', modal: '#102752',
      line: '#2a5299', line2: '#1c3b75', outline: '#4879cf',
      ink: '#f2f7ff', strong: '#ffffff', onAccent: '#091936', onSlab: '#ffffff', ink2: '#dceaff', ink3: '#bfd9ff',
      muted: '#99c1fa', muted2: '#82b0f2', faint: '#6291d9',
      blueSlab: '#164e87', orangeSlab: '#b05219', blueInk: '#88dbff', orangeInk: '#ffb06b',
      lossSlab: '#941634', win: '#52e686', loss: '#ff8095',
      bgImage: 'linear-gradient(rgba(255,255,255,0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.04) 1px, transparent 1px)',
    },
    light: {
      page: '#e8f0fe', panel: '#ffffff', panelHover: '#f1f6ff', modal: '#ffffff',
      line: '#93b8f5', line2: '#c9dcFC', outline: '#4b83eb',
      ink: '#081b42', strong: '#030d24', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#102a63', ink3: '#1d3c7d',
      muted: '#2d4f94', muted2: '#3d61ab', faint: '#5d7ec2',
      blueSlab: '#174d85', orangeSlab: '#ab4e16', blueInk: '#103d6b', orangeInk: '#8a3b0b',
      lossSlab: '#941634', win: '#04694a', loss: '#b8183d',
      bgImage: 'linear-gradient(rgba(8,27,66,0.05) 1px, transparent 1px), linear-gradient(90deg, rgba(8,27,66,0.05) 1px, transparent 1px)',
    },
  },
  oled: {
    name: '14 · Pure OLED Pitch (Zero-Glow True Black)',
    fontDisplay: F_COND, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '16px', bw: '1px', blur: 'none',
    dark: {
      page: '#000000', panel: '#070709', panelHover: '#101014', modal: '#0a0a0d',
      line: '#24242e', line2: '#17171f', outline: '#3b3b4d',
      ink: '#ffffff', strong: '#ffffff', onAccent: '#000000', onSlab: '#ffffff', ink2: '#ebebf2', ink3: '#d4d4e0',
      muted: '#adadbe', muted2: '#9595a8', faint: '#747487',
      blueSlab: '#163878', orangeSlab: '#a33f14', blueInk: '#78abfa', orangeInk: '#ff955c',
      lossSlab: '#8a1127', win: '#38d973', loss: '#ff667a', bgImage: 'none',
    },
    light: {
      page: '#ffffff', panel: '#f8fafc', panelHover: '#f1f5f9', modal: '#ffffff',
      line: '#cbd5e1', line2: '#e2e8f0', outline: '#94a3b8',
      ink: '#020617', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#0f172a', ink3: '#1e293b',
      muted: '#475569', muted2: '#59677d', faint: '#77869c',
      blueSlab: '#183b7a', orangeSlab: '#9e3c12', blueInk: '#122e63', orangeInk: '#7d2c08',
      lossSlab: '#8a1127', win: '#116e32', loss: '#a8132d', bgImage: 'none',
    },
  },
};

export function applyThemeToDOM(styleKey: ThemeStyleKey, mode: ColorMode): void {
  if (typeof document === 'undefined') return;
  const s = THEME_STYLES[styleKey] || THEME_STYLES.broadcast;
  const effectiveMode: ColorMode = styleKey === 'arashi' ? 'light' : mode;
  const p = s[mode] || s.dark;
  const statPalette = STAT_COLOR_PALETTES[effectiveMode];
  document.documentElement.setAttribute('data-style', styleKey);
  document.documentElement.setAttribute('data-mode', mode);
  const r = document.documentElement.style;
  r.colorScheme = effectiveMode;
  const metaScheme = document.getElementById('meta-color-scheme');
  if (metaScheme) metaScheme.setAttribute('content', effectiveMode);
  r.setProperty('--t-page', p.page);
  r.setProperty('--t-panel', p.panel);
  r.setProperty('--t-plaque', p.plaque || p.page);
  r.setProperty('--t-panel-hover', p.panelHover);
  r.setProperty('--t-modal', p.modal);
  r.setProperty('--t-line', p.line);
  r.setProperty('--t-line-2', p.line2);
  r.setProperty('--t-outline', p.outline);
  r.setProperty('--t-frieze', p.frieze || p.outline);
  r.setProperty('--t-ink', p.ink);
  r.setProperty('--t-ink-strong', p.strong);
  r.setProperty('--t-ink-on-accent', p.onAccent);
  r.setProperty('--t-on-slab', p.onSlab || '#ffffff');
  r.setProperty('--t-ink-2', p.ink2);
  r.setProperty('--t-ink-3', p.ink3);
  r.setProperty('--t-muted', p.muted);
  r.setProperty('--t-muted-2', p.muted2);
  r.setProperty('--t-faint', p.faint);
  r.setProperty('--t-blue-slab', p.blueSlab);
  r.setProperty('--t-orange-slab', p.orangeSlab);
  r.setProperty('--t-blue-ink', p.blueInk);
  r.setProperty('--t-orange-ink', p.orangeInk);
  r.setProperty('--t-vermilion-display', p.vermilionDisplay || p.orangeSlab);
  r.setProperty('--t-loss-slab', p.lossSlab || p.loss);
  r.setProperty('--t-win', p.win);
  r.setProperty('--t-loss', p.loss);
  r.setProperty('--t-gold', p.gold || (effectiveMode === 'dark' ? '#efc463' : '#7a4a00'));
  r.setProperty('--t-carry-ink', p.carryInk || (effectiveMode === 'dark' ? '#88bad9' : '#164561'));
  for (const key of Object.keys(STAT_CSS_VAR) as StatColorKey[]) {
    r.setProperty(STAT_CSS_VAR[key], statPalette[key]);
  }
  r.setProperty('--t-font-display', s.fontDisplay);
  r.setProperty('--t-font-sans', s.fontSans);
  r.setProperty('--t-font-mono', s.fontMono);
  r.setProperty('--t-radius', s.radius);
  r.setProperty('--t-slant', s.slant);
  r.setProperty('--t-bw', s.bw);
  r.setProperty('--t-blur', s.blur);
  r.setProperty('--t-bg-image', p.bgImage);
  if (styleKey === 'industrial' && p.bgImage !== 'none') {
    document.body.style.backgroundSize = '28px 28px';
  } else if (styleKey === 'tactical' && p.bgImage !== 'none') {
    document.body.style.backgroundSize = '18px 18px';
  } else {
    document.body.style.backgroundSize = 'auto';
  }
}

export function applyFontScaleToDOM(scale: FontScale): void {
  if (typeof document === 'undefined') return;
  document.documentElement.style.fontSize = scale === 'xl' ? '18.4px' : '16px';
}
