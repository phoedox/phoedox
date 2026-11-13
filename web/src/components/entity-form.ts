import { LitElement, css, html } from "lit";
import { ApiError, api, type Entity, type Suggestion } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { attrLabel, findKind, groupName, kindName, loadModel, optLabel, type Attr, type Model } from "../model";
import { navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type EntityResult = { applied: boolean; entity?: Entity; suggestion?: Suggestion };

/** Create (kind preset or chosen) or edit (entity set) an entry; fields come from the kind's schema. */
export class PhxEntityForm extends LitElement {
  static properties = { project: {}, entity: {}, kind: {}, model: { state: true }, current: { state: true }, error: { state: true }, badAttr: { state: true }, busy: { state: true } };
  static styles = [base, css`
    form { display: grid; gap: 12px; max-width: 720px; }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    .attrs { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    .unit { display: flex; align-items: center; gap: 6px; }
    .unit span { color: var(--muted); font-size: 13px; white-space: nowrap; }
    label.field.bad input, label.field.bad select { border-color: var(--bad); }
    .req { color: var(--bad); }
    .extra { font-size: 13px; }
    @media (max-width: 640px) { .two, .attrs { grid-template-columns: 1fr; } }
  `];
  project = "";
  /** Slug of the entity to edit; empty creates a new one. */
  entity = "";
  /** Preselected kind for new entries. */
  kind = "";
  model?: Model;
  current?: Entity;
  error = "";
  badAttr = "";
  busy = false;
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.entity}`;
    if (this.loaded === key) return;
    this.loaded = key;
    try {
      this.model = await loadModel(this.project);
      if (this.entity) {
        this.current = (await api<{ entity: Entity }>("GET", `/projects/${this.project}/entities/${this.entity}`)).entity;
        this.kind = this.current.kind;
      } else {
        this.kind ||= new URLSearchParams(location.search).get("kind") ?? "";
        if (!findKind(this.model, this.kind)) this.kind = this.model.kinds[0]?.id ?? "";
      }
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private field(a: Attr, v: unknown) {
    const name = `a.${a.name}`;
    const label = html`${attrLabel(a)}${a.required ? html` <span class="req" aria-hidden="true">*</span>` : ""}`;
    const cls = this.badAttr === a.name ? "field bad" : "field";
    const str = v === undefined || v === null ? "" : String(v);
    switch (a.type) {
      case "bool":
        return html`<label class=${cls}>${label}<select name=${name} ?required=${a.required}>
          <option value="">–</option>
          <option value="true" ?selected=${v === true}>${t("common.yes")}</option>
          <option value="false" ?selected=${v === false}>${t("common.no")}</option></select></label>`;
      case "select":
        return html`<label class=${cls}>${label}<select name=${name} ?required=${a.required}>
          <option value="">–</option>
          ${(a.options ?? []).map((o) => html`<option value=${o} ?selected=${o === str}>${optLabel(o)}</option>`)}</select></label>`;
      case "number":
        return html`<label class=${cls}>${label}<span class="unit"><input name=${name} type="number" step="any" inputmode="decimal" .value=${str} ?required=${a.required}>${a.unit ? html`<span>${a.unit}</span>` : ""}</span></label>`;
      case "date":
        return html`<label class=${cls}>${label}<input name=${name} type="date" .value=${str} ?required=${a.required}></label>`;
      case "url":
        return html`<label class=${cls}>${label}<input name=${name} type="url" placeholder="https://" .value=${str} ?required=${a.required}></label>`;
      case "ip":
        return html`<label class=${cls}>${label}<input name=${name} class="mono" inputmode="decimal" autocomplete="off" spellcheck="false" placeholder="192.168.0.1" .value=${str} ?required=${a.required}></label>`;
      default:
        return html`<label class=${cls}>${label}<input name=${name} .value=${str} maxlength="2000" ?required=${a.required}></label>`;
    }
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const k = findKind(this.model, this.kind);
    if (!k) return;
    const f = new FormData(e.target as HTMLFormElement);
    // Values of attributes the kind does not define stay untouched.
    const attrs: Record<string, unknown> = Object.fromEntries(Object.entries(this.current?.attrs ?? {}).filter(([n]) => !k.attrs.some((a) => a.name === n)));
    for (const a of k.attrs) {
      const v = String(f.get(`a.${a.name}`) ?? "").trim();
      if (v === "") continue;
      attrs[a.name] = a.type === "number" ? Number(v) : a.type === "bool" ? v === "true" : v;
    }
    this.busy = true;
    this.error = this.badAttr = "";
    try {
      const body = { kind: this.kind, name: String(f.get("name") ?? ""), slug: this.current?.slug, attrs };
      const r = await api<EntityResult>("POST", `/projects/${this.project}/entities`, body);
      if (!r.applied) {
        toast(t("suggestion.created"));
        navigate(this.current ? `/p/${this.project}/e/${this.current.slug}` : `/p/${this.project}/model?kind=${this.kind}&view=entries`);
        return;
      }
      navigate(`/p/${this.project}/e/${r.entity!.slug}`);
    } catch (err) {
      if (err instanceof ApiError) {
        this.badAttr = String(err.params.attr ?? "");
        const a = k.attrs.find((x) => x.name === this.badAttr);
        if (a) err.params.attr = attrLabel(a); // show the label, not the id
        this.error = err.text;
      } else this.error = t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    if (!this.model) return this.error ? html`<p class="error">${this.error}</p>` : html`<p class="muted">${t("app.loading")}</p>`;
    const k = findKind(this.model, this.kind);
    const groups = [...new Set(this.model.kinds.map((x) => x.group))];
    const extra = Object.entries(this.current?.attrs ?? {}).filter(([n]) => !k?.attrs.some((a) => a.name === n));
    return html`<div class="enter">
      <h1 style="margin-bottom:16px">${this.current ? t("entity.edit_title", { name: this.current.name }) : t("entity.new")}</h1>
      <form @submit=${this.submit} novalidate>
        <div class="two">
          <label class="field">${t("entity.kind")}<select name="kind" @change=${(e: Event) => (this.kind = (e.target as HTMLSelectElement).value)}>
            ${groups.map((g) => html`<optgroup label=${groupName(g)}>
              ${this.model!.kinds.filter((x) => x.group === g).map((x) => html`<option value=${x.id} ?selected=${x.id === this.kind}>${kindName(x)}</option>`)}</optgroup>`)}
          </select></label>
          <label class="field">${t("entity.name")}<input name="name" required maxlength="200" .value=${this.current?.name ?? ""} autocomplete="off"></label>
        </div>
        ${k?.attrs.length ? html`<div class="attrs">${k.attrs.map((a) => this.field(a, this.current?.attrs?.[a.name]))}</div>` : ""}
        ${extra.length ? html`<p class="muted extra">${t("entity.extra_kept")}: ${extra.map(([n]) => html`<code>${n}</code> `)}</p>` : ""}
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
        <div class="row">
          <button ?disabled=${this.busy}>${icon("Check", 14)} ${t("entity.save")}</button>
          <a class="btn ghost" href=${this.current ? `/p/${this.project}/e/${this.current.slug}` : `/p/${this.project}/model`}>${t("doc.cancel")}</a>
        </div>
      </form>
    </div>`;
  }
}
customElements.define("phx-entity-form", PhxEntityForm);
