import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { parseCSV, toCSV } from "../csv";
import { hasKey, t } from "../i18n";
import { icon } from "../icon";
import { attrLabel, findKind, formatValue, kindName, loadModel, relLabel, type Kind, type Model } from "../model";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

export interface Cond { field: string; op: string; value?: unknown }
export interface RelCond { rel: string; in?: boolean; to?: string; kind?: string; missing?: boolean }
export interface GraphQuery { kind: string; where?: Cond[]; rels?: RelCond[]; columns?: string[]; sort?: string; limit?: number }
export interface QueryRow { slug: string; name: string; kind: string; attrs: Record<string, unknown> }
export interface QueryResult { kind: string; columns: string[]; rows: QueryRow[]; total: number }
interface Saved { id: string; slug: string; name: string; def: GraphQuery }
interface BulkResult { created: number; updated: number; deleted: number; suggestions: number; errors: { row: number; ref?: string; key: string; params?: Record<string, unknown> }[] }
const OPS = ["contains", "eq", "ne", "empty", "set", "gt", "lt"];
type Key = Parameters<typeof t>[0];

const errText = (e: { key: string; params?: Record<string, unknown> }) => (hasKey(e.key) ? t(e.key as Key, e.params ?? {}) : t("error.unknown"));

/** Entries of one kind as a table: filters, saved queries, inline and bulk editing, CSV export and import. */
export class PhxEntityTable extends LitElement {
  static properties = {
    project: {}, model: { state: true }, query: { state: true }, result: { state: true }, saved: { state: true }, selected: { state: true },
    importRows: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    :host { display: block; padding: 24px 16px 60px; }
    .bar { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; margin-bottom: 10px; }
    .bar select, .bar input { width: auto; }
    .cond { display: inline-flex; gap: 4px; align-items: center; border: 1px solid var(--line); border-radius: 8px; padding: 2px 4px 2px 8px; background: var(--surface); font-size: 13px; }
    .wrap { overflow: auto; border: 1px solid var(--line); border-radius: 10px; max-height: 70vh; }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th, td { border-bottom: 1px solid var(--line); padding: 4px 8px; text-align: left; white-space: nowrap; }
    th { position: sticky; top: 0; background: var(--surface); z-index: 1; cursor: pointer; }
    td input { border: 1px solid transparent; background: transparent; padding: 2px 4px; min-width: 90px; width: 100%; }
    td input:hover, td input:focus { border-color: var(--line); background: var(--bg); }
    tr.sel td { background: var(--accent-soft); }
    .bulk { position: sticky; bottom: 0; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; padding: 8px 10px; margin-top: 8px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
    .bulk select, .bulk input { width: auto; }
    .imp { border: 1px dashed var(--line); border-radius: 10px; padding: 10px 12px; margin-bottom: 10px; display: grid; gap: 6px; }
  `];
  project = "";
  model?: Model;
  query: GraphQuery = { kind: "" };
  result?: QueryResult;
  saved: Saved[] = [];
  selected = new Set<string>();
  importRows?: string[][];
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    this.model = await loadModel(this.project, true);
    this.saved = (await api<Saved[]>("GET", `/projects/${this.project}/queries`).catch(() => [])) ?? [];
    const params = new URLSearchParams(location.search);
    const fromSaved = this.saved.find((s) => s.slug === params.get("query"));
    const used = this.model.kinds.filter((k) => k.count > 0);
    this.query = fromSaved?.def ?? { kind: params.get("kind") ?? used[0]?.id ?? this.model.kinds[0]?.id ?? "" };
    await this.run();
  }

  private get canEdit() {
    return ["editor", "owner"].includes(currentProject.get()?.role ?? "");
  }

  private get kind(): Kind | undefined {
    return findKind(this.model, this.query.kind);
  }

  private async run() {
    if (!this.query.kind) return;
    this.selected = new Set();
    try {
      this.result = await api<QueryResult>("POST", `/projects/${this.project}/query`, { ...this.query, columns: undefined, limit: 2000 });
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private setQuery(q: GraphQuery) {
    this.query = q;
    this.run();
  }

  private addCond(e: Event) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const op = String(f.get("op"));
    this.setQuery({ ...this.query, where: [...(this.query.where ?? []), { field: String(f.get("field")), op, value: ["empty", "set"].includes(op) ? undefined : String(f.get("value") ?? "") }] });
    (e.target as HTMLFormElement).reset();
  }

  private async bulk(body: unknown, ok = true): Promise<BulkResult | undefined> {
    this.busy = true;
    try {
      const r = await api<BulkResult>("POST", `/projects/${this.project}/entities/bulk`, body);
      if (r.errors.length) toast(r.errors.slice(0, 3).map((x) => `${x.ref ?? `#${x.row + 1}`}: ${errText(x)}`).join(" · "), "error");
      else if (ok) toast(t("table.bulk_done", { created: r.created, updated: r.updated, deleted: r.deleted }), "ok");
      if (r.suggestions) toast(t("suggestion.created"));
      return r;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private typed(field: string, raw: string): unknown {
    const a = this.kind?.attrs.find((x) => x.name === field);
    const v = raw.trim();
    if (v === "") return null;
    if (a?.type === "number") return Number.isFinite(Number(v.replace(",", "."))) ? Number(v.replace(",", ".")) : v;
    if (a?.type === "bool") return ["true", "yes", "ja", "1", "x"].includes(v.toLowerCase());
    return v;
  }

  private async editCell(row: QueryRow, field: string, value: string) {
    const v = this.typed(field, value);
    if (String(row.attrs[field] ?? "") === String(v ?? "")) return;
    const r = await this.bulk({ set: { slugs: [row.slug], attrs: { [field]: v } } }, false);
    if (r && !r.errors.length) row.attrs[field] = v ?? undefined;
  }

  private async bulkSet(e: Event) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const field = String(f.get("field"));
    await this.bulk({ set: { slugs: [...this.selected], attrs: { [field]: this.typed(field, String(f.get("value") ?? "")) } } });
    await this.run();
  }

  private async bulkDelete() {
    if (!confirm(t("table.delete_confirm", { n: this.selected.size }))) return;
    await this.bulk({ delete: [...this.selected] });
    await this.run();
  }

  private exportCSV() {
    const k = this.kind, r = this.result;
    if (!k || !r) return;
    const cols = r.columns;
    const rows = [["name", "slug", ...cols], ...r.rows.map((x) => [x.name, x.slug, ...cols.map((c) => x.attrs[c])])];
    const url = URL.createObjectURL(new Blob([toCSV(rows)], { type: "text/csv;charset=utf-8" }));
    const a = Object.assign(document.createElement("a"), { href: url, download: `${this.project}-${k.id}.csv` });
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  private async pickCSV(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0];
    (e.target as HTMLInputElement).value = "";
    if (!file) return;
    const rows = parseCSV(await file.text());
    if (rows.length < 2) {
      toast(t("table.import_empty"), "error");
      return;
    }
    this.importRows = rows;
  }

  /** Maps CSV headers to fields: name, slug or an attribute (name or label). */
  private mapping(header: string[]): (string | undefined)[] {
    const k = this.kind;
    return header.map((h) => {
      const x = h.trim().toLowerCase();
      if (["name", "slug"].includes(x)) return x;
      return k?.attrs.find((a) => a.name.toLowerCase() === x || attrLabel(a).toLowerCase() === x)?.name;
    });
  }

  private async runImport() {
    const rows = this.importRows!;
    const map = this.mapping(rows[0]);
    const upserts = rows.slice(1).map((r) => {
      const attrs: Record<string, unknown> = {};
      let name = "", slug: string | undefined;
      map.forEach((f, i) => {
        const v = (r[i] ?? "").trim();
        if (!f || v === "") return;
        if (f === "name") name = v;
        else if (f === "slug") slug = v;
        else attrs[f] = this.typed(f, v);
      });
      return { kind: this.query.kind, name, slug, attrs };
    }).filter((u) => u.name || u.slug);
    const r = await this.bulk({ upserts });
    if (r) {
      this.importRows = undefined;
      await this.run();
    }
  }

  private async saveQuery() {
    const name = prompt(t("table.save_name"));
    if (!name) return;
    try {
      const s = await api<Saved>("POST", `/projects/${this.project}/queries`, { name, query: this.query });
      this.saved = [...this.saved.filter((x) => x.id !== s.id), s].sort((a, b) => a.name.localeCompare(b.name));
      toast(t("table.saved", { name: s.name, slug: s.slug }), "ok");
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private toggle(slug: string, on: boolean) {
    const s = new Set(this.selected);
    if (on) s.add(slug); else s.delete(slug);
    this.selected = s;
  }

  private importPanel() {
    const rows = this.importRows!;
    const map = this.mapping(rows[0]);
    const unknown = rows[0].filter((_, i) => !map[i]);
    const hasName = map.includes("name") || map.includes("slug");
    return html`<div class="imp" role="region" aria-label=${t("table.import")}>
      <b>${t("table.import_preview", { n: rows.length - 1, kind: kindName(this.kind) })}</b>
      <span class="muted">${t("table.import_columns", { cols: rows[0].map((h, i) => (map[i] ? `${h} → ${map[i]}` : "")).filter(Boolean).join(", ") })}</span>
      ${unknown.length ? html`<span class="muted">${t("table.import_ignored", { cols: unknown.join(", ") })}</span>` : ""}
      ${hasName ? "" : html`<span class="error">${t("table.import_need_name")}</span>`}
      <div class="row"><button ?disabled=${!hasName || this.busy} @click=${this.runImport}>${icon("Upload", 14)} ${t("table.import_run")}</button>
        <button class="ghost" @click=${() => (this.importRows = undefined)}>${t("doc.cancel")}</button></div>
    </div>`;
  }

  render() {
    const m = this.model, k = this.kind, r = this.result;
    if (!m) return html`<p class="muted">${t("app.loading")}</p>`;
    const fields = [{ name: "name", label: t("entity.name") }, ...(k?.attrs ?? []).map((a) => ({ name: a.name, label: attrLabel(a) }))];
    const label = (f: string) => fields.find((x) => x.name === f)?.label ?? f;
    const rels = [...new Set([...(k?.relations ?? []).map((x) => x.rel), "located_in", "depends_on"])];
    const edit = this.canEdit;
    const sortBy = (c: string) => this.setQuery({ ...this.query, sort: this.query.sort === c ? `-${c}` : c });
    return html`
      <h1>${t("table.title")}</h1>
      <div class="bar">
        <label>${t("table.kind")} <select @change=${(e: Event) => this.setQuery({ kind: (e.target as HTMLSelectElement).value })}>
          ${m.kinds.filter((x) => x.count > 0 || x.id === this.query.kind).map((x) => html`<option value=${x.id} ?selected=${x.id === this.query.kind}>${kindName(x)} (${x.count})</option>`)}</select></label>
        ${this.saved.length ? html`<label>${t("table.saved_queries")} <select @change=${(e: Event) => { const s = this.saved.find((x) => x.id === (e.target as HTMLSelectElement).value); if (s) this.setQuery(s.def); }}>
          <option value="">–</option>${this.saved.map((s) => html`<option value=${s.id}>${s.name}</option>`)}</select></label>` : ""}
        <form class="row" @submit=${this.addCond}>
          <select name="field" aria-label=${t("table.field")}>${fields.map((f) => html`<option value=${f.name}>${f.label}</option>`)}</select>
          <select name="op" aria-label=${t("table.op")}>${OPS.map((o) => html`<option value=${o}>${t(`table.op.${o}` as Key)}</option>`)}</select>
          <input name="value" aria-label=${t("table.value")} placeholder=${t("table.value")}>
          <button class="ghost">${icon("Plus", 14)} ${t("table.add_filter")}</button>
        </form>
        <label>${t("table.without")} <select @change=${(e: Event) => { const v = (e.target as HTMLSelectElement).value; this.setQuery({ ...this.query, rels: v ? [{ rel: v, missing: true }] : [] }); }}>
          <option value="">–</option>${rels.map((x) => html`<option value=${x} ?selected=${this.query.rels?.[0]?.rel === x}>${relLabel(x)}</option>`)}</select></label>
      </div>
      <div class="bar">
        ${(this.query.where ?? []).map((c, i) => html`<span class="cond">${label(c.field)} ${t(`table.op.${c.op}` as Key)} ${c.value ?? ""}
          <button class="ghost icon" aria-label=${t("table.remove_filter")} @click=${() => this.setQuery({ ...this.query, where: this.query.where!.filter((_, j) => j !== i) })}>${icon("X", 12)}</button></span>`)}
        <span class="muted">${r ? t("table.count", { n: r.total }) : ""}</span>
        <span style="flex:1"></span>
        <button class="ghost" @click=${this.exportCSV}>${icon("Download", 14)} ${t("table.export")}</button>
        ${edit ? html`<label class="btn ghost">${icon("Upload", 14)} ${t("table.import")}<input type="file" accept=".csv,text/csv,text/plain" hidden @change=${this.pickCSV}></label>
          <button class="ghost" @click=${this.saveQuery}>${icon("Save", 14)} ${t("table.save_query")}</button>` : ""}
      </div>
      ${this.importRows ? this.importPanel() : ""}
      ${r ? html`<div class="wrap"><table>
        <thead><tr>${edit ? html`<th><input type="checkbox" aria-label=${t("table.select_all")} .checked=${r.rows.length > 0 && this.selected.size === r.rows.length}
          @change=${(e: Event) => (this.selected = (e.target as HTMLInputElement).checked ? new Set(r.rows.map((x) => x.slug)) : new Set())}></th>` : ""}
          <th @click=${() => sortBy("name")}>${t("entity.name")}</th>${r.columns.map((c) => html`<th @click=${() => sortBy(c)}>${label(c)}</th>`)}</tr></thead>
        <tbody>${r.rows.map((x) => html`<tr class=${this.selected.has(x.slug) ? "sel" : ""}>
          ${edit ? html`<td><input type="checkbox" aria-label=${t("table.select", { name: x.name })} .checked=${this.selected.has(x.slug)} @change=${(e: Event) => this.toggle(x.slug, (e.target as HTMLInputElement).checked)}></td>` : ""}
          <td><a href="/p/${this.project}/e/${x.slug}">${x.name}</a></td>
          ${r.columns.map((c) => {
            const a = k?.attrs.find((y) => y.name === c);
            return edit ? html`<td><input aria-label=${`${x.name}: ${label(c)}`} .value=${x.attrs[c] === undefined || x.attrs[c] === null ? "" : String(x.attrs[c])}
              @change=${(e: Event) => this.editCell(x, c, (e.target as HTMLInputElement).value)}></td>` : html`<td>${formatValue(a, x.attrs[c])}</td>`;
          })}</tr>`)}</tbody></table></div>` : html`<p class="muted">${t("app.loading")}</p>`}
      ${edit && this.selected.size ? html`<form class="bulk" @submit=${this.bulkSet}>
        <b>${t("table.selected", { n: this.selected.size })}</b>
        <select name="field" aria-label=${t("table.field")}>${(k?.attrs ?? []).map((a) => html`<option value=${a.name}>${attrLabel(a)}</option>`)}</select>
        <input name="value" aria-label=${t("table.value")} placeholder=${t("table.set_value")}>
        <button ?disabled=${this.busy}>${t("table.set")}</button>
        <span style="flex:1"></span>
        <button type="button" class="danger" ?disabled=${this.busy} @click=${this.bulkDelete}>${icon("Trash2", 14)} ${t("table.delete_selected")}</button>
      </form>` : ""}`;
  }
}
customElements.define("phx-entity-table", PhxEntityTable);
