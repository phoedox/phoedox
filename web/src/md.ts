import DOMPurify from "dompurify";
import { Marked } from "marked";

// Same rule as the server (internal/graph): a mention starts the text or
// follows a character that cannot be part of an e-mail address; code is
// never searched.
const mentionAt = /^@(?:([a-z0-9-]+)\/)?([a-z0-9]+(?:-[a-z0-9]+)*)/;
const mentionStart = /(^|[^\w@./-])@[a-z0-9]/g;

const escape = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

// Request blocks (```request or ```http) become interactive elements; the
// index matches the server's numbering (internal/requests.Blocks).
let requestIndex = 0;
let context = { project: "", doc: "" };
const marked = new Marked({
  // Mentions as an inline token: code spans and blocks keep "@x" as text.
  extensions: [{
    name: "mention",
    level: "inline",
    start(src: string) {
      mentionStart.lastIndex = 0;
      const m = mentionStart.exec(src);
      return m ? m.index + m[1].length : undefined;
    },
    tokenizer(src: string) {
      const m = mentionAt.exec(src);
      if (m) return { type: "mention", raw: m[0], project: m[1] ?? "", slug: m[2] };
      return undefined;
    },
    renderer(tok: any) {
      const p = tok.project || context.project;
      return `<a href="/p/${escape(p)}/e/${escape(tok.slug)}" title="mention">@${tok.project ? escape(tok.project) + "/" : ""}${escape(tok.slug)}</a>`;
    },
  }],
  renderer: {
    code({ text, lang }) {
      const kind = (lang ?? "").split(/\s/)[0];
      if (kind !== "request" && kind !== "http") return false;
      const i = requestIndex++;
      return `<phx-request-block data-index="${i}" data-project="${escape(context.project)}" data-doc="${escape(context.doc)}"><pre>${escape(text)}</pre></phx-request-block>`;
    },
  },
});

/** Renders Markdown to HTML without sanitising (for tests; use renderMarkdown). */
export function markdownHTML(md: string, project: string, doc = ""): string {
  requestIndex = 0;
  context = { project, doc };
  return marked.parse(md ?? "", { async: false }) as string;
}

/** Renders Markdown to sanitised HTML. */
export function renderMarkdown(md: string, project: string, doc = ""): string {
  const html = markdownHTML(md, project, doc);
  return DOMPurify.sanitize(html, { ADD_ATTR: ["target"], ADD_TAGS: ["phx-request-block"] });
}

/** Drops a leading "# Heading" that only repeats the page title. */
export function withoutTitle(md: string, title: string): string {
  const m = /^\s*#[ \t]+(.+?)[ \t#]*(?:\n|$)/.exec(md ?? "");
  const norm = (s: string) => s.replace(/[*_`]/g, "").trim().toLowerCase();
  return m && norm(m[1]) === norm(title) ? md.slice(m[0].length).replace(/^\s*\n/, "") : md;
}
