import React from 'react';
import {
  ColorMode,
  FontScale,
  PlatformDisplayMode,
  THEME_STYLES,
  ThemeStyleKey,
} from '../../context/DashboardContext';
import { PlatformIcon } from '../common/PlatformBadge';
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
  { id: 'classicOled', label: '06 · Classic Broadcast OLED (Pure Black)', shortName: '06 · CLASSIC OLED', core: true },
  { id: 'arena', label: '07 · Glassmorphic Arena (Frosted HUD)', shortName: '07 · ARENA' },
  { id: 'carbon', label: '08 · Carbon Paddock (Motorsport Telemetry)', shortName: '08 · CARBON' },
  { id: 'synthwave', label: '09 · Mannfield Night (Neon Violet)', shortName: '09 · MANNFIELD' },
  { id: 'editorial', label: '10 · Editorial Gazette (Fraunces Serif)', shortName: '10 · GAZETTE' },
  { id: 'nordic', label: '11 · Nordic Frost (Arctic Slate & Ice)', shortName: '11 · NORDIC' },
  { id: 'terminal', label: '12 · Retro CRT Phosphor (Monospace)', shortName: '12 · RETRO CRT' },
  { id: 'solar', label: '13 · Utopia Daylight (Warm Sand & Blue)', shortName: '13 · UTOPIA' },
  { id: 'industrial', label: '14 · Cobalt Blueprint (CAD Grid)', shortName: '14 · BLUEPRINT' },
  { id: 'oled', label: '15 · Pure OLED Pitch (True Zero Black)', shortName: '15 · PURE OLED' },
];

interface ThemeAppearanceSectionProps {
  themeStyle: ThemeStyleKey;
  colorMode: ColorMode;
  fontScale: FontScale;
  platformDisplay: PlatformDisplayMode;
  onSetTheme: (style: ThemeStyleKey, mode: ColorMode) => void;
  onSetFontScale: (scale: FontScale) => void;
  onSetPlatformDisplay: (mode: PlatformDisplayMode) => void;
}

export const ThemeAppearanceSection: React.FC<ThemeAppearanceSectionProps> = ({
  themeStyle,
  colorMode,
  fontScale,
  platformDisplay,
  onSetTheme,
  onSetFontScale,
  onSetPlatformDisplay,
}) => {
  const applyThemeSelection = (next: ThemeStyleKey) => {
    if (!THEME_STYLES[next]) return;
    if (next === 'arashi') {
      onSetTheme(next, 'light');
    } else if (
      next === 'broadcast' ||
      next === 'tactical' ||
      next === 'swiss' ||
      next === 'classicOled'
    ) {
      onSetTheme(next, 'dark');
    } else {
      onSetTheme(next, colorMode);
    }
  };

  const coreThemes = THEME_OPTIONS.filter((t) => t.core);

  return (
    <section className="flex flex-col gap-3.5">
      <SectionLabel>01 · DESIGN SYSTEM &amp; APPEARANCE</SectionLabel>

      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-2">
        {coreThemes.map((opt) => {
          const def = THEME_STYLES[opt.id];
          const pal = opt.id === 'arashi' ? def.light : def.dark;
          const active = themeStyle === opt.id;
          return (
            <button
              key={opt.id}
              type="button"
              onClick={() => applyThemeSelection(opt.id)}
              className={`p-2.5 text-left t-bg-panel border transition-colors cursor-pointer flex flex-col justify-between gap-2 t-radius min-w-0 ${
                active
                  ? 'border-blue-ink ring-1 ring-blue-ink t-bg-panel-hover'
                  : 'border-line hover:border-outline'
              }`}
            >
              <div className="flex items-start justify-between gap-1 min-w-0">
                <span className="t-font-display font-bold text-xs uppercase tracking-wider t-text-strong leading-tight break-words">
                  {opt.shortName}
                </span>
                {active && (
                  <span className="text-[10px] t-font-mono font-extrabold t-text-blue shrink-0">●</span>
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

      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1">
        <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
          <label
            htmlFor="theme-select"
            className="t-font-display font-bold text-xs uppercase tracking-wider text-muted"
          >
            ALL DESIGN EXPRESSIONS ({THEME_OPTIONS.length})
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

        <div className="surface-plaque border border-line p-3 flex flex-col justify-between gap-2 t-radius">
          <div className="flex items-center justify-between gap-2">
            <span className="t-font-display font-bold text-xs uppercase tracking-wider text-muted">
              PLAYER PLATFORM DISPLAY
            </span>
            <span className="inline-flex items-center gap-1.5 text-muted" aria-hidden="true">
              <PlatformIcon code="steam" className="w-3.5 h-3.5" />
              <PlatformIcon code="epic" className="w-3.5 h-3.5" />
              <PlatformIcon code="psn" className="w-3.5 h-3.5" />
              <PlatformIcon code="xbox" className="w-3.5 h-3.5" />
              <PlatformIcon code="switch" className="w-3.5 h-3.5" />
            </span>
          </div>
          <div
            className="grid grid-cols-2 t-border t-radius overflow-hidden"
            role="group"
            aria-label="Player platform display"
          >
            <button
              type="button"
              id="btn-platform-icon"
              data-testid="platform-display-icon"
              aria-pressed={platformDisplay === 'icon'}
              onClick={() => onSetPlatformDisplay('icon')}
              className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider cursor-pointer ${
                platformDisplay === 'icon'
                  ? 't-bg-blue-ink t-text-on-accent'
                  : 't-bg-page t-text-muted hover:opacity-80'
              }`}
              title="Show compact platform icons (Steam, Epic, PlayStation, Xbox, Switch)"
            >
              ICONS
            </button>
            <button
              type="button"
              id="btn-platform-name"
              data-testid="platform-display-name"
              aria-pressed={platformDisplay === 'name'}
              onClick={() => onSetPlatformDisplay('name')}
              className={`py-1.5 t-font-display font-bold text-xs uppercase tracking-wider border-l t-border cursor-pointer ${
                platformDisplay === 'name'
                  ? 't-bg-blue-ink t-text-on-accent'
                  : 't-bg-page t-text-muted hover:opacity-80'
              }`}
              title="Show platform text names (STEAM, EPIC, PLAYSTATION, XBOX, SWITCH)"
            >
              NAME
            </button>
          </div>
        </div>
      </div>
    </section>
  );
};
