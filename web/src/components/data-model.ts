import "./kind-rules";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, svg } from "lit";
import { ApiError, api, type Suggestion } from "../api";
import { bounds, layout } from "../graph-layout";
import { t } from "../i18n";
import { icon } from "../icon";
import {
  attrLabel, attrTypes, findKind, groupColor, groupName, kindIcons, kindName, loadModel, relLabel, toID,
  type Attr, type AttrType, type Kind, type KindInput, type Model, type RelUse,
} from "../model";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type View = "kinds" | "overview";
type KindResult = { applied: boolean; kind?: Kind; suggestion?: Suggestion };

/** One relation of a kind, read as a sentence: from → rel → to. */
export interface RelLine {
  from: string; rel: string; to: string;
  /** Defined in the data model (otherwise only used, through a relation allowed between any kinds). */
  defined: boolean;
  /** Part of the project (can be removed here), not of a core kind or plugin. */
  own: boolean;
  count: number;
}

/** The relations of a kind as sentences with direction and how often they are used. */
export function relationLines(m: Model, k: Kind): RelLine[] {
  const used = m.used ?? [];
  const count = (from: string, rel: string, to: string) =>
    used.filter((u) => u.rel === rel && (from === "*" || u.from === from) && (to === "*" || u.to === to)).reduce((n, u) => n + u.count, 0);
  const out: RelLine[] = [];
  for (const r of k.relations ?? []) for (const to of r.to) out.push({ from: k.id, rel: r.rel, to, defined: true, own: !!r.project || k.source === "project", count: count(k.id, r.rel, to) });
  for (const o of m.kinds) {
    if (o.id === k.id) continue;
    for (const r of o.relations ?? []) if (r.to.includes(k.id)) out.push({ from: o.id, rel: r.rel, to: k.id, defined: true, own: false, count: count(o.id, r.rel, k.id) });
  }
  // Edges between entries that no definition above covers (generic relations such as depends_on).
  const covered = (u: RelUse) => out.some((l) => l.rel === u.rel && (l.from === "*" || l.from === u.from) && (l.to === "*" || l.to === u.to));
  for (const u of used) if ((u.from === k.id || u.to === k.id) && !covered(u)) out.push({ from: u.from, rel: u.rel, to: u.to, defined: false, own: false, count: u.count });
  return out;
}

