import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Hook { id: string; url: string; events: string[]; active: boolean; failures: number; createdAt: string; secret?: string }
interface Delivery { id: string; eventType: string; status: string; response: number; error?: string; durationMs: number; createdAt: string }

/** Webhooks of a project (owners): add, switch, test, see deliveries. */
export class PhxWebhooks extends LitElement {
  static properties = { project: {}, hooks: { state: true }, events: { state: true }, created: { state: true }, open: { state: true }, deliveries: { state: true }, all: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .hook { padding: 12px 14px; margin-bottom: 8px; }
    .hook header { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .hook code { word-break: break-all; margin-right: auto; }
    .hook button { padding: 3px 9px; font-size: 13px; }
    .events { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 6px; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; margin-top: 8px; }
    td { padding: 4px 8px 4px 0; border-bottom: 1px solid var(--line); }
    .ok { color: var(--good); } .failed { color: var(--bad); }
    form { display: grid; gap: 8px; max-width: 640px; }
    fieldset { border: 1px solid var(--line); border-radius: 8px; display: flex; flex-wrap: wrap; gap: 6px 14px; font-size: 13px; }
    fieldset label { display: flex; gap: 5px; align-items: center; }
    fieldset input { width: auto; }
    .secret { border-color: var(--accent); }
  `];
  project = "";
  hooks: Hook[] = [];
  events: string[] = [];
  created?: Hook;
  open = "";
  deliveries: Delivery[] = [];
  all = true;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    const r = await api<{ webhooks: Hook[]; events: string[] }>("GET", `/projects/${this.project}/webhooks`).catch(() => undefined);
    if (r) [this.hooks, this.events] = [r.webhooks, r.events];
  }

  private async call<T>(fn: () => Promise<T>): Promise<T | undefined> {
    try {
      return await fn();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return undefined;
    }
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const events = this.all ? ["*"] : f.getAll("event").map(String);
    const h = await this.call(() => api<Hook>("POST", `/projects/${this.project}/webhooks`, { url: f.get("url"), events }));
    if (h) {
      this.created = h;
      form.reset();
      this.all = true;
      await this.load();
    }
  }

  private async showDeliveries(h: Hook) {
    this.open = this.open === h.id ? "" : h.id;
    if (this.open) this.deliveries = (await this.call(() => api<Delivery[]>("GET", `/projects/${this.project}/webhooks/${h.id}/deliveries`))) ?? [];
  }

  private async test(h: Hook) {
    const d = await this.call(() => api<Delivery>("POST", `/projects/${this.project}/webhooks/${h.id}/test`));
    if (d) toast(d.status === "ok" ? t("hooks.test_ok", { status: d.response }) : t("hooks.test_failed", { error: d.error ?? "" }), d.status === "ok" ? "ok" : "error");
    if (this.open === h.id) await this.showDeliveries(h).then(() => this.showDeliveries(h));
    await this.load();
  }

  render() {
    return html`
      <p class="muted" style="max-width:680px;margin-top:0">${t("hooks.intro")}</p>
      ${this.created?.secret ? html`<div class="card secret" style="margin-bottom:12px"><b>${t("hooks.secret_once")}</b>
        <p class="row" style="margin:8px 0 0"><code>${this.created.secret}</code>
          <button class="ghost" @click=${() => navigator.clipboard?.writeText(this.created!.secret!).then(() => toast(t("tokens.copied")))}>${icon("Copy", 13)} ${t("tokens.copy")}</button></p>
        <p class="muted" style="font-size:12px;margin:6px 0 0">${t("hooks.verify_hint")}</p></div>` : ""}
      ${this.hooks.map((h) => html`<div class="card hook">
        <header>${icon("Webhook", 15)}<code>${h.url}</code>
          ${h.failures ? html`<span class="chip" style="color:var(--bad)">${t("hooks.failures", { n: h.failures })}</span>` : ""}
          <label class="row" style="gap:5px;font-size:13px"><input type="checkbox" style="width:auto" .checked=${h.active}
            @change=${async (e: Event) => { await this.call(() => api("PATCH", `/projects/${this.project}/webhooks/${h.id}`, { active: (e.target as HTMLInputElement).checked })); await this.load(); }}> ${t("hooks.active")}</label>
          <button class="ghost" @click=${() => this.test(h)}>${icon("Send", 13)} ${t("hooks.test")}</button>
          <button class="ghost" aria-expanded=${this.open === h.id} @click=${() => this.showDeliveries(h)}>${t("hooks.deliveries")}</button>
          <button class="danger" aria-label=${t("hooks.delete")} @click=${async () => { if (confirm(t("hooks.delete_confirm"))) { await this.call(() => api("DELETE", `/projects/${this.project}/webhooks/${h.id}`)); await this.load(); } }}>${icon("Trash2", 13)}</button>
        </header>
        <div class="events">${h.events.map((e) => html`<span class="chip">${e === "*" ? t("hooks.all_events") : e}</span>`)}</div>
        ${this.open === h.id ? (this.deliveries.length ? html`<table>${this.deliveries.map((d) => html`<tr>
          <td class=${d.status}>${d.status === "ok" ? "✓" : d.status === "failed" ? "✗" : "…"} ${d.response || ""}</td><td class="mono">${d.eventType}</td>
          <td class="muted">${formatDate(d.createdAt)}</td><td class="muted">${d.durationMs} ms</td><td class="muted" style="font-size:12px">${d.error ?? ""}</td></tr>`)}</table>`
          : html`<p class="muted" style="font-size:13px">${t("hooks.no_deliveries")}</p>`) : ""}
      </div>`)}
      <form @submit=${this.create} style="margin-top:12px">
        <label class="field">${t("hooks.url")}<input name="url" type="url" required placeholder="https://example.org/phoedox-hook"></label>
        <label class="row" style="gap:6px;font-size:14px"><input type="checkbox" style="width:auto" .checked=${this.all} @change=${(e: Event) => (this.all = (e.target as HTMLInputElement).checked)}> ${t("hooks.all_events")}</label>
        ${this.all ? "" : html`<fieldset><legend class="muted">${t("hooks.events")}</legend>${this.events.map((e) => html`<label><input type="checkbox" name="event" value=${e}> <span class="mono">${e}</span></label>`)}</fieldset>`}
        <div><button>${icon("Plus", 13)} ${t("hooks.add")}</button></div>
      </form>`;
  }
}
customElements.define("phx-webhooks", PhxWebhooks);
