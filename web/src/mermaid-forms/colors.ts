// Colours for single blocks of a Mermaid diagram. The palette names follow
// the whiteboard tokens (--tb-* fill, --tc-* stroke); other colours are hex.
// Flowchart nodes get "classDef phx-<name>" + "class <ids> phx-<name>" (with
// the light colours, so other renderers show them too; Phoedox swaps in the
// tokens for dark mode) or "style <id> fill:#…". Gantt tasks have no classes:
// their bars are coloured with CSS rules in the front matter (themeCSS).

export const PALETTE = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"] as const;
export type PaletteName = (typeof PALETTE)[number];

// Light values of the tokens (styles/tokens.css), the fallback outside Phoedox.
const FILL: Record<PaletteName, string> = {
  gray: "#ebe7e3", red: "#f9dedb", orange: "#fbe4d4", yellow: "#f8ecbf", green: "#dcefe1", blue: "#dde6f8", purple: "#ebe0f6", pink: "#f7dde9",
};
const STROKE: Record<PaletteName, string> = {
  gray: "#6b6159", red: "#b4372f", orange: "#b84a0c", yellow: "#8a6a00", green: "#2c8a4b", blue: "#2f5bc0", purple: "#7a3eb8", pink: "#b0306f",
};

export const isPalette = (c: string): c is PaletteName => (PALETTE as readonly string[]).includes(c);
export const isHex = (c: string) => /^#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?$/.test(c);
/** A colour the forms accept: a palette name or a hex value. */
export const colorOk = (c: unknown): c is string => typeof c === "string" && (isPalette(c) || isHex(c));

/** The CSS fill and stroke of a colour (tokens with the light value as fallback). */
export function colorCss(c: string): { fill: string; stroke: string } {
  if (isPalette(c)) return { fill: `var(--tb-${c}, ${FILL[c]})`, stroke: `var(--tc-${c}, ${STROKE[c]})` };
  return { fill: c, stroke: c };
}

