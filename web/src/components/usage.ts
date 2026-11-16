import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { pageHead } from "./settings-ui";

type Level = "project" | "org" | "admin" | "me";
interface Line { id: string; name: string; slug?: string; calls: number; inputTokens: number; outputTokens: number; chargedCents: number; costCents?: number; ownKeyCents?: number }
interface Budget { key: string; name?: string; usedCents: number; limitCents: number; percent: number; hasLimit: boolean; usedTokens?: number; limitTokens?: number }
interface Storage { id: string; name: string; slug?: string; usedBytes: number; limitBytes: number }
interface Report {
  level: Level; name: string; since: string; chargedCents: number; costCents?: number; budgets: Budget[];
  byProject?: Line[]; byUser?: Line[]; byModel?: Line[]; byOrg?: Line[]; storage?: Storage[]; storageBytes: number;
  up: { level: "org" | "admin"; href: string; name?: string }[];
  pricesHidden: boolean; calls: number; tokens: number;
}

const money = (cents: number) => new Intl.NumberFormat(document.documentElement.lang || "en", { style: "currency", currency: "USD" }).format(cents / 100);
const num = (n: number) => new Intl.NumberFormat(document.documentElement.lang || "en", { notation: n >= 100_000 ? "compact" : "standard" }).format(n);
const bytes = (b: number) => {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${b.toFixed(i && b < 10 ? 1 : 0)} ${u[i]}`;
};

/** Usage of one level (project, organization, instance, person) this month,
 * with links up to the levels the viewer may open. */
export class PhxUsage extends LitElement {
  static properties = { level: {}, ref: {}, report: { state: true }, error: { state: true }, tab: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .up { display: flex; gap: 8px; flex-wrap: wrap; margin: -4px 0 18px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; margin-bottom: 22px; }
    .tile { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 14px 16px; display: grid; gap: 6px; min-width: 0; }
    .tile .k { font-size: 12.5px; color: var(--muted); }
    .tile .v { font: 600 22px var(--font-display, inherit); font-variant-numeric: tabular-nums; }
    .tile .v small { font: 400 13px var(--font-body, inherit); color: var(--muted); }
    .meter { height: 8px; border-radius: 4px; background: var(--surface2); overflow: hidden; }
    .meter span { display: block; height: 100%; background: var(--accent); border-radius: 4px; }
    .meter span.warn { background: var(--signal); }
    .meter span.full { background: var(--bad); }
    h2 { font-size: 17px; margin: 22px 0 8px; display: flex; gap: 8px; align-items: center; }
    .tbl { overflow-x: auto; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th { text-align: left; font-size: 12px; font-weight: 500; color: var(--muted); padding: 8px 12px; border-bottom: 1px solid var(--line); white-space: nowrap; }
    td { padding: 8px 12px; border-bottom: 1px solid var(--surface2); }
    tr:last-child td { border-bottom: 0; }
    td.n, th.n { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
    td a { color: var(--fg); }
    .share { display: inline-block; width: 60px; height: 6px; border-radius: 3px; background: var(--surface2); vertical-align: middle; margin-left: 8px; overflow: hidden; }
    .share span { display: block; height: 100%; background: var(--accent); }
    .empty { color: var(--muted); font-size: 14px; padding: 10px 12px; }
    .muted { color: var(--muted); }
    .tile .meta { font-size: 12.5px; color: var(--muted); font-variant-numeric: tabular-nums; }
    .tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin: 0 0 18px; }
    .tabs button { background: none; border: 0; border-bottom: 2px solid transparent; border-radius: 0; color: var(--muted); padding: 8px 12px; font-weight: 500; }
    .tabs button[aria-selected=true] { color: var(--fg); border-bottom-color: var(--accent); }
    @media (prefers-reduced-motion: no-preference) { .meter span { transition: width var(--dur) var(--ease-out); } }
  `];

  level: Level = "me";
  ref = "";
  report?: Report;
  error = "";
  tab: "quotas" | "ai" = "quotas";
  private loaded = "";

  protected willUpdate() {
    const key = `${this.level}:${this.ref}`;
    if (this.loaded === key) return;
    this.loaded = key;
    const url = this.level === "project" ? `/projects/${this.ref}/usage` : this.level === "org" ? `/orgs/${this.ref}/usage` : this.level === "admin" ? "/admin/usage" : "/me/usage";
    this.report = undefined;
    api<Report>("GET", url).then((r) => (this.report = r)).catch((e) => (this.error = e instanceof ApiError ? e.text : t("error.unknown")));
  }

  private meter(pct: number, label: string) {
    const w = Math.min(100, Math.max(0, pct));
    return html`<div class="meter" role="meter" aria-label=${label} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${Math.round(w)}><span class=${w >= 100 ? "full" : w >= 80 ? "warn" : ""} style="width:${w}%"></span></div>`;
  }

  /** One budget as a tile: bar plus percentage and (when visible) amount. */
  private budgetTile(b: Budget, hidden: boolean) {
    const name = t(b.key as any, { name: b.name ?? "" });
    const pct = new Intl.NumberFormat(document.documentElement.lang || "en", { maximumFractionDigits: 1 }).format(b.percent);
    let value;
    if (b.limitTokens) value = html`${num(b.usedTokens ?? 0)} <small>${t("usage.of_tokens", { limit: num(b.limitTokens) })}</small>`;
    else if (hidden) value = b.hasLimit ? html`${pct} %` : html`<small>${t("usage.no_limit")}</small>`;
    else value = html`${money(b.usedCents)} <small>${b.limitCents ? t("usage.of", { limit: money(b.limitCents) }) : t("usage.no_limit")}</small>`;
    return html`<div class="tile"><span class="k">${name}</span><span class="v">${value}</span>
      ${b.hasLimit ? html`${this.meter(b.percent, name)}${hidden && !b.limitTokens ? nothing : html`<span class="meta">${t("usage.percent_used", { p: pct })}</span>`}` : nothing}</div>`;
  }

  private table(title: string, ic: string, rows: Line[] | undefined, first: string, link?: (l: Line) => string, cost = false) {
    if (!rows) return nothing;
    const hidden = !!this.report?.pricesHidden;
    const amount = (r: Line) => (hidden ? r.inputTokens + r.outputTokens : r.chargedCents);
    const max = Math.max(1, ...rows.map(amount));
    return html`<h2>${icon(ic, 16)} ${title}</h2>
      <div class="tbl">${rows.length ? html`<table>
        <thead><tr><th>${first}</th><th class="n">${t("usage.calls")}</th><th class="n">${t("usage.tokens")}</th>${hidden ? html`<th class="n">${t("usage.share")}</th>` : html`<th class="n">${t("usage.charged")}</th>`}${cost ? html`<th class="n">${t("usage.cost")}</th>` : nothing}</tr></thead>
        <tbody>${rows.map((r) => html`<tr>
          <td>${link && r.slug ? html`<a href=${link(r)}>${r.name}</a>` : r.name || html`<span class="muted">${r.id ? r.id : t("usage.personal")}</span>`}</td>
          <td class="n">${num(r.calls)}</td><td class="n">${num(r.inputTokens + r.outputTokens)}</td>
          <td class="n">${hidden ? "" : money(r.chargedCents)}<span class="share" aria-hidden="true"><span style="width:${(amount(r) / max) * 100}%"></span></span></td>
          ${cost ? html`<td class="n">${money(r.costCents ?? 0)}</td>` : nothing}</tr>`)}</tbody></table>` : html`<p class="empty">${t("usage.none")}</p>`}</div>`;
  }

  private storageTiles(r: Report) {
    if (r.level === "me") return nothing;
    const one = r.storage?.length === 1 ? r.storage[0] : undefined;
    const pct = one?.limitBytes ? (one.usedBytes / one.limitBytes) * 100 : 0;
    return html`<div class="tile"><span class="k">${t("usage.storage")}</span><span class="v">${bytes(r.storageBytes)}${one?.limitBytes ? html` <small>${t("usage.of", { limit: bytes(one.limitBytes) })}</small>` : nothing}</span>
      ${one?.limitBytes ? html`${this.meter(pct, t("usage.storage"))}<span class="meta">${t("usage.percent_used", { p: Math.round(pct) })}</span>` : nothing}</div>`;
  }

  private aiTiles(r: Report) {
    const admin = r.level === "admin";
    return html`${r.pricesHidden ? nothing : html`<div class="tile"><span class="k">${t("usage.ai_month")}</span><span class="v">${money(r.chargedCents)}${admin ? html` <small>${t("usage.cost_is", { cost: money(r.costCents ?? 0) })}</small>` : nothing}</span></div>`}
      <div class="tile"><span class="k">${t("usage.tokens_month")}</span><span class="v">${num(r.tokens)}</span></div>
      <div class="tile"><span class="k">${t("usage.calls_month")}</span><span class="v">${num(r.calls)}</span></div>`;
  }

  private tables(r: Report) {
    const admin = r.level === "admin";
    return html`${this.table(t("usage.by_org"), "Building2", r.byOrg, t("usage.col.org"), (l) => `/admin/orgs/${l.slug}/usage`, admin)}
      ${this.table(t("usage.by_project"), "FolderOpen", r.byProject, t("usage.col.project"), (l) => `/p/${l.slug}/settings/usage`)}
      ${this.table(admin ? t("usage.by_user_top") : t("usage.by_user"), "Users", r.byUser, t("usage.col.user"), undefined, admin)}
      ${this.table(t("usage.by_model"), "Bot", r.byModel, t("usage.col.model"), undefined, admin)}
      ${r.storage && r.storage.length > 1 ? html`<h2>${icon("HardDrive", 16)} ${t(admin ? "usage.storage_by_org" : "usage.storage_by_project")}</h2>
        <div class="tbl"><table><thead><tr><th>${t(admin ? "usage.col.org" : "usage.col.project")}</th><th class="n">${t("usage.used")}</th><th class="n">${t("usage.limit")}</th></tr></thead>
        <tbody>${r.storage.map((s) => html`<tr><td>${s.name || html`<span class="muted">${t("usage.personal")}</span>`}</td><td class="n">${bytes(s.usedBytes)}</td>
          <td class="n">${s.limitBytes ? bytes(s.limitBytes) : "–"}</td></tr>`)}</tbody></table></div>` : nothing}`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const r = this.report;
    const title = t(`usage.title.${this.level}` as any);
    if (!r) return html`${pageHead(title, "")}<p class="muted">${t("app.loading")}</p>`;
    const month = new Date(r.since).toLocaleDateString(document.documentElement.lang || "en", { month: "long", year: "numeric" });
    const head = html`${pageHead(title, t(`usage.lead.${r.level}` as any, { month, name: r.name }))}
      ${r.up.length ? html`<div class="up">${r.up.map((u) => html`<a class="btn ghost" href=${u.href}>${icon(u.level === "org" ? "Building2" : "ShieldCheck", 14)} ${u.level === "org" ? t("usage.up.org", { name: u.name ?? "" }) : t("usage.up.admin")} ${icon("ArrowRight", 14)}</a>`)}</div>` : nothing}
      ${r.pricesHidden ? html`<p class="muted" style="margin:-6px 0 14px">${icon("EyeOff", 14)} ${t("usage.prices_hidden")}</p>` : nothing}`;
    const note = html`<p class="muted" style="font-size:13px;margin-top:18px">${t(r.pricesHidden ? "usage.note_hidden" : "usage.note")}</p>`;
    if (r.level === "project") {
      const tab = (id: "quotas" | "ai", label: string) => html`<button role="tab" aria-selected=${this.tab === id} @click=${() => (this.tab = id)}>${label}</button>`;
      return html`${head}
        <div class="tabs" role="tablist">${tab("quotas", t("usage.tab.quotas"))}${tab("ai", t("usage.tab.ai"))}</div>
        ${this.tab === "quotas" ? html`<div class="grid">${this.storageTiles(r)}${r.budgets.map((b) => this.budgetTile(b, r.pricesHidden))}</div>
            <p class="muted" style="font-size:13px">${t("usage.quotas_more")}</p>`
          : html`<div class="grid">${this.aiTiles(r)}</div>${this.tables(r)}`}
        ${note}`;
    }
    return html`${head}
      <div class="grid">${this.aiTiles(r)}${r.budgets.map((b) => this.budgetTile(b, r.pricesHidden))}${this.storageTiles(r)}</div>
      ${this.tables(r)}${note}`;
  }
}
customElements.get("phx-usage") || customElements.define("phx-usage", PhxUsage);
