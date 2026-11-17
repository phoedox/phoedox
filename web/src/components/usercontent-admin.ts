// Administration → User content: the domain apps run on (one subdomain per
// app) and the DNS provider Phoedox uses for the wildcard certificate.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t, type Key } from "../i18n";
import { icon } from "../icon";
import { pageHead, scard, settingsUI, srow, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";
import "./app-policy";

interface Status { state: "off" | "waiting" | "ready" | "failed"; error?: string; expires?: string; since?: string }
interface View { domain: string; provider: string; email: string; staging: boolean; customerNumber?: string; hasSecret: boolean; status: Status; serving: boolean }

export class PhxUserContentAdmin extends LitElement {
  static properties = { v: { state: true }, provider: { state: true }, busy: { state: true }, staging: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    form { display: grid; gap: 12px; padding: 0 18px 16px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; }
    .st { display: flex; gap: 10px; align-items: flex-start; margin: 0 18px 16px; padding: 10px 12px; border-radius: 10px; background: var(--surface2); font-size: 14px; }
    .st.ready { background: color-mix(in srgb, var(--good) 12%, var(--surface)); }
    .st.failed { background: color-mix(in srgb, var(--bad) 10%, var(--surface)); }
    .st code { font-size: 12.5px; word-break: break-all; }
    ol { margin: 0; padding-left: 20px; font-size: 14px; display: grid; gap: 4px; }
  `];

  v?: View;
  provider = "netcup";
  staging = false;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.v = await api<View>("GET", "/admin/user-content").catch(() => undefined);
    if (this.v?.provider) this.provider = this.v.provider;
    this.staging = !!this.v?.staging;
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const s = (k: string) => String(f.get(k) ?? "").trim();
    this.busy = true;
    try {
      this.v = await api<View>("PUT", "/admin/user-content", {
        domain: s("domain"), provider: this.provider, email: s("email"), staging: this.staging,
        token: s("token"), customerNumber: s("customerNumber"), apiKey: s("apiKey"), apiPassword: s("apiPassword"),
      });
      toast(t("doc.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async off() {
    if (!confirm(t("uc.off_confirm"))) return;
    try {
      this.v = await api<View>("PUT", "/admin/user-content", { domain: "", provider: "", email: "" });
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private status() {
    const st = this.v?.status;
    if (!this.v?.serving) return html`<p class="st failed">${icon("TriangleAlert", 16)}<span>${t("uc.not_serving")}</span></p>`;
    if (!st || st.state === "off") return html`<p class="st">${icon("Info", 16)}<span>${t("uc.state_off")}</span></p>`;
    return html`<p class="st ${st.state}">${icon(st.state === "ready" ? "CircleCheck" : st.state === "failed" ? "CircleAlert" : "Hourglass", 16)}
      <span>${t(`uc.state_${st.state}` as Key, { date: st.expires ? formatDate(st.expires) : "" })}${st.error ? html`<br><code>${st.error}</code>` : nothing}</span></p>`;
  }

  render() {
    const v = this.v;
    if (!v) return nothing;
    const keep = v.hasSecret && v.provider === this.provider ? t("uc.keep_secret") : "";
    return html`${pageHead(t("uc.title"), t("uc.text"))}
      ${scard(t("uc.state"), "", this.status())}
      ${scard(t("uc.setup"), t("uc.setup_text"), html`<form @submit=${this.save}>
        <ol><li>${t("uc.step_dns")}</li><li>${t("uc.step_api")}</li><li>${t("uc.step_save")}</li></ol>
        <div class="grid">
          <label class="field">${t("uc.domain")}<input name="domain" required placeholder="phoedoxusercontent.com" .value=${v.domain}></label>
          <label class="field">${t("uc.email")}<input name="email" type="email" required .value=${v.email}></label>
          <label class="field">${t("uc.provider")}<select @change=${(e: Event) => (this.provider = (e.target as HTMLSelectElement).value)}>
            ${[["netcup", "netcup"], ["hetzner", "Hetzner DNS"], ["cloudflare", "Cloudflare"]].map(([id, name]) => html`<option value=${id} ?selected=${this.provider === id}>${name}</option>`)}</select></label>
        </div>
        ${this.provider === "netcup" ? html`<div class="grid">
            <label class="field">${t("uc.customer")}<input name="customerNumber" .value=${v.customerNumber ?? ""} required></label>
            <label class="field">${t("uc.api_key")}<input name="apiKey" type="password" autocomplete="off" placeholder=${keep} ?required=${!keep}></label>
            <label class="field">${t("uc.api_password")}<input name="apiPassword" type="password" autocomplete="off" placeholder=${keep} ?required=${!keep}></label></div>`
          : html`<label class="field">${t("uc.token")}<input name="token" type="password" autocomplete="off" placeholder=${keep} ?required=${!keep}></label>`}
        ${srow(t("uc.staging"), t("uc.staging_help"), sswitch(this.staging, t("uc.staging"), (on) => (this.staging = on)))}
        <div class="row"><button ?disabled=${this.busy}>${icon("Save", 14)} ${t("uc.save")}</button>
          ${v.domain ? html`<button type="button" class="danger" @click=${this.off}>${t("uc.switch_off")}</button>` : nothing}</div>
      </form>`)}`;
  }
}
customElements.get("phx-usercontent-admin") || customElements.define("phx-usercontent-admin", PhxUserContentAdmin);
