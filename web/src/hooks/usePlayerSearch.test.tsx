// @vitest-environment happy-dom
import { describe, it, expect, afterEach, vi } from 'vitest';
import { act } from 'react';
import { usePlayerSearch } from './usePlayerSearch';
import { DashboardProvider } from '../context/DashboardContext';
import { cleanup, flush, mount } from '../test/dom';

interface RecordedFetch {
  url: string;
  signal: AbortSignal;
}

function Harness({ onReady }: { onReady: (api: ReturnType<typeof usePlayerSearch>) => void }) {
  const api = usePlayerSearch({ limit: 24 });
  onReady(api);
  return <div data-testid="page">{api.page}</div>;
}

describe('usePlayerSearch connection & pagination lifecycle', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('resets page to 1 synchronously on filter/sort change without firing an aborted stale-offset fetch', async () => {
    const requests: RecordedFetch[] = [];
    globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      requests.push({ url, signal: init?.signal as AbortSignal });
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ players: [], total: 100, limit: 24, offset: 0 }),
      } as Response);
    }) as unknown as typeof fetch;

    let latestApi!: ReturnType<typeof usePlayerSearch>;
    await mount(
      <DashboardProvider>
        <Harness onReady={(api) => { latestApi = api; }} />
      </DashboardProvider>,
    );
    await flush();

    expect(requests).toHaveLength(1);
    expect(requests[0].url).toContain('offset=0');
    expect(requests[0].signal.aborted).toBe(false);

    // Navigate to page 3 (offset=48)
    await act(async () => {
      latestApi.setPage(3);
    });
    await flush();

    expect(latestApi.page).toBe(3);
    expect(requests).toHaveLength(2);
    expect(requests[1].url).toContain('offset=48');
    expect(requests[1].signal.aborted).toBe(false);

    // Now change platform while on page 3: must issue EXACTLY ONE fetch at offset=0
    // (never firing a transient offset=48&platform=steam request that gets immediately aborted!)
    await act(async () => {
      latestApi.setPlatform('steam');
    });
    await flush();

    expect(latestApi.page).toBe(1);
    expect(requests).toHaveLength(3);
    expect(requests[2].url).toContain('offset=0');
    expect(requests[2].url).toContain('platform=steam');
    expect(requests[2].signal.aborted).toBe(false);

    // Switching back to platform='all' must stay on page 1 (never resurrecting old page 3)
    await act(async () => {
      latestApi.setPlatform('all');
    });
    await flush();

    expect(latestApi.page).toBe(1);
    expect(requests).toHaveLength(4);
    expect(requests[3].url).toContain('offset=0');
    expect(requests[3].signal.aborted).toBe(false);
  });
});
