import type {
  RLReplayViewer,
  SnapshotRendererLike,
  ViewportMediaRecorder,
} from '@rl-replay/viewer-sdk';

export interface ActiveWebmRecording {
  stop(): Promise<Blob>;
  dispose(): void;
  getMimeType(): string;
}

const WEBM_MIME_PREFERENCE = [
  'video/webm;codecs=vp8',
  'video/webm;codecs=vp9',
  'video/webm',
] as const;

export function pickReliableWebmMimeType(
  recorderCtor: typeof MediaRecorder | undefined = typeof MediaRecorder !== 'undefined'
    ? MediaRecorder
    : undefined,
): string {
  const isSupported = recorderCtor?.isTypeSupported;
  if (typeof isSupported === 'function') {
    for (const candidate of WEBM_MIME_PREFERENCE) {
      if (isSupported(candidate)) return candidate;
    }
  }
  return 'video/webm';
}

export function sanitizeFileBase(input: string): string {
  const cleaned = (input || 'replay')
    .replace(/\.replay$/i, '')
    .replace(/[^a-zA-Z0-9._-]+/g, '_')
    .replace(/^_+|_+$/g, '');
  return cleaned || 'replay';
}

export function triggerBlobDownload(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function triggerDataUrlDownload(dataUrl: string, filename: string): void {
  const a = document.createElement('a');
  a.href = dataUrl;
  a.download = filename;
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
}

export function triggerTextDownload(
  content: string,
  filename: string,
  mimeType = 'text/plain;charset=utf-8',
): void {
  triggerBlobDownload(new Blob([content], { type: mimeType }), filename);
}

export function exportViewerPngSnapshot(
  viewer: RLReplayViewer,
  baseName: string,
  title?: string,
): { width: number; height: number } {
  const canvas = viewer.stage.canvas;
  const renderer: SnapshotRendererLike = {
    domElement: canvas,
    getSize: () => ({
      width: Math.max(1, canvas.clientWidth || canvas.width || 1280),
      height: Math.max(1, canvas.clientHeight || canvas.height || 720),
    }),
    setSize: (w: number, h: number) => {
      viewer.stage.resize(w, h);
    },
    render: () => {
      viewer.stage.markRenderDirty();
      viewer.stage.scene.render();
    },
  };

  const result = viewer.studio.exportSnapshot({
    renderer,
    resolution: '1080p',
    title: title || baseName,
    timestamp: viewer.pipeline.getPlaybackState().currentTime,
    showHud: true,
    watermarkText: 'RL/SYNC · 3D REPLAY',
  });

  if (!result.dataUrl) {
    throw new Error('Could not capture PNG snapshot from WebGL canvas.');
  }
  triggerDataUrlDownload(result.dataUrl, `${sanitizeFileBase(baseName)}_snapshot.png`);
  return { width: result.width, height: result.height };
}

function flushStreamVideoTracks(recorder: ViewportMediaRecorder): void {
  const internals = recorder as unknown as {
    stream?: MediaStream | null;
  };
  const tracks = internals.stream?.getVideoTracks?.();
  if (!tracks) return;
  for (const track of tracks) {
    const canvasTrack = track as MediaStreamTrack & { requestFrame?: () => void };
    try {
      canvasTrack.requestFrame?.();
    } catch {
      // Ignore browsers where requestFrame is unavailable or track already ended.
    }
  }
}

function requestRecorderData(recorder: ViewportMediaRecorder): void {
  const internals = recorder as unknown as {
    recorder?: MediaRecorder | null;
  };
  const native = internals.recorder;
  if (native && native.state === 'recording' && typeof native.requestData === 'function') {
    try {
      native.requestData();
    } catch {
      // Ignore if MediaRecorder is transitioning states.
    }
  }
}

/**
 * Starts a WebM recording backed by `@rl-replay/viewer-sdk`'s `ViewportMediaRecorder`.
 * Pumps rendered WebGL2 frames onto an even-dimensioned 2D mirror canvas and prefers
 * `video/webm;codecs=vp8` so YUV420 encoders reliably emit non-zero chunks even on
 * odd-sized embedded stages, paused scenes, or headless/software WebGL2 backends.
 */
export function startViewerWebmRecording(viewer: RLReplayViewer): ActiveWebmRecording {
  const srcCanvas = viewer.stage.canvas;
  const rawW = srcCanvas.width || srcCanvas.clientWidth || 1280;
  const rawH = srcCanvas.height || srcCanvas.clientHeight || 720;
  const evenW = Math.max(2, Math.floor(rawW / 2) * 2);
  const evenH = Math.max(2, Math.floor(rawH / 2) * 2);

  let targetCanvas: HTMLCanvasElement = srcCanvas;
  let mirrorCtx: CanvasRenderingContext2D | null = null;
  if (typeof document !== 'undefined') {
    const mirror = document.createElement('canvas');
    mirror.width = evenW;
    mirror.height = evenH;
    const ctx = typeof mirror.getContext === 'function' ? mirror.getContext('2d') : null;
    if (ctx) {
      targetCanvas = mirror;
      mirrorCtx = ctx;
    }
  }

  const mimeType = pickReliableWebmMimeType();
  const fps = 30;
  const recorder = viewer.studio.createMediaRecorder({
    canvas: targetCanvas,
    fps,
    videoBitsPerSecond: 4_000_000,
    mimeType,
  });

  const paintFrame = () => {
    if (viewer.isDisposed()) return;
    try {
      viewer.stage.markRenderDirty?.();
      viewer.stage.scene?.render?.();
    } catch {
      // Proceed to copy current framebuffer if synchronous render is unavailable.
    }
    if (mirrorCtx) {
      try {
        mirrorCtx.fillStyle = '#000000';
        mirrorCtx.fillRect(0, 0, targetCanvas.width, targetCanvas.height);
        mirrorCtx.drawImage(srcCanvas, 0, 0, targetCanvas.width, targetCanvas.height);
      } catch {
        // Ignore transient WebGL context errors during teardown.
      }
    }
    flushStreamVideoTracks(recorder);
  };

  recorder.start(250);
  paintFrame();

  let timerId: ReturnType<typeof setInterval> | null = setInterval(
    paintFrame,
    Math.round(1000 / fps),
  );

  const stopPump = () => {
    if (timerId !== null) {
      clearInterval(timerId);
      timerId = null;
    }
  };

  return {
    getMimeType: () => recorder.getMimeType?.() || mimeType,
    stop: async () => {
      paintFrame();
      stopPump();
      requestRecorderData(recorder);
      return recorder.stop();
    },
    dispose: () => {
      stopPump();
      recorder.dispose();
    },
  };
}

export async function stopViewerWebmRecording(
  recorder: ActiveWebmRecording,
  baseName: string,
): Promise<number> {
  try {
    const blob = await recorder.stop();
    if (!blob || blob.size <= 0) {
      throw new Error(
        'WebM clip recording captured 0 bytes. Ensure the 3D stage is visible and browser WebM video codecs are enabled.',
      );
    }
    triggerBlobDownload(blob, `${sanitizeFileBase(baseName)}_clip.webm`);
    return blob.size;
  } finally {
    recorder.dispose();
  }
}

export async function exportViewerRlBundle(
  viewer: RLReplayViewer,
  baseName: string,
): Promise<number> {
  const bytes = await viewer.studio.exportBundle(viewer.pipeline.store);
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  triggerBlobDownload(
    new Blob([copy.buffer], { type: 'application/zip' }),
    `${sanitizeFileBase(baseName)}.rlbundle`,
  );
  return bytes.byteLength;
}

export function exportViewerNpz(viewer: RLReplayViewer, baseName: string): number {
  const bytes = viewer.studio.exportNpz(viewer.pipeline.store);
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  triggerBlobDownload(
    new Blob([copy.buffer], { type: 'application/octet-stream' }),
    `${sanitizeFileBase(baseName)}_telemetry.npz`,
  );
  return bytes.byteLength;
}

export function exportViewerAnalyticsCsv(
  viewer: RLReplayViewer,
  baseName: string,
  variant: 'metrics74' | 'combined' = 'metrics74',
): number {
  const report = viewer.pipeline.getReport();
  if (!report) {
    throw new Error('Analytics report is not available until a replay is loaded.');
  }
  const exported = viewer.studio.exportCsvTables(report);
  const content = variant === 'combined' ? exported.combined : exported.metrics74;
  const suffix = variant === 'combined' ? 'analytics_tables.csv' : 'analytics_79metrics.csv';
  triggerTextDownload(content, `${sanitizeFileBase(baseName)}_${suffix}`, 'text/csv;charset=utf-8');
  return content.length;
}

export function exportViewerAnalyticsJson(viewer: RLReplayViewer, baseName: string): number {
  const report = viewer.pipeline.getReport();
  if (!report) {
    throw new Error('Analytics report is not available until a replay is loaded.');
  }
  const exported = viewer.studio.exportCsvTables(report);
  triggerTextDownload(
    exported.json,
    `${sanitizeFileBase(baseName)}_analytics.json`,
    'application/json;charset=utf-8',
  );
  return exported.json.length;
}

export function exportViewerTrainingPack(viewer: RLReplayViewer, baseName: string): number {
  const json = viewer.studio.exportTrainingPackJson();
  triggerTextDownload(
    json,
    `${sanitizeFileBase(baseName)}_training_pack.json`,
    'application/json;charset=utf-8',
  );
  return json.length;
}
