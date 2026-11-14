import { LitElement, css, html } from "lit";
import { api } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import "./device-panel";
import type { DeviceTool } from "./device-panel";
import { base } from "./shared";

interface SharedDevice { grant: string; entity: string; name: string; kind: string; projectName: string; sharedBy: string; expiresAt: string | null; tools: DeviceTool[] }

/** Devices other people let me switch, without other rights in their projects. */
export class PhxSharedDevices extends LitElement {
  static properties = { devices: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 860px; margin: 0 auto; padding: 28px 16px; }
    h1 { display: flex; gap: 10px; align-items: center; }
    ul { list-style: none; padding: 0; margin: 16px 0 0; display: grid; gap: 12px; }
    li.card { padding: 14px 16px; display: grid; gap: 10px; }
    .head { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; }
    .head b { font-size: 16px; }
    .where { font-size: 13px; color: var(--muted); }
  `];
  devices?: SharedDevice[];

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    this.devices = (await api<SharedDevice[]>("GET", "/me/devices").catch(() => [])) ?? [];
  }

  render() {
    return html`<div class="enter">
      <h1>${icon("Power", 22)} ${t("device.shared_title")}</h1>
      <p class="muted">${t("device.shared_hint")}</p>
      ${!this.devices ? html`<p class="muted">${t("app.loading")}</p>`
        : this.devices.length === 0 ? html`<p class="muted">${t("device.shared_none")}</p>`
        : html`<ul>${this.devices.map((d) => html`<li class="card">
            <div class="head"><b>${d.name}</b><span class="where">${d.projectName} · ${t("device.shared_by", { name: d.sharedBy })}${d.expiresAt ? ` · ${t("device.until", { date: formatDay(d.expiresAt) })}` : ""}</span></div>
            <phx-device-controls .tools=${d.tools} .path=${`/me/devices/${d.grant}/control`}></phx-device-controls>
          </li>`)}</ul>`}
    </div>`;
  }
}
customElements.define("phx-shared-devices", PhxSharedDevices);
