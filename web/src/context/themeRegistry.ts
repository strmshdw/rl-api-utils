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
  line: '#cfc7b8', line2: '#dfd8ca', outline: '#1c1917', frieze: '#4a3f33',
  ink: '#171412', strong: '#090807', onAccent: '#fbf9f5', onSlab: '#fbf9f5', ink2: '#362f28', ink3: '#4a4036',
  muted: '#615548', muted2: '#786b5c', faint: '#968877',
  blueSlab: '#1d3f58', orangeSlab: '#a8461c', blueInk: '#1a4363', orangeInk: '#8f3812',
  vermilionDisplay: '#94171e', lossSlab: '#94171e',
  win: '#1e523b', loss: '#9e141c', gold: '#8c5604', carryInk: '#1b4965', bgImage: 'none',
};

export const THEME_STYLES: Record<ThemeStyleKey, ThemeStyleDefinition> = {
  broadcast: {
    name: '01 · Deco Monolith: Woodblock (Default)',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: {
      page: '#0a0908', panel: '#181614', plaque: '#0f0d0b', panelHover: '#221f1c', modal: '#181614',
      line: '#38332d', line2: '#292520', outline: '#e2dbd0', frieze: '#d8d0c5',
      ink: '#f2efe9', strong: '#fbf9f5', onAccent: '#12100e', onSlab: '#fbf9f5', ink2: '#cfc7b8', ink3: '#b8afa0',
      muted: '#918779', muted2: '#786f62', faint: '#5c544b',
      blueSlab: '#1e3a52', orangeSlab: '#9e4726', blueInk: '#789dba', orangeInk: '#d9825b',
      vermilionDisplay: '#8f1822', lossSlab: '#8f1822',
      win: '#82a890', loss: '#e04852', gold: '#deb052', carryInk: '#7da8c4', bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  tactical: {
    name: '02 · Deco Monolith: Lacquer & Brass',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: {
      page: '#040302', panel: '#120e09', plaque: '#080604', panelHover: '#1d160e', modal: '#120e09',
      line: '#4d391d', line2: '#332512', outline: '#d4a853', frieze: '#e5b85c',
      ink: '#f5ebd6', strong: '#fffdf7', onAccent: '#120e09', onSlab: '#fffdf9', ink2: '#d4c3a3', ink3: '#b8a482',
      muted: '#9e8866', muted2: '#806c4e', faint: '#5e4f37',
      blueSlab: '#13404d', orangeSlab: '#9c4f1a', blueInk: '#62a8ba', orangeInk: '#e09145',
      vermilionDisplay: '#8c161e', lossSlab: '#8c161e',
      win: '#82a890', loss: '#e0434d', gold: '#e5b85c', carryInk: '#7da8c4', bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  swiss: {
    name: '03 · Deco Monolith: Carved Inkstone',
    fontDisplay: F_DECO, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1.5px', blur: 'none',
    dark: {
      page: '#0c0b0a', panel: '#1d1916', plaque: '#12100e', panelHover: '#26211d', modal: '#1d1916',
      line: '#3d352e', line2: '#2b2520', outline: '#b5a692', frieze: '#cfc2b0',
      ink: '#ede6d8', strong: '#fbf8f3', onAccent: '#12100e', onSlab: '#fbf8f3', ink2: '#bfb5a2', ink3: '#a69b88',
      muted: '#8f8270', muted2: '#736859', faint: '#574e42',
      blueSlab: '#2b4559', orangeSlab: '#8f492a', blueInk: '#80a2bd', orangeInk: '#cf825f',
      vermilionDisplay: '#821d24', lossSlab: '#821d24',
      win: '#82a890', loss: '#d94b54', gold: '#deb052', carryInk: '#7da8c4', bgImage: 'none',
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
      line: '#1c1e22', line2: '#23262c', outline: '#2a2d33', frieze: '#2a2d33',
      ink: '#f2f3f5', strong: '#ffffff', onAccent: '#0a0b0d', onSlab: '#ffffff', ink2: '#c4c7cd', ink3: '#9a9ea7',
      muted: '#7b7f88', muted2: '#6c7079', faint: '#4a4e57',
      blueSlab: '#12598c', orangeSlab: '#b85214', blueInk: '#30aff8', orangeInk: '#f67f2f',
      vermilionDisplay: '#9e1c28', lossSlab: '#9e1c28',
      win: '#67d283', loss: '#ff6a65', gold: '#f0b429', carryInk: '#30aff8', bgImage: 'none',
    },
    light: {
      page: '#f1f2f5', panel: '#ffffff', plaque: '#f7f8fa', panelHover: '#f7f8fa', modal: '#ffffff',
      line: '#dcdfe6', line2: '#e5e8ef', outline: '#1c1f26', frieze: '#1c1f26',
      ink: '#121418', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#242933', ink3: '#434957',
      muted: '#525a6b', muted2: '#646d80', faint: '#7e879b',
      blueSlab: '#125687', orangeSlab: '#b34e12', blueInk: '#0b4a78', orangeInk: '#8f3b08',
      vermilionDisplay: '#991b26', lossSlab: '#991b26',
      win: '#157a38', loss: '#c41c1c', gold: '#b87d04', carryInk: '#0b4a78', bgImage: 'none',
    },
  },
  arena: {
    name: '06 · Glassmorphic Arena (Translucent Frosted HUD)',
    fontDisplay: F_GROT, fontSans: F_SANS, fontMono: F_MONO, radius: '12px', slant: '0px', bw: '1px', blur: 'blur(12px)',
    dark: {
      page: '#070913', panel: 'rgba(19, 25, 43, 0.78)', panelHover: 'rgba(28, 37, 62, 0.88)', modal: '#111626',
      line: 'rgba(148, 163, 184, 0.2)', line2: 'rgba(148, 163, 184, 0.1)', outline: 'rgba(148, 163, 184, 0.35)',
      ink: '#f8fafc', strong: '#ffffff', onAccent: '#070913', onSlab: '#ffffff', ink2: '#e2e8f0', ink3: '#cbd5e1',
      muted: '#94a3b8', muted2: '#78879c', faint: '#566378',
      blueSlab: '#155282', orangeSlab: '#b84a18', blueInk: '#56b4e9', orangeInk: '#f59356',
      lossSlab: '#9e193c', win: '#34d399', loss: '#f45b7a',
      bgImage: 'radial-gradient(circle at 12% 18%, rgba(86, 180, 233, 0.15), transparent 40%), radial-gradient(circle at 88% 15%, rgba(245, 147, 86, 0.13), transparent 40%)',
    },
    light: {
      page: '#eef2ff', panel: 'rgba(255, 255, 255, 0.82)', panelHover: 'rgba(255, 255, 255, 0.96)', modal: '#ffffff',
      line: 'rgba(99, 102, 241, 0.2)', line2: 'rgba(99, 102, 241, 0.1)', outline: 'rgba(99, 102, 241, 0.38)',
      ink: '#0f172a', strong: '#020617', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#1e293b', ink3: '#334155',
      muted: '#475569', muted2: '#64748b', faint: '#94a3b8',
      blueSlab: '#1c588c', orangeSlab: '#bd4d19', blueInk: '#134e7d', orangeInk: '#9e3a0e',
      lossSlab: '#9e193c', win: '#059669', loss: '#be123c',
      bgImage: 'radial-gradient(circle at 12% 18%, rgba(28, 88, 140, 0.12), transparent 40%), radial-gradient(circle at 88% 15%, rgba(189, 77, 25, 0.10), transparent 40%)',
    },
  },
  carbon: {
    name: '07 · Carbon Paddock (Motorsport Telemetry)',
    fontDisplay: F_SANS, fontSans: F_SANS, fontMono: F_MONO, radius: '5px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#09090b', panel: '#121215', panelHover: '#18181c', modal: '#141418',
      line: '#222228', line2: '#18181d', outline: '#32323b',
      ink: '#fafafa', strong: '#ffffff', onAccent: '#09090b', onSlab: '#ffffff', ink2: '#e4e4e7', ink3: '#d4d4d8',
      muted: '#a1a1aa', muted2: '#82828c', faint: '#5a5a66',
      blueSlab: '#1e4478', orangeSlab: '#ba5618', blueInk: '#6ea0e0', orangeInk: '#f29a4b',
      lossSlab: '#99152c', win: '#10b981', loss: '#f04864', bgImage: 'none',
    },
    light: {
      page: '#fafafa', panel: '#ffffff', panelHover: '#f4f4f5', modal: '#ffffff',
      line: '#e4e4e7', line2: '#f4f4f5', outline: '#d4d4d8',
      ink: '#09090b', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#18181b', ink3: '#27272a',
      muted: '#52525b', muted2: '#71717a', faint: '#a1a1aa',
      blueSlab: '#1b3f70', orangeSlab: '#b35014', blueInk: '#163763', orangeInk: '#8f3c0a',
      lossSlab: '#99152c', win: '#047857', loss: '#b81430', bgImage: 'none',
    },
  },
  synthwave: {
    name: '08 · Mannfield Night (Neon Violet & Laser)',
    fontDisplay: F_GROT, fontSans: F_GROT, fontMono: F_MONO, radius: '4px', slant: '14px', bw: '1px', blur: 'none',
    dark: {
      page: '#0d061a', panel: '#160b2b', panelHover: '#21113e', modal: '#190d30',
      line: '#331c5c', line2: '#231242', outline: '#522d91',
      ink: '#f5f0ff', strong: '#ffffff', onAccent: '#0d061a', onSlab: '#ffffff', ink2: '#e4d4ff', ink3: '#cdb4ff',
      muted: '#a78bfa', muted2: '#8b6ce0', faint: '#6546b5',
      blueSlab: '#263c91', orangeSlab: '#bd3b28', blueInk: '#5ce1e6', orangeInk: '#ff8a5c',
      lossSlab: '#96124b', win: '#00f59b', loss: '#ff4785',
      bgImage: 'radial-gradient(circle at 50% 0%, rgba(189, 59, 40, 0.15), transparent 55%)',
    },
    light: {
      page: '#f8f5ff', panel: '#ffffff', panelHover: '#f3edff', modal: '#ffffff',
      line: '#dfd2fc', line2: '#efe8ff', outline: '#c4abfa',
      ink: '#1e0b3b', strong: '#0d031c', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#2e1459', ink3: '#43207a',
      muted: '#623ca1', muted2: '#7c57ba', faint: '#a688db',
      blueSlab: '#293d8c', orangeSlab: '#b83725', blueInk: '#1f3175', orangeInk: '#942718',
      lossSlab: '#96124b', win: '#059669', loss: '#bd1358', bgImage: 'none',
    },
  },
  editorial: {
    name: '09 · Editorial Gazette (Fraunces Serif & Warm Press)',
    fontDisplay: F_SERIF, fontSans: F_SANS, fontMono: F_MONO, radius: '2px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#141210', panel: '#1d1a17', panelHover: '#26221e', modal: '#211d1a',
      line: '#332d27', line2: '#26221d', outline: '#4d443b',
      ink: '#f5efe6', strong: '#ffffff', onAccent: '#141210', onSlab: '#ffffff', ink2: '#e6dec3', ink3: '#d4c8be',
      muted: '#a89b8c', muted2: '#8c7f70', faint: '#695e52',
      blueSlab: '#233d54', orangeSlab: '#9c4322', blueInk: '#7ea3c4', orangeInk: '#e08658',
      lossSlab: '#801820', win: '#34d399', loss: '#e0535c', bgImage: 'none',
    },
    light: {
      page: '#f7f3eb', panel: '#fffdf9', panelHover: '#f2ece1', modal: '#fffdf9',
      line: '#dcd3c2', line2: '#eae3d5', outline: '#b8ab96',
      ink: '#1c1814', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#332c25', ink3: '#4a4036',
      muted: '#695c4e', muted2: '#827363', faint: '#a89b8c',
      blueSlab: '#20384d', orangeSlab: '#963f1e', blueInk: '#1a3347', orangeInk: '#7d3114',
      lossSlab: '#801820', win: '#15803d', loss: '#9e1c26', bgImage: 'none',
    },
  },
  nordic: {
    name: '10 · Nordic Frost (Clean Arctic Slate & Ice)',
    fontDisplay: F_GROT, fontSans: F_SANS, fontMono: F_MONO, radius: '8px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#1e222a', panel: '#262b36', panelHover: '#2e3440', modal: '#292f3b',
      line: '#3b4252', line2: '#2e3440', outline: '#4c566a',
      ink: '#eceff4', strong: '#ffffff', onAccent: '#1e222a', onSlab: '#ffffff', ink2: '#e5e9f0', ink3: '#d8dee9',
      muted: '#9aa5b8', muted2: '#7e8a9e', faint: '#616e88',
      blueSlab: '#2e506e', orangeSlab: '#9e4c2e', blueInk: '#88c0d0', orangeInk: '#d08770',
      lossSlab: '#822933', win: '#a3be8c', loss: '#bf616a', bgImage: 'none',
    },
    light: {
      page: '#eceff4', panel: '#ffffff', panelHover: '#e5e9f0', modal: '#ffffff',
      line: '#d8dee9', line2: '#e5e9f0', outline: '#b8c2d4',
      ink: '#2e3440', strong: '#191d24', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#3b4252', ink3: '#434c5e',
      muted: '#4c566a', muted2: '#647087', faint: '#8f9bb3',
      blueSlab: '#2c4d69', orangeSlab: '#99482b', blueInk: '#234159', orangeInk: '#7d371e',
      lossSlab: '#822933', win: '#4f7a38', loss: '#9e333f', bgImage: 'none',
    },
  },
  terminal: {
    name: '11 · Retro CRT Phosphor (Monospace Mainframe)',
    fontDisplay: F_MONO, fontSans: F_MONO, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '1px', blur: 'none',
    dark: {
      page: '#040a06', panel: '#08140c', panelHover: '#0d2114', modal: '#09170e',
      line: '#163822', line2: '#0f2617', outline: '#235c37',
      ink: '#d1fae5', strong: '#ecfdf5', onAccent: '#040a06', onSlab: '#ecfdf5', ink2: '#a7f3d0', ink3: '#6ee7b7',
      muted: '#34d399', muted2: '#10b981', faint: '#047857',
      blueSlab: '#0f4c5c', orangeSlab: '#964b12', blueInk: '#4fd1c5', orangeInk: '#f6ad55',
      lossSlab: '#821c26', win: '#4ade80', loss: '#f56565',
      bgImage: 'repeating-linear-gradient(0deg, rgba(16, 185, 129, 0.03) 0px, rgba(16, 185, 129, 0.03) 1px, transparent 1px, transparent 3px)',
    },
    light: {
      page: '#e6f0ea', panel: '#f4faf6', panelHover: '#ffffff', modal: '#ffffff',
      line: '#a7cbb6', line2: '#cce3d5', outline: '#6ca384',
      ink: '#062413', strong: '#021208', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#0c381f', ink3: '#144d2c',
      muted: '#23663f', muted2: '#357d52', faint: '#6ca384',
      blueSlab: '#125161', orangeSlab: '#914710', blueInk: '#0d4452', orangeInk: '#783808',
      lossSlab: '#821c26', win: '#047857', loss: '#9b1c28', bgImage: 'none',
    },
  },
  solar: {
    name: '12 · Utopia Daylight (Warm Sand, Blue & Terracotta)',
    fontDisplay: F_COND, fontSans: F_SANS, fontMono: F_MONO, radius: '4px', slant: '10px', bw: '1px', blur: 'none',
    dark: {
      page: '#0f1416', panel: '#171f22', panelHover: '#1f2a2e', modal: '#192226',
      line: '#2a383d', line2: '#1e282b', outline: '#3d5159',
      ink: '#f4f1ea', strong: '#ffffff', onAccent: '#0f1416', onSlab: '#ffffff', ink2: '#e2ddd2', ink3: '#ccc5b8',
      muted: '#9ca8ab', muted2: '#7e8c90', faint: '#5c696d',
      blueSlab: '#16536e', orangeSlab: '#ad4822', blueInk: '#5bc0be', orangeInk: '#ed8b64',
      lossSlab: '#8f1928', win: '#22c55e', loss: '#eb4d5d', bgImage: 'none',
    },
    light: {
      page: '#f5efe6', panel: '#fffdf9', panelHover: '#efe7da', modal: '#fffdf9',
      line: '#dcd2c0', line2: '#ebe3d5', outline: '#bfb29b',
      ink: '#182226', strong: '#0a1012', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#29383d', ink3: '#3d4f57',
      muted: '#596c73', muted2: '#73868c', faint: '#9ba9ad',
      blueSlab: '#195670', orangeSlab: '#a84520', blueInk: '#12465c', orangeInk: '#873314',
      lossSlab: '#8f1928', win: '#15803d', loss: '#b01b2e', bgImage: 'none',
    },
  },
  industrial: {
    name: '13 · Cobalt Blueprint (CAD Engineering Grid)',
    fontDisplay: F_COND, fontSans: F_MONO, fontMono: F_MONO, radius: '0px', slant: '0px', bw: '2px', blur: 'none',
    dark: {
      page: '#091936', panel: '#0e234a', panelHover: '#142f61', modal: '#102752',
      line: '#234582', line2: '#173263', outline: '#3b67b5',
      ink: '#f0f6ff', strong: '#ffffff', onAccent: '#091936', onSlab: '#ffffff', ink2: '#d6e6ff', ink3: '#b3d1ff',
      muted: '#85b2f2', muted2: '#6394db', faint: '#4272b8',
      blueSlab: '#164e87', orangeSlab: '#b05219', blueInk: '#7dd3fc', orangeInk: '#f6a45c',
      lossSlab: '#941634', win: '#4ade80', loss: '#fb7185',
      bgImage: 'linear-gradient(rgba(255,255,255,0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.04) 1px, transparent 1px)',
    },
    light: {
      page: '#e8f0fe', panel: '#ffffff', panelHover: '#f1f6ff', modal: '#ffffff',
      line: '#93b8f5', line2: '#c9dcFC', outline: '#4b83eb',
      ink: '#081b42', strong: '#030d24', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#102a63', ink3: '#1d3c7d',
      muted: '#34589e', muted2: '#4d71b8', faint: '#82a2de',
      blueSlab: '#174d85', orangeSlab: '#ab4e16', blueInk: '#103d6b', orangeInk: '#8a3b0b',
      lossSlab: '#941634', win: '#059669', loss: '#b8183d',
      bgImage: 'linear-gradient(rgba(8,27,66,0.05) 1px, transparent 1px), linear-gradient(90deg, rgba(8,27,66,0.05) 1px, transparent 1px)',
    },
  },
  oled: {
    name: '14 · Pure OLED Pitch (Zero-Glow True Black)',
    fontDisplay: F_COND, fontSans: F_SANS, fontMono: F_MONO, radius: '0px', slant: '16px', bw: '1px', blur: 'none',
    dark: {
      page: '#000000', panel: '#070709', panelHover: '#101014', modal: '#0a0a0d',
      line: '#1c1c24', line2: '#121217', outline: '#2e2e3b',
      ink: '#ffffff', strong: '#ffffff', onAccent: '#000000', onSlab: '#ffffff', ink2: '#e4e4eb', ink3: '#c6c6d1',
      muted: '#9898a8', muted2: '#7a7a8c', faint: '#555566',
      blueSlab: '#163878', orangeSlab: '#a33f14', blueInk: '#669df6', orangeInk: '#f5864c',
      lossSlab: '#8a1127', win: '#22c55e', loss: '#f2465c', bgImage: 'none',
    },
    light: {
      page: '#ffffff', panel: '#f8fafc', panelHover: '#f1f5f9', modal: '#ffffff',
      line: '#cbd5e1', line2: '#e2e8f0', outline: '#94a3b8',
      ink: '#020617', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#0f172a', ink3: '#1e293b',
      muted: '#475569', muted2: '#64748b', faint: '#94a3b8',
      blueSlab: '#183b7a', orangeSlab: '#9e3c12', blueInk: '#122e63', orangeInk: '#7d2c08',
      lossSlab: '#8a1127', win: '#15803d', loss: '#a8132d', bgImage: 'none',
    },
  },
};

export function applyThemeToDOM(styleKey: ThemeStyleKey, mode: ColorMode): void {
  if (typeof document === 'undefined') return;
  const s = THEME_STYLES[styleKey] || THEME_STYLES.broadcast;
  const p = s[mode] || s.dark;
  document.documentElement.setAttribute('data-style', styleKey);
  document.documentElement.setAttribute('data-mode', mode);
  const r = document.documentElement.style;
  r.colorScheme = mode;
  const metaScheme = document.getElementById('meta-color-scheme');
  if (metaScheme) metaScheme.setAttribute('content', mode);
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
  r.setProperty('--t-gold', p.gold || (mode === 'dark' ? '#deb052' : '#8c5604'));
  r.setProperty('--t-carry-ink', p.carryInk || (mode === 'dark' ? '#7da8c4' : '#1b4965'));
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
