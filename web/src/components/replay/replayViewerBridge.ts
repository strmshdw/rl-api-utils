import {
  buildTimelineClockMap,
  classifyReplayFile,
  type DockRegion,
  type GameWorldState,
  type ReplayLoadSnapshot,
  type RLReplayViewer,
  type RLViewerProfile,
} from '@rl-replay/viewer-sdk';

export const LEFT_RAIL_PANELS = new Set([
  'entity-outliner',
  'minimap',
  'camera-presets',
  'playbook-reel',
  'player-details',
  'clock-mutators',
]);

export const BUILTIN_PROFILES = new Set<string>([
  'broadcast',
  'coach',
  'studio',
  'minimal_canvas',
]);

const GENERIC_SDK_LABELS = new Set(['replay', 'match', 'payload']);

/**
 * Re-docks any SDK `<dialog popover="manual">` panel into the viewer stage's
 * left or right side rails so Studio panels never escape the embedded stage or
 * occlude RL-Sync's header, tabs, or toolbar (inline or Theater mode).
 */
export function redockPopoverPanels(viewer: RLReplayViewer): number {
  if (viewer.isDisposed()) return 0;
  const snapshot = viewer.ui.workspace.getSnapshot();
  let moved = 0;
  for (const [panelId, panelState] of Object.entries(snapshot.panels)) {
    if (panelState.dock === 'popover') {
      const targetRail = LEFT_RAIL_PANELS.has(panelId) ? 'left' : 'right';
      viewer.ui.workspace.dockWindowAt(panelId, targetRail);
      moved += 1;
    }
  }
  return moved;
}

/**
 * Wraps each workspace window's `setDock` method so clicking a panel's `Pop`
 * button converts `'popover'` into stage-contained `'float'` (and toggles back
 * to its home rail on a second click), keeping all panels strictly inside the
 * Shadow DOM stage bounds.
 */
export function containPopoverRequests(viewer: RLReplayViewer): void {
  const ws = viewer.ui?.workspace;
  if (!ws || typeof ws.getAllWindows !== 'function') return;
  for (const win of ws.getAllWindows()) {
    if (!win || typeof win.setDock !== 'function') continue;
    const origSetDock = win.setDock.bind(win);
    const homeRail: DockRegion = LEFT_RAIL_PANELS.has(win.config.id) ? 'left' : 'right';
    win.setDock = (dock: DockRegion) => {
      if (dock === 'popover') {
        const current = win.getState().dock;
        origSetDock(current === 'float' ? homeRail : 'float');
        return;
      }
      origSetDock(dock);
    };
  }
}

/**
 * Keeps the Studio right-rail inspector synchronized with entity selection
 * (`ball-inspector` <-> `car-kinematics`) even when panels are docked in side
 * rails rather than top-layer popovers.
 */
export function installStudioSelectionAutoInspector(
  viewer: RLReplayViewer,
  getProfile: () => RLViewerProfile,
): void {
  const panels = viewer.ui?.panels;
  const ws = viewer.ui?.workspace;
  if (!panels || !ws || typeof panels.syncWorld !== 'function') return;

  const origSyncWorld = panels.syncWorld.bind(panels);
  let prevSelectionKey = '';

  panels.syncWorld = (
    world,
    selected,
    branches,
    activeBranchId,
    followedCarId,
    directorStatus,
    historyState,
  ) => {
    const key = selected ? `${selected.kind}:${selected.id}` : '';
    if (key !== prevSelectionKey) {
      prevSelectionKey = key;
      if (getProfile() === 'studio' && selected) {
        const targetId = selected.kind === 'ball' ? 'ball-inspector' : 'car-kinematics';
        const otherId = selected.kind === 'ball' ? 'car-kinematics' : 'ball-inspector';
        if (ws.isWindowVisible(otherId)) {
          ws.setPanelVisible(otherId, false);
        }
        if (!ws.isWindowVisible(targetId)) {
          ws.setPanelVisible(targetId, true);
        }
        const snap = ws.getSnapshot();
        if (snap.panels[targetId]?.dock === 'popover') {
          ws.dockWindowAt(targetId, 'right');
        }
      }
    }
    return origSyncWorld(
      world,
      selected,
      branches,
      activeBranchId,
      followedCarId,
      directorStatus,
      historyState,
    );
  };
}

