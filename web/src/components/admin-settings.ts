import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { locale, t } from "../i18n";
import { dollars } from "../ai-chat";
import { icon } from "../icon";
import { pageHead, scard, settingsUI, srow, sswitch } from "./settings-ui";
import { base } from "./shared";
import "./ai-providers";
import type { AIProvider } from "./ai-providers";
import { toast } from "./toasts";

interface AIChat { enabled: boolean; configured: boolean; defaultModel: string; orgBudget: number; userBudget: number; instanceCap: number; usedCents: number; chargedCents?: number; pricesHiddenPersonal?: boolean }
interface Settings { aiChat: AIChat; aiEnabled: boolean; quotaProjectMb: number; vulnCheck: boolean; pluginBeta: boolean; signup: boolean; vault: { kind: string; url: string; connected: boolean }; tools?: Record<string, boolean> }

/** Instance settings for system admins, one section per page of the
 * administration (ADR-0070): sign-up, ai, storage, security, plugins. */
export class PhxAdminSettings extends SignalWatcher(LitElement) {
  static properties = { s: { state: true }, providers: { state: true }, section: {} };
  providers: AIProvider[] = [];
  section: "signup" | "ai" | "storage" | "security" | "plugins" = "ai";
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .scard .body p { margin: 0 0 10px; }
    p { max-width: 620px; line-height: 1.5; }
    h2 { font-size: 18px; margin: 28px 0 10px; }
    h3 { font-size: 15px; margin: 18px 0 6px; }
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
    return html`${scard(t("admin.ai_chat"), t("admin.ai_chat_hint"), html`
      ${srow(t("admin.ai_chat_enabled"), c.configured ? "" : t("admin.ai_chat_not_configured"), sswitch(c.enabled, t("admin.ai_chat_enabled"), (on) => this.set({ aiChat: { enabled: on } } as any), !c.configured))}`)}
      <section class="scard"><header><h2>${t("ai_providers.title")}</h2></header><div class="body">
      <p class="muted">${t("ai_providers.intro_instance")}</p>
      <phx-ai-providers @changed=${(e: CustomEvent<AIProvider[]>) => { this.providers = e.detail; void this.reloadSettings(); }}></phx-ai-providers>
      ${c.configured ? html`<label class="field" style="max-width:420px;margin-top:12px">${t("admin.ai_chat_default_model")}
          <select @change=${(e: Event) => this.set({ aiChat: { defaultModel: (e.target as HTMLSelectElement).value } } as any)}>
            <option value="" ?selected=${!c.defaultModel}>${t("admin.ai_chat_first_model")}</option>
            ${this.providers.filter((p) => p.enabled).flatMap((p) => p.models.map((m) => html`<option value=${`${p.id}/${m.id}`} ?selected=${c.defaultModel === `${p.id}/${m.id}`}>${p.name}: ${m.label || m.id}</option>`))}
          </select></label>`
        : html`<p class="muted">${icon("CircleAlert", 14)} ${t("admin.ai_chat_not_configured")}</p>`}
      </div></section>
      <section class="scard"><header><h2>${t("sx.admin.budgets")}</h2><p>${t("admin.ai_chat_budget_hint")}</p></header><div class="body">
      <form style="display:grid;gap:8px;max-width:520px" @submit=${(e: SubmitEvent) => {
        e.preventDefault();
        const f = new FormData(e.target as HTMLFormElement);
        void this.set({ aiChat: { orgBudget: this.cents(f, "org"), userBudget: this.cents(f, "user"), instanceCap: this.cents(f, "cap") } } as any);
      }}>
        <label class="field">${t("admin.ai_chat_org_budget")}<input name="org" type="number" min="0" step="0.01" .value=${val(c.orgBudget)} placeholder="∞" style="max-width:180px"></label>
        <label class="field">${t("admin.ai_chat_user_budget")}<input name="user" type="number" min="0" step="0.01" .value=${val(c.userBudget)} placeholder="∞" style="max-width:180px"></label>
        <label class="field">${t("admin.ai_chat_cap")}<input name="cap" type="number" min="0" step="0.01" .value=${val(c.instanceCap)} placeholder="∞" style="max-width:180px"></label>
        <div><button>${t("profile.save")}</button></div>
      </form>
      <p class="muted" style="margin-top:10px">${t("admin.ai_chat_used_cost", { cost: dollars(c.usedCents, locale()), charged: dollars(c.chargedCents ?? 0, locale()) })}</p></div></section>
      ${scard(t("admin.prices.title"), t("admin.prices.hint"), html`
        ${srow(t("admin.prices.hide_personal"), t("admin.prices.hide_personal_hint"), sswitch(!!c.pricesHiddenPersonal, t("admin.prices.hide_personal"), (on) => this.set({ aiChat: { pricesHiddenPersonal: on } } as any)))}`)}`;
  }

  private async reloadSettings() {
    this.s = (await api<Settings>("GET", "/admin/settings").catch(() => undefined)) ?? this.s;
  }

  private async set(patch: Partial<Settings>) {
    try {
      this.s = await api<Settings>("PUT", "/admin/settings", patch);
      toast(t("admin.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private vault() {
    const v = this.s!.vault;
    return scard(t("admin.vault"), t("admin.vault_hint"), html`<div class="body"><form style="display:grid;gap:8px;max-width:520px" @submit=${(e: SubmitEvent) => {
      e.preventDefault();
      const f = new FormData(e.target as HTMLFormElement);
      const token = String(f.get("vtoken") ?? "");
      void this.set({ vaultKind: String(f.get("vkind") ?? ""), vaultUrl: String(f.get("vurl") ?? ""), ...(token ? { vaultToken: token } : {}) } as any);
    }}>
      <label class="field">${t("admin.vault_kind")}<select name="vkind">
        <option value="" ?selected=${!v?.kind}>${t("admin.vault_none")}</option>
        <option value="hashicorp" ?selected=${v?.kind === "hashicorp"}>HashiCorp Vault / OpenBao (KV)</option>
        <option value="onepassword" ?selected=${v?.kind === "onepassword"}>1Password Connect</option></select></label>
      <label class="field">${t("admin.vault_url")}<input name="vurl" type="url" .value=${v?.url ?? ""} placeholder="https://vault.example.org"></label>
      <label class="field">${t("admin.vault_token")}<input name="vtoken" type="password" autocomplete="off" placeholder=${v?.connected ? t("notif.matrix_token_kept") : ""}></label>
      <div><button>${t("profile.save")}</button></div>
    </form></div>`);
  }

  private body() {
    const s = this.s!;
    switch (this.section) {
      case "signup":
        return html`${pageHead(t("sx.admin.signup"), t("sx.admin.signup_text"))}
          ${scard(t("sx.admin.signup_card"), "", srow(t("admin.signup"), t("admin.signup_hint"), sswitch(s.signup, t("admin.signup"), (on) => this.set({ signup: on }))))}
          ${scard(t("sx.admin.mail"), t("sx.admin.mail_text"), html`<div class="body"><code class="mono">phx admin test-mail --email you@example.org</code></div>`)}`;
      case "ai":
        return html`${pageHead(t("sx.admin.ai"), t("sx.admin.ai_text"))}
          ${scard(t("sx.admin.ai_access"), "", srow(t("admin.ai"), t("admin.ai_hint"), sswitch(s.aiEnabled, t("admin.ai"), (on) => this.set({ aiEnabled: on }))))}
          ${s.aiChat ? this.aiChat(s.aiChat) : ""}`;
      case "storage":
        return html`${pageHead(t("sx.admin.storage"), "")}
          ${scard(t("admin.files"), "", html`${srow(t("admin.quota"), t("admin.quota_hint"), html`<form class="row" style="flex-wrap:nowrap" @submit=${(e: SubmitEvent) => { e.preventDefault(); this.set({ quotaProjectMb: Number(new FormData(e.target as HTMLFormElement).get("quota") || 0) }); }}>
              <input name="quota" type="number" min="0" .value=${String(s.quotaProjectMb || "")} placeholder="∞" style="width:120px" aria-label=${t("admin.quota")}> MB <button class="ghost" type="submit">${t("doc.save")}</button></form>`)}
            ${srow(t("sx.admin.tools"), t("admin.tools_hint"), html`<span class="row">${(["pdf", "ocr", "virus_scan"] as const).map((k) => html`<span class="chip">${icon(s.tools?.[k] ? "Check" : "X", 13)} ${t(`admin.tool.${k}`)}</span>`)}</span>`)}`)}`;
      case "security":
        return html`${pageHead(t("sx.admin.security"), "")}
          ${scard(t("sx.admin.checks"), "", srow(t("admin.vuln_check"), t("admin.vuln_check_hint"), sswitch(s.vulnCheck, t("admin.vuln_check"), (on) => this.set({ vulnCheck: on }))))}
          ${this.vault()}`;
      case "plugins":
        return scard(t("sx.admin.plugin_updates"), "", srow(t("admin.plugin_beta"), t("admin.plugin_beta_hint"), sswitch(s.pluginBeta, t("admin.plugin_beta"), (on) => this.set({ pluginBeta: on }))));
    }
  }

  render() {
    return html`<div class="enter">${this.s ? this.body() : html`<p class="muted">${t("app.loading")}</p>`}</div>`;
  }
}
customElements.define("phx-admin-settings", PhxAdminSettings);
