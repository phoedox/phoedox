/** A recently opened page or entry (GET /me/recent). */
export interface RecentItem {
  kind: "document" | "entity";
  id: string;
  slug: string;
  title: string;
  type: string;
  projectSlug: string;
  projectName: string;
  viewedAt: string;
}

const docIcons: Record<string, string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot" };

export const recentHref = (r: RecentItem) => `/p/${r.projectSlug}/${r.kind === "entity" ? "e" : "d"}/${r.slug}`;
export const recentIcon = (r: RecentItem) => (r.kind === "entity" ? "Box" : docIcons[r.type] ?? "FileText");

/** Remembers that a page or entry was opened (fire and forget). */
export function recordView(project: string, kind: "document" | "entity", ref: string) {
  void fetch("/api/v1/me/recent", {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/json", "X-Phoedox": "1" },
    body: JSON.stringify({ project, kind, ref }),
  }).catch(() => {});
}
