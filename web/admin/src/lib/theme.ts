// Light/dark theme toggle, mirroring web/policy/script.js's theme mechanism exactly
// (explicit data-theme attribute + localStorage, with a prefers-color-scheme fallback for
// users who haven't toggled yet — see the matching CSS in styles/tokens.css).
//
// Deliberately a distinct localStorage key from policy's "vexel-policy-theme" — the two
// apps are independently deployed and shouldn't share (or fight over) a stored preference.
const THEME_KEY = "vexel-admin-theme";

export type Theme = "light" | "dark";

function isTheme(value: string | null): value is Theme {
  return value === "light" || value === "dark";
}

export function applyTheme(theme: Theme | null): void {
  const root = document.documentElement;
  if (theme) {
    root.setAttribute("data-theme", theme);
  } else {
    root.removeAttribute("data-theme");
  }
}

export function getStoredTheme(): Theme | null {
  const stored = localStorage.getItem(THEME_KEY);
  return isTheme(stored) ? stored : null;
}

// Resolves the theme actually in effect right now: explicit data-theme attribute first,
// falling back to the OS-level prefers-color-scheme the same way the CSS fallback does.
export function getCurrentTheme(): Theme {
  const explicit = document.documentElement.getAttribute("data-theme");
  if (isTheme(explicit)) return explicit;
  const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  return prefersDark ? "dark" : "light";
}

// Call once at app startup (see main.tsx) to re-apply a previously stored explicit choice
// before first paint, avoiding a flash of the wrong theme.
export function initTheme(): void {
  const stored = getStoredTheme();
  if (stored) applyTheme(stored);
}

export function toggleTheme(): Theme {
  const next: Theme = getCurrentTheme() === "dark" ? "light" : "dark";
  applyTheme(next);
  localStorage.setItem(THEME_KEY, next);
  return next;
}
