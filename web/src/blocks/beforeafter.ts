// Before/after: two photos on top of each other with a slider between them
// (a renovation, tidied-up cabling, damage and its repair). The slider is a
// range input, so it works with the keyboard; print shows both photos.
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, imageLine, keyValue, pickImage } from "./util";

export interface BeforeAfter { title: string; before?: string; after?: string; beforeLabel: string; afterLabel: string; start: number }

export function parseBeforeAfter(src: string): { model?: BeforeAfter; problems: BlockProblem[] } {
  const m: BeforeAfter = { title: "", beforeLabel: "", afterLabel: "", start: 50 };
  const problems: BlockProblem[] = [];
  const md: { src?: string; label: string }[] = [];
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["title", "titel", "before", "vorher", "after", "nachher", "start"]);
    if (kv) {
      const [k, v] = kv;
      if (k === "title" || k === "titel") m.title = v;
      else if (k === "start") m.start = Math.min(100, Math.max(0, Number(v.replace("%", "")) || 50));
      else if (v) {
        const img = imageLine(v);
        if (!img.src) problems.push({ key: "image.err_src", params: { value: v } });
        if (k === "before" || k === "vorher") { m.before = img.src; m.beforeLabel = img.label; } else { m.after = img.src; m.afterLabel = img.label; }
      }
      continue;
    }
    // Markdown images work too: the first is before, the second after.
    const im = /^!\[([^\]]*)\]\(([^)\s]+)\)$/.exec(l);
    if (im) {
      const img = imageLine(im[2]);
      if (!img.src) problems.push({ key: "image.err_src", params: { value: im[2] } });
      md.push({ src: img.src, label: im[1] });
      continue;
    }
    problems.push({ key: "beforeafter.err_line", params: { line: l } });
  }
  if (!m.before && md[0]) { m.before = md[0].src; m.beforeLabel ||= md[0].label; }
  if (!m.after && md[1]) { m.after = md[1].src; m.afterLabel ||= md[1].label; }
  return problems.length ? { problems } : { model: m, problems };
}

/** Both photos side by side (whiteboards, exports). */
function beforeAfterSVG(m: BeforeAfter, width: number, height: number): string {
  const top = m.title ? 30 : 0, w = (width - 10) / 2, h = height - top - 24;
  const pic = (src: string | undefined, x: number, label: string) =>
    (src ? `<image href="${esc(src)}" x="${x}" y="${top}" width="${w}" height="${h}" preserveAspectRatio="xMidYMid slice"/>` : `<rect x="${x}" y="${top}" width="${w}" height="${h}" rx="6" style="fill:var(--surface2)"/>`) +
    `<text x="${x + w / 2}" y="${top + h + 17}" text-anchor="middle" style="font-size:12px;fill:var(--muted)">${esc(label)}</text>`;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)">${m.title ? `<text x="4" y="20" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(m.title)}</text>` : ""}${pic(m.before, 0, m.beforeLabel || t("beforeafter.before"))}${pic(m.after, w + 10, m.afterLabel || t("beforeafter.after"))}</svg>`;
}