/**
 * Bridges `viewer.setProfile` and `viewer.ui.applyStoredProfile` with the host
 * toolbar. Built-in profiles selected via `LayoutManagerDialog` delegate
 * directly to `viewer.setProfile(...)` so Studio mode activates `stage.setStudioMode`,
 * 3D gizmos, gizmo toolbar, mode banner, studio camera, and rail redocking cleanly.
 */
export function installProfileSyncWrappers(
  viewer: RLReplayViewer,
  getLastProfile: () => RLViewerProfile,
  setLastProfile: (profile: RLViewerProfile) => void,
  onSyncProfile: (profile: RLViewerProfile) => void,
): void {
  const origSetProfile = viewer.setProfile.bind(viewer);
  viewer.setProfile = (nextProfile: RLViewerProfile) => {
    origSetProfile(nextProfile);
    redockPopoverPanels(viewer);
    if (getLastProfile() !== nextProfile) {
      setLastProfile(nextProfile);
      onSyncProfile(nextProfile);
    }
  };

  if (typeof viewer.ui?.applyStoredProfile === 'function') {
    const origApplyStored = viewer.ui.applyStoredProfile.bind(viewer.ui);
    viewer.ui.applyStoredProfile = (profileId: string) => {
      if (BUILTIN_PROFILES.has(profileId)) {
        viewer.setProfile(profileId as RLViewerProfile);
        return;
      }
      origApplyStored(profileId);
      redockPopoverPanels(viewer);
    };
  }
}

/**
 * Routes both SDK HUD `Load` file-picker selections and active-stage drag-and-drop
 * through RL-Sync's unified `loadTypedFileIntoViewer` pipeline so `.replay`,
 * `.rlbundle` (preserving Studio worldState edits & branches), `.rlplaybook.json`,
 * and `.json` (WorldState / RLBot / RocketSim / ViewerPayload) behave identically
 * across every load entry point.
 */
export function installUnifiedDropzoneBridge(
  viewer: RLReplayViewer,
  onFilesAccepted: (files: File[]) => void,
  onRejectedFile?: (filename: string) => void,
): void {
  const dropzone = viewer.ui?.dropzone as unknown as
    | { processFileList?: (files: File[]) => void }
    | undefined;
  if (!dropzone || typeof dropzone.processFileList !== 'function') return;

  dropzone.processFileList = (files: File[]) => {
    if (!Array.isArray(files) || files.length === 0) return;
    const accepted: File[] = [];
    for (const file of files) {
      if (classifyReplayFile(file.name)) {
        accepted.push(file);
      } else {
        onRejectedFile?.(file.name);
      }
    }
    if (accepted.length > 0) {
      onFilesAccepted(accepted);
    }
  };
}

/**
 * Defense-in-depth observer for any replay load invoked directly on `viewer.pipeline`
 * outside host-initiated loads, ensuring the Host Toolbar badge and download link
 * stay synchronized even when `appendSeries = true` or raw `Uint8Array` bytes are used.
 */
