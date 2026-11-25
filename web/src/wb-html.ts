// HTML screens of prototypes on the board: drawn as a still picture (an SVG
// image with the HTML in a foreignObject). Pictures never run scripts or
// load anything, so the board shows the design without running it; playing
// runs it in a sandboxed frame (player/proto-player.js).

const THEME_VARS = ["bg", "surface", "surface2", "fg", "muted", "line", "accent", "accent-soft", "on-color", "good", "bad", "warn", "font-body", "font-display", "font-mono"];
const PAL = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"];

const cache = new Map<string, string>();

function themeVars(): string {
  const cs = getComputedStyle(document.documentElement);
  return [...THEME_VARS, ...PAL.flatMap((c) => [`tc-${c}`, `tb-${c}`])]
    .map((v) => { const x = cs.getPropertyValue(`--${v}`).trim(); return x ? `--${v}:${x};` : ""; }).join("");
}

const truthy = (v: unknown) => (Array.isArray(v) ? v.length > 0 : !!v && v !== "false" && v !== "0");

/** Whether a data-show condition holds ("var", "!var", "var=value"). */
export function holds(cond: string, vars: Record<string, unknown>): boolean {
  let c = cond.trim();
  if (!c) return true;
  const neg = c.startsWith("!");
  if (neg) c = c.slice(1);
  const i = c.indexOf("=");
  const r = i < 0 ? truthy(vars[c]) : String(vars[c.slice(0, i)] ?? "") === c.slice(i + 1);
  return neg ? !r : r;
}

/** Removes what could run or load something (a picture would ignore it anyway) and shows the variables' start values. */
export function stillMarkup(html: string, vars: Record<string, unknown> = {}): { css: string; body: string } {
  const doc = new DOMParser().parseFromString(html, "text/html");
  doc.querySelectorAll<HTMLElement>("[data-show]").forEach((el) => { if (!holds(el.dataset.show ?? "", vars)) el.remove(); });
  doc.querySelectorAll<HTMLElement>("[data-text]").forEach((el) => {
    const v = vars[el.dataset.text ?? ""];
    if (v !== undefined) el.textContent = Array.isArray(v) ? v.join(", ") : String(v);
  });
  doc.querySelectorAll("script, iframe, object, embed, link, meta, base, noscript").forEach((n) => n.remove());
  for (const el of doc.querySelectorAll("*")) {
    for (const a of [...el.attributes]) {
      const v = a.value.trim().toLowerCase();
      if (a.name.startsWith("on") || ((a.name === "src" || a.name === "href" || a.name === "xlink:href") && !v.startsWith("data:") && !v.startsWith("#"))) el.removeAttribute(a.name);
    }
  }
  const css = [...doc.querySelectorAll("style")].map((s) => { const t = s.textContent ?? ""; s.remove(); return t; }).join("\n");
  const ser = new XMLSerializer();
  const body = ser.serializeToString(doc.body);
  return { css, body };
}

