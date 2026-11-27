// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest';
import type { DockRegion, RLReplayViewer } from '@rl-replay/viewer-sdk';
import {
  containPopoverRequests,
  hasStudioWorldStateEdits,
  installExternalLoadObserver,
  installProfileSyncWrappers,
  installStudioSelectionAutoInspector,
  installUnifiedDropzoneBridge,
  loadTypedFileIntoViewer,
  redockPopoverPanels,
} from './ReplayViewerStage';

function createFakeViewer(opts?: { projectedBallZ?: number }) {
  const dockWindowAt = vi.fn();
  const visiblePanels = new Set<string>(['entity-outliner', 'ball-inspector']);
  const setPanelVisible = vi.fn((id: string, visible: boolean) => {
    if (visible) visiblePanels.add(id);
    else visiblePanels.delete(id);
  });
  const isWindowVisible = vi.fn((id: string) => visiblePanels.has(id));
  const loadReplay = vi.fn(async () => ({}));
  const loadViewerPayload = vi.fn(async () => ({}));
  const loadRlv4Buffer = vi.fn(async () => ({
    metrics: {},
    insights: [],
    pvaMoments: [],
  }));
  const toViewerPayload = vi.fn(() => ({ metadata: {}, telemetry: {} }));
  const unpackBundle = vi.fn();
  const importDeck = vi.fn();
  const upsertBranch = vi.fn();
  const dispatchStudioCommand = vi.fn();
  const importRlBot = vi.fn();
  const importRocketSim = vi.fn();
  const setProfile = vi.fn();
  const play = vi.fn();
  const pause = vi.fn();
  const seek = vi.fn();
  const syncWorld = vi.fn();
  const syncTelemetry = vi.fn();
  const setSeriesMatches = vi.fn();

  const ballZ = opts?.projectedBallZ ?? 92.75;
  const projectReplayFrame = vi.fn(() => ({
    timestampSeconds: 0,
    sourceMode: 'replay',
    balls: [{ id: 'ball-0', pos: [0, 0, ballZ] }],
    cars: [{ id: 'car-0', pos: [0, -1000, 17] }],
  }));

  let outlinerDock: DockRegion = 'left';
  const outlinerSetDockSpy = vi.fn((d: DockRegion) => {
    outlinerDock = d;
  });
  const outlinerWin = {
    config: { id: 'entity-outliner' },
    getState: () => ({ id: 'entity-outliner', dock: outlinerDock, visible: true }),
    setDock: outlinerSetDockSpy,
  };

  const viewer = {
    isDisposed: () => false,
    loadReplay,
    loadViewerPayload,
    dispatchStudioCommand,
    setProfile,
    play,
    pause,
    seek,
    stage: {
      entities: { clearTransientVfx: vi.fn() },
      cameraDirector: { requestSnap: vi.fn() },
    },
    pipeline: {
      store: {
        frameCount: 2,
        times: new Float32Array([0, 30]),
        gameState: new Uint8Array(16),
        durationSeconds: 30,
        meta: { events: [], players: [] },
        getPlaylist: () => [],
        getActiveMatch: () => null,
      },
      getPacing: () => null,
      getMetrics: () => ({}),
      getInsights: () => [],
      getPvaMoments: () => [],
      loadReplay,
      loadRlv4Buffer,
      loadViewerPayload,
      toViewerPayload,
    },
    studio: {
      unpackBundle,
      importRlBot,
      importRocketSim,
      projectReplayFrame,
      playbookStore: { importDeck },
      branchStore: { upsertBranch },
    },
    ui: {
      toolbar: { setDrawMode: vi.fn() },
      kickoffOverlay: { reset: vi.fn() },
      hudBar: { setSeriesMatches },
      scrubber: {
        setClockMap: vi.fn(),
        setDeadTimeSpans: vi.fn(),
        markers: { setFromReplayEvents: vi.fn() },
        renderEventTicks: vi.fn(),
      },
      panels: {
        syncWorld,
        syncTelemetry,
      },
      workspace: {
        dockWindowAt,
        setPanelVisible,
        isWindowVisible,
        getAllWindows: () => [outlinerWin],
        getSnapshot: () => ({
          panels: {
            'entity-outliner': { dock: 'popover', visible: true },
            'clock-mutators': { dock: 'popover', visible: true },
            'ball-inspector': { dock: 'popover', visible: true },
            'car-kinematics': { dock: 'popover', visible: false },
            'stats-sidebar': { dock: 'right', visible: true },
          },
        }),
      },
      applyStoredProfile: vi.fn(),
      dropzone: {
        processFileList: vi.fn(),
      },
    },
  } as unknown as RLReplayViewer;

  return {
    viewer,
    outlinerWin,
    spies: {
      dockWindowAt,
      setPanelVisible,
      isWindowVisible,
      outlinerSetDockSpy,
      loadReplay,
      loadViewerPayload,
      loadRlv4Buffer,
      toViewerPayload,
      unpackBundle,
      importDeck,
      upsertBranch,
      dispatchStudioCommand,
      importRlBot,
      importRocketSim,
      setProfile,
      play,
      pause,
      seek,
      syncTelemetry,
      setSeriesMatches,
    },
  };
}

