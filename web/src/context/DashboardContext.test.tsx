// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, mount } from '../test/dom';
import { DashboardProvider, useDashboard } from './DashboardContext';
import { useTheme } from './ThemeContext';
import { STORAGE_KEYS } from './storage';

function Harness({
  onReady,
}: {
  onReady: (ctx: { dash: ReturnType<typeof useDashboard>; theme: ReturnType<typeof useTheme> }) => void;
}) {
  const dash = useDashboard();
  const theme = useTheme();
  onReady({ dash, theme });
  return <div data-testid="theme">{theme.themeStyle}</div>;
}

describe('DashboardContext + ThemeContext composition', () => {
  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  it('shares ThemeProvider state through both useTheme and useDashboard and persists storage keys', async () => {
    let latest!: { dash: ReturnType<typeof useDashboard>; theme: ReturnType<typeof useTheme> };
    await mount(
      <DashboardProvider>
        <Harness
          onReady={(v) => {
            latest = v;
          }}
        />
      </DashboardProvider>,
    );

    act(() => {
      latest.dash.setTheme('tactical', 'light');
      latest.dash.setFontScale('xl');
      latest.dash.setReplayBannerDismissed(true);
      latest.dash.togglePinPlayer('Steam|111|0');
    });

    expect(latest.theme.themeStyle).toBe('tactical');
    expect(latest.theme.colorMode).toBe('light');
    expect(latest.dash.themeStyle).toBe('tactical');
    expect(latest.dash.fontScale).toBe('xl');
    expect(latest.dash.pinnedPlayerIds).toEqual(['Steam|111|0']);
    expect(localStorage.getItem(STORAGE_KEYS.themeStyle)).toBe('tactical');
    expect(localStorage.getItem(STORAGE_KEYS.colorMode)).toBe('light');
    expect(localStorage.getItem(STORAGE_KEYS.fontScale)).toBe('xl');
    expect(localStorage.getItem(STORAGE_KEYS.replayBannerDismissed)).toBe('true');
    expect(localStorage.getItem(STORAGE_KEYS.pinnedPlayers)).toBe('["Steam|111|0"]');
    expect('dateRangeFactor' in latest.dash).toBe(false);
  });
});