/** Project settings → kinds and fields: the rules behind the knowledge graph. */
export class PhxDataModel extends SignalWatcher(LitElement) {
  static properties = {
    project: {}, kind: {}, model: { state: true }, view: { state: true }, showAll: { state: true },
    error: { state: true }, formError: { state: true }, creating: { state: true }, addType: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 16px; }
    .head p { margin: 0 auto 0 0; max-width: 60ch; }
    .seg { display: inline-flex; padding: 3px; gap: 2px; border-radius: 9px; background: var(--surface2); }
    .seg button { background: transparent; color: var(--muted); padding: 5px 12px; font-size: 13px; }
    .seg button[aria-pressed="true"] { background: var(--surface); color: var(--fg); box-shadow: 0 1px 2px rgb(0 0 0 / .08); }
    .grid { display: grid; grid-template-columns: 230px minmax(0, 1fr); gap: 22px; align-items: start; }
    .list { display: flex; flex-direction: column; gap: 12px; position: sticky; top: calc(var(--header-h, 56px) + 12px); }
    .lbl { font-size: 11px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); font-weight: 600; padding: 0 8px 4px; }
    .kind { all: unset; box-sizing: border-box; display: flex; align-items: center; gap: 8px; width: 100%; padding: 5px 8px; border-radius: 6px;
      font-size: 14px; color: var(--muted); cursor: pointer; transition: background-color var(--dur-fast) ease, color var(--dur-fast) ease; }
    .kind small { margin-left: auto; font-family: var(--font-mono); font-size: 11px; }
    .kind.on { background: var(--accent-soft); color: var(--fg); }
    .kind:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
    @media (hover: hover) and (pointer: fine) { .kind:hover { background: var(--surface2); color: var(--fg); } }
    .list .more { border: none; background: none; color: var(--muted); font-size: 13px; justify-content: flex-start; padding: 4px 8px; }
    .kic { display: inline-grid; place-items: center; width: 24px; height: 24px; border-radius: 6px; flex: none;
      color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent); }
    .kic.big { width: 44px; height: 44px; border-radius: 12px; }
    .sheet { background: var(--surface); border: 1px solid var(--line); border-radius: 14px; overflow: hidden; }
    .sheet > header { display: flex; gap: 14px; align-items: center; padding: 18px 20px; flex-wrap: wrap; }
    .sheet > header .t { flex: 1 1 240px; min-width: 0; }
    .sheet > header h2 { font-size: 22px; margin: 0; }
    .sheet > header code { font-size: 12px; }
    .sheet > header .chips { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
    .sheet .desc { padding: 0 20px 14px; margin: 0; color: var(--muted); }
    .sheet section { border-top: 1px solid var(--line); padding: 16px 20px; display: flex; flex-direction: column; gap: 10px; }
    .sheet section h3 { font-size: 15px; margin: 0; display: flex; align-items: center; gap: 8px; }
    .sheet section h3 .muted { font-weight: 400; font-size: 13px; }
    table.fields { width: 100%; border-collapse: collapse; font-size: 14px; }
    table.fields th { text-align: left; font-size: 12px; color: var(--muted); font-weight: 600; padding: 4px 8px 6px 0; border-bottom: 1px solid var(--line); }
    table.fields td { padding: 7px 8px 7px 0; border-bottom: 1px solid var(--surface2); vertical-align: top; }
    table.fields tr:last-child td { border-bottom: 0; }
    table.fields td.x { width: 1%; text-align: right; }
    table.fields button { padding: 2px 6px; }
    .tag { font-size: 10px; text-transform: uppercase; letter-spacing: .06em; color: var(--accent); margin-left: 6px; }
    ul.rels { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
    ul.rels li { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; gap: 10px; align-items: center; padding: 7px 0; border-bottom: 1px solid var(--surface2); }
    ul.rels li:last-child { border-bottom: 0; }
    .sent { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: 14px; }
    .sent .k { display: inline-flex; align-items: center; gap: 5px; font-weight: 600; }
    .sent a.k { cursor: pointer; color: var(--fg); text-decoration: underline; text-decoration-color: var(--line); text-underline-offset: 3px; }
    .sent .rel { color: var(--muted); display: inline-flex; align-items: center; gap: 4px; }
    .sent .rel svg { color: var(--accent); }
    .cnt { font-family: var(--font-mono); font-size: 12px; color: var(--muted); white-space: nowrap; text-decoration: none; }
    a.cnt:hover { color: var(--fg); }
    .sub { font-size: 12px; color: var(--muted); margin: 6px 0 0; }
    form.add { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; padding: 10px 12px; border: 1px dashed var(--line); border-radius: 10px; }
    form.add label.field { flex: 1 1 150px; }
    form.add .sentence { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; flex: 1 1 100%; font-size: 14px; }
    form.add .sentence input, form.add .sentence select { width: auto; flex: 1 1 140px; }
    form.stack { display: grid; gap: 10px; }
    label.check { display: flex; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); }
    label.check input { width: auto; }
    .icons { display: flex; flex-wrap: wrap; gap: 4px; }
    .icons label { display: inline-grid; place-items: center; width: 32px; height: 32px; border-radius: 8px; border: 1px solid var(--line); cursor: pointer; color: var(--muted); }
    .icons input { position: absolute; opacity: 0; pointer-events: none; }
    .icons label:has(input:checked) { border-color: var(--accent); color: var(--accent); background: var(--accent-soft); }
    .icons label:has(input:focus-visible) { outline: 2px solid var(--accent); outline-offset: 1px; }
    svg.ov { width: 100%; height: auto; max-height: 72vh; display: block; background: var(--surface); border: 1px solid var(--line); border-radius: 14px; }
    .ov .nd { cursor: pointer; }
    .ov .nd circle { fill: var(--surface); stroke: var(--c); stroke-width: 2; }
    .ov .nd .ico { color: var(--c); }
    .ov .nd text { font-size: calc(13px * var(--f, 1)); fill: var(--fg); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    .ov .nd:focus-visible circle { stroke: var(--accent); stroke-width: 3; }
    .ov .nd:focus { outline: none; }
    .ov line { stroke: var(--muted); stroke-width: calc(1.4px * var(--f, 1)); }
    .ov text.el { font-size: calc(11.5px * var(--f, 1)); fill: var(--muted); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    .ov marker path { fill: var(--muted); }
    .ovnote { font-size: 13px; color: var(--muted); margin: 8px 2px 0; }
    @media (max-width: 860px) {
      .grid { grid-template-columns: 1fr; }
      .list { position: static; flex-direction: row; overflow-x: auto; gap: 16px; padding-bottom: 6px; }
      .list > div { min-width: 180px; }
      ul.rels li { grid-template-columns: minmax(0, 1fr) auto; }
    }
  `];

  project = "";
  /** Selected kind from the URL (?kind=), may be empty. */
  kind = "";
  model?: Model;
  view: View = "kinds";
  error = "";
  formError = "";
  /** The "new kind" form is open. */
  creating = false;
  /** Also list built-in kinds without entries. */
  showAll = false;
  addType: AttrType = "text";
  busy = false;
  private loadedFor = "";

  private get canEdit() {
    return ["editor", "owner"].includes(currentProject.get()?.role ?? "");
  }

  private get sel(): Kind | undefined {
    return findKind(this.model, this.kind) ?? this.model?.kinds.find((k) => k.count > 0) ?? this.model?.kinds[0];
  }

  async willUpdate() {
    if (this.loadedFor === this.project) return;
    this.loadedFor = this.project;
    const q = new URLSearchParams(location.search);
    this.view = q.get("view") === "overview" ? "overview" : "kinds";
    this.kind ||= q.get("kind") ?? "";
    this.creating = q.get("new") === "1" && this.canEdit;
    try {
      this.model = await loadModel(this.project, true);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private select(id: string) {
    this.kind = id;
    this.view = "kinds";
    this.creating = false;
    this.formError = "";
    this.syncURL();
  }

  private setView(v: View) {
    this.view = v;
    this.syncURL();
  }

  private syncURL() {
    const q = new URLSearchParams();
    if (this.sel) q.set("kind", this.sel.id);
    if (this.view === "overview") q.set("view", "overview");
    history.replaceState(history.state, "", `${location.pathname}?${q}`);
  }

  /** Saves the project's part of a kind definition. */
  private async save(id: string, input: KindInput, after?: () => void) {
    this.busy = true;
    this.formError = "";
    try {
      const r = await api<KindResult>("PUT", `/projects/${this.project}/kinds/${id}`, input);
      if (!r.applied) toast(t("suggestion.created"));
      this.model = await loadModel(this.project, true);
      this.select(id);
      after?.();
    } catch (e) {
      this.formError = e instanceof ApiError ? e.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  /** The editable part of a kind: everything for own kinds, project additions for core kinds. */
  private own(k: Kind): KindInput {
    const strip = <T extends { project?: boolean }>(xs: T[] = []) => xs.filter((x) => k.source === "project" || x.project).map(({ project: _, ...x }) => x as T);
    return k.source === "project"
      ? { name: k.name, description: k.description, icon: k.icon, attrs: strip(k.attrs), relations: strip(k.relations) }
      : { attrs: strip(k.attrs), relations: strip(k.relations) };
  }

  private addAttr(e: SubmitEvent, k: Kind) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const label = String(f.get("label") ?? "").trim();
    const a: Attr = { name: toID(label), label, type: this.addType, required: f.get("required") === "on" };
    if (a.type === "number" && f.get("unit")) a.unit = String(f.get("unit")).trim();
    if (a.type === "select") a.options = String(f.get("options") ?? "").split(",").map((s) => s.trim()).filter(Boolean);
    const input = this.own(k);
    this.save(k.id, { ...input, attrs: [...(input.attrs ?? []), a] }, () => { form.reset(); this.addType = "text"; });
  }

  private removeAttr(k: Kind, name: string) {
    const input = this.own(k);
    this.save(k.id, { ...input, attrs: input.attrs?.filter((a) => a.name !== name) });
  }

  private addRel(e: SubmitEvent, k: Kind) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const rel = toID(String(f.get("rel") ?? ""));
    const to = String(f.get("to") ?? "");
    const input = this.own(k);
    const rels = [...(input.relations ?? [])];
    const same = rels.find((r) => r.rel === rel);
    if (same) same.to = [...new Set([...same.to, to])];
    else rels.push({ rel, to: [to] });
    this.save(k.id, { ...input, relations: rels }, () => form.reset());
  }

  private removeRel(k: Kind, rel: string, to: string) {
    const input = this.own(k);
    const rels = (input.relations ?? []).map((r) => (r.rel === rel ? { ...r, to: r.to.filter((x) => x !== to) } : r)).filter((r) => r.to.length);
    this.save(k.id, { ...input, relations: rels });
  }

  private createKind(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const name = String(f.get("name") ?? "").trim();
    const id = String(f.get("id") || toID(name));
    this.save(id, { name, description: String(f.get("description") ?? ""), icon: String(f.get("icon") ?? "Shapes"), attrs: [], relations: [] },
      () => (this.creating = false));
  }

  private editKind(e: SubmitEvent, k: Kind) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.save(k.id, { ...this.own(k), name: String(f.get("name") ?? ""), description: String(f.get("description") ?? ""), icon: String(f.get("icon") ?? k.icon) });
  }

  private async deleteKind(k: Kind) {
    try {
      await api("DELETE", `/projects/${this.project}/kinds/${k.id}`);
      this.model = await loadModel(this.project, true);
      this.kind = "";
      this.syncURL();
    } catch (e) {
      this.formError = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private iconPicker(current: string) {
    return html`<div class="icons" role="radiogroup" aria-label=${t("model.icon")}>
      ${kindIcons.map((i) => html`<label title=${i}><input type="radio" name="icon" value=${i} ?checked=${i === current}>${icon(i, 16)}</label>`)}
    </div>`;
  }

  private kindChip(id: string, current: string) {
    if (id === "*") return html`<span class="k">${t("model.any_kind")}</span>`;
    const k = findKind(this.model, id);
    const c = groupColor[k?.group ?? "custom"] ?? "var(--accent)";
    const inner = html`<span class="kic" style="--c:${c};width:20px;height:20px">${icon(k?.icon ?? "Shapes", 12)}</span>${kindName(k, id)}`;
    return id === current ? html`<span class="k">${inner}</span>` : html`<a class="k" @click=${() => this.select(id)}>${inner}</a>`;
  }

  private relSection(k: Kind, edit: boolean) {
    const m = this.model!;
    const lines = relationLines(m, k);
    const defined = lines.filter((l) => l.defined), extra = lines.filter((l) => !l.defined);
    const relNames = [...new Set(m.kinds.flatMap((x) => x.relations ?? []).map((r) => r.rel).concat(m.generic))];
    const line = (l: RelLine) => html`<li>
      <span class="sent">${this.kindChip(l.from, k.id)}<span class="rel">${icon("ArrowRight", 14)} ${relLabel(l.rel)} ${icon("ArrowRight", 14)}</span>${this.kindChip(l.to, k.id)}</span>
      ${l.count ? html`<a class="cnt" href="/p/${this.project}/graph?kind=${l.from === "*" ? k.id : l.from}" title=${t("dm.used_title", { n: l.count })}>${t("dm.used", { n: l.count })}</a>` : html`<span class="cnt">${t("dm.unused")}</span>`}
      <span>${edit && l.own && l.from === k.id ? html`<button class="ghost" style="padding:2px 6px" aria-label=${t("model.remove")} @click=${() => this.removeRel(k, l.rel, l.to)}>${icon("X", 13)}</button>` : ""}</span>
    </li>`;
    return html`<section aria-labelledby="h-rel">
      <h3 id="h-rel">${icon("Waypoints", 16)} ${t("model.relations")} <span class="muted">${t("dm.rel_hint")}</span></h3>
      ${defined.length ? html`<ul class="rels">${defined.map(line)}</ul>` : html`<p class="muted" style="margin:0">${t("dm.no_rels")}</p>`}
      ${extra.length ? html`<div><div class="lbl" style="padding:6px 0 0">${t("dm.rel_generic")}</div><ul class="rels">${extra.map(line)}</ul></div>` : ""}
      <p class="sub">${t("model.generic_hint", { rels: m.generic.map(relLabel).join(", ") })}</p>
      ${edit ? html`<form class="add" @submit=${(e: SubmitEvent) => this.addRel(e, k)}>
        <div class="sentence">${this.kindChip(k.id, k.id)}
          <input name="rel" required list="relnames" maxlength="40" autocomplete="off" placeholder=${t("dm.rel_ph")} aria-label=${t("model.rel_name")}>
          ${icon("ArrowRight", 14)}
          <select name="to" aria-label=${t("model.rel_target")}>
            <option value="*">${t("model.any_kind")}</option>
            ${m.kinds.map((o) => html`<option value=${o.id}>${kindName(o)}</option>`)}</select>
          <button class="ghost" ?disabled=${this.busy}>${icon("Plus", 13)} ${t("model.add_rel")}</button>
        </div>
        <datalist id="relnames">${relNames.map((r) => html`<option value=${r}>${relLabel(r)}</option>`)}</datalist>
      </form>` : ""}
    </section>`;
  }

  private fieldSection(k: Kind, edit: boolean) {
    return html`<section aria-labelledby="h-fields">
      <h3 id="h-fields">${icon("Rows3", 16)} ${t("model.attributes")}</h3>
      ${k.attrs.length ? html`<table class="fields">
        <thead><tr><th>${t("dm.col_field")}</th><th>${t("model.attr_type")}</th><th></th><th></th></tr></thead>
        <tbody>${k.attrs.map((a) => html`<tr>
          <td>${attrLabel(a)}${a.project && k.source !== "project" ? html`<span class="tag">${t("model.project_attr")}</span>` : ""}</td>
          <td class="muted">${t(`attrtype.${a.type}`)}${a.unit ? ` · ${a.unit}` : ""}${a.options ? html`<br><small>${a.options.join(", ")}</small>` : ""}</td>
          <td class="muted">${a.required ? t("model.required") : ""}</td>
          <td class="x">${edit && (a.project || k.source === "project") ? html`<button class="ghost" title=${t("model.remove")} aria-label=${t("model.remove_attr", { attr: attrLabel(a) })} @click=${() => this.removeAttr(k, a.name)}>${icon("X", 13)}</button>` : ""}</td>
        </tr>`)}</tbody></table>` : html`<p class="muted" style="margin:0">${t("model.no_attrs")}</p>`}
      ${edit ? html`<form class="add" @submit=${(e: SubmitEvent) => this.addAttr(e, k)}>
        <label class="field">${t("model.attr_label")}<input name="label" required maxlength="60" autocomplete="off"></label>
        <label class="field">${t("model.attr_type")}<select name="type" @change=${(e: Event) => (this.addType = (e.target as HTMLSelectElement).value as AttrType)}>
          ${attrTypes.map((ty) => html`<option value=${ty} ?selected=${ty === this.addType}>${t(`attrtype.${ty}`)}</option>`)}</select></label>
        ${this.addType === "number" ? html`<label class="field">${t("model.unit")}<input name="unit" maxlength="12" placeholder="mm, W, GB …"></label>` : ""}
        ${this.addType === "select" ? html`<label class="field">${t("model.options")}<input name="options" required placeholder=${t("model.options_hint")}></label>` : ""}
        <label class="check"><input type="checkbox" name="required"> ${t("model.required")}</label>
        <button class="ghost" ?disabled=${this.busy}>${icon("Plus", 13)} ${t("model.add_attr")}</button>
      </form>` : ""}
    </section>`;
  }

  private sheet(k: Kind) {
    const c = groupColor[k.group] ?? "var(--accent)";
    const fromPlugin = k.source.startsWith("plugin:");
    const edit = this.canEdit && !fromPlugin;
    return html`<article class="sheet" aria-labelledby="h-kind">
      <header>
        <span class="kic big" style="--c:${c}">${icon(k.icon, 22)}</span>
        <div class="t"><h2 id="h-kind">${kindName(k)}</h2><code class="muted">${k.id}</code></div>
        <div class="chips">
          <span class="chip">${fromPlugin ? html`${icon("Puzzle", 12)} ${t("model.source_plugin", { plugin: k.source.slice(7) })}` : t(k.source === "core" ? "model.source_core" : "model.source_project")}</span>
          ${k.count ? html`<a class="chip" href="/p/${this.project}/graph?kind=${k.id}">${t("model.count", { count: k.count })} ${icon("ArrowRight", 12)}</a>` : html`<span class="chip">${t("model.count", { count: 0 })}</span>`}
        </div>
      </header>
      ${k.description ? html`<p class="desc">${k.description}</p>` : ""}
      ${this.fieldSection(k, edit)}
      ${this.relSection(k, edit)}
      <section><phx-kind-rules .project=${this.project} .kind=${k} @changed=${() => loadModel(this.project, true).then((m) => (this.model = m))}></phx-kind-rules></section>
      ${edit && k.source === "project" ? html`<section><details><summary>${t("model.edit_kind")}</summary>
        <form class="stack" @submit=${(e: SubmitEvent) => this.editKind(e, k)} style="margin-top:10px">
          <label class="field">${t("model.kind_name")}<input name="name" required maxlength="60" .value=${k.name ?? ""}></label>
          <label class="field">${t("model.description")}<input name="description" maxlength="500" .value=${k.description ?? ""}></label>
          ${this.iconPicker(k.icon)}
          <div class="row"><button class="ghost" ?disabled=${this.busy}>${t("model.save")}</button>
            <button type="button" class="danger" ?disabled=${k.count > 0} title=${k.count ? t("validation.kind_in_use", { count: k.count }) : ""} @click=${() => this.deleteKind(k)}>${icon("Trash2", 13)} ${t("model.delete_kind")}</button></div>
        </form></details></section>` : ""}
      ${this.formError ? html`<section><p class="error" role="alert" style="margin:0">${this.formError}</p></section>` : ""}
    </article>`;
  }

  private newKindForm() {
    return html`<article class="sheet"><header><span class="kic big" style="--c:var(--accent)">${icon("Plus", 22)}</span>
        <div class="t"><h2>${t("model.new_kind")}</h2></div></header>
      <section><form class="stack" @submit=${this.createKind}>
        <label class="field">${t("model.kind_name")}<input name="name" required maxlength="60" autocomplete="off"></label>
        <label class="field">${t("model.description")}<input name="description" maxlength="500"></label>
        <span class="field" style="font-size:13px;color:var(--muted)">${t("model.icon")}</span>
        ${this.iconPicker("Shapes")}
        ${this.formError ? html`<p class="error" role="alert">${this.formError}</p>` : ""}
        <div class="row"><button ?disabled=${this.busy}>${icon("Plus", 14)} ${t("model.create_kind")}</button>
          <button type="button" class="ghost" @click=${() => (this.creating = false)}>${t("doc.cancel")}</button></div>
      </form></section>
    </article>`;
  }

  /** All kinds in use with the relations between their entries; arrows point from → to. */
  private overview() {
    const m = this.model!;
    const pairs = new Map<string, { from: string; to: string; rels: string[]; count: number }>();
    for (const u of m.used ?? []) {
      if (u.from === u.to) continue;
      const key = `${u.from}\u0000${u.to}`;
      const p = pairs.get(key) ?? { from: u.from, to: u.to, rels: [], count: 0 };
      p.rels.push(relLabel(u.rel));
      p.count += u.count;
      pairs.set(key, p);
    }
    const ids = [...new Set(m.kinds.filter((k) => k.count > 0).map((k) => k.id))];
    if (!ids.length) return html`<p class="muted">${t("dm.overview_empty")}</p>`;
    const links = [...pairs.values()].filter((p) => ids.includes(p.from) && ids.includes(p.to));
    const pos = layout(ids, links.map((l) => [l.from, l.to]), { width: 1400, height: 820, seed: 7, k: 300 });
    // The page is wide: turn a tall layout on its side so it fills the width.
    const raw = bounds(pos.values(), 0);
    if (raw.h > raw.w) for (const [id, q] of pos) pos.set(id, { x: q.y, y: q.x });
    const b = bounds(pos.values(), 90);
    // Sizes grow with the drawing so labels stay readable when it is scaled down.
    const f = Math.min(1.6, Math.max(1, b.w / 1000));
    const R = 24 * f;
    return html`<svg class="ov" viewBox="${b.x} ${b.y} ${b.w} ${b.h}" style="--f:${f.toFixed(2)}" role="img" aria-label=${t("dm.overview_label")}>
        <defs><marker id="arr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z"></path></marker></defs>
        ${links.map((l) => {
          const a = pos.get(l.from)!, z = pos.get(l.to)!;
          const dx = z.x - a.x, dy = z.y - a.y, d = Math.hypot(dx, dy) || 1;
          const ux = dx / d, uy = dy / d;
          // Two-way pairs are drawn slightly apart so both arrows stay visible.
          const back = pairs.has(`${l.to}\u0000${l.from}`) ? 6 : 0;
          const ox = -uy * back, oy = ux * back;
          const x1 = a.x + ux * R + ox, y1 = a.y + uy * R + oy, x2 = z.x - ux * (R + 4) + ox, y2 = z.y - uy * (R + 4) + oy;
          const label = `${[...new Set(l.rels)].join(", ")} · ${l.count}`;
          return svg`<line x1=${x1} y1=${y1} x2=${x2} y2=${y2} marker-end="url(#arr)"></line>
            <text class="el" x=${(x1 + x2) / 2 + ox * 2} y=${(y1 + y2) / 2 + oy * 2 - 4}>${label}</text>`;
        })}
        ${ids.map((id) => {
          const p = pos.get(id)!, k = findKind(m, id);
          const c = groupColor[k?.group ?? "custom"] ?? "var(--accent)";
          return svg`<g class="nd" style="--c:${c}" transform="translate(${p.x} ${p.y})" tabindex="0" role="button" aria-label=${kindName(k, id)}
            @click=${() => this.select(id)} @keydown=${(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), this.select(id))}>
            <circle r=${R}></circle><g class="ico" transform="translate(${-9 * f} ${-9 * f}) scale(${f})">${icon(k?.icon ?? "Shapes", 18)}</g>
            <text y=${R + 17 * f}>${kindName(k, id)} (${k?.count ?? 0})</text></g>`;
        })}
      </svg>
      <p class="ovnote">${t("dm.overview_note")}</p>`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const m = this.model;
    if (!m) return html`<p class="muted">${t("app.loading")}</p>`;
    const sel = this.sel!;
    // Built-in kinds nobody uses are hidden until asked for.
    const used = (k: Kind) => k.count > 0 || k.source === "project" || k.id === sel.id;
    const visible = this.showAll ? m.kinds : m.kinds.filter(used);
    const hidden = m.kinds.length - m.kinds.filter(used).length;
    const groups = [...new Set(visible.map((k) => k.group))];
    return html`
      <div class="head"><p class="muted">${t("dm.intro")}</p>
        <div class="seg" role="group" aria-label=${t("model.view")}>
          <button aria-pressed=${this.view === "kinds"} @click=${() => this.setView("kinds")}>${t("dm.view_kinds")}</button>
          <button aria-pressed=${this.view === "overview"} @click=${() => this.setView("overview")}>${t("dm.view_overview")}</button>
        </div>
        ${this.canEdit ? html`<button class="ghost" @click=${() => { this.creating = true; this.view = "kinds"; this.formError = ""; }}>${icon("Plus", 14)} ${t("model.new_kind")}</button>` : ""}
      </div>
      ${this.view === "overview" ? this.overview() : html`<div class="grid">
        <nav class="list" aria-label=${t("model.kinds")}>
          ${groups.map((g) => html`<div><div class="lbl">${groupName(g)}</div>
            ${visible.filter((k) => k.group === g).map((k) => html`<button class=${k.id === sel.id && !this.creating ? "kind on" : "kind"}
              aria-current=${k.id === sel.id && !this.creating ? "true" : "false"} @click=${() => this.select(k.id)}>
              <span class="kic" style="--c:${groupColor[k.group]}">${icon(k.icon, 14)}</span><span>${kindName(k)}</span><small>${k.count || ""}</small></button>`)}
          </div>`)}
          ${m.unknown ? html`<div><div class="lbl">${t("model.unknown")}</div>
            ${Object.entries(m.unknown).map(([id, n]) => html`<div class="kind" style="cursor:default"><span class="kic" style="--c:var(--muted)">${icon("CircleAlert", 14)}</span><code>${id}</code><small>${n}</small></div>`)}
          </div>` : ""}
          ${hidden ? html`<button class="ghost more" aria-expanded=${this.showAll ? "true" : "false"} @click=${() => (this.showAll = !this.showAll)}>
            ${icon(this.showAll ? "ChevronDown" : "ChevronRight", 13)} ${this.showAll ? t("model.hide_unused") : t("model.show_unused", { count: hidden })}</button>` : ""}
        </nav>
        ${this.creating ? this.newKindForm() : this.sheet(sel)}
      </div>`}`;
  }
}
customElements.define("phx-data-model", PhxDataModel);
