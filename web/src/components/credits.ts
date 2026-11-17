// AI credits (ADR-0080): the balance of a person or an organization with
// its lots (each with its own expiry), offers to buy, and the system
// admin's page for offers and grants.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, formatDay, t } from "../i18n";
import { icon } from "../icon";
import { money } from "./payments";
import { pageHead, scard, settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Lot { id: string; ownerKind: string; ownerName?: string; amount: number; remaining: number; expiresAt?: string; source: string; note?: string; createdAt: string }
interface Offer { id: string; name: string; price: number; credit: number; validMonths: number; for: "user" | "org" | "both" }
interface CreditInfo { balance: number; lots: Lot[]; history: Lot[]; offers: Offer[]; currency: string }
interface CreditAdmin { offers: Offer[]; grants: Lot[]; paymentsReady: boolean; currency: string }

/** In-app amounts are US cents (like the AI budgets); lots keep micro-dollars. */
const usd = (cents: number) => new Intl.NumberFormat(document.documentElement.lang || "en", { style: "currency", currency: "USD" }).format(cents / 100);
const microsToCents = (m: number) => Math.round(m / 10_000);

const shared = css`
  :host { display: block; }
  .body { padding: 0 18px 16px; display: grid; gap: 10px; }
  .bal { font-size: 26px; font-weight: 700; }
  .muted { color: var(--muted); font-size: 13px; }
  table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
  th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid var(--line); vertical-align: middle; }
  th { color: var(--muted); font-weight: 600; font-size: 12.5px; }
  .offers { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
  .offer { border: 1px solid var(--line); border-radius: 12px; padding: 12px; display: grid; gap: 6px; background: var(--surface); }
  .offer b { font-size: 15px; }
  .wrap { overflow-x: auto; }
`;

/** The credit of the signed-in person (org = "") or of an organization. */
export class PhxAiCredits extends LitElement {
  static properties = { org: {}, info: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, shared];
  org = "";
  info?: CreditInfo;
  busy = false;

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("org") || !this.info) void this.load();
  }

  private path() {
    return this.org ? `/orgs/${encodeURIComponent(this.org)}/ai-credits` : "/me/ai-credits";
  }

  private async load() {
    this.info = await api<CreditInfo>("GET", this.path()).catch(() => undefined);
  }

  private async buy(o: Offer) {
    this.busy = true;
    try {
      const r = await api<{ url: string }>("POST", `${this.path()}/buy`, { offer: o.id });
      location.href = r.url;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      this.busy = false;
    }
  }

  private expiry(l: Lot) {
    return l.expiresAt ? formatDay(l.expiresAt) : t("credits.never");
  }

  render() {
    const i = this.info;
    if (!i) return nothing;
    return scard(t("credits.title"), t(this.org ? "credits.intro_org" : "credits.intro"), html`<div class="body">
      <div><span class="bal">${usd(i.balance)}</span> <span class="muted">${t("credits.balance")}</span></div>
      ${i.lots.length ? html`<div class="wrap"><table><tr><th>${t("credits.col.left")}</th><th>${t("credits.col.source")}</th><th>${t("credits.col.expires")}</th></tr>
        ${i.lots.map((l) => html`<tr><td>${usd(microsToCents(l.remaining))} <span class="muted">/ ${usd(microsToCents(l.amount))}</span></td>
          <td>${t(l.source === "grant" ? "credits.source.grant" : "credits.source.purchase")}${l.note ? html`<br><span class="muted">${l.note}</span>` : nothing}</td>
          <td>${this.expiry(l)}</td></tr>`)}</table></div>` : html`<p class="muted">${t("credits.none")}</p>`}
      ${i.offers.length ? html`<h3 style="margin:6px 0 0;font-size:14px">${t("credits.buy")}</h3>
        <div class="offers">${i.offers.map((o) => html`<div class="offer"><b>${o.name}</b>
          <span>${t("credits.offer_credit", { credit: usd(o.credit) })}</span>
          <span class="muted">${o.validMonths ? t("credits.valid_months", { n: o.validMonths }) : t("credits.valid_never")}</span>
          <button ?disabled=${this.busy} @click=${() => void this.buy(o)}>${icon("CreditCard", 14)} ${money(o.price, i.currency)}</button></div>`)}</div>
        <p class="muted">${t("credits.buy_note")}</p>` : nothing}
    </div>`);
  }
}
customElements.get("phx-ai-credits") || customElements.define("phx-ai-credits", PhxAiCredits);

