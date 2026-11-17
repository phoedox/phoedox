// Service providers (ADR-0073): customer admins grant a provider
// organization access; provider members see their customers with what is
// open and download the time per customer.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Link {
  id: string; customerName: string; customerSlug: string; providerName: string; providerSlug: string;
  role: "reader" | "editor" | "admin"; projects: string[]; expiresAt?: string; note?: string; createdAt: string; revokedAt?: string;
}
interface Links { providers: Link[]; customers: Link[] }
interface CustomerProject { slug: string; name: string; role: string; openTasks: number; failingChecks: number; maintenanceDue: number; minutesMonth: number }
interface Customer { org: string; name: string; role: "reader" | "editor" | "admin"; expiresAt?: string; projects: CustomerProject[] }

/** The last day of access (stored as the start of the next day). */
const lastDay = (iso: string) => formatDay(new Date(Date.parse(iso) - 1).toISOString().slice(0, 10));
const hours = (m: number) => (m ? `${Math.floor(m / 60)}:${String(m % 60).padStart(2, "0")} h` : "–");

/** The org page section for providers (as customer) and customers (as provider). */
export class PhxOrgProviders extends LitElement {
  static properties = { org: {}, admin: { type: Boolean }, projects: { attribute: false }, links: { state: true }, customers: { state: true }, busy: { state: true }, all: { state: true }, templates: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; }
    th, td { text-align: left; padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); vertical-align: top; }
    th { color: var(--muted); font-weight: 500; font-size: 12.5px; }
    td.n { font-variant-numeric: tabular-nums; }
    td .bad { color: var(--bad); font-weight: 600; }
    td .sig { color: var(--signal); font-weight: 600; }
    .chip { display: inline-block; padding: 1px 8px; border-radius: 999px; background: var(--surface2); font-size: 12.5px; }
    .chip.off { opacity: .6; text-decoration: line-through; }
    form.grant { display: grid; gap: 8px; max-width: 560px; margin-top: 10px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; }
    .inline-check { display: inline-flex; gap: 8px; align-items: center; font-size: 14px; }
    .inline-check input { width: auto; }
    .checks { display: flex; flex-wrap: wrap; gap: 4px 14px; font-size: 13.5px; }
    .cust { margin: 14px 0 6px; display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; }
    .cust h3 { margin: 0; font-size: 15px; }
    .wrap { overflow-x: auto; }
    @media (max-width: 600px) { th:nth-child(5), td:nth-child(5) { display: none; } }
  `];
  org = "";
  admin = false;
  projects: { id: string; slug: string; name: string }[] = [];
  links?: Links;
  customers: Customer[] = [];
  busy = false;
  all = true;
  /** Ids of the organization's template projects. */
  templates: string[] = [];

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("org") && this.org) void this.load();
  }

  private async load() {
    this.links = await api<Links>("GET", `/orgs/${this.org}/providers`).catch(() => undefined);
    this.templates = await api<string[]>("GET", `/orgs/${this.org}/templates`).catch(() => []);
    this.customers = this.links?.customers.length ? await api<Customer[]>("GET", `/orgs/${this.org}/customers`).catch(() => []) : [];
  }

  private async grant(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.busy = true;
    try {
      await api("POST", `/orgs/${this.org}/providers`, {
        provider: String(f.get("provider") ?? "").trim(), role: f.get("role"), until: f.get("until") || "",
        projects: this.all ? [] : f.getAll("projects"), note: String(f.get("note") ?? ""),
      });
      form.reset();
      this.all = true;
      toast(t("prov.granted"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async revoke(l: Link, mine: boolean) {
    if (!confirm(t(mine ? "prov.give_up_confirm" : "prov.revoke_confirm", { name: mine ? l.customerName : l.providerName }))) return;
    try {
      await api("DELETE", `/orgs/${this.org}/providers/${l.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async saveTemplates(id: string, on: boolean) {
    const ids = on ? [...this.templates, id] : this.templates.filter((x) => x !== id);
    const slugs = ids.map((x) => this.projects.find((p) => p.id === x)?.slug ?? x);
    try {
      this.templates = await api<string[]>("PUT", `/orgs/${this.org}/templates`, { projects: slugs });
      toast(t("doc.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private templatesView() {
    if (!this.projects.length) return nothing;
    return html`<h2 id="templates">${icon("Copy", 16)} ${t("prov.templates")}</h2>
      <p class="muted" style="margin-top:0">${t("prov.templates_intro")}</p>
      <div class="checks">${this.projects.map((p) => html`<label class="inline-check"><input type="checkbox" .checked=${this.templates.includes(p.id)}
        @change=${(e: Event) => this.saveTemplates(p.id, (e.target as HTMLInputElement).checked)}> ${p.name}</label>`)}</div>`;
  }

  private scope(l: Link) {
    if (!l.projects.length) return t("prov.all_projects");
    return l.projects.map((id) => this.projects.find((p) => p.id === id)?.name ?? id).join(", ");
  }

  private providers() {
    const ls = this.links?.providers ?? [];
    return html`<h2 id="providers">${icon("Handshake", 16)} ${t("prov.title")}</h2>
      <p class="muted" style="margin-top:0">${t("prov.intro")}</p>
      ${ls.length ? html`<div class="wrap"><table><tr><th>${t("prov.provider")}</th><th>${t("prov.role")}</th><th>${t("prov.scope")}</th><th>${t("prov.until")}</th><th></th></tr>
        ${ls.map((l) => html`<tr><td><b>${l.providerName}</b>${l.note ? html`<br><span class="muted">${l.note}</span>` : nothing}</td>
          <td><span class="chip ${l.revokedAt ? "off" : ""}">${t(`prov.role.${l.role}`)}</span></td><td>${this.scope(l)}</td>
          <td>${l.revokedAt ? t("prov.revoked") : l.expiresAt ? lastDay(l.expiresAt) : "–"}</td>
          <td>${l.revokedAt ? nothing : html`<button class="danger" @click=${() => this.revoke(l, false)}>${t("prov.revoke")}</button>`}</td></tr>`)}</table></div>`
        : html`<p class="muted">${t("prov.none")}</p>`}
      <form class="grant" @submit=${this.grant}>
        <div class="row">
          <label class="field" style="flex:1 1 200px">${t("prov.provider_slug")}<input name="provider" required placeholder="systemhaus" spellcheck="false"></label>
          <label class="field">${t("prov.role")}<select name="role"><option value="reader">${t("prov.role.reader")}</option><option value="editor" selected>${t("prov.role.editor")}</option><option value="admin">${t("prov.role.admin")}</option></select></label>
          <label class="field">${t("prov.until")}<input name="until" type="date"></label>
        </div>
        <label class="inline-check"><input type="checkbox" .checked=${this.all} @change=${(e: Event) => (this.all = (e.target as HTMLInputElement).checked)}> ${t("prov.all_projects")}</label>
        ${this.all ? nothing : html`<div class="checks">${this.projects.map((p) => html`<label><input type="checkbox" name="projects" value=${p.slug}> ${p.name}</label>`)}</div>`}
        <label class="field">${t("prov.note")}<input name="note" maxlength="500"></label>
        <div><button ?disabled=${this.busy}>${icon("Plus", 13)} ${t("prov.grant")}</button></div>
      </form>`;
  }

  private customersView() {
    const ls = this.links?.customers ?? [];
    if (!ls.length) return nothing;
    const month = new Date().toISOString().slice(0, 7);
    return html`<h2 id="customers">${icon("Building2", 16)} ${t("prov.customers")}</h2>
      <p class="muted" style="margin-top:0">${t("prov.customers_intro")}</p>
      ${this.admin ? html`<p><a class="btn ghost" href="/api/v1/orgs/${this.org}/customers/time.csv?month=${month}" download>${icon("Download", 14)} ${t("prov.time_csv")}</a></p>` : nothing}
      ${this.customers.map((c) => html`<div class="cust"><h3>${c.name}</h3><span class="chip">${t(`prov.role.${c.role}`)}</span>
          ${c.expiresAt ? html`<span class="muted">${t("prov.until_date", { date: lastDay(c.expiresAt) })}</span>` : nothing}
          ${this.admin ? html`<button class="ghost" @click=${() => { const l = ls.find((x) => x.customerSlug === c.org); if (l) void this.revoke(l, true); }}>${t("prov.give_up")}</button>` : nothing}</div>
        ${c.projects.length ? html`<div class="wrap"><table><tr><th>${t("prov.project")}</th><th>${t("prov.open")}</th><th>${t("prov.failing")}</th><th>${t("prov.maintenance")}</th><th>${t("prov.time")}</th></tr>
          ${c.projects.map((p) => html`<tr><td><a href="/p/${p.slug}">${p.name}</a></td><td class="n">${p.openTasks}</td>
            <td class="n">${p.failingChecks ? html`<span class="bad">${p.failingChecks}</span>` : "0"}</td>
            <td class="n">${p.maintenanceDue ? html`<span class="sig">${p.maintenanceDue}</span>` : "0"}</td><td class="n">${hours(p.minutesMonth)}</td></tr>`)}</table></div>`
          : html`<p class="muted">${t("prov.no_projects")}</p>`}`)}`;
  }

  render() {
    if (!this.links) return nothing;
    return html`${this.admin ? this.providers() : nothing}${this.customersView()}${this.admin ? this.templatesView() : nothing}`;
  }
}
customElements.get("phx-org-providers") || customElements.define("phx-org-providers", PhxOrgProviders);
