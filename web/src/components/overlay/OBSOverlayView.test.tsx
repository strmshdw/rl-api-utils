// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { OBSOverlayView } from './OBSOverlayView';
import { cleanup, flush, mount, setSearch } from '../../test/dom';
import { SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from '../../utils/sampleData';

describe('OBSOverlayView', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = typeof input === 'string' ? input : input.toString();
        if (url.includes('/api/current-match')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({
              ...SAMPLE_CURRENT_MATCH,
              time_remaining: 185,
              is_overtime: false,
            }),
          } as Response;
        }
        if (url.includes('/api/session')) {
          return {
            ok: true,
            status: 200,
            json: async () => SAMPLE_SESSION_RESPONSE,
          } as Response;
        }
        return { ok: true, status: 200, json: async () => ({}) } as Response;
      })
    );
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    setSearch('');
  });

  it('renders score, clock, and session delta by default', async () => {
    setSearch('overlay=true');
    const { container } = await mount(<OBSOverlayView />);
    await flush(10);

    const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
    expect(overlay).toBeTruthy();
    // Default show includes clock, score, session, delta
    expect(overlay?.textContent).toContain('03:05'); // 185s format
    expect(overlay?.textContent).toContain('2'); // Blue score
    expect(overlay?.textContent).toContain('1'); // Orange score
    expect(overlay?.textContent).toContain('+29'); // MMR delta
  });

  it('respects ?theme=neon and ?font=mono and ?scale=1.5', async () => {
    setSearch('theme=neon&font=mono&scale=1.5');
    const { container } = await mount(<OBSOverlayView />);
    await flush(10);

    const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
    expect(overlay).toBeTruthy();
    expect(overlay?.className).toContain('font-mono');
    expect(overlay?.className).toContain('border-cyan-400');
    expect(overlay?.getAttribute('style')).toContain('scale(1.5)');
  });

  it('filters visible sections based on ?show parameter', async () => {
    // Only show score and clock
    setSearch('show=score,clock');
    const { container } = await mount(<OBSOverlayView />);
    await flush(10);

    const overlay = container.querySelector('[data-testid="obs-overlay-view"]');
    expect(overlay).toBeTruthy();
    expect(overlay?.textContent).toContain('03:05'); // Clock visible
    expect(overlay?.textContent).not.toContain('+29'); // Delta hidden
    expect(overlay?.textContent).not.toContain('4–1'); // Session hidden
  });
});
