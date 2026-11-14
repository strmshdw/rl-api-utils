import React from 'react';
import {
  ColorMode,
  FontScale,
  THEME_STYLES,
  ThemeStyleKey,
} from '../../context/DashboardContext';
import { SectionLabel } from '../common/ui';

export const THEME_OPTIONS: ReadonlyArray<{
  id: ThemeStyleKey;
  label: string;
  shortName: string;
  core?: boolean;
}> = [
  { id: 'broadcast', label: '01 · Deco Monolith: Woodblock (Default)', shortName: '01 · WOODBLOCK', core: true },
  { id: 'tactical', label: '02 · Deco Monolith: Lacquer & Brass', shortName: '02 · LACQUER', core: true },
  { id: 'swiss', label: '03 · Deco Monolith: Carved Inkstone', shortName: '03 · INKSTONE', core: true },
  { id: 'arashi', label: '04 · Deco Monolith: Warm Parchment (Light)', shortName: '04 · PARCHMENT', core: true },
  { id: 'classic', label: '05 · Classic Broadcast (Original RL/SYNC)', shortName: '05 · CLASSIC', core: true },
  { id: 'arena', label: '06 · Glassmorphic Arena (Frosted HUD)', shortName: '06 · ARENA' },
  { id: 'carbon', label: '07 · Carbon Paddock (Motorsport Telemetry)', shortName: '07 · CARBON' },
  { id: 'synthwave', label: '08 · Mannfield Night (Neon Violet)', shortName: '08 · MANNFIELD' },
  { id: 'editorial', label: '09 · Editorial Gazette (Fraunces Serif)', shortName: '09 · GAZETTE' },
  { id: 'nordic', label: '10 · Nordic Frost (Arctic Slate & Ice)', shortName: '10 · NORDIC' },
  { id: 'terminal', label: '11 · Retro CRT Phosphor (Monospace)', shortName: '11 · RETRO CRT' },
  { id: 'solar', label: '12 · Utopia Daylight (Warm Sand & Blue)', shortName: '12 · UTOPIA' },
  { id: 'industrial', label: '13 · Cobalt Blueprint (CAD Grid)', shortName: '13 · BLUEPRINT' },
  { id: 'oled', label: '14 · Pure OLED Pitch (True Zero Black)', shortName: '14 · PURE OLED' },
];

interface ThemeAppearanceSectionProps {
  themeStyle: ThemeStyleKey;
  colorMode: ColorMode;
  fontScale: FontScale;
  onSetTheme: (style: ThemeStyleKey, mode: ColorMode) => void;
  onSetFontScale: (scale: FontScale) => void;
}

export const ThemeAppearanceSection: React.FC<ThemeAppearanceSectionProps> = ({
  themeStyle,
  colorMode,
  fontScale,
  onSetTheme,
  onSetFontScale,
}) => {
  const applyThemeSelection = (next: ThemeStyleKey) => {
    if (!THEME_STYLES[next]) return;
    if (next === 'arashi') {
      onSetTheme(next, 'light');
    } else if (next === 'broadcast' || next === 'tactical' || next === 'swiss') {
      onSetTheme(next, 'dark');
    } else {
      onSetTheme(next, colorMode);
    }
  };

  const coreThemes = THEME_OPTIONS.filter((t) => t.core);

  return (
    <section className="flex flex-col gap-3.5">
      <SectionLabel>01 · DESIGN SYSTEM &amp; APPEARANCE</SectionLabel>

      <div className="grid grid-cols-1 sm:grid-cols-5 gap-2">
        {coreThemes.map((opt) => {
          const def = THEME_STYLES[opt.id];
          const pal = opt.id === 'arashi' ? def.light : def.dark;
          const active = themeStyle === opt.id;
          return (
            <button
              key={opt.id}
              type="button"
              onClick={() => applyThemeSelection(opt.id)}
              className={`p-2.5 text-left surface-plaque border transition-colors cursor-pointer flex flex-col gap-2 t-radius ${
                active
                  ? 'border-blue-ink ring-1 ring-blue-ink t-bg-panel-hover'
                  : 'border-line hover:border-outline'
              }`}
            >
              <div className="flex items-center justify-between gap-1">
                <span className="t-font-display font-bold text-xs uppercase tracking-wider t-text-strong truncate">
                  {opt.shortName}
                </span>
                {active && (
                  <span className="text-[10px] t-font-mono font-extrabold t-text-blue">●</span>
                )}
              </div>
              <div className="flex items-center gap-1 h-3 w-full overflow-hidden border border-line">
                <span className="flex-1 h-full" style={{ backgroundColor: pal.page }} title="Surface" />
                <span className="flex-1 h-full" style={{ backgroundColor: pal.blueSlab }} title="Blue Team" />
                <span className="flex-1 h-full" style={{ backgroundColor: pal.orangeSlab }} title="Orange Team" />
              </div>
            </button>
          );
        })}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-1">
        <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
          <label
            htmlFor="theme-select"
            className="t-font-display font-bold text-xs uppercase tracking-wider text-muted"
          >
            ALL DESIGN EXPRESSIONS (14)
          </label>
          <select
            id="theme-select"
            value={themeStyle}
            onChange={(e) => applyThemeSelection(e.target.value as ThemeStyleKey)}
            className="t-bg-page t-text-strong border border-outline px-2.5 py-1.5 text-xs font-semibold t-radius focus:outline-none cursor-pointer w-full"
          >
            {THEME_OPTIONS.map((opt) => (
              <option key={opt.id} value={opt.id}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
          <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
            COLOR POLARITY
          </span>
          <div className="grid grid-cols-2 t-border t-radius overflow-hidden">
            <button
              type="button"
              id="btn-color-dark"
              onClick={() => onSetTheme(themeStyle === 'arashi' ? 'broadcast' : themeStyle, 'dark')}
              className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                colorMode === 'dark'
                  ? 't-bg-blue-ink t-text-on-accent'
                  : 't-bg-page t-text-muted hover:opacity-80'
              }`}
            >
              DARK MODE
            </button>
            <button
              type="button"
              id="btn-color-light"
              onClick={() => onSetTheme(themeStyle, 'light')}
              className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                colorMode === 'light'
                  ? 't-bg-blue-ink t-text-on-accent'
                  : 't-bg-page t-text-muted hover:opacity-80'
              }`}
            >
              LIGHT MODE
            </button>
          </div>
        </div>

        <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
          <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
            TYPOGRAPHY SCALE
          </span>
          <div className="grid grid-cols-2 t-border t-radius overflow-hidden">
            <button
              type="button"
              id="btn-scale-normal"
              onClick={() => onSetFontScale('normal')}
              className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                fontScale === 'normal'
                  ? 't-bg-blue-ink t-text-on-accent'
                  : 't-bg-page t-text-muted hover:opacity-80'
              }`}
              title="Default Large Readable Scale (16px base)"
            >
              TEXT: LARGE (DEFAULT)
            </button>
            <button
              type="button"
              id="btn-scale-xl"
              onClick={() => onSetFontScale('xl')}
              className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                fontScale === 'xl'
                  ? 't-bg-blue-ink t-text-on-accent'
                  : 't-bg-page t-text-muted hover:opacity-80'
              }`}
              title="Extra-Large Scale (+15% for 1440p/4K monitors)"
            >
              TEXT: XL (+15%)
            </button>
          </div>
        </div>
      </div>
    </section>
  );
};
