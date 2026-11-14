import { LitElement, css, html } from "lit";
import { ApiError, api, type Edge, type Entity } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { findKind, groupColor, type Model } from "../model";
import { rackSlots, type Mounted } from "../rack";
import { base } from "./shared";
import { toast } from "./toasts";

/** Front view of a rack with the devices mounted in it, by height unit. */
export class PhxRackView extends LitElement {
  static properties = { project: {}, rack: { attribute: false }, edges: { attribute: false }, model: { attribute: false }, canEdit: { type: Boolean }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-bottom: 18px; }
    .box { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 12px 14px; display: grid; gap: 12px; }
    .wrap { display: grid; grid-template-columns: minmax(0, 420px) minmax(0, 1fr); gap: 18px; align-items: start; }
    .rack { border: 2px solid var(--muted); border-radius: 6px; padding: 4px; background: var(--surface2); display: grid; gap: 2px; }
    .slot { display: grid; grid-template-columns: 28px 1fr; align-items: stretch; min-height: 22px; font-size: 12.5px; }
    .slot .u { color: var(--muted); font-family: var(--font-mono); font-size: 11px; text-align: right; padding-right: 6px; align-self: center; }
    .slot .empty { border: 1px dashed var(--line); border-radius: 3px; }
    .dev { display: flex; gap: 6px; align-items: center; padding: 2px 8px; border-radius: 4px; background: var(--surface); border-left: 4px solid var(--c);
      color: var(--fg); text-decoration: none; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
    .dev .ico { color: var(--c); flex: none; }
    .dev.conflict { outline: 2px solid var(--bad); }
    @media (hover: hover) and (pointer: fine) { a.dev:hover { background: var(--accent-soft); } }
    table { border-collapse: collapse; font-size: 13px; width: 100%; }
    td { padding: 4px 6px 4px 0; border-bottom: 1px solid var(--line); }
    td input { width: 64px; padding: 3px 6px; }
    .hint { font-size: 13px; color: var(--muted); margin: 0; }
    .bad { color: var(--bad); font-size: 13px; }
    @media (max-width: 800px) { .wrap { grid-template-columns: 1fr; } }
  `];
  project = "";
  rack?: Entity;
  edges: Edge[] = [];
  model?: Model;
  canEdit = false;
  busy = false;

  private devices(): Mounted[] {
    return this.edges.filter((e) => e.rel === "mounted_in" && e.to === this.rack?.id).map((e) => ({
      slug: e.fromSlug ?? "", name: e.fromName, kind: e.fromKind ?? "",
      u: Number(e.attrs?.u) || undefined, height: Math.max(1, Number(e.attrs?.height) || 1),
    }));
  }

  private async place(d: Mounted, form: HTMLFormElement) {
    const f = new FormData(form);
    const u = Number(f.get(`u-${d.slug}`)), height = Number(f.get(`h-${d.slug}`)) || 1;
    this.busy = true;
    try {
      await api("POST", `/projects/${this.project}/edges`, { from: d.slug, rel: "mounted_in", to: this.rack!.slug, attrs: u ? { u, height } : { height } });
      this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const units = Math.min(Math.max(Number(this.rack?.attrs?.height_units) || 42, 1), 60);
    const devices = this.devices();
    const { slots, unplaced, conflicts } = rackSlots(units, devices);
    const color = (kind: string) => groupColor[findKind(this.model, kind)?.group ?? "custom"];
    const kicon = (kind: string) => findKind(this.model, kind)?.icon ?? "Box";
    return html`<div class="box">
      <h2 style="font-size:16px;margin:0">${t("rack.title", { units })}</h2>
      <div class="wrap">
        <div class="rack" role="list" aria-label=${t("rack.title", { units })}>
          ${slots.map((s) => html`<div class="slot" role="listitem" style="min-height:${(s.top - s.bottom + 1) * 22 + (s.top - s.bottom) * 2}px">
            <span class="u">${s.top === s.bottom ? s.top : `${s.top}–${s.bottom}`}</span>
            ${s.device ? html`<a class="dev ${s.conflict ? "conflict" : ""}" style="--c:${color(s.device.kind)}" href="/p/${this.project}/e/${s.device.slug}"
              title=${`${s.device.name} · U${s.device.u}${s.device.height > 1 ? `–${s.device.u! + s.device.height - 1}` : ""}`}>${icon(kicon(s.device.kind), 13)} ${s.device.name}</a>`
              : html`<span class="empty"></span>`}</div>`)}
        </div>
        <div>
          ${conflicts.length ? html`<p class="bad" role="alert">${icon("CircleAlert", 13)} ${t("rack.conflict")}</p>` : ""}
          ${devices.length ? html`<form @submit=${(e: SubmitEvent) => e.preventDefault()}><table>
            ${devices.map((d) => html`<tr><td><a href="/p/${this.project}/e/${d.slug}">${d.name}</a></td>
              ${this.canEdit ? html`<td><label>U <input type="number" min="1" max=${units} name="u-${d.slug}" .value=${d.u ? String(d.u) : ""}></label></td>
                <td><label>${t("rack.height")} <input type="number" min="1" max="20" name="h-${d.slug}" .value=${String(d.height)}></label></td>
                <td><button class="ghost" style="padding:2px 8px;font-size:12px" ?disabled=${this.busy}
                  @click=${(e: Event) => this.place(d, (e.currentTarget as HTMLElement).closest("form")!)}>${t("rack.set")}</button></td>`
                : html`<td>${d.u ? `U${d.u}${d.height > 1 ? `–${d.u + d.height - 1}` : ""}` : t("rack.unplaced")}</td>`}</tr>`)}</table></form>`
            : html`<p class="hint">${t("rack.empty")}</p>`}
          ${unplaced.length && devices.length ? html`<p class="hint">${t("rack.unplaced_hint", { count: unplaced.length })}</p>` : ""}
        </div>
      </div>
    </div>`;
  }
}
customElements.define("phx-rack-view", PhxRackView);
