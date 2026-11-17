// The calendar (ADR-0089): the project's dates – maintenance, cards with a
// due date, date fields of entries – and external calendars, as a month or
// a list. The same element shows the project tab, calendar documents and
// the ```calendar block.
import { LitElement, css, html, nothing } from "lit";
import { api } from "../api";
import { hasKey, locale, t } from "../i18n";
import { icon } from "../icon";
import { navigate } from "../state";
import { base } from "./shared";

export interface CalendarSpec { view?: "month" | "list"; sources?: string[]; kinds?: string[]; calendars?: string[]; days?: number }
export interface CalEvent {
  id: string; title: string; start: string; end: string; allDay: boolean; source: string; calendar?: string; color?: string;
  link?: string; entity?: string; kind?: string; attr?: string; done?: boolean; location?: string; note?: string; url?: string;
}
export interface ExternalCalendar { id: string; name: string; kind: string; url: string; color?: string; writeBack: boolean; syncedAt?: string | null; lastError?: string; username?: string; hasPassword: boolean }
export interface CalendarView { from: string; to: string; events: CalEvent[]; calendars: ExternalCalendar[]; feed?: { on: boolean; since?: string | null; token?: string } }

export const SOURCES = ["maintenance", "tasks", "entities", "external"] as const;
const SOURCE_COLOR: Record<string, string> = { maintenance: "orange", tasks: "blue", entities: "purple", external: "gray" };
const SOURCE_ICON: Record<string, string> = { maintenance: "Wrench", tasks: "SquareKanban", entities: "CalendarClock", external: "CalendarDays" };

const pad = (n: number) => String(n).padStart(2, "0");
export const dayKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const parseDay = (s: string) => {
  const [y, m, d] = s.slice(0, 10).split("-").map(Number);
  return new Date(y, m - 1, d);
};
const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);

/** The local days an event covers (end exclusive). */
export function eventDays(e: CalEvent): string[] {
  const start = e.allDay ? parseDay(e.start) : new Date(e.start);
  let end = e.allDay ? parseDay(e.end) : new Date(e.end);
  const first = new Date(start.getFullYear(), start.getMonth(), start.getDate());
  if (!e.allDay && end > start && end.getHours() === 0 && end.getMinutes() === 0) end = new Date(end.getTime() - 1);
  const out: string[] = [];
  for (let d = first, i = 0; (e.allDay ? d < end : d <= end) && i < 62; d = addDays(d, 1), i++) out.push(dayKey(d));
  return out.length ? out : [dayKey(first)];
}

/** The title shown: date fields get their label ("Car · Inspection"). */
export function eventLabel(e: CalEvent): string {
  if (!e.attr) return e.title;
  const k = `attr.${e.attr}`;
  const label = hasKey(k) ? t(k) : e.attr.replace(/_/g, " ");
  return `${e.title} · ${label}`;
}

/** Monday-first weeks covering a month (6 rows). */
export function monthGrid(year: number, month: number): Date[] {
  const first = new Date(year, month, 1);
  const shift = (first.getDay() + 6) % 7;
  const start = addDays(first, -shift);
  return Array.from({ length: 42 }, (_, i) => addDays(start, i));
}

