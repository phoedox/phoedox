import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, svg } from "lit";
import { ApiError, api, type Doc, type Edge, type Entity } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { allowedRels, attrLabel, findKind, formatValue, groupColor, groupName, kindName, loadModel, relLabel, type Model } from "../model";
import { currentProject, docsVersion, navigate } from "../state";
import { pluginViews } from "../plugin-host";
import { unsafeStatic, html as shtml } from "lit/static-html.js";
import { base } from "./shared";
import "./attachments";
import "./diagnosis";
import { toast } from "./toasts";

type Detail = { entity: Entity; edges: Edge[]; documents: Doc[] };

/** Number of distinct neighbors of an entity. */
const neighborCount = (edges: Edge[], id: string) => new Set(edges.map((x) => (x.from === id ? x.to : x.from))).size;

/** An entry of the knowledge graph: values, relations, neighborhood and documents. */
export class PhxEntityView extends SignalWatcher(LitElement) {
  static properties = {
    project: {}, entity: {}, d: { state: true }, model: { state: true }, all: { state: true }, target: { state: true },
    error: { state: true }, relError: { state: true }, confirming: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    header { display: flex; gap: 14px; align-items: flex-start; flex-wrap: wrap; margin-bottom: 18px; }
    header .title { flex: 1; min-width: 220px; }
    .kic { display: inline-grid; place-items: center; width: 44px; height: 44px; border-radius: 11px; flex: none;
      color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent); }
    .cols { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; align-items: start; }
    section { display: flex; flex-direction: column; gap: 8px; }
    h2 { font-size: 16px; }
    dl { display: grid; grid-template-columns: max-content 1fr; gap: 6px 16px; margin: 0; font-size: 14px; }
    dt { color: var(--muted); }
    dd { margin: 0; overflow-wrap: anywhere; }
    ul { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 4px; }
    li.rel { display: flex; align-items: center; gap: 8px; font-size: 14px; }
    li.rel .rl { color: var(--muted); }
    li.rel button { margin-left: auto; padding: 2px 6px; }
    form.add { display: grid; grid-template-columns: 1fr 1fr auto; gap: 8px; align-items: end; padding: 10px; border: 1px dashed var(--line); border-radius: 10px; }
    svg.hood { width: 100%; max-width: 680px; margin-inline: auto; height: auto; max-height: min(520px, 60vh); display: block; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; }
    .n { cursor: pointer; }
    .n circle { fill: var(--surface); stroke: var(--c); stroke-width: 1.5; }
    .n.me circle { fill: color-mix(in srgb, var(--c) 16%, var(--surface)); stroke-width: 2.5; }
    .n text { font-size: 13px; fill: var(--fg); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    .n .ico { color: var(--c); }
    .n:focus { outline: none; }
    .n:focus-visible circle { stroke: var(--accent); stroke-width: 3; }
    @media (hover: hover) and (pointer: fine) { .n:hover circle { stroke-width: 2.5; } }
    line { stroke: var(--line); stroke-width: 1.5; }
    text.e.side { text-anchor: start; }
    text.e { font-size: 12px; fill: var(--muted); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    /* Plugin views hide themselves (hidden) when an entry is not their business. */
    section.plugin:has(> [hidden]) { display: none; }
    @media (max-width: 900px) { .cols { grid-template-columns: 1fr; } form.add { grid-template-columns: 1fr; } }
  `];
  project = "";
  entity = "";
  d?: Detail;
  model?: Model;
  /** All entries of the project, offered as relation targets. */
  all?: Entity[];
  /** Chosen target in the "add relation" form. */
  target = "";
  error = "";
  relError = "";
  confirming = false;
  busy = false;
  private loaded = "";

  private get canEdit() {
    return ["editor", "owner"].includes(currentProject.get()?.role ?? "");
  }

  async willUpdate() {
    if (this.loaded !== this.entity) {
      this.loaded = this.entity;
      this.confirming = false;
      this.target = "";
      await this.load();
    }
  }

  private async load() {
    try {
      const [d, m] = await Promise.all([
        api<Detail>("GET", `/projects/${this.project}/entities/${this.entity}`),
        loadModel(this.project),
      ]);
      if (this.canEdit) void this.loadTargets();
      d.edges ??= [];
      d.documents ??= [];
      this.d = d;
      this.model = m;
      this.error = "";
    } catch (e) {
      this.d = undefined;
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async loadTargets() {
    this.all = (await api<Entity[]>("GET", `/projects/${this.project}/entities`).catch(() => [])) ?? [];
  }

  private async addRelation(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement; // e.target is cleared after dispatch in shadow DOM
    const f = new FormData(form);
    const [dir, rel] = String(f.get("rel") ?? "").split(":");
    const me = this.d!.entity.id;
    const other = this.target;
    if (!rel || !other) return;
    this.busy = true;
    this.relError = "";
    try {
      const r = await api<{ applied: boolean }>("POST", `/projects/${this.project}/edges`, dir === "in" ? { from: other, rel, to: me } : { from: me, rel, to: other });
      if (!r.applied) toast(t("suggestion.created"));
      this.target = "";
      form.reset();
      await this.load();
    } catch (err) {
      this.relError = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async removeRelation(x: Edge) {
    try {
      const r = await api<{ applied: boolean }>("POST", `/projects/${this.project}/edges/remove`, { from: x.from, rel: x.rel, to: x.to });
      if (!r.applied) toast(t("suggestion.created"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async createDoc() {
    const e = this.d!.entity;
    try {
      const r = await api<{ applied: boolean; document?: Doc }>("POST", `/projects/${this.project}/documents`, { type: "markdown", title: e.name, markdown: `@${e.slug}\n\n` });
      docsVersion.set(docsVersion.get() + 1);
      if (r.document) navigate(`/p/${this.project}/d/${r.document.slug}/edit`);
      else toast(t("suggestion.created"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async removeEntity() {
    const e = this.d!.entity;
    const project = this.project;
    try {
      const r = await api<{ applied: boolean }>("DELETE", `/projects/${project}/entities/${e.slug}`);
      if (!r.applied) {
        toast(t("suggestion.created"));
        this.confirming = false;
        return;
      }
      navigate(`/p/${project}/model?kind=${e.kind}&view=entries`);
      toast(t("entity.trashed", { name: e.name }), "ok", {
        label: t("doc.undo"),
        run: async () => {
          try {
            await api("POST", `/projects/${project}/trash/${e.id}/restore`);
            navigate(`/p/${project}/e/${e.slug}`);
          } catch (err) {
            toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
          }
        },
      });
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  /** The entry with its direct neighbors around it. */
  private neighborhood(e: Entity, edges: Edge[]) {
    const count = neighborCount(edges, e.id);
    // The ring grows with the number of neighbours so labels keep their distance.
    const rx = count > 8 ? 230 + Math.min(count, 20) * 8 : 230;
    const W = 2 * rx + 180, H = count <= 2 ? 200 : count <= 3 ? 320 : count <= 8 ? 380 : Math.round(rx * 1.45), cy = H / 2;
    // A single neighbour: the pair sits in the middle.
    const cx = count === 1 ? W / 2 - rx / 2 : W / 2;
    const others = new Map<string, { slug: string; name: string; kind: string; labels: string[] }>();
    for (const x of edges) {
      const out = x.from === e.id;
      const id = out ? x.to : x.from;
      const o = others.get(id) ?? { slug: (out ? x.toSlug : x.fromSlug) ?? "", name: out ? x.toName : x.fromName, kind: (out ? x.toKind : x.fromKind) ?? "", labels: [] };
      // The same relation can come from several sources (e.g. both directions of related_to): show it once.
      // related_to has no direction.
      const label = `${out || x.rel === "related_to" ? "" : "← "}${relLabel(x.rel)}`;
      if (!o.labels.includes(label)) o.labels.push(label);
      others.set(id, o);
    }
    const list = [...others.values()];
    const node = (x: number, y: number, kind: string, name: string, me: boolean, slug: string) => {
      const k = findKind(this.model, kind);
      const r = me ? 28 : 22;
      const go = () => !me && navigate(`/p/${this.project}/e/${slug}`);
      return svg`<g class=${me ? "n me" : "n"} transform="translate(${x} ${y})" style="--c:${groupColor[k?.group ?? "custom"]}"
        tabindex=${me ? -1 : 0} role=${me ? "img" : "link"} aria-label=${name}
        @click=${go} @keydown=${(ev: KeyboardEvent) => ev.key === "Enter" && go()}>
        <circle r=${r}></circle><g transform="translate(-9 -9)">${icon(k?.icon ?? "Shapes", 18)}</g>
        <text y=${r + 15}>${name.length > 22 ? `${name.slice(0, 21)}…` : name}</text></g>`;
    };
    // One or two neighbours sit left and right (room for names and labels);
    // more go around a ring starting at the top.
    const pos = list.map((_, i) => {
      const a = list.length <= 2 ? i * Math.PI : (i / list.length) * Math.PI * 2 - Math.PI / 2;
      return [cx + Math.cos(a) * rx, cy + Math.sin(a) * (H / 2 - 55)] as const;
    });
    return html`<svg class="hood" viewBox="0 0 ${W} ${H}" role="group" aria-label=${t("entity.neighborhood")}>
      ${list.map((o, i) => {
        // Neighbours alternate the label position so labels do not meet near the middle.
        const f = list.length > 6 && i % 2 ? 0.45 : 0.55;
        const dx = pos[i][0] - cx, dy = pos[i][1] - cy;
        // Labels sit beside the line: above flat lines, right of steep ones.
        const steep = Math.abs(dy) > Math.abs(dx);
        const lx = cx + dx * f + (steep ? 10 : 0), ly = cy + dy * f + (steep ? 4 : -9) - ((o.labels.length - 1) * 14) / 2;
        return svg`<line x1=${cx} y1=${cy} x2=${pos[i][0]} y2=${pos[i][1]}></line>
          <text class=${steep ? "e side" : "e"} x=${lx} y=${ly}>${o.labels.map((l, j) => svg`<tspan x=${lx} dy=${j ? 14 : 0}>${l}</tspan>`)}</text>`;
      })}
      ${list.map((o, i) => node(pos[i][0], pos[i][1], o.kind, o.name, false, o.slug))}
      ${node(cx, cy, e.kind, e.name, true, e.slug)}
    </svg>`;
  }

  private relForm(e: Entity) {
    const m = this.model!;
    const targets = (this.all ?? []).filter((x) => x.id !== e.id);
    const target = targets.find((x) => x.id === this.target);
    const out = target ? allowedRels(m, e.kind, target.kind) : [];
    const inc = target ? allowedRels(m, target.kind, e.kind).filter((r) => !m.generic.includes(r)) : [];
    const groups = [...new Set(targets.map((x) => x.kind))];
    return html`<form class="add" @submit=${this.addRelation}>
      <label class="field">${t("entity.rel_target")}<select name="target" required @change=${(ev: Event) => (this.target = (ev.target as HTMLSelectElement).value)}>
        <option value="">${t("entity.pick_target")}</option>
        ${groups.map((g) => html`<optgroup label=${kindName(findKind(m, g), g)}>${targets.filter((x) => x.kind === g).map((x) => html`<option value=${x.id}>${x.name}</option>`)}</optgroup>`)}
      </select></label>
      <label class="field">${t("entity.rel_kind")}<select name="rel" required ?disabled=${!target}>
        ${out.map((r) => html`<option value="out:${r}">→ ${relLabel(r)}</option>`)}
        ${inc.map((r) => html`<option value="in:${r}">← ${relLabel(r)}</option>`)}
      </select></label>
      <button class="ghost" ?disabled=${!target || this.busy}>${icon("Plus", 13)} ${t("entity.add_rel")}</button>
    </form>
    ${this.relError ? html`<p class="error" role="alert">${this.relError}</p>` : ""}`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    if (!this.d || !this.model) return html`<p class="muted">${t("app.loading")}</p>`;
    const { entity: e, edges, documents } = this.d;
    const k = findKind(this.model, e.kind);
    const values = (k?.attrs ?? []).filter((a) => e.attrs?.[a.name] !== undefined && e.attrs?.[a.name] !== "");
    const extra = Object.entries(e.attrs ?? {}).filter(([n]) => !k?.attrs.some((a) => a.name === n));
    const edit = this.canEdit;
    return html`<article class="enter">
      <header>
        <span class="kic" style="--c:${groupColor[k?.group ?? "custom"]}">${icon(k?.icon ?? "CircleAlert", 22)}</span>
        <div class="title"><h1>${e.name}</h1>
          <p class="row" style="margin:6px 0 0">
            <a class="chip" href="/p/${this.project}/model?kind=${e.kind}">${kindName(k, e.kind)}${k ? ` · ${groupName(k.group)}` : ""}</a>
            <span class="chip mono" title=${t("entity.mention_hint")}>@${e.slug}</span></p></div>
        ${edit ? html`<div class="row">
          <a class="btn ghost" href="/p/${this.project}/e/${e.slug}/edit">${icon("Pencil", 14)} ${t("entity.edit")}</a>
          <button class="ghost" @click=${this.createDoc}>${icon("FilePlus", 14)} ${t("entity.new_doc")}</button>
          ${this.confirming
            ? html`<span class="muted">${t("entity.delete_confirm")}</span><button class="danger" @click=${this.removeEntity}>${t("doc.delete")}</button><button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button>`
            : html`<button class="danger" @click=${() => (this.confirming = true)}>${icon("Trash2", 14)} ${t("doc.delete")}</button>`}
        </div>` : ""}
      </header>
      ${(pluginViews.get()[e.kind] ?? []).map((tag) => html`<section class="plugin card" style="margin-bottom:12px">${shtml`<${unsafeStatic(tag)} entity=${e.slug}></${unsafeStatic(tag)}>`}</section>`)}
      ${e.kind === "room" || e.kind === "floor" || edges.some((x) => x.from === e.id && ["uplink", "wired_to", "runs_on"].includes(x.rel))
        ? html`<phx-diagnosis .project=${this.project} .entity=${e.slug}></phx-diagnosis>` : ""}
      <div class="cols">
        <section>
          <h2>${t("entity.attrs")}</h2>
          ${values.length || extra.length ? html`<dl>
            ${values.map((a) => html`<dt>${attrLabel(a)}</dt><dd class=${a.type === "ip" ? "mono" : ""}>${a.type === "url" ? html`<a href=${String(e.attrs[a.name])} target="_blank" rel="noopener external">${String(e.attrs[a.name])}</a>` : formatValue(a, e.attrs[a.name])}</dd>`)}
            ${extra.map(([n, v]) => html`<dt><code>${n}</code></dt><dd class="mono">${formatValue(undefined, v)}</dd>`)}
          </dl>` : html`<p class="muted">${t("entity.none")}</p>`}
          <h2 style="margin-top:12px">${t("entity.mentioned_in")}</h2>
          ${documents.length ? html`<ul>${documents.map((d) => html`<li><a href="/p/${this.project}/d/${d.slug}">${icon("FileText", 14)} ${d.title}</a></li>`)}</ul>` : html`<p class="muted">${t("entity.none")}</p>`}
          <phx-attachments style="margin-top:12px" .project=${this.project} .entity=${e.id} .editable=${edit}></phx-attachments>
        </section>
        <section>
          <h2>${t("entity.relations")}</h2>
          ${edges.length ? this.neighborhood(e, edges) : ""}
          ${edges.length ? html`<ul>${edges.map((x) => {
            const out = x.from === e.id;
            return html`<li class="rel">${icon(out ? "ArrowRight" : "ArrowLeft", 13)}
              <span class="rl">${relLabel(x.rel)}</span>
              <a href="/p/${this.project}/e/${out ? x.toSlug : x.fromSlug}">${out ? x.toName : x.fromName}</a>
              ${edit ? html`<button class="ghost" aria-label=${t("entity.remove_rel")} title=${t("entity.remove_rel")} @click=${() => this.removeRelation(x)}>${icon("X", 13)}</button>` : ""}</li>`;
          })}</ul>` : html`<p class="muted">${t("entity.none")}</p>`}
          ${edit ? this.relForm(e) : ""}
        </section>
      </div>
    </article>`;
  }
}
customElements.define("phx-entity-view", PhxEntityView);
