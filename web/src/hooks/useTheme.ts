import { useCallback, useEffect, useState } from 'react';

export type ThemeId = 'atelier-light' | 'atelier-dark' | 'swiss-light' | 'swiss-dark';

const STORAGE_KEY = 'cbp-console-theme';

export const THEME_OPTIONS: Array<{ id: ThemeId; label: string; short: string; swatch: string; dark: boolean }> = [
  { id: 'atelier-light', label: '温润纸刊 · 亮', short: '纸刊亮', swatch: '#F5F2EB', dark: false },
  { id: 'atelier-dark', label: '暗墨工坊 · 暗', short: '暗墨暗', swatch: '#1E1D1A', dark: true },
  { id: 'swiss-light', label: '精密冷白 · 亮', short: '冷白亮', swatch: '#3B5BDB', dark: false },
  { id: 'swiss-dark', label: '深邃黑曜 · 暗', short: '黑曜暗', swatch: '#0F1219', dark: true },
];

function resolveInitialTheme(): ThemeId {
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY) as ThemeId | null;
    if (saved && THEME_OPTIONS.some((t) => t.id === saved)) return saved;
  } catch {
    // ignore storage errors
  }
  const prefersDark =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches;
  return prefersDark ? 'atelier-dark' : 'atelier-light';
}

export function useTheme() {
  const [theme, setThemeState] = useState<ThemeId>(resolveInitialTheme);

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    try {
      window.localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      // ignore
    }
  }, [theme]);

  const setTheme = useCallback((next: ThemeId) => {
    setThemeState(next);
  }, []);

  const toggleDarkMode = useCallback(() => {
    setThemeState((cur) => {
      if (cur === 'atelier-light') return 'atelier-dark';
      if (cur === 'atelier-dark') return 'atelier-light';
      if (cur === 'swiss-light') return 'swiss-dark';
      return 'swiss-light';
    });
  }, []);

  const toggleStyleFamily = useCallback(() => {
    setThemeState((cur) => {
      if (cur === 'atelier-light') return 'swiss-light';
      if (cur === 'atelier-dark') return 'swiss-dark';
      if (cur === 'swiss-light') return 'atelier-light';
      return 'atelier-dark';
    });
  }, []);

  const isDark = theme.endsWith('-dark');
  const family = theme.startsWith('atelier') ? 'atelier' : 'swiss';

  return { theme, isDark, family, setTheme, toggleDarkMode, toggleStyleFamily };
}
