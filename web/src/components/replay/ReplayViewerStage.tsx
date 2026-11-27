import React, { useEffect, useRef } from 'react';
import {
  createRapierRolloutPlugin,
  createReplayViewer,
  type ArenaThemeId,
  type GlassPresetId,
  type IPhysicsStepper,
  type RLReplayViewer,
  type RLViewerProfile,
} from '@rl-replay/viewer-sdk';
import type { QaShowroomPreset } from './ReplayViewerToolbar';
import {
  containPopoverRequests,
  hasStudioWorldStateEdits,
  installExternalLoadObserver,
  installProfileSyncWrappers,
  installStudioSelectionAutoInspector,
  installUnifiedDropzoneBridge,
  loadTypedFileIntoViewer,
  redockPopoverPanels,
} from './replayViewerBridge';

export {
  containPopoverRequests,
  hasStudioWorldStateEdits,
  installExternalLoadObserver,
  installProfileSyncWrappers,
  installStudioSelectionAutoInspector,
  installUnifiedDropzoneBridge,
  loadTypedFileIntoViewer,
  redockPopoverPanels,
};

export const VIEWER_SDK_WASM_URL = '/viewer-sdk/wasm/rl_boxcars_v4.wasm';
export const VIEWER_SDK_ASSETS_BASE_URL = '/viewer-sdk/assets/';

export interface SdkSyncedState {
  profile?: RLViewerProfile;
  voronoiVisible?: boolean;
  hitboxesVisible?: boolean;
}

export interface ReplayLoadInput {
  readonly id: number;
  readonly filename: string;
  readonly bytes?: Uint8Array;
  readonly file?: File;
  readonly appendSeries?: boolean;
  readonly extraFiles?: readonly File[];
}

export interface ReplayViewerStageProps {
  loadInput: ReplayLoadInput | null;
  profile: RLViewerProfile;
  arenaTheme: ArenaThemeId;
  glassPreset: GlassPresetId;
  hitboxesVisible: boolean;
  voronoiVisible: boolean;
  qaShowroom: QaShowroomPreset;
  theaterMode: boolean;
  onViewerReady: (
    viewer: RLReplayViewer,
    shadowRoot: ShadowRoot,
    physicsEngine: 'rapier' | 'builtin',
  ) => void;
  onReplayLoaded?: (notice?: string) => void;
  onSdkStateSync?: (patch: SdkSyncedState) => void;
  onExternalLocalLoad?: (filename?: string) => void;
  onExternalFilesSelected?: (files: File[]) => void;
  onNotice?: (message: string) => void;
  onError?: (err: Error) => void;
}

/**
 * React 19 StrictMode-safe open Shadow DOM host for `@rl-replay/viewer-sdk`.
 * Mounts via `createReplayViewer(shadowRoot, opts)` so SDK stylesheets (including
 * `telestrator.css`) stay 100% encapsulated in the ShadowRoot with zero Light DOM
 * Tailwind/XL-scale interference while exposing full constructor options such as
 * the 120Hz Rapier rollout plugin.
 */
