import { locale } from "./i18n";

/** Languages offered for multilingual projects (ADR-0028); any code like "pt-BR" also works via the API. */
export const contentLocales = ["en", "de", "fr", "es", "it", "nl", "pl", "pt", "cs", "da", "sv", "fi", "nb", "tr", "uk", "ja", "zh"] as const;

/** The name of a language in the interface language, e.g. "Deutsch" or "German". */
export function languageName(code: string): string {
  try {
    const n = new Intl.DisplayNames([locale()], { type: "language" }).of(code);
    return n ? n.charAt(0).toUpperCase() + n.slice(1) : code;
  } catch {
    return code;
  }
}

/**
 * The language to show a page in: the chosen one (remembered per project),
 * else the interface language if the project has it, else the main language.
 */
export function preferredLocale(project: { slug: string; mainLocale: string; locales: string[] }): string {
  const all = [project.mainLocale, ...project.locales];
  try {
    const chosen = localStorage.getItem(`phx.lang.${project.slug}`);
    if (chosen && all.includes(chosen)) return chosen;
  } catch {
    /* no storage */
  }
  const ui = locale();
  return all.includes(ui) ? ui : project.mainLocale;
}

export function rememberLocale(projectSlug: string, code: string) {
  try {
    localStorage.setItem(`phx.lang.${projectSlug}`, code);
  } catch {
    /* only for this visit */
  }
}
