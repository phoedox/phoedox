import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, adrRef, api, decisionStatuses, type Decision, type DecisionStatus } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";

/** Decision log of a project: numbered, filterable by status (?status=…). */
export class PhxDecisions extends SignalWatcher(LitElement) {
  static properties = { project: {}, items: { state: true }, status: { state: true }, error: { state: true }, query: { state: true }, hits: { state: true },
    sort: { state: true }, desc: { state: true }, page: { state: true } };
  static styles = [base, css`
    header { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
    header p { margin: 4px 0 0; max-width: 60ch; }
    .bar { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; margin: 18px 0 10px; }
    .search { position: relative; flex: 1 1 240px; max-width: 360px; }
    .search .ico { position: absolute; left: 10px; top: 50%; transform: translateY(-50%); color: var(--muted); pointer-events: none; }
    .search input { padding-left: 32px; }
    .filters { display: flex; gap: 6px; flex-wrap: wrap; }
    th button { all: unset; cursor: pointer; display: inline-flex; gap: 4px; align-items: center; border-radius: 4px; }
    th button:focus-visible { outline: 2px solid var(--accent); }
    th[aria-sort="ascending"] .ico, th[aria-sort="descending"] .ico { color: var(--fg); }
    th[aria-sort="ascending"] .ico { transform: rotate(180deg); }
    th .ico { opacity: .5; transition: transform var(--dur-fast) var(--ease-out); }
    th[aria-sort="ascending"] .ico, th[aria-sort="descending"] .ico { opacity: 1; }
    .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
    .pager { display: flex; gap: 8px; align-items: center; justify-content: flex-end; margin-top: 12px; font-size: 13px; color: var(--muted); }
    .pager button { padding: 4px 10px; }
    .filters button { font-size: 13px; font-weight: 500; padding: 4px 11px; border-radius: 999px; background: var(--surface); color: var(--muted); border: 1px solid var(--line); }
    .filters button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); border-color: transparent; }
    .count { font-variant-numeric: tabular-nums; opacity: .75; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; }
    th { text-align: left; font-size: 12px; font-weight: 600; color: var(--muted); padding: 6px 10px; border-bottom: 1px solid var(--line); }
    td { padding: 10px; border-bottom: 1px solid var(--line); vertical-align: top; }
    tr { position: relative; }
    td { transition: background-color var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { tbody tr:hover td { background: var(--surface2); } }
    .num { font-family: var(--font-mono); font-size: 13px; color: var(--muted); white-space: nowrap; width: 1%; }
    a.title { color: var(--fg); text-decoration: none; font-weight: 500; }
    a.title::after { content: ""; position: absolute; inset: 0; }
    tr.superseded a.title, tr.rejected a.title, tr.deprecated a.title { color: var(--muted); }
    .sub { font-size: 12.5px; color: var(--muted); margin-top: 3px; }
    .sub a { position: relative; z-index: 1; font-family: var(--font-mono); font-size: 12px; white-space: nowrap; }
    .date { white-space: nowrap; color: var(--muted); font-variant-numeric: tabular-nums; }
    @media (max-width: 700px) { .date { display: none; } }
  `];
  project = "";
  items?: Decision[];
  status: DecisionStatus | "" = "";
  error = "";
  /** Text search (server side, over number, title and text). */
  query = "";
  /** Matching decision numbers, undefined without a query. */
  hits?: Set<number>;
  sort: "number" | "title" | "status" | "date" = "number";
  desc = false;
  page = 0;
  private loaded = "";
  private timer?: number;

  connectedCallback() {
    super.connectedCallback();
    const q = new URLSearchParams(location.search);
    const s = q.get("status") as DecisionStatus | null;
    this.status = s && decisionStatuses.includes(s) ? s : "";
    this.query = q.get("q") ?? "";
    if (this.query) this.search(this.query, true);
  }

  async willUpdate() {
    const key = `${this.project}/${docsVersion.get()}`;
    if (this.project && key !== this.loaded) {
      this.loaded = key;
      try {
        this.items = await api<Decision[]>("GET", `/projects/${this.project}/decisions`);
      } catch (e) {
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
      }
    }
  }

  private syncURL() {
    const q = new URLSearchParams();
    if (this.status) q.set("status", this.status);
    if (this.query) q.set("q", this.query);
    history.replaceState(history.state, "", q.size ? `?${q}` : location.pathname);
  }

  private filter(s: DecisionStatus | "") {
    this.status = s;
    this.page = 0;
    this.syncURL();
  }

  private search(q: string, now = false) {
    this.query = q;
    this.page = 0;
    window.clearTimeout(this.timer);
    const run = async () => {
      this.syncURL();
      if (!q.trim()) {
        this.hits = undefined;
        return;
      }
      const res = await api<Decision[]>("GET", `/projects/${this.project}/decisions?q=${encodeURIComponent(q)}`).catch(() => undefined);
      if (res && this.query === q) this.hits = new Set(res.map((d) => d.number));
    };
    if (now) void run();
    else this.timer = window.setTimeout(run, 200);
  }

