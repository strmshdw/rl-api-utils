import React, {
  Component,
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useRef,
  useState,
} from 'react';
import type {
  ArenaThemeId,
  GlassPresetId,
  RLReplayViewer,
  RLViewerProfile,
} from '@rl-replay/viewer-sdk';
import { HistoricalMatchRecord } from '../../types/api';
import {
  ReplayApiError,
  ReplayErrorGuidance,
  ReplayFetchProgress,
  ReplaySource,
  ReplayStatusResponse,
  buildReplayFileUrl,
  describeReplayError,
  describeUnavailableStatus,
  fetchReplayBinary,
  fetchReplayStatus,
  formatBytes,
} from './replayApi';
import {
  ActiveWebmRecording,
  exportViewerAnalyticsCsv,
  exportViewerAnalyticsJson,
  exportViewerNpz,
  exportViewerPngSnapshot,
  exportViewerRlBundle,
  exportViewerTrainingPack,
  startViewerWebmRecording,
  stopViewerWebmRecording,
} from './replayExporters';
import {
  QaShowroomPreset,
  ReplayExportAction,
  ReplayToolAction,
  ReplayViewerToolbar,
} from './ReplayViewerToolbar';
import type { ReplayLoadInput, SdkSyncedState } from './ReplayViewerStage';

const LazyReplayViewerStage = lazy(() => import('./ReplayViewerStage'));

interface MatchReplayTabProps {
  match: HistoricalMatchRecord;
  matchTitle: string;
  theaterMode: boolean;
  onToggleTheaterMode: () => void;
  onRegisterEscapeGuard?: (guard: ((event: Event) => boolean) | null) => void;
}

type ReplayLoadPhase =
  | 'checking'
  | 'searching_ballchasing'
  | 'downloading'
  | 'parsing'
  | 'ready'
  | 'unavailable'
  | 'error';

class ViewerErrorBoundary extends Component<
  { onError?: (err: Error) => void; children: React.ReactNode },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error): { error: Error } {
    return { error };
  }

  componentDidCatch(error: Error): void {
    this.props.onError?.(error);
  }

  render(): React.ReactNode {
    if (this.state.error) {
      return (
        <div
          data-testid="replay-webgl-error"
          className="flex flex-col items-center justify-center gap-3 p-8 text-center bg-black text-ink h-full min-h-[380px]"
        >
          <span className="text-loss font-extrabold text-base uppercase tracking-wider">
            3D VIEWER RENDER ERROR
          </span>
          <p className="text-sm text-muted max-w-md font-sans">
            {this.state.error.message || 'WebGL2 context could not be initialized.'}
          </p>
        </div>
      );
    }
    return this.props.children;
  }
}

