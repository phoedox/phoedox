import { de } from "./de";
import { en } from "./en";

export type Key = keyof typeof de;
export type Locale = "de" | "en";
const tables: Record<Locale, Record<Key, string>> = { de, en };

/** Picks the first supported locale from the browser preferences. */
export function detectLocale(langs: readonly string[] = globalThis.navigator?.languages ?? []): Locale {
  for (const l of langs) {
    const base = l.toLowerCase().split("-")[0];
    if (base === "de" || base === "en") return base;
  }
  return "de";
}

let current: Locale = detectLocale();
export const locale = () => current;
export function setLocale(l: Locale) {
  current = l;
  if (typeof document !== "undefined") document.documentElement.lang = l;
}

export function hasKey(key: string): key is Key {
  return key in de;
}

/** Translates a key and fills {placeholders}. Unknown placeholders stay visible. */
export function t(key: Key, vars: Record<string, unknown> = {}, loc: Locale = current): string {
  return tables[loc][key].replace(/\{(\w+)\}/g, (m, name) => (name in vars ? String(vars[name]) : m));
}

export function formatDate(iso: string, loc: Locale = current): string {
  if (!iso) return "";
  return new Date(iso).toLocaleString(loc === "de" ? "de-DE" : "en-GB", { dateStyle: "medium", timeStyle: "short" });
}
