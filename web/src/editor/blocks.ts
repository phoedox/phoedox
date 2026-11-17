// Editor nodes for the page building blocks of ADR-0031 and their Markdown
// transformers. Rendering reuses the elements in components/blocks.ts.

import { blockDef } from "../blocks";
import { $convertFromMarkdownString, $convertToMarkdownString, type MultilineElementTransformer, type TextFormatTransformer, type TextMatchTransformer, type Transformer } from "@lexical/markdown";
import {
  $applyNodeReplacement, $isTextNode, DecoratorNode, ElementNode, TextNode, type EditorConfig, type LexicalNode, type NodeKey,
  type SerializedElementNode, type SerializedLexicalNode, type Spread,
} from "lexical";
import { iconSVG } from "../icon";
import { renderMarkdown } from "../md";
import {
  CALLOUT_START, COLOR_SPAN, colorAttrs, calloutHead, isColor, parseCallout, parseColorAttrs, type CalloutAttrs, type Color, FENCES, type FenceKind, type TimeKind,
} from "../syntax";

// ---------- Callout ----------

type SerializedCallout = Spread<{ kind: string; icon: string; color: Color; iconColor?: Color; fold?: "" | "+" | "-"; title?: string; id?: string }, SerializedElementNode>;

const iconMask = (name: string) => `url("data:image/svg+xml;utf8,${encodeURIComponent(iconSVG(name, 24).replace('stroke="currentColor"', 'stroke="black"'))}")`;

/** A callout box; its children are paragraphs, lists, … like a table cell. */
export class CalloutNode extends ElementNode {
  __kind: string;
  __icon: string;
  __color: Color;
  __iconColor?: Color;
  __fold: "" | "+" | "-";
  __title: string;
  __id: string;

  static getType() { return "callout"; }
  static clone(n: CalloutNode) { return new CalloutNode(n.attrsOf(), n.__key); }
  static importJSON(j: SerializedCallout) { return $createCalloutNode({ kind: j.kind, icon: j.icon, color: j.color, iconColor: j.iconColor, fold: j.fold, title: j.title, id: j.id }); }

  constructor(a: CalloutAttrs = { kind: "note", icon: "Info", color: "blue" }, key?: NodeKey) {
    super(key);
    this.__kind = a.kind;
    this.__icon = a.icon;
    this.__color = a.color;
    this.__iconColor = a.iconColor;
    this.__fold = a.fold ?? "";
    this.__title = a.title ?? "";
    this.__id = a.id ?? "";
  }

  private attrsOf(): CalloutAttrs {
    return { kind: this.__kind, icon: this.__icon, color: this.__color, iconColor: this.__iconColor, fold: this.__fold, title: this.__title || undefined, id: this.__id || undefined };
  }

  exportJSON(): SerializedCallout {
    return { ...super.exportJSON(), type: "callout", version: 1, kind: this.__kind, icon: this.__icon, color: this.__color, iconColor: this.__iconColor, fold: this.__fold, title: this.__title, id: this.__id };
  }

  createDOM(): HTMLElement {
    const div = document.createElement("div");
    this.paint(div);
    return div;
  }

  private paint(div: HTMLElement) {
    div.className = `callout ed-callout cb-${this.__color}${this.__iconColor ? ` icx-${this.__iconColor}` : ""}`;
    div.dataset.kind = this.__kind;
    div.style.setProperty("--ci", iconMask(this.__icon));
    // The title and whether it folds show above the content (edited in the callout settings).
    const head = this.__title || (this.__fold ? this.__kind : "");
    if (head) div.dataset.title = (this.__fold === "-" ? "▸ " : this.__fold === "+" ? "▾ " : "") + head;
    else delete div.dataset.title;
  }

  updateDOM(prev: CalloutNode, dom: HTMLElement): boolean {
    if (prev.__icon !== this.__icon || prev.__color !== this.__color || prev.__kind !== this.__kind || prev.__iconColor !== this.__iconColor
      || prev.__fold !== this.__fold || prev.__title !== this.__title) this.paint(dom);
    return false;
  }

