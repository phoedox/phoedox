import { LitElement, css, html } from "lit";
import { ApiError, api, type Doc, type Edge, type Entity } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

type Detail = { entity: Entity; edges: Edge[]; documents: Doc[] };

export class PhxEntityView extends LitElement {
  static properties = { project: {}, entity: {}, d: { state: true }, error: { state: true } };
  static styles = [base, css`
    table { border-collapse: collapse; margin-top: 6px; } td { padding: 4px 12px 4px 0; vertical-align: top; }
    section { margin-top: 20px; } h2 { font-size: 16px; margin-bottom: 6px; }
    ul { margin: 0; padding-left: 18px; }
  `];
  project = "";
  entity = "";
  d?: Detail;
  error = "";
  private loaded = "";

  async willUpdate() {
    if (this.loaded !== this.entity) {
      this.loaded = this.entity;
      this.d = undefined;
      try {
        this.d = await api<Detail>("GET", `/projects/${this.project}/entities/${this.entity}`);
      } catch (e) {
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
      }
    }
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    if (!this.d) return html`<p class="muted">${t("app.loading")}</p>`;
    const { entity: e, edges, documents } = this.d;
    const attrs = Object.entries(e.attrs ?? {});
    return html`<div class="enter">
      <h1>${e.name}</h1>
      <p class="row"><span class="chip">${e.kind}</span><span class="chip mono">@${e.slug}</span></p>
      ${attrs.length ? html`<section><h2>${t("entity.attrs")}</h2><table>${attrs.map(([k, v]) => html`<tr><td class="muted">${k}</td><td class="mono">${typeof v === "object" ? JSON.stringify(v) : String(v)}</td></tr>`)}</table></section>` : ""}
      <section><h2>${t("entity.relations")}</h2>
        ${edges.length ? html`<ul>${edges.map((x) => html`<li>${x.from === e.id
          ? html`${icon("ArrowRight", 13)} <span class="mono">${x.rel}</span> → ${x.toName}`
          : html`${icon("ArrowLeft", 13)} ${x.fromName} → <span class="mono">${x.rel}</span>`}</li>`)}</ul>` : html`<p class="muted">${t("entity.none")}</p>`}
      </section>
      <section><h2>${t("entity.mentioned_in")}</h2>
        ${documents.length ? html`<ul>${documents.map((d) => html`<li><a href="/p/${this.project}/d/${d.slug}">${d.title}</a></li>`)}</ul>` : html`<p class="muted">${t("entity.none")}</p>`}
      </section>
    </div>`;
  }
}
customElements.define("phx-entity-view", PhxEntityView);
