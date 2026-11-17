// A 19" rack in a page: the devices mounted in a rack entry by height unit
// (the layout of entry pages, web/src/rack.ts), each with its live status
// and a link to its entry. Always current.
import { t } from "../i18n";
import { rackSlots, type Mounted } from "../rack";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, keyValue, liveStatus, openEntry, statusColor } from "./util";

export interface RackSpec { rack: string; title: string }

export function parseRack(src: string): { model?: RackSpec; problems: BlockProblem[] } {
  const m: RackSpec = { rack: "", title: "" };
  const problems: BlockProblem[] = [];
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["rack", "title", "titel"]);
    const v = kv ? kv[1] : l;
    if (kv && kv[0] !== "rack") { m.title = v; continue; }
    const s = /^@?([a-z0-9]+(?:-[a-z0-9]+)*)$/.exec(v.trim());
    if (s && !m.rack) m.rack = s[1];
    else problems.push({ key: "rack.err_line", params: { line: l } });
  }
  if (!m.rack && !problems.length) problems.push({ key: "rack.err_missing" });
  return problems.length ? { problems } : { model: m, problems };
}

function rackSketch(s: RackSpec, width: number, height: number): string {
  const w = Math.min(220, width - 12), x = (width - w) / 2, top = 34, h = height - top - 6;
  const units = Array.from({ length: 12 }, (_, i) => `<rect x="${x + 8}" y="${top + 6 + i * ((h - 12) / 12)}" width="${w - 16}" height="${(h - 12) / 12 - 3}" rx="2" style="fill:var(--surface);stroke:var(--line)"/>`).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)"><text x="6" y="22" style="font-size:15px;font-weight:600;fill:var(--fg)">${esc(s.title || "@" + s.rack)}</text><rect x="${x}" y="${top}" width="${w}" height="${h}" rx="5" style="fill:var(--surface2);stroke:var(--muted)" stroke-width="2"/>${units}</svg>`;
}

registerBlock<RackSpec>({
  kind: "rack",
  icon: "Server",
  label: "slash.rack",
  hint: "block.rack_hint",
  example: "@rack",
  parse: parseRack,
  svg: (s, size) => rackSketch(s, size.width, size.height ?? 360),
  board: { w: 360, h: 520, live: true },
  async mount(host, s, ctx) {
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .wrap { display: grid; grid-template-columns: minmax(0, 380px) minmax(0, 1fr); gap: 16px; align-items: start; }
      .rack { border: 2px solid var(--muted); border-radius: 6px; padding: 4px; background: var(--surface2); display: grid; gap: 2px; }
      .slot { display: grid; grid-template-columns: 30px 1fr; min-height: 20px; font-size: 12.5px; }
      .u { color: var(--muted); font: 11px var(--font-mono, monospace); text-align: right; padding-right: 6px; align-self: center; }
      .empty { border: 1px dashed var(--line); border-radius: 3px; }
      a.dev { display: flex; gap: 6px; align-items: center; padding: 2px 8px; border-radius: 4px; background: var(--surface); border-left: 4px solid var(--c);
        color: var(--fg); text-decoration: none; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
      a.dev.conflict { outline: 2px solid var(--bad); }
      @media (hover: hover) and (pointer: fine) { a.dev:hover { background: var(--accent-soft, var(--surface2)); } }
      a.dev:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
      .side ul { margin: 0; padding-left: 18px; font-size: 13px; }
      .side a, h3 a { color: var(--accent); }
      .side p { margin: 0 0 6px; }
      .bad { color: var(--bad); font-size: 13px; }
      :host([data-board]) .wrap { grid-template-columns: 1fr; }
      @media (max-width: 700px) { .wrap { grid-template-columns: 1fr; } }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    box.innerHTML = `<p class="muted">${esc(t("rack.loading"))}</p>`;
    if (!ctx.project) return;
    const { api, ApiError } = await import("../api");
    let timer = 0;
    const load = async () => {
      try {
        const d = await api<{ entity: { slug: string; name: string; kind: string; id: string; attrs?: Record<string, unknown> }; edges: { rel: string; to: string; fromSlug?: string; fromName: string; fromKind?: string; attrs?: Record<string, unknown> }[] }>(
          "GET", `/projects/${ctx.project}/entities/${encodeURIComponent(s.rack)}`);
        const units = Math.min(Math.max(Number(d.entity.attrs?.height_units) || 42, 1), 60);
        const devices: Mounted[] = d.edges.filter((e) => e.rel === "mounted_in" && e.to === d.entity.id).map((e) => ({
          slug: e.fromSlug ?? "", name: e.fromName, kind: e.fromKind ?? "",
          u: Number(e.attrs?.u) || undefined, height: Math.max(1, Number(e.attrs?.height) || 1),
        }));
        const status = await liveStatus(ctx.project, devices.map((x) => x.slug));
        const { slots, unplaced, conflicts } = rackSlots(units, devices);
        const href = (slug: string) => `/p/${esc(ctx.project)}/e/${esc(slug)}`;
        const label = (dv: Mounted) => {
          const st = status.get(dv.slug)?.status;
          return `${dv.name} · U${dv.u}${dv.height > 1 ? `–${dv.u! + dv.height - 1}` : ""}${st && st !== "unknown" ? ` · ${t(`diag.status.${st}` as never)}` : ""}`;
        };
        box.innerHTML = `<h3><a href="${href(d.entity.slug)}" data-entity style="color:inherit">${esc(s.title || d.entity.name)}</a> <span class="muted">${esc(t("rack.units", { units }))}</span></h3>
          <div class="wrap"><div class="rack" role="list" aria-label="${esc(t("rack.units", { units }))}">
          ${slots.map((sl) => `<div class="slot" role="listitem" style="min-height:${(sl.top - sl.bottom + 1) * 20 + (sl.top - sl.bottom) * 2}px">
            <span class="u">${sl.top === sl.bottom ? sl.top : `${sl.top}–${sl.bottom}`}</span>
            ${sl.device ? `<a class="dev${sl.conflict ? " conflict" : ""}" data-entity href="${href(sl.device.slug)}" style="--c:${statusColor(status.get(sl.device.slug)?.status)}" title="${esc(label(sl.device))}" aria-label="${esc(label(sl.device))}">${esc(sl.device.name)}</a>` : `<span class="empty" aria-hidden="true"></span>`}
          </div>`).join("")}</div>
          <div class="side">
            ${conflicts.length ? `<p class="bad" role="alert">${esc(t("rack.conflict"))}</p>` : ""}
            ${unplaced.length ? `<p class="muted">${esc(t("rack.unplaced_hint", { count: unplaced.length }))}</p><ul>${unplaced.map((u) => `<li><a data-entity href="${href(u.slug)}">${esc(u.name)}</a></li>`).join("")}</ul>` : ""}
            ${!devices.length ? `<p class="muted">${esc(t("rack.empty"))}</p>` : ""}
          </div></div>`;
      } catch (e) {
        box.innerHTML = `<p class="muted">${esc(e instanceof ApiError && e.status === 404 ? t("rack.missing", { rack: s.rack }) : t("rack.failed"))}</p>`;
      }
    };
    box.addEventListener("click", (e) => {
      const a = (e.target as Element).closest<HTMLAnchorElement>("a[data-entity]");
      if (a) void openEntry(e, a.getAttribute("href")!);
    });
    await load();
    timer = window.setInterval(() => document.visibilityState === "visible" && void load(), 60_000);
    return () => clearInterval(timer);
  },
});
