// Editor nodes for the page building blocks of ADR-0031 and their Markdown
// transformers. Rendering reuses the elements in components/blocks.ts.

import { $convertFromMarkdownString, $convertToMarkdownString, type MultilineElementTransformer, type TextFormatTransformer, type TextMatchTransformer, type Transformer } from "@lexical/markdown";
import {
  $applyNodeReplacement, $isTextNode, DecoratorNode, ElementNode, TextNode, type EditorConfig, type LexicalNode, type NodeKey,
  type SerializedElementNode, type SerializedLexicalNode, type Spread,
} from "lexical";
import { iconSVG } from "../icon";
import {
  CALLOUT_START, COLOR_SPAN, colorAttrs, calloutHead, isColor, parseCallout, parseColorAttrs, type CalloutAttrs, type Color, type FenceKind, type TimeKind,
} from "../syntax";

// ---------- Callout ----------

type SerializedCallout = Spread<{ kind: string; icon: string; color: Color; iconColor?: Color }, SerializedElementNode>;

const iconMask = (name: string) => `url("data:image/svg+xml;utf8,${encodeURIComponent(iconSVG(name, 24).replace('stroke="currentColor"', 'stroke="black"'))}")`;

/** A callout box; its children are paragraphs, lists, … like a table cell. */
export class CalloutNode extends ElementNode {
  __kind: string;
  __icon: string;
  __color: Color;
  __iconColor?: Color;

  static getType() { return "callout"; }
  static clone(n: CalloutNode) { return new CalloutNode({ kind: n.__kind, icon: n.__icon, color: n.__color, iconColor: n.__iconColor }, n.__key); }
  static importJSON(j: SerializedCallout) { return $createCalloutNode({ kind: j.kind, icon: j.icon, color: j.color, iconColor: j.iconColor }); }

  constructor(a: CalloutAttrs = { kind: "note", icon: "Info", color: "blue" }, key?: NodeKey) {
    super(key);
    this.__kind = a.kind;
    this.__icon = a.icon;
    this.__color = a.color;
    this.__iconColor = a.iconColor;
  }

  exportJSON(): SerializedCallout {
    return { ...super.exportJSON(), type: "callout", version: 1, kind: this.__kind, icon: this.__icon, color: this.__color, iconColor: this.__iconColor };
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
  }

  updateDOM(prev: CalloutNode, dom: HTMLElement): boolean {
    if (prev.__icon !== this.__icon || prev.__color !== this.__color || prev.__kind !== this.__kind || prev.__iconColor !== this.__iconColor) this.paint(dom);
    return false;
  }

  attrs(): CalloutAttrs {
    const n = this.getLatest();
    return { kind: n.__kind, icon: n.__icon, color: n.__color, iconColor: n.__iconColor };
  }

  setAttrs(a: Partial<CalloutAttrs>) {
    const w = this.getWritable();
    if (a.kind) w.__kind = a.kind;
    if (a.icon) w.__icon = a.icon;
    if (a.color) w.__color = a.color;
    w.__iconColor = "iconColor" in a ? a.iconColor : w.__iconColor;
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
    const attrs = parseCallout(startMatch[1], startMatch[2] ?? "");
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
    } else if (this.__kind === "board") {
      el = document.createElement("phx-board-embed");
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
  regExpStart: /^```(math|embed|iframe|board)\s*$/,
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

export const BLOCK_NODES = [CalloutNode, FenceNode, TimeNode, InlineMathNode];
