import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { attrLabel, type Kind, type KindRules } from "../model";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

const INTERVALS = [0, 90, 180, 365, 730];

/** Documentation rules of a kind on the data model page (owners edit). */
export class PhxKindRules extends LitElement {
  static properties = { project: {}, kind: { attribute: false }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .checks { display: flex; flex-wrap: wrap; gap: 6px 16px; }
    label.check { display: inline-flex; gap: 6px; align-items: center; font-weight: 400; }
    label.check input { width: auto; }
    select { width: auto; }
    .lbl { padding: 0 0 4px; }
    p.sum { margin: 0; }
  `];
  project = "";
  kind?: Kind;
  busy = false;

  private get owner() {
    return currentProject.get()?.role === "owner";
  }

  private async save(change: Partial<KindRules>) {
    const k = this.kind!;
    const next: KindRules = { ...(k.rules ?? {}), ...change };
    this.busy = true;
    try {
      const res = await api<Kind>("PUT", `/projects/${this.project}/kinds/${k.id}/rules`, next);
      this.kind = { ...k, rules: res.rules };
      this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const k = this.kind;
    if (!k) return "";
    const r = k.rules ?? {};
    if (!this.owner) {
      const parts = [r.location && t("rules.kind.location"), r.page && t("rules.kind.page"), r.owner && t("rules.kind.owner"),
        r.fields?.length && t("rules.kind.fields_list", { fields: r.fields.map((f) => attrLabel(k.attrs.find((a) => a.name === f) ?? { name: f, type: "text" })).join(", ") }),
        r.reviewDays && t("rules.days", { n: r.reviewDays })].filter(Boolean);
      return html`<div class="lbl">${t("rules.kind.title")}</div><p class="sum muted">${parts.length ? parts.join(" · ") : t("rules.kind.none")}</p>`;
    }
    const box = (key: "location" | "page" | "owner", label: string) =>
      html`<label class="check"><input type="checkbox" .checked=${!!r[key]} ?disabled=${this.busy} @change=${(e: Event) => this.save({ [key]: (e.target as HTMLInputElement).checked })}> ${label}</label>`;
    return html`
      <div class="lbl">${t("rules.kind.title")}</div>
      <p class="muted" style="margin:0 0 6px">${t("rules.kind.hint")}</p>
      <div class="checks">${box("location", t("rules.kind.location"))}${box("page", t("rules.kind.page"))}${box("owner", t("rules.kind.owner"))}</div>
      ${k.attrs.length ? html`<div class="lbl" style="margin-top:8px">${t("rules.kind.fields")}</div>
        <div class="checks">${k.attrs.map((a) => html`<label class="check"><input type="checkbox" .checked=${!!r.fields?.includes(a.name)} ?disabled=${this.busy}
          @change=${(e: Event) => this.save({ fields: (e.target as HTMLInputElement).checked ? [...(r.fields ?? []), a.name] : (r.fields ?? []).filter((f) => f !== a.name) })}> ${attrLabel(a)}</label>`)}</div>` : ""}
      <label style="margin-top:8px;display:inline-flex;gap:8px;align-items:center">${t("rules.review_every")}
        <select ?disabled=${this.busy} @change=${(e: Event) => this.save({ reviewDays: Number((e.target as HTMLSelectElement).value) })}>
          ${INTERVALS.map((d) => html`<option value=${d} ?selected=${d === (r.reviewDays ?? 0)}>${d ? t("rules.days", { n: d }) : t("rules.no_review")}</option>`)}
          ${INTERVALS.includes(r.reviewDays ?? 0) ? "" : html`<option selected value=${r.reviewDays}>${t("rules.days", { n: r.reviewDays })}</option>`}
        </select></label>`;
  }
}
customElements.define("phx-kind-rules", PhxKindRules);