export class PhxCalendar extends LitElement {
  static properties = {
    project: {}, doc: {}, spec: { attribute: false }, compact: { type: Boolean }, filters: { type: Boolean },
    month: { state: true }, data: { state: true }, error: { state: true }, off: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .bar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 10px; }
    .bar h2 { font-size: 18px; margin: 0 8px 0 0; min-width: 9em; }
    .bar .sp { flex: 1; }
    .bar button { padding: 5px 10px; font-size: 13px; }
    .chips { display: flex; gap: 6px; flex-wrap: wrap; }
    .chip { display: inline-flex; gap: 5px; align-items: center; border: 1px solid var(--line); border-radius: 999px; padding: 3px 10px; font-size: 12.5px; background: var(--surface); color: var(--fg); cursor: pointer; }
    .chip[aria-pressed="false"] { color: var(--muted); text-decoration: line-through; }
    .dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: none; }
    .grid { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); border: 1px solid var(--line); border-radius: 12px; overflow: hidden; background: var(--surface); }
    .wd { font-size: 11.5px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; padding: 6px 8px; border-bottom: 1px solid var(--line); }
    .cell { min-height: 92px; padding: 4px 5px 6px; border-top: 1px solid var(--line); border-left: 1px solid var(--line); display: grid; align-content: start; gap: 2px; min-width: 0; }
    .cell:nth-child(7n + 1) { border-left: 0; }
    .cell.out { background: var(--bg); }
    .cell.out .n { color: var(--muted); }
    .n { font-size: 12px; font-weight: 600; justify-self: start; padding: 1px 6px; border-radius: 999px; }
    .cell.today .n { background: var(--accent); color: var(--on-color); }
    .ev { display: flex; gap: 5px; align-items: center; min-width: 0; font-size: 12px; line-height: 1.3; padding: 1px 5px; border-radius: 5px; border: 0; background: transparent; color: var(--fg); text-align: left; cursor: pointer; width: 100%; }
    .ev span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .ev.done span { text-decoration: line-through; color: var(--muted); }
    @media (hover: hover) and (pointer: fine) { .ev:hover { background: var(--surface2); } .chip:hover { border-color: var(--accent); } }
    .ev:focus-visible, .chip:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
    .more { font-size: 11.5px; color: var(--muted); padding-left: 5px; }
    .list { display: grid; gap: 14px; }
    .day h3 { font-size: 13px; color: var(--muted); font-weight: 600; margin: 0 0 4px; text-transform: none; }
    .day h3.today { color: var(--accent); }
    .row { display: grid; grid-template-columns: 5.5em minmax(0, 1fr); gap: 8px; align-items: baseline; padding: 6px 10px; border: 1px solid var(--line); border-radius: 9px; background: var(--surface); margin-bottom: 4px; cursor: pointer; text-align: left; width: 100%; color: var(--fg); font: inherit; }
    .row .time { font-size: 12.5px; color: var(--muted); font-variant-numeric: tabular-nums; }
    .row .what { display: flex; gap: 6px; align-items: center; min-width: 0; }
    .row .what b { font-weight: 500; overflow: hidden; text-overflow: ellipsis; }
    .row .meta { grid-column: 2; font-size: 12px; color: var(--muted); }
    .row.done b { text-decoration: line-through; color: var(--muted); }
    @media (hover: hover) and (pointer: fine) { .row:hover { border-color: var(--accent); } }
    .empty { color: var(--muted); padding: 12px 2px; }
    .err { color: var(--bad); }
    @media (max-width: 640px) {
      .cell { min-height: 58px; }
      .ev span { display: none; }
      .ev { justify-content: center; padding: 1px; }
    }
  `];
  project = "";
  doc = "";
  spec: CalendarSpec = {};
  compact = false;
  filters = false;
  month = new Date(new Date().getFullYear(), new Date().getMonth(), 1);
  data?: CalendarView;
  error = "";
  off = new Set<string>();
  private loaded = "";

  private get view() {
    return this.spec?.view === "list" ? "list" : "month";
  }

  private range(): [string, string] {
    if (this.view === "list") {
      const now = new Date();
      const from = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      return [dayKey(from), dayKey(addDays(from, Math.min(366, Math.max(1, this.spec?.days ?? 60))))];
    }
    const grid = monthGrid(this.month.getFullYear(), this.month.getMonth());
    return [dayKey(grid[0]), dayKey(addDays(grid[41], 1))];
  }

  willUpdate() {
    const [from, to] = this.range();
    const s = this.spec ?? {};
    const q = new URLSearchParams({ from, to });
    if (this.doc) q.set("doc", this.doc);
    if (s.sources?.length) q.set("sources", s.sources.join(","));
    if (s.kinds?.length) q.set("kinds", s.kinds.join(","));
    if (s.calendars?.length) q.set("calendars", s.calendars.join(","));
    const key = `${this.project}?${q}`;
    if (!this.project || key === this.loaded) return;
    this.loaded = key;
    api<CalendarView>("GET", `/projects/${this.project}/calendar?${q}`)
      .then((v) => { if (this.loaded === key) { this.data = v; this.error = ""; } })
      .catch((e) => { if (this.loaded === key) this.error = e?.text ?? t("cal.failed"); });
  }

  /** Loads again (after a calendar was synced or changed). */
  reload() {
    this.loaded = "";
    this.requestUpdate();
  }

  private color(e: CalEvent) {
    return `var(--tc-${e.color || SOURCE_COLOR[e.source] || "gray"})`;
  }

  private open(e: CalEvent) {
    if (e.link) navigate(`/p/${this.project}/${e.link}`);
    else if (e.url) window.open(e.url, "_blank", "noopener");
  }

  private time(e: CalEvent) {
    if (e.allDay) return t("cal.all_day");
    return new Date(e.start).toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit" });
  }

  private tip(e: CalEvent) {
    const parts = [eventLabel(e), e.allDay ? t("cal.all_day") : `${this.time(e)}–${new Date(e.end).toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit" })}`, t(`cal.src.${e.source}` as any)];
    if (e.location) parts.push(e.location);
    return parts.join(" · ");
  }

  private events() {
    return (this.data?.events ?? []).filter((e) => !this.off.has(e.source));
  }

  private byDay() {
    const m = new Map<string, CalEvent[]>();
    for (const e of this.events()) for (const d of eventDays(e)) (m.get(d) ?? m.set(d, []).get(d)!).push(e);
    return m;
  }

  private toggle(src: string) {
    const h = new Set(this.off);
    if (h.has(src)) h.delete(src); else h.add(src);
    this.off = h;
  }

  private shift(n: number) {
    this.month = n === 0 ? new Date(new Date().getFullYear(), new Date().getMonth(), 1) : new Date(this.month.getFullYear(), this.month.getMonth() + n, 1);
  }

  private chips() {
    const present = new Set((this.data?.events ?? []).map((e) => e.source));
    const srcs = SOURCES.filter((s) => (this.spec?.sources?.length ? this.spec.sources.includes(s) : true) && (present.has(s) || this.filters));
    if (srcs.length < 2) return nothing;
    return html`<div class="chips" role="group" aria-label=${t("cal.sources")}>${srcs.map((s) => html`<button type="button" class="chip" aria-pressed=${!this.off.has(s)} @click=${() => this.toggle(s)}>
      <i class="dot" style="background:var(--tc-${SOURCE_COLOR[s]})"></i>${t(`cal.src.${s}` as any)}</button>`)}</div>`;
  }

  private evButton(e: CalEvent) {
    return html`<button type="button" class="ev ${e.done ? "done" : ""}" title=${this.tip(e)} aria-label=${this.tip(e)} @click=${() => this.open(e)}>
      <i class="dot" style="background:${this.color(e)}"></i><span>${e.allDay ? "" : `${this.time(e)} `}${eventLabel(e)}</span></button>`;
  }

  private monthView() {
    const y = this.month.getFullYear(), mo = this.month.getMonth();
    const grid = monthGrid(y, mo);
    const days = this.byDay();
    const today = dayKey(new Date());
    const wd = Array.from({ length: 7 }, (_, i) => grid[i].toLocaleDateString(locale(), { weekday: "short" }));
    const max = this.compact ? 2 : 3;
    return html`<div class="grid" role="grid" aria-label=${this.month.toLocaleDateString(locale(), { month: "long", year: "numeric" })}>
      ${wd.map((w) => html`<div class="wd" role="columnheader">${w}</div>`)}
      ${grid.map((d) => {
        const k = dayKey(d), evs = days.get(k) ?? [];
        return html`<div class="cell ${d.getMonth() !== mo ? "out" : ""} ${k === today ? "today" : ""}" role="gridcell" aria-label=${d.toLocaleDateString(locale(), { dateStyle: "full" })}>
          <span class="n">${d.getDate()}</span>
          ${evs.slice(0, max).map((e) => this.evButton(e))}
          ${evs.length > max ? html`<span class="more">${t("cal.more", { n: evs.length - max })}</span>` : ""}</div>`;
      })}</div>`;
  }

  private listView() {
    const days = [...this.byDay().entries()].sort(([a], [b]) => a.localeCompare(b));
    const today = dayKey(new Date());
    if (!days.length) return html`<p class="empty">${t("cal.none")}</p>`;
    return html`<div class="list">${days.map(([k, evs]) => html`<section class="day">
      <h3 class=${k === today ? "today" : ""}>${k === today ? `${t("cal.today")} · ` : ""}${parseDay(k).toLocaleDateString(locale(), { weekday: "long", day: "numeric", month: "long" })}</h3>
      ${evs.map((e) => html`<button type="button" class="row ${e.done ? "done" : ""}" @click=${() => this.open(e)}>
        <span class="time">${this.time(e)}</span>
        <span class="what"><i class="dot" style="background:${this.color(e)}"></i>${icon(SOURCE_ICON[e.source] ?? "Calendar", 14)}<b>${eventLabel(e)}</b></span>
        ${e.location || e.source === "external" ? html`<span class="meta">${[e.location, this.data?.calendars.find((c) => c.id === e.calendar)?.name].filter(Boolean).join(" · ")}</span>` : ""}
      </button>`)}</section>`)}</div>`;
  }

  render() {
    if (this.error) return html`<p class="err">${this.error}</p>`;
    const month = this.view === "month";
    return html`<div class="bar">
        ${month ? html`<h2>${this.month.toLocaleDateString(locale(), { month: "long", year: "numeric" })}</h2>
          <button type="button" class="ghost" aria-label=${t("cal.prev")} @click=${() => this.shift(-1)}>${icon("ChevronLeft", 15)}</button>
          <button type="button" class="ghost" @click=${() => this.shift(0)}>${t("cal.today")}</button>
          <button type="button" class="ghost" aria-label=${t("cal.next")} @click=${() => this.shift(1)}>${icon("ChevronRight", 15)}</button>` : ""}
        <span class="sp"></span>${this.chips()}</div>
      ${!this.data ? html`<p class="empty">${t("app.loading")}</p>` : month ? this.monthView() : this.listView()}`;
  }
}
customElements.get("phx-calendar") || customElements.define("phx-calendar", PhxCalendar);

