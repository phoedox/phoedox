import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { dollars } from "../ai-chat";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Price { input: number; output: number; cacheRead: number; cacheWrite: number }
interface OrgPrice { id: string; percent: number; models: Record<string, Price>; mode: string; effectiveFrom: string; createdAt: string }
interface Prices { current: OrgPrice | null; history: OrgPrice[] }
interface Preview { effectiveFrom: string; beforeCents: number; afterCents: number; budgets: { key: string; limitCents: number; beforeCents: number; afterCents: number; exceeded: boolean }[] }
type Mode = "date" | "now" | "next_month" | "month_start";
interface Row { model: string; input: string; output: string }

const today = () => new Date().toISOString().slice(0, 10);

/** An organization's own token prices (system admins, usage-prices-concept):
 * a percentage on all in-app prices plus own prices per model, from a date,
 * now, the next month or back to the month start – with a preview of what
 * the change does to this month and the price history. */
export class PhxOrgPrices extends LitElement {
  static properties = { org: {}, prices: { state: true }, preview: { state: true }, mode: { state: true }, rows: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    form { display: grid; gap: 10px; max-width: 620px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 10px; }
    .models { display: grid; gap: 6px; }
    .models .r { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) auto; gap: 6px; align-items: end; }
    .pv { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; background: var(--surface2); font-size: 14px; display: grid; gap: 4px; }
    .pv .bad { color: var(--bad); }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; }
    table { border-collapse: collapse; width: 100%; font-size: 13.5px; margin-top: 10px; }
    th { text-align: left; font-weight: 500; color: var(--muted); font-size: 12px; padding: 6px 8px; border-bottom: 1px solid var(--line); }
    td { padding: 6px 8px; border-bottom: 1px solid var(--surface2); vertical-align: top; }
    .tag { font-size: 12px; border-radius: 99px; padding: 1px 8px; background: var(--accent-soft); color: var(--accent); white-space: nowrap; }
    .tbl { overflow-x: auto; }
  `];

  org = "";
  prices?: Prices;
  preview?: Preview;
  mode: Mode = "date";
  rows: Row[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.prices = await api<Prices>("GET", `/admin/orgs/${this.org}/prices`).catch(() => undefined);
  }

  private input(): Record<string, unknown> | undefined {
    const f = new FormData(this.renderRoot.querySelector("form")!);
    const percent = Number(String(f.get("percent") || "0").replace(",", "."));
    const models: Record<string, Price> = {};
    for (const r of this.rows) {
      const m = r.model.trim();
      if (!m) continue;
      models[m] = { input: Number(r.input.replace(",", ".")) || 0, output: Number(r.output.replace(",", ".")) || 0, cacheRead: 0, cacheWrite: 0 };
    }
    return { percent: Number.isFinite(percent) ? percent : 0, models, mode: this.mode, from: String(f.get("from") || "") };
  }

  private async run(path: string) {
    const body = this.input();
    this.busy = true;
    try {
      if (path === "preview") {
        this.preview = await api<Preview>("POST", `/admin/orgs/${this.org}/prices/preview`, body);
      } else {
        this.prices = await api<Prices>("POST", `/admin/orgs/${this.org}/prices`, body);
        this.preview = undefined;
        toast(t("org_prices.saved"));
      }
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removePrice(p: OrgPrice) {
    try {
      this.prices = await api<Prices>("DELETE", `/admin/orgs/${this.org}/prices/${p.id}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private describe(p: OrgPrice) {
    const parts: string[] = [];
    if (p.percent) parts.push(t("org_prices.percent_value", { p: (p.percent > 0 ? "+" : "") + p.percent }));
    for (const [m, v] of Object.entries(p.models ?? {})) parts.push(t("org_prices.model_value", { model: m, in: v.input, out: v.output }));
    return parts.length ? parts.join(" · ") : t("org_prices.list_price");
  }

  private setRow(i: number, k: keyof Row, v: string) {
    this.rows = this.rows.map((r, j) => (j === i ? { ...r, [k]: v } : r));
    this.preview = undefined;
  }

  render() {
    const loc = locale();
    const date = (s: string) => new Date(s).toLocaleDateString(loc, { year: "numeric", month: "short", day: "numeric" });
    const now = Date.now();
    const pv = this.preview;
    return html`
      ${this.prices?.current ? html`<p>${icon("Coins", 14)} ${t("org_prices.current", { what: this.describe(this.prices.current), from: date(this.prices.current.effectiveFrom) })}</p>` : html`<p class="muted">${t("org_prices.none")}</p>`}
      <form @submit=${(e: SubmitEvent) => { e.preventDefault(); void this.run("save"); }} @input=${() => (this.preview = undefined)}>
        <div class="grid">
          <label class="field">${t("org_prices.percent")}<input name="percent" type="number" step="0.1" min="-100" max="1000" placeholder="0"></label>
          <label class="field">${t("org_prices.mode")}<select @change=${(e: Event) => { this.mode = (e.target as HTMLSelectElement).value as Mode; this.preview = undefined; }}>
            ${(["date", "now", "next_month", "month_start"] as Mode[]).map((m) => html`<option value=${m} ?selected=${this.mode === m}>${t(`org_prices.mode.${m}`)}</option>`)}
          </select></label>
          ${this.mode === "date" ? html`<label class="field">${t("org_prices.from")}<input name="from" type="date" .value=${today()} required></label>` : nothing}
        </div>
        <p class="muted">${t(`org_prices.mode_hint.${this.mode}`)}</p>
        <div class="models">
          <strong style="font-size:13px">${t("org_prices.models")}</strong>
          ${this.rows.map((r, i) => html`<div class="r">
            <label class="field">${t("org_prices.model")}<input .value=${r.model} placeholder="claude-sonnet-5-5" @input=${(e: Event) => this.setRow(i, "model", (e.target as HTMLInputElement).value)}></label>
            <label class="field">${t("org_prices.input")}<input type="number" min="0" step="0.01" .value=${r.input} @input=${(e: Event) => this.setRow(i, "input", (e.target as HTMLInputElement).value)}></label>
            <label class="field">${t("org_prices.output")}<input type="number" min="0" step="0.01" .value=${r.output} @input=${(e: Event) => this.setRow(i, "output", (e.target as HTMLInputElement).value)}></label>
            <button type="button" class="ghost" title=${t("org_prices.remove_model")} aria-label=${t("org_prices.remove_model")} @click=${() => { this.rows = this.rows.filter((_, j) => j !== i); this.preview = undefined; }}>${icon("X", 14)}</button>
          </div>`)}
          <div><button type="button" class="ghost" @click=${() => (this.rows = [...this.rows, { model: "", input: "", output: "" }])}>${icon("Plus", 14)} ${t("org_prices.add_model")}</button></div>
        </div>
        ${pv ? html`<div class="pv" role="status">
          <span>${t("org_prices.preview_month", { before: dollars(pv.beforeCents, loc), after: dollars(pv.afterCents, loc), from: date(pv.effectiveFrom) })}</span>
          ${pv.budgets.map((b) => html`<span class=${b.exceeded ? "bad" : ""}>${t(b.key as any, { name: "" })}: ${dollars(b.beforeCents, loc)} → ${dollars(b.afterCents, loc)}${b.limitCents ? ` / ${dollars(b.limitCents, loc)}` : ""}${b.exceeded ? html` · ${icon("TriangleAlert", 13)} ${t("org_prices.exceeded")}` : nothing}</span>`)}
        </div>` : nothing}
        <div class="actions">
          <button type="button" class="ghost" ?disabled=${this.busy} @click=${() => void this.run("preview")}>${icon("Eye", 14)} ${t("org_prices.preview")}</button>
          <button ?disabled=${this.busy || (this.mode === "month_start" && !pv)}>${t("org_prices.save")}</button>
        </div>
        ${this.mode === "month_start" && !pv ? html`<p class="muted">${t("org_prices.preview_first")}</p>` : nothing}
      </form>
      ${this.prices?.history.length ? html`<h4 style="margin:18px 0 4px">${t("org_prices.history")}</h4><div class="tbl"><table>
        <thead><tr><th>${t("org_prices.col_from")}</th><th>${t("org_prices.col_prices")}</th><th>${t("org_prices.col_mode")}</th><th></th></tr></thead>
        <tbody>${this.prices.history.map((p) => {
          const planned = new Date(p.effectiveFrom).getTime() > now;
          const active = this.prices?.current?.id === p.id;
          return html`<tr><td>${date(p.effectiveFrom)} ${planned ? html`<span class="tag">${t("org_prices.planned")}</span>` : active ? html`<span class="tag">${t("org_prices.active")}</span>` : nothing}</td>
            <td>${this.describe(p)}</td><td>${t(`org_prices.mode.${p.mode}` as any)}</td>
            <td>${planned ? html`<button class="ghost" title=${t("org_prices.cancel")} aria-label=${t("org_prices.cancel")} @click=${() => void this.removePrice(p)}>${icon("Trash2", 14)}</button>` : nothing}</td></tr>`;
        })}</tbody></table></div>` : nothing}`;
  }
}
customElements.get("phx-org-prices") || customElements.define("phx-org-prices", PhxOrgPrices);
