// More feedback storage: the project owners' card (usage, packs, buy) and
// the system admins' offers.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import { money } from "./payments";
import { scard, settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Lot { id: string; mb: number; source: string; note?: string; createdAt: string; expiresAt?: string; revoked?: boolean }
interface Offer { id: string; name: string; price: number; mb: number; validMonths: number }
interface Storage { usedBytes: number; limitBytes: number; baseMb: number; extraMb: number; lots: Lot[]; offers: Offer[]; currency?: string }

const size = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toLocaleString(document.documentElement.lang || "en", { maximumFractionDigits: 1 })} GB` : `${mb} MB`);
const bytes = (b: number) => size(Math.round(b / (1 << 20)));

/** Project settings → Feedback portals: storage and packs (owners). */
export class PhxFeedbackStorage extends LitElement {
  static properties = { project: {}, data: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .card { border: 1px solid var(--line); border-radius: 14px; background: var(--surface); padding: 16px; display: grid; gap: 10px; margin-bottom: 14px; }
    .card h3 { margin: 0; font-size: 15px; display: flex; gap: 8px; align-items: center; }
    .bar { height: 8px; border-radius: 99px; background: var(--line); overflow: hidden; }
    .bar span { display: block; height: 100%; background: var(--accent); }
    .muted { color: var(--muted); font-size: 13px; margin: 0; }
    .offers { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
    .offer { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: grid; gap: 6px; }
    .lot { display: flex; gap: 8px; font-size: 13.5px; align-items: center; }
    .lot.off { color: var(--muted); text-decoration: line-through; }
  `];

  project = "";
  data?: Storage;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
    const q = new URLSearchParams(location.search);
    if (q.get("purchase") && q.get("result") === "ok") toast(t("fbs.bought"));
  }

  private async load() {
    this.data = await api<Storage>("GET", `/projects/${this.project}/feedback/storage`).catch(() => undefined);
  }

  private async buy(o: Offer) {
    this.busy = true;
    try {
      const r = await api<{ url: string }>("POST", `/projects/${this.project}/feedback/storage/buy`, { offer: o.id });
      location.href = r.url;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      this.busy = false;
    }
  }

  render() {
    const d = this.data;
    if (!d) return nothing;
    const pct = d.limitBytes ? Math.min(100, (d.usedBytes / d.limitBytes) * 100) : 0;
    const now = Date.now();
    return html`<div class="card"><h3>${icon("HardDrive", 15)} ${t("fbs.title")}</h3>
      <div>${d.limitBytes ? t("fbs.used_of", { used: bytes(d.usedBytes), limit: bytes(d.limitBytes) }) : t("fbs.used", { used: bytes(d.usedBytes) })}</div>
      ${d.limitBytes ? html`<div class="bar" role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow=${Math.round(pct)} aria-label=${t("fbs.title")}><span style=${`width:${pct}%`}></span></div>` : nothing}
      ${d.extraMb ? html`<p class="muted">${t("fbs.split", { base: size(d.baseMb), extra: size(d.extraMb) })}</p>` : nothing}
      ${d.lots.map((l) => {
        const off = l.revoked || (l.expiresAt && Date.parse(l.expiresAt) < now);
        return html`<div class=${off ? "lot off" : "lot"}>${icon("Package", 13)} +${size(l.mb)} · ${l.note || t(l.source === "grant" ? "fbs.grant" : "fbs.pack")}
          <span class="muted">${l.expiresAt ? t("fbs.until", { date: formatDay(l.expiresAt) }) : t("fbs.no_end")}</span></div>`;
      })}
      ${d.offers.length ? html`<strong style="font-size:14px">${t("fbs.buy")}</strong>
        <div class="offers">${d.offers.map((o) => html`<div class="offer"><b>${o.name}</b><span class="muted">+${size(o.mb)} · ${o.validMonths ? t("fbs.months", { n: o.validMonths }) : t("fbs.no_end")}</span>
          <div><button ?disabled=${this.busy} @click=${() => void this.buy(o)}>${icon("CreditCard", 14)} ${money(o.price, d.currency || "eur")}</button></div></div>`)}</div>` : nothing}
    </div>`;
  }
}
customElements.get("phx-feedback-storage") || customElements.define("phx-feedback-storage", PhxFeedbackStorage);

/** Administration → Storage: feedback storage packs (system admins). */
export class PhxFeedbackStorageAdmin extends LitElement {
  static properties = { offers: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; margin-top: 16px; }
    table { width: 100%; border-collapse: collapse; }
    td, th { padding: 4px 6px; text-align: left; font-size: 13px; }
    td input { width: 100%; }
  `];

  offers: Offer[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void api<Offer[]>("GET", "/admin/feedback-storage/offers").then((o) => (this.offers = o ?? [])).catch(() => {});
  }

  private set(i: number, patch: Partial<Offer>) {
    this.offers = this.offers.map((o, j) => (j === i ? { ...o, ...patch } : o));
  }

  private async save() {
    this.busy = true;
    try {
      this.offers = await api<Offer[]>("PUT", "/admin/feedback-storage/offers", this.offers);
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    return scard(t("fbs.admin_title"), t("fbs.admin_help"), html`<div class="body">
      ${this.offers.length ? html`<div class="wrap"><table>
        <tr><th>${t("credits.offer.id")}</th><th>${t("credits.offer.name")}</th><th>${t("fbs.price")}</th><th>MB</th><th>${t("credits.offer.months")}</th><th></th></tr>
        ${this.offers.map((o, i) => html`<tr>
          <td><input .value=${o.id} aria-label=${t("credits.offer.id")} @change=${(e: Event) => this.set(i, { id: (e.target as HTMLInputElement).value })}></td>
          <td><input .value=${o.name} aria-label=${t("credits.offer.name")} @change=${(e: Event) => this.set(i, { name: (e.target as HTMLInputElement).value })}></td>
          <td><input type="number" min="0.01" step="0.01" .value=${String(o.price / 100)} aria-label=${t("fbs.price")} @change=${(e: Event) => this.set(i, { price: Math.round(Number((e.target as HTMLInputElement).value) * 100) })}></td>
          <td><input type="number" min="1" .value=${String(o.mb)} aria-label="MB" @change=${(e: Event) => this.set(i, { mb: Number((e.target as HTMLInputElement).value) })}></td>
          <td><input type="number" min="0" max="120" .value=${String(o.validMonths)} aria-label=${t("credits.offer.months")} @change=${(e: Event) => this.set(i, { validMonths: Number((e.target as HTMLInputElement).value) })}></td>
          <td><button class="ghost" aria-label=${t("credits.offer.remove")} @click=${() => (this.offers = this.offers.filter((_, j) => j !== i))}>${icon("Trash2", 13)}</button></td></tr>`)}</table></div>`
        : html`<p class="muted">${t("fbs.admin_none")}</p>`}
      <div class="row" style="display:flex;gap:8px">
        <button class="ghost" @click=${() => (this.offers = [...this.offers, { id: `storage-${this.offers.length + 1}`, name: "", price: 500, mb: 5120, validMonths: 12 }])}>${icon("Plus", 13)} ${t("credits.offer.add")}</button>
        <button ?disabled=${this.busy} @click=${() => void this.save()}>${t("doc.save")}</button>
      </div></div>`);
  }
}
customElements.get("phx-feedback-storage-admin") || customElements.define("phx-feedback-storage-admin", PhxFeedbackStorageAdmin);
