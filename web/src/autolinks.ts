// Auto-links: references in page text become links when a page is shown
// (decision numbers, ext refs, §6.4 sections and the project's own rules).
// The text itself stays unchanged.
import { api } from "./api";

export interface AutoLinkRule { pattern: string; to: "url" | "ref"; url?: string; note?: string }
export interface AutoLinkRef { href: string; title: string }
export interface AutoLinks { rules: AutoLinkRule[]; refs: Record<string, AutoLinkRef> }

export interface Hit { start: number; end: number; href: string; title: string }

const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** A matcher over plain text: built-in references, sections and own rules. */
export function matcher(al: AutoLinks, sections: (n: string) => string | undefined) {
  const refs = Object.keys(al.refs).sort((a, b) => b.length - a.length);
  const builtIn = refs.length ? new RegExp(`(?<![\\w-])(?:${refs.map(esc).join("|")})(?![\\w-])`, "g") : null;
  const own: { re: RegExp; rule: AutoLinkRule }[] = [];
  for (const rule of al.rules) {
    try {
      own.push({ re: new RegExp(rule.pattern, "g"), rule });
    } catch {
      /* the server checks patterns; skip one this browser cannot read */
    }
  }
  return (text: string): Hit[] => {
    const hits: Hit[] = [];
    const add = (h: Hit) => {
      if (h.end > h.start && !hits.some((o) => h.start < o.end && o.start < h.end)) hits.push(h);
    };
    if (builtIn) for (const m of text.matchAll(builtIn)) {
      const r = al.refs[m[0]];
      add({ start: m.index!, end: m.index! + m[0].length, href: r.href, title: r.title });
    }
    for (const m of text.matchAll(/§\s?(\d+(?:\.\d+)*)/g)) {
      const id = sections(m[1]);
      if (id) add({ start: m.index!, end: m.index! + m[0].length, href: `#${id}`, title: "" });
    }
    for (const { re, rule } of own) {
      for (const m of text.matchAll(re)) {
        if (!m[0]) continue;
        let href = "";
        if (rule.to === "ref") href = al.refs[m[0]]?.href ?? "";
        else href = (rule.url ?? "").replace(/\$(\d)/g, (_, i) => encodeURIComponent(m[Number(i)] ?? ""));
        if (/^(https:\/\/|\/)/.test(href)) add({ start: m.index!, end: m.index! + m[0].length, href, title: rule.note ?? al.refs[m[0]]?.title ?? "" });
      }
    }
    return hits.sort((a, b) => a.start - b.start);
  };
}

const SKIP = new Set(["A", "CODE", "PRE", "SCRIPT", "STYLE", "TEXTAREA", "BUTTON", "H1", "H2", "H3", "H4", "H5", "H6"]);

/** Links references inside a rendered page. */
export function applyAutoLinks(root: HTMLElement, al: AutoLinks) {
  if (!al.rules.length && !Object.keys(al.refs).length && !/§/.test(root.textContent ?? "")) return;
  // Headings that start with a section number ("6.4 Backups") are targets of §6.4.
  const heads = new Map<string, string>();
  root.querySelectorAll<HTMLElement>("h1, h2, h3, h4, h5, h6").forEach((h) => {
    const n = (h.textContent ?? "").trim().match(/^(\d+(?:\.\d+)*)\.?\s/);
    if (!n || heads.has(n[1])) return;
    if (!h.id) h.id = `sec-${n[1].replace(/\./g, "-")}`;
    heads.set(n[1], h.id);
  });
  const find = matcher(al, (n) => heads.get(n));
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(n) {
      for (let p = n.parentElement; p && p !== root; p = p.parentElement) {
        if (SKIP.has(p.tagName) || p.tagName.includes("-")) return NodeFilter.FILTER_REJECT;
      }
      return n.nodeValue && n.nodeValue.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT;
    },
  });
  const nodes: Text[] = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) nodes.push(n as Text);
  for (const node of nodes) {
    const text = node.nodeValue ?? "";
    const hits = find(text);
    if (!hits.length) continue;
    const frag = document.createDocumentFragment();
    let at = 0;
    for (const h of hits) {
      frag.append(text.slice(at, h.start));
      const a = document.createElement("a");
      a.href = h.href;
      a.className = "autolink";
      if (h.title) a.title = h.title;
      if (h.href.startsWith("https://")) {
        a.target = "_blank";
        a.rel = "noopener external";
      }
      a.textContent = text.slice(h.start, h.end);
      frag.append(a);
      at = h.end;
    }
    frag.append(text.slice(at));
    node.replaceWith(frag);
  }
}

const cache = new Map<string, Promise<AutoLinks>>();

/** The auto-links of a project (cached for this visit; reset after changes). */
export function projectAutoLinks(project: string, fresh = false): Promise<AutoLinks> {
  if (fresh || !cache.has(project)) cache.set(project, api<AutoLinks>("GET", `/projects/${project}/autolinks`).catch(() => ({ rules: [], refs: {} })));
  return cache.get(project)!;
}