/** A data: URL of the HTML drawn at w×h in the current theme. */
export function htmlScreenImage(html: string, w: number, h: number, state: Record<string, unknown> = {}): string {
  const vars = themeVars();
  const key = `${w}x${h}|${vars}|${JSON.stringify(state)}|${html}`;
  const hit = cache.get(key);
  if (hit) return hit;
  const { css, body } = stillMarkup(html, state);
  const esc = (s: string) => s.replace(/]]>/g, "]]&gt;");
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><foreignObject x="0" y="0" width="${w}" height="${h}">` +
    `<div xmlns="http://www.w3.org/1999/xhtml" style="width:${w}px;height:${h}px;overflow:hidden">` +
    `<style><![CDATA[:root,body{${vars}}body{margin:0;min-height:100%;font-family:var(--font-body,system-ui,sans-serif);color:var(--fg);background:var(--surface);font-size:15px;line-height:1.45}[hidden]{display:none!important}${esc(css)}]]></style>` +
    `${body}</div></foreignObject></svg>`;
  const url = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
  if (cache.size > 64) cache.delete(cache.keys().next().value!);
  cache.set(key, url);
  return url;
}

// ---- pictures for server-side screenshots -------------------------------------

let fontCSS: Promise<string> | undefined;

/** The app's body and display fonts as @font-face rules with data: URLs (pictures cannot load files). */
function inlineFonts(): Promise<string> {
  fontCSS ??= (async () => {
    const cs = getComputedStyle(document.documentElement);
    const wanted = `${cs.getPropertyValue("--font-body")},${cs.getPropertyValue("--font-display")}`.toLowerCase();
    const rules: CSSFontFaceRule[] = [];
    for (const sheet of [...document.styleSheets, ...(document.adoptedStyleSheets ?? [])]) {
      let list: CSSRuleList;
      try { list = sheet.cssRules; } catch { continue; }
      for (const r of list) if (r instanceof CSSFontFaceRule) rules.push(r);
    }
    const out: string[] = [];
    for (const r of rules) {
      const fam = r.style.getPropertyValue("font-family").replace(/["']/g, "").trim();
      const url = /url\(["']?([^"')]+)["']?\)/.exec(r.style.getPropertyValue("src"))?.[1];
      if (!fam || !url || !wanted.includes(fam.toLowerCase())) continue;
      try {
        const b = await (await fetch(url)).blob();
        const data = await new Promise<string>((ok) => { const fr = new FileReader(); fr.onload = () => ok(String(fr.result)); fr.readAsDataURL(b); });
        out.push(`@font-face{font-family:"${fam}";src:url(${data}) format("woff2");font-weight:${r.style.getPropertyValue("font-weight") || "400"}}`);
      } catch { /* the picture falls back to system fonts */ }
    }
    return out.join("");
  })();
  return fontCSS;
}

/** Whether the app shows the dark theme now. */
export function darkTheme(): boolean {
  const t = document.documentElement.dataset.theme;
  return t === "dark" || (t !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
}

async function sha256hex(s: string): Promise<string> {
  const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/** The server's file name of a screen's picture (internal/core/screen_pictures.go). */
export async function screenPictureName(frameID: string, html: string, w: number, h: number, dark: boolean): Promise<string> {
  return `screen-${(await sha256hex(frameID)).slice(0, 8)}-${(await sha256hex(`${html}|${w}x${h}`)).slice(0, 16)}-${dark ? "dark" : "light"}.png`;
}

/** A PNG of an HTML screen as the board shows it, with the app's fonts. */
export async function screenPicturePNG(html: string, w: number, h: number, state: Record<string, unknown> = {}): Promise<Blob> {
  const fonts = await inlineFonts();
  const { css, body } = stillMarkup(html, state);
  const esc = (s: string) => s.replace(/]]>/g, "]]&gt;");
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><foreignObject x="0" y="0" width="${w}" height="${h}">` +
    `<div xmlns="http://www.w3.org/1999/xhtml" style="width:${w}px;height:${h}px;overflow:hidden">` +
    `<style><![CDATA[${esc(fonts)}:root,body{${themeVars()}}body{margin:0;min-height:100%;font-family:var(--font-body,system-ui,sans-serif);color:var(--fg);background:var(--surface);font-size:15px;line-height:1.45}[hidden]{display:none!important}${esc(css)}]]></style>` +
    `${body}</div></foreignObject></svg>`;
  const img = new Image();
  img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
  await img.decode();
  await new Promise((r) => setTimeout(r, 60)); // fonts inside the picture
  const c = document.createElement("canvas");
  c.width = Math.round(w);
  c.height = Math.round(h);
  const g = c.getContext("2d")!;
  g.fillStyle = getComputedStyle(document.documentElement).getPropertyValue("--surface").trim() || "#fff";
  g.fillRect(0, 0, c.width, c.height);
  g.drawImage(img, 0, 0, c.width, c.height);
  return new Promise((ok, fail) => c.toBlob((b) => (b ? ok(b) : fail(new Error("no picture"))), "image/png"));
}
