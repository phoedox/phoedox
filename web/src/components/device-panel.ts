import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { quickEnum, toolFields, type ToolField } from "../device-form";
import { formatDate, formatDay, hasKey, t, type Key } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

export interface DeviceTool { plugin: string; pluginName: string; tool: string; description: string; input?: unknown; arg: string; value: string }
interface Grant { id: string; userName: string; userEmail: string; expiresAt: string | null }
interface DeviceInfo { tools: DeviceTool[]; aiControl: "suggest" | "direct"; canControl: boolean; canManage: boolean; grants?: Grant[] }
interface LogEntry { id: number; kind: string; who: string; via: string; createdAt: string; payload: { tool?: string; args?: Record<string, unknown>; ok?: boolean; aiControl?: string } }

const optLabel = (o: string) => (hasKey(`device.opt.${o}`) ? t(`device.opt.${o}` as Key) : o);

/** Buttons and small forms for a device's control tools, built from each
 * tool's input schema. path is where calls go (…/device/control or
 * /me/devices/{grant}/control). */
export class PhxDeviceControls extends LitElement {
  static properties = { tools: { attribute: false }, path: {}, busy: { state: true } };
  static styles = [base, css`
    :host { display: grid; gap: 10px; }
    .tool { display: grid; gap: 6px; }
    .tool b { font-size: 13.5px; font-weight: 600; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; }
    .row label { display: grid; gap: 2px; font-size: 13px; }
    .row input[type=number] { width: 7em; }
  `];
  tools: DeviceTool[] = [];
  path = "";
  busy = false;

