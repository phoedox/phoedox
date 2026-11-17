// A floor plan (or one room of it) in a page: walls, doors and windows,
// rooms and the devices in them with their live status, always current.
// The plan stays the floor plan document; the block only shows it.
import { area, bounds, inside, labelPoint, normalizePlan, openingSpan, wallOutline, type Floorplan, type FloorRoom } from "../floorplan";
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, keyValue, liveStatus, openEntry, statusColor, type LiveEntry } from "./util";

export interface PlanSpec { plan: string; room: string; title: string; devices: boolean }

export function parsePlanBlock(src: string): { model?: PlanSpec; problems: BlockProblem[] } {
  const m: PlanSpec = { plan: "", room: "", title: "", devices: true };
  const problems: BlockProblem[] = [];
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["plan", "grundriss", "room", "raum", "title", "titel", "devices", "geraete"]);
    if (!kv) {
      const s = /^([a-z0-9]+(?:-[a-z0-9]+)*)$/.exec(l);
      if (s && !m.plan) { m.plan = s[1]; continue; }
      problems.push({ key: "floorplan.err_line", params: { line: l } });
      continue;
    }
    const [k, v] = kv;
    if (k === "plan" || k === "grundriss") {
      if (/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(v)) m.plan = v; else problems.push({ key: "floorplan.err_plan", params: { value: v } });
    } else if (k === "room" || k === "raum") m.room = v;
    else if (k === "devices" || k === "geraete") m.devices = !/^(no|nein|false|off|0|aus)$/i.test(v);
    else m.title = v;
  }
  if (!m.plan && !problems.length) problems.push({ key: "floorplan.err_plan", params: { value: "" } });
  return problems.length ? { problems } : { model: m, problems };
}

/** The room a block names (by name, case-insensitive, or by its entry). */
export function findRoom(plan: Floorplan, name: string): FloorRoom | undefined {
  const n = name.trim().toLowerCase().replace(/^@/, "");
  return plan.rooms.find((r) => r.name.toLowerCase() === n) ?? plan.rooms.find((r) => r.entity === n);
}

/** The devices of the plan (or of one room) with their position. */
export function planDevices(plan: Floorplan, room?: FloorRoom) {
  return plan.devices.filter((d) => !room || inside(d.x, d.y, room.points));
}

const pts = (p: [number, number][]) => p.map((q) => `${Math.round(q[0])},${Math.round(q[1])}`).join(" ");

export function planSVG(plan: Floorplan, o: { room?: FloorRoom; names?: Map<string, LiveEntry>; project?: string; interactive?: boolean; label: string }): string {
  const b = o.room
    ? { min: [Math.min(...o.room.points.map((p) => p[0])), Math.min(...o.room.points.map((p) => p[1]))], max: [Math.max(...o.room.points.map((p) => p[0])), Math.max(...o.room.points.map((p) => p[1]))] }
    : bounds(plan) ?? { min: [0, 0], max: [plan.width, plan.height] };
  const span = Math.max(b.max[0] - b.min[0], b.max[1] - b.min[1], 1000);
  const pad = span * 0.06;
  const x0 = b.min[0] - pad, y0 = b.min[1] - pad, w = b.max[0] - b.min[0] + 2 * pad, h = b.max[1] - b.min[1] + 2 * pad;
  const font = span / 32;
  const rooms = plan.rooms.map((r) => {
    const on = !o.room || r.id === o.room.id;
    const [lx, ly] = labelPoint(r.points);
    const sqm = area(r.points) / 1e6;
    return `<polygon points="${pts(r.points)}" style="fill:var(--tb-${esc(r.color || "gray")});stroke:var(--tc-${esc(r.color || "gray")})" stroke-width="${span / 400}" opacity="${on ? 1 : 0.35}"/>` +
      (on ? `<text x="${lx}" y="${ly}" text-anchor="middle" style="font-size:${font}px;font-weight:600;fill:var(--fg)">${esc(r.name)}</text>` +
        (sqm >= 1 ? `<text x="${lx}" y="${ly + font * 1.2}" text-anchor="middle" style="font-size:${font * 0.75}px;fill:var(--muted)">${sqm.toLocaleString(undefined, { maximumFractionDigits: 1 })} m²</text>` : "") : "");
  }).join("");
  const walls = plan.walls.map((wl) => `<polygon points="${pts(wallOutline(wl))}" style="fill:var(--fg)" opacity=".75"/>`).join("");
  const openings = plan.openings.map((op) => {
    const wl = plan.walls.find((x) => x.id === op.wall);
    if (!wl) return "";
    const [a, c] = openingSpan(wl, op);
    return `<line x1="${a[0]}" y1="${a[1]}" x2="${c[0]}" y2="${c[1]}" style="stroke:var(--surface)" stroke-width="${wl.thickness * 1.05}"/>` +
      (op.kind === "window" ? `<line x1="${a[0]}" y1="${a[1]}" x2="${c[0]}" y2="${c[1]}" style="stroke:var(--tc-blue)" stroke-width="${wl.thickness / 3}"/>` : "");
  }).join("");
  const r = span / 70;
  const devices = planDevices(plan, o.room).map((d) => {
    const e = o.names?.get(d.entity);
    const name = e?.name ?? d.entity;
    const dot = `<circle cx="${d.x}" cy="${d.y}" r="${r}" style="fill:${statusColor(e?.status)};stroke:var(--surface)" stroke-width="${r / 3}"/>` +
      `<text x="${d.x + r * 1.5}" y="${d.y + r * 0.45}" style="font-size:${font * 0.8}px;fill:var(--fg);paint-order:stroke;stroke:var(--surface);stroke-width:${font / 5}px">${esc(name)}</text><title>${esc(name)}</title>`;
    return o.interactive && o.project ? `<a href="/p/${esc(o.project)}/e/${esc(d.entity)}" data-entity aria-label="${esc(name)}">${dot}</a>` : `<g>${dot}</g>`;
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${x0} ${y0} ${w} ${h}" width="100%" role="img" aria-label="${esc(o.label)}" style="font-family:var(--font-body,system-ui,sans-serif);display:block;max-height:min(60vh, 480px);margin:0 auto">${rooms}${walls}${openings}${devices}</svg>`;
}

function planSketch(s: PlanSpec, width: number): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} 70" width="${width}" height="70" style="font-family:var(--font-body,system-ui,sans-serif)"><text x="6" y="24" style="font-size:15px;font-weight:600;fill:var(--fg)">${esc(s.title || s.room || s.plan)}</text><text x="6" y="48" style="font-size:12px;fill:var(--muted)">${esc(s.plan)}${s.room ? ` · ${esc(s.room)}` : ""}</text></svg>`;
}