/** ```calendar block: "view: list", "sources: …", "kinds: …", "calendar: <doc>", "days: 30". */
export function parseCalendarBlock(src: string): { spec: CalendarSpec; doc: string; problems: string[] } {
  const spec: CalendarSpec = { view: "month" };
  let doc = "";
  const problems: string[] = [];
  const names: Record<string, string> = { maintenance: "maintenance", wartung: "maintenance", tasks: "tasks", cards: "tasks", karten: "tasks", entities: "entities", entries: "entities", "einträge": "entities", external: "external", extern: "external" };
  for (const raw of src.replace(/\r\n/g, "\n").split("\n")) {
    const l = raw.trim();
    if (!l || l.startsWith("#")) continue;
    const m = /^([A-Za-zä]+)\s*:\s*(.*)$/.exec(l);
    const k = m?.[1].toLowerCase() ?? "", v = m?.[2].trim() ?? "";
    const list = () => v.split(",").map((x) => x.trim()).filter(Boolean);
    if (k === "view" || k === "ansicht") spec.view = /^(list|liste)$/i.test(v) ? "list" : "month";
    else if (k === "sources" || k === "quellen") spec.sources = list().map((x) => names[x.toLowerCase()]).filter(Boolean);
    else if (k === "kinds" || k === "arten") spec.kinds = list();
    else if (k === "calendar" || k === "kalender") doc = v;
    else if (k === "days" || k === "tage") spec.days = Math.min(366, Math.max(1, Number(v) || 60));
    else problems.push(l);
  }
  return { spec, doc, problems };
}

