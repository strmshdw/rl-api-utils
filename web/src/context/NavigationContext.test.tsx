// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, mount } from '../test/dom';
import { NavigationProvider, useNavigation } from './NavigationContext';

function NavHarness({ onReady }: { onReady: (ctx: ReturnType<typeof useNavigation>) => void }) {
  const ctx = useNavigation();
  onReady(ctx);
  return (
    <div data-tab={ctx.activeTab}>
      {ctx.fullPagePlayer?.id ?? 'none'}|{ctx.quickModalPlayer?.id ?? 'none'}
    </div>
  );
}

describe('NavigationContext', () => {
  afterEach(() => {
    cleanup();
    window.history.replaceState(null, '', '/');
  });

  it('tracks tab changes, quick player inspection, closing, and full profile navigation', async () => {
    let latest!: ReturnType<typeof useNavigation>;
    const mounted = await mount(
      <NavigationProvider>
        <NavHarness
          onReady={(c) => {
            latest = c;
          }}
        />
      </NavigationProvider>,
    );

    expect(latest.activeTab).toBe('live');

    act(() => {
      latest.handleTabChange('history');
    });
    expect(latest.activeTab).toBe('history');
    expect(window.location.pathname).toBe('/history');

    act(() => {
      latest.handleInspectQuickPlayer('Steam|123|0', 'AeroPulse');
    });
    expect(latest.quickModalPlayer).toEqual({ id: 'Steam|123|0', name: 'AeroPulse' });

    act(() => {
      latest.handleCloseQuickPlayer();
    });
    expect(latest.quickModalPlayer).toBeNull();

    act(() => {
      latest.handleOpenFullProfile('Steam|123|0', 'AeroPulse');
    });
    expect(latest.activeTab).toBe('players');
    expect(latest.fullPagePlayer).toEqual({ id: 'Steam|123|0', name: 'AeroPulse' });
    expect(window.location.pathname).toBe('/players/Steam%7C123%7C0');

    mounted.unmount();
    expect(window.location.pathname).toBe('/');
    expect(window.location.search).toBe('');
  });

  it('falls back to players directory when backing out from a deep-linked profile root', async () => {
    window.history.replaceState(null, '', '/?tab=players&player=Steam%7C777%7C0');
    let latest!: ReturnType<typeof useNavigation>;
    await mount(
      <NavigationProvider>
        <NavHarness
          onReady={(c) => {
            latest = c;
          }}
        />
      </NavigationProvider>,
    );

    expect(latest.activeTab).toBe('players');
    expect(latest.fullPagePlayer?.id).toBe('Steam|777|0');

    act(() => {
      latest.handleBackFromFullProfile();
    });
    expect(latest.activeTab).toBe('players');
    expect(latest.fullPagePlayer).toBeNull();
  });
});
