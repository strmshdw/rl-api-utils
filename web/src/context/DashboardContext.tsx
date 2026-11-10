import React, { createContext, useContext, useEffect, useMemo, useState, useCallback } from 'react';
import {
  getAppVersion,
  isDemoMode,
  isPreviewBuildAllowed,
  setPreviewModeEnabled,
} from '../utils/sampleData';

export type ThemeStyleKey =
  | 'broadcast'
  | 'tactical'
  | 'swiss'
  | 'arashi'
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
export type StatMode = 'avg' | 'total';
export type DateRangePreset = 'all' | 'today' | '7d' | '30d' | 'custom';
export type ReplayLimitPreviewState = 'auto' | 'warn' | 'error' | 'ok';
export type LiveViewMode = 'match' | 'idle';

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
const F_SANS = "'IBM Plex Sans', 'Inter', system-ui, sans-serif";
const F_GROT = "'Space Grotesk', 'Inter', sans-serif";
const F_MONO = "'JetBrains Mono', monospace";
const F_SERIF = "'Fraunces', Georgia, serif";

const D2_ARASHI_LIGHT_PALETTE: ThemePalette = {
  page: '#ebe5d9',
  panel: '#f4efe6',
  plaque: '#fcfaf6',
  panelHover: '#fcfaf6',
  modal: '#fcfaf6',
  line: '#cfc7b8',
  line2: '#dfd8ca',
  outline: '#1c1917',
  frieze: '#4a3f33',
  ink: '#171412',
  strong: '#090807',
  onAccent: '#fbf9f5',
  onSlab: '#fbf9f5',
  ink2: '#362f28',
  ink3: '#4a4036',
  muted: '#615548',
  muted2: '#786b5c',
  faint: '#968877',
  blueSlab: '#184361',
  orangeSlab: '#bf4a13',
  blueInk: '#16425b',
  orangeInk: '#9e3908',
  vermilionDisplay: '#a8180e',
  lossSlab: '#a8180e',
  win: '#1e523b',
  loss: '#9e140a',
  gold: '#8c5604',
  carryInk: '#1b4965',
  bgImage: 'none',
};

