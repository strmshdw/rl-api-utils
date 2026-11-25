// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DashboardProvider } from '../../context/DashboardContext';
import { SAMPLE_SESSION_RESPONSE } from '../../utils/sampleData';
import { cleanup, flush, mount } from '../../test/dom';
import { ReplayLimitBanner } from './ReplayLimitBanner';

describe('ReplayLimitBanner', () => {
  const realFetch = globalThis.fetch;

  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it('triggers ONLY PsyNet replay sync (/api/sync/now) and NEVER calls /api/sync/ballchasing', async () => {
    const calls: string[] = [];
    const onRefresh = vi.fn();

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input.toString();
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      if (url === '/api/sync/now' && init?.method === 'POST') {
        return new Response(JSON.stringify({ status: 'sync_triggered', running: false }), { status: 200 });
      }
      if (url === '/api/sync/now/status') {
        return new Response(
          JSON.stringify({
            running: false,
            discovered_count: 16,
            downloaded_count: 16,
            uploaded_count: 16,
            pending_count: 0,
          }),
          { status: 200 },
        );
      }
      return new Response('{}', { status: 200 });
    }) as unknown as typeof fetch;

    const warnSession = {
      ...SAMPLE_SESSION_RESPONSE,
      pending_count: 16,
      trigger_threshold: 15,
      max_hosted_replays: 20,
    };

    const { container } = await mount(
      <DashboardProvider>
        <ReplayLimitBanner session={warnSession} onRefreshSession={onRefresh} />
      </DashboardProvider>,
    );

    const syncBtn = container.querySelector<HTMLButtonElement>('[data-testid="banner-sync-now-btn"]')!;
    expect(syncBtn).not.toBeNull();

    await act(async () => {
      syncBtn.click();
    });
    await flush(30);

    expect(calls).toContain('POST /api/sync/now');
    expect(calls.some((c) => c.includes('/api/sync/ballchasing'))).toBe(false);
    expect(onRefresh).toHaveBeenCalled();
  });
});
