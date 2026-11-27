// @vitest-environment happy-dom
import React, { useEffect } from 'react';
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { act } from 'react';
import { cleanup, clickText, flush, mount } from '../../test/dom';
import { HistoricalMatchRecord } from '../../types/api';
import { Modal } from '../common/Modal';
import { HistoricalMatchDetailModal } from '../historical/HistoricalMatchDetailModal';
import { HistoryRow } from '../historical/HistoryRow';
import type { ReplayViewerStageProps } from './ReplayViewerStage';

let drawModeActive = false;

const mockViewerSpies = {
  setProfile: vi.fn(),
  setArenaTheme: vi.fn(),
  setGlassPreset: vi.fn(),
  setHitboxesVisible: vi.fn(),
  setVoronoiEnabled: vi.fn(),
  setQaShowroom: vi.fn(),
  loadReplay: vi.fn(async () => ({})),
  openPalette: vi.fn(),
  openLayouts: vi.fn(),
  openSettings: vi.fn(),
  openHotkeys: vi.fn(),
  openImportExport: vi.fn(),
  setDrawMode: vi.fn((active: boolean) => {
    drawModeActive = active;
  }),
  exportSnapshot: vi.fn(() => ({
    dataUrl: 'data:image/png;base64,AAA',
    width: 1920,
    height: 1080,
    usedComposer: false,
    timestamp: 12,
  })),
  exportBundle: vi.fn(async () => new Uint8Array([1, 2, 3])),
  exportNpz: vi.fn(() => new Uint8Array([4, 5, 6])),
  exportCsvTables: vi.fn(() => ({
    tables: {},
    combined: 'a,b\n1,2',
    metrics74: 'm,v\n1,2',
    json: '{"ok":true}',
  })),
  exportTrainingPackJson: vi.fn(() => '{"shots":[]}'),
  recorderStart: vi.fn(),
  recorderStop: vi.fn(async () => new Blob(['webm'], { type: 'video/webm' })),
  recorderDispose: vi.fn(),
  requestFullscreen: vi.fn(async () => {}),
};

let lastShadowRoot: ShadowRoot | null = null;
let lastStageProps: ReplayViewerStageProps | null = null;

vi.mock('./ReplayViewerStage', () => {
  const MockStage: React.FC<ReplayViewerStageProps> = (props) => {
    lastStageProps = props;

    useEffect(() => {
      const host = document.createElement('div');
      const shadow = host.attachShadow({ mode: 'open' });
      lastShadowRoot = shadow;

      const stageEl = document.createElement('div');
      stageEl.requestFullscreen = mockViewerSpies.requestFullscreen;
      shadow.appendChild(stageEl);

      const canvas = document.createElement('canvas');
      canvas.width = 1280;
      canvas.height = 720;

      const fakeViewer = {
        isDisposed: () => false,
        loadReplay: mockViewerSpies.loadReplay,
        setProfile: mockViewerSpies.setProfile,
        setArenaTheme: mockViewerSpies.setArenaTheme,
        setGlassPreset: mockViewerSpies.setGlassPreset,
        setHitboxesVisible: mockViewerSpies.setHitboxesVisible,
        setVoronoiEnabled: mockViewerSpies.setVoronoiEnabled,
        setQaShowroom: mockViewerSpies.setQaShowroom,
        stage: {
          canvas,
          resize: vi.fn(),
          markRenderDirty: vi.fn(),
          scene: { render: vi.fn() },
        },
        pipeline: {
          store: {},
          getReport: () => ({ summary: {} }),
          getPlaybackState: () => ({ currentTime: 12 }),
        },
        studio: {
          exportSnapshot: mockViewerSpies.exportSnapshot,
          exportBundle: mockViewerSpies.exportBundle,
          exportNpz: mockViewerSpies.exportNpz,
          exportCsvTables: mockViewerSpies.exportCsvTables,
          exportTrainingPackJson: mockViewerSpies.exportTrainingPackJson,
          createMediaRecorder: () => ({
            start: mockViewerSpies.recorderStart,
            stop: mockViewerSpies.recorderStop,
            dispose: mockViewerSpies.recorderDispose,
          }),
        },
        ui: {
          stageEl,
          toolbar: {
            isDrawModeActive: () => drawModeActive,
            setDrawMode: mockViewerSpies.setDrawMode,
          },
          paletteDialog: { open: mockViewerSpies.openPalette },
          layoutDialog: { open: mockViewerSpies.openLayouts },
          settingsDialog: { open: mockViewerSpies.openSettings },
          hotkeyDialog: { open: mockViewerSpies.openHotkeys },
          importExportDialog: { open: mockViewerSpies.openImportExport },
          hotkeys: { getBindings: () => [] },
        },
      };

      props.onViewerReady(fakeViewer as never, shadow, 'rapier');
    }, []);

    useEffect(() => {
      if (props.loadInput) {
        props.onReplayLoaded?.();
      }
    }, [props.loadInput]);

    return (
      <div
        data-testid="mock-replay-stage"
        data-filename={props.loadInput?.filename ?? ''}
        data-profile={props.profile}
        data-theme={props.arenaTheme}
        data-glass={props.glassPreset}
        data-hitboxes={String(props.hitboxesVisible)}
        data-voronoi={String(props.voronoiVisible)}
        data-qa={props.qaShowroom}
        data-theater={String(props.theaterMode)}
      />
    );
  };

  return { default: MockStage };
});

