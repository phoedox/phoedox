// Annotated images: a photo with numbered markers and their list under it
// (a fuse box, the back of a router, a rack), in the style of the chart's
// numbered list. Markers sit at x, y in percent and may name entries with
// @slug. In the editor, markers are placed by clicking on the photo.
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, imageLine, keyValue, pickImage } from "./util";

const COLORS = ["gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"];

export interface Marker { x: number; y: number; text: string; color: string; entities: string[] }
export interface Annotated { title: string; image?: string; alt: string; markers: Marker[] }

export function parseAnnotated(src: string): { model?: Annotated; problems: BlockProblem[] } {
  const m: Annotated = { title: "", alt: "", markers: [] };
  const problems: BlockProblem[] = [];
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["title", "titel", "image", "bild"]);
    if (kv) {
      if (kv[0] === "title" || kv[0] === "titel") m.title = kv[1];
      else if (kv[1]) {
        const img = imageLine(kv[1]);
        if (!img.src) problems.push({ key: "image.err_src", params: { value: kv[1] } });
        m.image = img.src;
        m.alt = img.label;
      }
      continue;
    }
    const mk = /^[-*+]?\s*(\d+(?:[.,]\d+)?)\s*%?\s*[,;]\s*(\d+(?:[.,]\d+)?)\s*%?(?:\s+([a-z]+))?\s*:\s*(.*)$/i.exec(l);
    if (!mk) { problems.push({ key: "annotate.err_line", params: { line: l } }); continue; }
    const x = Number(mk[1].replace(",", ".")), y = Number(mk[2].replace(",", "."));
    if (x > 100 || y > 100) { problems.push({ key: "annotate.err_pos", params: { line: l } }); continue; }
    const color = (mk[3] ?? "").toLowerCase();
    if (color && !COLORS.includes(color)) { problems.push({ key: "annotate.err_color", params: { color } }); continue; }
    m.markers.push({ x, y, text: mk[4].trim(), color: color || "red", entities: [...mk[4].matchAll(/(?:^|[^\w@./-])@([a-z0-9]+(?:-[a-z0-9]+)*)/g)].map((e) => e[1]) });
  }
  if (!m.image && !m.markers.length) problems.push({ key: "annotate.err_image" });
  return problems.length ? { problems } : { model: m, problems };
}

/** A marker's text with @mentions as links to the entries. */
function markerText(text: string, project: string): string {
  return esc(text).replace(/(^|[^\w@./-])@([a-z0-9]+(?:-[a-z0-9]+)*)/g, (_, pre, slug) =>
    project ? `${pre}<a href="/p/${esc(project)}/e/${slug}" data-entity="${slug}">@${slug}</a>` : `${pre}@${slug}`);
}

const dot = (mk: Marker, n: number, cx: number, cy: number, r: number) =>
  `<circle cx="${cx}" cy="${cy}" r="${r}" style="fill:var(--tc-${mk.color});stroke:#fff" stroke-width="2"/><text x="${cx}" y="${cy + r * 0.38}" text-anchor="middle" style="font-size:${r * 1.05}px;font-weight:700;fill:#fff">${n}</text>`;

function annotatedSVG(m: Annotated, width: number, height: number): string {
  const top = m.title ? 30 : 0;
  const listH = Math.min(m.markers.length, 6) * 18 + 6;
  const ih = Math.max(60, height - top - listH);
  const markers = m.markers.map((mk, i) => dot(mk, i + 1, (mk.x / 100) * width, top + (mk.y / 100) * ih, 11)).join("");
  const list = m.markers.slice(0, 6).map((mk, i) => `<text x="4" y="${top + ih + 18 + i * 18}" style="font-size:12px;fill:var(--fg)"><tspan style="font-weight:700;fill:var(--tc-${mk.color})">${i + 1}</tspan>  ${esc(mk.text)}</text>`).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)">${m.title ? `<text x="4" y="20" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(m.title)}</text>` : ""}${m.image ? `<image href="${esc(m.image)}" x="0" y="${top}" width="${width}" height="${ih}" preserveAspectRatio="none"/>` : `<rect x="0" y="${top}" width="${width}" height="${ih}" style="fill:var(--surface2)"/>`}${markers}${list}</svg>`;
}

