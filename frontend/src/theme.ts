type Theme = 'light' | 'dark';
const preference = 'clash-studio-theme';

export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';
}

export function applyTheme(theme: Theme, persist = false) {
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#18191a' : '#f7f5ef');
  if (!persist) return;
  try {
    localStorage.setItem(preference, theme);
  } catch {
    // A blocked browser storage should still allow switching the current page.
  }
}

export function initializeTheme() {
  let saved: string | null = null;
  try {
    saved = localStorage.getItem(preference);
  } catch {
    // Fall back to the device setting when browser storage is unavailable.
  }
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  applyTheme(saved === 'light' || saved === 'dark' ? saved : system.matches ? 'dark' : 'light');
}
