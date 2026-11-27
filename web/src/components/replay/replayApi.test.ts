import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  ReplayApiError,
  buildReplayFileUrl,
  buildReplayStatusUrl,
  describeReplayError,
  describeUnavailableStatus,
  fetchReplayBinary,
  fetchReplayStatus,
  formatBytes,
  type ReplayStatusResponse,
} from './replayApi';

describe('replayApi', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('builds encoded status and download URLs', () => {
    expect(buildReplayStatusUrl('guid 1/2')).toBe('/api/matches/guid%201%2F2/replay/status');
    expect(buildReplayStatusUrl('abc-123', true)).toBe(
      '/api/matches/abc-123/replay/status?lookup=true',
    );
    expect(buildReplayFileUrl('abc-123')).toBe('/api/matches/abc-123/replay');
    expect(buildReplayFileUrl('abc-123', true)).toBe('/api/matches/abc-123/replay?download=1');
  });

  it('fetches replay status and supports ?lookup=true', async () => {
    const payload: ReplayStatusResponse = {
      match_guid: 'g-1',
      available: true,
      source: 'local',
      local_available: true,
      ballchasing_available: true,
      ballchasing_configured: true,
      ballchasing_id: 'bc-1',
      ballchasing_url: 'https://ballchasing.com/replay/bc-1',
      filename: '2026_11_21_001_2v2_WIN.replay',
      size_bytes: 1024000,
      download_url: '/api/matches/g-1/replay',
      download_status: 'DOWNLOADED',
      upload_status: 'UPLOADED',
      reason: '',
      message: '',
    };

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => payload,
    } as Response);

    const status = await fetchReplayStatus('g-1', { lookup: true });
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/matches/g-1/replay/status?lookup=true',
      expect.any(Object),
    );
    expect(status.source).toBe('local');
    expect(status.size_bytes).toBe(1024000);
  });

  it('streams replay binary with ReadableStream byte progress and metadata headers', async () => {
    const chunk1 = new Uint8Array([1, 2, 3, 4]);
    const chunk2 = new Uint8Array([5, 6, 7, 8, 9, 10]);
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(chunk1);
        controller.enqueue(chunk2);
        controller.close();
      },
    });

    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(stream, {
        status: 200,
        headers: {
          'Content-Length': '10',
          'X-Replay-Source': 'ballchasing',
          'X-Replay-Filename': 'match_01.replay',
          'X-Replay-Ballchasing-ID': 'bc-99',
          'X-Replay-Match-GUID': 'guid-99',
        },
      }),
    );

    const updates: Array<{ loadedBytes: number; totalBytes: number | null; percent: number | null }> = [];
    const result = await fetchReplayBinary('guid-99', {
      onProgress: (p) => updates.push(p),
    });

    expect(Array.from(result.bytes)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    expect(result.source).toBe('ballchasing');
    expect(result.filename).toBe('match_01.replay');
    expect(result.ballchasingId).toBe('bc-99');
    expect(result.matchGuid).toBe('guid-99');
    expect(result.sizeBytes).toBe(10);
    expect(updates).toEqual([
      { loadedBytes: 4, totalBytes: 10, percent: 40 },
      { loadedBytes: 10, totalBytes: 10, percent: 100 },
    ]);
  });

  it('parses structured 429 rate-limit and 503 missing-key API errors', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: 'Rate limited by Ballchasing',
            code: 'ballchasing_rate_limited',
            retry_after_seconds: 12,
          }),
          { status: 429, headers: { 'Content-Type': 'application/json', 'Retry-After': '12' } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: 'API key missing',
            code: 'missing_ballchasing_api_key',
          }),
          { status: 503, headers: { 'Content-Type': 'application/json' } },
        ),
      );

    await expect(fetchReplayBinary('g-rate')).rejects.toMatchObject({
      code: 'ballchasing_rate_limited',
      status: 429,
      retryAfterSeconds: 12,
    });

    await expect(fetchReplayBinary('g-key')).rejects.toMatchObject({
      code: 'missing_ballchasing_api_key',
      status: 503,
    });
  });

  it('returns clear user guidance for all error and unavailable states', () => {
    const missingKey = describeReplayError(
      new ReplayApiError('No key', 'missing_ballchasing_api_key', 503),
    );
    expect(missingKey.title).toContain('API KEY');
    expect(missingKey.canSearchBallchasing).toBe(false);

    const rateLimited = describeReplayError(
      new ReplayApiError('Too many requests', 'ballchasing_rate_limited', 429, 8),
    );
    expect(rateLimited.detail).toContain('8s');
    expect(rateLimited.canRetry).toBe(true);

    const unavailable = describeUnavailableStatus({
      match_guid: 'g-2',
      available: false,
      source: 'unavailable',
      local_available: false,
      ballchasing_available: false,
      ballchasing_configured: true,
      ballchasing_id: '',
      ballchasing_url: '',
      filename: '',
      size_bytes: 0,
      download_url: '/api/matches/g-2/replay',
      download_status: 'PENDING',
      upload_status: 'PENDING',
      reason: 'not_synced_yet',
      message: '',
    });
    expect(unavailable.canSearchBallchasing).toBe(true);
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(1024 * 1024 * 2.5)).toBe('2.50 MB');
  });
});