/** Black or white, whichever reads better on a hex colour. */
export function textOn(hex: string): string {
  const h = hex.length === 4 ? hex.replace(/^#(.)(.)(.)$/, "#$1$1$2$2$3$3") : hex;
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.179 ? "#000000" : "#ffffff";
}

/** The text colour on a block of colour c. */
export function textCss(c: string): string {
  return isPalette(c) ? `var(--tc-${c}, ${STROKE[c]})` : textOn(c);
}

/** A colour as a hex value (for the colour picker and other renderers). */
export function colorHex(c: string, part: "fill" | "stroke" = "fill"): string {
  if (isPalette(c)) return (part === "fill" ? FILL : STROKE)[c];
  return c;
}

/** CSS for the shadow root of a rendered diagram (svg id: scope): palette
 * classes in the current theme. The id beats Mermaid's own classDef rules. */
export function paletteCss(scope: string): string {
  return PALETTE.map((c) =>
    `#${scope} .node.phx-${c} :is(rect,polygon,circle,ellipse,path){fill:var(--tb-${c})!important;stroke:var(--tc-${c})!important}`).join("");
}

// ---- flowcharts ----

const CLASS_DEF = /^classDef\s+phx-([a-z]+)\s/;
const CLASS_USE = /^class\s+([^\s]+)\s+phx-([a-z]+)$/;
const STYLE = /^style\s+([^\s]+)\s+fill:\s*(#[0-9a-fA-F]{3,6})\s*,\s*stroke:\s*(#[0-9a-fA-F]{3,6})\s*(?:,\s*color:\s*#[0-9a-fA-F]{3,6}\s*)?$/;

/** Reads a colour statement written by the form; null when the line is something else. */
export function readFlowColor(line: string): { ids: string[]; color?: string } | null {
  const l = line.trim().replace(/;\s*$/, "");
  if (CLASS_DEF.test(l) && isPalette(CLASS_DEF.exec(l)![1])) return { ids: [] };
  let m = CLASS_USE.exec(l);
  if (m && isPalette(m[2])) return { ids: m[1].split(",").map((x) => x.trim()).filter(Boolean), color: m[2] };
  m = STYLE.exec(l);
  if (m && m[2].toLowerCase() === m[3].toLowerCase()) return { ids: [m[1]], color: m[2].toLowerCase() };
  return null;
}

/** The colour statements for nodes { id, color }. */
export function writeFlowColors(nodes: { id: string; color?: unknown }[], indent: string): string[] {
  const byName = new Map<PaletteName, string[]>();
  const styles: string[] = [];
  for (const n of nodes) {
    const c = typeof n.color === "string" ? n.color : "";
    if (!n.id || !colorOk(c)) continue;
    if (isPalette(c)) byName.set(c, [...(byName.get(c) ?? []), n.id]);
    else styles.push(`${indent}style ${n.id} fill:${c},stroke:${c},color:${textOn(c)}`);
  }
  const used = PALETTE.filter((c) => byName.has(c));
  return [
    ...used.map((c) => `${indent}classDef phx-${c} fill:${FILL[c]},stroke:${STROKE[c]}`),
    ...used.map((c) => `${indent}class ${byName.get(c)!.join(",")} phx-${c}`),
    ...styles,
  ];
}

// ---- front matter (themeCSS) ----

/** The themeCSS value in front matter lines (a double-quoted or plain one-line value). */
export function readThemeCss(pre: string[]): string {
  const fm = frontMatter(pre);
  if (!fm) return "";
  for (let i = fm.start + 1; i < fm.end; i++) {
    const m = /^\s*themeCSS:\s*(.*)$/.exec(pre[i]);
    if (!m) continue;
    const v = m[1].trim();
    if (v.startsWith('"')) {
      try { return JSON.parse(v) as string; } catch { return v.slice(1, -1); }
    }
    if (v.startsWith("'") && v.endsWith("'")) return v.slice(1, -1).replace(/''/g, "'");
    return v;
  }
  return "";
}

/** Lines with themeCSS set to css (removed when empty); adds front matter and "config:" as needed. */
export function writeThemeCss(pre: string[], css: string): string[] {
  const out = [...pre];
  const val = `themeCSS: ${JSON.stringify(css)}`;
  let fm = frontMatter(out);
  if (!fm) {
    if (!css) return out;
    out.unshift("---", "config:", `  ${val}`, "---");
    return out;
  }
  for (let i = fm.start + 1; i < fm.end; i++) {
    const m = /^(\s*)themeCSS:/.exec(out[i]);
    if (!m) continue;
    if (css) out[i] = m[1] + val;
    else out.splice(i, 1);
    fm = frontMatter(out)!;
    // Drop an empty "config:" and empty front matter.
    const cfg = out.findIndex((l, k) => k > fm!.start && k < fm!.end && /^config:\s*$/.test(l));
    if (cfg >= 0 && (cfg + 1 >= fm.end || !/^\s+\S/.test(out[cfg + 1]))) { out.splice(cfg, 1); fm = frontMatter(out)!; }
    if (fm.end === fm.start + 1) out.splice(fm.start, 2);
    return out;
  }
  if (!css) return out;
  const cfg = out.findIndex((l, k) => k > fm!.start && k < fm!.end && /^config:\s*$/.test(l));
  if (cfg >= 0) out.splice(cfg + 1, 0, `  ${val}`);
  else out.splice(fm.end, 0, "config:", `  ${val}`);
  return out;
}

function frontMatter(pre: string[]): { start: number; end: number } | null {
  if (pre[0]?.trim() !== "---") return null;
  const end = pre.findIndex((l, i) => i > 0 && l.trim() === "---");
  return end > 0 ? { start: 0, end } : null;
}

// ---- Gantt bars ----

const BAR_RULE = /rect\[id\$='-([^']+)'\]\s*\{\s*fill:\s*([^;!]+?)\s*!important;\s*stroke:\s*([^;!]+?)\s*!important;?\s*\}\s*(?:text\[id\$='-\1-text'\]\s*\{\s*fill:\s*[^;!]+?\s*!important;?\s*\}\s*)?/g;

/** Splits themeCSS into the bar colours of the form (task id → colour) and the rest. */
export function readBarColors(css: string): { colors: Record<string, string>; raw: Record<string, string>; rest: string } {
  const colors: Record<string, string> = {};
  const raw: Record<string, string> = {};
  const rest = css.replace(BAR_RULE, (_, id: string, fill: string) => {
    const tok = /^var\(--tb-([a-z]+)/.exec(fill);
    const c = tok ? tok[1] : fill.trim().toLowerCase();
    if (!colorOk(c)) return _;
    colors[id] = c;
    raw[id] = _.trim();
    return "";
  });
  return { colors, raw, rest: rest.trim() };
}

/** themeCSS with the bar colours (task id → colour) before the rest. */
export function writeBarColors(colors: Record<string, string>, rest: string): string {
  const rules = Object.entries(colors).filter(([id, c]) => /^[\w-]+$/.test(id) && colorOk(c)).map(([id, c]) => {
    const { fill, stroke } = colorCss(c);
    return `rect[id$='-${id}'] { fill: ${fill} !important; stroke: ${stroke} !important; } text[id$='-${id}-text'] { fill: ${textCss(c)} !important; }`;
  });
  return [...rules, rest.trim()].filter(Boolean).join(" ");
}
