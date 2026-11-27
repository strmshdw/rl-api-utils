import React, { useEffect, useRef, useState } from 'react';
import type {
  ArenaThemeId,
  GlassPresetId,
  RLViewerProfile,
} from '@rl-replay/viewer-sdk';
import type { ReplaySource } from './replayApi';

export type QaShowroomPreset =
  | ''
  | 'octane'
  | 'cars'
  | 'ball'
  | 'pads'
  | 'shaders'
  | 'gizmos';

export type ReplayExportAction =
  | 'png'
  | 'webm_toggle'
  | 'rlbundle'
  | 'npz'
  | 'csv_metrics'
  | 'csv_tables'
  | 'analytics_json'
  | 'training_pack'
  | 'state_dialog';

export type ReplayToolAction =
  | 'command_palette'
  | 'layout_manager'
  | 'viewer_settings'
  | 'hotkey_guide'
  | 'load_local'
  | 'append_series';

export interface ReplayViewerToolbarProps {
  source: ReplaySource | 'local_file' | null;
  showPhysicsBadge?: boolean;
  physicsEngine: 'rapier' | 'builtin' | 'initializing';
  downloadHref?: string | null;
  downloadFilename?: string;
  viewerReady: boolean;
  profile: RLViewerProfile;
  arenaTheme: ArenaThemeId;
  glassPreset: GlassPresetId;
  hitboxesVisible: boolean;
  voronoiVisible: boolean;
  qaShowroom: QaShowroomPreset;
  isRecording: boolean;
  recordingSeconds: number;
  theaterMode: boolean;
  statusNotice?: string | null;
  onProfileChange: (profile: RLViewerProfile) => void;
  onArenaThemeChange: (theme: ArenaThemeId) => void;
  onGlassPresetChange: (preset: GlassPresetId) => void;
  onToggleHitboxes: () => void;
  onToggleVoronoi: () => void;
  onQaShowroomChange: (qa: QaShowroomPreset) => void;
  onExportAction: (action: ReplayExportAction) => void;
  onToolAction: (action: ReplayToolAction) => void;
  onToggleTheaterMode: () => void;
  onToggleFullscreen: () => void;
  onRegisterCloseMenus?: (closeFn: (() => boolean) | null) => void;
}

const SOURCE_BADGE: Record<
  ReplaySource | 'local_file',
  { label: string; className: string; title: string }
> = {
  local: {
    label: 'LOCAL',
    className: 'cartouche-celadon',
    title: 'Loaded from local Rocket League replays directory',
  },
  cache: {
    label: 'CACHE',
    className: 'cartouche-carry',
    title: 'Loaded from local Ballchasing replay cache',
  },
  ballchasing: {
    label: 'BALLCHASING',
    className: 'cartouche-gold',
    title: 'Downloaded from Ballchasing API and cached locally',
  },
  local_file: {
    label: 'LOCAL FILE',
    className: 'cartouche-monolith',
    title: 'Loaded directly from a local file on your computer',
  },
  unavailable: {
    label: 'UNAVAILABLE',
    className: 'cartouche-mono',
    title: 'Replay source unavailable',
  },
};

const PROFILE_OPTIONS: ReadonlyArray<readonly [RLViewerProfile, string]> = [
  ['coach', 'COACH'],
  ['broadcast', 'BROADCAST'],
  ['studio', 'STUDIO'],
  ['minimal_canvas', 'CANVAS'],
];

const ARENA_THEME_OPTIONS: ReadonlyArray<readonly [ArenaThemeId, string]> = [
  ['champions_night', 'CHAMPIONS NIGHT'],
  ['dfh_sunset', 'DFH SUNSET'],
  ['mannfield_studio', 'MANNFIELD STUDIO'],
  ['cyber_blueprint', 'CYBER BLUEPRINT'],
];