  attrs(): CalloutAttrs {
    return this.getLatest().attrsOf();
  }

  setAttrs(a: Partial<CalloutAttrs>) {
    const w = this.getWritable();
    if (a.kind) w.__kind = a.kind;
    if (a.icon) w.__icon = a.icon;
    if (a.color) w.__color = a.color;
    w.__iconColor = "iconColor" in a ? a.iconColor : w.__iconColor;
    if ("fold" in a) w.__fold = a.fold ?? "";
    if ("title" in a) w.__title = (a.title ?? "").replace(/\s+/g, " ").trimStart();
    if (a.id) w.__id = a.id;
    return w;
  }

  isShadowRoot() { return true; }
  canBeEmpty() { return false; }
}

export function $createCalloutNode(a: CalloutAttrs) {
  return $applyNodeReplacement(new CalloutNode(a));
}
export const $isCalloutNode = (n: LexicalNode | null | undefined): n is CalloutNode => n instanceof CalloutNode;

/** Set by markdown.ts to the full list, so callouts can hold any block. */
let inner: Transformer[] = [];
export function setInnerTransformers(t: Transformer[]) {
  inner = t;
}

export const CALLOUT: MultilineElementTransformer = {
  dependencies: [CalloutNode],
  type: "multiline-element",
  regExpStart: CALLOUT_START,
  handleImportAfterStartMatch({ lines, rootNode, startLineIndex, startMatch }) {
    const attrs = parseCallout(startMatch[1], startMatch[2] ?? "", startMatch[3] ?? "", startMatch[4] ?? "");
    if (!attrs) return null;
    let end = startLineIndex + 1;
    while (end < lines.length && /^>( |$)/.test(lines[end])) end++;
    const body = lines.slice(startLineIndex + 1, end).map((l) => l.replace(/^> ?/, "")).join("\n");
    const node = $createCalloutNode(attrs);
    $convertFromMarkdownString(body, inner, node, false, true);
    rootNode.append(node);
    return [true, end - 1];
  },
  replace: () => false,
  export(node) {
    if (!$isCalloutNode(node)) return null;
    const body = $convertToMarkdownString(inner, node).split("\n").map((l) => (l ? `> ${l}` : ">")).join("\n");
    return `${calloutHead(node.attrs())}\n${body}`;
  },
};

// ---------- Fenced blocks: math, embed, iframe, board ----------

type SerializedFence = Spread<{ kind: FenceKind; source: string }, SerializedLexicalNode>;

/** A fenced block shown as its rendering; "edit" asks the host to change the source. */
export class FenceNode extends DecoratorNode<null> {
  __kind: FenceKind;
  __source: string;

  static getType() { return "fence"; }
  static clone(n: FenceNode) { return new FenceNode(n.__kind, n.__source, n.__key); }
  static importJSON(j: SerializedFence) { return $createFenceNode(j.kind, j.source); }

  constructor(kind: FenceKind = "math", source = "", key?: NodeKey) {
    super(key);
    this.__kind = kind;
    this.__source = source;
  }

  exportJSON(): SerializedFence {
    return { ...super.exportJSON(), type: "fence", version: 1, kind: this.__kind, source: this.__source };
  }

  createDOM(config: EditorConfig): HTMLElement {
    const div = document.createElement("div");
    div.className = "ed-fence";
    div.contentEditable = "false";
    this.fill(div, config);
    return div;
  }

