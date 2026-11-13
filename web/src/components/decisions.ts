import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, adrRef, api, decisionStatuses, type Decision, type DecisionStatus } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";

/** Decision log of a project: numbered, filterable by status (?status=…). */
export class PhxDecisions extends SignalWatcher(LitElement) {
  static properties = { project: {}, items: { state: true }, status: { state: true }, error: { state: true } };
  static styles = [base, css`
    header { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
    header p { margin: 4px 0 0; max-width: 60ch; }
    .filters { display: flex; gap: 6px; flex-wrap: wrap; margin: 18px 0 10px; }
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
  private loaded = "";

  connectedCallback() {
    super.connectedCallback();
    const s = new URLSearchParams(location.search).get("status") as DecisionStatus | null;
    this.status = s && decisionStatuses.includes(s) ? s : "";
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

  private filter(s: DecisionStatus | "") {
    this.status = s;
    history.replaceState(history.state, "", s ? `?status=${s}` : location.pathname);
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
    const shown = this.status ? all.filter((d) => d.status === this.status) : all;
    const bySlug = new Map(all.map((d) => [d.slug, d]));
    const count = (s: DecisionStatus) => all.filter((d) => d.status === s).length;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`<div class="enter">
      <header>
        <div><h1>${t("decisions.title")}</h1><p class="muted">${t("decisions.intro")}</p></div>
        ${canEdit ? html`<a class="btn" href="/p/${this.project}/new/adr">${icon("Plus", 15)} ${t("decisions.new")}</a>` : ""}
      </header>
      ${all.length === 0 ? html`<p class="muted">${t("decisions.empty")}</p>` : html`
        <div class="filters" role="group" aria-label=${t("decisions.filter")}>
          <button aria-pressed=${this.status === ""} @click=${() => this.filter("")}>${t("decisions.all")} <span class="count">${all.length}</span></button>
          ${decisionStatuses.filter((s) => count(s) > 0 || s === this.status).map((s) => html`
            <button aria-pressed=${this.status === s} @click=${() => this.filter(s)}>${t(`adr.status.${s}`)} <span class="count">${count(s)}</span></button>`)}
        </div>
        ${shown.length === 0 ? html`<p class="muted">${t("decisions.empty_filter")}</p>` : html`
          <table>
            <thead><tr><th>${t("decisions.number")}</th><th>${t("doc.title")}</th><th>${t("adr.status")}</th><th class="date">${t("adr.date")}</th></tr></thead>
            <tbody>${shown.map((d) => this.row(d, bySlug))}</tbody>
          </table>`}`}
    </div>`;
  }
}
customElements.define("phx-decisions", PhxDecisions);