const ReplayViewerStage: React.FC<ReplayViewerStageProps> = ({
  loadInput,
  profile,
  arenaTheme,
  glassPreset,
  hitboxesVisible,
  voronoiVisible,
  qaShowroom,
  theaterMode,
  onViewerReady,
  onReplayLoaded,
  onSdkStateSync,
  onExternalLocalLoad,
  onExternalFilesSelected,
  onNotice,
  onError,
}) => {
  const hostRef = useRef<HTMLDivElement>(null);
  const viewerRef = useRef<RLReplayViewer | null>(null);
  const shadowRef = useRef<ShadowRoot | null>(null);
  const loadGenRef = useRef(0);
  const lastLoadedIdRef = useRef<number>(-1);
  const hostLoadingRef = useRef(false);

  // Value-comparison refs prevent double SDK calls when syncing back from SDK HUD / Palette / Layouts.
  const lastAppliedProfileRef = useRef<RLViewerProfile>(profile);
  const lastAppliedVoronoiRef = useRef<boolean>(voronoiVisible);
  const lastAppliedHitboxesRef = useRef<boolean>(hitboxesVisible);

  const callbacksRef = useRef({
    onViewerReady,
    onReplayLoaded,
    onSdkStateSync,
    onExternalLocalLoad,
    onExternalFilesSelected,
    onNotice,
    onError,
  });
  callbacksRef.current = {
    onViewerReady,
    onReplayLoaded,
    onSdkStateSync,
    onExternalLocalLoad,
    onExternalFilesSelected,
    onNotice,
    onError,
  };

  const initialOptsRef = useRef({
    profile,
    arenaTheme,
    glassPreset,
    hitboxesVisible,
    voronoiVisible,
    qaShowroom,
  });

  const latestInputRef = useRef<ReplayLoadInput | null>(loadInput);
  latestInputRef.current = loadInput;

  const executeLoad = async (viewer: RLReplayViewer, input: ReplayLoadInput) => {
    if (viewer.isDisposed() || lastLoadedIdRef.current === input.id) return;
    lastLoadedIdRef.current = input.id;
    const gen = ++loadGenRef.current;
    hostLoadingRef.current = true;
    try {
      const outcome = await loadTypedFileIntoViewer(viewer, input, Boolean(input.appendSeries));
      let notice = outcome.notice;
      if (input.extraFiles && input.extraFiles.length > 0) {
        for (const extra of input.extraFiles) {
          if (gen !== loadGenRef.current || viewer.isDisposed()) return;
          await loadTypedFileIntoViewer(viewer, { file: extra, filename: extra.name }, true);
        }
        notice = `LOADED ${1 + input.extraFiles.length} FILES`;
      }
      if (gen !== loadGenRef.current || viewer.isDisposed()) return;
      redockPopoverPanels(viewer);
      callbacksRef.current.onReplayLoaded?.(notice);
    } catch (err) {
      if (gen !== loadGenRef.current || viewer.isDisposed()) return;
      callbacksRef.current.onError?.(
        err instanceof Error ? err : new Error(String(err)),
      );
    } finally {
      hostLoadingRef.current = false;
    }
  };

  // Mount / dispose viewer cleanly across React 19 StrictMode double-invocations.
  useEffect(() => {
    const hostEl = hostRef.current;
    if (!hostEl) return;

    let cancelled = false;
    let createdViewer: RLReplayViewer | null = null;
    let stepper: IPhysicsStepper | undefined;
    let unsubWorkspaceVis: (() => void) | null = null;
    let redockScheduled = false;

    const shadow = hostEl.shadowRoot ?? hostEl.attachShadow({ mode: 'open' });
    shadowRef.current = shadow;

    const scheduleRedock = (v: RLReplayViewer) => {
      if (redockScheduled) return;
      redockScheduled = true;
      queueMicrotask(() => {
        redockScheduled = false;
        if (!v.isDisposed()) {
          redockPopoverPanels(v);
        }
      });
    };

    (async () => {
      let physicsEngine: 'rapier' | 'builtin' = 'builtin';
      try {
        stepper = await createRapierRolloutPlugin({ fallbackToBuiltin: true });
        physicsEngine = stepper.id === 'rapier3d-120hz' ? 'rapier' : 'builtin';
      } catch {
        stepper = undefined;
        physicsEngine = 'builtin';
      }

      if (cancelled) {
        stepper?.dispose?.();
        return;
      }

      try {
        while (shadow.firstChild) {
          shadow.removeChild(shadow.firstChild);
        }

        const init = initialOptsRef.current;
        const viewer = createReplayViewer(shadow, {
          profile: init.profile,
          quality: 'auto',
          background: '#000000',
          arenaTheme: init.arenaTheme,
          glassPreset: init.glassPreset,
          autoplay: true,
          qa: init.qaShowroom || undefined,
          physicsStepper: stepper,
          assets: {
            wasmSource: VIEWER_SDK_WASM_URL,
            assetBaseUrl: VIEWER_SDK_ASSETS_BASE_URL,
          },
        });

        if (cancelled) {
          viewer.dispose();
          stepper?.dispose?.();
          return;
        }

        // 1. Keep Pop buttons & Studio panels strictly inside stage bounds
        containPopoverRequests(viewer);
        installStudioSelectionAutoInspector(viewer, () => lastAppliedProfileRef.current);

        // 2. Wrap viewer profile/overlay/layout methods for bi-directional sync
        installProfileSyncWrappers(
          viewer,
          () => lastAppliedProfileRef.current,
          (next) => {
            lastAppliedProfileRef.current = next;
          },
          (next) => {
            callbacksRef.current.onSdkStateSync?.({ profile: next });
          },
        );

        const origSetVoronoi = viewer.setVoronoiEnabled.bind(viewer);
        viewer.setVoronoiEnabled = (enabled: boolean) => {
          origSetVoronoi(enabled);
          if (lastAppliedVoronoiRef.current !== enabled) {
            lastAppliedVoronoiRef.current = enabled;
            callbacksRef.current.onSdkStateSync?.({ voronoiVisible: enabled });
          }
        };

        const origSetHitboxes = viewer.setHitboxesVisible.bind(viewer);
        viewer.setHitboxesVisible = (enabled: boolean) => {
          origSetHitboxes(enabled);
          if (lastAppliedHitboxesRef.current !== enabled) {
            lastAppliedHitboxesRef.current = enabled;
            callbacksRef.current.onSdkStateSync?.({ hitboxesVisible: enabled });
          }
        };

        // 3. Unify SDK HUD "Load" & active-stage drag-and-drop with RL-Sync's typed loader
        installUnifiedDropzoneBridge(
          viewer,
          (files) => callbacksRef.current.onExternalFilesSelected?.(files),
          (rejectedName) => callbacksRef.current.onNotice?.(`UNSUPPORTED FILE: ${rejectedName}`),
        );

        // 4. Defense-in-depth observer for any direct pipeline loads outside host orchestration
        installExternalLoadObserver(
          viewer,
          () => hostLoadingRef.current,
          (label) => callbacksRef.current.onExternalLocalLoad?.(label),
        );

        unsubWorkspaceVis = viewer.ui.workspace.onVisibilityChange(() => {
          scheduleRedock(viewer);
        });

        redockPopoverPanels(viewer);
        lastAppliedProfileRef.current = init.profile;
        lastAppliedHitboxesRef.current = init.hitboxesVisible;
        lastAppliedVoronoiRef.current = init.voronoiVisible;
        if (init.hitboxesVisible) origSetHitboxes(true);
        if (init.voronoiVisible) origSetVoronoi(true);

        createdViewer = viewer;
        viewerRef.current = viewer;
        lastLoadedIdRef.current = -1;
        callbacksRef.current.onViewerReady(viewer, shadow, physicsEngine);

        const pendingInput = latestInputRef.current;
        if (pendingInput) {
          void executeLoad(viewer, pendingInput);
        }
      } catch (err) {
        stepper?.dispose?.();
        if (!cancelled) {
          callbacksRef.current.onError?.(
            err instanceof Error ? err : new Error(String(err)),
          );
        }
      }
    })();

    return () => {
      cancelled = true;
      loadGenRef.current += 1;
      unsubWorkspaceVis?.();
      unsubWorkspaceVis = null;
      viewerRef.current = null;
      lastLoadedIdRef.current = -1;
      if (createdViewer && !createdViewer.isDisposed()) {
        createdViewer.dispose();
      }
      stepper?.dispose?.();
    };
  }, []);

  // Load new input whenever loadInput.id changes and viewer is ready.
  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed() || !loadInput) return;
    void executeLoad(viewer, loadInput);
  }, [loadInput]);

  // Sync host toolbar controls to live viewer using value-comparison refs (zero double calls).
  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed() || lastAppliedProfileRef.current === profile) return;
    lastAppliedProfileRef.current = profile;
    viewer.setProfile(profile);
  }, [profile]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed()) return;
    viewer.setArenaTheme(arenaTheme);
  }, [arenaTheme]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed()) return;
    viewer.setGlassPreset(glassPreset);
  }, [glassPreset]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed() || lastAppliedHitboxesRef.current === hitboxesVisible) return;
    lastAppliedHitboxesRef.current = hitboxesVisible;
    viewer.setHitboxesVisible(hitboxesVisible);
  }, [hitboxesVisible]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed() || lastAppliedVoronoiRef.current === voronoiVisible) return;
    lastAppliedVoronoiRef.current = voronoiVisible;
    viewer.setVoronoiEnabled(voronoiVisible);
  }, [voronoiVisible]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed()) return;
    viewer.setQaShowroom(qaShowroom || null);
  }, [qaShowroom]);

  // Trigger resize after inline <-> Theater layout transition.
  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed()) return;
    const raf = requestAnimationFrame(() => {
      if (!viewer.isDisposed()) {
        redockPopoverPanels(viewer);
        viewer.resizeViewport();
      }
    });
    return () => cancelAnimationFrame(raf);
  }, [theaterMode]);

  return (
    <div
      ref={hostRef}
      data-testid="replay-viewer-shadow-host"
      className="w-full h-full relative bg-black"
      style={{
        fontSize: '12px',
        lineHeight: 1.4,
        minHeight: '320px',
      }}
    />
  );
};

export default ReplayViewerStage;