const TEST_MATCH: HistoricalMatchRecord = {
  match_guid: 'replay-guid-42',
  record_start_timestamp: 1700000000,
  map_name: 'DFH Stadium',
  playlist: 11,
  replay_url: '',
  download_status: 'DOWNLOADED',
  local_file_path: 'replays/test.replay',
  upload_status: 'UPLOADED',
  ballchasing_id: 'bc-42',
  ballchasing_url: 'https://ballchasing.com/replay/bc-42',
  retry_count: 0,
  last_error: '',
  blue_score: 3,
  orange_score: 1,
  winner_team: 0,
  replay_stats_json: '',
  created_at: '2026-10-16T12:00:00Z',
  updated_at: '2026-10-16T12:05:00Z',
  stats: {
    match_guid: 'replay-guid-42',
    map_name: 'DFH Stadium',
    playlist: 11,
    blue_score: 3,
    orange_score: 1,
    winner_team: 0,
    duration: 300,
    overtime: false,
    date: '2026-10-16T12:00:00Z',
    players: [
      {
        name: 'DriverA',
        platform: 'Steam',
        online_id: 'steam_a',
        team: 0,
        score: 500,
        goals: 3,
        assists: 0,
        saves: 2,
        shots: 5,
        demos: 1,
        boost: { bpm: 410 },
      },
    ],
  },
};

