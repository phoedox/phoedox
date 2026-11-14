import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { checkIntervals, checkState, type MonitorCheck } from "../checks";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import { intervalLabel } from "./request-block";
import { base } from "./shared";
import { toast } from "./toasts";

/** Monitoring checks of a project: state, interval, switch, run now. */
export class PhxChecks extends LitElement {
  static properties = { project: {}, checks: { state: true }, loaded: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
    th { text-align: left; font-weight: 600; color: var(--muted); font-size: 12.5px; padding: 4px 8px 4px 0; }
    td { padding: 6px 8px 6px 0; border-top: 1px solid var(--line); vertical-align: top; }
    td.nw { white-space: nowrap; }
    .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: var(--muted); margin-right: 6px; }
    .dot.ok { background: var(--good); } .dot.fail { background: var(--bad); } .dot.paused { background: var(--line); }
    select { font-size: 13px; padding: 3px 6px; }
    button { padding: 3px 9px; font-size: 13px; }
    .wrap { overflow-x: auto; }
    .detail { color: var(--bad); font-size: 12.5px; }
  `];
  project = "";
  checks: MonitorCheck[] = [];
  loaded = false;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    this.checks = (await api<MonitorCheck[]>("GET", `/projects/${this.project}/checks`).catch(() => [])) ?? [];
    this.loaded = true;
  }

  private async call(fn: () => Promise<unknown>) {
    try {
      await fn();
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private patch(c: MonitorCheck, change: Partial<{ intervalSec: number; enabled: boolean }>) {
    return this.call(() => api("PATCH", `/projects/${this.project}/checks/${c.id}`, { intervalSec: c.intervalSec, enabled: c.enabled, entity: c.entitySlug, ...change }));
  }

  render() {
    if (!this.loaded) return "";
    if (!this.checks.length) return html`<p class="muted">${t("check.empty")}</p>`;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`<div class="wrap"><table>
      <thead><tr><th>${t("check.name")}</th><th>${t("check.status")}</th><th>${t("check.interval")}</th><th>${t("check.last")}</th><th></th></tr></thead>
      <tbody>${this.checks.map((c) => {
        const state = checkState(c);
        return html`<tr>
          <td><a href="/p/${this.project}/d/${c.documentSlug}">${c.name}</a>
            ${c.entitySlug ? html`<br /><span class="muted">${icon("Link2", 12)} <a href="/p/${this.project}/e/${c.entitySlug}">${c.entityName}</a></span>` : ""}</td>
          <td class="nw"><span class="dot ${state}" aria-hidden="true"></span>${t(`check.state.${state}` as any)}
            ${c.lastStatus === "fail" && c.lastMessage ? html`<br /><span class="detail">${t(c.lastMessage as any, { detail: c.lastDetail })}</span>` : ""}</td>
          <td>${canEdit ? html`<select aria-label=${t("check.interval")} @change=${(e: Event) => this.patch(c, { intervalSec: Number((e.target as HTMLSelectElement).value) })}>
              ${checkIntervals.map((s) => html`<option value=${s} ?selected=${s === c.intervalSec}>${intervalLabel(s)}</option>`)}</select>` : intervalLabel(c.intervalSec)}</td>
          <td class="nw">${c.lastRun ? html`${formatDate(c.lastRun)}<br /><span class="muted">${c.lastMs} ms</span>` : "–"}</td>
          <td class="nw">${canEdit ? html`
            <button class="ghost" @click=${() => this.call(() => api("POST", `/projects/${this.project}/checks/${c.id}/run`))}>${t("check.run_now")}</button>
            <button class="ghost" @click=${() => this.patch(c, { enabled: !c.enabled })}>${c.enabled ? t("check.pause") : t("check.resume")}</button>
            <button class="ghost" aria-label=${t("check.remove")} title=${t("check.remove")} @click=${() => this.call(() => api("DELETE", `/projects/${this.project}/checks/${c.id}`))}>${icon("Trash2", 14)}</button>` : ""}</td>
        </tr>`;
      })}</tbody></table></div>`;
  }
}
customElements.define("phx-checks", PhxChecks);
