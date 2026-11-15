// The first page of the administration (ADR-0070): the state of the
// instance at a glance – version, users, organisations, backups, AI – and
// what needs attention.

import { LitElement, css, html, nothing } from "lit";
import { api, type User } from "../api";
import { dollars } from "../ai-chat";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { pageHead, scard, settingsUI } from "./settings-ui";
import { base } from "./shared";

interface Backups { backups: { createdAt: string }[]; running: boolean; lastError?: string; everyHours: number }
interface Settings { aiChat?: { enabled: boolean; usedCents: number; chargedCents?: number }; signup: boolean }

export class PhxAdminOverview extends LitElement {
  static properties = { version: { state: true }, users: { state: true }, orgs: { state: true }, backups: { state: true }, settings: { state: true } };
  static styles = [base, settingsUI, css`
    .tiles { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin-bottom: 18px; }
    .tile { background: var(--surface); border: 1px solid var(--line); border-radius: 12px; padding: 14px 16px; display: grid; gap: 4px; color: var(--fg); text-decoration: none; }
    .tile .k { color: var(--muted); font-size: 12.5px; display: flex; gap: 6px; align-items: center; }
    .tile .v { font: 600 22px var(--font-display); font-variant-numeric: tabular-nums; }
    .tile .s { font-size: 12.5px; color: var(--muted); }
    .tile .s.warn { color: var(--signal); }
    @media (hover: hover) and (pointer: fine) { a.tile:hover { border-color: var(--muted); } }
    @media (max-width: 900px) { .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  `];
  version = "";
  users?: User[];
  orgs?: unknown[];
  backups?: Backups;
  settings?: Settings;

  connectedCallback() {
    super.connectedCallback();
    void fetch("/healthz").then((r) => r.json()).then((h: { version?: string }) => (this.version = h.version ?? "")).catch(() => {});
    void api<User[]>("GET", "/admin/users").then((u) => (this.users = u ?? [])).catch(() => (this.users = []));
    void api<unknown[]>("GET", "/orgs").then((o) => (this.orgs = o ?? [])).catch(() => (this.orgs = []));
    void api<Backups>("GET", "/admin/backups").then((b) => (this.backups = b)).catch(() => {});
    void api<Settings>("GET", "/admin/settings").then((s) => (this.settings = s)).catch(() => {});
  }

  /** Hours since the newest backup, or undefined. */
  private backupAge(): number | undefined {
    const last = this.backups?.backups?.map((b) => Date.parse(b.createdAt)).sort((a, b) => b - a)[0];
    return last ? (Date.now() - last) / 3.6e6 : undefined;
  }

  render() {
    const age = this.backupAge();
    const every = this.backups?.everyHours || 24;
    const stale = this.backups && (age === undefined || age > every * 1.5 || !!this.backups.lastError);
    const invited = this.users?.filter((u) => !u.lastLoginAt).length ?? 0;
    const ai = this.settings?.aiChat;
    const attention: { icon: string; text: string; href: string }[] = [];
    if (stale) attention.push({ icon: "TriangleAlert", text: this.backups?.lastError ? t("sx.ov.backup_failed", { error: this.backups.lastError }) : t("sx.ov.backup_old"), href: "/admin/backups" });
    if (this.settings && !this.settings.signup) { /* closed sign-up is a normal state */ }
    return html`${pageHead(t("sx.admin.overview"), this.version ? t("sx.ov.version", { version: this.version, host: location.host }) : location.host)}
      <div class="tiles">
        <a class="tile" href="/admin/users"><span class="k">${icon("Users", 14)} ${t("users.title")}</span><span class="v">${this.users?.length ?? "–"}</span>
          <span class="s">${invited ? t("sx.ov.invited", { count: invited }) : t("sx.ov.all_active")}</span></a>
        <a class="tile" href="/admin/orgs"><span class="k">${icon("Building2", 14)} ${t("sx.admin.orgs")}</span><span class="v">${this.orgs?.length ?? "–"}</span><span class="s">&nbsp;</span></a>
        <a class="tile" href="/admin/backups"><span class="k">${icon("Archive", 14)} ${t("sx.ov.last_backup")}</span>
          <span class="v">${age === undefined ? "–" : age < 1 ? t("sx.ov.just_now") : age < 48 ? t("sx.ov.hours", { count: Math.round(age) }) : t("sx.ov.days", { count: Math.round(age / 24) })}</span>
          <span class="s ${stale ? "warn" : ""}">${stale ? t("sx.ov.backup_attention") : t("sx.ov.every", { hours: every })}</span></a>
        <a class="tile" href="/admin/ai"><span class="k">${icon("Bot", 14)} ${t("sx.ov.ai_month")}</span>
          <span class="v">${ai ? dollars(ai.chargedCents ?? ai.usedCents, locale()) : "–"}</span><span class="s">${ai ? t(ai.enabled ? "sx.ov.ai_on" : "sx.ov.ai_off") : "&nbsp;"}</span></a>
      </div>
      ${scard(t("sx.ov.attention"), "", attention.length
        ? html`<div class="slist">${attention.map((a) => html`<div class="item"><span class="bx" style="color:var(--signal)">${icon(a.icon, 16)}</span><div class="t"><b>${a.text}</b></div><a class="btn ghost" href=${a.href}>${t("sx.ov.open")}</a></div>`)}</div>`
        : html`<div class="body"><p class="muted" style="margin:0">${icon("CircleCheck", 14)} ${t("sx.ov.nothing")}</p></div>`)}
      ${this.backups?.backups?.length ? html`<p class="muted" style="font-size:12.5px">${t("sx.ov.backup_at", { date: formatDate(this.backups.backups.map((b) => b.createdAt).sort().reverse()[0]) })}</p>` : nothing}`;
  }
}
customElements.define("phx-admin-overview", PhxAdminOverview);
