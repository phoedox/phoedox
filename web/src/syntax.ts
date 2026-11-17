// Markdown extensions of Phoedox pages (ADR-0031), shared by the renderer
// (md.ts) and the editor (editor/markdown.ts).

/** Colors of text and highlights; CSS comes from the tokens --tc-* / --tb-*. */
export const COLORS = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"] as const;
export type Color = (typeof COLORS)[number];
export const isColor = (c: string | undefined): c is Color => !!c && (COLORS as readonly string[]).includes(c);

/** [text]{color=red bg=yellow} */
export const COLOR_SPAN = /^\[((?:\\.|[^\]\\\n])+)\]\{((?:color|bg)=[a-z]+(?: (?:color|bg)=[a-z]+)?)\}/;

export function parseColorAttrs(s: string): { color?: Color; bg?: Color } {
  const out: { color?: Color; bg?: Color } = {};
  for (const part of s.split(" ")) {
    const [k, v] = part.split("=");
    if ((k === "color" || k === "bg") && isColor(v)) out[k] = v;
  }
  return out;
}

export function colorAttrs(a: { color?: string; bg?: string }): string {
  return [a.color && `color=${a.color}`, a.bg && `bg=${a.bg}`].filter(Boolean).join(" ");
}

/** ++underlined++ */
export const UNDERLINE = /^\+\+(?=\S)((?:\\.|[^\\\n])*?\S)\+\+/;

/** $inline math$: no space inside at the edges, no digit right after (so "$5 and $10" stays text). */
export const INLINE_MATH = /^\$(?=[^\s$])((?:\\.|[^$\\\n])*?[^\s\\])\$(?!\d)/;

/** <time datetime="…" data-kind="…">fallback</time> */
export const TIME = /^<time datetime="([0-9T:\-+.Z]+)"(?: data-kind="(date|time|datetime|countdown)")?>([^<]*)<\/time>/;
export type TimeKind = "date" | "time" | "datetime" | "countdown";

/** Callouts: GitHub alerts and our own with icon and color. Kind and attribute
 * names ignore case ("> [!NOTE]" as on GitHub); the editor writes them lower case.
 * Like Obsidian, "-" or "+" right after the bracket makes a callout foldable
 * (closed or open) and text after it is its title: "> [!faq]- Why?".
 * "> [!tab] Windows" callouts that follow each other form a tab group;
 * "> [!steps] Title" holds an ordered list readers tick off. */
export const CALLOUT_START = /^> \[!([A-Za-z]+)((?: [A-Za-z]+=[A-Za-z0-9-]+)*)\]([+-])?(?:[ \t]+(\S.*?))?\s*$/;
export const CALLOUT_PRESETS: Record<string, { icon: string; color: Color }> = {
  note: { icon: "Info", color: "blue" },
  tip: { icon: "Lightbulb", color: "green" },
  important: { icon: "MessageSquareWarning", color: "purple" },
  warning: { icon: "TriangleAlert", color: "orange" },
  caution: { icon: "OctagonAlert", color: "red" },
  callout: { icon: "Info", color: "gray" },
  faq: { icon: "CircleHelp", color: "gray" },
  tab: { icon: "PanelTop", color: "gray" },
  steps: { icon: "ListOrdered", color: "blue" },
};

export interface CalloutAttrs {
  kind: string; icon: string; color: Color; iconColor?: Color;
  /** "-" closed, "+" open: a foldable callout; "" not foldable. */
  fold?: "" | "+" | "-";
  /** The text after the marker: the title (of a tab, a foldable callout, steps). */
  title?: string;
  /** Steps: an id so readers' ticks survive edits of the text. */
  id?: string;
}

export function parseCallout(kind: string, attrs: string, fold = "", title = ""): CalloutAttrs | undefined {
  kind = kind.toLowerCase();
  const preset = CALLOUT_PRESETS[kind];
  if (!preset) return undefined;
  const out: CalloutAttrs = { kind, icon: preset.icon, color: preset.color };
  if (fold === "+" || fold === "-") out.fold = fold;
  if (title.trim()) out.title = title.trim();
  for (const part of attrs.trim().split(/\s+/).filter(Boolean)) {
    const [key, v] = part.split("=");
    const k = key.toLowerCase();
    if (k === "icon" && /^[A-Z][A-Za-z0-9]{1,40}$/.test(v)) out.icon = v;
    if (k === "color" && isColor(v.toLowerCase())) out.color = v.toLowerCase() as Color;
    if (k === "iconcolor" && isColor(v.toLowerCase())) out.iconColor = v.toLowerCase() as Color;
    if (k === "id" && /^[A-Za-z0-9-]{1,40}$/.test(v)) out.id = v;
  }
  return out;
}

/** The first line of a callout; presets without changes stay short ("> [!note]"). */
export function calloutHead(a: CalloutAttrs): string {
  const p = CALLOUT_PRESETS[a.kind] ?? CALLOUT_PRESETS.callout;
  const kind = CALLOUT_PRESETS[a.kind] ? a.kind : "callout";
  const parts = [kind];
  if (a.icon !== p.icon) parts.push(`icon=${a.icon}`);
  if (a.color !== p.color) parts.push(`color=${a.color}`);
  if (a.iconColor) parts.push(`iconcolor=${a.iconColor}`);
  if (a.id) parts.push(`id=${a.id}`);
  const title = (a.title ?? "").replace(/\s+/g, " ").trim();
  return `> [!${parts.join(" ")}]${a.fold ?? ""}${title ? " " + title : ""}`;
}

/** A short random id for blocks that keep state per reader (steps, polls, quizzes). */
export function blockId(): string {
  return Math.random().toString(36).slice(2, 10);
}

/** Fenced blocks with their own rendering. */
export const FENCES = ["math", "embed", "iframe", "board", "whiteboard", "app", "mermaid", "chart", "poll", "calc", "quiz", "beforeafter", "annotate", "tree", "rack", "floorplan", "compare", "dbmodel", "status", "entity", "include", "query", "map"] as const;
export type FenceKind = (typeof FENCES)[number];
