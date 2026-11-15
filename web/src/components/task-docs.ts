import { LitElement, css, html } from "lit";
import { ApiError, adrRef, api, type Doc, type Task, type TaskDoc } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

export const docTypeIcon: Record<Doc["type"], string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot", whiteboard: "Presentation" };

/** Pages and decisions linked to a task card; several of each (ADR-0064). */
export class PhxTaskDocs extends LitElement {
  static properties = { project: {}, task: {}, docs: {}, canEdit: { type: Boolean }, all: { state: true }, query: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    h3 { font-size: 13px; margin: 4px 0 6px; color: var(--muted); font-weight: 600; display: flex; gap: 6px; align-items: center; }
    ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 4px; }
    li { display: flex; gap: 8px; align-items: center; font-size: 13px; padding: 5px 8px; border-radius: 8px; background: var(--surface2); }
    li a { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg); }
    li .ref { font-family: var(--font-mono); font-size: 12px; color: var(--muted); }
    li button { padding: 2px; }
    .add { position: relative; margin-top: 6px; }
    .add input { width: 100%; }
    .hits { position: absolute; z-index: 5; left: 0; right: 0; top: calc(100% + 2px); display: grid; gap: 1px; padding: 4px; max-height: 240px; overflow: auto;
      background: var(--bg); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 8px 24px rgb(0 0 0 / .12); }
    .hits button { justify-content: flex-start; gap: 8px; background: none; border: 0; color: var(--fg); font-weight: 400; padding: 6px 8px; border-radius: 6px; text-align: left; }
    .hits button:hover, .hits button:focus-visible { background: var(--surface2); outline: none; }
    p.hint { font-size: 12px; margin: 4px 0 0; }
  `];
  project = "";
  task = "";
  docs: TaskDoc[] = [];
  canEdit = false;
  all: Doc[] = [];
  query = "";
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    if (this.canEdit) this.all = (await api<Doc[]>("GET", `/projects/${this.project}/documents`).catch(() => [])) ?? [];
  }

  private label(d: { type: Doc["type"]; number?: number }) {
    return d.type === "adr" && d.number ? adrRef(d.number) : "";
  }

  private async save(next: TaskDoc[]) {
    this.busy = true;
    try {
      const task = await api<Task>("PATCH", `/projects/${this.project}/tasks/${this.task}`, { documents: next.map((d) => d.id) });
      this.docs = task.documents ?? [];
      this.dispatchEvent(new CustomEvent("changed", { detail: this.docs, bubbles: true, composed: true }));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private hits() {
    const q = this.query.trim().toLowerCase();
    if (!q) return [];
    const linked = new Set(this.docs.map((d) => d.id));
    return this.all.filter((d) => !linked.has(d.id) && (d.title.toLowerCase().includes(q) || d.slug.includes(q) || this.label(d).toLowerCase().includes(q))).slice(0, 12);
  }

  private pick(d: Doc) {
    this.query = "";
    void this.save([...this.docs, { id: d.id, slug: d.slug, title: d.title, type: d.type, number: d.number }]);
  }

  render() {
    if (!this.docs.length && !this.canEdit) return "";
    const hits = this.hits();
    return html`<h3>${icon("Link", 13)} ${t("taskdocs.title")}</h3>
      ${this.docs.length ? html`<ul>${this.docs.map((d) => html`<li>
        ${icon(docTypeIcon[d.type] as any, 14)}
        <a href="/p/${this.project}/d/${d.slug}">${this.label(d) ? html`<span class="ref">${this.label(d)}</span> ` : ""}${d.title}</a>
        ${this.canEdit ? html`<button type="button" class="ghost icon" ?disabled=${this.busy} title=${t("taskdocs.remove")} aria-label=${t("taskdocs.remove")}
          @click=${() => this.save(this.docs.filter((x) => x.id !== d.id))}>${icon("X", 13)}</button>` : ""}
      </li>`)}</ul>` : html`<p class="muted hint">${t("taskdocs.empty")}</p>`}
      ${this.canEdit ? html`<div class="add">
        <input type="search" role="combobox" aria-expanded=${hits.length ? "true" : "false"} aria-controls="hits" autocomplete="off"
          placeholder=${t("taskdocs.search")} aria-label=${t("taskdocs.search")} .value=${this.query}
          @input=${(e: Event) => (this.query = (e.target as HTMLInputElement).value)}
          @keydown=${(e: KeyboardEvent) => {
            if (e.key === "Enter") { e.preventDefault(); if (hits[0]) this.pick(hits[0]); }
            if (e.key === "Escape" && this.query) { e.stopPropagation(); this.query = ""; }
            if (e.key === "ArrowDown") { e.preventDefault(); this.renderRoot.querySelector<HTMLElement>(".hits button")?.focus(); }
          }}>
        ${hits.length ? html`<div class="hits" id="hits" role="listbox">${hits.map((d) => html`<button type="button" role="option" @click=${() => this.pick(d)}
            @keydown=${(e: KeyboardEvent) => {
              const el = e.currentTarget as HTMLElement;
              if (e.key === "ArrowDown") { e.preventDefault(); (el.nextElementSibling as HTMLElement | null)?.focus(); }
              if (e.key === "ArrowUp") { e.preventDefault(); ((el.previousElementSibling as HTMLElement | null) ?? this.renderRoot.querySelector<HTMLElement>(".add input"))?.focus(); }
            }}>
          ${icon(docTypeIcon[d.type] as any, 14)} ${this.label(d) ? html`<span class="ref">${this.label(d)}</span>` : ""} ${d.title}</button>`)}</div>` : ""}
      </div>` : ""}`;
  }
}
customElements.define("phx-task-docs", PhxTaskDocs);

/** On a page or decision: the cards that link it. */
export class PhxDocTasks extends LitElement {
  static properties = { project: {}, document: {}, tasks: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    h2 { font-size: 15px; margin: 24px 0 8px; display: flex; gap: 6px; align-items: center; }
    ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 4px; }
    li a { display: flex; gap: 8px; align-items: center; padding: 6px 10px; border: 1px solid var(--line); border-radius: 8px; color: var(--fg); text-decoration: none; font-size: 14px; }
    li a .board { margin-left: auto; color: var(--muted); font-size: 12px; }
    li a.done span:first-of-type { text-decoration: line-through; color: var(--muted); }
    @media (hover: hover) and (pointer: fine) { li a:hover { border-color: var(--accent); } }
  `];
  project = "";
  document = "";
  tasks: Task[] = [];
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.document}`;
    if (!this.document || key === this.loaded) return;
    this.loaded = key;
    this.tasks = (await api<Task[]>("GET", `/projects/${this.project}/documents/${this.document}/tasks`).catch(() => [])) ?? [];
  }

  render() {
    if (!this.tasks.length) return "";
    return html`<h2>${icon("SquareKanban", 16)} ${t("taskdocs.cards")}</h2>
      <ul>${this.tasks.map((x) => html`<li><a class=${x.doneAt ? "done" : ""} href="/p/${this.project}/d/${x.boardSlug}?task=${x.id}">
        ${icon(x.doneAt ? "CircleCheck" : "Circle" as any, 14)} <span>${x.title}</span><span class="board">${x.boardTitle}</span></a></li>`)}</ul>`;
  }
}
customElements.define("phx-doc-tasks", PhxDocTasks);
