import { LitElement, css, html } from "lit";
import { api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

interface Gap { kind: string; subject: string; title: string; detail?: Record<string, unknown> }
interface GapReport { gaps: Gap[]; counts: Record<string, number> }
interface PageChange { slug: string; title: string; created?: boolean; deleted?: boolean; edits: number; authors: string[] }
interface EntryChange { slug: string; name: string; kind: string; added?: boolean; removed?: boolean }
interface ChangeSummary { since: string; pages: PageChange[]; entries: EntryChange[]; tasksDone: string[]; comments: number; people: string[] }

type Key = Parameters<typeof t>[0];
const KINDS = ["failing_check", "rule_violation", "review_due", "broken_mention", "unplaced_device", "undocumented_entry", "incomplete_entry", "empty_page", "stale_page"];
const DOC_KINDS = new Set(["broken_mention", "empty_page", "stale_page", "review_due"]);

/** Project insights without AI: what changed recently and where documentation is missing. */
export class PhxInsights extends LitElement {
  static properties = { project: {}, days: { state: true }, changes: { state: true }, gaps: { state: true }, skills: { state: true }, open: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 980px; margin: 0 auto; padding: 24px 16px 60px; }
    ul.skills { list-style: none; padding: 0; display: grid; gap: 8px; }
    ul.skills code { font-family: var(--font-mono); }
    h2 { font-size: 18px; margin: 28px 0 10px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    h2 select { width: auto; margin-left: auto; font-size: 14px; font-weight: 400; }
    .card { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; background: var(--surface); margin-bottom: 12px; }
    ul { list-style: none; padding: 0; margin: 6px 0 0; display: grid; gap: 4px; font-size: 14px; }
    .tag { font-size: 12px; color: var(--muted); margin-left: 6px; }
    .stats { display: flex; gap: 18px; flex-wrap: wrap; color: var(--muted); font-size: 14px; }
    .stats b { color: var(--text); }
    details summary { cursor: pointer; font-weight: 600; display: flex; gap: 8px; align-items: center; }
    .count { background: var(--surface-2, var(--line)); border-radius: 999px; padding: 0 8px; font-size: 12.5px; font-weight: 500; }
  `];
  project = "";
  days = 7;
  changes?: ChangeSummary;
  gaps?: GapReport;
  skills?: { name: string; description: string; document: string; title: string }[];

  connectedCallback() {
    super.connectedCallback();
    this.loadChanges();
    api<NonNullable<PhxInsights["skills"]>>("GET", `/projects/${this.project}/skills`).then((x) => (this.skills = x)).catch(() => (this.skills = []));
    api<GapReport>("GET", `/projects/${this.project}/gaps`).then((g) => (this.gaps = g)).catch(() => (this.gaps = { gaps: [], counts: {} }));
  }

  private async loadChanges() {
    this.changes = undefined;
    this.changes = await api<ChangeSummary>("GET", `/projects/${this.project}/changes?days=${this.days}`).catch(() => undefined);
  }

  private link(g: Gap) {
    if (g.kind === "failing_check") return `/p/${this.project}/monitoring`;
    return DOC_KINDS.has(g.kind) && !g.detail?.entity ? `/p/${this.project}/d/${g.subject}` : `/p/${this.project}/e/${g.subject}`;
  }

  private gapNote(g: Gap) {
    const d = g.detail ?? {};
    if (g.kind === "broken_mention") return (d.mentions as string[]).map((m) => "@" + m).join(", ");
    if (g.kind === "incomplete_entry") return t("insights.missing", { fields: (d.missing as string[]).join(", ") });
    if (g.kind === "stale_page" && d.updatedAt) return t("insights.updated", { date: formatDate(String(d.updatedAt)) });
    if (g.kind === "failing_check" && d.detail) return String(d.detail);
    if (g.kind === "review_due" && d.due) return formatDate(String(d.due));
    if (g.kind === "rule_violation") return (d.rules as string[]).map((r) => r === "fields" ? t("rules.violation.fields", { fields: (d.missing as string[]).join(", ") }) : t(`rules.violation.${r}` as Key)).join(" · ");
    return typeof d.kind === "string" ? d.kind : "";
  }

  private renderChanges() {
    const c = this.changes;
    if (!c) return html`<p class="muted">${t("app.loading")}</p>`;
    if (!c.pages.length && !c.entries.length && !c.tasksDone.length && !c.comments) return html`<p class="muted">${t("insights.no_changes")}</p>`;
    return html`
      <p class="stats">
        <span><b>${c.pages.length}</b> ${t("insights.pages")}</span>
        <span><b>${c.entries.length}</b> ${t("insights.entries")}</span>
        <span><b>${c.tasksDone.length}</b> ${t("insights.tasks_done")}</span>
        <span><b>${c.comments}</b> ${t("insights.comments")}</span>
        ${c.people.length ? html`<span>${t("insights.by", { people: c.people.join(", ") })}</span>` : ""}
      </p>
      ${c.pages.length ? html`<div class="card"><b>${t("insights.pages")}</b><ul>${c.pages.map((p) => html`<li>
        ${p.deleted ? html`<s>${p.title}</s>` : html`<a href="/p/${this.project}/d/${p.slug}">${p.title}</a>`}
        <span class="tag">${p.created ? t("insights.created") : p.deleted ? t("insights.deleted") : t("insights.edits", { n: p.edits })}${p.authors.length ? " · " + p.authors.join(", ") : ""}</span></li>`)}</ul></div>` : ""}
      ${c.entries.length ? html`<div class="card"><b>${t("insights.entries")}</b><ul>${c.entries.map((e) => html`<li>
        ${e.removed ? html`<s>${e.name}</s>` : html`<a href="/p/${this.project}/e/${e.slug}">${e.name}</a>`}
        <span class="tag">${e.kind} · ${e.added ? t("insights.added") : e.removed ? t("insights.removed") : t("insights.changed")}</span></li>`)}</ul></div>` : ""}
      ${c.tasksDone.length ? html`<div class="card"><b>${t("insights.tasks_done")}</b><ul>${c.tasksDone.map((x) => html`<li>${x}</li>`)}</ul></div>` : ""}`;
  }

  private renderGaps() {
    const r = this.gaps;
    if (!r) return html`<p class="muted">${t("app.loading")}</p>`;
    if (!r.gaps.length) return html`<p class="muted">${t("insights.no_gaps")}</p>`;
    return KINDS.filter((k) => r.counts[k]).map((k) => html`<details class="card" ?open=${k === "failing_check" || k === "broken_mention"}>
      <summary>${t(`insights.gap.${k}` as Key)} <span class="count">${r.counts[k]}</span></summary>
      <p class="muted">${t(`insights.gap.${k}.hint` as Key)}</p>
      <ul>${r.gaps.filter((g) => g.kind === k).map((g) => html`<li><a href=${this.link(g)}>${g.title || g.subject}</a> <span class="tag">${this.gapNote(g)}</span></li>`)}</ul>
    </details>`);
  }

  render() {
    return html`
      <h1>${t("insights.title")}</h1>
      <h2>${icon("History", 18)} ${t("insights.changes")}
        <select aria-label=${t("insights.period")} @change=${(e: Event) => { this.days = Number((e.target as HTMLSelectElement).value); this.loadChanges(); }}>
          ${[7, 30, 90].map((d) => html`<option value=${d} ?selected=${d === this.days}>${t("insights.days", { n: d })}</option>`)}
        </select></h2>
      ${this.renderChanges()}
      <h2>${icon("ListChecks", 18)} ${t("insights.gaps")}</h2>
      ${this.renderGaps()}
      <h2>${icon("Sparkles", 18)} ${t("skill.list_title")}
        ${this.skills?.length ? html`<a class="btn ghost" href="/api/v1/projects/${this.project}/skills.zip" download>${icon("Download", 14)} ${t("skill.download_all")}</a>` : ""}</h2>
      ${!this.skills ? "" : this.skills.length ? html`<ul class="skills">${this.skills.map((x) => html`<li>
          <a href="/p/${this.project}/d/${x.document}"><code>${x.name}</code></a> <span class="muted">${x.description}</span></li>`)}</ul>`
        : html`<p class="muted">${t("skill.none")}</p>`}`;
  }
}
customElements.define("phx-insights", PhxInsights);
