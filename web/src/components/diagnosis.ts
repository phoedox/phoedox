import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { relLabel } from "../model";
import { base } from "./shared";

interface Ref { slug: string; name: string; kind: string }
interface Hop extends Ref { via?: string; status: "ok" | "warn" | "down" | "unknown"; reason: string; params?: Record<string, unknown>; ip?: string; power?: Ref }
interface Path { hops: Hop[]; reached: boolean; loop?: boolean; broken: number }
interface Diagnosis { entity: Ref; paths: Path[]; result: "ok" | "broken" | "incomplete" | "none"; text: string }

/** Network path of an entry towards the internet, station by station. */
export class PhxDiagnosis extends LitElement {
  static properties = { project: {}, entity: {}, d: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-bottom: 18px; }
    .box { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 12px 14px; display: grid; gap: 10px; }
    .head { display: flex; gap: 8px; align-items: center; }
    .head h2 { font-size: 16px; margin: 0; flex: 1; }
    .result { font-size: 14px; display: flex; gap: 6px; align-items: center; }
    .result.ok { color: var(--good); } .result.broken { color: var(--bad); } .result.incomplete, .result.none { color: var(--muted); }
    ol { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
    li.hop { display: grid; gap: 2px; padding: 6px 10px; border: 1px solid var(--line); border-radius: 10px; min-width: 120px; font-size: 14px; }
    li.hop.down { border-color: var(--bad); background: color-mix(in srgb, var(--bad) 8%, var(--surface)); }
    li.hop .name { display: flex; gap: 6px; align-items: center; font-weight: 600; }
    .dot { width: 9px; height: 9px; border-radius: 50%; flex: none; background: var(--muted); }
    .dot.ok { background: var(--good); } .dot.warn { background: var(--signal); } .dot.down { background: var(--bad); }
    .why, .ip, .power { font-size: 12px; color: var(--muted); }
    .ip { font-family: var(--font-mono); }
    li.arrow { display: flex; flex-direction: column; align-items: center; color: var(--muted); font-size: 11px; }
    li.end { font-size: 12px; color: var(--muted); display: flex; gap: 4px; align-items: center; }
    .path + .path { border-top: 1px solid var(--line); padding-top: 10px; }
    button.small { padding: 3px 9px; font-size: 13px; }
  `];
  project = "";
  entity = "";
  d?: Diagnosis;
  error = "";
  busy = false;
  private loaded = "";

  willUpdate() {
    const key = `${this.project}/${this.entity}`;
    if (key !== this.loaded) {
      this.loaded = key;
      void this.load();
    }
  }

  private async load() {
    this.busy = true;
    try {
      this.d = await api<Diagnosis>("GET", `/projects/${this.project}/entities/${this.entity}/diagnose`);
      this.error = "";
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private hop(h: Hop) {
    return html`<li class="hop ${h.status}">
      <span class="name"><span class="dot ${h.status}" aria-hidden="true"></span><a href="/p/${this.project}/e/${h.slug}">${h.name}</a></span>
      <span class="why">${t(h.reason as any, h.params as any)}</span>
      ${h.ip ? html`<span class="ip">${h.ip}</span>` : ""}
      ${h.power ? html`<span class="power">${icon("PlugZap", 11)} ${t("diag.power", { name: h.power.name })}</span>` : ""}
    </li>`;
  }

  private path(p: Path) {
    return html`<div class="path"><ol>${p.hops.map((h, i) => html`
      ${i > 0 ? html`<li class="arrow" aria-hidden="true">${icon("ArrowRight", 14)}<span>${relLabel(h.via ?? "")}</span></li>` : ""}${this.hop(h)}`)}
      <li class="end">${p.reached ? html`${icon("Globe", 13)} ${t("diag.reached")}` : p.loop ? t("diag.loop") : t("diag.open_end")}</li>
    </ol></div>`;
  }

  render() {
    const d = this.d;
    const res = d ? { ok: ["CircleCheck", "diag.result_ok"], broken: ["CircleAlert", "diag.result_broken"], incomplete: ["CircleDashed", "diag.result_incomplete"], none: ["CircleDashed", "diag.result_none"] }[d.result] : undefined;
    return html`<div class="box" aria-busy=${this.busy ? "true" : "false"}>
      <div class="head"><h2>${t("diag.title")}</h2>
        <button class="ghost small" ?disabled=${this.busy} @click=${this.load}>${icon("RefreshCw", 13)} ${t("diag.refresh")}</button></div>
      ${this.error ? html`<p class="error">${this.error}</p>` : ""}
      ${d && res ? html`<p class="result ${d.result}" role="status">${icon(res[0], 15)} ${t(res[1] as any)}</p>
        ${d.paths.map((p) => this.path(p))}` : ""}
    </div>`;
  }
}
customElements.define("phx-diagnosis", PhxDiagnosis);
