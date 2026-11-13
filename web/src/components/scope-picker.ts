import { LitElement, css, html } from "lit";
import { api, type Project } from "../api";
import { t } from "../i18n";
import { base } from "./shared";

/** Access level and project choice for tokens and connected apps. */
export class PhxScopePicker extends LitElement {
  static properties = { access: { state: true }, some: { state: true }, projects: { state: true }, chosen: { state: true } };
  static styles = [base, css`
    fieldset { border: 0; padding: 0; margin: 0 0 10px; display: flex; flex-direction: column; gap: 6px; }
    legend { font-size: 13px; color: var(--muted); margin-bottom: 4px; padding: 0; }
    label { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    input[type=radio], input[type=checkbox] { width: auto; }
    .list { display: flex; flex-direction: column; gap: 4px; margin: 2px 0 0 24px; max-height: 200px; overflow-y: auto; }
  `];
  access: "read" | "write" = "write";
  some = false;
  projects: Project[] = [];
  chosen = new Set<string>();

  async connectedCallback() {
    super.connectedCallback();
    this.projects = (await api<Project[]>("GET", "/projects").catch(() => [])) ?? [];
  }

  /** The current choice, or null if "selected projects" has none chosen. */
  get value(): { access: string; projects: string[] } | null {
    if (this.some && this.chosen.size === 0) return null;
    return { access: this.access, projects: this.some ? [...this.chosen] : [] };
  }

  private toggle(id: string, on: boolean) {
    const next = new Set(this.chosen);
    if (on) next.add(id);
    else next.delete(id);
    this.chosen = next;
  }

  render() {
    return html`
      <fieldset><legend>${t("scope.access")}</legend>
        <label><input type="radio" name="access" .checked=${this.access === "read"} @change=${() => (this.access = "read")} /> ${t("scope.read")}</label>
        <label><input type="radio" name="access" .checked=${this.access === "write"} @change=${() => (this.access = "write")} /> ${t("scope.write")}</label>
      </fieldset>
      <fieldset><legend>${t("scope.projects")}</legend>
        <label><input type="radio" name="some" .checked=${!this.some} @change=${() => (this.some = false)} /> ${t("scope.all")}</label>
        <label><input type="radio" name="some" .checked=${this.some} @change=${() => (this.some = true)} /> ${t("scope.some")}</label>
        ${this.some ? html`<div class="list">${this.projects.map((p) => html`<label><input type="checkbox" .checked=${this.chosen.has(p.id)}
          @change=${(e: Event) => this.toggle(p.id, (e.target as HTMLInputElement).checked)} /> ${p.name}</label>`)}</div>` : ""}
      </fieldset>`;
  }
}
customElements.define("phx-scope-picker", PhxScopePicker);
