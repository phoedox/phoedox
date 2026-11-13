import DOMPurify from "dompurify";
import { Marked } from "marked";

// Same rule as the server (internal/graph): a mention starts the text or
// follows a character that cannot be part of an e-mail address.
const mentionRe = /(^|[^\w@./-])@(?:([a-z0-9-]+)\/)?([a-z0-9]+(?:-[a-z0-9]+)*)/g;

/** Replaces @mentions with links to the entity page. */
export function linkMentions(md: string, project: string): string {
  return md.replace(mentionRe, (_m, pre: string, proj: string | undefined, slug: string) => {
    const p = proj ?? project;
    return `${pre}[@${proj ? proj + "/" : ""}${slug}](/p/${p}/e/${slug} "mention")`;
  });
}

const escape = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

// Request blocks (```request or ```http) become interactive elements; the
// index matches the server's numbering (internal/requests.Blocks).
let requestIndex = 0;
let context = { project: "", doc: "" };
const marked = new Marked({
  renderer: {
    code({ text, lang }) {
      const kind = (lang ?? "").split(/\s/)[0];
      if (kind !== "request" && kind !== "http") return false;
      const i = requestIndex++;
      return `<phx-request-block data-index="${i}" data-project="${escape(context.project)}" data-doc="${escape(context.doc)}"><pre>${escape(text)}</pre></phx-request-block>`;
    },
  },
});

/** Renders Markdown to sanitised HTML. */
export function renderMarkdown(md: string, project: string, doc = ""): string {
  requestIndex = 0;
  context = { project, doc };
  // Mentions are not linked inside code; request blocks are code fences.
  const html = marked.parse(linkMentions(md ?? "", project), { async: false }) as string;
  return DOMPurify.sanitize(html, { ADD_ATTR: ["target"], ADD_TAGS: ["phx-request-block"] });
}