/** <phx-calendar-block data-project data-src>: the block in pages. */
export class PhxCalendarBlock extends HTMLElement {
  connectedCallback() {
    const { spec, doc } = parseCalendarBlock(this.dataset.src ?? this.querySelector("pre")?.textContent ?? "");
    const el = document.createElement("phx-calendar") as unknown as PhxCalendar;
    el.project = this.dataset.project ?? "";
    el.doc = doc;
    el.spec = spec;
    el.compact = true;
    el.style.margin = "1em 0";
    this.replaceChildren(el);
  }
}
customElements.get("phx-calendar-block") || customElements.define("phx-calendar-block", PhxCalendarBlock);

/** A calendar document: its filters, editable by editors, and the calendar. */
export class PhxCalendarDoc extends LitElement {
  static properties = { project: {}, doc: { attribute: false }, canEdit: { type: Boolean }, editing: { state: true }, spec: { state: true }, cals: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .filters { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); margin-bottom: 14px; }
    .opts { display: flex; gap: 12px; flex-wrap: wrap; }
    .opts label { display: inline-flex; gap: 6px; align-items: center; font-size: 13.5px; }
    .top { display: flex; justify-content: flex-end; margin-bottom: 6px; }
    .acts { display: flex; gap: 8px; }
  `];
  project = "";
  doc?: { slug: string; id: string; content: CalendarSpec };
  canEdit = false;
  editing = false;
  spec: CalendarSpec = {};
  cals: ExternalCalendar[] = [];

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("doc") && this.doc) this.spec = { ...(this.doc.content ?? {}) };
  }

  private async edit() {
    this.editing = true;
    const today = dayKey(new Date());
    const v = await api<CalendarView>("GET", `/projects/${this.project}/calendar?from=${today}&to=${dayKey(addDays(new Date(), 1))}&sources=external`).catch(() => undefined);
    this.cals = v?.calendars ?? [];
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const spec: CalendarSpec = {
      view: f.get("view") === "list" ? "list" : "month",
      sources: SOURCES.filter((s) => f.get(`src-${s}`) === "on"),
      kinds: String(f.get("kinds") ?? "").split(",").map((x) => x.trim()).filter(Boolean),
      calendars: this.cals.filter((c) => f.get(`cal-${c.id}`) === "on").map((c) => c.id),
      days: Number(f.get("days")) || undefined,
    };
    if (spec.sources!.length === SOURCES.length) spec.sources = [];
    try {
      await api("PATCH", `/projects/${this.project}/documents/${this.doc!.id}`, { calendar: spec });
      this.spec = spec;
      this.editing = false;
      this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
    } catch (err) {
      const { toast } = await import("./toasts");
      toast((err as { text?: string })?.text ?? t("error.unknown"), "error");
    }
  }

  render() {
    const s = this.spec;
    const all = !s.sources?.length;
    return html`${this.canEdit && !this.editing ? html`<div class="top"><button class="ghost" @click=${() => this.edit()}>${icon("SlidersHorizontal", 14)} ${t("cal.filters")}</button></div>` : ""}
      ${this.editing ? html`<form class="filters" @submit=${this.save}>
        <div class="opts"><label><input type="radio" name="view" value="month" ?checked=${s.view !== "list"} /> ${t("cal.view_month")}</label>
          <label><input type="radio" name="view" value="list" ?checked=${s.view === "list"} /> ${t("cal.view_list")}</label>
          <label>${t("cal.days")} <input name="days" type="number" min="1" max="366" style="width:5em" .value=${String(s.days ?? 60)} /></label></div>
        <div class="opts" role="group" aria-label=${t("cal.sources")}>${SOURCES.map((src) => html`<label><input type="checkbox" name="src-${src}" ?checked=${all || s.sources!.includes(src)} /> ${t(`cal.src.${src}` as any)}</label>`)}</div>
        <label class="field">${t("cal.kinds")}<input name="kinds" .value=${(s.kinds ?? []).join(", ")} placeholder="car, contract" /></label>
        ${this.cals.length ? html`<div class="opts" role="group" aria-label=${t("cal.external")}>${this.cals.map((c) => html`<label><input type="checkbox" name="cal-${c.id}" ?checked=${!s.calendars?.length || s.calendars.includes(c.id)} /> ${c.name}</label>`)}</div>` : ""}
        <div class="acts"><button>${t("doc.save")}</button><button type="button" class="ghost" @click=${() => (this.editing = false)}>${t("doc.cancel")}</button></div>
      </form>` : ""}
      <phx-calendar .project=${this.project} .spec=${s} filters></phx-calendar>`;
  }
}
customElements.get("phx-calendar-doc") || customElements.define("phx-calendar-doc", PhxCalendarDoc);
