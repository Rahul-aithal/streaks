import { useSyncExternalStore } from "react"

export type Theme = "light" | "dark"

const KEY = "streaks-theme"

// Runs before the first paint so a dark-mode user never sees a white flash.
export const THEME_SCRIPT = `try{var t=localStorage.getItem("${KEY}");if(t==="dark"||(!t&&matchMedia("(prefers-color-scheme: dark)").matches))document.documentElement.classList.add("dark")}catch(e){}`

const listeners = new Set<() => void>()

function current(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light"
}

function apply(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark")
  try {
    localStorage.setItem(KEY, theme)
  } catch {
    // Private windows can refuse storage. The theme still applies for this page.
  }
  for (const listener of listeners) listener()
}

// Fades between the themes instead of snapping. Browsers with View Transitions
// cross-fade the whole page in one frame sequence; the rest get a short colour
// transition (styles.css, .theme-fade). Reduced motion switches instantly.
function setTheme(theme: Theme) {
  if (matchMedia("(prefers-reduced-motion: reduce)").matches) {
    apply(theme)
  } else if (typeof document.startViewTransition === "function") {
    document.startViewTransition(() => apply(theme))
  } else {
    const root = document.documentElement
    root.classList.add("theme-fade")
    apply(theme)
    setTimeout(() => root.classList.remove("theme-fade"), 400)
  }
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, current, () => "light" as Theme)
  return { theme, toggle: () => setTheme(theme === "dark" ? "light" : "dark") }
}
