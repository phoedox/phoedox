/** Helpers of the projects page. */

/** "2 hours ago" style text in the given locale. */
export function relativeTime(iso: string, now: number, locale: string): string {
  const diff = (new Date(iso).getTime() - now) / 1000;
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const abs = Math.abs(diff);
  if (abs < 60) return rtf.format(0, "minute");
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  if (abs < 86400 * 30) return rtf.format(Math.round(diff / 86400), "day");
  if (abs < 86400 * 365) return rtf.format(Math.round(diff / (86400 * 30)), "month");
  return rtf.format(Math.round(diff / (86400 * 365)), "year");
}

/** Projects whose name, slug or description contain every word of q. */
export function matchProjects<T extends { name: string; slug: string; description: string }>(list: T[], q: string): T[] {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return list;
  return list.filter((p) => {
    const hay = `${p.name} ${p.slug} ${p.description}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

export interface Tile { kind: "due" | "suggestions" | "recent"; href: string; title: string; sub: string; icon: string }

/** At most four tiles: due tasks, then pending suggestions, then recently opened. */
export function continueTiles(due: number, suggestions: { project: string; name: string; count: number }[],
  recent: Tile[], text: { due: (n: number) => string; dueSub: string; sugg: (n: number) => string }): Tile[] {
  const out: Tile[] = [];
  if (due > 0) out.push({ kind: "due", href: "/tasks", title: text.due(due), sub: text.dueSub, icon: "CalendarDays" });
  for (const s of suggestions.slice(0, 2)) out.push({ kind: "suggestions", href: `/p/${s.project}/suggestions`, title: text.sugg(s.count), sub: s.name, icon: "Bot" });
  for (const r of recent) {
    if (out.length >= 4) break;
    out.push(r);
  }
  return out.slice(0, 4);
}
