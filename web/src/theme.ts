import { useCallback, useEffect, useState } from "react";

// Storage key is duplicated in index.html's pre-paint bootstrap script — keep
// the two in sync.
const STORAGE_KEY = "tgv-theme";

export type Theme = "light" | "dark";

function currentTheme(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

// useTheme reads the polarity the bootstrap script already applied and lets the
// header toggle flip it. The choice is persisted, so an explicit pick wins over
// the OS preference on the next visit.
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(currentTheme);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      /* storage disabled — the in-memory toggle still works for this session */
    }
  }, [theme]);

  const toggle = useCallback(
    () => setTheme((t) => (t === "dark" ? "light" : "dark")),
    [],
  );

  return { theme, toggle };
}
