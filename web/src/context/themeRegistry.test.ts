// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest';
import {
  THEME_STYLES,
  ThemeStyleKey,
  ColorMode,
  applyThemeToDOM,
} from './themeRegistry';
import { STAT_COLOR_PALETTES, STAT_CSS_VAR, StatColorKey } from '../utils/statColors';

interface RGB {
  r: number;
  g: number;
  b: number;
  a: number;
}

function parseColor(raw: string): RGB {
  const s = raw.trim();
  if (s.startsWith('#')) {
    const hex = s.slice(1);
    if (hex.length === 3) {
      return {
        r: parseInt(hex[0] + hex[0], 16),
        g: parseInt(hex[1] + hex[1], 16),
        b: parseInt(hex[2] + hex[2], 16),
        a: 1,
      };
    }
    return {
      r: parseInt(hex.slice(0, 2), 16),
      g: parseInt(hex.slice(2, 4), 16),
      b: parseInt(hex.slice(4, 6), 16),
      a: 1,
    };
  }
  const match = s.match(/rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)(?:\s*,\s*([\d.]+))?\s*\)/i);
  if (match) {
    return {
      r: Number(match[1]),
      g: Number(match[2]),
      b: Number(match[3]),
      a: match[4] !== undefined ? Number(match[4]) : 1,
    };
  }
  throw new Error(`Unsupported color format: ${raw}`);
}

function compositeOver(fg: RGB, bg: RGB): RGB {
  if (fg.a >= 1) return fg;
  const a = fg.a;
  return {
    r: fg.r * a + bg.r * (1 - a),
    g: fg.g * a + bg.g * (1 - a),
    b: fg.b * a + bg.b * (1 - a),
    a: 1,
  };
}

