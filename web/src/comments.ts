/** Comment threads on pages (see internal/core/comments.go). */
export interface PageComment {
  id: string;
  documentId: string;
  parentId?: string;
  anchor?: string;
  body: string;
  mentions: string[];
  authorId: string;
  authorName?: string;
  authorAvatar?: string;
  resolvedBy?: string;
  resolvedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface CommentThread extends PageComment {
  replies: PageComment[];
}

export interface Mentionable { id: string; name: string; email: string }

/** Collapses whitespace like the server does for anchors. */
export function normalizeText(s: string): string {
  return s.replace(/\s+/g, " ").trim();
}

/** The quote stored for a block: its text, shortened. */
export function anchorOf(text: string, max = 160): string {
  const t = normalizeText(text);
  return t.length > max ? t.slice(0, max) : t;
}

/** Index of the block a thread belongs to, or -1 when the text changed. */
export function blockFor(anchor: string, blocks: string[]): number {
  const a = normalizeText(anchor);
  if (!a) return -1;
  const exact = blocks.findIndex((b) => normalizeText(b).startsWith(a));
  return exact >= 0 ? exact : blocks.findIndex((b) => normalizeText(b).includes(a.slice(0, 40)));
}

/** The "@word" being typed right before the cursor, if any. */
export function mentionQuery(text: string, cursor: number): { start: number; query: string } | null {
  const m = /(^|\s)@([\p{L}\p{N}._-]*)$/u.exec(text.slice(0, cursor));
  return m ? { start: cursor - m[2].length - 1, query: m[2] } : null;
}

/** People whose "@Name" is still in the text. */
export function mentionedIds(text: string, picked: Map<string, string>): string[] {
  return [...picked].filter(([name]) => text.includes(`@${name}`)).map(([, id]) => id);
}

/** Splits a comment into text and "@Name" parts for the people it mentions. */
export function splitMentions(body: string, names: string[]): { text: string; mention: boolean }[] {
  const list = names.filter(Boolean).sort((a, b) => b.length - a.length);
  if (!list.length) return [{ text: body, mention: false }];
  const re = new RegExp(`@(${list.map((n) => n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})`, "g");
  const out: { text: string; mention: boolean }[] = [];
  let last = 0;
  for (const m of body.matchAll(re)) {
    if (m.index! > last) out.push({ text: body.slice(last, m.index), mention: false });
    out.push({ text: m[0], mention: true });
    last = m.index! + m[0].length;
  }
  if (last < body.length) out.push({ text: body.slice(last), mention: false });
  return out;
}
