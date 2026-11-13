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

/** Translates a key and fills {placeholders}. Unknown placeholders stay visible. */
export function t(key: Key, vars: Record<string, string | number> = {}, locale: Locale = detectLocale()): string {
  return tables[locale][key].replace(/\{(\w+)\}/g, (m, name) => (name in vars ? String(vars[name]) : m));
}
