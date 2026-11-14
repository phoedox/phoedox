import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { checkIntervals, checkState, pageChecks, type MonitorCheck } from "../checks";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { isSafeMethod, parseRequest, splitVars, type RequestBlock } from "../request";
import { currentProject } from "../state";
import { base } from "./shared";

interface Check { expr: string; ok: boolean; actual?: string; error?: string }
interface Result { status: number; timeMs: number; url: string; headers: [string, string][]; body: string; truncated?: boolean; checks: Check[]; error?: string }

/** A runnable request block inside a rendered page (```request). */
export class PhxRequestBlock extends SignalWatcher(LitElement) {
  static properties = { req: { state: true }, result: { state: true }, busy: { state: true }, confirming: { state: true }, error: { state: true },
    check: { state: true }, monitorForm: { state: true } };
  static styles = [base, css`
    :host { display: block; margin: 0 0 1em; }
    .box { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); overflow: hidden; }
    .line { display: flex; gap: 10px; align-items: center; padding: 10px 12px; flex-wrap: wrap; }
    .m { font: 700 12px var(--font-mono); padding: 3px 8px; border-radius: 6px; background: var(--accent-soft); color: var(--fg); }
    .url { font-family: var(--font-mono); font-size: 13px; flex: 1; min-width: 0; word-break: break-all; }
    .var { background: var(--surface2); border-radius: 4px; padding: 0 4px; color: var(--accent); }
    .line button { margin-left: auto; }
    details { border-top: 1px solid var(--line); padding: 6px 12px; font-size: 13px; }
    summary { cursor: pointer; color: var(--muted); }
    pre { font-family: var(--font-mono); font-size: 12.5px; background: var(--surface2); padding: 8px 10px; border-radius: 6px; overflow-x: auto; white-space: pre-wrap; word-break: break-word; margin: 6px 0; max-height: 360px; overflow-y: auto; }
    ul { list-style: none; padding: 0; margin: 6px 0; font-family: var(--font-mono); font-size: 12.5px; }
    li { display: flex; gap: 8px; align-items: baseline; padding: 2px 0; }
    .ok { color: var(--good); } .bad { color: var(--bad); }
    .confirm { border-top: 1px solid var(--line); padding: 8px 12px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; font-size: 13px; }
    /* Appears after a click: a short reveal points at the answer. */
    .result { border-top: 1px solid var(--line); padding: 8px 12px 10px;
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(-4px); } }
    @media (prefers-reduced-motion: reduce) { .result { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
    .status { font: 700 13px var(--font-mono); padding: 2px 8px; border-radius: 6px; }
    .status.good { background: color-mix(in srgb, var(--good) 15%, transparent); color: var(--good); }
    .status.bad { background: color-mix(in srgb, var(--bad) 12%, transparent); color: var(--bad); }
    .meta { display: flex; gap: 10px; align-items: center; font-size: 13px; color: var(--muted); }
    .monitor { border-top: 1px solid var(--line); padding: 7px 12px; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; font-size: 13px; }
    .monitor .txt { margin-right: auto; display: inline-flex; gap: 6px; align-items: center; min-width: 0; }
    .monitor button { padding: 3px 9px; font-size: 13px; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); flex: none; }
    .dot.ok { background: var(--good); } .dot.fail { background: var(--bad); } .dot.paused { background: var(--line); }
    form.monitor label { display: grid; gap: 2px; font-size: 12.5px; color: var(--muted); }
    form.monitor input, form.monitor select { font-size: 13px; padding: 4px 8px; }
    form.monitor .actions { display: flex; gap: 6px; margin-left: auto; align-self: end; }
  `];
  req: RequestBlock | null = null;
  result?: Result;
  busy = false;
  confirming = false;
  error = "";
  check?: MonitorCheck;
  monitorForm = false;

  connectedCallback() {
    super.connectedCallback();
    this.req = parseRequest(this.querySelector("pre")?.textContent ?? "");
    void this.loadCheck();
  }