describe('ReplayViewerStage helpers', () => {
  it('re-docks all popover panels into left/right rails (including clock-mutators on left) without touching rail-docked panels', () => {
    const { viewer, spies } = createFakeViewer();
    const moved = redockPopoverPanels(viewer);
    expect(moved).toBe(4);
    expect(spies.dockWindowAt).toHaveBeenCalledWith('entity-outliner', 'left');
    expect(spies.dockWindowAt).toHaveBeenCalledWith('clock-mutators', 'left');
    expect(spies.dockWindowAt).toHaveBeenCalledWith('ball-inspector', 'right');
    expect(spies.dockWindowAt).toHaveBeenCalledWith('car-kinematics', 'right');
    expect(spies.dockWindowAt).not.toHaveBeenCalledWith('stats-sidebar', expect.anything());
  });

  it('contains panel Pop requests inside stage bounds by toggling float <-> rail instead of popover', () => {
    const { viewer, outlinerWin, spies } = createFakeViewer();
    containPopoverRequests(viewer);

    outlinerWin.setDock('popover');
    expect(spies.outlinerSetDockSpy).toHaveBeenLastCalledWith('float');

    outlinerWin.setDock('popover');
    expect(spies.outlinerSetDockSpy).toHaveBeenLastCalledWith('left');
  });

  it('auto-swaps ball-inspector <-> car-kinematics on Studio entity selection while docked in rails', () => {
    const { viewer, spies } = createFakeViewer();
    installStudioSelectionAutoInspector(viewer, () => 'studio');

    // Selecting a car switches right rail from ball-inspector -> car-kinematics
    viewer.ui.panels.syncWorld(
      {} as never,
      { kind: 'car', id: 'car-0' },
      [],
      'main',
    );
    expect(spies.setPanelVisible).toHaveBeenCalledWith('ball-inspector', false);
    expect(spies.setPanelVisible).toHaveBeenCalledWith('car-kinematics', true);
    expect(spies.dockWindowAt).toHaveBeenCalledWith('car-kinematics', 'right');

    // Selecting the ball switches back from car-kinematics -> ball-inspector
    viewer.ui.panels.syncWorld(
      {} as never,
      { kind: 'ball', id: 'ball-0' },
      [],
      'main',
    );
    expect(spies.setPanelVisible).toHaveBeenCalledWith('car-kinematics', false);
    expect(spies.setPanelVisible).toHaveBeenCalledWith('ball-inspector', true);
    expect(spies.dockWindowAt).toHaveBeenCalledWith('ball-inspector', 'right');
  });

  it('passes a named File to viewer.loadReplay for raw .replay bytes', async () => {
    const { viewer, spies } = createFakeViewer();
    await loadTypedFileIntoViewer(viewer, {
      bytes: new Uint8Array([1, 2, 3, 4]),
      filename: '2026_11_21_001_2v2_WIN.replay',
    });

    expect(spies.loadReplay).toHaveBeenCalledTimes(1);
    const [argFile, append] = spies.loadReplay.mock.calls[0] as unknown as [File, boolean];
    expect(argFile).toBeInstanceOf(File);
    expect(argFile.name).toBe('2026_11_21_001_2v2_WIN.replay');
    expect(append).toBe(false);
    expect(spies.play).toHaveBeenCalledTimes(1);
  });

  it('loads plain .rlbundle via direct RLV4 without lossy double-conversion and restores playbook & branches', async () => {
    const { viewer, spies } = createFakeViewer({ projectedBallZ: 92.75 });
    spies.unpackBundle.mockResolvedValueOnce({
      manifest: { match_guid: 'bundle-match-7' },
      telemetryRlv4: new Uint8Array([10, 20, 30]),
      playbook: { slides: [{ id: 's1' }] },
      branches: [{ id: 'main', label: 'Main' }],
      worldState: {
        timestampSeconds: 15,
        sourceMode: 'replay',
        balls: [{ id: 'ball-0', pos: [0, 0, 92.75] }],
        cars: [{ id: 'car-0', pos: [0, -1000, 17] }],
      },
    });

    const bundleFile = new File([new Uint8Array([80, 75, 3, 4])], 'clip.rlbundle');
    const result = await loadTypedFileIntoViewer(viewer, {
      file: bundleFile,
      filename: 'clip.rlbundle',
    });

    expect(spies.loadReplay).not.toHaveBeenCalled();
    expect(spies.toViewerPayload).not.toHaveBeenCalled();
    expect(spies.loadViewerPayload).not.toHaveBeenCalled();
    expect(spies.loadRlv4Buffer).toHaveBeenCalledTimes(1);
    expect(spies.upsertBranch).toHaveBeenCalledWith({ id: 'main', label: 'Main' });
    expect(spies.importDeck).toHaveBeenCalledTimes(1);
    expect(spies.seek).toHaveBeenCalledWith(15);
    expect(spies.play).toHaveBeenCalledTimes(1);
    expect(result.notice).toContain('clip.rlbundle');
  });

  it('preserves modified Studio worldState (e.g. Ball Z = 777), branches, and playbook on .rlbundle round-trip without clobbering via play()', async () => {
    const { viewer, spies } = createFakeViewer({ projectedBallZ: 92.75 });
    const editedWorld = {
      timestampSeconds: 12,
      sourceMode: 'replay',
      balls: [{ id: 'ball-0', pos: [0, 0, 777] }],
      cars: [{ id: 'car-0', pos: [0, -1000, 17] }],
    };
    spies.unpackBundle.mockResolvedValueOnce({
      manifest: { match_guid: 'studio-bundle-1' },
      telemetryRlv4: new Uint8Array([10, 20, 30]),
      playbook: { slides: [{ id: 'slide-1', title: 'Ball High' }] },
      branches: [
        { id: 'main', label: 'Main' },
        { id: 'fork-1', label: 'Alt Kickoff' },
      ],
      worldState: editedWorld,
    });

    const bundleFile = new File([new Uint8Array([80, 75])], 'edited_studio.rlbundle');
    const result = await loadTypedFileIntoViewer(viewer, {
      file: bundleFile,
      filename: 'edited_studio.rlbundle',
    });

    expect(spies.loadRlv4Buffer).toHaveBeenCalledTimes(1);
    expect(spies.upsertBranch).toHaveBeenCalledTimes(2);
    expect(spies.importDeck).toHaveBeenCalledTimes(1);
    expect(spies.dispatchStudioCommand).toHaveBeenCalledWith({
      kind: 'LOAD_WORLD_STATE',
      state: editedWorld,
    });
    expect(spies.pause).toHaveBeenCalledTimes(1);
    expect(spies.play).not.toHaveBeenCalled();
    expect(spies.setProfile).toHaveBeenCalledWith('studio');
    expect(result.switchedToStudio).toBe(true);
  });

  it('notifies host toolbar on external SDK HUD Load / dropzone even when appendSeries=true and bytes are Uint8Array', async () => {
    const { viewer } = createFakeViewer();
    let hostLoading = false;
    const onExternalLocalLoad = vi.fn();

    // Simulate real SDK pipeline.loadReplay delegating to pipeline.loadRlv4Buffer
    viewer.pipeline.loadReplay = vi.fn(async (_source, appendSeries) => {
      return viewer.pipeline.loadRlv4Buffer(new ArrayBuffer(8), 'replay', appendSeries);
    });
    viewer.pipeline.store.getActiveMatch = () => ({ id: 'm2', label: 'sample.replay' } as never);

    installExternalLoadObserver(viewer, () => hostLoading, onExternalLocalLoad);

    // 1. External SDK HUD Load while a match is already loaded (Uint8Array + appendSeries=true)
    await viewer.pipeline.loadReplay(new Uint8Array([1, 2, 3, 4]), true);
    expect(onExternalLocalLoad).toHaveBeenCalledTimes(1);
    expect(onExternalLocalLoad).toHaveBeenLastCalledWith('sample.replay');

    // 2. Fallback to "Local Replay" when active match label is also generic
    viewer.pipeline.store.getActiveMatch = () => ({ id: 'm3', label: 'replay' } as never);
    await viewer.pipeline.loadReplay(new Uint8Array([5, 6, 7, 8]), true);
    expect(onExternalLocalLoad).toHaveBeenCalledTimes(2);
    expect(onExternalLocalLoad).toHaveBeenLastCalledWith('Local Replay');

    // 3. Host-initiated load (initial load or TOOLS -> APPEND TO SERIES) is suppressed
    hostLoading = true;
    await viewer.pipeline.loadReplay(new File([new Uint8Array([9])], 'host_append.replay'), true);
    expect(onExternalLocalLoad).toHaveBeenCalledTimes(2);
  });

  it('delegates Layout Manager applyStoredProfile("studio") directly to viewer.setProfile("studio") so full Studio mode activates', () => {
    const { viewer, spies } = createFakeViewer();
    const origApplyStored = viewer.ui.applyStoredProfile as ReturnType<typeof vi.fn>;
    let lastProfile: 'broadcast' | 'coach' | 'studio' | 'minimal_canvas' = 'coach';
    const onSyncProfile = vi.fn();

    installProfileSyncWrappers(
      viewer,
      () => lastProfile,
      (p) => {
        lastProfile = p;
      },
      onSyncProfile,
    );

    // Selecting built-in "studio" via LayoutManagerDialog delegates to viewer.setProfile("studio")
    viewer.ui.applyStoredProfile('studio');
    expect(spies.setProfile).toHaveBeenCalledWith('studio');
    expect(origApplyStored).not.toHaveBeenCalled();
    expect(onSyncProfile).toHaveBeenCalledWith('studio');
    expect(lastProfile).toBe('studio');

    // Selecting a custom user-saved layout falls through to origApplyStored + redocks popovers
    viewer.ui.applyStoredProfile('custom-scrim-layout');
    expect(origApplyStored).toHaveBeenCalledWith('custom-scrim-layout');
  });

  it('unifies SDK ViewportDropzone file picks & drops across .replay, .rlbundle, and .json while rejecting unsupported extensions', () => {
    const { viewer } = createFakeViewer();
    const onAccepted = vi.fn();
    const onRejected = vi.fn();

    installUnifiedDropzoneBridge(viewer, onAccepted, onRejected);

    const bundleFile = new File([new Uint8Array([1])], 'studio.rlbundle');
    const jsonFile = new File([new Uint8Array([2])], 'world.json');
    const badFile = new File([new Uint8Array([3])], 'notes.txt');

    const dz = viewer.ui.dropzone as unknown as { processFileList: (files: File[]) => void };
    dz.processFileList([bundleFile, jsonFile, badFile]);

    expect(onAccepted).toHaveBeenCalledTimes(1);
    expect(onAccepted).toHaveBeenCalledWith([bundleFile, jsonFile]);
    expect(onRejected).toHaveBeenCalledWith('notes.txt');
  });

  it('detects Studio edits to velocity, car boost, boost pads, and clock/score even when positions match', () => {
    const { viewer } = createFakeViewer({ projectedBallZ: 92.75 });
    const baseState = {
      timestampSeconds: 10,
      sourceMode: 'replay',
      balls: [{ id: 'ball-0', pos: [0, 0, 92.75], vel: [0, 0, 0], rot: [0, 0, 0, 1], angVel: [0, 0, 0] }],
      cars: [{ id: 'car-0', pos: [0, -1000, 17], vel: [0, 0, 0], rot: [0, 0, 0, 1], angVel: [0, 0, 0], boostAmount: 33 }],
      boostPads: [{ padIndex: 0, pos: [0, 0, 0], isBig: true, isActive: true, cooldownRemaining: 0 }],
      clockAndScore: { phase: 'active', clockSeconds: 290, countdownNumber: 0, isOvertime: false, blueScore: 1, orangeScore: 0, unlimitedTime: false },
    };
    viewer.studio.projectReplayFrame = vi.fn(() => structuredClone(baseState)) as never;

    expect(hasStudioWorldStateEdits(viewer, baseState as never, 1)).toBe(false);

    // Ball velocity edit
    const velEdited = structuredClone(baseState);
    velEdited.balls[0].vel = [1200, 0, 400];
    expect(hasStudioWorldStateEdits(viewer, velEdited as never, 1)).toBe(true);

    // Car boost edit
    const boostEdited = structuredClone(baseState);
    boostEdited.cars[0].boostAmount = 100;
    expect(hasStudioWorldStateEdits(viewer, boostEdited as never, 1)).toBe(true);

    // Boost pad toggle edit
    const padEdited = structuredClone(baseState);
    padEdited.boostPads[0].isActive = false;
    expect(hasStudioWorldStateEdits(viewer, padEdited as never, 1)).toBe(true);

    // Score/clock edit
    const scoreEdited = structuredClone(baseState);
    scoreEdited.clockAndScore.blueScore = 5;
    expect(hasStudioWorldStateEdits(viewer, scoreEdited as never, 1)).toBe(true);
  });

  it('routes RocketSim Arena JSON (with cars array) to importRocketSim instead of LOAD_WORLD_STATE', async () => {
    const { viewer, spies } = createFakeViewer();
    const rocketSimPayload = {
      format: 'rocketsim_arena_v1',
      game_mode: 'SOCCAR',
      tick_count: 120,
      tick_rate: 120,
      ball: { pos: [0, 0, 100], vel: [10, 0, 0], ang_vel: [0, 0, 0], rot_mat: [1, 0, 0, 0, 1, 0, 0, 0, 1] },
      cars: [{ id: 1, name: 'car-0', team: 0, pos: [0, -1000, 17], boost: 33 }],
      pads: [{ pos: [0, 0, 0], is_big: true, is_active: true, cooldown: 0 }],
    };
    const bytes = new TextEncoder().encode(JSON.stringify(rocketSimPayload));

    const result = await loadTypedFileIntoViewer(viewer, {
      bytes,
      filename: 'rocketsim_snapshot.json',
    });

    expect(spies.importRocketSim).toHaveBeenCalledTimes(1);
    expect(spies.importRocketSim).toHaveBeenCalledWith(rocketSimPayload);
    expect(spies.dispatchStudioCommand).not.toHaveBeenCalled();
    expect(spies.pause).toHaveBeenCalledTimes(1);
    expect(spies.setProfile).toHaveBeenCalledWith('studio');
    expect(result.switchedToStudio).toBe(true);
  });

  it('routes RLBot DesiredGameState JSON with { car_states, game_info_state } (no ball_state) to importRlBot', async () => {
    const { viewer, spies } = createFakeViewer();
    const rlbotPayload = {
      car_states: { '0': { physics: { location: { x: 0, y: -1500, z: 17 } }, boost_amount: 100 } },
      game_info_state: { world_gravity_z: -650, game_speed: 1 },
    };
    const bytes = new TextEncoder().encode(JSON.stringify(rlbotPayload));

    const result = await loadTypedFileIntoViewer(viewer, {
      bytes,
      filename: 'test_rlbot_cars_only.json',
    });

    expect(spies.importRlBot).toHaveBeenCalledTimes(1);
    expect(spies.importRlBot).toHaveBeenCalledWith(rlbotPayload);
    expect(spies.dispatchStudioCommand).not.toHaveBeenCalled();
    expect(spies.pause).toHaveBeenCalledTimes(1);
    expect(spies.setProfile).toHaveBeenCalledWith('studio');
    expect(result.switchedToStudio).toBe(true);
  });
});
