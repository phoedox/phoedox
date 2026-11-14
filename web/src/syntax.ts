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

/** Callouts: GitHub alerts and our own with icon and color. */
export const CALLOUT_START = /^> \[!([a-z]+)((?: [a-z]+=[A-Za-z0-9-]+)*)\]\s*$/;
export const CALLOUT_PRESETS: Record<string, { icon: string; color: Color }> = {
  note: { icon: "Info", color: "blue" },
  tip: { icon: "Lightbulb", color: "green" },
  important: { icon: "MessageSquareWarning", color: "purple" },
  warning: { icon: "TriangleAlert", color: "orange" },
  caution: { icon: "OctagonAlert", color: "red" },
  callout: { icon: "Info", color: "gray" },
};

export interface CalloutAttrs { kind: string; icon: string; color: Color; iconColor?: Color }

export function parseCallout(kind: string, attrs: string): CalloutAttrs | undefined {
  const preset = CALLOUT_PRESETS[kind];
  if (!preset) return undefined;
  const out: CalloutAttrs = { kind, icon: preset.icon, color: preset.color };
  for (const part of attrs.trim().split(/\s+/).filter(Boolean)) {
    const [k, v] = part.split("=");
    if (k === "icon" && /^[A-Z][A-Za-z0-9]{1,40}$/.test(v)) out.icon = v;
    if (k === "color" && isColor(v)) out.color = v;
    if (k === "iconcolor" && isColor(v)) out.iconColor = v;
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
  return `> [!${parts.join(" ")}]`;
}

/** Fenced blocks with their own rendering. */
export const FENCES = ["math", "embed", "iframe", "board", "mermaid"] as const;
export type FenceKind = (typeof FENCES)[number];
