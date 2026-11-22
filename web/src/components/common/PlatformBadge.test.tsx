// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { DashboardProvider, useDashboard } from '../../context/DashboardContext';
import { STORAGE_KEYS } from '../../context/storage';
import { cleanup, mount } from '../../test/dom';
import { PlatformBadge } from './PlatformBadge';

const PLATFORMS = [
  { input: 'Steam|76561198000000010|0', code: 'steam', label: 'STEAM', title: 'Steam' },
  { input: 'Epic|abc123|0', code: 'epic', label: 'EPIC', title: 'Epic Games' },
  { input: 'PlayStation|psn_1|0', code: 'psn', label: 'PLAYSTATION', title: 'PlayStation' },
  { input: 'PS4|psn_2|0', code: 'psn', label: 'PLAYSTATION', title: 'PlayStation' },
  { input: 'XboxOne|xbl_1|0', code: 'xbox', label: 'XBOX', title: 'Xbox' },
  { input: 'Switch|nintendo_1|0', code: 'switch', label: 'SWITCH', title: 'Nintendo Switch' },
  { input: 'PsyNet|cross_1|0', code: 'psynet', label: 'PSYNET', title: 'PsyNet' },
  { input: 'Unknown|0|0', code: 'bot', label: 'BOT', title: 'AI Bot' },
] as const;

describe('PlatformBadge', () => {
  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  it('renders crisp SVG icons with accessible labels and titles for every platform in icon mode', async () => {
    for (const p of PLATFORMS) {
      const { container, unmount } = await mount(<PlatformBadge platform={p.input} displayMode="icon" />);
      const badge = container.querySelector<HTMLElement>('[data-testid="platform-badge"]')!;
      expect(badge).not.toBeNull();
      expect(badge.dataset.platform).toBe(p.code);
      expect(badge.dataset.platformDisplay).toBe('icon');
      expect(badge.getAttribute('title')).toBe(p.title);
      expect(badge.getAttribute('aria-label')).toBe(p.title);
      expect(badge.querySelector('svg')).not.toBeNull();
      expect(badge.querySelector('.sr-only')?.textContent).toBe(p.label);
      unmount();
    }
  });

  it('renders platform names without SVG icons in name mode', async () => {
    for (const p of PLATFORMS) {
      const { container, unmount } = await mount(<PlatformBadge platform={p.input} displayMode="name" />);
      const badge = container.querySelector<HTMLElement>('[data-testid="platform-badge"]')!;
      expect(badge.dataset.platform).toBe(p.code);
      expect(badge.dataset.platformDisplay).toBe('name');
      expect(badge.querySelector('svg')).toBeNull();
      expect(badge.textContent).toBe(p.label);
      unmount();
    }
  });

  it('renders an em-dash for empty platform strings and handles explicit isBot flag', async () => {
    const empty = await mount(<PlatformBadge platform="" displayMode="icon" />);
    expect(empty.container.textContent).toBe('—');
    expect(empty.container.querySelector('svg')).toBeNull();
    empty.unmount();

    const bot = await mount(<PlatformBadge platform="Steam" isBot displayMode="icon" />);
    const badge = bot.container.querySelector<HTMLElement>('[data-testid="platform-badge"]')!;
    expect(badge.dataset.platform).toBe('bot');
    expect(badge.querySelector('svg')).not.toBeNull();
    expect(badge.textContent).toBe('BOT');
    bot.unmount();
  });

  it('reacts to DashboardContext platformDisplay setting and persists to localStorage', async () => {
    function SwitchHarness() {
      const { platformDisplay, setPlatformDisplay } = useDashboard();
      return (
        <div>
          <PlatformBadge platform="PlayStation" />
          <button type="button" data-testid="to-name" onClick={() => setPlatformDisplay('name')}>
            NAME
          </button>
          <button type="button" data-testid="to-icon" onClick={() => setPlatformDisplay('icon')}>
            ICON
          </button>
          <span data-testid="mode">{platformDisplay}</span>
        </div>
      );
    }

    const { container } = await mount(
      <DashboardProvider>
        <SwitchHarness />
      </DashboardProvider>,
    );

    expect(container.querySelector('[data-testid="mode"]')?.textContent).toBe('icon');
    expect(container.querySelector('[data-testid="platform-badge"] svg')).not.toBeNull();

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="to-name"]')!.click();
    });
    expect(container.querySelector('[data-testid="mode"]')?.textContent).toBe('name');
    expect(container.querySelector('[data-testid="platform-badge"] svg')).toBeNull();
    expect(container.querySelector('[data-testid="platform-badge"]')?.textContent).toBe('PLAYSTATION');
    expect(localStorage.getItem(STORAGE_KEYS.platformDisplay)).toBe('name');

    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="to-icon"]')!.click();
    });
    expect(container.querySelector('[data-testid="platform-badge"] svg')).not.toBeNull();
    expect(localStorage.getItem(STORAGE_KEYS.platformDisplay)).toBe('icon');
  });
});