describe('MatchReplayTab & HistoricalMatchDetailModal 3D Replay integration', () => {
  beforeEach(() => {
    lastShadowRoot = null;
    lastStageProps = null;
    drawModeActive = false;
    Object.values(mockViewerSpies).forEach((fn) => fn.mockClear());
    URL.createObjectURL = vi.fn(() => 'blob:mock');
    URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('keeps replay fetches lazy, wires host controls + SDK sync, and enforces strict 6-layer Escape peeling', async () => {
    const fetchCalls: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      fetchCalls.push(url);
      if (url.endsWith('/replay/status')) {
        return new Response(
          JSON.stringify({
            match_guid: 'replay-guid-42',
            available: true,
            source: 'local',
            local_available: true,
            ballchasing_available: true,
            ballchasing_configured: true,
            ballchasing_id: 'bc-42',
            ballchasing_url: 'https://ballchasing.com/replay/bc-42',
            filename: '2026_11_21_001_2v2_WIN.replay',
            size_bytes: 8,
            download_url: '/api/matches/replay-guid-42/replay',
            download_status: 'DOWNLOADED',
            upload_status: 'UPLOADED',
            reason: '',
            message: '',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      if (url.endsWith('/replay')) {
        return new Response(new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]), {
          status: 200,
          headers: {
            'Content-Length': '8',
            'X-Replay-Source': 'local',
            'X-Replay-Filename': '2026_11_21_001_2v2_WIN.replay',
            'X-Replay-Ballchasing-ID': 'bc-42',
            'X-Replay-Match-GUID': 'replay-guid-42',
          },
        });
      }
      return new Response('{}', { status: 200 });
    });

    const onClose = vi.fn();
    const { container } = await mount(
      <HistoricalMatchDetailModal match={TEST_MATCH} onClose={onClose} />,
    );
    await flush(20);

    // Strictly lazy: zero /replay calls on default Core tab
    expect(fetchCalls.some((u) => u.includes('/replay'))).toBe(false);

    // Open 3D Replay via modal header CTA
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="modal-watch-replay-btn"]')!.click();
    });
    await flush(40);

    expect(fetchCalls).toContain('/api/matches/replay-guid-42/replay/status');
    expect(fetchCalls).toContain('/api/matches/replay-guid-42/replay');
    expect(lastStageProps?.loadInput?.filename).toBe('2026_11_21_001_2v2_WIN.replay');
    expect(container.querySelector('[data-testid="replay-source-badge"]')?.textContent).toBe('LOCAL');
    expect(container.querySelector('[data-testid="replay-physics-badge"]')?.textContent).toBe('RAPIER 120HZ');
    expect(
      container.querySelector<HTMLAnchorElement>('[data-testid="replay-download-link"]')?.getAttribute('href'),
    ).toBe('/api/matches/replay-guid-42/replay?download=1');

    // Host toolbar selects & overlay toggles sync cleanly to stage props
    const profileSel = container.querySelector<HTMLSelectElement>('[data-testid="replay-profile-select"]')!;
    const themeSel = container.querySelector<HTMLSelectElement>('[data-testid="replay-theme-select"]')!;
    const glassSel = container.querySelector<HTMLSelectElement>('[data-testid="replay-glass-select"]')!;
    await act(async () => {
      profileSel.value = 'studio';
      profileSel.dispatchEvent(new Event('change', { bubbles: true }));
      themeSel.value = 'cyber_blueprint';
      themeSel.dispatchEvent(new Event('change', { bubbles: true }));
      glassSel.value = 'tactical';
      glassSel.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-hitboxes-toggle"]')!.click();
      container.querySelector<HTMLButtonElement>('[data-testid="replay-voronoi-toggle"]')!.click();
    });
    expect(lastStageProps?.profile).toBe('studio');
    expect(lastStageProps?.arenaTheme).toBe('cyber_blueprint');
    expect(lastStageProps?.glassPreset).toBe('tactical');
    expect(lastStageProps?.hitboxesVisible).toBe(true);
    expect(lastStageProps?.voronoiVisible).toBe(true);

    // Bi-directional SDK sync (e.g. changing profile/voronoi inside SDK HUD / Ctrl+K) updates host toolbar
    await act(async () => {
      lastStageProps?.onSdkStateSync?.({ profile: 'broadcast', voronoiVisible: false });
    });
    expect(profileSel.value).toBe('broadcast');
    expect(
      container.querySelector<HTMLButtonElement>('[data-testid="replay-voronoi-toggle"]')?.getAttribute('aria-pressed'),
    ).toBe('false');

    // Exports menu -> PNG Snapshot + WebM recording start/stop
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-exports-menu-btn"]')!.click();
    });
    await clickText(container.querySelector('[data-testid="replay-exports-menu"]')!, 'PNG SNAPSHOT');
    expect(mockViewerSpies.exportSnapshot).toHaveBeenCalledTimes(1);

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-exports-menu-btn"]')!.click();
    });
    await clickText(container.querySelector('[data-testid="replay-exports-menu"]')!, 'RECORD WEBM');
    expect(mockViewerSpies.recorderStart).toHaveBeenCalledTimes(1);
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-stop-rec-btn"]')!.click();
    });
    await flush(10);
    expect(mockViewerSpies.recorderStop).toHaveBeenCalledTimes(1);

    // Tools menu -> Command Palette & QA Showroom
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-tools-menu-btn"]')!.click();
    });
    await clickText(container.querySelector('[data-testid="replay-tools-menu"]')!, 'COMMAND PALETTE');
    expect(mockViewerSpies.openPalette).toHaveBeenCalledTimes(1);

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-tools-menu-btn"]')!.click();
    });
    const qaSel = container.querySelector<HTMLSelectElement>('[data-testid="replay-qa-select"]')!;
    await act(async () => {
      qaSel.value = 'cars';
      qaSel.dispatchEvent(new Event('change', { bubbles: true }));
    });
    expect(lastStageProps?.qaShowroom).toBe('cars');

    // Enter Theater Mode
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-theater-toggle"]')!.click();
    });
    expect(container.querySelector('[data-full-viewport="true"]')).not.toBeNull();
    const dialog = container.querySelector('dialog')!;

    // Layer 1: Open EXPORTS dropdown -> Escape closes ONLY dropdown (keeps Theater & Modal open)
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-exports-menu-btn"]')!.click();
    });
    expect(container.querySelector('[data-testid="replay-exports-menu"]')).not.toBeNull();
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(container.querySelector('[data-testid="replay-exports-menu"]')).toBeNull();
    expect(container.querySelector('[data-full-viewport="true"]')).not.toBeNull();
    expect(onClose).not.toHaveBeenCalled();

    // Layer 2: Open SDK modal dialog (.rlv-modal) -> Escape explicitly closes it, keeps Theater & Modal open
    const innerModal = document.createElement('dialog');
    innerModal.className = 'rlv-modal rlv-settings-dialog';
    innerModal.setAttribute('open', '');
    innerModal.close = vi.fn(() => innerModal.removeAttribute('open'));
    lastShadowRoot!.appendChild(innerModal);

    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(innerModal.close).toHaveBeenCalledTimes(1);
    expect(innerModal.hasAttribute('open')).toBe(false);
    expect(container.querySelector('[data-full-viewport="true"]')).not.toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    innerModal.remove();

    // Verify non-modal popover dialogs do NOT hijack Escape!
    const popoverPanel = document.createElement('dialog');
    popoverPanel.className = 'rlv-popover';
    popoverPanel.setAttribute('popover', 'manual');
    popoverPanel.setAttribute('open', '');
    lastShadowRoot!.appendChild(popoverPanel);

    // Layer 3: Active Telestrator Draw Mode -> Escape exits Draw Mode, keeps Theater & Modal open
    drawModeActive = true;
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(mockViewerSpies.setDrawMode).toHaveBeenCalledWith(false);
    expect(container.querySelector('[data-full-viewport="true"]')).not.toBeNull();
    expect(onClose).not.toHaveBeenCalled();

    // Layer 5: Theater Mode active -> Escape exits Theater Mode; immediate native `cancel` must NOT close Modal!
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      dialog.dispatchEvent(new Event('cancel', { cancelable: true }));
    });
    expect(onClose).not.toHaveBeenCalled();
    expect(container.querySelector('[data-full-viewport="false"]')).not.toBeNull();
    popoverPanel.remove();

    // Layer 6: Inline mode with no open sub-layers -> Escape closes outer Modal
    await act(async () => {
      dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('auto-searches Ballchasing (?lookup=true), hides stale badges on error, and loads local .rlbundle from error state', async () => {
    const fetchCalls: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      fetchCalls.push(url);
      if (url.endsWith('/replay/status')) {
        return new Response(
          JSON.stringify({
            match_guid: 'replay-guid-42',
            available: false,
            source: 'unavailable',
            local_available: false,
            ballchasing_available: false,
            ballchasing_configured: true,
            ballchasing_id: '',
            ballchasing_url: '',
            filename: '',
            size_bytes: 0,
            download_url: '/api/matches/replay-guid-42/replay',
            download_status: 'PENDING',
            upload_status: 'PENDING',
            reason: 'no_replay_source',
            message: 'No cached replay yet',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      if (url.endsWith('/replay/status?lookup=true')) {
        return new Response(
          JSON.stringify({
            error: 'Ballchasing rate limit active',
            code: 'ballchasing_rate_limited',
            retry_after_seconds: 9,
          }),
          { status: 429, headers: { 'Content-Type': 'application/json', 'Retry-After': '9' } },
        );
      }
      return new Response('{}', { status: 200 });
    });

    const { container } = await mount(
      <HistoricalMatchDetailModal match={TEST_MATCH} initialTab="replay" onClose={vi.fn()} />,
    );
    await flush(40);

    // Seamless auto-lookup ran automatically without requiring a manual click
    expect(fetchCalls).toContain('/api/matches/replay-guid-42/replay/status');
    expect(fetchCalls).toContain('/api/matches/replay-guid-42/replay/status?lookup=true');
    expect(container.querySelector('[data-testid="replay-error-state"]')).not.toBeNull();
    expect(container.textContent).toContain('BALLCHASING RATE LIMIT REACHED');
    expect(container.textContent).toContain('9s');
    // Stale PHYSICS… & source badges hidden before stage mount
    expect(container.querySelector('[data-testid="replay-physics-badge"]')).toBeNull();
    expect(container.querySelector('[data-testid="replay-source-badge"]')).toBeNull();

    // Selecting a local .rlbundle file from the error screen mounts the stage directly
    const input = container.querySelector<HTMLInputElement>('[data-testid="replay-local-file-input"]')!;
    const bundleFile = new File([new Uint8Array([80, 75, 3, 4])], 'session_clip.rlbundle', {
      type: 'application/zip',
    });
    Object.defineProperty(input, 'files', { value: [bundleFile], configurable: true });
    await act(async () => {
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await flush(30);

    expect(container.querySelector('[data-testid="mock-replay-stage"]')).not.toBeNull();
    expect(lastStageProps?.loadInput?.file?.name).toBe('session_clip.rlbundle');
    expect(container.querySelector('[data-testid="replay-source-badge"]')?.textContent).toBe('LOCAL FILE');
  });

  it('exposes always-clickable ▶ 3D button on HistoryRow even when Ballchasing stats have 0 players', async () => {
    const onWatchReplay = vi.fn();
    const localNoStats: HistoricalMatchRecord = {
      ...TEST_MATCH,
      match_guid: 'local-nostats-1',
      upload_status: 'PENDING',
      ballchasing_id: '',
      ballchasing_url: '',
      stats: { ...TEST_MATCH.stats!, players: [] },
    };

    const { container } = await mount(
      <HistoryRow
        match={localNoStats}
        localIds={new Set()}
        open={false}
        onToggle={vi.fn()}
        onDetail={vi.fn()}
        onWatchReplay={onWatchReplay}
      />,
    );

    const replayBtn = container.querySelector<HTMLButtonElement>(
      '[data-testid="history-row-watch-replay-btn"]',
    )!;
    expect(replayBtn).not.toBeNull();
    await act(async () => {
      replayBtn.click();
    });
    expect(onWatchReplay).toHaveBeenCalledTimes(1);
  });

  it('peels strictly one layer per Escape when HistoricalMatchDetailModal is nested inside an outer Modal (e.g. PlayerProfileDialog)', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/replay/status')) {
        return new Response(
          JSON.stringify({
            match_guid: 'replay-guid-42',
            available: true,
            source: 'local',
            local_available: true,
            ballchasing_available: false,
            ballchasing_configured: false,
            ballchasing_id: '',
            ballchasing_url: '',
            filename: 'nested.replay',
            size_bytes: 4,
            download_url: '/api/matches/replay-guid-42/replay',
            download_status: 'DOWNLOADED',
            upload_status: 'PENDING',
            reason: '',
            message: '',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      return new Response(new Uint8Array([1, 2, 3, 4]), {
        status: 200,
        headers: {
          'Content-Length': '4',
          'X-Replay-Source': 'local',
          'X-Replay-Filename': 'nested.replay',
        },
      });
    });

    const onOuterClose = vi.fn();
    const NestedHarness: React.FC = () => {
      const [innerOpen, setInnerOpen] = React.useState(true);
      return (
        <Modal isOpen onClose={onOuterClose} title="PLAYER PROFILE">
          {innerOpen && (
            <HistoricalMatchDetailModal
              match={TEST_MATCH}
              initialTab="replay"
              onClose={() => setInnerOpen(false)}
            />
          )}
        </Modal>
      );
    };

    const { container } = await mount(<NestedHarness />);
    await flush(40);

    const dialogs = container.querySelectorAll('dialog.broadcast-modal');
    expect(dialogs).toHaveLength(2);
    const outerDialog = dialogs[0];
    const innerDialog = dialogs[1];

    // Enter Theater Mode & open an internal SDK modal dialog (.rlv-modal)
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="replay-theater-toggle"]')!.click();
    });
    const innerSdkModal = document.createElement('dialog');
    innerSdkModal.className = 'rlv-modal';
    innerSdkModal.setAttribute('open', '');
    innerSdkModal.close = vi.fn(() => innerSdkModal.removeAttribute('open'));
    lastShadowRoot!.appendChild(innerSdkModal);

    // Escape #1: closes ONLY inner SDK modal (both broadcast modals & Theater stay open!)
    await act(async () => {
      innerDialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(innerSdkModal.close).toHaveBeenCalledTimes(1);
    expect(onOuterClose).not.toHaveBeenCalled();
    expect(container.querySelectorAll('dialog.broadcast-modal')).toHaveLength(2);
    expect(container.querySelector('[data-full-viewport="true"]')).not.toBeNull();
    innerSdkModal.remove();

    // Escape #2: exits ONLY Theater Mode (both broadcast modals stay open!)
    await act(async () => {
      innerDialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onOuterClose).not.toHaveBeenCalled();
    expect(container.querySelectorAll('dialog.broadcast-modal')).toHaveLength(2);
    expect(container.querySelector('[data-full-viewport="false"]')).not.toBeNull();

    // Escape #3: closes ONLY inner HistoricalMatchDetailModal (outer PlayerProfileDialog stays open!)
    await act(async () => {
      innerDialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onOuterClose).not.toHaveBeenCalled();
    expect(container.querySelectorAll('dialog.broadcast-modal')).toHaveLength(1);

    // Escape #4: now closes outer Modal
    await act(async () => {
      outerDialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(onOuterClose).toHaveBeenCalledTimes(1);
  });

  it('syncs Host Toolbar badge to LOCAL FILE and clears match download link when SDK HUD Load replaces replay', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/replay/status')) {
        return new Response(
          JSON.stringify({
            match_guid: 'replay-guid-42',
            available: true,
            source: 'local',
            local_available: true,
            ballchasing_available: false,
            ballchasing_configured: false,
            ballchasing_id: '',
            ballchasing_url: '',
            filename: 'original.replay',
            size_bytes: 4,
            download_url: '/api/matches/replay-guid-42/replay',
            download_status: 'DOWNLOADED',
            upload_status: 'PENDING',
            reason: '',
            message: '',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      return new Response(new Uint8Array([1, 2, 3, 4]), {
        status: 200,
        headers: {
          'Content-Length': '4',
          'X-Replay-Source': 'local',
          'X-Replay-Filename': 'original.replay',
        },
      });
    });

    const { container } = await mount(
      <HistoricalMatchDetailModal match={TEST_MATCH} initialTab="replay" onClose={vi.fn()} />,
    );
    await flush(40);

    expect(container.querySelector('[data-testid="replay-source-badge"]')?.textContent).toBe('LOCAL');
    expect(container.querySelector('[data-testid="replay-download-link"]')).not.toBeNull();

    // Simulate SDK HUD "Load" / canvas dropzone loading a different local file
    await act(async () => {
      lastStageProps?.onExternalLocalLoad?.('other_local.replay');
    });

    expect(container.querySelector('[data-testid="replay-source-badge"]')?.textContent).toBe('LOCAL FILE');
    expect(container.querySelector('[data-testid="replay-download-link"]')).toBeNull();
  });

  it('routes SDK HUD Load / active-stage drop files through unified host loader & clears stale download link', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/replay/status')) {
        return new Response(
          JSON.stringify({
            match_guid: 'replay-guid-42',
            available: true,
            source: 'local',
            local_available: true,
            ballchasing_available: false,
            ballchasing_configured: false,
            ballchasing_id: '',
            ballchasing_url: '',
            filename: 'original.replay',
            size_bytes: 4,
            download_url: '/api/matches/replay-guid-42/replay',
            download_status: 'DOWNLOADED',
            upload_status: 'PENDING',
            reason: '',
            message: '',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      return new Response(new Uint8Array([1, 2, 3, 4]), {
        status: 200,
        headers: {
          'Content-Length': '4',
          'X-Replay-Source': 'local',
          'X-Replay-Filename': 'original.replay',
        },
      });
    });

    const { container } = await mount(
      <HistoricalMatchDetailModal match={TEST_MATCH} initialTab="replay" onClose={vi.fn()} />,
    );
    await flush(40);
    expect(container.querySelector('[data-testid="replay-download-link"]')).not.toBeNull();

    const studioBundle = new File([new Uint8Array([80, 75, 3, 4])], 'dropped_studio.rlbundle');
    await act(async () => {
      lastStageProps?.onExternalFilesSelected?.([studioBundle]);
    });
    await flush(30);

    expect(lastStageProps?.loadInput?.file?.name).toBe('dropped_studio.rlbundle');
    expect(container.querySelector('[data-testid="replay-source-badge"]')?.textContent).toBe('LOCAL FILE');
    expect(container.querySelector('[data-testid="replay-download-link"]')).toBeNull();
  });

  it('ignores stale ReplayApiError(aborted) from a superseded loadMatchReplay call without showing DOWNLOAD CANCELLED', async () => {
    let firstReject!: (err: unknown) => void;
    let callNum = 0;

    vi.spyOn(globalThis, 'fetch').mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/replay/status')) {
        callNum += 1;
        if (callNum === 1) {
          return new Promise<Response>((_resolve, reject) => {
            firstReject = reject;
            init?.signal?.addEventListener('abort', () => {
              // Simulate late abort rejection after a new load takes over
              setTimeout(() => {
                const abortErr = new Error('The operation was aborted');
                abortErr.name = 'AbortError';
                firstReject(abortErr);
              }, 10);
            });
          });
        }
      }
      return Promise.resolve(
        new Response(new Uint8Array([1, 2, 3, 4]), {
          status: 200,
          headers: { 'Content-Length': '4', 'X-Replay-Source': 'local' },
        }),
      );
    });

    const { container } = await mount(
      <HistoricalMatchDetailModal match={TEST_MATCH} initialTab="replay" onClose={vi.fn()} />,
    );
    await flush(10);

    // Supersede the first pending status request by loading a local file via hidden input
    const input = container.querySelector<HTMLInputElement>('[data-testid="replay-local-file-input"]')!;
    const localFile = new File([new Uint8Array([1, 2, 3])], 'override.replay');
    Object.defineProperty(input, 'files', { value: [localFile], configurable: true });
    await act(async () => {
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });

    // Wait past the late AbortError timer from call #1
    await flush(40);

    expect(container.querySelector('[data-testid="replay-error-state"]')).toBeNull();
    expect(container.querySelector('[data-testid="replay-source-badge"]')?.textContent).toBe('LOCAL FILE');
    expect(lastStageProps?.loadInput?.file?.name).toBe('override.replay');
  });
});