function relativeLuminance(c: RGB): number {
  const toLinear = (v: number) => {
    const n = v / 255;
    return n <= 0.03928 ? n / 12.92 : Math.pow((n + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * toLinear(c.r) + 0.7152 * toLinear(c.g) + 0.0722 * toLinear(c.b);
}

function contrastRatio(fgRaw: string, bgRaw: string, pageRaw?: string): number {
  const baseBg = pageRaw ? parseColor(pageRaw) : { r: 0, g: 0, b: 0, a: 1 };
  const bg = compositeOver(parseColor(bgRaw), baseBg);
  const fg = compositeOver(parseColor(fgRaw), bg);
  const l1 = relativeLuminance(fg);
  const l2 = relativeLuminance(bg);
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}

function rgbToLab(c: RGB): [number, number, number] {
  const toLin = (v: number) => {
    const n = v / 255;
    return n <= 0.04045 ? n / 12.92 : Math.pow((n + 0.055) / 1.055, 2.4);
  };
  const r = toLin(c.r);
  const g = toLin(c.g);
  const b = toLin(c.b);
  const f = (v: number) => (v > 0.008856 ? Math.cbrt(v) : 7.787 * v + 16 / 116);
  const x = f((r * 0.4124 + g * 0.3576 + b * 0.1805) / 0.95047);
  const y = f((r * 0.2126 + g * 0.7152 + b * 0.0722) / 1.0);
  const z = f((r * 0.0193 + g * 0.1192 + b * 0.9505) / 1.08883);
  return [116 * y - 16, 500 * (x - y), 200 * (y - z)];
}

function deltaE(hexA: string, hexB: string): number {
  const a = rgbToLab(parseColor(hexA));
  const b = rgbToLab(parseColor(hexB));
  return Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
}

const ALL_STYLES = Object.keys(THEME_STYLES) as ThemeStyleKey[];
const ALL_MODES: ColorMode[] = ['dark', 'light'];

describe('Theme Registry WCAG 2.1 Contrast & Stat Label Color Audit', () => {
  it('enforces >= 4.5:1 AA contrast for all readable text tokens across all 14 themes (dark + light)', () => {
    const failures: string[] = [];

    for (const styleKey of ALL_STYLES) {
      for (const mode of ALL_MODES) {
        const p = THEME_STYLES[styleKey][mode];
        const effectiveMode: ColorMode = styleKey === 'arashi' ? 'light' : mode;
        const surfaces: Array<[string, string]> = [
          ['page', p.page],
          ['panel', p.panel],
          ['plaque', p.plaque || p.page],
          ['panelHover', p.panelHover],
        ];
        const textTokens: Array<[string, string]> = [
          ['ink', p.ink],
          ['strong', p.strong],
          ['ink2', p.ink2],
          ['ink3', p.ink3],
          ['muted', p.muted],
          ['muted2', p.muted2],
          ['blueInk', p.blueInk],
          ['orangeInk', p.orangeInk],
          ['win', p.win],
          ['loss', p.loss],
          ['gold', p.gold || (effectiveMode === 'dark' ? '#f0c45c' : '#7a4a00')],
          ['carryInk', p.carryInk || (effectiveMode === 'dark' ? '#84bfe0' : '#164a6b')],
        ];

        for (const [tokenName, fg] of textTokens) {
          for (const [surfName, bg] of surfaces) {
            const ratio = contrastRatio(fg, bg, p.page);
            if (ratio < 4.5) {
              failures.push(
                `${styleKey}.${mode} ${tokenName}(${fg}) on ${surfName}(${bg}) = ${ratio.toFixed(2)}:1 (< 4.5:1)`,
              );
            }
          }
        }

        // Faint (zeros, placeholders, dividers): >= 3.0:1 on primary surfaces
        for (const [surfName, bg] of surfaces.slice(0, 3)) {
          const ratio = contrastRatio(p.faint, bg, p.page);
          if (ratio < 3.0) {
            failures.push(
              `${styleKey}.${mode} faint(${p.faint}) on ${surfName}(${bg}) = ${ratio.toFixed(2)}:1 (< 3.0:1)`,
            );
          }
        }

        // Slab contrast (onSlab on blueSlab, orangeSlab, lossSlab)
        const onSlab = p.onSlab || '#ffffff';
        const slabs: Array<[string, string]> = [
          ['blueSlab', p.blueSlab],
          ['orangeSlab', p.orangeSlab],
          ['lossSlab', p.lossSlab || p.loss],
        ];
        for (const [slabName, bg] of slabs) {
          const ratio = contrastRatio(onSlab, bg, p.page);
          if (ratio < 4.5) {
            failures.push(
              `${styleKey}.${mode} onSlab(${onSlab}) on ${slabName}(${bg}) = ${ratio.toFixed(2)}:1 (< 4.5:1)`,
            );
          }
        }

        // onAccent text on active buttons (ink, strong, blueInk)
        const accents: Array<[string, string]> = [
          ['ink', p.ink],
          ['strong', p.strong],
          ['blueInk', p.blueInk],
        ];
        for (const [accName, bg] of accents) {
          const ratio = contrastRatio(p.onAccent, bg, p.page);
          if (ratio < 4.5) {
            failures.push(
              `${styleKey}.${mode} onAccent(${p.onAccent}) on ${accName}(${bg}) = ${ratio.toFixed(2)}:1 (< 4.5:1)`,
            );
          }
        }
      }
    }

    expect(failures).toEqual([]);
  });

  it('enforces >= 4.7:1 high contrast and strong CIELAB perceptual separation for every hypercolor stat metric across all theme surfaces', () => {
    const failures: string[] = [];

    for (const styleKey of ALL_STYLES) {
      for (const mode of ALL_MODES) {
        const p = THEME_STYLES[styleKey][mode];
        const effectiveMode: ColorMode = styleKey === 'arashi' ? 'light' : mode;
        const statPalette = STAT_COLOR_PALETTES[effectiveMode];
        const surfaces: Array<[string, string]> = [
          ['page', p.page],
          ['panel', p.panel],
          ['plaque', p.plaque || p.page],
          ['panelHover', p.panelHover],
        ];

        for (const [statKey, fg] of Object.entries(statPalette) as Array<[StatColorKey, string]>) {
          for (const [surfName, bg] of surfaces) {
            const ratio = contrastRatio(fg, bg, p.page);
            if (ratio < 4.7) {
              failures.push(
                `${styleKey}.${mode} stat.${statKey}(${fg}) on ${surfName}(${bg}) = ${ratio.toFixed(2)}:1 (< 4.7:1)`,
              );
            }
          }
        }
      }
    }

    const keys = Object.keys(STAT_COLOR_PALETTES.dark) as StatColorKey[];
    const boxKeys: StatColorKey[] = ['score', 'goals', 'assists', 'saves', 'shots', 'shotpct', 'demos', 'demod'];
    for (let i = 0; i < keys.length; i++) {
      for (let j = i + 1; j < keys.length; j++) {
        const a = keys[i];
        const b = keys[j];
        const darkDE = deltaE(STAT_COLOR_PALETTES.dark[a], STAT_COLOR_PALETTES.dark[b]);
        if (darkDE < 25) {
          failures.push(`dark hypercolor separation too low: ${a} vs ${b} dE=${darkDE.toFixed(1)} (< 25)`);
        }
        const lightDE = deltaE(STAT_COLOR_PALETTES.light[a], STAT_COLOR_PALETTES.light[b]);
        if (lightDE < 17) {
          failures.push(`light hypercolor separation too low: ${a} vs ${b} dE=${lightDE.toFixed(1)} (< 17)`);
        }
      }
    }
    for (let i = 0; i < boxKeys.length; i++) {
      for (let j = i + 1; j < boxKeys.length; j++) {
        const a = boxKeys[i];
        const b = boxKeys[j];
        const boxDE = deltaE(STAT_COLOR_PALETTES.dark[a], STAT_COLOR_PALETTES.dark[b]);
        if (boxDE < 30) {
          failures.push(`dark box-score hypercolor separation too low: ${a} vs ${b} dE=${boxDE.toFixed(1)} (< 30)`);
        }
      }
    }

    // Adjacent Saves vs Shots columns must remain wildly distinct in both dark and light modes.
    expect(deltaE(STAT_COLOR_PALETTES.dark.saves, STAT_COLOR_PALETTES.dark.shots)).toBeGreaterThanOrEqual(150);
    expect(deltaE(STAT_COLOR_PALETTES.light.saves, STAT_COLOR_PALETTES.light.shots)).toBeGreaterThanOrEqual(80);

    expect(failures).toEqual([]);
  });

  it('assigns unique colors to all core box-score stat attributes and applies CSS variables via applyThemeToDOM', () => {
    const coreKeys: StatColorKey[] = [
      'score',
      'goals',
      'assists',
      'saves',
      'shots',
      'shotpct',
      'demos',
      'demod',
      'mmr',
      'rank',
      'bpm',
      'boost',
      'speed',
      'games',
      'h2h',
    ];
    for (const mode of ALL_MODES) {
      const uniqueHexes = new Set(coreKeys.map((k) => STAT_COLOR_PALETTES[mode][k].toLowerCase()));
      expect(uniqueHexes.size).toBe(coreKeys.length);
    }

    applyThemeToDOM('broadcast', 'dark');
    expect(document.documentElement.style.getPropertyValue(STAT_CSS_VAR.score)).toBe(
      STAT_COLOR_PALETTES.dark.score,
    );
    expect(document.documentElement.style.getPropertyValue(STAT_CSS_VAR.goals)).toBe(
      STAT_COLOR_PALETTES.dark.goals,
    );

    // Arashi stays light even when colorMode='dark'
    applyThemeToDOM('arashi', 'dark');
    expect(document.documentElement.style.getPropertyValue(STAT_CSS_VAR.score)).toBe(
      STAT_COLOR_PALETTES.light.score,
    );
    expect(document.documentElement.style.colorScheme).toBe('light');
  });
});