export function installExternalLoadObserver(
  viewer: RLReplayViewer,
  isHostLoading: () => boolean,
  onExternalLocalLoad: (filename?: string) => void,
): void {
  const resolveLabel = (candidate?: string): string => {
    const trimmed = candidate?.trim();
    if (trimmed && !GENERIC_SDK_LABELS.has(trimmed.toLowerCase())) {
      return trimmed;
    }
    const activeLabel = viewer.pipeline.store.getActiveMatch?.()?.label?.trim();
    if (activeLabel && !GENERIC_SDK_LABELS.has(activeLabel.toLowerCase())) {
      return activeLabel;
    }
    return 'Local Replay';
  };

  let inReplayWrapper = false;
  let notifiedInWrapper = false;

  const origPipeLoadReplay = viewer.pipeline.loadReplay.bind(viewer.pipeline);
  viewer.pipeline.loadReplay = async (source, appendSeries) => {
    const wasInWrapper = inReplayWrapper;
    inReplayWrapper = true;
    if (!wasInWrapper) notifiedInWrapper = false;
    try {
      const snap = await origPipeLoadReplay(source, appendSeries);
      if (!isHostLoading() && !notifiedInWrapper) {
        notifiedInWrapper = true;
        const name = source instanceof File ? source.name : undefined;
        onExternalLocalLoad(resolveLabel(name));
      }
      return snap;
    } finally {
      inReplayWrapper = wasInWrapper;
    }
  };

  const origPipeLoadRlv4 = viewer.pipeline.loadRlv4Buffer.bind(viewer.pipeline);
  viewer.pipeline.loadRlv4Buffer = async (buf, label, appendSeries, precomputed) => {
    const snap = await origPipeLoadRlv4(buf, label, appendSeries, precomputed);
    if (!isHostLoading()) {
      if (inReplayWrapper) {
        notifiedInWrapper = true;
      }
      onExternalLocalLoad(resolveLabel(label));
    }
    return snap;
  };

  const origPipeLoadPayload = viewer.pipeline.loadViewerPayload.bind(viewer.pipeline);
  viewer.pipeline.loadViewerPayload = async (payload, label) => {
    const snap = await origPipeLoadPayload(payload, label);
    if (!isHostLoading()) {
      onExternalLocalLoad(resolveLabel(label));
    }
    return snap;
  };
}

function syncUiAfterDirectRlv4Load(viewer: RLReplayViewer, snap: ReplayLoadSnapshot): void {
  const store = viewer.pipeline.store;
  const meta = store.meta;
  const pacing = viewer.pipeline.getPacing();
  const activeMatch = store.getActiveMatch();

  viewer.ui.toolbar?.setDrawMode(false);
  viewer.ui.kickoffOverlay?.reset();
  viewer.stage?.entities?.clearTransientVfx?.();
  viewer.stage?.cameraDirector?.requestSnap?.();

  viewer.ui.hudBar?.setSeriesMatches(store.getPlaylist(), activeMatch?.id);
  const clockMap =
    store.times && store.gameState
      ? buildTimelineClockMap(store, pacing?.getIndex() ?? null)
      : null;
  viewer.ui.scrubber?.setClockMap(clockMap);
  viewer.ui.scrubber?.setDeadTimeSpans(pacing?.getStoppages() ?? []);
  viewer.ui.scrubber?.markers?.setFromReplayEvents(meta.events);
  viewer.ui.scrubber?.renderEventTicks();
  viewer.ui.panels?.syncTelemetry({
    metrics: snap.metrics ?? viewer.pipeline.getMetrics(),
    insights: snap.insights ?? viewer.pipeline.getInsights(),
    pvaMoments: snap.pvaMoments ?? viewer.pipeline.getPvaMoments(),
    players: meta.players.map((p) => ({ name: p.name, team: p.team })),
  });
}

function tupleDiffers(
  a: readonly number[] | undefined,
  b: readonly number[] | undefined,
  eps: number,
): boolean {
  if (!a || !b) return false;
  if (a.length !== b.length) return true;
  for (let i = 0; i < a.length; i++) {
    if (Math.abs(a[i] - b[i]) > eps) return true;
  }
  return false;
}

function quatDiffers(
  a: readonly [number, number, number, number] | undefined,
  b: readonly [number, number, number, number] | undefined,
  eps = 0.01,
): boolean {
  if (!a || !b) return false;
  // Quaternions q and -q represent the same orientation.
  const dot = Math.abs(a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3]);
  return 1 - Math.min(1, dot) > eps;
}

