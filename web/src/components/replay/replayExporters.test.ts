// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest';
import type { RLReplayViewer, ViewportMediaRecorder } from '@rl-replay/viewer-sdk';
import {
  pickReliableWebmMimeType,
  startViewerWebmRecording,
  stopViewerWebmRecording,
  type ActiveWebmRecording,
} from './replayExporters';

describe('replayExporters WebM recording', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('prefers video/webm;codecs=vp8 over vp9 for reliable real-time & headless capture', () => {
    const fakeCtor = {
      isTypeSupported: (mime: string) =>
        mime === 'video/webm;codecs=vp9' || mime === 'video/webm;codecs=vp8',
    } as unknown as typeof MediaRecorder;

    expect(pickReliableWebmMimeType(fakeCtor)).toBe('video/webm;codecs=vp8');
  });

  it('rejects 0-byte WebM blobs with an actionable error without downloading', async () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock');
    const dispose = vi.fn();
    const emptyRecording: ActiveWebmRecording = {
      getMimeType: () => 'video/webm;codecs=vp8',
      stop: vi.fn(async () => new Blob([], { type: 'video/webm;codecs=vp8' })),
      dispose,
    };

    await expect(stopViewerWebmRecording(emptyRecording, 'match-1')).rejects.toThrow(/0 bytes/i);
    expect(createObjectURL).not.toHaveBeenCalled();
    expect(dispose).toHaveBeenCalledTimes(1);
  });

  it('downloads _clip.webm and disposes cleanly for non-empty WebM recordings', async () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock-webm');
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    const dispose = vi.fn();
    const bytes = new Uint8Array([0x1a, 0x45, 0xdf, 0xa3, 1, 2, 3, 4]);
    const recording: ActiveWebmRecording = {
      getMimeType: () => 'video/webm;codecs=vp8',
      stop: vi.fn(async () => new Blob([bytes], { type: 'video/webm;codecs=vp8' })),
      dispose,
    };

    const size = await stopViewerWebmRecording(recording, 'match-42.replay');
    expect(size).toBe(bytes.byteLength);
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(clickSpy).toHaveBeenCalledTimes(1);
    expect(dispose).toHaveBeenCalledTimes(1);
  });

  it('starts SDK ViewportMediaRecorder with 250ms timeslice, flushes frames on stop, and cleans up timer', async () => {
    vi.useFakeTimers();
    const requestFrame = vi.fn();
    const requestData = vi.fn();
    const start = vi.fn();
    const stop = vi.fn(async () => new Blob([new Uint8Array([1, 2, 3, 4])], { type: 'video/webm' }));
    const dispose = vi.fn();
    const fakeSdkRecorder = {
      start,
      stop,
      dispose,
      getMimeType: () => 'video/webm;codecs=vp8',
      stream: { getVideoTracks: () => [{ requestFrame }] },
      recorder: { state: 'recording', requestData },
    } as unknown as ViewportMediaRecorder;

    const createMediaRecorder = vi.fn(() => fakeSdkRecorder);
    const markRenderDirty = vi.fn();
    const render = vi.fn();
    const canvas = document.createElement('canvas');
    canvas.width = 1279; // odd dimension to verify even rounding
    canvas.height = 719;

    const viewer = {
      isDisposed: () => false,
      stage: {
        canvas,
        markRenderDirty,
        scene: { render },
      },
      studio: {
        createMediaRecorder,
      },
    } as unknown as RLReplayViewer;

    const handle = startViewerWebmRecording(viewer);
    expect(createMediaRecorder).toHaveBeenCalledTimes(1);
    expect(start).toHaveBeenCalledWith(250);
    expect(markRenderDirty).toHaveBeenCalled();
    expect(requestFrame).toHaveBeenCalled();

    const blob = await handle.stop();
    expect(requestData).toHaveBeenCalledTimes(1);
    expect(blob.size).toBe(4);
    handle.dispose();
    expect(dispose).toHaveBeenCalledTimes(1);
  });
});