const GLASS_PRESET_OPTIONS: ReadonlyArray<readonly [GlassPresetId, string]> = [
  ['coach', 'COACH'],
  ['broadcast', 'BROADCAST'],
  ['tactical', 'TACTICAL'],
  ['cinematic', 'CINEMATIC'],
  ['blueprint', 'BLUEPRINT'],
];

const QA_SHOWROOM_OPTIONS: ReadonlyArray<readonly [QaShowroomPreset, string]> = [
  ['', 'OFF (LIVE REPLAY)'],
  ['octane', 'OCTANE SHOWROOM'],
  ['cars', 'ALL CARS'],
  ['ball', 'BALL'],
  ['pads', 'BOOST PADS'],
  ['shaders', 'SHADERS'],
  ['gizmos', 'GIZMOS'],
];

function formatRecTime(totalSeconds: number): string {
  const mins = Math.floor(totalSeconds / 60);
  const secs = totalSeconds % 60;
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
}

export const ReplayViewerToolbar: React.FC<ReplayViewerToolbarProps> = ({
  source,
  showPhysicsBadge = true,
  physicsEngine,
  downloadHref,
  downloadFilename,
  viewerReady,
  profile,
  arenaTheme,
  glassPreset,
  hitboxesVisible,
  voronoiVisible,
  qaShowroom,
  isRecording,
  recordingSeconds,
  theaterMode,
  statusNotice,
  onProfileChange,
  onArenaThemeChange,
  onGlassPresetChange,
  onToggleHitboxes,
  onToggleVoronoi,
  onQaShowroomChange,
  onExportAction,
  onToolAction,
  onToggleTheaterMode,
  onToggleFullscreen,
  onRegisterCloseMenus,
}) => {
  const [openMenu, setOpenMenu] = useState<'exports' | 'tools' | null>(null);
  const openMenuRef = useRef<'exports' | 'tools' | null>(null);
  openMenuRef.current = openMenu;
  const toolbarRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!onRegisterCloseMenus) return;
    onRegisterCloseMenus(() => {
      if (openMenuRef.current !== null) {
        setOpenMenu(null);
        return true;
      }
      return false;
    });
    return () => onRegisterCloseMenus(null);
  }, [onRegisterCloseMenus]);

  useEffect(() => {
    if (!openMenu) return;
    const onPointerDown = (e: PointerEvent) => {
      if (toolbarRef.current && e.target instanceof Node && !toolbarRef.current.contains(e.target)) {
        setOpenMenu(null);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        setOpenMenu(null);
      }
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown, true);
    };
  }, [openMenu]);

  const badge = source && source !== 'unavailable' ? SOURCE_BADGE[source] : null;

  const selectClass =
    'h-[26px] t-bg-page bg-page t-border border border-line hover:border-outline px-1.5 text-[11px] font-bold t-text-strong text-ink-strong uppercase outline-none t-radius cursor-pointer disabled:opacity-45';
  const btnClass =
    'h-[26px] px-2 t-bg-page bg-page t-border border border-line hover:border-outline text-[11px] font-extrabold t-text-ink text-ink hover:text-ink-strong uppercase tracking-wider t-radius cursor-pointer transition-colors disabled:opacity-45 whitespace-nowrap';
  const activeToggleClass =
    'h-[26px] px-2 t-bg-blue-ink bg-blue-ink t-text-on-accent text-page border border-blue-ink text-[11px] font-extrabold uppercase tracking-wider t-radius cursor-pointer transition-colors disabled:opacity-45 whitespace-nowrap';
  const menuItemClass =
    'w-full text-left px-3 py-1.5 text-xs font-bold uppercase tracking-wider text-ink hover:bg-panel-hover hover:text-ink-strong flex items-center justify-between gap-4 cursor-pointer disabled:opacity-45';

  return (
    <div
      ref={toolbarRef}
      data-testid="replay-viewer-toolbar"
      className="relative z-30 flex flex-wrap xl:flex-nowrap items-center justify-between gap-1.5 px-2.5 py-1.5 border-b border-line t-bg-panel bg-panel font-display shrink-0"
    >
      {/* Left: Source Badge, Physics Engine Status & Raw .replay Download */}
      <div className="flex items-center gap-1.5 min-w-0">
        {badge && (
          <span
            data-testid="replay-source-badge"
            title={badge.title}
            className={`px-1.5 py-0.5 text-[11px] font-extrabold t-font-mono uppercase tracking-wider whitespace-nowrap ${badge.className}`}
          >
            {badge.label}
          </span>
        )}
        {showPhysicsBadge && (
          <span
            data-testid="replay-physics-badge"
            title={
              physicsEngine === 'rapier'
                ? 'State Studio 120Hz physics powered by Rapier3D WASM'
                : physicsEngine === 'builtin'
                ? 'State Studio physics running on built-in analytical 120Hz solver'
                : 'Initializing 120Hz physics engine'
            }
            className="px-1.5 py-0.5 text-[11px] font-bold t-font-mono uppercase tracking-wider cartouche-mono whitespace-nowrap"
          >
            {physicsEngine === 'rapier'
              ? 'RAPIER 120HZ'
              : physicsEngine === 'builtin'
              ? 'ANALYTICAL 120HZ'
              : 'PHYSICS…'}
          </span>
        )}
        {downloadHref && (
          <a
            href={downloadHref}
            download={downloadFilename || true}
            data-testid="replay-download-link"
            className="h-[26px] inline-flex items-center px-2 t-bg-page bg-page border border-line hover:border-outline text-[11px] font-extrabold text-muted hover:text-ink uppercase tracking-wider t-radius transition-colors whitespace-nowrap"
            title="Download raw .replay file"
          >
            ↓ .REPLAY
          </a>
        )}
        {statusNotice && (
          <span
            data-testid="replay-toolbar-notice"
            role="status"
            className="px-1.5 py-0.5 text-[11px] font-bold text-blue-ink uppercase tracking-wider truncate max-w-[200px]"
          >
            {statusNotice}
          </span>
        )}
      </div>

      {/* Center: Profile, Arena Theme, Glass Preset & 3D Overlays */}
      <div className="flex items-center gap-1.5 shrink-0">
        <label className="inline-flex items-center gap-1 text-[10px] font-bold text-muted uppercase">
          <span className="hidden 2xl:inline">PROFILE</span>
          <select
            aria-label="Viewer profile"
            data-testid="replay-profile-select"
            value={profile}
            disabled={!viewerReady}
            onChange={(e) => onProfileChange(e.target.value as RLViewerProfile)}
            className={selectClass}
          >
            {PROFILE_OPTIONS.map(([val, lbl]) => (
              <option key={val} value={val}>
                {lbl}
              </option>
            ))}
          </select>
        </label>

        <label className="inline-flex items-center gap-1 text-[10px] font-bold text-muted uppercase">
          <span className="hidden 2xl:inline">THEME</span>
          <select
            aria-label="Arena theme"
            data-testid="replay-theme-select"
            value={arenaTheme}
            disabled={!viewerReady}
            onChange={(e) => onArenaThemeChange(e.target.value as ArenaThemeId)}
            className={selectClass}
          >
            {ARENA_THEME_OPTIONS.map(([val, lbl]) => (
              <option key={val} value={val}>
                {lbl}
              </option>
            ))}
          </select>
        </label>

        <label className="inline-flex items-center gap-1 text-[10px] font-bold text-muted uppercase">
          <span className="hidden 2xl:inline">GLASS</span>
          <select
            aria-label="Glass preset"
            data-testid="replay-glass-select"
            value={glassPreset}
            disabled={!viewerReady}
            onChange={(e) => onGlassPresetChange(e.target.value as GlassPresetId)}
            className={selectClass}
          >
            {GLASS_PRESET_OPTIONS.map(([val, lbl]) => (
              <option key={val} value={val}>
                {lbl}
              </option>
            ))}
          </select>
        </label>

        <button
          type="button"
          data-testid="replay-hitboxes-toggle"
          aria-pressed={hitboxesVisible}
          disabled={!viewerReady}
          onClick={onToggleHitboxes}
          className={hitboxesVisible ? activeToggleClass : btnClass}
          title="Toggle 3D car & ball collision hitboxes"
        >
          HITBOXES
        </button>

        <button
          type="button"
          data-testid="replay-voronoi-toggle"
          aria-pressed={voronoiVisible}
          disabled={!viewerReady}
          onClick={onToggleVoronoi}
          className={voronoiVisible ? activeToggleClass : btnClass}
          title="Toggle 3D field Voronoi territory overlay"
        >
          VORONOI
        </button>
      </div>

      {/* Right: REC pill, Exports Menu, Tools Menu, Theater & Fullscreen */}
      <div className="flex flex-wrap items-center gap-1.5">
        {isRecording && (
          <button
            type="button"
            data-testid="replay-stop-rec-btn"
            onClick={() => onExportAction('webm_toggle')}
            className="h-[28px] px-2.5 bg-[#c81624] text-white border border-[#ff6b78] text-xs font-extrabold t-font-mono uppercase tracking-wider t-radius cursor-pointer inline-flex items-center gap-1.5 whitespace-nowrap"
            title="Stop WebM recording and download clip"
          >
            <span className="inline-block w-2 h-2 rounded-full bg-white animate-pulse" />
            REC {formatRecTime(recordingSeconds)} · STOP
          </button>
        )}

        {/* Exports Dropdown */}
        <div className="relative">
          <button
            type="button"
            data-testid="replay-exports-menu-btn"
            aria-haspopup="menu"
            aria-expanded={openMenu === 'exports'}
            disabled={!viewerReady}
            onClick={() => setOpenMenu((m) => (m === 'exports' ? null : 'exports'))}
            className={openMenu === 'exports' ? activeToggleClass : btnClass}
          >
            EXPORTS ▾
          </button>
          {openMenu === 'exports' && (
            <div
              role="menu"
              data-testid="replay-exports-menu"
              className="absolute right-0 mt-1 w-64 t-bg-modal bg-modal border border-outline shadow-2xl py-1 z-50 t-radius"
            >
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('png');
                }}
              >
                <span>PNG SNAPSHOT (1080P)</span>
                <span className="text-muted text-[10px]">.PNG</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('webm_toggle');
                }}
              >
                <span>{isRecording ? 'STOP WEBM RECORDING' : 'RECORD WEBM CLIP'}</span>
                <span className="text-muted text-[10px]">.WEBM</span>
              </button>
              <div className="my-1 border-t border-line" />
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('rlbundle');
                }}
              >
                <span>STATE &amp; PLAYBOOK BUNDLE</span>
                <span className="text-muted text-[10px]">.RLBUNDLE</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('npz');
                }}
              >
                <span>TELEMETRY TENSOR</span>
                <span className="text-muted text-[10px]">.NPZ</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('csv_metrics');
                }}
              >
                <span>ANALYTICS (79 METRICS)</span>
                <span className="text-muted text-[10px]">.CSV</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('csv_tables');
                }}
              >
                <span>ANALYTICS (ALL 11 TABLES)</span>
                <span className="text-muted text-[10px]">.CSV</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('analytics_json');
                }}
              >
                <span>ANALYTICS REPORT</span>
                <span className="text-muted text-[10px]">.JSON</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('training_pack');
                }}
              >
                <span>BAKKESMOD TRAINING PACK</span>
                <span className="text-muted text-[10px]">.JSON</span>
              </button>
              <div className="my-1 border-t border-line" />
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onExportAction('state_dialog');
                }}
              >
                <span>STATE IMPORT / EXPORT…</span>
                <span className="text-muted text-[10px]">RLBOT/SIM</span>
              </button>
            </div>
          )}
        </div>

        {/* Tools / More Dropdown */}
        <div className="relative">
          <button
            type="button"
            data-testid="replay-tools-menu-btn"
            aria-haspopup="menu"
            aria-expanded={openMenu === 'tools'}
            onClick={() => setOpenMenu((m) => (m === 'tools' ? null : 'tools'))}
            className={openMenu === 'tools' ? activeToggleClass : btnClass}
          >
            TOOLS ▾
          </button>
          {openMenu === 'tools' && (
            <div
              role="menu"
              data-testid="replay-tools-menu"
              className="absolute right-0 mt-1 w-64 t-bg-modal bg-modal border border-outline shadow-2xl py-1 z-50 t-radius"
            >
              <button
                type="button"
                role="menuitem"
                disabled={!viewerReady}
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onToolAction('command_palette');
                }}
              >
                <span>COMMAND PALETTE</span>
                <span className="text-muted text-[10px] t-font-mono">CTRL+K</span>
              </button>
              <button
                type="button"
                role="menuitem"
                disabled={!viewerReady}
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onToolAction('layout_manager');
                }}
              >
                <span>LAYOUT MANAGER</span>
                <span className="text-muted text-[10px]">PROFILES</span>
              </button>
              <button
                type="button"
                role="menuitem"
                disabled={!viewerReady}
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onToolAction('viewer_settings');
                }}
              >
                <span>VIEWER SETTINGS</span>
                <span className="text-muted text-[10px]">PANELS</span>
              </button>
              <button
                type="button"
                role="menuitem"
                disabled={!viewerReady}
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onToolAction('hotkey_guide');
                }}
              >
                <span>HOTKEY GUIDE</span>
                <span className="text-muted text-[10px] t-font-mono">?</span>
              </button>
              <div className="my-1 border-t border-line" />
              <button
                type="button"
                role="menuitem"
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onToolAction('load_local');
                }}
              >
                <span>LOAD LOCAL FILE…</span>
                <span className="text-muted text-[10px]">.REPLAY</span>
              </button>
              <button
                type="button"
                role="menuitem"
                disabled={!viewerReady}
                className={menuItemClass}
                onClick={() => {
                  setOpenMenu(null);
                  onToolAction('append_series');
                }}
              >
                <span>APPEND TO SERIES…</span>
                <span className="text-muted text-[10px]">+MATCH</span>
              </button>
              <div className="my-1 border-t border-line" />
              <div className="px-3 py-1.5 flex flex-col gap-1">
                <label
                  htmlFor="replay-qa-select"
                  className="text-[10px] font-extrabold uppercase tracking-wider text-muted"
                >
                  VISUAL QA SHOWROOM
                </label>
                <select
                  id="replay-qa-select"
                  data-testid="replay-qa-select"
                  value={qaShowroom}
                  disabled={!viewerReady}
                  onChange={(e) => {
                    onQaShowroomChange(e.target.value as QaShowroomPreset);
                    setOpenMenu(null);
                  }}
                  className="h-[26px] w-full t-bg-page bg-page border border-line px-2 text-xs font-bold text-ink uppercase outline-none t-radius cursor-pointer"
                >
                  {QA_SHOWROOM_OPTIONS.map(([val, lbl]) => (
                    <option key={val || 'off'} value={val}>
                      {lbl}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          )}
        </div>

        <button
          type="button"
          data-testid="replay-theater-toggle"
          aria-pressed={theaterMode}
          onClick={onToggleTheaterMode}
          className={theaterMode ? activeToggleClass : btnClass}
          title={
            theaterMode
              ? 'Exit Full-Viewport Theater Mode (Esc)'
              : 'Expand to Full-Viewport Theater Mode'
          }
        >
          {theaterMode ? 'EXIT THEATER' : 'THEATER'}
        </button>

        <button
          type="button"
          data-testid="replay-fullscreen-btn"
          disabled={!viewerReady}
          onClick={onToggleFullscreen}
          className={btnClass}
          title="Toggle native browser fullscreen (F)"
        >
          FULLSCREEN
        </button>
      </div>
    </div>
  );
};