export function hasStudioWorldStateEdits(
  viewer: RLReplayViewer,
  worldState: GameWorldState,
  branchCount: number,
): boolean {
  if (worldState.sourceMode === 'studio_branch' || worldState.sourceMode === 'sandbox') {
    return true;
  }
  if (branchCount > 1) return true;
  if (viewer.pipeline.store.frameCount <= 0) return true;

  try {
    const projected = viewer.studio.projectReplayFrame(
      viewer.pipeline.store,
      worldState.timestampSeconds ?? 0,
      false,
    );
    if (projected.balls.length !== worldState.balls.length) return true;
    if (projected.cars.length !== worldState.cars.length) return true;

    for (let i = 0; i < worldState.balls.length; i++) {
      const a = worldState.balls[i];
      const b = projected.balls[i];
      if (tupleDiffers(a.pos, b.pos, 0.5)) return true;
      if (tupleDiffers(a.vel, b.vel, 1.0)) return true;
      if (tupleDiffers(a.angVel, b.angVel, 0.05)) return true;
      if (quatDiffers(a.rot, b.rot, 0.01)) return true;
      if (a.shape !== undefined && b.shape !== undefined && a.shape !== b.shape) return true;
      if (
        a.isKinematicLocked !== undefined &&
        b.isKinematicLocked !== undefined &&
        a.isKinematicLocked !== b.isKinematicLocked
      ) {
        return true;
      }
    }

    for (let i = 0; i < worldState.cars.length; i++) {
      const a = worldState.cars[i];
      const b = projected.cars[i];
      if (tupleDiffers(a.pos, b.pos, 0.5)) return true;
      if (tupleDiffers(a.vel, b.vel, 1.0)) return true;
      if (tupleDiffers(a.angVel, b.angVel, 0.05)) return true;
      if (quatDiffers(a.rot, b.rot, 0.01)) return true;
      if (
        typeof a.boostAmount === 'number' &&
        typeof b.boostAmount === 'number' &&
        Math.abs(a.boostAmount - b.boostAmount) > 0.5
      ) {
        return true;
      }
      if (
        a.isDemolished !== undefined &&
        b.isDemolished !== undefined &&
        a.isDemolished !== b.isDemolished
      ) {
        return true;
      }
      if (a.role !== undefined && b.role !== undefined && a.role !== b.role) return true;
    }

    if (worldState.boostPads && projected.boostPads) {
      if (worldState.boostPads.length !== projected.boostPads.length) return true;
      for (let i = 0; i < worldState.boostPads.length; i++) {
        const a = worldState.boostPads[i];
        const b = projected.boostPads[i];
        if (a.isActive !== b.isActive) return true;
        if (Math.abs((a.cooldownRemaining ?? 0) - (b.cooldownRemaining ?? 0)) > 0.25) {
          return true;
        }
      }
    }

    if (worldState.clockAndScore && projected.clockAndScore) {
      const a = worldState.clockAndScore;
      const b = projected.clockAndScore;
      if (
        a.blueScore !== b.blueScore ||
        a.orangeScore !== b.orangeScore ||
        a.isOvertime !== b.isOvertime ||
        a.unlimitedTime !== b.unlimitedTime ||
        Math.abs(a.clockSeconds - b.clockSeconds) > 1.0
      ) {
        return true;
      }
    }

    if (worldState.mutators && projected.mutators) {
      const a = worldState.mutators;
      const b = projected.mutators;
      if (
        Math.abs(a.gravityZ - b.gravityZ) > 0.5 ||
        Math.abs(a.gameSpeedScale - b.gameSpeedScale) > 0.01 ||
        Math.abs(a.ballMaxSpeed - b.ballMaxSpeed) > 1.0 ||
        a.boostRechargeMode !== b.boostRechargeMode ||
        a.demoMode !== b.demoMode
      ) {
        return true;
      }
    }
  } catch {
    return true;
  }
  return false;
}

/**
 * Loads `.replay`, `.rlbundle`, `.rlplaybook.json`, or `.json` files cleanly
 * using SDK classification & codecs instead of feeding non-replay files to WASM.
 */
