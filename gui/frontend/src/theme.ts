// theme manages light/dark appearance. "system" follows the OS; an explicit
// choice overrides it and is mirrored onto the <body> for CSS variables and the
// highlight.js theme stylesheets.
export type ThemeMode = "system" | "light" | "dark";

let mode: ThemeMode = "system";
const listeners = new Set<(dark: boolean) => void>();
const mql = window.matchMedia("(prefers-color-scheme: dark)");

export function initTheme(initial: ThemeMode): void {
  mode = initial;
  mql.addEventListener("change", () => {
    if (mode === "system") apply();
  });
  apply();
}

export function setTheme(next: ThemeMode): void {
  mode = next;
  apply();
}

export function toggleTheme(): void {
  setTheme(isDark() ? "light" : "dark");
}

export function isDark(): boolean {
  if (mode === "system") return mql.matches;
  return mode === "dark";
}

export function onThemeChange(cb: (dark: boolean) => void): void {
  listeners.add(cb);
}

function apply(): void {
  const dark = isDark();
  document.body.classList.toggle("theme-dark", dark);
  document.body.classList.toggle("theme-light", !dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
  for (const cb of listeners) cb(dark);
}
