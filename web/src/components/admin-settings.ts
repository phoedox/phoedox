import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { locale, t } from "../i18n";
import { dollars } from "../ai-chat";
import { icon } from "../icon";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

interface AIChat { enabled: boolean; configured: boolean; model?: string; orgBudget: number; userBudget: number; instanceCap: number; usedCents: number }
interface Settings { aiChat: AIChat; aiEnabled: boolean; quotaProjectMb: number; vulnCheck: boolean; pluginBeta: boolean; signup: boolean; vault: { kind: string; url: string; connected: boolean }; tools?: Record<string, boolean> }

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

  /** Cents from a dollar input; empty means no limit (0). */
  private cents(f: FormData, name: string): number {
    const v = Number(String(f.get(name) ?? "").replace(",", "."));
    return Number.isFinite(v) && v > 0 ? Math.round(v * 100) : 0;
  }

  private aiChat(c: AIChat) {
    const val = (cents: number) => (cents ? (cents / 100).toFixed(2) : "");
    return html`<h2>${icon("BotMessageSquare", 16)} ${t("admin.ai_chat")}</h2>
      <p class="muted">${t("admin.ai_chat_hint")}</p>
      ${c.configured ? html`<p class="muted">${t("admin.ai_chat_model", { model: c.model ?? "" })}</p>`
        : html`<p class="muted">${icon("CircleAlert", 14)} ${t("admin.ai_chat_not_configured")}</p>`}
      <label class="switch"><input type="checkbox" .checked=${c.enabled} ?disabled=${!c.configured}
        @change=${(e: Event) => this.set({ aiChat: { enabled: (e.target as HTMLInputElement).checked } } as any)} /> ${t("admin.ai_chat_enabled")}</label>
      <form style="display:grid;gap:8px;max-width:520px;margin-top:12px" @submit=${(e: SubmitEvent) => {
        e.preventDefault();
        const f = new FormData(e.target as HTMLFormElement);
        void this.set({ aiChat: { orgBudget: this.cents(f, "org"), userBudget: this.cents(f, "user"), instanceCap: this.cents(f, "cap") } } as any);
      }}>
        <label class="field">${t("admin.ai_chat_org_budget")}<input name="org" type="number" min="0" step="0.01" .value=${val(c.orgBudget)} placeholder="∞" style="max-width:180px"></label>
        <label class="field">${t("admin.ai_chat_user_budget")}<input name="user" type="number" min="0" step="0.01" .value=${val(c.userBudget)} placeholder="∞" style="max-width:180px"></label>
        <label class="field">${t("admin.ai_chat_cap")}<input name="cap" type="number" min="0" step="0.01" .value=${val(c.instanceCap)} placeholder="∞" style="max-width:180px"></label>
        <div><button>${t("profile.save")}</button></div>
      </form>
      <p class="muted">${t("admin.ai_chat_used", { used: dollars(c.usedCents, locale()) })} ${t("admin.ai_chat_budget_hint")}</p>`;
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
        ${this.s.aiChat ? this.aiChat(this.s.aiChat) : ""}
        <label class="switch"><input type="checkbox" .checked=${this.s.vulnCheck} @change=${(e: Event) => this.set({ vulnCheck: (e.target as HTMLInputElement).checked })} /> ${t("admin.vuln_check")}</label>
        <p class="muted">${t("admin.vuln_check_hint")}</p>
        <label class="switch"><input type="checkbox" .checked=${this.s.pluginBeta} @change=${(e: Event) => this.set({ pluginBeta: (e.target as HTMLInputElement).checked })} /> ${t("admin.plugin_beta")}</label>
        <p class="muted">${t("admin.plugin_beta_hint")}</p>
        <label class="switch"><input type="checkbox" .checked=${this.s.signup} @change=${(e: Event) => this.set({ signup: (e.target as HTMLInputElement).checked })} /> ${t("admin.signup")}</label>
        <p class="muted">${t("admin.signup_hint")}</p>
        <h2>${icon("Lock", 16)} ${t("admin.vault")}</h2>
        <p class="muted">${t("admin.vault_hint")}</p>
        <form style="display:grid;gap:8px;max-width:520px" @submit=${(e: SubmitEvent) => {
          e.preventDefault();
          const f = new FormData(e.target as HTMLFormElement);
          const token = String(f.get("vtoken") ?? "");
          void this.set({ vaultKind: String(f.get("vkind") ?? ""), vaultUrl: String(f.get("vurl") ?? ""), ...(token ? { vaultToken: token } : {}) } as any);
        }}>
          <label class="field">${t("admin.vault_kind")}<select name="vkind">
            <option value="" ?selected=${!this.s.vault?.kind}>${t("admin.vault_none")}</option>
            <option value="hashicorp" ?selected=${this.s.vault?.kind === "hashicorp"}>HashiCorp Vault / OpenBao (KV)</option>
            <option value="onepassword" ?selected=${this.s.vault?.kind === "onepassword"}>1Password Connect</option></select></label>
          <label class="field">${t("admin.vault_url")}<input name="vurl" type="url" .value=${this.s.vault?.url ?? ""} placeholder="https://vault.example.org"></label>
          <label class="field">${t("admin.vault_token")}<input name="vtoken" type="password" autocomplete="off" placeholder=${this.s.vault?.connected ? t("notif.matrix_token_kept") : ""}></label>
          <div><button>${t("profile.save")}</button></div>
        </form>
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