export async function loadTypedFileIntoViewer(
  viewer: RLReplayViewer,
  input: { file?: File; bytes?: Uint8Array; filename: string },
  appendSeries = false,
): Promise<{ notice?: string; switchedToStudio?: boolean }> {
  const filename = input.file?.name || input.filename || 'replay.replay';
  const kind = classifyReplayFile(filename) ?? 'replay';

  if (kind === 'replay') {
    const file =
      input.file ??
      (() => {
        const raw = input.bytes ?? new Uint8Array(0);
        const copy = new Uint8Array(raw.byteLength);
        copy.set(raw);
        return new File([copy.buffer], filename, { type: 'application/octet-stream' });
      })();
    await viewer.loadReplay(file, appendSeries);
    if (!appendSeries) {
      viewer.play();
    }
    return { notice: appendSeries ? `APPENDED ${filename} TO SERIES` : undefined };
  }

  const rawBytes =
    input.bytes ??
    new Uint8Array(
      await (input.file ? input.file.arrayBuffer() : Promise.resolve(new ArrayBuffer(0))),
    );
  if (rawBytes.byteLength === 0) {
    throw new Error(`File "${filename}" is empty.`);
  }

  if (kind === 'bundle') {
    const bundle = await viewer.studio.unpackBundle(rawBytes);
    let loadedTelemetry = false;

    if (bundle.telemetryRlv4 && bundle.telemetryRlv4.byteLength > 0) {
      const copy = new Uint8Array(bundle.telemetryRlv4.byteLength);
      copy.set(bundle.telemetryRlv4);
      const label = bundle.manifest?.match_guid || filename;
      const snap = await viewer.pipeline.loadRlv4Buffer(copy.buffer, label, false);
      syncUiAfterDirectRlv4Load(viewer, snap);
      loadedTelemetry = true;
    }

    if (Array.isArray(bundle.branches)) {
      for (const branch of bundle.branches) {
        viewer.studio.branchStore.upsertBranch(branch);
      }
    }

    if (bundle.playbook) {
      viewer.studio.playbookStore.importDeck(bundle.playbook);
    }

    const hasWorldState =
      bundle.worldState &&
      (bundle.worldState.balls?.length > 0 || bundle.worldState.cars?.length > 0);
    const hasEdits =
      hasWorldState &&
      (!loadedTelemetry ||
        hasStudioWorldStateEdits(viewer, bundle.worldState, bundle.branches?.length ?? 0));

    if (hasWorldState && hasEdits) {
      viewer.pause?.();
      viewer.dispatchStudioCommand({ kind: 'LOAD_WORLD_STATE', state: bundle.worldState });
      viewer.setProfile('studio');
      return { notice: `LOADED STUDIO BUNDLE ${filename}`, switchedToStudio: true };
    }

    if (loadedTelemetry) {
      viewer.seek?.(bundle.worldState?.timestampSeconds ?? 0);
      viewer.play();
      return { notice: `LOADED BUNDLE ${filename}` };
    }

    if (bundle.playbook) {
      return { notice: `LOADED PLAYBOOK FROM ${filename}` };
    }

    throw new Error(
      `Bundle "${filename}" did not contain replay telemetry, playbook, or world state.`,
    );
  }

  // kind === 'playbook' || kind === 'json'
  const text = new TextDecoder().decode(rawBytes);
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(text) as Record<string, unknown>;
  } catch {
    throw new Error(`"${filename}" is not valid JSON.`);
  }

  if (Array.isArray(parsed.slides)) {
    viewer.studio.playbookStore.importDeck(parsed as never);
    return { notice: `LOADED PLAYBOOK ${filename}` };
  }

  if (parsed.telemetry && parsed.metadata) {
    await viewer.loadViewerPayload(parsed as never, filename);
    viewer.play();
    return { notice: `LOADED PAYLOAD ${filename}` };
  }

  if (parsed.format === 'rocketsim_arena_v1' || (parsed.ball !== undefined && Array.isArray(parsed.pads))) {
    viewer.pause?.();
    viewer.studio.importRocketSim(parsed as never);
    viewer.setProfile('studio');
    return { notice: `IMPORTED ROCKETSIM STATE ${filename}`, switchedToStudio: true };
  }

  if (
    parsed.game_info_state !== undefined ||
    parsed.car_states !== undefined ||
    parsed.ball_state !== undefined ||
    parsed.console_commands !== undefined
  ) {
    viewer.pause?.();
    viewer.studio.importRlBot(parsed as never);
    viewer.setProfile('studio');
    return { notice: `IMPORTED RLBOT STATE ${filename}`, switchedToStudio: true };
  }

  if (Array.isArray(parsed.cars) || Array.isArray(parsed.balls)) {
    viewer.pause?.();
    viewer.dispatchStudioCommand({ kind: 'LOAD_WORLD_STATE', state: parsed as never });
    viewer.setProfile('studio');
    return { notice: `LOADED WORLD STATE ${filename}`, switchedToStudio: true };
  }

  throw new Error(
    `Unsupported JSON format in "${filename}". Expected a Playbook deck, ViewerPayload, GameWorldState, RLBot, or RocketSim JSON.`,
  );
}
