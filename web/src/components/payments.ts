// Payments (ADR-0079): the administration page (provider dropdown – Stripe,
// Stripe Managed Payments or Paddle – with the provider's own fields, tax,
// webhook, check, test checkout, purchases) and the purchases page of a
// person (with the provider's customer portal).

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { pageHead, scard, settingsUI, srow, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Settings {
  provider: "" | "stripe" | "paddle"; mode: "classic" | "managed"; hasSecretKey: boolean; keyHint?: string; live: boolean; hasWebhookSecret: boolean;
  ready: boolean; taxCode: string; taxCategory: string; taxCategories: string[]; automaticTax: boolean; currency: string; webhookUrl: string; webhookEvents: string[];
  clientToken?: string; payUrl?: string;
}
interface Check { id: string; country: string; live: boolean; charges: boolean; managedEligible: boolean; warnings: string[] }
interface Purchase {
  id: string; userName?: string; kind: string; description?: string; amount: number; currency: string; recurring?: string; status: string;
  provider: string; mode: string; paymentRef?: string; createdAt: string;
}

type Choice = "off" | "stripe" | "managed" | "paddle";

const CHOICES: [Choice, string][] = [["off", "pay.off"], ["stripe", "pay.stripe"], ["managed", "pay.managed"], ["paddle", "pay.paddle"]];

/** Tax codes of Stripe that fit Phoedox's own products. */
const TAX_CODES: [string, string][] = [
  ["txcd_10103001", "pay.tax.saas_business"],
  ["txcd_10103000", "pay.tax.saas_personal"],
  ["txcd_10105001", "pay.tax.aiaas"],
];

export function money(amount: number, currency: string) {
  try {
    return new Intl.NumberFormat(document.documentElement.lang || undefined, { style: "currency", currency: currency.toUpperCase() }).format(amount / 100);
  } catch {
    return `${(amount / 100).toFixed(2)} ${currency.toUpperCase()}`;
  }
}

export class PhxPaymentsAdmin extends LitElement {
  static properties = { s: { state: true }, check: { state: true }, purchases: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .body { padding: 0 18px 16px; display: grid; gap: 10px; }
    .hint { color: var(--muted); font-size: 13px; margin: 0; }
    .note { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; padding: 12px; border-radius: 10px; background: var(--surface2); font-size: 13.5px; }
    .note.warn { background: color-mix(in srgb, var(--signal) 12%, var(--surface)); }
    .note ul { margin: 4px 0 0; padding-left: 18px; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    input.key { font-family: var(--font-mono); width: min(380px, 100%); }
    code.copy { background: var(--surface2); padding: 4px 8px; border-radius: 6px; font-size: 12.5px; word-break: break-all; }
    table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
    th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid var(--line); }
    th { color: var(--muted); font-weight: 600; font-size: 12.5px; }
    .st { font-weight: 600; } .st.paid { color: var(--good); } .st.failed, .st.refunded { color: var(--bad); }
    .tbl { overflow-x: auto; padding: 0 18px 16px; }
  `];
  s?: Settings;
  check?: Check;
  purchases: Purchase[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.s = await api<Settings>("GET", "/admin/payments").catch(() => undefined);
    this.purchases = (await api<Purchase[]>("GET", "/admin/payments/purchases").catch(() => [])) ?? [];
  }

  private choice(): Choice {
    if (!this.s?.provider) return "off";
    if (this.s.provider === "paddle") return "paddle";
    return this.s.mode === "managed" ? "managed" : "stripe";
  }

  private async save(patch: Partial<Settings> & { secretKey?: string; webhookSecret?: string }, msg = t("doc.saved")) {
    const s = this.s!;
    this.busy = true;
    try {
      const body = { provider: s.provider, mode: s.mode, taxCode: s.taxCode, taxCategory: s.taxCategory, automaticTax: s.automaticTax, currency: s.currency, ...patch };
      this.s = await api<Settings>("PUT", "/admin/payments", body);
      this.check = undefined;
      toast(msg);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  /** Keys are kept per provider, so switching back and forth loses nothing. */
  private setChoice(c: Choice) {
    if (c === "off") return this.save({ provider: "" });
    if (c === "paddle") return this.save({ provider: "paddle", mode: "classic" });
    return this.save({ provider: "stripe", mode: c === "managed" ? "managed" : "classic" });
  }

  private async runCheck() {
    this.busy = true;
    try {
      this.check = await api<Check>("POST", "/admin/payments/check");
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async testCheckout() {
    try {
      const r = await api<{ url: string }>("POST", "/admin/payments/test");
      window.open(r.url, "_blank", "noopener");
      void this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async refund(p: Purchase) {
    if (!confirm(t("pay.refund_confirm", { amount: money(p.amount, p.currency) }))) return;
    try {
      await api("POST", `/admin/payments/purchases/${p.id}/refund`);
      toast(t("pay.refund_sent"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private keyField(name: "secretKey" | "webhookSecret", has: boolean, placeholder: string, label: string) {
    return html`<div class="row"><input class="key" type="password" autocomplete="off" placeholder=${has ? t("pay.key_set") : placeholder} aria-label=${label}
        @change=${(e: Event) => { const v = (e.target as HTMLInputElement).value.trim(); if (v) { (e.target as HTMLInputElement).value = ""; void this.save({ [name]: v } as any); } }}>
      ${has ? html`<button class="ghost" @click=${() => void this.save({ [name]: "" } as any, t("pay.key_removed"))}>${t("pay.remove")}</button>` : nothing}</div>`;
  }

  private providerNote(c: Choice) {
    switch (c) {
      case "managed":
        return html`<div class="note">${icon("ShieldCheck", 16)}<div><b>${t("pay.managed_title")}</b><ul>
          <li>${t("pay.managed_1")}</li><li>${t("pay.managed_2")}</li><li>${t("pay.managed_4")}</li></ul></div></div>`;
      case "paddle":
        return html`<div class="note">${icon("ShieldCheck", 16)}<div><b>${t("pay.paddle_title")}</b><ul>
          <li>${t("pay.paddle_1")}</li><li>${t("pay.paddle_2")}</li></ul></div></div>`;
      case "stripe":
        return html`<div class="note">${icon("Info", 16)}<div>${t("pay.classic_note")}</div></div>`;
      default:
        return html`<p class="hint">${t("pay.off_note")}</p>`;
    }
  }

  private stripeFields(s: Settings, c: Choice) {
    return html`
      ${srow(t("pay.secretKey"), s.keyHint ? t("pay.key_hint", { hint: s.keyHint }) : t("pay.secretKey_help"), this.keyField("secretKey", s.hasSecretKey, "sk_live_… / rk_live_…", t("pay.secretKey")))}
      ${srow(t("pay.webhookSecret"), t("pay.webhookSecret_help"), this.keyField("webhookSecret", s.hasWebhookSecret, "whsec_…", t("pay.webhookSecret")))}
      ${srow(t("pay.tax_code"), t(c === "managed" ? "pay.tax_code_help_managed" : "pay.tax_code_help"), html`<select aria-label=${t("pay.tax_code")} @change=${(e: Event) => void this.save({ taxCode: (e.target as HTMLSelectElement).value })}>
        ${TAX_CODES.map(([code, label]) => html`<option value=${code} ?selected=${code === s.taxCode}>${t(label as any)} (${code})</option>`)}
        ${TAX_CODES.some(([code]) => code === s.taxCode) ? nothing : html`<option value=${s.taxCode} selected>${s.taxCode}</option>`}</select>`)}
      ${c === "stripe" ? srow(t("pay.auto_tax"), t("pay.auto_tax_help"), sswitch(s.automaticTax, t("pay.auto_tax"), (on) => void this.save({ automaticTax: on }), this.busy)) : nothing}`;
  }

  private paddleFields(s: Settings) {
    return html`
      ${srow(t("pay.paddle.apiKey"), s.keyHint ? t("pay.key_hint", { hint: s.keyHint }) : t("pay.paddle.apiKey_help"), this.keyField("secretKey", s.hasSecretKey, "pdl_live_apikey_…", t("pay.paddle.apiKey")))}
      ${srow(t("pay.webhookSecret"), t("pay.paddle.webhookSecret_help"), this.keyField("webhookSecret", s.hasWebhookSecret, "pdl_ntfset_…", t("pay.webhookSecret")))}
      ${srow(t("pay.paddle.clientToken"), t("pay.paddle.clientToken_help"), html`<input class="key" autocomplete="off" placeholder="live_… / test_…" .value=${s.clientToken ?? ""} aria-label=${t("pay.paddle.clientToken")}
        @change=${(e: Event) => void this.save({ clientToken: (e.target as HTMLInputElement).value.trim() })}>`)}
      ${srow(t("pay.paddle.payUrl"), t("pay.paddle.payUrl_help"), html`<code class="copy">${s.payUrl}</code>`)}
      ${srow(t("pay.paddle.taxCategory"), t("pay.paddle.taxCategory_help"), html`<select aria-label=${t("pay.paddle.taxCategory")} @change=${(e: Event) => void this.save({ taxCategory: (e.target as HTMLSelectElement).value })}>
        ${s.taxCategories.map((cat) => html`<option value=${cat} ?selected=${cat === s.taxCategory}>${t(`pay.taxcat.${cat}` as any)}</option>`)}</select>`)}`;
  }

  render() {
    const s = this.s;
    if (!s) return nothing;
    const c = this.choice();
    const ck = this.check;
    return html`${pageHead(t("pay.title"), t("pay.intro"))}
      ${scard(t("pay.provider"), t("pay.provider_help"), html`
        ${srow(t("pay.choose"), undefined, html`<select aria-label=${t("pay.choose")} ?disabled=${this.busy} @change=${(e: Event) => void this.setChoice((e.target as HTMLSelectElement).value as Choice)}>
          ${CHOICES.map(([v, label]) => html`<option value=${v} ?selected=${v === c}>${t(label as any)}</option>`)}</select>`)}
        <div class="body">${this.providerNote(c)}</div>
        ${c === "off" ? nothing : html`
          ${c === "paddle" ? this.paddleFields(s) : this.stripeFields(s, c)}
          ${srow(t("pay.currency"), t("pay.currency_help"), html`<input style="width:90px;max-width:90px" .value=${s.currency} maxlength="3" aria-label=${t("pay.currency")} @change=${(e: Event) => void this.save({ currency: (e.target as HTMLInputElement).value })}>`)}
          <div class="body">
            <b>${t("pay.webhook")}</b>
            <p class="hint">${t(c === "paddle" ? "pay.paddle.webhook_help" : "pay.webhook_help")}</p>
            <code class="copy">${s.webhookUrl}</code>
            <p class="hint">${t("pay.webhook_events")} ${s.webhookEvents.map((e) => html`<code>${e}</code> `)}</p>
            <div class="row">
              <button class="ghost" ?disabled=${this.busy || !s.hasSecretKey} @click=${() => void this.runCheck()}>${icon("ShieldCheck", 14)} ${t("pay.check")}</button>
              <button class="ghost" ?disabled=${!s.ready} @click=${() => void this.testCheckout()}>${icon("CreditCard", 14)} ${t("pay.test")}</button>
            </div>
            ${ck ? html`<div class="note ${ck.warnings.length ? "warn" : ""}" role="status">${icon(ck.warnings.length ? "TriangleAlert" : "CircleCheck", 16)}<div>
              ${c === "paddle" ? t("pay.check_ok_paddle", { mode: t(ck.live ? "pay.live" : "pay.testmode") }) : t("pay.check_ok", { id: ck.id, country: ck.country, mode: t(ck.live ? "pay.live" : "pay.testmode") })}
              ${ck.warnings.length ? html`<ul>${ck.warnings.map((w) => html`<li>${t(`pay.warn.${w}` as any)}</li>`)}</ul>` : nothing}</div></div>` : nothing}
          </div>`}
      `)}
      ${scard(t("pay.purchases"), t("pay.purchases_help"), this.purchases.length ? html`<div class="tbl"><table>
        <thead><tr><th>${t("pay.col.date")}</th><th>${t("pay.col.who")}</th><th>${t("pay.col.what")}</th><th>${t("pay.col.amount")}</th><th>${t("pay.col.status")}</th><th>${t("pay.col.mode")}</th><th></th></tr></thead>
        <tbody>${this.purchases.map((p) => html`<tr><td>${formatDate(p.createdAt)}</td><td>${p.userName ?? ""}</td><td>${p.description || p.kind}</td>
          <td>${money(p.amount, p.currency)}${p.recurring ? html` / ${t(`pay.per.${p.recurring}` as any)}` : nothing}</td>
          <td><span class="st ${p.status}">${t(`pay.status.${p.status}` as any)}</span></td><td>${p.mode === "managed" ? (p.provider === "paddle" ? "Paddle" : "Stripe") : t("pay.classic_short")}</td>
          <td>${p.status === "paid" && p.paymentRef && !p.recurring ? html`<button class="ghost" @click=${() => void this.refund(p)}>${t("pay.refund")}</button>` : nothing}</td></tr>`)}</tbody></table></div>`
        : html`<div class="body"><p class="hint">${t("pay.none")}</p></div>`)}`;
  }
}
customElements.get("phx-payments-admin") || customElements.define("phx-payments-admin", PhxPaymentsAdmin);

/** A person's purchases, with the provider's portal for payment methods, invoices and subscriptions. */
export class PhxMyPurchases extends LitElement {
  static properties = { list: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    ul { list-style: none; margin: 0; padding: 0 18px 16px; display: grid; gap: 6px; }
    li { display: grid; grid-template-columns: 1fr auto auto; gap: 12px; align-items: center; padding: 8px 0; border-bottom: 1px solid var(--line); font-size: 14px; }
    .muted { color: var(--muted); font-size: 12.5px; }
    .body { padding: 0 18px 16px; }
  `];
  list?: Purchase[];

  connectedCallback() {
    super.connectedCallback();
    void api<Purchase[]>("GET", "/me/purchases").then((l) => (this.list = l ?? [])).catch(() => (this.list = []));
  }

  private async portal() {
    try {
      const r = await api<{ url: string }>("POST", "/me/payments/portal", { returnPath: "/settings/billing" });
      location.href = r.url;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const l = this.list;
    if (!l) return nothing;
    const result = new URLSearchParams(location.search).get("result");
    return html`${pageHead(t("pay.mine"), t("pay.mine_intro"))}
      ${result === "ok" ? html`<p role="status">${icon("CircleCheck", 14)} ${t("pay.thanks")}</p>` : nothing}
      ${scard(t("pay.purchases"), undefined, l.length ? html`<ul>${l.map((p) => html`<li><div>${p.description || p.kind}<div class="muted">${formatDate(p.createdAt)}</div></div>
          <span>${money(p.amount, p.currency)}${p.recurring ? html` / ${t(`pay.per.${p.recurring}` as any)}` : nothing}</span><span class="muted">${t(`pay.status.${p.status}` as any)}</span></li>`)}</ul>
          <div class="body"><button class="ghost" @click=${() => void this.portal()}>${icon("CreditCard", 14)} ${t("pay.portal")}</button></div>`
        : html`<div class="body"><p class="muted">${t("pay.none_mine")}</p></div>`)}`;
  }
}
customElements.get("phx-my-purchases") || customElements.define("phx-my-purchases", PhxMyPurchases);
