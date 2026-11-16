import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { dollars } from "../ai-chat";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Member { userId: string; name: string; email: string; ownCents: number | null; ownPercent?: number; limitCents: number; usedCents: number; percent: number; extraCents?: number; pending?: boolean }
interface Request { id: string; userId: string; userName?: string; percent: number; scope: "month" | "permanent"; reason: string; status: string; grantedCents?: number; note?: string; createdAt: string; decidedAt?: string }
interface Budgets { pricesHidden: boolean; defaultCents: number; requests: boolean; members: Member[]; pending: Request[]; recent: Request[] }

const PERCENTS = [25, 50, 100, 200];

/** AI limits per member and requests for more usage (organization admins). */
export class PhxMemberBudgets extends LitElement {
  static properties = { org: {}, data: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .tbl { overflow-x: auto; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th { text-align: left; font-size: 12px; font-weight: 500; color: var(--muted); padding: 8px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; }
    td { padding: 6px 10px; border-bottom: 1px solid var(--surface2); vertical-align: middle; }
    tr:last-child td { border-bottom: 0; }
    td.n { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
    .lim { display: flex; gap: 6px; align-items: center; }
    .lim input { max-width: 120px; }
    .meter { height: 6px; width: 80px; border-radius: 3px; background: var(--surface2); overflow: hidden; display: inline-block; vertical-align: middle; margin-left: 6px; }
    .meter span { display: block; height: 100%; background: var(--accent); }
    .meter span.warn { background: var(--signal); }
    .meter span.full { background: var(--bad); }
    .req { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 10px 12px; display: grid; gap: 8px; margin-bottom: 8px; }
    .req .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .req select { width: auto; }
    .muted { color: var(--muted); }
    .tag { font-size: 12px; border-radius: 99px; padding: 1px 8px; background: var(--accent-soft); color: var(--accent); }
    h4 { margin: 16px 0 6px; font-size: 14px; }
  `];

  org = "";
  data?: Budgets;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.data = await api<Budgets>("GET", `/orgs/${this.org}/ai/members`).catch(() => undefined);
  }

  private async setLimit(m: Member, raw: string) {
    const v = raw.trim().replace(",", ".");
    const hidden = this.data?.pricesHidden;
    const body = v === "" ? { cents: -1 } : hidden ? { percent: Number(v) } : { cents: Math.round(Number(v) * 100) };
    if (v !== "" && !Number.isFinite(Number(v))) return;
    try {
      this.data = await api<Budgets>("PUT", `/orgs/${this.org}/ai/members/${m.userId}`, body);
      toast(t("settings.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async decide(r: Request, approve: boolean, el: HTMLElement) {
    const q = (n: string) => el.querySelector<HTMLSelectElement | HTMLInputElement>(`[name=${n}]`)?.value ?? "";
    try {
      this.data = await api<Budgets>("POST", `/orgs/${this.org}/ai/requests/${r.id}`, { approve, percent: Number(q("percent")) || undefined, scope: q("scope") || undefined, note: q("note") });
      toast(approve ? t("budget_req.approved") : t("budget_req.rejected"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const d = this.data;
    if (!d) return nothing;
    const loc = locale();
    const pct = (p: number) => new Intl.NumberFormat(loc, { maximumFractionDigits: 1 }).format(p);
    const date = (s: string) => new Date(s).toLocaleDateString(loc, { month: "short", day: "numeric" });
    const meter = (p: number) => html`<span class="meter" aria-hidden="true"><span class=${p >= 100 ? "full" : p >= 80 ? "warn" : ""} style="width:${Math.min(100, p)}%"></span></span>`;
    return html`
      ${d.pending.length ? html`<h4>${icon("Inbox", 14)} ${t("budget_req.pending_title", { n: d.pending.length })}</h4>
        ${d.pending.map((r) => html`<div class="req">
          <div><strong>${r.userName || r.userId}</strong> · ${t("budget_req.asks", { p: r.percent })} · ${t(`budget_req.scope.${r.scope}`)} <span class="muted">· ${date(r.createdAt)}</span></div>
          ${r.reason ? html`<div class="muted">“${r.reason}”</div>` : nothing}
          <div class="row">
            <select name="percent" aria-label=${t("budget_req.percent")}>${PERCENTS.map((p) => html`<option value=${p} ?selected=${p === r.percent}>+${p} %</option>`)}</select>
            <select name="scope" aria-label=${t("budget_req.scope")}>${(["month", "permanent"] as const).map((s) => html`<option value=${s} ?selected=${s === r.scope}>${t(`budget_req.scope.${s}`)}</option>`)}</select>
            <input name="note" placeholder=${t("budget_req.note")} style="flex:1;min-width:160px">
            <button @click=${(e: Event) => void this.decide(r, true, (e.target as HTMLElement).closest(".req")!)}>${t("budget_req.approve")}</button>
            <button class="ghost" @click=${(e: Event) => void this.decide(r, false, (e.target as HTMLElement).closest(".req")!)}>${t("budget_req.reject")}</button>
          </div></div>`)}` : nothing}
      <h4>${t("member_budgets.title")}</h4>
      <p class="muted" style="font-size:13px;margin:0 0 8px">${d.pricesHidden ? t("member_budgets.hint_percent") : t("member_budgets.hint", { def: d.defaultCents ? dollars(d.defaultCents, loc) : "∞" })}</p>
      <div class="tbl"><table>
        <thead><tr><th>${t("usage.col.user")}</th><th class="n">${t("member_budgets.used")}</th><th>${d.pricesHidden ? t("member_budgets.own_percent") : t("member_budgets.own")}</th></tr></thead>
        <tbody>${d.members.map((m) => html`<tr>
          <td>${m.name || m.email}${m.pending ? html` <span class="tag">${t("budget_req.pending")}</span>` : nothing}</td>
          <td class="n">${d.pricesHidden ? `${pct(m.percent)} %` : html`${dollars(m.usedCents, loc)}${m.limitCents ? html` / ${dollars(m.limitCents, loc)}` : nothing}`}${m.limitCents || d.pricesHidden ? meter(m.percent) : nothing}
            ${m.extraCents ? html`<div class="muted" style="font-size:12px">${t("member_budgets.extra", { x: dollars(m.extraCents, loc) })}</div>` : nothing}</td>
          <td><div class="lim"><input type="number" min="0" step=${d.pricesHidden ? "10" : "0.01"} aria-label=${t("member_budgets.own")}
              .value=${d.pricesHidden ? (m.ownPercent != null ? String(m.ownPercent) : "") : (m.ownCents != null ? (m.ownCents / 100).toFixed(2) : "")}
              placeholder=${t("member_budgets.default")} @change=${(e: Event) => void this.setLimit(m, (e.target as HTMLInputElement).value)}>${d.pricesHidden ? "%" : ""}</div></td>
        </tr>`)}</tbody></table></div>
      ${d.recent.length ? html`<h4>${t("budget_req.recent")}</h4>
        <div class="tbl"><table><tbody>${d.recent.map((r) => html`<tr><td>${r.userName || r.userId}</td><td>+${r.percent} % · ${t(`budget_req.scope.${r.scope}`)}</td>
          <td>${t(`budget_req.status.${r.status}` as any)}${r.note ? html` <span class="muted">– ${r.note}</span>` : nothing}</td><td class="n muted">${r.decidedAt ? date(r.decidedAt) : ""}</td></tr>`)}</tbody></table></div>` : nothing}`;
  }
}
customElements.get("phx-member-budgets") || customElements.define("phx-member-budgets", PhxMemberBudgets);

/** Dialog to ask an organization for more AI usage. */
export class PhxBudgetRequest extends LitElement {
  static properties = { org: {}, orgName: {}, open: { type: Boolean, reflect: true }, busy: { state: true } };
  static styles = [base, css`
    dialog { border: 1px solid var(--line); border-radius: 14px; background: var(--surface); color: var(--fg); padding: 18px; width: min(440px, calc(100vw - 32px)); }
    dialog::backdrop { background: rgb(0 0 0 / 0.35); }
    form { display: grid; gap: 10px; }
    h3 { margin: 0; font-size: 17px; }
    .actions { display: flex; gap: 8px; justify-content: flex-end; }
    textarea { min-height: 80px; }
  `];
  org = "";
  orgName = "";
  open = false;
  busy = false;

  show() {
    this.open = true;
    void this.updateComplete.then(() => this.renderRoot.querySelector("dialog")?.showModal());
  }

  private close() {
    this.renderRoot.querySelector("dialog")?.close();
    this.open = false;
  }

  private async send(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.busy = true;
    try {
      await api("POST", `/orgs/${this.org}/ai/requests`, { percent: Number(f.get("percent")), scope: String(f.get("scope")), reason: String(f.get("reason") ?? "") });
      toast(t("budget_req.sent"));
      this.close();
      this.dispatchEvent(new CustomEvent("sent", { bubbles: true, composed: true }));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    if (!this.open) return nothing;
    return html`<dialog @close=${() => (this.open = false)} aria-labelledby="brt">
      <form @submit=${this.send}>
        <h3 id="brt">${t("budget_req.title", { org: this.orgName })}</h3>
        <p class="muted" style="margin:0;font-size:13.5px">${t("budget_req.intro")}</p>
        <label class="field">${t("budget_req.percent")}<select name="percent">${PERCENTS.map((p) => html`<option value=${p} ?selected=${p === 50}>+${p} %</option>`)}</select></label>
        <label class="field">${t("budget_req.scope")}<select name="scope"><option value="month">${t("budget_req.scope.month")}</option><option value="permanent">${t("budget_req.scope.permanent")}</option></select></label>
        <label class="field">${t("budget_req.reason")}<textarea name="reason" maxlength="1000" placeholder=${t("budget_req.reason_ph")}></textarea></label>
        <div class="actions"><button type="button" class="ghost" @click=${this.close}>${t("doc.cancel")}</button><button ?disabled=${this.busy}>${t("budget_req.send")}</button></div>
      </form></dialog>`;
  }
}
customElements.get("phx-budget-request") || customElements.define("phx-budget-request", PhxBudgetRequest);