export const THEME_STYLES: Record<ThemeStyleKey, ThemeStyleDefinition> = {
  broadcast: {
    name: '01 · Deco Monolith: Woodblock (Default)',
    fontDisplay: F_DECO,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '0px',
    slant: '0px',
    bw: '1.5px',
    blur: 'none',
    dark: {
      page: '#0a0908', panel: '#181614', plaque: '#0f0d0b', panelHover: '#221f1c', modal: '#181614',
      line: '#38332d', line2: '#292520', outline: '#e2dbd0', frieze: '#d8d0c5',
      ink: '#f2efe9', strong: '#fbf9f5', onAccent: '#12100e', onSlab: '#fbf9f5', ink2: '#cfc7b8', ink3: '#b8afa0',
      muted: '#918779', muted2: '#786f62', faint: '#5c544b',
      blueSlab: '#1c4c70', orangeSlab: '#c45119', blueInk: '#68a8d4', orangeInk: '#eb7f47',
      vermilionDisplay: '#d4261d', lossSlab: '#b81d13',
      win: '#82a890', loss: '#e03e33', gold: '#deb052', carryInk: '#7da8c4',
      bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  tactical: {
    name: '02 · Deco Monolith: Lacquer & Brass',
    fontDisplay: F_DECO,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '0px',
    slant: '0px',
    bw: '1.5px',
    blur: 'none',
    dark: {
      page: '#040302', panel: '#120e09', plaque: '#080604', panelHover: '#1d160e', modal: '#120e09',
      line: '#4d391d', line2: '#332512', outline: '#d4a853', frieze: '#e5b85c',
      ink: '#f5ebd6', strong: '#fffdf7', onAccent: '#120e09', onSlab: '#fffdf9', ink2: '#d4c3a3', ink3: '#b8a482',
      muted: '#9e8866', muted2: '#806c4e', faint: '#5e4f37',
      blueSlab: '#18456b', orangeSlab: '#bd4b14', blueInk: '#66a3cf', orangeInk: '#e87b42',
      vermilionDisplay: '#d4261d', lossSlab: '#a8180e',
      win: '#82a890', loss: '#e03e33', gold: '#e5b85c', carryInk: '#7da8c4',
      bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  swiss: {
    name: '03 · Deco Monolith: Carved Inkstone',
    fontDisplay: F_DECO,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '0px',
    slant: '0px',
    bw: '1.5px',
    blur: 'none',
    dark: {
      page: '#0c0b0a', panel: '#1d1916', plaque: '#12100e', panelHover: '#26211d', modal: '#1d1916',
      line: '#3d352e', line2: '#2b2520', outline: '#b5a692', frieze: '#cfc2b0',
      ink: '#ede6d8', strong: '#fbf8f3', onAccent: '#12100e', onSlab: '#fbf8f3', ink2: '#bfb5a2', ink3: '#a69b88',
      muted: '#8f8270', muted2: '#736859', faint: '#574e42',
      blueSlab: '#225070', orangeSlab: '#ba501c', blueInk: '#72aacf', orangeInk: '#e3804b',
      vermilionDisplay: '#d4261d', lossSlab: '#b81d13',
      win: '#82a890', loss: '#dc3c32', gold: '#deb052', carryInk: '#7da8c4',
      bgImage: 'none',
    },
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  arashi: {
    name: '04 · Deco Monolith: Warm Parchment (Light)',
    fontDisplay: F_DECO,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '0px',
    slant: '0px',
    bw: '1.5px',
    blur: 'none',
    dark: D2_ARASHI_LIGHT_PALETTE,
    light: D2_ARASHI_LIGHT_PALETTE,
  },
  arena: {
    name: '05 · Glassmorphic Arena (Translucent Frosted HUD)',
    fontDisplay: F_GROT,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '12px',
    slant: '0px',
    bw: '1px',
    blur: 'blur(12px)',
    dark: {
      page: '#070913', panel: 'rgba(19, 25, 43, 0.78)', panelHover: 'rgba(28, 37, 62, 0.88)', modal: '#111626',
      line: 'rgba(148, 163, 184, 0.2)', line2: 'rgba(148, 163, 184, 0.1)', outline: 'rgba(148, 163, 184, 0.35)',
      ink: '#f8fafc', strong: '#ffffff', onAccent: '#070913', onSlab: '#ffffff', ink2: '#e2e8f0', ink3: '#cbd5e1',
      muted: '#94a3b8', muted2: '#78879c', faint: '#566378',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#38bdf8', orangeInk: '#fb923c',
      lossSlab: '#be123c', win: '#34d399', loss: '#f43f5e',
      bgImage:
        'radial-gradient(circle at 12% 18%, rgba(56, 189, 248, 0.16), transparent 40%), radial-gradient(circle at 88% 15%, rgba(234, 88, 12, 0.14), transparent 40%)',
    },
    light: {
      page: '#eef2ff', panel: 'rgba(255, 255, 255, 0.82)', panelHover: 'rgba(255, 255, 255, 0.96)', modal: '#ffffff',
      line: 'rgba(99, 102, 241, 0.2)', line2: 'rgba(99, 102, 241, 0.1)', outline: 'rgba(99, 102, 241, 0.38)',
      ink: '#0f172a', strong: '#020617', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#1e293b', ink3: '#334155',
      muted: '#475569', muted2: '#64748b', faint: '#94a3b8',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#0369a1', orangeInk: '#c2410c',
      lossSlab: '#b91c1c', win: '#059669', loss: '#dc2626',
      bgImage:
        'radial-gradient(circle at 12% 18%, rgba(2, 132, 199, 0.12), transparent 40%), radial-gradient(circle at 88% 15%, rgba(234, 88, 12, 0.10), transparent 40%)',
    },
  },
  carbon: {
    name: '06 · Carbon Paddock (Motorsport Telemetry)',
    fontDisplay: F_SANS,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '5px',
    slant: '0px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#09090b', panel: '#121215', panelHover: '#18181c', modal: '#141418',
      line: '#222228', line2: '#18181d', outline: '#32323b',
      ink: '#fafafa', strong: '#ffffff', onAccent: '#09090b', onSlab: '#ffffff', ink2: '#e4e4e7', ink3: '#d4d4d8',
      muted: '#a1a1aa', muted2: '#82828c', faint: '#5a5a66',
      blueSlab: '#2563eb', orangeSlab: '#ea580c', blueInk: '#60a5fa', orangeInk: '#fb923c',
      lossSlab: '#be123c', win: '#10b981', loss: '#f43f5e', bgImage: 'none',
    },
    light: {
      page: '#fafafa', panel: '#ffffff', panelHover: '#f4f4f5', modal: '#ffffff',
      line: '#e4e4e7', line2: '#f4f4f5', outline: '#d4d4d8',
      ink: '#09090b', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#18181b', ink3: '#27272a',
      muted: '#52525b', muted2: '#71717a', faint: '#a1a1aa',
      blueSlab: '#1d4ed8', orangeSlab: '#ea580c', blueInk: '#1e40af', orangeInk: '#c2410c',
      lossSlab: '#9f1239', win: '#047857', loss: '#be123c', bgImage: 'none',
    },
  },
  synthwave: {
    name: '07 · Mannfield Night (Neon Violet & Laser)',
    fontDisplay: F_GROT,
    fontSans: F_GROT,
    fontMono: F_MONO,
    radius: '4px',
    slant: '14px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#0d061a', panel: '#160b2b', panelHover: '#21113e', modal: '#190d30',
      line: '#331c5c', line2: '#231242', outline: '#522d91',
      ink: '#f5f0ff', strong: '#ffffff', onAccent: '#0d061a', onSlab: '#ffffff', ink2: '#e4d4ff', ink3: '#cdb4ff',
      muted: '#a78bfa', muted2: '#8b6ce0', faint: '#6546b5',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#38bdf8', orangeInk: '#fb923c',
      lossSlab: '#be123c', win: '#00f59b', loss: '#ff3366',
      bgImage:
        'radial-gradient(circle at 50% 0%, rgba(234, 88, 12, 0.15), transparent 55%)',
    },
    light: {
      page: '#f8f5ff', panel: '#ffffff', panelHover: '#f3edff', modal: '#ffffff',
      line: '#dfd2fc', line2: '#efe8ff', outline: '#c4abfa',
      ink: '#1e0b3b', strong: '#0d031c', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#2e1459', ink3: '#43207a',
      muted: '#623ca1', muted2: '#7c57ba', faint: '#a688db',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#0369a1', orangeInk: '#c2410c',
      lossSlab: '#b91c1c', win: '#059669', loss: '#e11d48', bgImage: 'none',
    },
  },
  editorial: {
    name: '08 · Editorial Gazette (Fraunces Serif & Warm Press)',
    fontDisplay: F_SERIF,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '2px',
    slant: '0px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#141210', panel: '#1d1a17', panelHover: '#26221e', modal: '#211d1a',
      line: '#332d27', line2: '#26221d', outline: '#4d443b',
      ink: '#f5efe6', strong: '#ffffff', onAccent: '#141210', onSlab: '#ffffff', ink2: '#e6dec3', ink3: '#d4c8be',
      muted: '#a89b8c', muted2: '#8c7f70', faint: '#695e52',
      blueSlab: '#2563eb', orangeSlab: '#d97706', blueInk: '#60a5fa', orangeInk: '#fbbf24',
      lossSlab: '#b91c1c', win: '#34d399', loss: '#f87171', bgImage: 'none',
    },
    light: {
      page: '#f7f3eb', panel: '#fffdf9', panelHover: '#f2ece1', modal: '#fffdf9',
      line: '#dcd3c2', line2: '#eae3d5', outline: '#b8ab96',
      ink: '#1c1814', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#332c25', ink3: '#4a4036',
      muted: '#695c4e', muted2: '#827363', faint: '#a89b8c',
      blueSlab: '#1d4ed8', orangeSlab: '#c2410c', blueInk: '#1e40af', orangeInk: '#9a3412',
      lossSlab: '#991b1b', win: '#15803d', loss: '#b91c1c', bgImage: 'none',
    },
  },
  nordic: {
    name: '09 · Nordic Frost (Clean Arctic Slate & Ice)',
    fontDisplay: F_GROT,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '8px',
    slant: '0px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#1e222a', panel: '#262b36', panelHover: '#2e3440', modal: '#292f3b',
      line: '#3b4252', line2: '#2e3440', outline: '#4c566a',
      ink: '#eceff4', strong: '#ffffff', onAccent: '#1e222a', onSlab: '#ffffff', ink2: '#e5e9f0', ink3: '#d8dee9',
      muted: '#9aa5b8', muted2: '#7e8a9e', faint: '#616e88',
      blueSlab: '#2b6cb0', orangeSlab: '#c05621', blueInk: '#88c0d0', orangeInk: '#ed8936',
      lossSlab: '#9b2c2c', win: '#a3be8c', loss: '#bf616a', bgImage: 'none',
    },
    light: {
      page: '#eceff4', panel: '#ffffff', panelHover: '#e5e9f0', modal: '#ffffff',
      line: '#d8dee9', line2: '#e5e9f0', outline: '#b8c2d4',
      ink: '#2e3440', strong: '#191d24', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#3b4252', ink3: '#434c5e',
      muted: '#4c566a', muted2: '#647087', faint: '#8f9bb3',
      blueSlab: '#2b6cb0', orangeSlab: '#c05621', blueInk: '#2c5282', orangeInk: '#9c4221',
      lossSlab: '#9b2c2c', win: '#4f7a38', loss: '#b53845', bgImage: 'none',
    },
  },
  terminal: {
    name: '10 · Retro CRT Phosphor (Monospace Mainframe)',
    fontDisplay: F_MONO,
    fontSans: F_MONO,
    fontMono: F_MONO,
    radius: '0px',
    slant: '0px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#040a06', panel: '#08140c', panelHover: '#0d2114', modal: '#09170e',
      line: '#163822', line2: '#0f2617', outline: '#235c37',
      ink: '#d1fae5', strong: '#ecfdf5', onAccent: '#040a06', onSlab: '#ecfdf5', ink2: '#a7f3d0', ink3: '#6ee7b7',
      muted: '#34d399', muted2: '#10b981', faint: '#047857',
      blueSlab: '#0369a1', orangeSlab: '#c2410c', blueInk: '#38bdf8', orangeInk: '#fb923c',
      lossSlab: '#991b1b', win: '#4ade80', loss: '#fb7185',
      bgImage: 'repeating-linear-gradient(0deg, rgba(16, 185, 129, 0.03) 0px, rgba(16, 185, 129, 0.03) 1px, transparent 1px, transparent 3px)',
    },
    light: {
      page: '#e6f0ea', panel: '#f4faf6', panelHover: '#ffffff', modal: '#ffffff',
      line: '#a7cbb6', line2: '#cce3d5', outline: '#6ca384',
      ink: '#062413', strong: '#021208', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#0c381f', ink3: '#144d2c',
      muted: '#23663f', muted2: '#357d52', faint: '#6ca384',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#0369a1', orangeInk: '#c2410c',
      lossSlab: '#991b1b', win: '#047857', loss: '#be123c', bgImage: 'none',
    },
  },
  solar: {
    name: '11 · Utopia Daylight (Warm Sand, Blue & Terracotta)',
    fontDisplay: F_COND,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '4px',
    slant: '10px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#0f1416', panel: '#171f22', panelHover: '#1f2a2e', modal: '#192226',
      line: '#2a383d', line2: '#1e282b', outline: '#3d5159',
      ink: '#f4f1ea', strong: '#ffffff', onAccent: '#0f1416', onSlab: '#ffffff', ink2: '#e2ddd2', ink3: '#ccc5b8',
      muted: '#9ca8ab', muted2: '#7e8c90', faint: '#5c696d',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#38bdf8', orangeInk: '#fb923c',
      lossSlab: '#b91c1c', win: '#22c55e', loss: '#ef4444', bgImage: 'none',
    },
    light: {
      page: '#f5efe6', panel: '#fffdf9', panelHover: '#efe7da', modal: '#fffdf9',
      line: '#dcd2c0', line2: '#ebe3d5', outline: '#bfb29b',
      ink: '#182226', strong: '#0a1012', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#29383d', ink3: '#3d4f57',
      muted: '#596c73', muted2: '#73868c', faint: '#9ba9ad',
      blueSlab: '#0369a1', orangeSlab: '#ea580c', blueInk: '#075985', orangeInk: '#c2410c',
      lossSlab: '#991b1b', win: '#15803d', loss: '#dc2626', bgImage: 'none',
    },
  },
  industrial: {
    name: '12 · Cobalt Blueprint (CAD Engineering Grid)',
    fontDisplay: F_COND,
    fontSans: F_MONO,
    fontMono: F_MONO,
    radius: '0px',
    slant: '0px',
    bw: '2px',
    blur: 'none',
    dark: {
      page: '#091936', panel: '#0e234a', panelHover: '#142f61', modal: '#102752',
      line: '#234582', line2: '#173263', outline: '#3b67b5',
      ink: '#f0f6ff', strong: '#ffffff', onAccent: '#091936', onSlab: '#ffffff', ink2: '#d6e6ff', ink3: '#b3d1ff',
      muted: '#85b2f2', muted2: '#6394db', faint: '#4272b8',
      blueSlab: '#1d4ed8', orangeSlab: '#ea580c', blueInk: '#7dd3fc', orangeInk: '#fb923c',
      lossSlab: '#be123c', win: '#4ade80', loss: '#fb7185',
      bgImage:
        'linear-gradient(rgba(255,255,255,0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.04) 1px, transparent 1px)',
    },
    light: {
      page: '#e8f0fe', panel: '#ffffff', panelHover: '#f1f6ff', modal: '#ffffff',
      line: '#93b8f5', line2: '#c9dcFC', outline: '#4b83eb',
      ink: '#081b42', strong: '#030d24', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#102a63', ink3: '#1d3c7d',
      muted: '#34589e', muted2: '#4d71b8', faint: '#82a2de',
      blueSlab: '#0284c7', orangeSlab: '#ea580c', blueInk: '#0369a1', orangeInk: '#c2410c',
      lossSlab: '#b91c1c', win: '#059669', loss: '#e11d48',
      bgImage:
        'linear-gradient(rgba(8,27,66,0.05) 1px, transparent 1px), linear-gradient(90deg, rgba(8,27,66,0.05) 1px, transparent 1px)',
    },
  },
  oled: {
    name: '13 · Pure OLED Pitch (Zero-Glow True Black)',
    fontDisplay: F_COND,
    fontSans: F_SANS,
    fontMono: F_MONO,
    radius: '0px',
    slant: '16px',
    bw: '1px',
    blur: 'none',
    dark: {
      page: '#000000', panel: '#070709', panelHover: '#101014', modal: '#0a0a0d',
      line: '#1c1c24', line2: '#121217', outline: '#2e2e3b',
      ink: '#ffffff', strong: '#ffffff', onAccent: '#000000', onSlab: '#ffffff', ink2: '#e4e4eb', ink3: '#c6c6d1',
      muted: '#9898a8', muted2: '#7a7a8c', faint: '#555566',
      blueSlab: '#1d4ed8', orangeSlab: '#ea580c', blueInk: '#60a5fa', orangeInk: '#fb923c',
      lossSlab: '#b91c1c', win: '#22c55e', loss: '#ef4444', bgImage: 'none',
    },
    light: {
      page: '#ffffff', panel: '#f8fafc', panelHover: '#f1f5f9', modal: '#ffffff',
      line: '#cbd5e1', line2: '#e2e8f0', outline: '#94a3b8',
      ink: '#020617', strong: '#000000', onAccent: '#ffffff', onSlab: '#ffffff', ink2: '#0f172a', ink3: '#1e293b',
      muted: '#475569', muted2: '#64748b', faint: '#94a3b8',
      blueSlab: '#1d4ed8', orangeSlab: '#ea580c', blueInk: '#1e40af', orangeInk: '#c2410c',
      lossSlab: '#991b1b', win: '#15803d', loss: '#b91c1c', bgImage: 'none',
    },
  },
};

function safeStorageGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeStorageSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // ignore storage errors
  }
}

export interface DashboardContextValue {
  themeStyle: ThemeStyleKey;
  colorMode: ColorMode;
  setTheme: (style: ThemeStyleKey, mode: ColorMode) => void;
  fontScale: FontScale;
  setFontScale: (scale: FontScale) => void;
  statMode: StatMode;
  setStatMode: (mode: StatMode) => void;
  dateRange: DateRangePreset;
  setDateRange: (range: DateRangePreset) => void;
  customFrom: string;
  setCustomFrom: (date: string) => void;
  customTo: string;
  setCustomTo: (date: string) => void;
  showCustomCalendar: boolean;
  setShowCustomCalendar: (open: boolean | ((prev: boolean) => boolean)) => void;
  dateRangeQueryParams: { from?: string; to?: string };
  dateRangeLabel: string;
  dateRangeFactor: number;
  replayLimitPreview: ReplayLimitPreviewState;
  setReplayLimitPreview: (state: ReplayLimitPreviewState) => void;
  replayBannerDismissed: boolean;
  setReplayBannerDismissed: (dismissed: boolean) => void;
  liveModeOverride: 'auto' | 'match' | 'idle';
  setLiveModeOverride: (mode: 'auto' | 'match' | 'idle') => void;
  showLiveHistData: boolean;
  setShowLiveHistData: (show: boolean | ((prev: boolean) => boolean)) => void;
  pinnedPlayerIds: string[];
  togglePinPlayer: (playerId: string) => void;
  isPlayerPinned: (playerId: string) => boolean;
  isDevBuild: boolean;
  appVersion: string;
  previewMode: boolean;
  setPreviewMode: (enabled: boolean) => void;
}

