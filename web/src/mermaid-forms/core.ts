// The engine behind the Mermaid diagram forms: every diagram kind describes
// its form as fields and (nested) lists and brings a parser and a writer
// for its syntax. Lines a form does not know are kept as "further lines",
// so editing in the form never loses anything.

export type Row = Record<string, unknown>;
export interface Model extends Row {
  /** Lines the form does not know, written back unchanged. */
  extra?: string;
}

export interface FieldDef {
  key: string;
  /** i18n key. */
  label: string;
  type?: "text" | "number" | "select" | "check" | "lines";
  /** select: [value, i18n key or plain text]. */
  options?: [string, string][];
  /** select: show the value before the label (symbols like "<|--"). */
  showValue?: boolean;
  placeholder?: string;
  /** Width in a row: s (short), m (default), l (takes the rest). */
  size?: "s" | "m" | "l";
  /** Suggestions (e.g. the ids of the nodes for an edge). */
  suggest?: (m: Model) => string[];
  /** Shown only when this holds for the row (or the model). */
  show?: (row: Row) => boolean;
}

export interface ListDef {
  key: string;
  label: string;
  /** i18n key of the add button. */
  add: string;
  fields: FieldDef[];
  /** A list inside each item (e.g. tasks of a section). */
  children?: ListDef;
  hint?: string;
  /** A new item. */
  make?: (m: Model) => Row;
}

export interface MermaidKind {
  id: string;
  /** i18n key of the name. */
  label: string;
  /** Matches the first line of the diagram (the keyword). */
  detect: RegExp;
  example: string;
  hint?: string;
  fields?: FieldDef[];
  lists?: ListDef[];
  /** body: the lines after the keyword line; head: the keyword line itself. */
  parse(body: string[], head: string): Model;
  /** The whole diagram from its keyword line on. */
  write(m: Model): string;
}

/** Front matter, init directives and comments before the keyword stay as they are. */
export function splitSource(src: string): { pre: string[]; head: string; body: string[] } {
  const lines = src.replace(/\r/g, "").split("\n");
  const pre: string[] = [];
  let i = 0;
  if (lines[0]?.trim() === "---") {
    pre.push(lines[0]);
    for (i = 1; i < lines.length; i++) {
      pre.push(lines[i]);
      if (lines[i].trim() === "---") { i++; break; }
    }
  }
  for (; i < lines.length; i++) {
    const l = lines[i].trim();
    if (l === "" || l.startsWith("%%")) pre.push(lines[i]);
    else break;
  }
  while (pre.length && pre[pre.length - 1].trim() === "") pre.pop();
  return { pre, head: (lines[i] ?? "").trim(), body: lines.slice(i + 1) };
}

// ---- helpers for parsers and writers ----

export const IND = "    ";

export const s = (v: unknown): string => (v === undefined || v === null ? "" : String(v));

/** Removes surrounding double quotes. */
export function unq(v: string): string {
  const t = v.trim();
  return t.length >= 2 && t.startsWith('"') && t.endsWith('"') ? t.slice(1, -1) : t;
}

/** Quotes a text for Mermaid when it has spaces or special characters. */
export function q(v: unknown, always = false): string {
  const t = s(v).replace(/"/g, "'");
  return always || t === "" || /[\s:;,()[\]{}<>|#%&-]/.test(t) ? `"${t}"` : t;
}

/** Splits at commas that are not inside quotes or brackets. */
export function splitArgs(v: string): string[] {
  const out: string[] = [];
  let cur = "", depth = 0, inQ = false;
  for (const ch of v) {
    if (ch === '"') inQ = !inQ;
    else if (!inQ && "([{".includes(ch)) depth++;
    else if (!inQ && ")]}".includes(ch)) depth--;
    if (ch === "," && !inQ && depth === 0) {
      out.push(cur.trim());
      cur = "";
    } else cur += ch;
  }
  if (cur.trim() !== "" || out.length) out.push(cur.trim());
  return out;
}

/** The further lines of a model as lines (empty ones dropped). */
export function extraLines(m: Model, indent = IND): string[] {
  return s(m.extra).split("\n").filter((l) => l.trim() !== "").map((l) => indent + l.trim());
}

/** Collects a line the form does not know. */
export function keep(extra: string[], line: string) {
  if (line.trim()) extra.push(line.trim());
}

export const rows = (m: Row, key: string): Row[] => (Array.isArray(m[key]) ? (m[key] as Row[]) : []);

/** The indentation width of a line (tabs count as four). */
export function indentOf(line: string): number {
  const m = /^[ \t]*/.exec(line)![0];
  return m.replace(/\t/g, "    ").length;
}

/** Levels from indentation (for trees like mindmaps): a stack of widths. */
export function levels(lines: string[]): { level: number; text: string }[] {
  const out: { level: number; text: string }[] = [];
  const stack: number[] = [];
  for (const raw of lines) {
    if (!raw.trim() || raw.trim().startsWith("%%")) continue;
    const w = indentOf(raw);
    while (stack.length && stack[stack.length - 1] >= w) stack.pop();
    stack.push(w);
    out.push({ level: stack.length - 1, text: raw.trim() });
  }
  return out;
}

/** Lines inside a block (from "{" or a keyword to its "}" / "end"), for keeping whole blocks. */
export function takeBlock(lines: string[], i: number, open: RegExp, close: RegExp): number {
  let depth = 0;
  for (let k = i; k < lines.length; k++) {
    const l = lines[k].trim();
    if (open.test(l)) depth++;
    if (close.test(l)) depth--;
    if (depth <= 0) return k;
  }
  return lines.length - 1;
}
