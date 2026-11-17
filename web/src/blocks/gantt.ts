// Schedules (Gantt): bars for tasks over time, grouped in sections, with
// milestones, progress and dependencies ("after id"). The time axis is the
// chart renderer's (ADR-0084). "board: slug" adds the board's cards that
// have a due date (from the day they were created to the due date).
import { XScale, xNum, xTicks, type ChartSpec } from "../chart";
import { locale, t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, keyValue } from "./util";

export interface GanttItem {
  name: string; id?: string; section: number;
  start: number; end: number; milestone: boolean;
  done: boolean; progress?: number; color?: string;
  after?: string[]; card?: string;
}
export interface Gantt { title: string; sections: string[]; items: GanttItem[]; today: boolean; board?: string }

const DAY = 864e5;
const COLORS = ["blue", "green", "orange", "purple", "pink", "red", "yellow", "gray"];
const DATE = /^\d{4}-\d{2}-\d{2}$/;

const day = (s: string) => (DATE.test(s) ? xNum(s, "time") : null);

/** "3d", "2w", "1m" → days (months as 30). */
function duration(s: string): number | null {
  const m = /^(\d+(?:[.,]\d+)?)\s*(d|t|w|m)$/i.exec(s.trim());
  if (!m) return null;
  const n = Number(m[1].replace(",", "."));
  return { d: 1, t: 1, w: 7, m: 30 }[m[2].toLowerCase() as "d"]! * n;
}

