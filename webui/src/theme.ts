export const THEME_KEY = 'goxio-theme';

export type ThemeChoice = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export function readThemeChoice(): ThemeChoice {
  try {
    const stored = localStorage.getItem(THEME_KEY);
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored;
  } catch {
    /* ignore */
  }
  return 'dark';
}

export function resolveTheme(choice: ThemeChoice): ResolvedTheme {
  if (choice === 'system' && typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: light)').matches) {
    return 'light';
  }
  if (choice === 'light') return 'light';
  return 'dark';
}

/** Paint the root. Does not write storage, so the first visit stays uncommitted night. */
export function paintTheme(choice: ThemeChoice = readThemeChoice()): ResolvedTheme {
  const resolved = resolveTheme(choice);
  const root = document.documentElement;
  root.setAttribute('data-theme', resolved);
  root.setAttribute('data-theme-choice', choice);
  root.style.colorScheme = resolved;
  return resolved;
}

export function chooseTheme(choice: ThemeChoice): ResolvedTheme {
  try {
    localStorage.setItem(THEME_KEY, choice);
  } catch {
    /* ignore */
  }
  const resolved = paintTheme(choice);
  window.dispatchEvent(new CustomEvent('goxio-theme', { detail: choice }));
  return resolved;
}

export function graphPaint() {
  const style = getComputedStyle(document.documentElement);
  const pick = (name: string) => style.getPropertyValue(name).trim();
  return {
    dot: pick('--graph-dot'),
    node: pick('--minimap-node'),
    stroke: pick('--minimap-line'),
    mask: pick('--minimap-mask'),
    bg: pick('--minimap-bg'),
  };
}
