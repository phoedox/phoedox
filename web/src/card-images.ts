/** Images in card descriptions: Markdown images pointing to project files. */
const IMG = /!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g;

/** The first image of a description, for the card's cover. Only images
 * stored in Phoedox (/api/v1/files/…) are used, never external URLs. */
export function coverImage(md: string): string | undefined {
  for (const m of md.matchAll(IMG)) {
    if (m[2].startsWith("/api/v1/files/")) return m[2];
  }
  return undefined;
}

/** Markdown for an uploaded image; brackets in the name are dropped. */
export const imageMarkdown = (name: string, url: string) => `![${name.replace(/[[\]]/g, "")}](${url})`;

/** Inserts text at the cursor of a textarea value, on its own line. */
export function insertAt(value: string, start: number, end: number, text: string): { value: string; cursor: number } {
  const before = value.slice(0, start);
  const after = value.slice(end);
  const pre = before && !before.endsWith("\n") ? "\n" : "";
  const post = after && !after.startsWith("\n") ? "\n" : "";
  const out = before + pre + text + post + after;
  return { value: out, cursor: (before + pre + text).length };
}

/** Image files of a paste or drop. Pasted screenshots have no name. */
export function imageFiles(list: FileList | null | undefined): File[] {
  return [...(list ?? [])].filter((f) => f.type.startsWith("image/"));
}
