import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";

export interface Settings {
  theme: "light" | "dark" | "system";
  accent: AccentName;
  fontScale: number; // 0.875 | 1 | 1.125
  reducedMotion: boolean;
  highContrast: boolean;
  sound: boolean;
  desktopNotifications: boolean;
  showPreviews: boolean; // message text in desktop notifications
}

export type AccentName = "blue" | "violet" | "emerald" | "rose" | "amber";

/** [base, hover] pairs for each accent preset. */
export const ACCENTS: Record<AccentName, [string, string]> = {
  blue: ["#2563eb", "#1d4ed8"],
  violet: ["#7c3aed", "#6d28d9"],
  emerald: ["#059669", "#047857"],
  rose: ["#e11d48", "#be123c"],
  amber: ["#d97706", "#b45309"],
};

const KEY = "jmessage.settings";

const defaults: Settings = {
  theme: "system",
  accent: "blue",
  fontScale: 1,
  reducedMotion: false,
  highContrast: false,
  sound: true,
  desktopNotifications: false,
  showPreviews: true,
};

/** readSettings works outside React (used by the notification path in
 *  useWebSocket) — localStorage is the single source of truth. */
export function readSettings(): Settings {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? { ...defaults, ...(JSON.parse(raw) as Partial<Settings>) } : defaults;
  } catch {
    return defaults;
  }
}

function apply(s: Settings) {
  const root = document.documentElement;
  const systemDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  root.classList.toggle("dark", s.theme === "dark" || (s.theme === "system" && systemDark));
  root.classList.toggle("reduce-motion", s.reducedMotion);
  root.classList.toggle("high-contrast", s.highContrast);
  root.style.setProperty("--font-scale", String(s.fontScale));
  const [base, hover] = ACCENTS[s.accent] ?? ACCENTS.blue;
  root.style.setProperty("--color-accent", base);
  root.style.setProperty("--color-accent-hover", hover);
}

interface SettingsCtx {
  settings: Settings;
  update: (patch: Partial<Settings>) => void;
}

const Ctx = createContext<SettingsCtx>({ settings: defaults, update: () => {} });

export function SettingsProvider({ children }: { children: ReactNode }) {
  const [settings, setSettings] = useState<Settings>(readSettings);

  const update = useCallback((patch: Partial<Settings>) => {
    setSettings((prev) => {
      const next = { ...prev, ...patch };
      localStorage.setItem(KEY, JSON.stringify(next));
      return next;
    });
  }, []);

  useEffect(() => apply(settings), [settings]);

  // Follow OS theme changes while in "system" mode.
  useEffect(() => {
    if (settings.theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => apply(settings);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [settings]);

  return <Ctx.Provider value={{ settings, update }}>{children}</Ctx.Provider>;
}

export function useSettings(): SettingsCtx {
  return useContext(Ctx);
}
