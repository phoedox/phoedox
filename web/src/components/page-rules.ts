import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, formatDay, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Reader { name: string; version: number; current: boolean; confirmedAt: string | null }
interface Rules {
  reviewDays: number; reviewedAt: string | null; reviewedBy?: string; reviewDue?: string; overdue: boolean;
  requiredReading: boolean; confirmed: boolean; readers?: Reader[];
}
const INTERVALS = [0, 30, 90, 180, 365, 730];

/** Review date and required reading of a page: banners for everyone, the
 * settings and who confirmed for editors. */
export class PhxPageRules extends LitElement {
  static properties = { project: {}, doc: {}, version: {}, canEdit: { type: Boolean }, mode: {}, rules: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .banner { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; border: 1px solid var(--line); border-radius: 10px; padding: 10px 14px; margin: 0 0 16px; background: var(--surface); font-size: 14px; }
    .banner.warn { border-color: var(--warn, var(--accent)); }
    .banner span { flex: 1; min-width: 200px; }
    details { margin: 28px 0 0; border-top: 1px solid var(--line); padding-top: 12px; font-size: 14px; }
    summary { cursor: pointer; color: var(--muted); display: inline-flex; gap: 6px; align-items: center; }
    .grid { display: flex; gap: 16px; flex-wrap: wrap; align-items: end; margin: 12px 0; }
    .grid select { width: auto; }
    label.check { display: inline-flex; gap: 8px; align-items: center; }
    label.check input { width: auto; }
    ul { list-style: none; padding: 0; margin: 6px 0 0; display: grid; gap: 4px; }
    .ok { color: var(--good); }
    .missing { color: var(--muted); }
  `];
  project = "";
  doc = "";
  version = 0;
  canEdit = false;
  /** "banner" (above the page) or "settings" (below it, editors). */
  mode: "banner" | "settings" = "banner";
  rules?: Rules;
  private onChanged = (e: Event) => {
    const d = (e as CustomEvent).detail;
    if (d?.doc === this.doc && d.from !== this) this.rules = d.rules;
  };

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("phx-page-rules", this.onChanged);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("phx-page-rules", this.onChanged);
  }
  busy = false;

  updated(changed: Map<string, unknown>) {
    if (changed.has("doc") || changed.has("version")) this.load();
  }

  private async load() {
    if (!this.doc) return;
    this.rules = await api<Rules>("GET", `/projects/${this.project}/documents/${this.doc}/rules`).catch(() => undefined);
  }

  private async call(method: string, path: string, body?: unknown, ok?: string) {
    this.busy = true;
    try {
      this.rules = await api<Rules>(method, `/projects/${this.project}/documents/${this.doc}/${path}`, body);
      window.dispatchEvent(new CustomEvent("phx-page-rules", { detail: { doc: this.doc, rules: this.rules, from: this } }));
      if (ok) toast(ok, "ok");
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const r = this.rules;
    if (!r) return "";
    if (this.mode === "settings") return this.canEdit ? this.settings(r) : r.reviewDue && !r.overdue ? html`<p class="muted">${t("rules.next_review", { date: formatDay(r.reviewDue) })}</p>` : "";
    return html`
      ${r.requiredReading && !r.confirmed ? html`<div class="banner warn" role="note">${icon("BookOpenCheck", 16)}
        <span>${t("rules.must_read")}</span>
        <button ?disabled=${this.busy} @click=${() => this.call("POST", "read", undefined, t("rules.read_toast"))}>${icon("Check", 14)} ${t("rules.confirm_read")}</button></div>` : ""}
      ${r.reviewDue && r.overdue ? html`<div class="banner warn" role="note">${icon("CalendarClock", 16)}
        <span>${t("rules.review_overdue", { date: formatDay(r.reviewDue) })}</span>
        ${this.canEdit ? html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.call("POST", "reviewed", undefined, t("rules.reviewed_toast"))}>${icon("CircleCheck", 14)} ${t("rules.mark_reviewed")}</button>` : ""}</div>` : ""}
`;
  }

  private settings(r: Rules) {
    const confirmed = r.readers?.filter((x) => x.current).length ?? 0;
    return html`<details>
      <summary>${icon("CalendarClock", 14)} ${t("rules.title")}${r.reviewDue ? html` · ${t("rules.next_review", { date: formatDay(r.reviewDue) })}` : ""}${r.requiredReading ? html` · ${t("rules.confirmed_count", { n: confirmed, total: r.readers?.length ?? 0 })}` : ""}</summary>
      <div class="grid">
        <label>${t("rules.review_every")}
          <select ?disabled=${this.busy} @change=${(e: Event) => this.call("PUT", "rules", { reviewDays: Number((e.target as HTMLSelectElement).value) })}>
            ${INTERVALS.map((d) => html`<option value=${d} ?selected=${d === r.reviewDays}>${d ? t("rules.days", { n: d }) : t("rules.no_review")}</option>`)}
            ${INTERVALS.includes(r.reviewDays) ? "" : html`<option selected value=${r.reviewDays}>${t("rules.days", { n: r.reviewDays })}</option>`}
          </select></label>
        <label class="check"><input type="checkbox" .checked=${r.requiredReading} ?disabled=${this.busy}
          @change=${(e: Event) => this.call("PUT", "rules", { requiredReading: (e.target as HTMLInputElement).checked })}> ${t("rules.required_reading")}</label>
        ${r.reviewDays ? html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.call("POST", "reviewed", undefined, t("rules.reviewed_toast"))}>${icon("CircleCheck", 14)} ${t("rules.mark_reviewed")}</button>` : ""}
      </div>
      ${r.reviewedAt ? html`<p class="muted">${t("rules.last_review", { date: formatDate(r.reviewedAt), name: r.reviewedBy ?? "" })}</p>` : ""}
      ${r.requiredReading && r.readers ? html`<ul aria-label=${t("rules.readers")}>${r.readers.map((x) => html`<li class=${x.current ? "ok" : "missing"}>
        ${icon(x.current ? "CircleCheck" : "Clock", 13)} ${x.name}
        <span class="muted">${x.current ? formatDate(x.confirmedAt!) : x.version ? t("rules.read_older", { v: x.version }) : t("rules.not_read")}</span></li>`)}</ul>` : ""}
    </details>`;
  }
}
customElements.define("phx-page-rules", PhxPageRules);
