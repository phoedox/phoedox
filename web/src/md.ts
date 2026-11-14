import DOMPurify from "dompurify";
import { Marked, type Tokens } from "marked";
import { iconSVG } from "./icon";
import { CALLOUT_START, COLOR_SPAN, INLINE_MATH, TIME, UNDERLINE, parseCallout, parseColorAttrs } from "./syntax";

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
/** First index in src where re (unanchored) matches, for marked's start(). */
const startAt = (src: string, re: RegExp) => {
  const i = src.search(re);
  return i < 0 ? undefined : i;
};

const marked = new Marked({
  // Mentions as an inline token: code spans and blocks keep "@x" as text.
  extensions: [{
    // > [!note] / > [!callout icon=User color=orange] (ADR-0031)
    name: "callout",
    level: "block",
    start: (src: string) => startAt(src, /^> \[!/m),
    tokenizer(src: string) {
      const lines = src.split("\n");
      const m = CALLOUT_START.exec(lines[0]);
      const attrs = m && parseCallout(m[1], m[2]);
      if (!attrs) return undefined;
      let n = 1;
      while (n < lines.length && /^>( |$)/.test(lines[n])) n++;
      const inner = lines.slice(1, n).map((l) => l.replace(/^> ?/, "")).join("\n");
      const raw = lines.slice(0, n).join("\n") + (n < lines.length ? "\n" : "");
      return { type: "callout", raw, attrs, tokens: this.lexer.blockTokens(inner, []) };
    },
    renderer(tok: any) {
      const a = tok.attrs;
      const ic = a.iconColor ? ` ic-${a.iconColor}` : "";
      return `<div class="callout cb-${a.color}" data-kind="${escape(a.kind)}"><span class="ci${ic}">${iconSVG(a.icon, 18)}</span><div class="cc">${this.parser.parse(tok.tokens)}</div></div>`;
    },
  }, {
    name: "colorspan",
    level: "inline",
    start: (src: string) => startAt(src, /\[[^\]\n]+\]\{(?:color|bg)=/),
    tokenizer(src: string) {
      const m = COLOR_SPAN.exec(src);
      if (!m) return undefined;
      return { type: "colorspan", raw: m[0], attrs: parseColorAttrs(m[2]), tokens: this.lexer.inlineTokens(m[1]) };
    },
    renderer(tok: any) {
      const cls = [tok.attrs.color && `tc-${tok.attrs.color}`, tok.attrs.bg && `tb-${tok.attrs.bg}`].filter(Boolean).join(" ");
      return `<span class="${cls}">${this.parser.parseInline(tok.tokens)}</span>`;
    },
  }, {
    name: "underline",
    level: "inline",
    start: (src: string) => startAt(src, /\+\+\S/),
    tokenizer(src: string) {
      const m = UNDERLINE.exec(src);
      return m ? { type: "underline", raw: m[0], tokens: this.lexer.inlineTokens(m[1]) } : undefined;
    },
    renderer(tok: any) {
      return `<u>${this.parser.parseInline(tok.tokens)}</u>`;
    },
  }, {
    name: "imath",
    level: "inline",
    start: (src: string) => startAt(src, /\$[^\s$]/),
    tokenizer(src: string) {
      const m = INLINE_MATH.exec(src);
      return m ? { type: "imath", raw: m[0], tex: m[1] } : undefined;
    },
    renderer(tok: any) {
      return `<phx-math data-tex="${escape(tok.tex)}">${escape(tok.tex)}</phx-math>`;
    },
  }, {
    name: "time",
    level: "inline",
    start: (src: string) => startAt(src, /<time datetime=/),
    tokenizer(src: string) {
      const m = TIME.exec(src);
      return m ? { type: "time", raw: m[0], datetime: m[1], kind: m[2] ?? "date", text: m[3] } : undefined;
    },
    renderer(tok: any) {
      return `<phx-time datetime="${escape(tok.datetime)}" data-kind="${tok.kind}">${escape(tok.text)}</phx-time>`;
    },
  }, {
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
    code({ text, lang }: Tokens.Code) {
      const kind = (lang ?? "").split(/\s/)[0];
      const value = text.trim();
      if (kind === "math") return `<phx-math data-tex="${escape(value)}" data-display="1"><pre>${escape(value)}</pre></phx-math>`;
      if (kind === "embed" || kind === "iframe") return `<phx-embed data-url="${escape(value)}" data-kind="${kind}"><a href="${escape(value)}">${escape(value)}</a></phx-embed>`;
      if (kind === "mermaid") return `<phx-mermaid><pre>${escape(value)}</pre></phx-mermaid>`;
      if (kind === "dbmodel") return `<phx-dbmodel><pre>${escape(value)}</pre></phx-dbmodel>`;
      if (kind === "status") return `<phx-status data-project="${escape(context.project)}"><pre>${escape(value)}</pre></phx-status>`;
      if (kind === "entity") return `<phx-entity-card data-project="${escape(context.project)}" data-src="${escape(value)}"></phx-entity-card>`;
      if (kind === "map") return `<phx-map-block data-src="${escape(value)}" data-project="${escape(context.project)}"><pre>${escape(value)}</pre></phx-map-block>`;
      if (kind === "include") return `<phx-include data-page="${escape(value)}" data-project="${escape(context.project)}"></phx-include>`;
      if (kind === "query") return `<phx-query-block data-query="${escape(value)}" data-project="${escape(context.project)}"></phx-query-block>`;
      if (kind === "board") return `<phx-board-embed data-board="${escape(value)}" data-project="${escape(context.project)}"></phx-board-embed>`;
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
  return DOMPurify.sanitize(html, { ADD_ATTR: ["target"], ADD_TAGS: ["phx-request-block", "phx-math", "phx-embed", "phx-time", "phx-board-embed", "phx-mermaid", "phx-dbmodel", "phx-status", "phx-entity-card", "phx-include", "phx-query-block", "phx-map-block"] });
}

/** Drops a leading "# Heading" that only repeats the page title. */
export function withoutTitle(md: string, title: string): string {
  const m = /^\s*#[ \t]+(.+?)[ \t#]*(?:\n|$)/.exec(md ?? "");
  const norm = (s: string) => s.replace(/[*_`]/g, "").trim().toLowerCase();
  return m && norm(m[1]) === norm(title) ? md.slice(m[0].length).replace(/^\s*\n/, "") : md;
}