const DEFAULT_CUSTOM_FROM = '2026-10-01';
const DEFAULT_CUSTOM_TO = '2026-10-18';

export function buildDateRangeQueryParams(
  preset: DateRangePreset,
  customFrom: string,
  customTo: string
): { from?: string; to?: string } {
  if (preset === 'all') {
    return {};
  }
  const now = new Date();
  if (preset === 'today') {
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    return { from: start.toISOString() };
  }
  if (preset === '7d') {
    const start = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
    return { from: start.toISOString() };
  }
  if (preset === '30d') {
    const start = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
    return { from: start.toISOString() };
  }
  if (preset === 'custom') {
    const res: { from?: string; to?: string } = {};
    if (customFrom) res.from = customFrom;
    if (customTo) res.to = customTo;
    return res;
  }
  return {};
}

export function getDateRangeLabel(
  preset: DateRangePreset,
  customFrom: string,
  customTo: string
): string {
  switch (preset) {
    case 'today':
      return 'TODAY';
    case '7d':
      return 'LAST 7D';
    case '30d':
      return 'LAST 30D';
    case 'custom':
      return customFrom && customTo ? `${customFrom} → ${customTo}` : 'CUSTOM RANGE';
    default:
      return 'ALL TIME';
  }
}

export function getDateRangeFactor(
  preset: DateRangePreset,
  customFrom: string,
  customTo: string
): number {
  if (preset === 'today') return 0.35;
  if (preset === '7d') return 0.65;
  if (preset === '30d') return 0.85;
  if (preset === 'custom' && customFrom && customTo) {
    const d1 = new Date(customFrom);
    const d2 = new Date(customTo);
    const days = Math.max(1, Math.round((d2.getTime() - d1.getTime()) / (1000 * 60 * 60 * 24)) + 1);
    return Math.min(1.0, Math.max(0.25, days / 25));
  }
  return 1.0;
}

