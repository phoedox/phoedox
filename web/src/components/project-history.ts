import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Point { documentId: string; slug: string; type: string; version: number; title: string; actor: string; actorName?: string; viaAI?: boolean; createdAt: string }
interface Change { id: string; slug: string; type: string; title: string; category: string; number?: number; change: "changed" | "deleted" | "created"; versionAt: number; version: number }
interface StateAt { at: string; changes: Change[]; unchanged: number }

const typeIcon: Record<string, string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot", whiteboard: "Presentation" };

const day = (iso: string) => new Date(iso).toLocaleDateString(document.documentElement.lang || "en", { weekday: "short", day: "numeric", month: "long", year: "numeric" });
const time = (iso: string) => new Date(iso).toLocaleTimeString(document.documentElement.lang || "en", { hour: "2-digit", minute: "2-digit" });

/** Project history: every saved change is a point; look at the project as it
 * was right after it and roll every page back to that point. */
export class PhxProjectHistory extends LitElement {
  static properties = { project: {}, points: { state: true }, sel: { state: true }, state: { state: true }, busy: { state: true }, confirming: { state: true }, more: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .lead { color: var(--muted); max-width: 70ch; margin: 4px 0 18px; }
    .cols { display: grid; grid-template-columns: minmax(260px, 360px) minmax(0, 1fr); gap: 24px; align-items: start; }
    @media (max-width: 860px) { .cols { grid-template-columns: 1fr; } }
    .points { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); max-height: calc(100dvh - var(--header-h) - 180px); overflow: auto; }
    .day { position: sticky; top: 0; z-index: 1; padding: 6px 12px; font-size: 12px; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; color: var(--muted); background: var(--surface2); }
    .points button { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 10px; align-items: center; width: 100%; padding: 9px 12px; border: 0; border-top: 1px solid var(--line);
      background: transparent; color: var(--fg); text-align: left; border-radius: 0; font-size: 14px; }
    .points button[aria-current="true"] { background: var(--accent-soft); }
    @media (hover: hover) and (pointer: fine) { .points button:hover:not([aria-current="true"]) { background: var(--surface2); } }
    .points .t { min-width: 0; } .points .t b { display: block; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .points .t span, .points .tm { font-size: 12.5px; color: var(--muted); }
    .points .ico { color: var(--muted); }
    .panel { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 16px 18px; display: grid; gap: 12px; }
    .panel h2 { font-size: 18px; margin: 0; }
    .sum { display: flex; gap: 8px; flex-wrap: wrap; }
    ul.ch { list-style: none; margin: 0; padding: 0; display: grid; }
    ul.ch li { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 8px 0; border-top: 1px solid var(--line); font-size: 14px; }
    ul.ch li a { color: var(--fg); text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    ul.ch li .ico { color: var(--muted); }
    .chip.changed { background: var(--tb-blue); color: var(--tc-blue); } .chip.deleted { background: var(--tb-red); color: var(--tc-red); } .chip.created { background: var(--tb-green); color: var(--tc-green); }
    .acts { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .warn { font-size: 13.5px; color: var(--muted); }
    .confirm { border: 1px solid color-mix(in srgb, var(--bad) 40%, var(--line)); border-radius: 10px; padding: 12px; display: grid; gap: 10px; }
    .more { width: 100%; border: 0; border-top: 1px solid var(--line); border-radius: 0; background: transparent; color: var(--accent); padding: 10px; }
  `];

  project = "";
  points: Point[] = [];
  sel?: Point;
  state?: StateAt;
  busy = false;
  confirming = false;
  more = false;
  private loaded = "";

  async willUpdate() {
    if (this.project && this.project !== this.loaded) {
      this.loaded = this.project;
      await this.load();
    }
  }

  private async load(before?: string) {
    try {
      const page = await api<Point[]>("GET", `/projects/${this.project}/history/points?limit=100${before ? `&before=${encodeURIComponent(before)}` : ""}`);
      this.points = before ? [...this.points, ...page] : page;
      this.more = page.length === 100;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async pick(p: Point) {
    this.sel = p;
    this.state = undefined;
    this.confirming = false;
    try {
      this.state = await api<StateAt>("GET", `/projects/${this.project}/history/at?at=${encodeURIComponent(p.createdAt)}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async rollback() {
    if (!this.sel) return;
    this.busy = true;
    try {
      const r = await api<{ restored: number; revived: number; trashed: number; failed?: { title: string; error: string }[] }>("POST", `/projects/${this.project}/history/rollback`, { at: this.sel.createdAt });
      toast(t("phist.done", { restored: r.restored, revived: r.revived, trashed: r.trashed }));
      if (r.failed?.length) toast(t("phist.failed", { titles: r.failed.map((f) => f.title).join(", ") }), "error");
      docsVersion.set(docsVersion.get() + 1);
      this.confirming = false;
      this.loaded = "";
      this.sel = undefined;
      this.state = undefined;
      await this.willUpdate();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private pointList() {
    let last = "";
    return html`<div class="points" role="list" aria-label=${t("phist.points")}>
      ${this.points.map((p) => {
        const d = day(p.createdAt);
        const head = d !== last ? ((last = d), html`<div class="day">${d}</div>`) : "";
        return html`${head}<button role="listitem" aria-current=${this.sel === p ? "true" : "false"} @click=${() => this.pick(p)}>
          ${icon(typeIcon[p.type] ?? "FileText", 15)}
          <span class="t"><b>${p.title}</b><span>${t("history.version", { version: p.version })} · ${p.actorName || t("phist.someone")}${p.viaAI ? ` · ${t("phist.ai")}` : ""}</span></span>
          <span class="tm">${time(p.createdAt)}</span></button>`;
      })}
      ${this.more ? html`<button class="more" @click=${() => this.load(this.points[this.points.length - 1]?.createdAt)}>${t("phist.older")}</button>` : ""}
    </div>`;
  }

  private panel() {
    const p = this.sel;
    if (!p) return html`<div class="panel"><p class="muted">${t("phist.pick")}</p></div>`;
    const st = this.state;
    const owner = currentProject.get()?.role === "owner";
    const count = (k: Change["change"]) => st?.changes.filter((c) => c.change === k).length ?? 0;
    return html`<div class="panel" aria-live="polite">
      <h2>${t("phist.at", { date: day(p.createdAt), time: time(p.createdAt) })}</h2>
      <p class="muted" style="margin:0">${t("phist.after", { title: p.title, version: p.version })}</p>
      ${!st ? html`<p class="muted">${t("app.loading")}</p>` : !st.changes.length ? html`<p>${t("phist.same")}</p>` : html`
        <div class="sum">
          ${count("changed") ? html`<span class="chip changed">${t("phist.n_changed", { count: count("changed") })}</span>` : ""}
          ${count("deleted") ? html`<span class="chip deleted">${t("phist.n_deleted", { count: count("deleted") })}</span>` : ""}
          ${count("created") ? html`<span class="chip created">${t("phist.n_created", { count: count("created") })}</span>` : ""}
          <span class="chip">${t("phist.n_same", { count: st.unchanged })}</span>
        </div>
        <ul class="ch">${st.changes.map((c) => html`<li>
          ${icon(typeIcon[c.type] ?? "FileText", 15)}
          ${c.change === "created" ? html`<a href="/p/${this.project}/d/${c.id}">${c.title}</a>`
            : html`<a href="/p/${this.project}/d/${c.id}/history?v=${c.versionAt}" title=${t("phist.view_then")}>${c.title}</a>`}
          <span class="chip ${c.change}">${t(`phist.${c.change}` as any, { from: c.versionAt, to: c.version })}</span></li>`)}</ul>
        ${owner ? this.confirming ? html`<div class="confirm" role="alertdialog" aria-label=${t("phist.rollback")}>
            <p style="margin:0">${t("phist.confirm", { count: st.changes.length })}</p>
            <div class="acts"><button class="danger" ?disabled=${this.busy} @click=${this.rollback}>${icon("RotateCcw", 14)} ${t("phist.rollback_now")}</button>
              <button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button></div></div>`
          : html`<div class="acts"><button @click=${() => (this.confirming = true)}>${icon("RotateCcw", 14)} ${t("phist.rollback")}</button>
            <span class="warn">${t("phist.rollback_hint")}</span></div>`
        : html`<p class="warn">${t("phist.owner_only")}</p>`}`}
    </div>`;
  }

  render() {
    return html`<h1>${t("phist.title")}</h1>
      <p class="lead">${t("phist.lead")}</p>
      ${this.points.length ? html`<div class="cols">${this.pointList()}${this.panel()}</div>` : html`<p class="muted">${t("phist.none")}</p>`}`;
  }
}
customElements.define("phx-project-history", PhxProjectHistory);