  private get base() {
    return `/projects/${this.dataset.project}/checks`;
  }

  private async loadCheck(fresh = false) {
    if (!this.dataset.project || !this.dataset.doc) return;
    const list = await pageChecks(this.dataset.project, this.dataset.doc, fresh);
    this.check = list.find((c) => c.blockIndex === Number(this.dataset.index));
  }

  private async checkCall(fn: () => Promise<MonitorCheck | undefined>) {
    this.error = "";
    try {
      const c = await fn();
      // Siblings on the page read the shared list: refresh it.
      await pageChecks(this.dataset.project!, this.dataset.doc!, true);
      this.check = c;
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async createCheck(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    await this.checkCall(() => api<MonitorCheck>("POST", this.base, { document: this.dataset.doc, block: Number(this.dataset.index),
      name: String(f.get("name") ?? ""), intervalSec: Number(f.get("interval")), entity: String(f.get("entity") ?? "").trim() || undefined }));
    if (!this.error) this.monitorForm = false;
  }

  private monitorView(canEdit: boolean) {
    const c = this.check;
    if (this.monitorForm) {
      return html`<form class="monitor" @submit=${this.createCheck}>
        <label>${t("check.name")}<input name="name" maxlength="120" placeholder=${t("check.name_placeholder")} /></label>
        <label>${t("check.interval")}<select name="interval">${checkIntervals.map((s) => html`<option value=${s} ?selected=${s === 300}>${t("check.every", { n: intervalLabel(s) })}</option>`)}</select></label>
        <label>${t("check.entity")}<input name="entity" placeholder=${t("check.entity_placeholder")} /></label>
        <span class="actions"><button type="submit">${t("check.start")}</button><button type="button" class="ghost" @click=${() => (this.monitorForm = false)}>${t("doc.cancel")}</button></span>
      </form>`;
    }
    if (!c) return "";
    const state = checkState(c);
    return html`<div class="monitor" role="status">
      <span class="txt" title=${c.name}><span class="dot ${state}" aria-hidden="true"></span>${icon("Activity", 14)}
        <span>${t(`check.state.${state}` as any)} · ${t("check.every", { n: intervalLabel(c.intervalSec) })}${c.lastRun ? html` · ${t("check.last_run", { at: formatDate(c.lastRun) })}` : ""}</span>
        ${c.lastStatus === "fail" && c.lastMessage ? html`<span class="bad">${t(c.lastMessage as any, { detail: c.lastDetail })}</span>` : ""}
        ${c.entitySlug ? html`<a href="/p/${this.dataset.project}/e/${c.entitySlug}">${c.entityName}</a>` : ""}</span>
      ${canEdit ? html`
        <button class="ghost" @click=${() => this.checkCall(() => api<MonitorCheck>("POST", `${this.base}/${c.id}/run`))}>${t("check.run_now")}</button>
        <button class="ghost" @click=${() => this.checkCall(() => api<MonitorCheck>("PATCH", `${this.base}/${c.id}`, { intervalSec: c.intervalSec, entity: c.entitySlug, enabled: !c.enabled }))}>${c.enabled ? t("check.pause") : t("check.resume")}</button>
        <button class="ghost" aria-label=${t("check.remove")} title=${t("check.remove")} @click=${() => this.checkCall(async () => { await api("DELETE", `${this.base}/${c.id}`); return undefined; })}>${icon("Trash2", 14)}</button>` : ""}
    </div>`;
  }

  private withVars(s: string) {
    return splitVars(s).map((p) => (p.variable ? html`<span class="var" title=${t("req.secret_var")}>{{${p.text}}}</span>` : p.text));
  }

  private async run() {
    this.confirming = false;
    this.busy = true;
    this.error = "";
    try {
      this.result = await api<Result>("POST", `/projects/${this.dataset.project}/documents/${this.dataset.doc}/requests/${this.dataset.index}/run`);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private resultView(r: Result) {
    const good = r.status >= 200 && r.status < 400 && !r.error && r.checks.every((c) => c.ok);
    return html`<div class="result" role="status">
      <div class="meta">
        ${r.status ? html`<span class="status ${good ? "good" : "bad"}">${r.status}</span>` : ""}
        <span>${t("req.time", { ms: r.timeMs })}</span>
      </div>
      ${r.error ? html`<p class="bad" style="margin:6px 0 0">${r.error}</p>` : ""}
      ${r.checks?.length ? html`<ul aria-label=${t("req.expect")}>${r.checks.map((c) => html`<li>
        <span class=${c.ok ? "ok" : "bad"}>${icon(c.ok ? "Check" : "X", 13)}</span><span>${c.expr}</span>${c.actual !== undefined && c.actual !== "" ? html`<span class="muted">→ ${c.actual}</span>` : ""}</li>`)}</ul>` : ""}
      ${r.status ? html`<details><summary>${t("req.headers")} (${r.headers?.length ?? 0})</summary><pre>${(r.headers ?? []).map(([k, v]) => `${k}: ${v}`).join("\n")}</pre></details>
        <details open><summary>${t("req.response")}</summary><pre>${r.body}</pre>${r.truncated ? html`<p class="muted">${t("req.truncated")}</p>` : ""}</details>` : ""}
    </div>`;
  }

  render() {
    const r = this.req;
    if (!r) return html`<div class="box"><div class="line"><span class="bad">${t("req.invalid")}</span></div><pre style="margin:0 12px 12px">${this.querySelector("pre")?.textContent}</pre></div>`;
    const safe = isSafeMethod(r.method);
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    const allowed = safe || canEdit;
    return html`<div class="box">
      <div class="line">
        <span class="m">${r.method}</span><span class="url">${this.withVars(r.url)}</span>
        ${allowed
          ? html`<button ?disabled=${this.busy} @click=${() => (safe ? this.run() : (this.confirming = true))}>${icon("Play", 14)} ${this.busy ? t("req.running") : t("req.run")}</button>`
          : html`<span class="muted" style="font-size:13px;margin-left:auto">${t("req.editors_only", { method: r.method })}</span>`}
        ${safe && canEdit && !this.check && !this.monitorForm && this.dataset.doc
          ? html`<button class="ghost" title=${t("check.hint")} @click=${() => (this.monitorForm = true)}>${icon("Activity", 14)} ${t("check.monitor")}</button>` : ""}
      </div>
      ${this.monitorView(canEdit)}
      ${this.confirming ? html`<div class="confirm"><span>${t("req.confirm", { method: r.method })}</span>
        <button class="danger" @click=${this.run}>${t("req.run")}</button><button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button></div>` : ""}
      ${r.headers.length ? html`<details><summary>${t("req.headers")} (${r.headers.length})</summary><ul>${r.headers.map(([k, v]) => html`<li><b>${k}:</b> <span>${this.withVars(v)}</span></li>`)}</ul></details>` : ""}
      ${r.body ? html`<details><summary>${t("req.body")}</summary><pre>${this.withVars(r.body)}</pre></details>` : ""}
      ${r.expects.length ? html`<details><summary>${t("req.expect")} (${r.expects.length})</summary><ul>${r.expects.map((e) => html`<li>${e}</li>`)}</ul></details>` : ""}
      ${this.error ? html`<div class="confirm bad" role="alert">${this.error}</div>` : ""}
      ${this.result ? this.resultView(this.result) : ""}
    </div>`;
  }
}
customElements.define("phx-request-block", PhxRequestBlock);

/** "5 min", "1 h", "24 h" for an interval in seconds. */
export function intervalLabel(sec: number): string {
  return sec < 3600 ? t("check.minutes", { n: Math.round(sec / 60) }) : t("check.hours", { n: Math.round(sec / 3600) });
}
