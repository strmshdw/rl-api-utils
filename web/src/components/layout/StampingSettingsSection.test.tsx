// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DashboardProvider } from '../../context/DashboardContext';
import { cleanup, flush, mount } from '../../test/dom';
import { SAMPLE_CURRENT_MATCH, SAMPLE_SESSION_RESPONSE } from '../../utils/sampleData';
import { LiveGameView } from '../live/LiveGameView';
import { SettingsPanel } from './SettingsPanel';

describe('StampingSettingsSection — UI Configuration & Live Reactivity', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it('renders stamping controls in Settings, persists edits via PATCH /api/stamping/config, updates live badges immediately, and resets to defaults', async () => {
    const apiCalls: Array<{ url: string; method: string; body?: unknown }> = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = init?.method || 'GET';
        const body = init?.body ? JSON.parse(String(init.body)) : undefined;
        apiCalls.push({ url, method, body });
        if (url.endsWith('/api/stamping/config/reset')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({
              alt_detection_enabled: true,
              alt_min_tier: 12,
              alt_high_sigma: 3.35,
              alt_high_sigma_max_games: 35,
              alt_streak_sigma: 2.75,
              alt_streak_max_games: 45,
              alt_min_win_streak: 4,
              show_off_mode_peak: true,
              off_mode_min_games: 15,
              off_mode_self_tier_diff: 2.0,
              off_mode_vs_you_tier_diff: 1.5,
              carry_mmr_diff: 100,
              higher_mmr_diff: 60,
              show_hot_streak: true,
              hot_streak_min_wins: 4,
              show_teammate_off_mode_peak: false,
              relationship_min_games: 3,
            }),
          } as Response;
        }
        if (url.endsWith('/api/stamping/config')) {
          return {
            ok: true,
            status: 200,
            json: async () => ({
              alt_detection_enabled: true,
              alt_min_tier: 12,
              alt_high_sigma: 3.35,
              alt_high_sigma_max_games: 35,
              alt_streak_sigma: 2.75,
              alt_streak_max_games: 45,
              alt_min_win_streak: 4,
              show_off_mode_peak: true,
              off_mode_min_games: 15,
              off_mode_self_tier_diff: 2.0,
              off_mode_vs_you_tier_diff: 1.5,
              carry_mmr_diff: 100,
              higher_mmr_diff: 60,
              show_hot_streak: true,
              hot_streak_min_wins: 4,
              show_teammate_off_mode_peak: false,
              relationship_min_games: 3,
              ...(body as Record<string, unknown>),
            }),
          } as Response;
        }
        return {
          ok: true,
          status: 200,
          json: async () => ({ running: false, total_replays: 0, synced_matches: 0, synced_players: 0 }),
        } as Response;
      }),
    );

    const { container } = await mount(
      <DashboardProvider>
        <LiveGameView
          match={SAMPLE_CURRENT_MATCH}
          session={SAMPLE_SESSION_RESPONSE}
          connection="connected"
          columnsOpen={false}
          onCloseColumns={() => {}}
          onShowSession={() => {}}
        />
        <SettingsPanel isOpen={true} onClose={() => {}} inMatch={true} onSwitchToLive={() => {}} />
      </DashboardProvider>,
    );
    await flush(20);

    // Initially, Zenith_99 (+95 VS YOU, +166 VS MATE) shows HIGHER MMR and PARTY CARRY, and VortexRL shows OFF-MODE PEAK
    expect(container.textContent).toContain('◆ HIGHER MMR (+95 VS YOU)');
    expect(container.textContent).toContain('◆ PARTY CARRY (+166 MMR VS MATE)');
    expect(container.textContent).toContain('◆ OFF-MODE PEAK');

    // Toggle OFF "Show Off-Mode Peak" in Settings UI -> VortexRL's OFF-MODE PEAK disappears immediately!
    const offModeToggle = document.querySelector<HTMLInputElement>('#stamping-show-off-mode-peak')!;
    expect(offModeToggle).not.toBeNull();
    await act(async () => {
      offModeToggle.click();
    });
    await flush(15);
    expect(container.textContent).not.toContain('◆ OFF-MODE PEAK');

    // Raise Higher MMR threshold from 60 -> 150 via numeric input -> Zenith's +95 HIGHER MMR disappears!
    const higherInput = document.querySelector<HTMLInputElement>('#stamping-higher-mmr-diff')!;
    expect(higherInput).not.toBeNull();
    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
      setter?.call(higherInput, '150');
      higherInput.dispatchEvent(new Event('input', { bubbles: true }));
      higherInput.dispatchEvent(new Event('change', { bubbles: true }));
      higherInput.dispatchEvent(new Event('blur', { bubbles: true }));
    });
    await flush(15);

    expect(container.textContent).not.toContain('◆ HIGHER MMR (+95 VS YOU)');
    expect(apiCalls.some((c) => c.method === 'PATCH' && c.url.endsWith('/api/stamping/config'))).toBe(true);

    // Click Reset Defaults -> restores 60 MMR and re-enables OFF-MODE PEAK!
    const resetBtn = document.querySelector<HTMLButtonElement>('[data-testid="reset-stamping-defaults-btn"]')!;
    expect(resetBtn).not.toBeNull();
    await act(async () => {
      resetBtn.click();
    });
    await flush(15);

    expect(apiCalls.some((c) => c.method === 'POST' && c.url.endsWith('/api/stamping/config/reset'))).toBe(true);
    expect(container.textContent).toContain('◆ HIGHER MMR (+95 VS YOU)');
    expect(container.textContent).toContain('◆ OFF-MODE PEAK');
  });
});