  private fill(div: HTMLElement, config: EditorConfig) {
    div.replaceChildren();
    const project = (config.theme as { project?: string }).project ?? "";
    const src = this.__source.trim();
    let el: HTMLElement;
    if (this.__kind === "math") {
      el = document.createElement("phx-math");
      el.dataset.tex = src;
      el.dataset.display = "1";
    } else if (this.__kind === "mermaid") {
      el = document.createElement("phx-mermaid");
      el.dataset.src = src;
    } else if (this.__kind === "compare") {
      el = document.createElement("phx-compare");
      el.dataset.src = src;
    } else if (blockDef(this.__kind)) {
      el = document.createElement("phx-block");
      el.dataset.kind = this.__kind;
      el.dataset.src = src;
      el.dataset.project = project;
    } else if (this.__kind === "dbmodel") {
      el = document.createElement("phx-dbmodel");
      el.dataset.src = src;
    } else if (this.__kind === "status" || this.__kind === "entity") {
      el = document.createElement(this.__kind === "status" ? "phx-status" : "phx-entity-card");
      el.dataset.src = src;
      el.dataset.project = project;
    } else if (this.__kind === "map") {
      el = document.createElement("phx-map-block");
      el.dataset.src = src;
      el.dataset.project = project;
    } else if (this.__kind === "include") {
      el = document.createElement("phx-include");
      el.dataset.page = src;
      el.dataset.project = project;
      el.dataset.nested = "1"; // the editor shows a link; the page view shows the content
    } else if (this.__kind === "query") {
      el = document.createElement("phx-query-block");
      el.dataset.query = src;
      el.dataset.project = project;
    } else if (this.__kind === "app") {
      el = document.createElement("phx-app-embed");
      el.dataset.app = src;
      el.dataset.project = project;
    } else if (this.__kind === "board" || this.__kind === "whiteboard") {
      el = document.createElement(this.__kind === "board" ? "phx-board-embed" : "phx-whiteboard-embed");
      el.dataset.board = src;
      el.dataset.project = project;
    } else {
      el = document.createElement("phx-embed");
      el.dataset.url = src;
      el.dataset.kind = this.__kind;
    }
    const edit = document.createElement("button");
    edit.type = "button";
    edit.className = "ed-fence-edit";
    edit.textContent = "</>";
    edit.dataset.key = this.getKey();
    div.append(el, edit);
  }

  updateDOM(prev: FenceNode, dom: HTMLElement, config: EditorConfig): boolean {
    if (prev.__kind !== this.__kind || prev.__source !== this.__source) this.fill(dom, config);
    return false;
  }

  getKind() { return this.getLatest().__kind; }
  getSource() { return this.getLatest().__source; }
  setSource(s: string) {
    this.getWritable().__source = s;
  }
  isInline() { return false; }
  decorate() { return null; }
}

export function $createFenceNode(kind: FenceKind, source: string) {
  return $applyNodeReplacement(new FenceNode(kind, source));
}
export const $isFenceNode = (n: LexicalNode | null | undefined): n is FenceNode => n instanceof FenceNode;

