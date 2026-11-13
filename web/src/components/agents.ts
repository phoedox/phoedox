import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Agent { id: string; hostname: string; entitySlug: string; version: string; lastSeen: string; revokedAt: string; createdAt: string }
interface Enrollment { code: string; expiresAt: string; uses: number }

const zero = (s: string) => !s || s.startsWith("0001-");

/** Agents of a project (ADR-0033): enrollment code, list, revoke. */
export class PhxAgents extends LitElement {
  static properties = { project: {}, agents: { state: true }, enrollment: { state: true }, copied: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    table { width: 100%; max-width: 760px; border-collapse: collapse; font-size: 14px; margin: 8px 0; }
    td { padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); }
    .off { color: var(--muted); text-decoration: line-through; }
    .code { display: grid; gap: 8px; padding: 12px 14px; border: 1px solid var(--accent); border-radius: 10px; max-width: 760px; margin: 8px 0; }
    pre { margin: 0; white-space: pre-wrap; word-break: break-all; font-size: 13px; }
    .hint { font-size: 13px; color: var(--muted); margin: 0; }
    button.small { padding: 3px 9px; font-size: 13px; }
  `];
  project = "";
  agents?: Agent[];
  enrollment?: Enrollment;
  copied = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.agents = (await api<Agent[]>("GET", `/projects/${this.project}/agents`).catch(() => [])) ?? [];
  }

  private async call<T>(fn: () => Promise<T>) {
    try {
      return await fn();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return undefined;
    }
  }

  private command() {
    return `phoedox-agent enroll ${location.origin} ${this.enrollment?.code ?? ""}\nphoedox-agent run`;
  }

  private async create() {
    this.enrollment = await this.call(() => api<Enrollment>("POST", `/projects/${this.project}/agents/enrollments`));
    this.copied = false;
  }

  private async copy() {
    await navigator.clipboard.writeText(this.command()).catch(() => undefined);
    this.copied = true;
  }

  private async revoke(a: Agent) {
    if (!confirm(t("agents.revoke_confirm", { host: a.hostname }))) return;
    await this.call(() => api("DELETE", `/projects/${this.project}/agents/${a.id}`));
    await this.load();
  }

  render() {
    const list = this.agents ?? [];
    return html`<p class="hint">${t("agents.intro")}</p>
      ${list.length ? html`<table>${list.map((a) => html`<tr>
        <td>${icon("Server", 14)} ${zero(a.entitySlug) ? a.hostname : html`<a href="/p/${this.project}/e/${a.entitySlug}">${a.hostname}</a>`}</td>
        <td class="muted">${zero(a.lastSeen) ? t("agents.never") : t("agents.seen", { date: formatDate(a.lastSeen) })}</td>
        <td class="muted">${a.version}</td>
        <td style="text-align:right">${zero(a.revokedAt) ? html`<button class="ghost small" @click=${() => this.revoke(a)}>${t("agents.revoke")}</button>`
          : html`<span class="off">${t("agents.revoked")}</span>`}</td></tr>`)}</table>` : this.agents ? html`<p class="muted">${t("agents.empty")}</p>` : ""}
      ${this.enrollment ? html`<div class="code">
        <strong>${t("agents.code_title")}</strong>
        <pre><code>${this.command()}</code></pre>
        <p class="hint">${t("agents.code_hint", { date: formatDate(this.enrollment.expiresAt), uses: this.enrollment.uses })}</p>
        <div><button class="ghost small" @click=${this.copy}>${icon(this.copied ? "Check" : "Copy", 13)} ${this.copied ? t("tokens.copied") : t("tokens.copy")}</button></div>
      </div>` : html`<button class="ghost" @click=${this.create}>${icon("Plus", 14)} ${t("agents.add")}</button>`}`;
  }
}
customElements.define("phx-agents", PhxAgents);