export function parseGantt(src: string): { model?: Gantt; problems: BlockProblem[] } {
  const g: Gantt = { title: "", sections: [], items: [], today: false };
  const problems: BlockProblem[] = [];
  const pending: { item: GanttItem; after: string[]; dur: number | null; end: number | null }[] = [];
  let section = -1;
  for (const l of blockLines(src.replace(/^##\s+/gm, "section "))) {
    const kv = keyValue(l, ["title", "titel", "board"]);
    if (kv) { if (kv[0] === "board") g.board = kv[1]; else g.title = kv[1]; continue; }
    if (/^(today|heute)$/i.test(l)) { g.today = true; continue; }
    const sec = /^(?:section|abschnitt)\s+(.+)$/i.exec(l);
    if (sec) { g.sections.push(sec[1].trim()); section = g.sections.length - 1; continue; }
    const m = /^(.+?)(?:\s*\[([A-Za-z0-9-]+)\])?\s*:\s*(.+)$/.exec(l);
    if (!m) { problems.push({ key: "gantt.err_line", params: { line: l } }); continue; }
    if (section < 0) { g.sections.push(""); section = 0; }
    const [, name, id] = m;
    // "2026-02-02 – 2026-02-06" is the same as "2026-02-02, 2026-02-06".
    const parts = m[3].replace(/\s+(?:–|—|-|\.\.|bis|to)\s+(?=\d{4}-)/, ", ").split(",").map((x) => x.trim()).filter(Boolean);
    const item: GanttItem = { name: name.trim(), id, section, start: NaN, end: NaN, milestone: false, done: false };
    let after: string[] = [], dur: number | null = null, end: number | null = null;
    const first = parts.shift() ?? "";
    const af = /^(?:after|nach)\s+(.+)$/i.exec(first);
    if (af) after = af[1].split(/\s+/).filter(Boolean);
    else {
      const s = day(first);
      if (s === null) { problems.push({ key: "gantt.err_date", params: { name: item.name, value: first } }); continue; }
      item.start = s;
    }
    for (const p of parts) {
      const d = day(p), du = duration(p), pct = /^(\d{1,3})\s*%$/.exec(p);
      if (d !== null && end === null && dur === null) end = d;
      else if (du !== null && end === null && dur === null) dur = du;
      else if (/^(done|fertig|erledigt)$/i.test(p)) item.done = true;
      else if (pct) item.progress = Math.min(100, Number(pct[1]));
      else if (COLORS.includes(p.toLowerCase())) item.color = p.toLowerCase();
      else problems.push({ key: "gantt.err_part", params: { name: item.name, value: p } });
    }
    if (after.length) item.after = after;
    g.items.push(item);
    pending.push({ item, after, dur, end });
  }
  // Starts after other items: resolve in order, so chains work.
  const byId = new Map(g.items.filter((i) => i.id).map((i) => [i.id!, i]));
  for (let round = 0; round <= pending.length; round++) {
    for (const p of pending) {
      if (!Number.isNaN(p.item.start)) continue;
      const deps = p.after.map((a) => byId.get(a));
      if (deps.some((d) => !d)) continue;
      if (deps.some((d) => Number.isNaN(d!.end))) continue;
      p.item.start = Math.max(...deps.map((d) => d!.end));
    }
    for (const p of pending) {
      const i = p.item;
      if (Number.isNaN(i.start) || !Number.isNaN(i.end)) continue;
      if (p.end !== null) i.end = p.end + DAY; // an end date includes that day
      else if (p.dur !== null) i.end = i.start + p.dur * DAY;
      else { i.end = i.start; i.milestone = true; }
    }
  }
  for (const p of pending) {
    const missing = p.after.find((a) => !byId.has(a));
    if (missing) problems.push({ key: "gantt.err_after", params: { name: p.item.name, id: missing } });
    else if (Number.isNaN(p.item.start)) problems.push({ key: "gantt.err_cycle", params: { name: p.item.name } });
    else if (p.item.end < p.item.start) problems.push({ key: "gantt.err_order", params: { name: p.item.name } });
  }
  if (!g.items.length && !g.board) problems.push({ key: "gantt.err_empty" });
  return problems.length ? { problems } : { model: g, problems };
}

/** The board's cards with a due date as items (one section named after the board). */
export function cardItems(g: Gantt, cards: { id: string; title: string; due?: string; createdAt?: string; doneAt?: string | null; boardTitle?: string }[]): Gantt {
  const withDue = cards.filter((c) => c.due && day(c.due) !== null);
  if (!withDue.length) return g;
  const sections = [...g.sections, withDue[0].boardTitle || g.board || ""];
  const s = sections.length - 1;
  const items = withDue.map((c) => {
    const due = day(c.due!)!;
    const made = c.createdAt ? day(c.createdAt.slice(0, 10)) : null;
    const start = made !== null && made < due ? made : due;
    return { name: c.title, section: s, start, end: start === due ? due : due + DAY, milestone: start === due, done: !!c.doneAt, card: c.id } as GanttItem;
  });
  return { ...g, sections, items: [...g.items, ...items] };
}

const ROW = 26, HEAD = 30;

export function ganttSVG(g: Gantt, width: number, loc = "de", now = Date.now()): string {
  if (!g.items.length) return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="40"><text x="6" y="24" style="font-size:13px;fill:var(--muted)">${esc(t("gantt.no_items"))}</text></svg>`;
  const labelW = Math.min(Math.max(120, width * 0.3), Math.max(...g.items.map((i) => i.name.length * 7 + 24)));
  const left = labelW + 8, right = width - 14;
  const xs = g.items.flatMap((i) => [i.start, i.end]);
  let a = Math.min(...xs), b = Math.max(...xs);
  if (g.today) { a = Math.min(a, now); b = Math.max(b, now); }
  const pad = Math.max(DAY, (b - a) * 0.02);
  const spec = { points: [{ x: iso(a - pad), y: 0 }, { x: iso(b + pad), y: 0 }] } as ChartSpec;
  const sc = new XScale("time", spec, left, right);
  const top = (g.title ? 30 : 0) + HEAD;
  // Rows: a heading row per named section, then its items.
  const rows: ({ section: string; color: string } | { item: GanttItem; color: string })[] = [];
  g.sections.forEach((s, si) => {
    const color = COLORS[si % COLORS.length];
    const its = g.items.filter((i) => i.section === si);
    if (!its.length) return;
    if (s) rows.push({ section: s, color });
    for (const item of its) rows.push({ item, color: item.color ?? color });
  });
  const height = top + rows.length * ROW + 10;
  const y = (i: number) => top + i * ROW;
  const pos = new Map<GanttItem, { x0: number; x1: number; y: number }>();
  let body = "";
  const ticks = xTicks(sc, loc, 70);
  const grid = ticks.map((tk) => `<line x1="${tk.x}" x2="${tk.x}" y1="${top - 4}" y2="${height - 10}" style="stroke:var(--line)" stroke-width="1"/><text x="${tk.x}" y="${top - 10}" text-anchor="middle" style="font-size:11px;fill:var(--muted)">${esc(tk.label)}</text>`).join("");
  rows.forEach((r, i) => {
    const ry = y(i);
    if ("section" in r) {
      body += `<text x="6" y="${ry + ROW / 2 + 4.5}" style="font-size:12px;font-weight:700;fill:var(--muted);letter-spacing:.02em">${esc(r.section.toUpperCase())}</text>`;
      return;
    }
    const it = r.item;
    const x0 = sc.num(it.start), x1 = Math.max(sc.num(it.end), x0 + 3);
    pos.set(it, { x0, x1, y: ry + ROW / 2 });
    body += `<text x="14" y="${ry + ROW / 2 + 4.5}" style="font-size:13px;fill:var(--fg)${it.done ? ";opacity:.6" : ""}">${esc(it.name.length > labelW / 7 ? it.name.slice(0, Math.floor(labelW / 7) - 1) + "…" : it.name)}</text>`;
    const tip = `<title>${esc(it.name)}: ${esc(fmt(it.start, loc))}${it.milestone ? "" : " – " + esc(fmt(it.end - DAY, loc))}${it.progress !== undefined ? ` (${it.progress} %)` : ""}${it.done ? ` – ${esc(t("gantt.done"))}` : ""}</title>`;
    if (it.milestone) {
      const cy = ry + ROW / 2;
      body += `<path d="M${x0} ${cy - 8} L${x0 + 8} ${cy} L${x0} ${cy + 8} L${x0 - 8} ${cy} Z" style="fill:var(--tc-${r.color})">${tip}</path>`;
    } else {
      const op = it.done ? ";opacity:.45" : "";
      body += `<g>${tip}<rect x="${x0}" y="${ry + 6}" width="${x1 - x0}" height="${ROW - 12}" rx="4" style="fill:var(--tb-${r.color});stroke:var(--tc-${r.color})${op}"/>` +
        (it.progress ? `<rect x="${x0}" y="${ry + 6}" width="${((x1 - x0) * it.progress) / 100}" height="${ROW - 12}" rx="4" style="fill:var(--tc-${r.color})${op}"/>` : "") +
        (it.done ? `<path d="M${x1 + 5} ${ry + 13} l3 3 l6 -7" fill="none" style="stroke:var(--good)" stroke-width="2"/>` : "") + `</g>`;
    }
  });
  // Dependencies: an elbow from the end of the earlier item to the start of the later one.
  const byId = new Map(g.items.filter((i) => i.id).map((i) => [i.id!, i]));
  let deps = "";
  for (const it of g.items) {
    const to = pos.get(it);
    if (!to) continue;
    for (const a of it.after ?? []) {
      const from = pos.get(byId.get(a)!);
      if (!from) continue;
      const mx = Math.max(from.x1 + 6, to.x0 - 8);
      deps += `<path d="M${from.x1} ${from.y} H${mx} V${to.y} H${to.x0 - 2}" fill="none" style="stroke:var(--muted)" stroke-width="1.2" marker-end="url(#ga)"/>`;
    }
  }
  const today = g.today && now >= sc.segs[0].a && now <= sc.segs[sc.segs.length - 1].b
    ? `<line x1="${sc.num(now)}" x2="${sc.num(now)}" y1="${top - 4}" y2="${height - 10}" style="stroke:var(--bad)" stroke-width="1.5" stroke-dasharray="4 3"/><text x="${sc.num(now) + 4}" y="${height - 12}" style="font-size:11px;fill:var(--bad)">${esc(t("gantt.today"))}</text>`
    : "";
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" role="img" aria-label="${esc(g.title || t("slash.gantt"))}" style="font-family:var(--font-body,system-ui,sans-serif)">
    <defs><marker id="ga" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto"><path d="M0 0 L8 4 L0 8 z" style="fill:var(--muted)"/></marker></defs>
    ${g.title ? `<text x="6" y="20" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(g.title)}</text>` : ""}${grid}${body}${deps}${today}</svg>`;
}

const iso = (ms: number) => new Date(ms).toISOString().slice(0, 10);
const fmt = (ms: number, loc: string) => new Intl.DateTimeFormat(loc, { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" }).format(new Date(ms));

/** A list of the items for screen readers and print (the picture alone does not read well). */
function ganttList(g: Gantt, loc: string): string {
  return `<ul class="sr">${g.items.map((i) => `<li>${esc(i.name)}: ${esc(fmt(i.start, loc))}${i.milestone ? "" : ` – ${esc(fmt(i.end - DAY, loc))}`}${i.progress !== undefined ? ` (${i.progress} %)` : ""}${i.done ? ` – ${esc(t("gantt.done"))}` : ""}</li>`).join("")}</ul>`;
}

registerBlock<Gantt>({
  kind: "gantt",
  icon: "ChartGantt",
  label: "slash.gantt",
  hint: "block.gantt_hint",
  example: "title: Badsanierung\ntoday\n## Planung\nAngebote einholen [angebote]: 2026-01-05, 2w, done\nHandwerker wählen [wahl]: after angebote, 3d, 50%\n## Umbau\nAbriss [abriss]: after wahl, 1w\nFliesen [fliesen]: after abriss, 10d\nSanitär: after fliesen, 1w\nFertig: 2026-03-20",
  parse: parseGantt,
  svg: (g, s) => ganttSVG(g, s.width, s.locale),
  board: { w: 720, h: 320 },
  async mount(host, g, ctx) {
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .chart { overflow-x: auto; }
      .chart svg { display: block; }
      .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
      .note { margin-top: 6px; }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    let model = g;
    const loc = locale();
    const draw = () => {
      const w = Math.max(560, Math.floor(host.getBoundingClientRect().width) - 34 || 680);
      box.innerHTML = `<div class="chart">${ganttSVG(model, w, loc)}</div>${ganttList(model, loc)}`;
    };
    draw();
    if (g.board && ctx.project) {
      try {
        const { api } = await import("../api");
        const cards = await api<{ id: string; title: string; due?: string; createdAt?: string; doneAt?: string | null; boardTitle?: string }[]>("GET", `/projects/${ctx.project}/tasks?board=${encodeURIComponent(g.board)}`);
        model = cardItems(g, cards ?? []);
        draw();
      } catch {
        box.insertAdjacentHTML("beforeend", `<p class="muted note">${esc(t("gantt.board_failed", { board: g.board }))}</p>`);
      }
    }
    let lastW = host.getBoundingClientRect().width;
    const ro = new ResizeObserver(() => {
      const w = host.getBoundingClientRect().width;
      if (Math.abs(w - lastW) > 8) { lastW = w; draw(); }
    });
    ro.observe(host);
    return () => ro.disconnect();
  },
});
