import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { signPayload } from "../signing";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";
import "./web-terminal";

interface Action { action: string; args?: string[]; agent?: string; hostname?: string }
interface Job { id: string; action: string; payload: string; status: string; ok: boolean; output?: string; createdAt: string; finishedAt: string; createdBy?: string }
interface Actions { agentId?: string; hostname?: string; actions: Action[]; jobs: Job[] }

const ICONS: Record<string, string> = { "container.restart": "RefreshCw", "container.update": "Download", "stack.redeploy": "Rocket", "system.update": "Download", "agent.update": "CircleArrowUp", wol: "Power", speedtest: "Gauge", terminal: "SquareTerminal" };

/** Agent actions of an entry: signed in this browser, run by the agent (ADR-0036). */
export class PhxAgentActions extends LitElement {
  static properties = { project: {}, entity: {}, a: { state: true }, busy: { state: true }, open: { state: true }, terminal: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-bottom: 18px; }
    :host([hidden]) { display: none; }
    .box { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 12px 14px; display: grid; gap: 10px; }
    .head { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .head h2 { font-size: 16px; margin: 0; margin-right: auto; }
    .acts { display: flex; gap: 8px; flex-wrap: wrap; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    td { padding: 4px 8px 4px 0; border-bottom: 1px solid var(--line); vertical-align: top; }
    .ok { color: var(--good); } .bad { color: var(--bad); } .wait { color: var(--muted); }
    pre { margin: 4px 0 0; max-height: 200px; overflow: auto; font-size: 12px; background: var(--surface2); border-radius: 6px; padding: 6px 8px; white-space: pre-wrap; }
    .hint { font-size: 13px; color: var(--muted); margin: 0; }
  `];
  project = "";
  entity = "";
  a?: Actions;
  busy = "";
  open = "";
  /** The terminal job whose session is shown. */
  terminal = "";
  private loaded = "";
  private timer?: number;

  willUpdate() {
    const key = `${this.project}/${this.entity}`;
    if (key !== this.loaded) {
      this.loaded = key;
      this.hidden = true;
      void this.load();
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.clearTimeout(this.timer);
  }

  private async load() {
    this.a = await api<Actions>("GET", `/projects/${this.project}/entities/${this.entity}/agent-actions`).catch(() => undefined);
    this.hidden = !this.a?.actions.length;
    // Follow running jobs until they finish.
    window.clearTimeout(this.timer);
    if (this.a?.jobs.some((j) => j.status === "approved" || j.status === "sent")) this.timer = window.setTimeout(() => void this.load(), 3000);
  }

  private async run(act: Action) {
    const host = act.hostname ?? this.a?.hostname ?? "";
    if (!confirm(t(act.action === "terminal" ? "agentjob.confirm_terminal" : "agentjob.confirm", { action: t(`agentjob.${act.action}` as any), host }))) return;
    this.busy = act.action;
    try {
      const job = await api<Job>("POST", `/projects/${this.project}/entities/${this.entity}/agent-jobs`, { action: act.action });
      const signed = await signPayload(job.payload);
      if (!signed) {
        toast(t("agentjob.no_key"), "error");
        return;
      }
      await api("POST", `/projects/${this.project}/agent-jobs/${job.id}/sign`, signed);
      toast(t("agentjob.sent"));
      if (act.action === "terminal") this.terminal = job.id;
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = "";
    }
  }

  private status(j: Job) {
    if (j.status === "done") return html`<span class="ok">${icon("CircleCheck", 13)} ${t("agentjob.done")}</span>`;
    if (j.status === "failed") return html`<span class="bad">${icon("CircleAlert", 13)} ${t("agentjob.failed")}</span>`;
    if (j.status === "unsigned") return html`<span class="wait">${t("agentjob.unsigned")}</span>`;
    return html`<span class="wait">${t("agentjob.running")}</span>`;
  }

  render() {
    const a = this.a;
    if (!a?.actions.length) return html``;
    const role = currentProject.get()?.role ?? "";
    const canRun = ["editor", "owner"].includes(role);
    const offered = a.actions.filter((x) => x.action !== "terminal" || role === "owner");
    return html`<div class="box">
      <div class="head"><h2>${t("agentjob.title")}</h2><span class="hint">${icon("Server", 12)} ${a.hostname ?? a.actions[0]?.hostname ?? ""}</span></div>
      ${this.terminal ? html`<phx-web-terminal .project=${this.project} .job=${this.terminal} .host=${a.hostname ?? ""} @closed=${() => void this.load()}></phx-web-terminal>
        <button class="ghost" @click=${() => (this.terminal = "")}>${t("term.hide")}</button>` : ""}
      ${canRun ? html`<div class="acts">${offered.map((x) => html`<button class="ghost" ?disabled=${!!this.busy} @click=${() => this.run(x)}>
        ${icon(ICONS[x.action] ?? "Play", 14)} ${t(`agentjob.${x.action}` as any)}${x.action === "agent.update" ? ` (${x.args?.[0]?.split("/")[5] ?? ""})` : ""}${x.action === "wol" && x.hostname ? ` (${t("agentjob.via", { host: x.hostname })})` : ""}</button>`)}</div>
        <p class="hint">${t("agentjob.hint")} <a href="/settings/security">${t("agentjob.keys")}</a></p>` : ""}
      ${a.jobs.length ? html`<table>${a.jobs.map((j) => html`<tr>
        <td>${t(`agentjob.${j.action}` as any)}</td><td>${formatDate(j.createdAt)}</td><td>${this.status(j)}</td>
        <td>${j.output ? html`<button class="ghost" style="padding:2px 8px;font-size:12px" @click=${() => (this.open = this.open === j.id ? "" : j.id)}>${t("agentjob.output")}</button>` : ""}</td></tr>
        ${this.open === j.id ? html`<tr><td colspan="4"><pre>${j.output}</pre></td></tr>` : ""}`)}</table>` : ""}
    </div>`;
  }
}
customElements.define("phx-agent-actions", PhxAgentActions);
