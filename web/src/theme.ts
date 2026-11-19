import { useEffect, useState } from "react";

export type ThemeChoice = "system" | "light" | "dark";

const storageKey = "runnel.theme";
const darkQuery = "(prefers-color-scheme: dark)";

function storedChoice(): ThemeChoice {
  try {
    const v = localStorage.getItem(storageKey);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    // storage blocked: fall back to the default
  }
  return "system";
}

/** Resolves a choice to the theme shown; "system" follows the Windows setting. */
export function resolveTheme(choice: ThemeChoice, prefersDark: boolean): "light" | "dark" {
  if (choice === "system") return prefersDark ? "dark" : "light";
  return choice;
}

/** useTheme keeps the theme choice (default: system) and applies it to <html data-theme>. */
export function useTheme(): [ThemeChoice, (c: ThemeChoice) => void] {
  const [choice, setChoice] = useState<ThemeChoice>(storedChoice);
  const [prefersDark, setPrefersDark] = useState(() => window.matchMedia(darkQuery).matches);

  useEffect(() => {
    const mq = window.matchMedia(darkQuery);
    const onChange = () => setPrefersDark(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = resolveTheme(choice, prefersDark);
  }, [choice, prefersDark]);

  const choose = (c: ThemeChoice) => {
    setChoice(c);
    try {
      localStorage.setItem(storageKey, c);
    } catch {
      // not persisted; the choice still applies to this tab
    }
  };
  return [choice, choose];
}