registerBlock<PlanSpec>({
  kind: "floorplan",
  icon: "LandPlot",
  label: "slash.floorplan",
  hint: "block.floorplan_hint",
  example: "plan: erdgeschoss\nroom: Küche",
  parse: parsePlanBlock,
  svg: (s, size) => planSketch(s, size.width),
  board: { w: 560, h: 420, live: true },
  async mount(host, s, ctx) {
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .plan { border-radius: 8px; overflow: hidden; background: var(--surface); }
      .plan a[data-entity]:focus-visible { outline: none; }
      .plan a[data-entity]:focus-visible circle { stroke: var(--accent); }
      ul.devs { margin: 8px 0 0; padding: 0; list-style: none; display: flex; flex-wrap: wrap; gap: 6px; }
      ul.devs a { display: inline-flex; gap: 6px; align-items: center; padding: 3px 9px; border: 1px solid var(--line); border-radius: 999px; font-size: 12.5px; color: var(--fg); text-decoration: none; }
      ul.devs a i { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
      @media (hover: hover) and (pointer: fine) { ul.devs a:hover { background: var(--surface2); } }
      h3 a { color: inherit; }
      .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    box.innerHTML = `<p class="muted">${esc(t("floorplan.loading"))}</p>`;
    if (!ctx.project) return;
    const { api, ApiError } = await import("../api");
    let timer = 0;
    const load = async () => {
      try {
        const doc = await api<{ slug: string; title: string; type: string; content: unknown }>("GET", `/projects/${ctx.project}/documents/${encodeURIComponent(s.plan)}`);
        if (doc.type !== "floorplan") throw new Error("type");
        const plan = normalizePlan(doc.content as Partial<Floorplan>);
        const room = s.room ? findRoom(plan, s.room) : undefined;
        if (s.room && !room) {
          box.innerHTML = `<p class="muted">${esc(t("floorplan.no_room", { room: s.room, plan: doc.title }))}</p>`;
          return;
        }
        const devs = s.devices ? planDevices(plan, room) : [];
        const names = await liveStatus(ctx.project, [...new Set(devs.map((d) => d.entity))]);
        const title = s.title || (room ? `${room.name} · ${doc.title}` : doc.title);
        const shown = s.devices ? plan : { ...plan, devices: [] };
        box.innerHTML = `<h3><a href="/p/${esc(ctx.project)}/d/${esc(doc.slug)}" data-entity>${esc(title)}</a></h3>
          <div class="plan">${planSVG(shown, { room, names, project: ctx.project, interactive: true, label: title })}</div>
          ${devs.length ? `<ul class="devs" aria-label="${esc(t("floorplan.devices"))}">${[...new Map(devs.map((d) => [d.entity, d])).values()].map((d) => {
            const e = names.get(d.entity);
            return `<li><a data-entity href="/p/${esc(ctx.project)}/e/${esc(d.entity)}"><i style="background:${statusColor(e?.status)}" aria-hidden="true"></i>${esc(e?.name ?? d.entity)}${e?.status && e.status !== "unknown" ? `<span class="sr"> (${esc(t(`diag.status.${e.status}` as never))})</span>` : ""}</a></li>`;
          }).join("")}</ul>` : ""}`;
      } catch (e) {
        box.innerHTML = `<p class="muted">${esc(e instanceof ApiError && e.status === 404 ? t("floorplan.missing", { plan: s.plan }) : e instanceof Error && e.message === "type" ? t("floorplan.not_plan", { plan: s.plan }) : t("floorplan.failed"))}</p>`;
      }
    };
    box.addEventListener("click", (e) => {
      const a = (e.target as Element).closest<Element>("a[data-entity]");
      if (a) void openEntry(e, a.getAttribute("href")!);
    });
    await load();
    timer = window.setInterval(() => document.visibilityState === "visible" && void load(), 60_000);
    return () => clearInterval(timer);
  },
});
