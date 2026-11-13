import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

interface Settings { aiEnabled: boolean; quotaProjectMb: number; tools?: Record<string, boolean> }

/** Instance settings for system admins. */
export class PhxAdminSettings extends SignalWatcher(LitElement) {
  static properties = { s: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    label.switch { display: flex; gap: 10px; align-items: center; font-weight: 600; font-size: 15px; }
    label.switch input { width: auto; }
    p { max-width: 620px; line-height: 1.5; }
    h2 { font-size: 18px; margin: 28px 0 10px; }
    ul.tools { list-style: none; padding: 0; display: grid; gap: 4px; }
  `];
  s?: Settings;

  async connectedCallback() {
    super.connectedCallback();
    this.s = await api<Settings>("GET", "/admin/settings").catch(() => undefined);
  }

  private async set(patch: Partial<Settings>) {
    try {
      this.s = await api<Settings>("PUT", "/admin/settings", patch);
      toast(t("admin.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    return html`<div class="enter">
      <h1>${t("admin.title")}</h1>
      ${settingsNav("instance")}
      ${this.s ? html`<label class="switch"><input type="checkbox" .checked=${this.s.aiEnabled} @change=${(e: Event) => this.set({ aiEnabled: (e.target as HTMLInputElement).checked })} /> ${t("admin.ai")}</label>
        <p class="muted">${t("admin.ai_hint")}</p>
        <h2>${t("admin.files")}</h2>
        <form @submit=${(e: SubmitEvent) => { e.preventDefault(); this.set({ quotaProjectMb: Number(new FormData(e.target as HTMLFormElement).get("quota") || 0) }); }}>
          <label class="field">${t("admin.quota")}
            <span class="row"><input name="quota" type="number" min="0" .value=${String(this.s.quotaProjectMb || "")} placeholder="∞" style="width:160px"> MB
            <button class="ghost" type="submit">${t("doc.save")}</button></span></label>
        </form>
        <p class="muted">${t("admin.quota_hint")}</p>
        <ul class="tools">${(["pdf", "ocr", "virus_scan"] as const).map((k) => html`<li>${icon(this.s!.tools?.[k] ? "Check" : "X", 14)} ${t(`admin.tool.${k}`)}</li>`)}</ul>
        <p class="muted">${t("admin.tools_hint")}</p>` : html`<p class="muted">${t("app.loading")}</p>`}
    </div>`;
  }
}
customElements.define("phx-admin-settings", PhxAdminSettings);