  private async run(tl: DeviceTool, args: Record<string, unknown>) {
    this.busy = true;
    try {
      await api("POST", this.path, { plugin: tl.plugin, tool: tl.tool, args });
      toast(t("device.done"), "ok");
      this.dispatchEvent(new CustomEvent("controlled", { bubbles: true, composed: true }));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private submit(e: Event, tl: DeviceTool, fields: ToolField[]) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const args: Record<string, unknown> = {};
    for (const x of fields) {
      const v = f.get(x.name);
      if (x.type === "bool") args[x.name] = v === "on";
      else if (v !== null && v !== "") args[x.name] = x.type === "number" ? Number(v) : String(v);
    }
    this.run(tl, args);
  }

  private tool(tl: DeviceTool) {
    const fields = toolFields(tl.input, tl.arg);
    const q = quickEnum(fields);
    const title = html`<b>${tl.description || tl.tool}</b>`;
    if (q) {
      return html`<div class="tool">${title}<div class="row">${q.options!.map((o) => html`
        <button class="ghost" ?disabled=${this.busy} @click=${() => this.run(tl, { [q.name]: o })}>${icon(o === "off" ? "Power" : o === "on" ? "Zap" : "Repeat", 14)} ${optLabel(o)}</button>`)}</div></div>`;
    }
    return html`<form class="tool" @submit=${(e: Event) => this.submit(e, tl, fields)}>${title}<div class="row">
      ${fields.map((x) => html`<label>${x.label}${x.type === "enum"
        ? html`<select name=${x.name} ?required=${x.required}>${x.required ? "" : html`<option value="">–</option>`}${x.options!.map((o) => html`<option>${o}</option>`)}</select>`
        : x.type === "bool" ? html`<input type="checkbox" name=${x.name}>`
        : html`<input name=${x.name} type=${x.type === "number" ? "number" : "text"} min=${x.min ?? ""} max=${x.max ?? ""} ?required=${x.required}>`}</label>`)}
      <button ?disabled=${this.busy}>${icon("Play", 14)} ${t("device.run")}</button></div></form>`;
  }

  render() {
    return this.tools.map((tl) => this.tool(tl));
  }
}
customElements.define("phx-device-controls", PhxDeviceControls);

/** Device section of an entry: switch it, let AI clients switch it, share
 * it with single people and see who switched it (ADR-0050). */
export class PhxDevicePanel extends LitElement {
  static properties = { project: {}, entity: {}, info: { state: true }, log: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    section { margin: 20px 0; display: grid; gap: 14px; }
    h3 { margin: 0; font-size: 15px; display: flex; gap: 8px; align-items: center; }
    details summary { cursor: pointer; color: var(--muted); font-size: 14px; display: inline-flex; gap: 6px; align-items: center; }
    .box { display: grid; gap: 10px; margin-top: 10px; font-size: 14px; }
    label.check { display: inline-flex; gap: 8px; align-items: center; }
    label.check input { width: auto; }
    ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 6px; }
    li { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    li .grow { flex: 1; min-width: 160px; }
    form.add { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; }
    form.add input[type=email] { flex: 1; min-width: 200px; }
    form.add select { width: auto; }
    .bad { color: var(--bad); }
  `];
  project = "";
  entity = "";
  info?: DeviceInfo;
  log?: LogEntry[];
  busy = false;

  updated(changed: Map<string, unknown>) {
    if (changed.has("entity")) this.load();
  }

  private get base() {
    return `/projects/${this.project}/entities/${encodeURIComponent(this.entity)}/device`;
  }

  private async load() {
    this.info = await api<DeviceInfo>("GET", this.base).catch(() => undefined);
    this.log = undefined;
  }

  private async loadLog() {
    this.log = (await api<LogEntry[]>("GET", `${this.base}/log`).catch(() => [])) ?? [];
  }

  private async call(method: string, path: string, body?: unknown) {
    this.busy = true;
    try {
      this.info = await api<DeviceInfo>(method, this.base + path, body);
      return true;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return false;
    } finally {
      this.busy = false;
    }
  }

  private async share(e: Event) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    if (await this.call("POST", "/grants", { email: String(f.get("email") ?? "").trim(), days: Number(f.get("days") ?? 0) })) {
      form.reset();
      toast(t("device.shared"), "ok");
    }
  }

  private logLine(x: LogEntry) {
    const p = x.payload ?? {};
    const what = x.kind === "device.control" ? html`${p.tool} ${p.args ? Object.entries(p.args).filter(([k]) => !k.endsWith("_id") && k !== "ups").map(([k, v]) => `${k}=${v}`).join(" ") : ""}${p.ok === false ? html` <span class="bad">${t("device.failed")}</span>` : ""}`
      : hasKey(`device.log.${x.kind}`) ? t(`device.log.${x.kind}` as Key, { mode: p.aiControl ?? "" }) : x.kind;
    return html`<li><span class="muted">${formatDate(x.createdAt)}</span> <span class="grow">${x.who}${x.via === "mcp" ? html` (${t("device.via_ai")})` : ""}: ${what}</span></li>`;
  }

  render() {
    const d = this.info;
    if (!d || d.tools.length === 0) return "";
    return html`<section aria-label=${t("device.title")}>
      <h3>${icon("Power", 16)} ${t("device.title")}</h3>
      ${d.canControl ? html`<phx-device-controls .tools=${d.tools} .path=${`${this.base}/control`} @controlled=${() => this.log && this.loadLog()}></phx-device-controls>` : html`<p class="muted">${t("device.no_rights")}</p>`}
      ${d.canManage ? html`<details>
        <summary>${icon("Share2", 14)} ${t("device.manage")}${d.grants?.length ? ` · ${t("device.shared_count", { n: d.grants.length })}` : ""}${d.aiControl === "direct" ? ` · ${t("device.ai_direct_short")}` : ""}</summary>
        <div class="box">
          <label class="check"><input type="checkbox" .checked=${d.aiControl === "direct"} ?disabled=${this.busy}
            @change=${(e: Event) => this.call("PUT", "", { aiControl: (e.target as HTMLInputElement).checked ? "direct" : "suggest" })}> ${t("device.ai_direct")}</label>
          <p class="muted">${t("device.ai_hint")}</p>
          <b>${t("device.grants")}</b>
          ${d.grants?.length ? html`<ul>${d.grants.map((g) => html`<li><span class="grow">${g.userName || g.userEmail} <span class="muted">${g.userEmail}</span>
            ${g.expiresAt ? html`<span class="muted">· ${t("device.until", { date: formatDay(g.expiresAt) })}</span>` : ""}</span>
            <button class="ghost" ?disabled=${this.busy} aria-label=${t("device.revoke", { name: g.userName || g.userEmail })} @click=${() => this.call("DELETE", `/grants/${g.id}`)}>${icon("X", 14)}</button></li>`)}</ul>`
            : html`<p class="muted">${t("device.no_grants")}</p>`}
          <form class="add" @submit=${this.share}>
            <label>${t("device.email")}<input type="email" name="email" required autocomplete="off"></label>
            <label>${t("device.duration")}<select name="days">
              ${[0, 1, 7, 30, 365].map((n) => html`<option value=${n}>${n ? t("device.days", { n }) : t("device.forever")}</option>`)}</select></label>
            <button ?disabled=${this.busy}>${icon("UserPlus", 14)} ${t("device.share")}</button>
          </form>
        </div>
      </details>` : ""}
      ${d.canControl ? html`<details @toggle=${(e: Event) => (e.target as HTMLDetailsElement).open && !this.log && this.loadLog()}>
        <summary>${icon("History", 14)} ${t("device.log")}</summary>
        <div class="box">${!this.log ? html`<p class="muted">${t("app.loading")}</p>` : this.log.length ? html`<ul>${this.log.map((x) => this.logLine(x))}</ul>` : html`<p class="muted">${t("device.log_empty")}</p>`}</div>
      </details>` : ""}
    </section>`;
  }
}
customElements.define("phx-device-panel", PhxDevicePanel);