  private sortBy(col: PhxDecisions["sort"]) {
    if (this.sort === col) this.desc = !this.desc;
    else {
      this.sort = col;
      this.desc = col === "date";
    }
    this.page = 0;
  }

  private th(col: PhxDecisions["sort"], label: string, cls = "") {
    const sorted = this.sort === col ? (this.desc ? "descending" : "ascending") : undefined;
    return html`<th class=${cls} aria-sort=${sorted ?? "none"}><button @click=${() => this.sortBy(col)}>${label} ${icon("ChevronDown", 12)}</button></th>`;
  }

  private row(d: Decision, bySlug: Map<string, Decision>) {
    const link = (slug: string) => {
      const o = bySlug.get(slug);
      return html`<a href="/p/${this.project}/d/${slug}" title=${o?.title ?? slug}>${o ? adrRef(o.number) : slug}</a>`;
    };
    return html`<tr class=${d.status}>
      <td class="num">${d.ref}</td>
      <td>
        <a class="title" href="/p/${this.project}/d/${d.slug}">${d.title}</a>
        ${d.status === "superseded" && d.supersededBy ? html`<div class="sub">${t("adr.superseded_by")} ${link(d.supersededBy)}</div>` : ""}
        ${d.supersedes ? html`<div class="sub">${t("adr.supersedes")} ${link(d.supersedes)}</div>` : ""}
      </td>
      <td><span class="chip status-${d.status}">${t(`adr.status.${d.status}`)}</span></td>
      <td class="date">${d.date ?? ""}</td>
    </tr>`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    if (!this.items) return html`<p class="muted">${t("app.loading")}</p>`;
    const all = this.items;
    const PAGE = 25;
    const order = decisionStatuses as readonly string[];
    const cmp: Record<PhxDecisions["sort"], (a: Decision, b: Decision) => number> = {
      number: (a, b) => a.number - b.number,
      title: (a, b) => a.title.localeCompare(b.title),
      status: (a, b) => order.indexOf(a.status) - order.indexOf(b.status) || a.number - b.number,
      date: (a, b) => (a.date ?? "").localeCompare(b.date ?? "") || a.number - b.number,
    };
    const matching = all.filter((d) => (!this.status || d.status === this.status) && (!this.hits || this.hits.has(d.number)));
    const sorted = [...matching].sort((a, b) => (this.desc ? -1 : 1) * cmp[this.sort](a, b));
    const pages = Math.max(1, Math.ceil(sorted.length / PAGE));
    const page = Math.min(this.page, pages - 1);
    const shown = sorted.slice(page * PAGE, page * PAGE + PAGE);
    const bySlug = new Map(all.map((d) => [d.slug, d]));
    const searched = this.hits ? all.filter((d) => this.hits!.has(d.number)) : all;
    const count = (s: DecisionStatus) => searched.filter((d) => d.status === s).length;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`<div class="enter">
      <header>
        <div><h1>${t("decisions.title")}</h1><p class="muted">${t("decisions.intro")}</p></div>
        ${canEdit ? html`<a class="btn" href="/p/${this.project}/new/adr">${icon("Plus", 15)} ${t("decisions.new")}</a>` : ""}
      </header>
      ${all.length === 0 ? html`<p class="muted">${t("decisions.empty")}</p>` : html`
        <div class="bar">
          <label class="search"><span class="sr-only">${t("decisions.search")}</span>${icon("Search", 15)}
            <input type="search" placeholder=${t("decisions.search")} .value=${this.query} @input=${(e: Event) => this.search((e.target as HTMLInputElement).value)}></label>
          <div class="filters" role="group" aria-label=${t("decisions.filter")}>
            <button aria-pressed=${this.status === ""} @click=${() => this.filter("")}>${t("decisions.all")} <span class="count">${searched.length}</span></button>
            ${decisionStatuses.filter((s) => count(s) > 0 || s === this.status).map((s) => html`
              <button aria-pressed=${this.status === s} @click=${() => this.filter(s)}>${t(`adr.status.${s}`)} <span class="count">${count(s)}</span></button>`)}
          </div>
        </div>
        ${sorted.length === 0 ? html`<p class="muted">${t("decisions.empty_filter")}</p>` : html`
          <table>
            <thead><tr>${this.th("number", t("decisions.number"))}${this.th("title", t("doc.title"))}${this.th("status", t("adr.status"))}${this.th("date", t("adr.date"), "date")}</tr></thead>
            <tbody>${shown.map((d) => this.row(d, bySlug))}</tbody>
          </table>
          ${pages > 1 ? html`<nav class="pager" aria-label=${t("decisions.pages")}>
            <span>${t("decisions.range", { from: page * PAGE + 1, to: page * PAGE + shown.length, total: sorted.length })}</span>
            <button class="ghost" ?disabled=${page === 0} aria-label=${t("decisions.prev")} @click=${() => (this.page = page - 1)}>${icon("ArrowLeft", 14)}</button>
            <button class="ghost" ?disabled=${page >= pages - 1} aria-label=${t("decisions.next")} @click=${() => (this.page = page + 1)}>${icon("ArrowRight", 14)}</button>
          </nav>` : ""}`}`}
    </div>`;
  }
}
customElements.define("phx-decisions", PhxDecisions);
