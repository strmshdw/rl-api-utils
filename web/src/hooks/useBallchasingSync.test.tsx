// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, flush, mount } from '../test/dom';
import { useBallchasingSync, type UseBallchasingSyncResult } from './useBallchasingSync';

function Harness({
  onReady,
  onSyncComplete,
}: {
  onReady: (api: UseBallchasingSyncResult) => void;
  onSyncComplete?: () => void;
}) {
  const api = useBallchasingSync({ onSyncComplete, pollIntervalMs: 25 });
  onReady(api);
  return <div data-syncing={String(api.isSyncing)}>{api.syncMessage ?? ''}</div>;
}

describe('useBallchasingSync', () => {
  const realFetch = globalThis.fetch;

  afterEach(() => {
    cleanup();
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it('triggers sync, polls status until completion, and notifies caller', async () => {
    let running = false;
    let pollCount = 0;
    const onComplete = vi.fn();

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/sync/ballchasing' && init?.method === 'POST') {
        running = true;
        pollCount = 0;
        return new Response(JSON.stringify({ status: 'started' }), { status: 202 });
      }
      if (url === '/api/sync/ballchasing/status') {
        pollCount++;
        if (pollCount >= 2) running = false;
        return new Response(
          JSON.stringify({
            running,
            total_replays: 12,
            synced_matches: 12,
            synced_players: 28,
          }),
          { status: 200 },
        );
      }
      return new Response('{}', { status: 404 });
    }) as unknown as typeof fetch;

    let latest!: UseBallchasingSyncResult;
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
      await latest.triggerSync();
    });

    await flush(90);
    expect(latest.isSyncing).toBe(false);
    expect(latest.syncMessage).toBe('SYNCED 12 MATCHES · 28 PLAYERS FROM BALLCHASING');
    expect(onComplete).toHaveBeenCalledTimes(1);
  });

  it('handles HTTP 409 conflict without erroring and stops gracefully on HTTP 500', async () => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url === '/api/sync/ballchasing' && init?.method === 'POST') {
        return new Response(JSON.stringify({ error: 'already running' }), { status: 409 });
      }
      if (url === '/api/sync/ballchasing/status') {
        return new Response(
          JSON.stringify({
            running: false,
            total_replays: 4,
            synced_matches: 4,
            synced_players: 10,
            last_error: 'rate limited',
          }),
          { status: 200 },
        );
      }
      return new Response(JSON.stringify({ error: 'boom' }), { status: 500 });
    }) as unknown as typeof fetch;

    let latest!: UseBallchasingSyncResult;
    await mount(
      <Harness
        onReady={(api) => {
          latest = api;
        }}
      />,
    );

    await act(async () => {
      await latest.triggerSync();
    });
    await flush(60);

    expect(latest.error).toBeNull();
    expect(latest.isSyncing).toBe(false);
    expect(latest.syncMessage).toBe('SYNC FAILED · rate limited');
  });
});
