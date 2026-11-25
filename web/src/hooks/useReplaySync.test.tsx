// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, flush, mount } from '../test/dom';
import { useReplaySync, type UseReplaySyncResult } from './useReplaySync';

function Harness({
  onReady,
  onSyncComplete,
}: {
  onReady: (api: UseReplaySyncResult) => void;
  onSyncComplete?: () => void;
}) {
  const api = useReplaySync({ onSyncComplete, pollIntervalMs: 25 });
  onReady(api);
  return <div data-syncing={String(api.isSyncing)}>{api.syncMessage ?? ''}</div>;
}

describe('useReplaySync', () => {
  const realFetch = globalThis.fetch;

  afterEach(() => {
    cleanup();
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it('calls ONLY /api/sync/now and /api/sync/now/status and NEVER triggers Ballchasing full-history sync', async () => {
    let running = false;
    let pollCount = 0;
    const calls: string[] = [];
    const onComplete = vi.fn();

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      if (url === '/api/sync/now' && init?.method === 'POST') {
        running = true;
        pollCount = 0;
        return new Response(JSON.stringify({ status: 'sync_triggered', running: true }), { status: 200 });
      }
      if (url === '/api/sync/now/status') {
        pollCount++;
        if (pollCount >= 2) running = false;
        return new Response(
          JSON.stringify({
            running,
            discovered_count: 5,
            downloaded_count: 3,
            uploaded_count: 3,
            duplicate_count: 0,
            skipped_count: 0,
            failed_count: 0,
            pending_count: 0,
          }),
          { status: 200 },
        );
      }
      return new Response('{}', { status: 404 });
    }) as unknown as typeof fetch;

    let latest!: UseReplaySyncResult;
    await mount(
      <Harness
        onReady={(api) => {
          latest = api;
        }}
        onSyncComplete={onComplete}
      />,
    );

    await flush(10);
    await act(async () => {
      const ok = await latest.triggerReplaySync();
      expect(ok).toBe(true);
    });

    await flush(90);
    expect(latest.isSyncing).toBe(false);
    expect(latest.syncMessage).toBe('DOWNLOADED 3 NEW REPLAYS · 3 UPLOADED');
    expect(onComplete).toHaveBeenCalledTimes(1);
    expect(calls).toContain('POST /api/sync/now');
    expect(calls.some((c) => c.includes('/api/sync/ballchasing'))).toBe(false);
  });

  it('surfaces HTTP errors cleanly without marking sync complete', async () => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/sync/now' && init?.method === 'POST') {
        return new Response(JSON.stringify({ error: 'psynet unavailable' }), { status: 500 });
      }
      if (url === '/api/sync/now/status') {
        return new Response(JSON.stringify({ running: false }), { status: 200 });
      }
      return new Response('{}', { status: 404 });
    }) as unknown as typeof fetch;

    let latest!: UseReplaySyncResult;
    await mount(
      <Harness
        onReady={(api) => {
          latest = api;
        }}
      />,
    );

    await act(async () => {
      const ok = await latest.triggerReplaySync();
      expect(ok).toBe(false);
    });

    expect(latest.isSyncing).toBe(false);
    expect(latest.error).toBe('psynet unavailable');
  });
});
