import type { Theme } from './api'

export const themeStorageKey = 'ovm.theme'

export function isTheme(value: unknown): value is Theme {
  return value === 'system' || value === 'light' || value === 'dark'
}

export function readStoredTheme(): Theme {
  try {
    const stored = globalThis.localStorage?.getItem(themeStorageKey)
    return isTheme(stored) ? stored : 'system'
  } catch {
    return 'system'
  }
}

export function applyTheme(theme: Theme): void {
  if (typeof document !== 'undefined') {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme =
      theme === 'system' ? 'light dark' : theme
  }

  try {
    globalThis.localStorage?.setItem(themeStorageKey, theme)
  } catch {
    // The backend remains the source of truth if browser storage is unavailable.
  }
}

export function applyStoredTheme(): Theme {
  const theme = readStoredTheme()
  applyTheme(theme)
  return theme
}
