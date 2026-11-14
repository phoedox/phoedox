import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, formatDay, t } from "../i18n";
import { icon } from "../icon";
import { attrLabel, type Kind } from "../model";
import { base } from "./shared";
import { toast } from "./toasts";

interface Meta {
  owner?: { actor: string; name: string }; ownerId?: string; reviewDays: number; kindReviewDays: number; reviewedAt: string | null;
  reviewDue?: string; overdue: boolean; violations: { rules: string[]; missing?: string[] }; personOwner: boolean;
}
interface Member { userId: string; name: string; email: string }
const INTERVALS = [0, 30, 90, 180, 365, 730];
type Key = Parameters<typeof t>[0];

/** Responsible person, review date and broken documentation rules of an entry. */
export class PhxEntityMeta extends LitElement {
  static properties = { project: {}, entity: {}, kind: { attribute: false }, canEdit: { type: Boolean }, meta: { state: true }, members: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 0 0 14px; }
    .banner { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; border: 1px solid var(--warn, var(--accent)); border-radius: 10px; padding: 10px 14px; margin-bottom: 10px; background: var(--surface); font-size: 14px; }
    .banner span { flex: 1; min-width: 200px; }
    .bar { display: flex; gap: 14px; align-items: center; flex-wrap: wrap; font-size: 14px; color: var(--muted); }
    .bar select { width: auto; }
    .bar label { display: inline-flex; gap: 6px; align-items: center; }
  `];
  project = "";
  entity = "";
  kind?: Kind;
  canEdit = false;
  meta?: Meta;
  members: Member[] = [];
  busy = false;

  updated(changed: Map<string, unknown>) {
    if (changed.has("entity")) this.load();
  }

  async load() {
    this.meta = await api<Meta>("GET", `/projects/${this.project}/entities/${this.entity}/meta`).catch(() => undefined);
    if (this.canEdit && !this.members.length) this.members = (await api<Member[]>("GET", `/projects/${this.project}/members`).catch(() => [])) ?? [];
  }

  private async call(method: string, path: string, body?: unknown, ok?: string) {
    this.busy = true;
    try {
      this.meta = await api<Meta>(method, `/projects/${this.project}/entities/${this.entity}/${path}`, body);
      if (ok) toast(ok, "ok");
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private violation(rule: string, m: Meta) {
    if (rule === "fields") {
      const names = (m.violations.missing ?? []).map((f) => attrLabel(this.kind?.attrs.find((a) => a.name === f) ?? { name: f, type: "text" }));
      return t("rules.violation.fields", { fields: names.join(", ") });
    }
    return t(`rules.violation.${rule}` as Key);
  }

  render() {
    const m = this.meta;
    if (!m) return "";
    const v = m.violations.rules;
    const days = m.reviewDays || m.kindReviewDays;
    return html`
      ${v.length ? html`<div class="banner" role="note">${icon("ListChecks", 16)}
        <span>${t("rules.violations")}: ${v.map((r) => this.violation(r, m)).join(" · ")}</span></div>` : ""}
      ${m.overdue && m.reviewDue ? html`<div class="banner" role="note">${icon("CalendarClock", 16)}
        <span>${t("rules.entity_overdue", { date: formatDay(m.reviewDue) })}</span>
        ${this.canEdit ? html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.call("POST", "reviewed", undefined, t("rules.reviewed_toast"))}>${icon("CircleCheck", 14)} ${t("rules.mark_reviewed")}</button>` : ""}</div>` : ""}
      <div class="bar">
        ${this.canEdit ? html`<label>${icon("UserRound", 14)} ${t("rules.responsible")}
          <select ?disabled=${this.busy} @change=${(e: Event) => this.call("PUT", "meta", { owner: (e.target as HTMLSelectElement).value })}>
            <option value="" ?selected=${!m.ownerId}>${t("rules.nobody")}</option>
            ${this.members.map((x) => html`<option value=${x.userId} ?selected=${x.userId === m.ownerId}>${x.name || x.email}</option>`)}
          </select></label>
          <label>${icon("CalendarClock", 14)} ${t("rules.review_every")}
          <select ?disabled=${this.busy} @change=${(e: Event) => this.call("PUT", "meta", { reviewDays: Number((e.target as HTMLSelectElement).value) })}>
            ${INTERVALS.map((d) => html`<option value=${d} ?selected=${d === m.reviewDays}>${d ? t("rules.days", { n: d }) : m.kindReviewDays ? t("rules.kind_default", { n: m.kindReviewDays }) : t("rules.no_review")}</option>`)}
          </select></label>`
        : html`${m.owner ? html`<span>${icon("UserRound", 14)} ${t("rules.responsible")}: ${m.owner.name}</span>` : ""}`}
        ${days && m.reviewDue && !m.overdue ? html`<span>${t("rules.next_review", { date: formatDay(m.reviewDue) })}</span>` : ""}
        ${m.reviewedAt ? html`<span>${t("rules.last_review_short", { date: formatDate(m.reviewedAt) })}</span>` : ""}
      </div>`;
  }
}
customElements.define("phx-entity-meta", PhxEntityMeta);
