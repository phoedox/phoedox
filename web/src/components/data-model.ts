import "./kind-rules";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, svg } from "lit";
import { repeat } from "lit/directives/repeat.js";
import { ApiError, api, type Entity, type Suggestion } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import {
  attrLabel, attrTypes, findKind, formatValue, groupColor, groupName, kindIcons, kindName, loadModel, relLabel, toID,
  type Attr, type AttrType, type Kind, type KindInput, type Model,
} from "../model";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type View = "schema" | "entries";
type KindResult = { applied: boolean; kind?: Kind; suggestion?: Suggestion };

/** Kinds of entities in a project: schema graph, entries and the kind's definition. */
export class PhxDataModel extends SignalWatcher(LitElement) {
  static properties = { showAll: { state: true }, hoverKind: { state: true },
    project: {}, kind: {}, model: { state: true }, view: { state: true }, entries: { state: true },
    error: { state: true }, formError: { state: true }, creating: { state: true }, addType: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    .head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
    .head h1 { margin-right: auto; }
    .grid { display: grid; grid-template-columns: 220px minmax(0, 1fr) 320px; gap: 16px; align-items: start; }
    .list { display: flex; flex-direction: column; gap: 12px; }
    .list .more { border: none; background: none; color: var(--muted); font-size: 13px; justify-content: flex-start; text-align: left; padding: 4px 8px; }
    @media (hover: hover) and (pointer: fine) { .list .more:hover { color: var(--fg); } }
    .lbl { font-size: 11px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); font-weight: 600; padding: 0 8px 4px; }
    .kind { all: unset; box-sizing: border-box; display: flex; align-items: center; gap: 8px; width: 100%; padding: 5px 8px; border-radius: 6px;
      font-size: 14px; color: var(--muted); cursor: pointer; transition: background-color var(--dur-fast) ease, color var(--dur-fast) ease; }
    .kind small { margin-left: auto; font-family: var(--font-mono); font-size: 11px; }
    .kind.on { background: var(--accent-soft); color: var(--fg); }
    .kind:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
    @media (hover: hover) and (pointer: fine) { .kind:hover { background: var(--surface2); color: var(--fg); } }
    .kic { display: inline-grid; place-items: center; width: 24px; height: 24px; border-radius: 6px; flex: none;
      color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent); }
    .kic.big { width: 40px; height: 40px; border-radius: 10px; }
    .center { min-width: 0; position: sticky; top: calc(var(--header-h) + 12px); }
    .seg { display: inline-flex; padding: 3px; gap: 2px; border-radius: 9px; background: var(--surface2); }
    .seg button { background: transparent; color: var(--muted); padding: 5px 12px; font-size: 13px; }
    .seg button[aria-pressed="true"] { background: var(--surface); color: var(--fg); box-shadow: 0 1px 2px rgb(0 0 0 / .08); }
    svg.schema { width: 100%; height: auto; max-height: min(560px, 64vh); display: block; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; }
    .node { cursor: pointer; transition: transform var(--dur-enter) var(--ease-out); }
    .node circle { fill: var(--surface); stroke: var(--c); stroke-width: 1.5; }
    .node.sel circle { fill: color-mix(in srgb, var(--c) 14%, var(--surface)); stroke-width: 2.5; }
    .node text { font-size: calc(14px * var(--fs, 1)); fill: var(--fg); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    .node:focus-visible { outline: none; }
    .node:focus-visible circle { stroke: var(--accent); stroke-width: 3; }
    .node .ico { color: var(--c); }
    .edges { transition: opacity var(--dur) var(--ease-out) 120ms; @starting-style { opacity: 0; } }
    .edges line { stroke: var(--line); stroke-width: 1.5; }
    .edges line.hl { stroke: var(--accent); }
    .edges text { font-size: calc(12.5px * var(--fs, 1)); fill: var(--muted); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; }
    th { text-align: left; font-size: 12px; color: var(--muted); font-weight: 600; padding: 6px 8px; border-bottom: 1px solid var(--line); }
    td { padding: 7px 8px; border-bottom: 1px solid var(--line); vertical-align: top; }
    td.nw { white-space: nowrap; }
    .tablewrap { overflow-x: auto; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; }
    .detail { display: flex; flex-direction: column; gap: 16px; }
    .dh { display: flex; gap: 12px; align-items: center; }
    .dh h2 { font-size: 20px; }
    .atbl td { padding: 5px 4px; font-size: 13px; }
    .atbl button { padding: 2px 6px; }
    .rels { display: flex; flex-direction: column; gap: 4px; }
    .rels .r { display: flex; align-items: center; gap: 8px; font-size: 13px; }
    .rels .r a { cursor: pointer; }
    form.add { display: grid; gap: 8px; padding: 12px; border: 1px dashed var(--line); border-radius: 10px; }
    form.add .two { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    label.check { display: flex; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); }
    label.check input { width: auto; }
    .icons { display: flex; flex-wrap: wrap; gap: 4px; }
    .icons label { display: inline-grid; place-items: center; width: 32px; height: 32px; border-radius: 8px; border: 1px solid var(--line); cursor: pointer; color: var(--muted); }
    .icons input { position: absolute; opacity: 0; pointer-events: none; }
    .icons label:has(input:checked) { border-color: var(--accent); color: var(--accent); background: var(--accent-soft); }
    .icons label:has(input:focus-visible) { outline: 2px solid var(--accent); outline-offset: 1px; }
    .tag { font-size: 10px; text-transform: uppercase; letter-spacing: .06em; color: var(--accent); margin-left: 4px; }
    @media (prefers-reduced-motion: reduce) { .node { transition: none; } }
    @media (max-width: 1150px) { .grid { grid-template-columns: 200px minmax(0, 1fr); } .detail { grid-column: 1 / -1; } }
    @media (max-width: 760px) {
      .grid { grid-template-columns: 1fr; }
      .center { position: static; }
      .list { flex-direction: row; overflow-x: auto; gap: 16px; padding-bottom: 6px; }
      .list > div { min-width: 180px; }
    }
  `];

  project = "";
  /** Selected kind from the URL (?kind=), may be empty. */
  kind = "";
  model?: Model;
  view: View = "schema";
  entries?: Entity[];
  error = "";
  formError = "";
  /** The "new kind" form is open. */
  creating = false;
  /** Also list built-in kinds without entries. */
  showAll = false;
  /** Kind under the pointer in the schema graph. */
  hoverKind = "";
  addType: AttrType = "text";
  busy = false;
  private loadedFor = "";
  private entriesFor = "";

  private get canEdit() {
    return ["editor", "owner"].includes(currentProject.get()?.role ?? "");
  }

  private get sel(): Kind | undefined {
    return findKind(this.model, this.kind) ?? this.model?.kinds.find((k) => k.count > 0) ?? this.model?.kinds[0];
  }

  async willUpdate() {
    if (this.loadedFor !== this.project) {
      this.loadedFor = this.project;
      this.view = new URLSearchParams(location.search).get("view") === "entries" ? "entries" : "schema";
      this.kind ||= new URLSearchParams(location.search).get("kind") ?? "";
      try {
        this.model = await loadModel(this.project, true);
      } catch (e) {
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
      }
    }
    const sel = this.sel;
    if (this.view === "entries" && sel && this.entriesFor !== sel.id) {
      this.entriesFor = sel.id;
      this.entries = undefined;
      this.entries = await api<Entity[]>("GET", `/projects/${this.project}/entities?kind=${encodeURIComponent(sel.id)}`).catch(() => []) ?? [];
    }
  }

  private select(id: string) {
    this.kind = id;
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
    if (this.view === "entries") q.set("view", "entries");
    history.replaceState(history.state, "", `${location.pathname}?${q}`);
  }

  /** Saves the project's part of a kind definition. */
  private async save(id: string, input: KindInput, after?: () => void) {
    this.busy = true;
    this.formError = "";
    try {
      const r = await api<KindResult>("PUT", `/projects/${this.project}/kinds/${id}`, input);
      if (r.kind) r.kind.attrs ??= [];
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

  /** Relations from and to the selected kind as [rel, other kind id, outgoing, project]. */
  private relationsOf(k: Kind): [string, string, boolean, boolean][] {
    const out: [string, string, boolean, boolean][] = [];
    for (const r of k.relations ?? []) for (const to of r.to) out.push([r.rel, to, true, !!r.project]);
    for (const o of this.model?.kinds ?? []) {
      if (o.id === k.id) continue;
      for (const r of o.relations ?? []) if (r.to.includes(k.id)) out.push([r.rel, o.id, false, !!r.project]);
    }
    return out;
  }

  private schemaGraph(k: Kind) {
    const rels = this.relationsOf(k);
    const others = [...new Set(rels.map(([, o]) => o))];
    const n = others.length;
    // The ring grows with the number of kinds; with many kinds the relation
    // names on the lines appear only for the kind under the pointer or focus
    // (the panel on the right lists all of them).
    const dense = n > 8;
    const rx = dense ? 200 + Math.min(n, 16) * 7 : n > 4 ? 245 : 190, ry = Math.round(rx * 0.74);
    const W = 2 * rx + 200, H = 2 * ry + 110, cx = W / 2, cy = H / 2;
    const pos = new Map<string, [number, number]>([[k.id, [cx, cy]]]);
    others.forEach((o, i) => {
      const a = (i / Math.max(n, 1)) * Math.PI * 2 - Math.PI / 2;
      pos.set(o, [cx + Math.cos(a) * rx, cy + Math.sin(a) * ry]);
    });
    const nodes = [k.id, ...others];
    // One line per relation; several relations to the same kind stack.
    const labelsFor = (o: string) => [...new Set(rels.filter(([, x]) => x === o).map(([rel, , outgoing]) => `${outgoing ? "" : "← "}${relLabel(rel)}`))];
    // Text grows a little with the drawing so it stays readable when scaled down.
    const fs = (dense ? W / 640 : 1).toFixed(2);
    return html`<svg class="schema" viewBox="0 0 ${W} ${H}" style="--fs:${fs}" role="img" aria-label=${t("model.schema_label", { kind: kindName(k) })}>
      ${repeat([k.id], (id) => id, () => svg`<g class="edges">${others.map((o, i) => {
        const [x, y] = pos.get(o)!;
        // Labels sit towards the outer kind; neighbours alternate so they do not meet near the middle.
        const f = 0.62;
        const shown = !dense || this.hoverKind === o;
        const lx = cx + (x - cx) * f, labels = labelsFor(o);
        const ly = cy + (y - cy) * f + 4 - ((labels.length - 1) * 15) / 2;
        return svg`<line class=${shown && dense ? "hl" : ""} x1=${cx} y1=${cy} x2=${x} y2=${y}></line>${shown ? svg`<text x=${lx} y=${ly}>${labels.map((l, j) => svg`<tspan x=${lx} dy=${j ? "1.2em" : 0}>${l}</tspan>`)}</text>` : ""}`;
      })}</g>`)}
      ${repeat(nodes, (id) => id, (id) => {
        const [x, y] = pos.get(id)!;
        const kk = findKind(this.model, id);
        const c = groupColor[kk?.group ?? "custom"] ?? "var(--accent)";
        const r = id === k.id ? 30 : 22;
        return svg`<g class=${id === k.id ? "node sel" : "node"} style="transform: translate(${x}px, ${y}px); --c: ${c}" tabindex="0" role="button"
          aria-label=${id === "*" ? t("model.any_kind") : kindName(kk, id)}
          @pointerenter=${() => (this.hoverKind = id)} @pointerleave=${() => (this.hoverKind = "")} @focus=${() => (this.hoverKind = id)} @blur=${() => (this.hoverKind = "")}
          @click=${() => id !== "*" && this.select(id)} @keydown=${(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && id !== "*" && (e.preventDefault(), this.select(id))}>
          <circle r=${r}></circle>
          <g transform="translate(-9 -9)">${icon(id === "*" ? "Shapes" : kk?.icon ?? "Shapes", 18)}</g>
          <text y=${r + 17}>${id === "*" ? t("model.any_kind") : kindName(kk, id)}</text>
        </g>`;
      })}
    </svg>`;
  }

  private entriesTable(k: Kind) {
    if (!this.entries) return html`<p class="muted">${t("app.loading")}</p>`;
    const cols = k.attrs.slice(0, 3);
    return html`<div class="row" style="margin-bottom:10px">
        ${this.canEdit ? html`<a class="btn" href="/p/${this.project}/new-entity?kind=${k.id}">${icon("Plus", 14)} ${t("entity.new")}</a>` : ""}
      </div>
      ${this.entries.length ? html`<div class="tablewrap"><table>
        <tr><th>${t("entity.name")}</th>${cols.map((a) => html`<th>${attrLabel(a)}</th>`)}<th>${t("entity.updated")}</th></tr>
        ${this.entries.map((e) => html`<tr>
          <td class="nw"><a href="/p/${this.project}/e/${e.slug}">${e.name}</a></td>
          ${cols.map((a) => html`<td class=${a.type === "ip" ? "mono" : ""}>${formatValue(a, e.attrs?.[a.name])}</td>`)}
          <td class="muted nw">${formatDate(e.updatedAt ?? "")}</td>
        </tr>`)}
      </table></div>` : html`<p class="muted">${t("model.no_entries")}</p>`}`;
  }

  private iconPicker(current: string) {
    return html`<div class="icons" role="radiogroup" aria-label=${t("model.icon")}>
      ${kindIcons.map((i) => html`<label title=${i}><input type="radio" name="icon" value=${i} ?checked=${i === current}>${icon(i, 16)}</label>`)}
    </div>`;
  }

  private newKindForm() {
    return html`<div class="detail card">
      <h2>${t("model.new_kind")}</h2>
      <form class="add" @submit=${this.createKind}>
        <label class="field">${t("model.kind_name")}<input name="name" required maxlength="60" autocomplete="off"></label>
        <label class="field">${t("model.description")}<input name="description" maxlength="500"></label>
        <span class="field" style="font-size:13px;color:var(--muted)">${t("model.icon")}</span>
        ${this.iconPicker("Shapes")}
        ${this.formError ? html`<p class="error" role="alert">${this.formError}</p>` : ""}
        <div class="row"><button ?disabled=${this.busy}>${icon("Plus", 14)} ${t("model.create_kind")}</button>
          <button type="button" class="ghost" @click=${() => (this.creating = false)}>${t("doc.cancel")}</button></div>
      </form>
    </div>`;
  }

  private detail(k: Kind) {
    const c = groupColor[k.group] ?? "var(--accent)";
    const fromPlugin = k.source.startsWith("plugin:");
    const edit = this.canEdit && !fromPlugin;
    const rels = this.relationsOf(k);
    const relNames = [...new Set(this.model!.kinds.flatMap((x) => x.relations ?? []).map((r) => r.rel).concat(this.model!.generic))];
    return html`<div class="detail card">
      <div class="dh"><span class="kic big" style="--c:${c}">${icon(k.icon, 20)}</span>
        <div><h2>${kindName(k)}</h2><code class="muted">${k.id}</code></div></div>
      <p class="row"><span class="chip">${fromPlugin ? html`${icon("Puzzle", 12)} ${t("model.source_plugin", { plugin: k.source.slice(7) })}` : t(k.source === "core" ? "model.source_core" : "model.source_project")}</span>
        <span class="chip">${t("model.count", { count: k.count })}</span></p>
      ${k.description ? html`<p class="muted" style="margin:0">${k.description}</p>` : ""}

      <div><div class="lbl" style="padding:0 0 4px">${t("model.attributes")}</div>
        ${k.attrs.length ? html`<table class="atbl">${k.attrs.map((a) => html`<tr>
          <td>${attrLabel(a)}${a.project && k.source === "core" ? html`<span class="tag">${t("model.project_attr")}</span>` : ""}</td>
          <td class="muted">${t(`attrtype.${a.type}`)}${a.unit ? ` · ${a.unit}` : ""}${a.options ? html`<br><small>${a.options.join(", ")}</small>` : ""}</td>
          <td class="muted">${a.required ? t("model.required") : ""}</td>
          <td>${edit && (a.project || k.source === "project") ? html`<button class="ghost" title=${t("model.remove")} aria-label=${t("model.remove_attr", { attr: attrLabel(a) })} @click=${() => this.removeAttr(k, a.name)}>${icon("X", 13)}</button>` : ""}</td>
        </tr>`)}</table>` : html`<p class="muted">${t("model.no_attrs")}</p>`}
        ${edit ? html`<form class="add" @submit=${(e: SubmitEvent) => this.addAttr(e, k)} style="margin-top:8px">
          <div class="two">
            <label class="field">${t("model.attr_label")}<input name="label" required maxlength="60" autocomplete="off"></label>
            <label class="field">${t("model.attr_type")}<select name="type" @change=${(e: Event) => (this.addType = (e.target as HTMLSelectElement).value as AttrType)}>
              ${attrTypes.map((ty) => html`<option value=${ty} ?selected=${ty === this.addType}>${t(`attrtype.${ty}`)}</option>`)}</select></label>
          </div>
          ${this.addType === "number" ? html`<label class="field">${t("model.unit")}<input name="unit" maxlength="12" placeholder="mm, W, GB …"></label>` : ""}
          ${this.addType === "select" ? html`<label class="field">${t("model.options")}<input name="options" required placeholder=${t("model.options_hint")}></label>` : ""}
          <label class="check"><input type="checkbox" name="required"> ${t("model.required")}</label>
          <button class="ghost" ?disabled=${this.busy}>${icon("Plus", 13)} ${t("model.add_attr")}</button>
        </form>` : ""}
      </div>

      <phx-kind-rules .project=${this.project} .kind=${k} @changed=${() => loadModel(this.project, true).then((m) => (this.model = m))}></phx-kind-rules>

      <div><div class="lbl" style="padding:0 0 4px">${t("model.relations")}</div>
        <div class="rels">${rels.map(([rel, other, outgoing, project]) => html`<div class="r">
          ${icon(outgoing ? "ArrowRight" : "ArrowLeft", 13)}
          <span><span class="muted">${relLabel(rel)}</span> ${outgoing ? "→" : "←"}
            ${other === "*" ? t("model.any_kind") : html`<a @click=${() => this.select(other)}>${kindName(findKind(this.model, other), other)}</a>`}</span>
          ${edit && outgoing && (project || k.source === "project") ? html`<button class="ghost" style="margin-left:auto;padding:2px 6px" aria-label=${t("model.remove")} @click=${() => this.removeRel(k, rel, other)}>${icon("X", 13)}</button>` : ""}
        </div>`)}
        <p class="muted" style="font-size:12px;margin:4px 0 0">${t("model.generic_hint", { rels: this.model!.generic.map(relLabel).join(", ") })}</p></div>
        ${edit ? html`<form class="add" @submit=${(e: SubmitEvent) => this.addRel(e, k)} style="margin-top:8px">
          <div class="two">
            <label class="field">${t("model.rel_name")}<input name="rel" required list="relnames" maxlength="40" autocomplete="off" placeholder="located_in"></label>
            <label class="field">${t("model.rel_target")}<select name="to">
              <option value="*">${t("model.any_kind")}</option>
              ${this.model!.kinds.map((o) => html`<option value=${o.id}>${kindName(o)}</option>`)}</select></label>
          </div>
          <datalist id="relnames">${relNames.map((r) => html`<option value=${r}>${relLabel(r)}</option>`)}</datalist>
          <button class="ghost" ?disabled=${this.busy}>${icon("Plus", 13)} ${t("model.add_rel")}</button>
        </form>` : ""}
      </div>

      ${edit && k.source === "project" ? html`<details><summary>${t("model.edit_kind")}</summary>
        <form class="add" @submit=${(e: SubmitEvent) => this.editKind(e, k)} style="margin-top:8px">
          <label class="field">${t("model.kind_name")}<input name="name" required maxlength="60" .value=${k.name ?? ""}></label>
          <label class="field">${t("model.description")}<input name="description" maxlength="500" .value=${k.description ?? ""}></label>
          ${this.iconPicker(k.icon)}
          <div class="row"><button class="ghost" ?disabled=${this.busy}>${t("model.save")}</button>
            <button type="button" class="danger" ?disabled=${k.count > 0} title=${k.count ? t("validation.kind_in_use", { count: k.count }) : ""} @click=${() => this.deleteKind(k)}>${icon("Trash2", 13)} ${t("model.delete_kind")}</button></div>
        </form></details>` : ""}
      ${this.formError ? html`<p class="error" role="alert">${this.formError}</p>` : ""}
    </div>`;
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
    return html`<div class="enter">
      <div class="head"><h1>${t("model.title")}</h1>
        <div class="seg" role="group" aria-label=${t("model.view")}>
          <button aria-pressed=${this.view === "schema"} @click=${() => this.setView("schema")}>${t("model.view_schema")}</button>
          <button aria-pressed=${this.view === "entries"} @click=${() => this.setView("entries")}>${t("model.view_entries")}</button>
        </div>
        <a class="btn ghost" href="/p/${this.project}/graph">${icon("Waypoints", 14)} ${t("nav.graph")}</a>
      </div>
      <p class="muted" style="margin-top:0">${t("model.intro")}</p>
      <div class="grid">
        <nav class="list" aria-label=${t("model.kinds")}>
          ${groups.map((g) => html`<div><div class="lbl">${groupName(g)}</div>
            ${visible.filter((k) => k.group === g).map((k) => html`<button class=${k.id === sel.id && !this.creating ? "kind on" : "kind"}
              aria-current=${k.id === sel.id ? "true" : "false"} @click=${() => this.select(k.id)}>
              <span class="kic" style="--c:${groupColor[k.group]}">${icon(k.icon, 14)}</span><span>${kindName(k)}</span><small>${k.count || ""}</small></button>`)}
          </div>`)}
          ${m.unknown ? html`<div><div class="lbl">${t("model.unknown")}</div>
            ${Object.entries(m.unknown).map(([id, n]) => html`<div class="kind" style="cursor:default"><span class="kic" style="--c:var(--muted)">${icon("CircleAlert", 14)}</span><code>${id}</code><small>${n}</small></div>`)}
          </div>` : ""}
          ${hidden ? html`<button class="ghost more" aria-expanded=${this.showAll ? "true" : "false"} @click=${() => (this.showAll = !this.showAll)}>
            ${icon(this.showAll ? "ChevronDown" : "ChevronRight", 13)} ${this.showAll ? t("model.hide_unused") : t("model.show_unused", { count: hidden })}</button>` : ""}
          ${this.canEdit ? html`<button class="ghost" @click=${() => { this.creating = true; this.formError = ""; }}>${icon("Plus", 13)} ${t("model.new_kind")}</button>` : ""}
        </nav>
        <section class="center" aria-live="polite">${this.view === "schema" ? this.schemaGraph(sel) : this.entriesTable(sel)}</section>
        ${this.creating ? this.newKindForm() : this.detail(sel)}
      </div>
    </div>`;
  }
}
customElements.define("phx-data-model", PhxDataModel);
