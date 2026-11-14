import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";

interface AuditEntry { id: number; actor: string; kind: string; subject: string; projectId?: string; payload: unknown; createdAt: string; who: string; via: string; detail?: string }

const VIA_ICON: Record<string, string> = { web: "UserRound", mcp: "Bot", token: "KeyRound", plugin: "Puzzle", agent: "Server", cli: "SquareCode", oauth: "Link" };

/** Audit log of the instance (admins) or of one project (owners). */
export class PhxAuditLog extends LitElement {
  static properties = { project: {}, page: { type: Boolean }, events: { state: true }, more: { state: true }, error: { state: true }, open: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; }
    :host([page]) { max-width: 1000px; margin: 0 auto; padding: 28px 16px; }
    form { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; margin: 8px 0 12px; }
    form .field { min-width: 140px; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    td, th { padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); text-align: left; vertical-align: top; }
    th { color: var(--muted); font-weight: 600; font-size: 12px; }
    td.when { white-space: nowrap; color: var(--muted); font-variant-numeric: tabular-nums; }
    .via { display: inline-flex; gap: 4px; align-items: center; }
    code { font-size: 12px; }
    pre { margin: 4px 0 0; font-size: 12px; white-space: pre-wrap; word-break: break-all; background: var(--surface2); padding: 6px 8px; border-radius: 6px; }
    button.small { padding: 2px 8px; font-size: 12px; }
  `];
  project = "";
  page = false;
  events: AuditEntry[] = [];
  more = false;
  error = "";
  open = 0;
  private filter: Record<string, string> = {};

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private url(before?: number) {
    const q = new URLSearchParams({ ...this.filter, limit: "50" });
    if (before) q.set("before", String(before));
    return `${this.project ? `/projects/${this.project}` : "/admin"}/events?${q}`;
  }

  private async load(append = false) {
    try {
      const list = (await api<AuditEntry[]>("GET", this.url(append ? this.events.at(-1)?.id : undefined))) ?? [];
      this.events = append ? [...this.events, ...list] : list;
      this.more = list.length === 50;
      this.error = "";
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private apply(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.filter = Object.fromEntries([...f.entries()].map(([k, v]) => [k, String(v).trim()]).filter(([, v]) => v));
    void this.load();
  }

  private body() {
    return html`
      <form @submit=${this.apply} @change=${(e: Event) => (e.currentTarget as HTMLFormElement).requestSubmit()}>
        <label class="field">${t("audit.via")}<select name="via">
          <option value="">${t("audit.any")}</option>${Object.keys(VIA_ICON).map((v) => html`<option value=${v}>${t(`audit.via.${v}` as any)}</option>`)}</select></label>
        <label class="field">${t("audit.kind")}<input name="kind" placeholder="document." list="audit-kinds">
          <datalist id="audit-kinds">${["document.", "entity.", "edge.", "task.", "project.", "agent.", "plugin.", "auth.", "suggestion."].map((k) => html`<option value=${k}></option>`)}</datalist></label>
        <label class="field">${t("audit.period")}<select name="days"><option value="">${t("audit.any")}</option>
          ${[1, 7, 30, 90].map((d) => html`<option value=${d}>${t("audit.days", { days: d })}</option>`)}</select></label>
        <button type="submit" class="ghost">${icon("Search", 14)} ${t("audit.filter")}</button>
      </form>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      ${this.events.length ? html`<table>
        <thead><tr><th>${t("audit.when")}</th><th>${t("audit.who")}</th><th>${t("audit.what")}</th><th></th></tr></thead>
        <tbody>${this.events.map((e) => html`<tr>
          <td class="when">${formatDate(e.createdAt)}</td>
          <td><span class="via" title=${t(`audit.via.${e.via}` as any)}>${icon(VIA_ICON[e.via] ?? "Circle", 12)} ${e.who}</span>${e.detail ? html` <code>${e.detail}</code>` : ""}</td>
          <td><code>${e.kind}</code></td>
          <td>${e.payload && JSON.stringify(e.payload) !== "null" && JSON.stringify(e.payload) !== "{}" ? html`<button class="ghost small" @click=${() => (this.open = this.open === e.id ? 0 : e.id)}>${t("audit.details")}</button>` : ""}</td></tr>
          ${this.open === e.id ? html`<tr><td colspan="4"><pre>${JSON.stringify(e.payload, null, 2)}</pre><span class="muted">${t("audit.subject")}: <code>${e.subject}</code></span></td></tr>` : ""}`)}</tbody></table>`
        : html`<p class="muted">${t("audit.empty")}</p>`}
      ${this.more ? html`<p><button class="ghost" @click=${() => this.load(true)}>${t("audit.more")}</button></p>` : ""}`;
  }

  render() {
    if (!this.page) return this.body();
    return html`<div class="enter"><h1>${t("audit.title")}</h1>${settingsNav("audit")}<p class="muted">${t("audit.intro")}</p>${this.body()}</div>`;
  }
}
customElements.define("phx-audit-log", PhxAuditLog);