registerBlock<Annotated>({
  kind: "annotate",
  icon: "MapPinned",
  label: "slash.annotate",
  hint: "block.annotate_hint",
  example: "title: Sicherungskasten\nimage:\n- 22, 30: F1 Küche und @herd\n- 48, 30: F2 Bad\n- 74, 64 blue: FI-Schalter, monatlich testen",
  parse: parseAnnotated,
  svg: (m, s) => annotatedSVG(m, s.width, s.height ?? Math.round(s.width * 0.75)),
  board: { w: 520, h: 420 },
  async mount(host, m, ctx) {
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .blk { padding: 12px; }
      .stage { position: relative; line-height: 0; border-radius: 8px; overflow: hidden; background: var(--surface2); }
      .stage img { display: block; width: 100%; height: auto; }
      .stage.placing { cursor: crosshair; outline: 2px dashed var(--accent); outline-offset: -2px; }
      .mk { position: absolute; width: 26px; height: 26px; margin: -13px 0 0 -13px; border-radius: 50%; border: 2px solid #fff; color: #fff; font: 700 13px/22px var(--font-body, system-ui); text-align: center; box-shadow: 0 1px 4px rgb(0 0 0 / .4); padding: 0; transition: transform var(--dur, 150ms) var(--ease-out, ease-out); }
      .mk.on { transform: scale(1.25); }
      .empty { padding: 30px 12px; line-height: 1.4; display: flex; gap: 10px; justify-content: center; align-items: center; flex-wrap: wrap; }
      ol { margin: 10px 0 0; padding: 0; list-style: none; display: grid; gap: 4px; }
      li { display: flex; gap: 10px; align-items: baseline; padding: 4px 6px; border-radius: 6px; }
      li.on { background: var(--surface2); }
      li .n { flex: none; width: 22px; height: 22px; border-radius: 50%; color: #fff; font: 700 12px/22px var(--font-body, system-ui); text-align: center; }
      li a { color: var(--accent); }
      .foot { display: flex; gap: 8px; margin-top: 8px; align-items: center; flex-wrap: wrap; }
      .add { display: flex; gap: 6px; margin-top: 8px; }
      .add input { flex: 1; font: inherit; padding: 5px 8px; border: 1px solid var(--line); border-radius: 6px; background: var(--bg); color: var(--fg); }
      @media (prefers-reduced-motion: reduce) { .mk { transition: none; } }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    let placing = false;
    let pending: { x: number; y: number } | undefined;
    const fill = (c: string) => `var(--tc-${c})`;
    const draw = () => {
      const img = m.image
        ? `<img src="${esc(m.image)}" alt="${esc(m.alt || m.title)}" draggable="false">`
        : `<div class="empty"><span class="muted">${esc(t("annotate.no_image"))}</span>${ctx.edit && ctx.project ? `<button type="button" class="ghost" data-pick>${esc(t("annotate.pick"))}</button>` : ""}</div>`;
      box.innerHTML = `${m.title ? `<h3>${esc(m.title)}</h3>` : ""}
        <div class="stage${placing ? " placing" : ""}">${img}
          ${m.markers.map((mk, i) => `<button type="button" class="mk" data-i="${i}" style="left:${mk.x}%;top:${mk.y}%;background:${fill(mk.color)}" aria-label="${esc(`${i + 1}: ${mk.text}`)}">${i + 1}</button>`).join("")}
          ${pending ? `<span class="mk on" style="left:${pending.x}%;top:${pending.y}%;background:var(--accent)" aria-hidden="true">${m.markers.length + 1}</span>` : ""}
        </div>
        ${m.markers.length ? `<ol>${m.markers.map((mk, i) => `<li data-i="${i}"><span class="n" style="background:${fill(mk.color)}" aria-hidden="true">${i + 1}</span><span>${markerText(mk.text, ctx.project)}</span></li>`).join("")}</ol>` : ""}
        ${pending ? `<form class="add"><input name="t" required placeholder="${esc(t("annotate.text_for", { n: m.markers.length + 1 }))}" aria-label="${esc(t("annotate.text_for", { n: m.markers.length + 1 }))}"><button class="ghost">${esc(t("annotate.add"))}</button><button type="button" class="ghost" data-cancel>${esc(t("annotate.cancel"))}</button></form>` : ""}
        ${ctx.edit && m.image && !pending ? `<div class="foot"><button type="button" class="ghost" data-place aria-pressed="${placing}">${esc(t(placing ? "annotate.placing" : "annotate.place"))}</button>${placing ? `<span class="muted">${esc(t("annotate.place_hint"))}</span>` : ""}</div>` : ""}`;
      box.querySelector<HTMLInputElement>(".add input")?.focus();
    };
    const mark = (i: number | null) => {
      box.querySelectorAll("[data-i]").forEach((el) => el.classList.toggle("on", el.getAttribute("data-i") === String(i)));
    };
    box.addEventListener("pointerover", (e) => {
      const el = (e.target as Element).closest("[data-i]");
      mark(el ? Number(el.getAttribute("data-i")) : null);
    });
    box.addEventListener("focusin", (e) => {
      const el = (e.target as Element).closest(".mk[data-i]");
      if (el) mark(Number(el.getAttribute("data-i")));
    });
    box.addEventListener("click", async (e) => {
      const el = e.target as HTMLElement;
      const a = el.closest<HTMLAnchorElement>("a[data-entity]");
      if (a && !(e as MouseEvent).ctrlKey && !(e as MouseEvent).metaKey) {
        e.preventDefault();
        (await import("../state")).navigate(a.getAttribute("href")!);
        return;
      }
      if (el.closest("[data-pick]")) {
        const url = await pickImage(ctx.project, ctx.doc);
        if (!url || !ctx.edit) return;
        const src = ctx.source ?? "";
        ctx.edit(/^\s*(image|bild)\s*:/mi.test(src) ? src.replace(/^(\s*(?:image|bild)\s*:).*$/mi, `$1 ${url}`) : `image: ${url}\n${src}`);
        return;
      }
      if (el.closest("[data-place]")) { placing = !placing; draw(); return; }
      if (el.closest("[data-cancel]")) { pending = undefined; draw(); return; }
      if (placing && !pending && el.closest(".stage") && !el.closest(".mk")) {
        const r = box.querySelector(".stage")!.getBoundingClientRect();
        const ev = e as MouseEvent;
        pending = { x: Math.round(((ev.clientX - r.left) / r.width) * 1000) / 10, y: Math.round(((ev.clientY - r.top) / r.height) * 1000) / 10 };
        draw();
        return;
      }
      const mk = el.closest<HTMLElement>(".mk[data-i]");
      if (mk) {
        const li = box.querySelector<HTMLElement>(`li[data-i="${mk.dataset.i}"]`);
        li?.scrollIntoView({ block: "nearest", behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
        mark(Number(mk.dataset.i));
      }
    });
    box.addEventListener("submit", (e) => {
      e.preventDefault();
      const text = (box.querySelector<HTMLInputElement>(".add input")?.value ?? "").trim();
      if (!text || !pending || !ctx.edit) return;
      const line = `- ${pending.x}, ${pending.y}: ${text}`;
      ctx.edit(`${(ctx.source ?? "").replace(/\s+$/, "")}\n${line}`);
      pending = undefined;
      placing = false;
    });
    draw();
  },
});
