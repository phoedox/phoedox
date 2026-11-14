import { LitElement, css, html } from "lit";
import { api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

interface User { slug: string; name: string; kind: string }
interface Addr { ip: string; entries: User[]; inDhcp?: boolean; gateway?: boolean }
interface Subnet { slug: string; name: string; cidr: string; vlan?: number; gateway?: string; dhcp?: string; size: string; used: number; free: string; percent: number; nextFree?: string; addresses: Addr[]; invalid?: boolean }
interface Report { subnets: Subnet[]; conflicts: Addr[]; outside: Addr[] }

/** IP address management: subnets with used and free addresses, conflicts. */
export class PhxIpam extends LitElement {
  static properties = { project: {}, r: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 1050px; margin: 0 auto; padding: 24px 16px 60px; }
    .card { border: 1px solid var(--line); border-radius: 12px; padding: 12px 14px; background: var(--surface); margin-bottom: 12px; }
    .card header { display: flex; gap: 10px; align-items: baseline; flex-wrap: wrap; }
    .card header b { font-size: 16px; }
    .bar { height: 8px; border-radius: 4px; background: var(--surface2); overflow: hidden; margin: 8px 0; }
    .bar span { display: block; height: 100%; background: var(--accent); }
    table { border-collapse: collapse; width: 100%; font-size: 13.5px; }
    td { border-bottom: 1px solid var(--line); padding: 3px 8px; }
    td.ip { font-family: var(--font-mono); white-space: nowrap; width: 1%; }
    .warn { border-color: var(--bad); }
    details summary { cursor: pointer; color: var(--muted); font-size: 13.5px; }
  `];
  project = "";
  r?: Report;

  connectedCallback() {
    super.connectedCallback();
    api<Report>("GET", `/projects/${this.project}/ipam`).then((r) => (this.r = r), () => (this.r = { subnets: [], conflicts: [], outside: [] }));
  }

  private users(a: Addr) {
    return a.entries.map((e, i) => html`${i ? ", " : ""}<a href="/p/${this.project}/e/${e.slug}">${e.name}</a>`);
  }

  private rows(list: Addr[]) {
    return html`<table>${list.map((a) => html`<tr><td class="ip">${a.ip}</td><td>${this.users(a)}${a.gateway ? html` <span class="chip">${t("ipam.gateway")}</span>` : ""}${a.inDhcp ? html` <span class="chip">DHCP</span>` : ""}</td></tr>`)}</table>`;
  }

  render() {
    const r = this.r;
    if (!r) return html`<p class="muted">${t("app.loading")}</p>`;
    return html`
      <h1>${t("ipam.title")}</h1>
      ${r.conflicts.length ? html`<div class="card warn" role="alert"><header>${icon("TriangleAlert", 16)} <b>${t("ipam.conflicts", { n: r.conflicts.length })}</b></header>${this.rows(r.conflicts)}</div>` : ""}
      ${r.subnets.length ? "" : html`<p class="muted">${t("ipam.none")}</p>`}
      ${r.subnets.map((s) => html`<div class="card">
        <header><b><a href="/p/${this.project}/e/${s.slug}">${s.name}</a></b><span class="mono">${s.cidr}</span>
          ${s.vlan ? html`<span class="chip">VLAN ${s.vlan}</span>` : ""}${s.gateway ? html`<span class="muted">${t("ipam.gateway")} ${s.gateway}</span>` : ""}
          ${s.dhcp ? html`<span class="muted">DHCP ${s.dhcp}</span>` : ""}</header>
        ${s.invalid ? html`<p class="error">${t("ipam.invalid", { cidr: s.cidr })}</p>` : html`
          <div class="bar" role="progressbar" aria-valuenow=${s.percent} aria-valuemin="0" aria-valuemax="100" aria-label=${t("ipam.usage")}><span style="width:${Math.min(100, s.percent)}%"></span></div>
          <p class="muted" style="margin:0">${t("ipam.counts", { used: s.used, size: s.size, free: s.free })}${s.nextFree ? html` · ${t("ipam.next")} <b class="mono">${s.nextFree}</b>` : ""}</p>
          ${s.addresses.length ? html`<details><summary>${t("ipam.addresses", { n: s.addresses.length })}</summary>${this.rows(s.addresses)}</details>` : ""}`}
      </div>`)}
      ${r.outside.length ? html`<div class="card"><header>${icon("Info", 16)} <b>${t("ipam.outside", { n: r.outside.length })}</b></header>${this.rows(r.outside)}</div>` : ""}`;
  }
}
customElements.define("phx-ipam", PhxIpam);