registerBlock<BeforeAfter>({
  kind: "beforeafter",
  icon: "SquareSplitHorizontal",
  label: "slash.beforeafter",
  hint: "block.beforeafter_hint",
  example: "title: Kabel im Rack\nbefore:\nafter:",
  parse: parseBeforeAfter,
  svg: (m, s) => beforeAfterSVG(m, s.width, s.height ?? Math.round(s.width * 0.42)),
  board: { w: 560, h: 280 },
  async mount(host, m, ctx) {
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .blk { padding: 12px; }
      .stage { position: relative; overflow: hidden; border-radius: 8px; background: var(--surface2); line-height: 0; user-select: none; touch-action: pan-y; }
      .stage img { display: block; width: 100%; height: auto; }
      .stage .top { position: absolute; inset: 0; clip-path: inset(0 calc(100% - var(--pos)) 0 0); }
      .stage .top img { width: 100%; height: 100%; object-fit: cover; }
      .line { position: absolute; top: 0; bottom: 0; left: var(--pos); width: 2px; margin-left: -1px; background: #fff; box-shadow: 0 0 0 1px rgb(0 0 0 / .25); pointer-events: none; }
      .knob { position: absolute; top: 50%; left: var(--pos); width: 34px; height: 34px; margin: -17px 0 0 -17px; border-radius: 50%; background: #fff; box-shadow: 0 1px 4px rgb(0 0 0 / .35); pointer-events: none; display: grid; place-items: center; color: #333; font: 600 14px/1 system-ui; }
      .tag { position: absolute; top: 8px; padding: 3px 8px; border-radius: 6px; background: rgb(0 0 0 / .55); color: #fff; font: 600 12px/1.4 var(--font-body, system-ui); }
      .tag.l { left: 8px; } .tag.r { right: 8px; }
      input[type=range] { position: absolute; inset: 0; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: ew-resize; }
      .stage:has(input:focus-visible) .knob { outline: 3px solid var(--accent); outline-offset: 2px; }
      .empty { display: flex; gap: 10px; align-items: center; justify-content: center; padding: 30px 12px; line-height: 1.4; flex-wrap: wrap; }
      .both { display: none; }
      @media print {
        .stage { display: none; }
        .both { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
        .both figure { margin: 0; }
        .both img { width: 100%; height: auto; }
        .both figcaption { font-size: 12px; color: var(--muted); }
      }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    const bl = m.beforeLabel || t("beforeafter.before"), al = m.afterLabel || t("beforeafter.after");
    if (!m.before || !m.after) {
      // In the editor the missing photo can be chosen right here.
      const which = !m.before ? "before" : "after";
      box.innerHTML = `${m.title ? `<h3>${esc(m.title)}</h3>` : ""}<div class="stage empty"><span class="muted">${esc(t("beforeafter.missing"))}</span>
        ${ctx.edit && ctx.project ? `<button type="button" class="ghost" data-pick>${esc(t(which === "before" ? "beforeafter.pick_before" : "beforeafter.pick_after"))}</button>` : ""}</div>`;
      box.querySelector("[data-pick]")?.addEventListener("click", async () => {
        const url = await pickImage(ctx.project, ctx.doc);
        if (!url || !ctx.edit) return;
        const src = ctx.source ?? "";
        const re = new RegExp(`^(\\s*${which}\\s*:).*$`, "mi");
        ctx.edit(re.test(src) ? src.replace(re, `$1 ${url} ${which === "before" ? bl : al}`) : `${src}\n${which}: ${url} ${which === "before" ? bl : al}`);
      });
      return;
    }
    box.innerHTML = `${m.title ? `<h3>${esc(m.title)}</h3>` : ""}
      <div class="stage" style="--pos:${m.start}%">
        <img src="${esc(m.after)}" alt="${esc(al)}" draggable="false">
        <div class="top"><img src="${esc(m.before)}" alt="${esc(bl)}" draggable="false"></div>
        <div class="line"></div><div class="knob" aria-hidden="true">⇔</div>
        <span class="tag l" aria-hidden="true">${esc(bl)}</span><span class="tag r" aria-hidden="true">${esc(al)}</span>
        <input type="range" min="0" max="100" step="1" value="${m.start}" aria-label="${esc(t("beforeafter.slider", { before: bl, after: al }))}" aria-valuetext="${esc(t("beforeafter.value", { n: m.start, before: bl }))}">
      </div>
      <div class="both"><figure><img src="${esc(m.before)}" alt=""><figcaption>${esc(bl)}</figcaption></figure><figure><img src="${esc(m.after)}" alt=""><figcaption>${esc(al)}</figcaption></figure></div>`;
    const stage = box.querySelector<HTMLElement>(".stage")!;
    const input = box.querySelector<HTMLInputElement>("input")!;
    input.addEventListener("input", () => {
      stage.style.setProperty("--pos", `${input.value}%`);
      input.setAttribute("aria-valuetext", t("beforeafter.value", { n: Number(input.value), before: bl }));
    });
  },
});