export const MatchReplayTab: React.FC<MatchReplayTabProps> = ({
  match,
  matchTitle,
  theaterMode,
  onToggleTheaterMode,
  onRegisterEscapeGuard,
}) => {
  const [phase, setPhase] = useState<ReplayLoadPhase>('checking');
  const [statusInfo, setStatusInfo] = useState<ReplayStatusResponse | null>(null);
  const [progress, setProgress] = useState<ReplayFetchProgress | null>(null);
  const [guidance, setGuidance] = useState<ReplayErrorGuidance | null>(null);
  const [loadInput, setLoadInput] = useState<ReplayLoadInput | null>(null);
  const [resolvedSource, setResolvedSource] = useState<ReplaySource | 'local_file' | null>(null);
  const [resolvedFilename, setResolvedFilename] = useState<string>('');
  const [serverBacked, setServerBacked] = useState(false);

  // Host controls state (100% SDK feature access, kept bi-directionally synced with SDK HUD)
  const [viewerReady, setViewerReady] = useState(false);
  const [physicsEngine, setPhysicsEngine] = useState<'rapier' | 'builtin' | 'initializing'>(
    'initializing',
  );
  const [profile, setProfile] = useState<RLViewerProfile>('coach');
  const [arenaTheme, setArenaTheme] = useState<ArenaThemeId>('champions_night');
  const [glassPreset, setGlassPreset] = useState<GlassPresetId>('coach');
  const [hitboxesVisible, setHitboxesVisible] = useState(false);
  const [voronoiVisible, setVoronoiVisible] = useState(false);
  const [qaShowroom, setQaShowroom] = useState<QaShowroomPreset>('');
  const [isRecording, setIsRecording] = useState(false);
  const [recordingSeconds, setRecordingSeconds] = useState(0);
  const [statusNotice, setStatusNotice] = useState<string | null>(null);
  const [dragActive, setDragActive] = useState(false);

  const viewerRef = useRef<RLReplayViewer | null>(null);
  const shadowRootRef = useRef<ShadowRoot | null>(null);
  const recorderRef = useRef<ActiveWebmRecording | null>(null);
  const closeToolbarMenusRef = useRef<(() => boolean) | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const loadSeqRef = useRef(0);
  const noticeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const localFileInputRef = useRef<HTMLInputElement>(null);
  const appendSeriesInputRef = useRef<HTMLInputElement>(null);

  const showNotice = useCallback((msg: string) => {
    setStatusNotice(msg);
    if (noticeTimerRef.current) clearTimeout(noticeTimerRef.current);
    noticeTimerRef.current = setTimeout(() => setStatusNotice(null), 4000);
  }, []);

  // Capture-phase Escape coordinator (Layers 1-4; Layer 5 Theater Mode & Layer 6 Modal live in parent):
  // 1) Open toolbar dropdown -> close dropdown
  // 2) Open SDK modal dialog (strictly .rlv-modal, never popovers!) -> explicitly close dialog
  // 3) Active Telestrator Draw Mode -> exit Draw Mode back to Nav
  // 4) Native Fullscreen -> let browser exit fullscreen without closing Theater or outer Modal
  useEffect(() => {
    if (!onRegisterEscapeGuard) return;
    const guard = (e: Event): boolean => {
      if (closeToolbarMenusRef.current?.()) {
        e.preventDefault();
        e.stopPropagation();
        return true;
      }

      const shadow = shadowRootRef.current;
      if (shadow) {
        const openModal = shadow.querySelector<HTMLDialogElement>('dialog.rlv-modal[open]');
        if (openModal) {
          e.preventDefault();
          e.stopPropagation();
          if (typeof openModal.close === 'function') openModal.close();
          else openModal.removeAttribute('open');
          return true;
        }
      }

      const viewer = viewerRef.current;
      if (viewer && !viewer.isDisposed() && viewer.ui.toolbar?.isDrawModeActive?.()) {
        e.preventDefault();
        e.stopPropagation();
        viewer.ui.toolbar.setDrawMode(false);
        return true;
      }

      if (typeof document !== 'undefined' && document.fullscreenElement) {
        e.stopPropagation();
        return true;
      }

      return false;
    };

    onRegisterEscapeGuard(guard);
    return () => onRegisterEscapeGuard(null);
  }, [onRegisterEscapeGuard]);

  // Recording elapsed timer
  useEffect(() => {
    if (!isRecording) {
      setRecordingSeconds(0);
      return;
    }
    const id = setInterval(() => setRecordingSeconds((s) => s + 1), 1000);
    return () => clearInterval(id);
  }, [isRecording]);

  // Dispose active recorder & timers on unmount
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      if (noticeTimerRef.current) clearTimeout(noticeTimerRef.current);
      if (recorderRef.current) {
        try {
          recorderRef.current.dispose();
        } catch {
          // ignore teardown errors
        }
        recorderRef.current = null;
      }
    };
  }, []);

  const beginDownload = useCallback(
    async (
      guid: string,
      expectedSource: ReplaySource,
      expectedSizeBytes: number,
      signal: AbortSignal,
    ) => {
      setPhase('downloading');
      setResolvedSource(expectedSource);
      setProgress({
        loadedBytes: 0,
        totalBytes: expectedSizeBytes > 0 ? expectedSizeBytes : null,
        percent: expectedSizeBytes > 0 ? 0 : null,
      });

      const downloaded = await fetchReplayBinary(guid, {
        signal,
        onProgress: (p) => setProgress(p),
      });

      if (signal.aborted) return;
      // Avoid stale browser-cached `ballchasing` header overriding confirmed `cache` status.
      const finalSource =
        expectedSource === 'cache' && downloaded.source === 'ballchasing'
          ? 'cache'
          : downloaded.source;
      const filename = downloaded.filename || `${guid}.replay`;
      setResolvedSource(finalSource);
      setResolvedFilename(filename);
      setServerBacked(true);
      setPhase('parsing');
      setLoadInput({
        id: ++loadSeqRef.current,
        filename,
        bytes: downloaded.bytes,
      });
    },
    [],
  );

  const loadMatchReplay = useCallback(
    async (options?: { lookupBallchasing?: boolean }) => {
      abortRef.current?.abort();
      const ctrl = new AbortController();
      abortRef.current = ctrl;

      setGuidance(null);
      setProgress(null);
      setResolvedSource(null);
      setServerBacked(false);
      setPhase(options?.lookupBallchasing ? 'searching_ballchasing' : 'checking');

      try {
        let status = await fetchReplayStatus(match.match_guid, {
          lookup: Boolean(options?.lookupBallchasing),
          signal: ctrl.signal,
        });
        if (abortRef.current !== ctrl || ctrl.signal.aborted) return;
        setStatusInfo(status);
        setResolvedFilename(status.filename || `${match.match_guid}.replay`);

        // Seamlessly auto-lookup on Ballchasing when not found locally and Ballchasing is configured.
        if (
          !options?.lookupBallchasing &&
          !status.available &&
          status.ballchasing_configured &&
          (status.reason === 'no_replay_source' || status.reason === 'not_synced_yet')
        ) {
          setPhase('searching_ballchasing');
          status = await fetchReplayStatus(match.match_guid, {
            lookup: true,
            signal: ctrl.signal,
          });
          if (abortRef.current !== ctrl || ctrl.signal.aborted) return;
          setStatusInfo(status);
          setResolvedFilename(status.filename || `${match.match_guid}.replay`);
        }

        if (!status.available) {
          setResolvedSource(null);
          setGuidance(describeUnavailableStatus(status));
          setPhase('unavailable');
          return;
        }

        await beginDownload(match.match_guid, status.source, status.size_bytes, ctrl.signal);
      } catch (err) {
        if (abortRef.current !== ctrl) {
          return;
        }
        if (ctrl.signal.aborted && !(err instanceof ReplayApiError && err.code === 'aborted')) {
          return;
        }
        setResolvedSource(null);
        setGuidance(describeReplayError(err));
        setPhase('error');
      }
    },
    [match.match_guid, beginDownload],
  );

  useEffect(() => {
    void loadMatchReplay();
    return () => {
      const current = abortRef.current;
      abortRef.current = null;
      current?.abort();
    };
  }, [loadMatchReplay]);

  const handleCancelDownload = () => {
    abortRef.current?.abort();
    setResolvedSource(null);
    setGuidance(describeReplayError(new ReplayApiError('Download cancelled', 'aborted', 0)));
    setPhase('error');
  };

  const handleLocalFileSelected = useCallback(
    (file: File, appendToSeries = false, extraFiles?: readonly File[]) => {
      if (!file) return;
      if (appendToSeries && viewerRef.current && !viewerRef.current.isDisposed()) {
        setLoadInput({
          id: ++loadSeqRef.current,
          filename: file.name,
          file,
          appendSeries: true,
        });
        return;
      }

      const prev = abortRef.current;
      abortRef.current = null;
      prev?.abort();
      setGuidance(null);
      setServerBacked(false);
      setResolvedSource('local_file');
      setResolvedFilename(file.name);
      setPhase('parsing');
      setLoadInput({
        id: ++loadSeqRef.current,
        filename: file.name,
        file,
        extraFiles,
      });
    },
    [],
  );

  const handleExternalFilesSelected = useCallback(
    (files: File[]) => {
      if (!files.length) return;
      const [first, ...rest] = files;
      handleLocalFileSelected(first, false, rest.length > 0 ? rest : undefined);
    },
    [handleLocalFileSelected],
  );

  const handleViewerReady = useCallback(
    (viewer: RLReplayViewer, shadowRoot: ShadowRoot, engine: 'rapier' | 'builtin') => {
      viewerRef.current = viewer;
      shadowRootRef.current = shadowRoot;
      setViewerReady(true);
      setPhysicsEngine(engine);
    },
    [],
  );

  const handleReplayLoaded = useCallback(
    (notice?: string) => {
      setPhase('ready');
      if (notice) showNotice(notice);
    },
    [showNotice],
  );

  const handleSdkStateSync = useCallback((patch: SdkSyncedState) => {
    if (patch.profile !== undefined) setProfile(patch.profile);
    if (patch.voronoiVisible !== undefined) setVoronoiVisible(patch.voronoiVisible);
    if (patch.hitboxesVisible !== undefined) setHitboxesVisible(patch.hitboxesVisible);
  }, []);

  const handleExternalLocalLoad = useCallback(
    (filename?: string) => {
      setServerBacked(false);
      setResolvedSource('local_file');
      if (filename) {
        setResolvedFilename(filename);
        showNotice(`LOADED ${filename}`);
      }
    },
    [showNotice],
  );

  const handleViewerError = useCallback((err: Error) => {
    setResolvedSource(null);
    setGuidance(describeReplayError(err));
    setPhase('error');
  }, []);

  const handleExportAction = useCallback(
    async (action: ReplayExportAction) => {
      const viewer = viewerRef.current;
      if (!viewer || viewer.isDisposed()) return;
      const baseName = resolvedFilename || match.match_guid;

      try {
        switch (action) {
          case 'png': {
            const snap = exportViewerPngSnapshot(viewer, baseName, matchTitle);
            showNotice(`SAVED PNG (${snap.width}×${snap.height})`);
            break;
          }
          case 'webm_toggle': {
            if (recorderRef.current) {
              const rec = recorderRef.current;
              recorderRef.current = null;
              setIsRecording(false);
              const bytes = await stopViewerWebmRecording(rec, baseName);
              showNotice(`SAVED WEBM (${formatBytes(bytes)})`);
            } else {
              recorderRef.current = startViewerWebmRecording(viewer);
              setIsRecording(true);
              showNotice('RECORDING WEBM…');
            }
            break;
          }
          case 'rlbundle': {
            const bytes = await exportViewerRlBundle(viewer, baseName);
            showNotice(`SAVED .RLBUNDLE (${formatBytes(bytes)})`);
            break;
          }
          case 'npz': {
            const bytes = exportViewerNpz(viewer, baseName);
            showNotice(`SAVED .NPZ (${formatBytes(bytes)})`);
            break;
          }
          case 'csv_metrics': {
            exportViewerAnalyticsCsv(viewer, baseName, 'metrics74');
            showNotice('SAVED 79-METRIC CSV');
            break;
          }
          case 'csv_tables': {
            exportViewerAnalyticsCsv(viewer, baseName, 'combined');
            showNotice('SAVED ALL-TABLES CSV');
            break;
          }
          case 'analytics_json': {
            exportViewerAnalyticsJson(viewer, baseName);
            showNotice('SAVED ANALYTICS JSON');
            break;
          }
          case 'training_pack': {
            exportViewerTrainingPack(viewer, baseName);
            showNotice('SAVED TRAINING PACK JSON');
            break;
          }
          case 'state_dialog': {
            viewer.ui.importExportDialog.open();
            break;
          }
        }
      } catch (err) {
        if (action === 'webm_toggle') {
          setIsRecording(false);
          recorderRef.current = null;
        }
        showNotice(err instanceof Error ? err.message : 'Export failed');
      }
    },
    [match.match_guid, matchTitle, resolvedFilename, showNotice],
  );

  const handleToolAction = useCallback((action: ReplayToolAction) => {
    const viewer = viewerRef.current;
    switch (action) {
      case 'command_palette':
        viewer?.ui.paletteDialog.open();
        break;
      case 'layout_manager':
        viewer?.ui.layoutDialog.open();
        break;
      case 'viewer_settings':
        viewer?.ui.settingsDialog.open();
        break;
      case 'hotkey_guide':
        if (viewer) {
          viewer.ui.hotkeyDialog.open(viewer.ui.hotkeys.getBindings());
        }
        break;
      case 'load_local':
        localFileInputRef.current?.click();
        break;
      case 'append_series':
        appendSeriesInputRef.current?.click();
        break;
    }
  }, []);

  const handleToggleFullscreen = useCallback(() => {
    const viewer = viewerRef.current;
    if (!viewer || viewer.isDisposed()) return;
    const stageEl = viewer.ui.stageEl;
    if (document.fullscreenElement) {
      void document.exitFullscreen?.();
    } else {
      void stageEl.requestFullscreen?.();
    }
  }, []);

  const showViewerCanvas = loadInput !== null && (phase === 'parsing' || phase === 'ready');
  const downloadHref = serverBacked && phase === 'ready' ? buildReplayFileUrl(match.match_guid, true) : null;

  return (
    <div
      data-testid="match-replay-tab"
      className="flex flex-col flex-1 min-h-0 h-full"
      onDragOver={(e) => {
        if (showViewerCanvas) return;
        e.preventDefault();
        setDragActive(true);
      }}
      onDragLeave={() => {
        if (dragActive) setDragActive(false);
      }}
      onDrop={(e) => {
        if (showViewerCanvas) return;
        e.preventDefault();
        setDragActive(false);
        const file = e.dataTransfer?.files?.[0];
        if (file) void handleLocalFileSelected(file, false);
      }}
    >
      {/* Hidden local file inputs for loading/appending .replay, .rlbundle, or .json */}
      <input
        ref={localFileInputRef}
        type="file"
        accept=".replay,.rlbundle,.json,.rlplaybook.json"
        data-testid="replay-local-file-input"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) void handleLocalFileSelected(f, false);
          e.target.value = '';
        }}
      />
      <input
        ref={appendSeriesInputRef}
        type="file"
        accept=".replay"
        data-testid="replay-append-series-input"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) void handleLocalFileSelected(f, true);
          e.target.value = '';
        }}
      />

      <ReplayViewerToolbar
        source={phase === 'error' || phase === 'unavailable' ? null : resolvedSource}
        showPhysicsBadge={showViewerCanvas}
        physicsEngine={physicsEngine}
        downloadHref={downloadHref}
        downloadFilename={resolvedFilename}
        viewerReady={viewerReady && phase === 'ready'}
        profile={profile}
        arenaTheme={arenaTheme}
        glassPreset={glassPreset}
        hitboxesVisible={hitboxesVisible}
        voronoiVisible={voronoiVisible}
        qaShowroom={qaShowroom}
        isRecording={isRecording}
        recordingSeconds={recordingSeconds}
        theaterMode={theaterMode}
        statusNotice={statusNotice}
        onProfileChange={setProfile}
        onArenaThemeChange={setArenaTheme}
        onGlassPresetChange={setGlassPreset}
        onToggleHitboxes={() => setHitboxesVisible((v) => !v)}
        onToggleVoronoi={() => setVoronoiVisible((v) => !v)}
        onQaShowroomChange={setQaShowroom}
        onExportAction={(a) => void handleExportAction(a)}
        onToolAction={handleToolAction}
        onToggleTheaterMode={onToggleTheaterMode}
        onToggleFullscreen={handleToggleFullscreen}
        onRegisterCloseMenus={(fn) => {
          closeToolbarMenusRef.current = fn;
        }}
      />

      {/* Stage Area (Viewport-Bounded Flex Column in Both Inline and Theater Modes) */}
      <div
        data-testid="replay-stage-container"
        className={`relative w-full bg-black overflow-hidden flex-1 min-h-[340px] ${
          dragActive ? 'ring-2 ring-inset ring-blue-ink' : ''
        }`}
      >
        {showViewerCanvas && (
          <ViewerErrorBoundary onError={handleViewerError}>
            <Suspense
              fallback={
                <div
                  data-testid="replay-loading-chunk"
                  className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black text-ink"
                >
                  <span className="text-sm font-extrabold uppercase tracking-widest text-blue-ink animate-pulse">
                    LOADING 3D REPLAY ENGINE…
                  </span>
                </div>
              }
            >
              <LazyReplayViewerStage
                loadInput={loadInput}
                profile={profile}
                arenaTheme={arenaTheme}
                glassPreset={glassPreset}
                hitboxesVisible={hitboxesVisible}
                voronoiVisible={voronoiVisible}
                qaShowroom={qaShowroom}
                theaterMode={theaterMode}
                onViewerReady={handleViewerReady}
                onReplayLoaded={handleReplayLoaded}
                onSdkStateSync={handleSdkStateSync}
                onExternalLocalLoad={handleExternalLocalLoad}
                onExternalFilesSelected={handleExternalFilesSelected}
                onNotice={showNotice}
                onError={handleViewerError}
              />
            </Suspense>
          </ViewerErrorBoundary>
        )}

        {/* Loading / Downloading / Parsing Overlay */}
        {(phase === 'checking' ||
          phase === 'searching_ballchasing' ||
          phase === 'downloading' ||
          phase === 'parsing') && (
          <div
            data-testid="replay-status-overlay"
            data-phase={phase}
            className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-4 p-8 bg-[#050811]/95 text-center"
          >
            <div className="flex flex-col items-center gap-2 max-w-lg w-full">
              <span className="text-xs font-extrabold uppercase tracking-widest text-blue-ink">
                {phase === 'checking'
                  ? 'CHECKING REPLAY AVAILABILITY…'
                  : phase === 'searching_ballchasing'
                  ? 'SEARCHING BALLCHASING ARCHIVE…'
                  : phase === 'downloading'
                  ? resolvedSource === 'ballchasing'
                    ? 'DOWNLOADING REPLAY FROM BALLCHASING…'
                    : resolvedSource === 'cache'
                    ? 'LOADING FROM LOCAL BALLCHASING CACHE…'
                    : 'LOADING LOCAL REPLAY FILE…'
                  : 'PARSING REPLAY IN WASM & INITIALIZING 3D ARENA…'}
              </span>

              {resolvedFilename && (
                <span className="text-xs t-font-mono text-muted truncate max-w-full">
                  {resolvedFilename}
                </span>
              )}

              {/* Streaming Byte Progress Bar */}
              <div className="w-full max-w-md h-2 bg-line-2 overflow-hidden mt-1">
                {phase === 'downloading' && progress?.percent != null ? (
                  <div
                    data-testid="replay-progress-bar"
                    className="h-full bg-blue-ink transition-[width] duration-150"
                    style={{ width: `${progress.percent}%` }}
                  />
                ) : (
                  <div className="h-full w-1/3 bg-blue-ink animate-pulse" />
                )}
              </div>

              {phase === 'downloading' && progress && (
                <span data-testid="replay-progress-text" className="text-xs t-font-mono text-ink-2">
                  {formatBytes(progress.loadedBytes)}
                  {progress.totalBytes ? ` / ${formatBytes(progress.totalBytes)}` : ''}
                  {progress.percent != null ? ` (${progress.percent}%)` : ''}
                </span>
              )}

              {(phase === 'downloading' || phase === 'searching_ballchasing') && (
                <button
                  type="button"
                  data-testid="replay-cancel-btn"
                  onClick={handleCancelDownload}
                  className="mt-2 px-4 py-1.5 t-bg-page bg-page border border-outline text-xs font-extrabold uppercase tracking-wider text-muted hover:text-loss hover:border-loss t-radius cursor-pointer"
                >
                  CANCEL
                </button>
              )}
            </div>
          </div>
        )}

        {/* Unavailable / Error State (with Retry, Ballchasing Lookup & Local File Fallback) */}
        {(phase === 'unavailable' || phase === 'error') && guidance && (
          <div
            data-testid="replay-error-state"
            data-phase={phase}
            className="absolute inset-0 z-20 flex flex-col items-center justify-center p-8 bg-[#050811] text-center"
          >
            <div className="max-w-lg w-full t-bg-panel bg-panel border border-line p-6 flex flex-col items-center gap-3 t-radius">
              <span
                className={`text-sm font-extrabold uppercase tracking-widest ${
                  phase === 'error' ? 'text-loss' : 'text-orange-ink'
                }`}
              >
                {guidance.title}
              </span>
              <p className="text-sm text-ink font-sans leading-relaxed">{guidance.detail}</p>
              <p className="text-xs text-muted font-sans leading-relaxed">{guidance.hint}</p>

              {statusInfo?.ballchasing_url && (
                <a
                  href={statusInfo.ballchasing_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs font-bold uppercase tracking-wider text-blue-ink hover:underline"
                >
                  OPEN ON BALLCHASING ↗
                </a>
              )}

              <div className="flex flex-wrap items-center justify-center gap-2.5 pt-2">
                {guidance.canSearchBallchasing && (
                  <button
                    type="button"
                    data-testid="replay-search-ballchasing-btn"
                    onClick={() => void loadMatchReplay({ lookupBallchasing: true })}
                    className="px-4 py-2 t-bg-blue-slab bg-blue-slab text-white font-extrabold text-xs uppercase tracking-wider hover:opacity-90 t-radius cursor-pointer"
                  >
                    SEARCH BALLCHASING &amp; DOWNLOAD
                  </button>
                )}

                {guidance.canRetry && (
                  <button
                    type="button"
                    data-testid="replay-retry-btn"
                    onClick={() => void loadMatchReplay()}
                    className="px-4 py-2 t-bg-page bg-page border border-outline text-ink font-extrabold text-xs uppercase tracking-wider hover:border-ink hover:text-ink-strong t-radius cursor-pointer"
                  >
                    ⟳ RETRY
                  </button>
                )}

                <button
                  type="button"
                  data-testid="replay-load-local-btn"
                  onClick={() => localFileInputRef.current?.click()}
                  className="px-4 py-2 t-bg-ink bg-ink text-page font-extrabold text-xs uppercase tracking-wider hover:bg-blue-ink t-radius cursor-pointer"
                >
                  LOAD LOCAL .REPLAY / .RLBUNDLE
                </button>
              </div>

              <span className="text-[11px] text-faint uppercase tracking-wider pt-1">
                OR DRAG &amp; DROP A .REPLAY, .RLBUNDLE OR .JSON FILE ANYWHERE HERE
              </span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
