// Project settings → Auto-links: own rules (pattern → address or ext ref).
import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { projectAutoLinks, type AutoLinkRule } from "../autolinks";
import { t } from "../i18n";
import { icon } from "../icon";
import { settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

export class PhxAutoLinkRules extends LitElement {
  static properties = { project: {}, owner: { type: Boolean }, rules: { state: true }, dirty: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .rule { display: grid; grid-template-columns: 1.2fr 110px 1.6fr 1fr auto; gap: 6px; align-items: center; padding: 0 18px 8px; }
    .rule input, .rule select { padding: 6px 8px; font-size: 13px; }
    .rule input.pat { font-family: var(--font-mono); }
    .help { color: var(--muted); font-size: 13px; padding: 0 18px 12px; margin: 0; }
    .help code { font-size: 12px; background: var(--surface2); padding: 1px 5px; border-radius: 4px; }
    .acts { display: flex; gap: 8px; padding: 4px 18px 16px; }
    @media (max-width: 760px) { .rule { grid-template-columns: 1fr 1fr; } }
  `];

  project = "";
  owner = false;
  rules: AutoLinkRule[] = [];
  dirty = false;

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("project") && this.project) void projectAutoLinks(this.project, true).then((al) => (this.rules = al.rules.map((r) => ({ ...r }))));
  }

  private set(i: number, p: Partial<AutoLinkRule>) {
    this.rules = this.rules.map((r, j) => (j === i ? { ...r, ...p } : r));
    this.dirty = true;
  }

  private async save() {
    try {
      const res = await api<{ rules: AutoLinkRule[] }>("PUT", `/projects/${this.project}/autolinks`, { rules: this.rules });
      this.rules = res.rules;
      this.dirty = false;
      void projectAutoLinks(this.project, true);
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    return html`<p class="help">${t("autolink.help")} <code>ADR-0012</code> <code>D-640</code> <code>§6.4</code></p>
      ${this.rules.map((r, i) => html`<div class="rule">
        <input class="pat" placeholder="JIRA-(\\d+)" .value=${r.pattern} aria-label=${t("autolink.pattern")} ?disabled=${!this.owner} @input=${(e: Event) => this.set(i, { pattern: (e.target as HTMLInputElement).value })}>
        <select aria-label=${t("autolink.to")} ?disabled=${!this.owner} @change=${(e: Event) => this.set(i, { to: (e.target as HTMLSelectElement).value as AutoLinkRule["to"] })}>
          <option value="url" ?selected=${r.to === "url"}>${t("autolink.to_url")}</option><option value="ref" ?selected=${r.to === "ref"}>${t("autolink.to_ref")}</option></select>
        ${r.to === "url" ? html`<input placeholder="https://jira.example.org/browse/$0" .value=${r.url ?? ""} aria-label=${t("autolink.url")} ?disabled=${!this.owner} @input=${(e: Event) => this.set(i, { url: (e.target as HTMLInputElement).value })}>` : html`<span class="muted" style="font-size:13px">${t("autolink.ref_hint")}</span>`}
        <input placeholder=${t("autolink.note")} .value=${r.note ?? ""} aria-label=${t("autolink.note")} ?disabled=${!this.owner} @input=${(e: Event) => this.set(i, { note: (e.target as HTMLInputElement).value })}>
        ${this.owner ? html`<button class="danger" aria-label=${t("doc.delete")} @click=${() => { this.rules = this.rules.filter((_, j) => j !== i); this.dirty = true; }}>${icon("Trash2", 13)}</button>` : html`<span></span>`}
      </div>`)}
      ${this.owner ? html`<div class="acts"><button class="ghost" @click=${() => { this.rules = [...this.rules, { pattern: "", to: "url", url: "" }]; this.dirty = true; }}>${icon("Plus", 14)} ${t("autolink.add")}</button>
        ${this.dirty ? html`<button @click=${this.save}>${icon("Save", 14)} ${t("doc.save")}</button>` : nothing}</div>` : nothing}`;
  }
}
customElements.get("phx-autolink-rules") || customElements.define("phx-autolink-rules", PhxAutoLinkRules);
