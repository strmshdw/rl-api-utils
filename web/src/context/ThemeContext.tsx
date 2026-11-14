import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { STORAGE_KEYS, safeStorageGet, safeStorageSet } from './storage';
import {
  ColorMode,
  FontScale,
  THEME_STYLES,
  ThemeStyleKey,
  applyFontScaleToDOM,
  applyThemeToDOM,
} from './themeRegistry';

export interface ThemeContextValue {
  themeStyle: ThemeStyleKey;
  colorMode: ColorMode;
  setTheme: (style: ThemeStyleKey, mode: ColorMode) => void;
  fontScale: FontScale;
  setFontScale: (scale: FontScale) => void;
}

const DEFAULT_THEME_CONTEXT: ThemeContextValue = {
  themeStyle: 'broadcast',
  colorMode: 'dark',
  setTheme: () => {},
  fontScale: 'normal',
  setFontScale: () => {},
};

const ThemeContext = createContext<ThemeContextValue>(DEFAULT_THEME_CONTEXT);

export const ThemeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [themeStyle, setThemeStyleState] = useState<ThemeStyleKey>(() => {
    const saved = safeStorageGet(STORAGE_KEYS.themeStyle) as ThemeStyleKey | null;
    return saved && THEME_STYLES[saved] ? saved : 'broadcast';
  });

  const [colorMode, setColorModeState] = useState<ColorMode>(() => {
    const saved = safeStorageGet(STORAGE_KEYS.colorMode);
    return saved === 'light' ? 'light' : 'dark';
  });

  const [fontScale, setFontScaleState] = useState<FontScale>(() => {
    const saved = safeStorageGet(STORAGE_KEYS.fontScale);
    return saved === 'xl' ? 'xl' : 'normal';
  });

  useEffect(() => {
    applyThemeToDOM(themeStyle, colorMode);
  }, [themeStyle, colorMode]);

  useEffect(() => {
    applyFontScaleToDOM(fontScale);
  }, [fontScale]);

  const setTheme = useCallback((style: ThemeStyleKey, mode: ColorMode) => {
    const validStyle = THEME_STYLES[style] ? style : 'broadcast';
    const validMode: ColorMode = mode === 'light' ? 'light' : 'dark';
    setThemeStyleState(validStyle);
    setColorModeState(validMode);
    safeStorageSet(STORAGE_KEYS.themeStyle, validStyle);
    safeStorageSet(STORAGE_KEYS.colorMode, validMode);
    applyThemeToDOM(validStyle, validMode);
  }, []);

  const setFontScale = useCallback((scale: FontScale) => {
    const validScale: FontScale = scale === 'xl' ? 'xl' : 'normal';
    setFontScaleState(validScale);
    safeStorageSet(STORAGE_KEYS.fontScale, validScale);
    applyFontScaleToDOM(validScale);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({
      themeStyle,
      colorMode,
      setTheme,
      fontScale,
      setFontScale,
    }),
    [themeStyle, colorMode, setTheme, fontScale, setFontScale],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
};

export function useTheme(): ThemeContextValue {
  return useContext(ThemeContext);
}
