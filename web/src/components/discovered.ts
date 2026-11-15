// Devices agents saw in the network (ADR-0072): people add them as entries
// or ignore them. Devices that match an entry link to it.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { kindName, loadModel, type Model } from "../model";
import { base } from "./shared";
import { toast } from "./toasts";

interface Found {
  id: string; ip: string; mac?: string; hostname?: string; services: string[]; via: string[];
  firstSeen: string; lastSeen: string; dismissed?: boolean; entity?: string; entityName?: string; kind: string;
}

export class PhxDiscovered extends LitElement {
  static properties = { project: {}, list: { state: true }, show: { state: true }, adding: { state: true }, model: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .bar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 12px; }
    .bar p { margin: 0 auto 0 0; color: var(--muted); max-width: 70ch; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { border: 0; border-radius: 0; background: var(--surface); color: var(--muted); height: 32px; padding: 0 11px; }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
    .tw { overflow-x: auto; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th, td { text-align: left; padding: 9px 12px; border-bottom: 1px solid var(--line); vertical-align: top; }
    th { font-size: 12px; color: var(--muted); font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
    tr:last-child td { border-bottom: 0; }
    .mono { font-family: var(--font-mono); font-size: 13px; }
    .svc { display: flex; flex-wrap: wrap; gap: 4px; }
    .acts { display: flex; gap: 6px; justify-content: flex-end; flex-wrap: wrap; }
    .acts button { font-size: 13px; padding: 4px 10px; }
    form.add { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
    form.add input, form.add select { width: auto; min-width: 140px; height: 32px; }
    .empty { padding: 26px; text-align: center; color: var(--muted); }
  `];
  project = "";
  list?: Found[];
  show: "new" | "known" | "ignored" = "new";
  adding = "";
  model?: Model;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
    void loadModel(this.project).then((m) => (this.model = m)).catch(() => {});
  }

  private async load() {
    this.list = (await api<Found[]>("GET", `/projects/${this.project}/discovered?all=1`).catch(() => [])) ?? [];
  }

  private async adopt(e: SubmitEvent, d: Found) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      const r = await api<{ applied?: boolean; entity?: { slug: string } }>("POST", `/projects/${this.project}/discovered/${d.id}/adopt`, { kind: f.get("kind"), name: f.get("name") });
      toast(r.applied === false ? t("disc.suggested") : t("disc.added"));
      this.adding = "";
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async dismiss(d: Found, dismissed: boolean) {
    await api("POST", `/projects/${this.project}/discovered/${d.id}/dismiss`, { dismissed }).catch(() => {});
    await this.load();
  }

  private row(d: Found) {
    const kinds = (this.model?.kinds ?? []).map((k) => k.id);
    return html`<tr>
      <td><b>${d.hostname || d.ip}</b>${d.hostname ? html`<div class="mono muted">${d.ip}</div>` : nothing}</td>
      <td class="mono">${d.mac ?? "–"}</td>
      <td><div class="svc">${d.services.length ? d.services.map((s) => html`<span class="chip">${s.replace(/^_/, "").replace(/\._(tcp|udp)$/, "")}</span>`) : html`<span class="muted">–</span>`}</div></td>
      <td class="muted">${formatDate(d.lastSeen)}<div style="font-size:12px">${d.via.join(", ")}</div></td>
      <td>${d.entity ? html`<a href="/p/${this.project}/e/${d.entity}">${icon("Link", 13)} ${d.entityName}</a>`
        : this.adding === d.id ? html`<form class="add" @submit=${(e: SubmitEvent) => this.adopt(e, d)}>
            <input name="name" required .value=${d.hostname || d.ip} aria-label=${t("profile.name")}>
            <select name="kind" aria-label=${t("disc.kind")}>${(kinds.length ? kinds : [d.kind]).map((k) => html`<option value=${k} ?selected=${k === d.kind}>${kindName(this.model?.kinds.find((x) => x.id === k), k)}</option>`)}</select>
            <button type="submit">${t("disc.add")}</button><button type="button" class="ghost" @click=${() => (this.adding = "")}>${t("doc.cancel")}</button></form>`
        : html`<div class="acts">
            ${d.dismissed ? html`<button class="ghost" @click=${() => this.dismiss(d, false)}>${t("disc.unignore")}</button>`
              : html`<button @click=${() => (this.adding = d.id)}>${icon("Plus", 13)} ${t("disc.add")}</button><button class="ghost" @click=${() => this.dismiss(d, true)}>${t("disc.ignore")}</button>`}</div>`}</td>
    </tr>`;
  }

  render() {
    const all = this.list;
    if (!all) return html`<p class="muted">${t("app.loading")}</p>`;
    const groups = {
      new: all.filter((d) => !d.dismissed && !d.entity),
      known: all.filter((d) => !d.dismissed && d.entity),
      ignored: all.filter((d) => d.dismissed),
    };
    const shown = groups[this.show];
    return html`<div class="bar"><p>${t("disc.intro")}</p>
        <span class="seg" role="group" aria-label=${t("disc.title")}>${(["new", "known", "ignored"] as const).map((k) => html`<button aria-pressed=${this.show === k ? "true" : "false"} @click=${() => (this.show = k)}>${t(`disc.${k}` as any)} (${groups[k].length})</button>`)}</span></div>
      ${all.length === 0 ? html`<div class="tw empty">${icon("Radar", 22)}<p>${t("disc.none")}</p></div>`
        : shown.length === 0 ? html`<div class="tw empty">${t("disc.none_here")}</div>`
        : html`<div class="tw"><table><thead><tr><th>${t("disc.device")}</th><th>MAC</th><th>${t("disc.services")}</th><th>${t("disc.seen")}</th><th></th></tr></thead>
          <tbody>${shown.map((d) => this.row(d))}</tbody></table></div>`}`;
  }
}
customElements.define("phx-discovered", PhxDiscovered);