export const FENCE: MultilineElementTransformer = {
  dependencies: [FenceNode],
  type: "multiline-element",
  regExpStart: new RegExp("^```(" + FENCES.join("|") + ")\\s*$"),
  regExpEnd: /^```\s*$/,
  replace(rootNode, _children, startMatch, _endMatch, linesInBetween, isImport) {
    const node = $createFenceNode(startMatch[1] as FenceKind, (linesInBetween ?? []).join("\n").trim());
    if (isImport) rootNode.append(node);
    else rootNode.replace(node);
  },
  export(node) {
    return $isFenceNode(node) ? "```" + node.getKind() + "\n" + node.getSource() + "\n```" : null;
  },
};

// ---------- Raw Markdown ----------

type SerializedRaw = Spread<{ source: string }, SerializedLexicalNode>;

/** Markdown the editor cannot represent (HTML, footnotes, …): shown as in
 * the page, edited as source, saved exactly as it was. Only this block is
 * source; the rest of the page stays rich. */
export class RawNode extends DecoratorNode<null> {
  __source: string;
  static getType() { return "rawmd"; }
  static clone(n: RawNode) { return new RawNode(n.__source, n.__key); }
  static importJSON(j: SerializedRaw) { return $createRawNode(j.source); }

  constructor(source = "", key?: NodeKey) {
    super(key);
    this.__source = source;
  }

  exportJSON(): SerializedRaw {
    return { ...super.exportJSON(), type: "rawmd", version: 1, source: this.__source };
  }

  createDOM(config: EditorConfig): HTMLElement {
    const div = document.createElement("div");
    div.className = "ed-fence ed-raw";
    div.contentEditable = "false";
    this.fill(div, config);
    return div;
  }

  private fill(div: HTMLElement, config: EditorConfig) {
    const project = (config.theme as { project?: string }).project ?? "";
    const body = document.createElement("div");
    body.className = "ed-raw-body";
    body.innerHTML = renderMarkdown(this.__source, project); // sanitized like pages
    const edit = document.createElement("button");
    edit.type = "button";
    edit.className = "ed-fence-edit";
    edit.textContent = "</> Markdown";
    edit.dataset.raw = this.getKey();
    div.replaceChildren(body, edit);
  }

  updateDOM(prev: RawNode, dom: HTMLElement, config: EditorConfig): boolean {
    if (prev.__source !== this.__source) this.fill(dom, config);
    return false;
  }

  getSource() { return this.getLatest().__source; }
  setSource(s: string) {
    this.getWritable().__source = s;
  }
  getTextContent() { return this.__source; }
  isInline() { return false; }
  decorate() { return null; }
}

export function $createRawNode(source: string) {
  return $applyNodeReplacement(new RawNode(source));
}
export const $isRawNode = (n: LexicalNode | null | undefined): n is RawNode => n instanceof RawNode;

/** Marker lines around raw blocks; they exist only while a page is imported. */
export const RAW_OPEN = "<!--phx-raw-->", RAW_CLOSE = "<!--/phx-raw-->";

export const RAW: MultilineElementTransformer = {
  dependencies: [RawNode],
  type: "multiline-element",
  regExpStart: /^<!--phx-raw-->$/,
  regExpEnd: /^<!--\/phx-raw-->$/,
  replace(rootNode, _children, _start, _end, linesInBetween, isImport) {
    const node = $createRawNode((linesInBetween ?? []).join("\n"));
    if (isImport) rootNode.append(node);
    else rootNode.replace(node);
  },
  export(node) {
    return $isRawNode(node) ? node.getSource() : null;
  },
};

// ---------- Inline: date/time and math ----------

type SerializedTime = Spread<{ datetime: string; kind: TimeKind; text: string }, SerializedLexicalNode>;

export class TimeNode extends DecoratorNode<null> {
  __datetime: string;
  __kind: TimeKind;
  __text: string;

  static getType() { return "phxtime"; }
  static clone(n: TimeNode) { return new TimeNode(n.__datetime, n.__kind, n.__text, n.__key); }
  static importJSON(j: SerializedTime) { return $createTimeNode(j.datetime, j.kind, j.text); }

  constructor(datetime = "", kind: TimeKind = "date", text = "", key?: NodeKey) {
    super(key);
    this.__datetime = datetime;
    this.__kind = kind;
    this.__text = text;
  }

  exportJSON(): SerializedTime {
    return { ...super.exportJSON(), type: "phxtime", version: 1, datetime: this.__datetime, kind: this.__kind, text: this.__text };
  }

  createDOM(): HTMLElement {
    const span = document.createElement("span");
    span.className = "ed-time";
    const el = document.createElement("phx-time");
    el.setAttribute("datetime", this.__datetime);
    el.dataset.kind = this.__kind;
    el.textContent = this.__text;
    span.append(el);
    return span;
  }

  updateDOM(prev: TimeNode): boolean {
    return prev.__datetime !== this.__datetime || prev.__kind !== this.__kind;
  }

  markdown() {
    const n = this.getLatest();
    return `<time datetime="${n.__datetime}"${n.__kind === "date" ? "" : ` data-kind="${n.__kind}"`}>${n.__text.replace(/[<>]/g, "")}</time>`;
  }

  getTextContent() { return this.__text; }
  isInline() { return true; }
  decorate() { return null; }
}

export function $createTimeNode(datetime: string, kind: TimeKind, text: string) {
  return $applyNodeReplacement(new TimeNode(datetime, kind, text));
}

export const TIME_MATCH: TextMatchTransformer = {
  dependencies: [TimeNode],
  type: "text-match",
  importRegExp: /<time datetime="([0-9T:\-+.Z]+)"(?: data-kind="(date|time|datetime|countdown)")?>([^<]*)<\/time>/,
  regExp: /<time datetime="([0-9T:\-+.Z]+)"(?: data-kind="(date|time|datetime|countdown)")?>([^<]*)<\/time>$/,
  replace(node, m) {
    node.replace($createTimeNode(m[1], (m[2] ?? "date") as TimeKind, m[3]));
  },
  export(node) {
    return node instanceof TimeNode ? node.markdown() : null;
  },
};

type SerializedIMath = Spread<{ tex: string }, SerializedLexicalNode>;

export class InlineMathNode extends DecoratorNode<null> {
  __tex: string;
  static getType() { return "imath"; }
  static clone(n: InlineMathNode) { return new InlineMathNode(n.__tex, n.__key); }
  static importJSON(j: SerializedIMath) { return $createInlineMathNode(j.tex); }

  constructor(tex = "", key?: NodeKey) {
    super(key);
    this.__tex = tex;
  }

  exportJSON(): SerializedIMath {
    return { ...super.exportJSON(), type: "imath", version: 1, tex: this.__tex };
  }

  createDOM(): HTMLElement {
    const span = document.createElement("span");
    span.className = "ed-imath";
    const el = document.createElement("phx-math");
    el.dataset.tex = this.__tex;
    span.append(el);
    return span;
  }

  updateDOM(prev: InlineMathNode): boolean {
    return prev.__tex !== this.__tex;
  }

  getTex() { return this.getLatest().__tex; }
  setTex(t: string) {
    this.getWritable().__tex = t;
  }
  getTextContent() { return `$${this.__tex}$`; }
  isInline() { return true; }
  decorate() { return null; }
}

export function $createInlineMathNode(tex: string) {
  return $applyNodeReplacement(new InlineMathNode(tex));
}

const IMATH_RE = /\$(?=[^\s$])((?:\\.|[^$\\\n])*?[^\s\\])\$(?!\d)/;
export const IMATH_MATCH: TextMatchTransformer = {
  dependencies: [InlineMathNode],
  type: "text-match",
  importRegExp: IMATH_RE,
  regExp: new RegExp(IMATH_RE.source + "$"),
  trigger: "$",
  replace(node, m) {
    node.replace($createInlineMathNode(m[1]));
  },
  export(node) {
    return node instanceof InlineMathNode ? `$${node.getTex()}$` : null;
  },
};

// ---------- Colors and underline ----------

const STYLE_COLOR = /(?:^|;)\s*color:\s*var\(--tc-([a-z]+)\)/;
const STYLE_BG = /(?:^|;)\s*background-color:\s*var\(--tb-([a-z]+)\)/;

export function colorsOfStyle(style: string): { color?: Color; bg?: Color } {
  const c = STYLE_COLOR.exec(style)?.[1];
  const b = STYLE_BG.exec(style)?.[1];
  return { color: isColor(c) ? c : undefined, bg: isColor(b) ? b : undefined };
}

export function styleOfColors(a: { color?: string; bg?: string }): string {
  return [a.color && `color: var(--tc-${a.color})`, a.bg && `background-color: var(--tb-${a.bg})`].filter(Boolean).join("; ");
}

export const COLOR_MATCH: TextMatchTransformer = {
  dependencies: [TextNode],
  type: "text-match",
  importRegExp: new RegExp(COLOR_SPAN.source.slice(1)),
  regExp: new RegExp(COLOR_SPAN.source.slice(1) + "$"),
  replace(node, m) {
    const a = parseColorAttrs(m[2]);
    // The text inside may carry inline formats (**bold** …): import them too.
    const tmp = $createCalloutNode({ kind: "callout", icon: "Info", color: "gray" });
    $convertFromMarkdownString(m[1].replace(/\\([\]\\])/g, "$1"), INLINE, tmp);
    const parts = (tmp.getFirstChild() as ElementNode | null)?.getChildren() ?? [];
    const style = styleOfColors(a);
    let last: LexicalNode = node;
    for (const p of parts) {
      if ($isTextNode(p)) p.setStyle(style);
      last.insertAfter(p);
      last = p;
    }
    node.remove();
    return parts.find($isTextNode) as TextNode | undefined;
  },
  export(node, _children, exportFormat) {
    if (!$isTextNode(node)) return null;
    const a = colorsOfStyle(node.getStyle());
    if (!a.color && !a.bg) return null;
    const text = node.getTextContent().replace(/([\]\\])/g, "\\$1");
    return `[${exportFormat(node, text)}]{${colorAttrs(a)}}`;
  },
};

export const UNDERLINE_FORMAT: TextFormatTransformer = { format: ["underline"], tag: "++", type: "text-format" };

/** Inline transformers used inside colored spans and table cells (set by markdown.ts). */
let INLINE: Transformer[] = [];
export function setInlineTransformers(t: Transformer[]) {
  INLINE = t;
}

// ---------- Page links [[ref]] / [[ref|label]] ----------

type SerializedPageLink = Spread<{ ref: string; label: string }, SerializedLexicalNode>;

/** A link to a page or decision; kept as one piece so it saves as [[ref]]. */
export class PageLinkNode extends DecoratorNode<null> {
  __ref: string;
  __label: string;
  static getType() { return "pagelink"; }
  static clone(n: PageLinkNode) { return new PageLinkNode(n.__ref, n.__label, n.__key); }
  static importJSON(j: SerializedPageLink) { return $createPageLinkNode(j.ref, j.label); }

  constructor(ref = "", label = "", key?: NodeKey) {
    super(key);
    this.__ref = ref;
    this.__label = label;
  }

  exportJSON(): SerializedPageLink {
    return { ...super.exportJSON(), type: "pagelink", version: 1, ref: this.__ref, label: this.__label };
  }

  createDOM(): HTMLElement {
    const span = document.createElement("span");
    span.className = "ed-pagelink";
    span.title = this.__ref;
    span.textContent = this.__label || this.__ref;
    return span;
  }

  updateDOM(prev: PageLinkNode): boolean {
    return prev.__ref !== this.__ref || prev.__label !== this.__label;
  }

  markdown() {
    const n = this.getLatest();
    return n.__label ? `[[${n.__ref}|${n.__label}]]` : `[[${n.__ref}]]`;
  }

  getTextContent() { return this.__label || this.__ref; }
  isInline() { return true; }
  decorate() { return null; }
}

export function $createPageLinkNode(ref: string, label = "") {
  return $applyNodeReplacement(new PageLinkNode(ref.trim(), label.trim()));
}

const PAGELINK_RE = /\[\[([^\]|\n]+?)(?:\|([^\]\n]+))?\]\]/;

export const PAGELINK_MATCH: TextMatchTransformer = {
  dependencies: [PageLinkNode],
  type: "text-match",
  importRegExp: PAGELINK_RE,
  regExp: new RegExp(PAGELINK_RE.source + "$"),
  trigger: "]",
  replace(node, m) {
    node.replace($createPageLinkNode(m[1], m[2] ?? ""));
  },
  export(node) {
    return node instanceof PageLinkNode ? node.markdown() : null;
  },
};

export const BLOCK_NODES = [CalloutNode, FenceNode, RawNode, TimeNode, InlineMathNode, PageLinkNode];
