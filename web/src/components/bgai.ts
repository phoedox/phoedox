// Background AI (ADR-0077): a project's limit and use (quotas page), and an
// organization's general and per-project limits (organization page).
import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Status { orgGeneral?: number; orgProject?: number; owner?: number; limit: number; used: number; available: boolean; reason?: string; canSetOrg: boolean; canSetOwner: boolean; org?: string; pricesHidden?: boolean; percent?: number }
interface OrgLimits { general?: number; projects?: Record<string, number> }

const eur = (cents: number) => (cents / 100).toLocaleString(document.documentElement.lang || "en", { style: "currency", currency: "USD" });
const toCents = (v: string) => (v.trim() === "" ? null : Math.round(Number(v.replace(",", ".")) * 100));

/** The background AI tile of a project's quotas page. */
export class PhxBackgroundAI extends LitElement {
  static properties = { project: {}, st: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .tile { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); padding: 14px 16px; display: grid; gap: 8px; }
    .k { color: var(--muted); font-size: 13px; display: flex; gap: 6px; align-items: center; }
    .v { font-size: 22px; font-weight: 600; font-family: var(--font-display); }
    .v small { font-size: 13px; color: var(--muted); font-weight: 400; }
    .bar { height: 6px; border-radius: 3px; background: var(--surface2); overflow: hidden; }
    .bar i { display: block; height: 100%; background: var(--accent); }
    .bar i.full { background: var(--bad); }
    .meta { font-size: 12.5px; color: var(--muted); }
    form { display: flex; gap: 6px; align-items: flex-end; flex-wrap: wrap; }
    form label { display: grid; gap: 3px; font-size: 12.5px; color: var(--muted); }
    form input { width: 110px; padding: 5px 8px; }
  `];

  project = "";
  st?: Status;

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("project") && this.project) void this.load();
  }

  private async load() {
    this.st = await api<Status>("GET", `/projects/${this.project}/background-ai`).catch(() => undefined);
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const v = String(new FormData(e.target as HTMLFormElement).get("owner") ?? "");
    try {
      this.st = await api<Status>("PUT", `/projects/${this.project}/background-ai`, { owner: toCents(v) });
      toast(t("doc.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    const s = this.st;
    if (!s) return nothing;
    const pct = s.pricesHidden ? s.percent ?? 0 : s.limit ? Math.min(100, (s.used / s.limit) * 100) : 0;
    return html`<div class="tile">
      <span class="k">${icon("Bot", 14)} ${t("bgai.title")}</span>
      <span class="v">${s.pricesHidden ? `${Math.round(pct)} %` : eur(s.used)}${s.limit && !s.pricesHidden ? html` <small>${t("usage.of", { limit: eur(s.limit) })}</small>` : nothing}</span>
      ${s.limit ? html`<div class="bar" role="meter" aria-valuenow=${Math.round(pct)} aria-valuemin="0" aria-valuemax="100" aria-label=${t("bgai.title")}><i class=${pct >= 100 ? "full" : ""} style="width:${pct}%"></i></div>` : nothing}
      <span class="meta">${s.available ? t("bgai.available") : t(`bgai.reason.${s.reason ?? ""}` as Key, {}) || t("bgai.unavailable")}</span>
      <span class="meta">${[s.orgProject !== undefined ? t("bgai.org_project", { v: eur(s.orgProject) }) : s.orgGeneral !== undefined ? t("bgai.org_general", { v: eur(s.orgGeneral) }) : "",
        s.owner !== undefined ? t("bgai.owner_limit", { v: eur(s.owner) }) : ""].filter(Boolean).join(" · ") || t("bgai.no_limit")}</span>
      ${s.canSetOwner && !s.pricesHidden ? html`<form @submit=${this.save}><label>${t("bgai.owner_label")}<input name="owner" inputmode="decimal" .value=${s.owner !== undefined ? String(s.owner / 100) : ""} placeholder="–"></label>
        <button class="ghost">${t("doc.save")}</button></form>` : nothing}
      ${s.canSetOrg && s.org ? html`<a class="meta" href="/orgs/${s.org}#background-ai">${t("bgai.org_link")}</a>` : nothing}
    </div>`;
  }
}

/** An organization's background AI limits. */
export class PhxOrgBackgroundAI extends LitElement {
  static properties = { org: {}, projects: { attribute: false }, l: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    form { display: grid; gap: 10px; }
    .row label { display: grid; gap: 3px; font-size: 13px; color: var(--muted); }
    table { border-collapse: collapse; font-size: 14px; }
    td { padding: 4px 8px 4px 0; }
    input { width: 120px; padding: 5px 8px; }
  `];

  org = "";
  projects: { id: string; slug: string; name: string }[] = [];
  l?: OrgLimits;

  willUpdate(ch: Map<string, unknown>) {
    if (!ch.has("org") || !this.org) return;
    void api<OrgLimits>("GET", `/orgs/${this.org}/background-ai`).then((l) => (this.l = l)).catch(() => undefined);
    void api<{ id: string; slug: string; name: string; orgId?: string; orgSlug?: string }[]>("GET", "/projects").then(async (ps) => {
      const o = await api<{ id: string }>("GET", `/orgs/${this.org}`);
      this.projects = ps.filter((p) => p.orgId === o.id);
    }).catch(() => undefined);
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const projects: Record<string, number | null> = {};
    for (const p of this.projects) projects[p.slug] = toCents(String(f.get(`p_${p.slug}`) ?? ""));
    try {
      this.l = await api<OrgLimits>("PUT", `/orgs/${this.org}/background-ai`, { general: toCents(String(f.get("general") ?? "")), projects });
      toast(t("doc.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    if (!this.l) return nothing;
    const cents = (v?: number) => (v === undefined || v === null ? "" : String(v / 100));
    return html`<form @submit=${this.save}>
      <p class="muted" style="margin:0;font-size:13px">${t("bgai.org_help")}</p>
      <div class="row"><label>${t("bgai.general")}<input name="general" inputmode="decimal" .value=${cents(this.l.general)} placeholder="–"></label></div>
      ${this.projects.length ? html`<table>${this.projects.map((p) => html`<tr><td>${p.name}</td><td><input name=${`p_${p.slug}`} inputmode="decimal" aria-label=${p.name}
        .value=${cents(this.l?.projects?.[p.id])} placeholder=${t("bgai.like_general")}></td></tr>`)}</table>` : nothing}
      <div><button>${t("doc.save")}</button></div>
    </form>`;
  }
}

customElements.get("phx-background-ai") || customElements.define("phx-background-ai", PhxBackgroundAI);
customElements.get("phx-org-background-ai") || customElements.define("phx-org-background-ai", PhxOrgBackgroundAI);