function applyThemeToDOM(styleKey: ThemeStyleKey, mode: ColorMode) {
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

function applyFontScaleToDOM(scale: FontScale) {
  if (typeof document === 'undefined') return;
  document.documentElement.style.fontSize = scale === 'xl' ? '18.4px' : '16px';
}

const DEFAULT_CONTEXT: DashboardContextValue = {
  themeStyle: 'broadcast',
  colorMode: 'dark',
  setTheme: () => {},
  fontScale: 'normal',
  setFontScale: () => {},
  statMode: 'avg',
  setStatMode: () => {},
  dateRange: 'all',
  setDateRange: () => {},
  customFrom: DEFAULT_CUSTOM_FROM,
  setCustomFrom: () => {},
  customTo: DEFAULT_CUSTOM_TO,
  setCustomTo: () => {},
  showCustomCalendar: false,
  setShowCustomCalendar: () => {},
  dateRangeQueryParams: {},
  dateRangeLabel: 'ALL TIME',
  dateRangeFactor: 1.0,
  replayLimitPreview: 'auto',
  setReplayLimitPreview: () => {},
  replayBannerDismissed: false,
  setReplayBannerDismissed: () => {},
  liveModeOverride: 'auto',
  setLiveModeOverride: () => {},
  showLiveHistData: true,
  setShowLiveHistData: () => {},
  pinnedPlayerIds: [],
  togglePinPlayer: () => {},
  isPlayerPinned: () => false,
  isDevBuild: false,
  appVersion: '2.0.0',
  previewMode: false,
  setPreviewMode: () => {},
};

const DashboardContext = createContext<DashboardContextValue>(DEFAULT_CONTEXT);

export const DashboardProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const isDevBuild = useMemo(() => isPreviewBuildAllowed(), []);
  const appVersion = useMemo(() => getAppVersion(), []);

  const [previewMode, setPreviewModeState] = useState<boolean>(() => isDemoMode());

  const [themeStyle, setThemeStyleState] = useState<ThemeStyleKey>(() => {
    const saved = safeStorageGet('rlsync_theme_style') as ThemeStyleKey | null;
    return saved && THEME_STYLES[saved] ? saved : 'broadcast';
  });

  const [colorMode, setColorModeState] = useState<ColorMode>(() => {
    const saved = safeStorageGet('rlsync_color_mode');
    return saved === 'light' ? 'light' : 'dark';
  });

  const [fontScale, setFontScaleState] = useState<FontScale>(() => {
    const saved = safeStorageGet('rlsync_font_scale');
    return saved === 'xl' ? 'xl' : 'normal';
  });

  const [statMode, setStatMode] = useState<StatMode>('avg');
  const [dateRange, setDateRangeState] = useState<DateRangePreset>('all');
  const [customFrom, setCustomFromState] = useState<string>(DEFAULT_CUSTOM_FROM);
  const [customTo, setCustomToState] = useState<string>(DEFAULT_CUSTOM_TO);
  const [showCustomCalendar, setShowCustomCalendar] = useState<boolean>(false);

  const [replayLimitPreview, setReplayLimitPreviewState] = useState<ReplayLimitPreviewState>('auto');
  const [replayBannerDismissed, setReplayBannerDismissedState] = useState<boolean>(() => {
    return safeStorageGet('rlsync_replay_banner_dismissed') === 'true';
  });
  const [liveModeOverride, setLiveModeOverride] = useState<'auto' | 'match' | 'idle'>('auto');
  const [showLiveHistData, setShowLiveHistData] = useState<boolean>(true);

  const [pinnedPlayerIds, setPinnedPlayerIds] = useState<string[]>(() => {
    const saved = safeStorageGet('rlsync_pinned_players');
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        if (Array.isArray(parsed)) {
          return parsed.filter((x): x is string => typeof x === 'string').slice(0, 3);
        }
      } catch {
        // ignore invalid json
      }
    }
    return [];
  });

  useEffect(() => {
    applyThemeToDOM(themeStyle, colorMode);
  }, [themeStyle, colorMode]);

  useEffect(() => {
    applyFontScaleToDOM(fontScale);
  }, [fontScale]);

  const setTheme = useCallback((style: ThemeStyleKey, mode: ColorMode) => {
    const validStyle = THEME_STYLES[style] ? style : 'broadcast';
    const validMode: ColorMode = mode === 'light' ? 'light' : 'dark';
    setThemeStyleState(validStyle);
    setColorModeState(validMode);
    safeStorageSet('rlsync_theme_style', validStyle);
    safeStorageSet('rlsync_color_mode', validMode);
    applyThemeToDOM(validStyle, validMode);
  }, []);

  const setFontScale = useCallback((scale: FontScale) => {
    const validScale: FontScale = scale === 'xl' ? 'xl' : 'normal';
    setFontScaleState(validScale);
    safeStorageSet('rlsync_font_scale', validScale);
    applyFontScaleToDOM(validScale);
  }, []);

  const setDateRange = useCallback((range: DateRangePreset) => {
    setDateRangeState(range);
    if (range === 'custom') {
      setShowCustomCalendar(true);
    } else {
      setShowCustomCalendar(false);
    }
  }, []);

  const setCustomFrom = useCallback(
    (date: string) => {
      let nextFrom = date;
      let nextTo = customTo;
      if (nextFrom && nextTo && nextFrom > nextTo) {
        nextTo = nextFrom;
        setCustomToState(nextTo);
      }
      setCustomFromState(nextFrom);
      setDateRangeState('custom');
    },
    [customTo]
  );

  const setCustomTo = useCallback(
    (date: string) => {
      let nextTo = date;
      let nextFrom = customFrom;
      if (nextFrom && nextTo && nextFrom > nextTo) {
        nextFrom = nextTo;
        setCustomFromState(nextFrom);
      }
      setCustomToState(nextTo);
      setDateRangeState('custom');
    },
    [customFrom]
  );

  const setReplayBannerDismissed = useCallback((dismissed: boolean) => {
    setReplayBannerDismissedState(dismissed);
    safeStorageSet('rlsync_replay_banner_dismissed', dismissed ? 'true' : 'false');
  }, []);

  const setReplayLimitPreview = useCallback(
    (state: ReplayLimitPreviewState) => {
      setReplayLimitPreviewState(state);
      if (state !== 'auto') {
        setReplayBannerDismissed(false);
      }
    },
    [setReplayBannerDismissed]
  );

  const setPreviewMode = useCallback((enabled: boolean) => {
    const next = Boolean(enabled && isPreviewBuildAllowed());
    setPreviewModeEnabled(next);
    setPreviewModeState(next);
    if (!next) {
      setLiveModeOverride('auto');
      setReplayLimitPreviewState('auto');
    }
  }, []);

  const togglePinPlayer = useCallback((playerId: string) => {
    setPinnedPlayerIds((prev) => {
      const idx = prev.indexOf(playerId);
      let next: string[];
      if (idx >= 0) {
        next = prev.filter((id) => id !== playerId);
      } else if (prev.length < 3) {
        next = [...prev, playerId];
      } else {
        next = [...prev.slice(1), playerId];
      }
      safeStorageSet('rlsync_pinned_players', JSON.stringify(next));
      return next;
    });
  }, []);

  const isPlayerPinned = useCallback(
    (playerId: string) => pinnedPlayerIds.includes(playerId),
    [pinnedPlayerIds]
  );

  const dateRangeQueryParams = useMemo(
    () => buildDateRangeQueryParams(dateRange, customFrom, customTo),
    [dateRange, customFrom, customTo]
  );

  const dateRangeLabel = useMemo(
    () => getDateRangeLabel(dateRange, customFrom, customTo),
    [dateRange, customFrom, customTo]
  );

  const dateRangeFactor = useMemo(
    () => getDateRangeFactor(dateRange, customFrom, customTo),
    [dateRange, customFrom, customTo]
  );

  const value = useMemo<DashboardContextValue>(
    () => ({
      themeStyle,
      colorMode,
      setTheme,
      fontScale,
      setFontScale,
      statMode,
      setStatMode,
      dateRange,
      setDateRange,
      customFrom,
      setCustomFrom,
      customTo,
      setCustomTo,
      showCustomCalendar,
      setShowCustomCalendar,
      dateRangeQueryParams,
      dateRangeLabel,
      dateRangeFactor,
      replayLimitPreview,
      setReplayLimitPreview,
      replayBannerDismissed,
      setReplayBannerDismissed,
      liveModeOverride,
      setLiveModeOverride,
      showLiveHistData,
      setShowLiveHistData,
      pinnedPlayerIds,
      togglePinPlayer,
      isPlayerPinned,
      isDevBuild,
      appVersion,
      previewMode,
      setPreviewMode,
    }),
    [
      themeStyle,
      colorMode,
      setTheme,
      fontScale,
      fontScale,
      setFontScale,
      statMode,
      dateRange,
      setDateRange,
      customFrom,
      setCustomFrom,
      customTo,
      setCustomTo,
      showCustomCalendar,
      dateRangeQueryParams,
      dateRangeLabel,
      dateRangeFactor,
      replayLimitPreview,
      setReplayLimitPreview,
      replayBannerDismissed,
      setReplayBannerDismissed,
      liveModeOverride,
      showLiveHistData,
      pinnedPlayerIds,
      togglePinPlayer,
      isPlayerPinned,
      isDevBuild,
      appVersion,
      previewMode,
      setPreviewMode,
    ]
  );

  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>;
};

export function useDashboard(): DashboardContextValue {
  return useContext(DashboardContext);
}