/** System admins: offers and grants. */
export class PhxCreditsAdmin extends LitElement {
  static properties = { data: { state: true }, offers: { state: true }, busy: { state: true }, target: { state: true }, everyone: { state: true }, preview: { state: true } };
  static styles = [base, settingsUI, shared, css`
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 10px; }
    td input, td select { width: 100%; box-sizing: border-box; }
    textarea { width: 100%; box-sizing: border-box; min-height: 60px; }
    .check { display: flex; gap: 8px; align-items: center; font-size: 14px; }
  `];
  data?: CreditAdmin;
  offers: Offer[] = [];
  busy = false;
  target: "users" | "orgs" = "users";
  everyone = false;
  preview?: { count: number; names: string[] };

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.data = await api<CreditAdmin>("GET", "/admin/ai-credits").catch(() => undefined);
    this.offers = structuredClone(this.data?.offers ?? []);
  }

  private setOffer(i: number, patch: Partial<Offer>) {
    this.offers = this.offers.map((o, j) => (j === i ? { ...o, ...patch } : o));
  }

  private async saveOffers() {
    this.busy = true;
    try {
      this.offers = await api<Offer[]>("PUT", "/admin/ai-credits/offers", this.offers);
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private grantInput(form: HTMLFormElement, dryRun: boolean) {
    const f = new FormData(form);
    const list = (k: string) => String(f.get(k) ?? "").split(/[\s,;]+/).map((x) => x.trim()).filter(Boolean);
    return {
      target: this.target, all: this.everyone, orgs: list("orgs"), emails: this.target === "users" ? list("emails") : [],
      signedUpAfter: this.target === "users" ? String(f.get("after") ?? "") : "", signedUpBefore: this.target === "users" ? String(f.get("before") ?? "") : "",
      credit: Math.round(Number(f.get("credit") ?? 0) * 100), expiresAt: String(f.get("expires") ?? ""), note: String(f.get("note") ?? ""), dryRun,
    };
  }

  private async grant(e: Event, dryRun: boolean) {
    e.preventDefault();
    const form = (e.target as HTMLElement).closest("form") as HTMLFormElement;
    if (!form.reportValidity()) return;
    this.busy = true;
    try {
      const r = await api<{ count: number; names: string[] }>("POST", "/admin/ai-credits/grants", this.grantInput(form, dryRun));
      if (dryRun) {
        this.preview = r;
      } else {
        toast(t("credits.granted", { n: r.count }));
        this.preview = undefined;
        form.reset();
        this.everyone = false;
        await this.load();
      }
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const d = this.data;
    if (!d) return nothing;
    const cur = d.currency || "eur";
    return html`${pageHead(t("credits.title"), t("credits.admin_intro"))}
      ${scard(t("credits.offers"), d.paymentsReady ? t("credits.offers_help") : t("credits.offers_no_payments"), html`<div class="body">
        ${this.offers.length ? html`<div class="wrap"><table>
          <tr><th>${t("credits.offer.id")}</th><th>${t("credits.offer.name")}</th><th>${t("credits.offer.price", { cur: cur.toUpperCase() })}</th><th>${t("credits.offer.credit")}</th><th>${t("credits.offer.months")}</th><th>${t("credits.offer.for")}</th><th></th></tr>
          ${this.offers.map((o, i) => html`<tr>
            <td><input .value=${o.id} aria-label=${t("credits.offer.id")} @change=${(e: Event) => this.setOffer(i, { id: (e.target as HTMLInputElement).value })}></td>
            <td><input .value=${o.name} aria-label=${t("credits.offer.name")} @change=${(e: Event) => this.setOffer(i, { name: (e.target as HTMLInputElement).value })}></td>
            <td><input type="number" min="0.01" step="0.01" .value=${String(o.price / 100)} aria-label=${t("credits.offer.price", { cur })} @change=${(e: Event) => this.setOffer(i, { price: Math.round(Number((e.target as HTMLInputElement).value) * 100) })}></td>
            <td><input type="number" min="0.01" step="0.01" .value=${String(o.credit / 100)} aria-label=${t("credits.offer.credit")} @change=${(e: Event) => this.setOffer(i, { credit: Math.round(Number((e.target as HTMLInputElement).value) * 100) })}></td>
            <td><input type="number" min="0" max="120" .value=${String(o.validMonths)} aria-label=${t("credits.offer.months")} @change=${(e: Event) => this.setOffer(i, { validMonths: Number((e.target as HTMLInputElement).value) })}></td>
            <td><select aria-label=${t("credits.offer.for")} @change=${(e: Event) => this.setOffer(i, { for: (e.target as HTMLSelectElement).value as Offer["for"] })}>
              ${(["both", "user", "org"] as const).map((v) => html`<option value=${v} ?selected=${o.for === v}>${t(`credits.for.${v}`)}</option>`)}</select></td>
            <td><button class="ghost" aria-label=${t("credits.offer.remove")} @click=${() => (this.offers = this.offers.filter((_, j) => j !== i))}>${icon("Trash2", 13)}</button></td></tr>`)}</table></div>` : html`<p class="muted">${t("credits.offers_none")}</p>`}
        <div class="row" style="display:flex;gap:8px">
          <button class="ghost" @click=${() => (this.offers = [...this.offers, { id: `pack-${this.offers.length + 1}`, name: "", price: 1000, credit: 1000, validMonths: 12, for: "both" }])}>${icon("Plus", 13)} ${t("credits.offer.add")}</button>
          <button ?disabled=${this.busy} @click=${() => void this.saveOffers()}>${t("doc.save")}</button>
        </div>
      </div>`)}
      ${scard(t("credits.grant"), t("credits.grant_help"), html`<form class="body" @submit=${(e: Event) => void this.grant(e, false)}>
        <div class="grid">
          <label class="field">${t("credits.target")}<select @change=${(e: Event) => { this.target = (e.target as HTMLSelectElement).value as "users" | "orgs"; this.preview = undefined; }}>
            <option value="users">${t("credits.target.users")}</option><option value="orgs">${t("credits.target.orgs")}</option></select></label>
          <label class="field">${t("credits.amount")}<input name="credit" type="number" min="0.01" step="0.01" required></label>
          <label class="field">${t("credits.expires")}<input name="expires" type="date"><span class="muted">${t("credits.expires_help")}</span></label>
        </div>
        <label class="check"><input type="checkbox" .checked=${this.everyone} @change=${(e: Event) => { this.everyone = (e.target as HTMLInputElement).checked; this.preview = undefined; }}> ${t(this.target === "users" ? "credits.everyone_users" : "credits.everyone_orgs")}</label>
        ${this.everyone ? nothing : html`<div class="grid">
          <label class="field">${t(this.target === "users" ? "credits.filter.orgs_members" : "credits.filter.orgs")}<input name="orgs" placeholder="acme, kunde-gmbh"></label>
          ${this.target === "users" ? html`
            <label class="field">${t("credits.filter.after")}<input name="after" type="date"></label>
            <label class="field">${t("credits.filter.before")}<input name="before" type="date"></label>` : nothing}
        </div>
        ${this.target === "users" ? html`<label class="field">${t("credits.filter.emails")}<textarea name="emails" placeholder="ana@example.org, ben@example.org"></textarea></label>` : nothing}`}
        <label class="field">${t("credits.note")}<input name="note" maxlength="300" placeholder=${t("credits.note_placeholder")}></label>
        ${this.preview ? html`<p class="muted" role="status">${t("credits.preview", { n: this.preview.count })}${this.preview.names.length ? `: ${this.preview.names.join(", ")}${this.preview.count > this.preview.names.length ? " …" : ""}` : ""}</p>` : nothing}
        <div style="display:flex;gap:8px">
          <button type="button" class="ghost" ?disabled=${this.busy} @click=${(e: Event) => void this.grant(e, true)}>${icon("Users", 13)} ${t("credits.preview_btn")}</button>
          <button ?disabled=${this.busy || !this.preview}>${icon("Coins", 13)} ${t("credits.grant_btn")}</button>
        </div>
      </form>`)}
      ${d.grants.length ? scard(t("credits.recent"), undefined, html`<div class="body wrap"><table>
        <tr><th>${t("credits.col.date")}</th><th>${t("credits.col.to")}</th><th>${t("credits.col.amount")}</th><th>${t("credits.col.left")}</th><th>${t("credits.col.expires")}</th></tr>
        ${d.grants.map((g) => html`<tr><td>${formatDate(g.createdAt)}</td><td>${g.ownerName || g.ownerKind}${g.note ? html`<br><span class="muted">${g.note}</span>` : nothing}</td>
          <td>${usd(microsToCents(g.amount))}</td><td>${usd(microsToCents(g.remaining))}</td><td>${g.expiresAt ? formatDay(g.expiresAt) : t("credits.never")}</td></tr>`)}</table></div>`) : nothing}`;
  }
}
customElements.get("phx-credits-admin") || customElements.define("phx-credits-admin", PhxCreditsAdmin);
