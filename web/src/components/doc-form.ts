import { LitElement, css, html } from "lit";
import { ApiError, api, decisionStatuses, type Decision, type DecisionOption, type Doc, type DraftInfo } from "../api";
import { icon } from "../icon";
import { t } from "../i18n";
import { docsVersion, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type DocResult = { applied: boolean; document?: Doc };

/** Create (type set) or edit (docSlug set) a markdown page, decision or link. */
export class PhxDocForm extends LitElement {
  static properties = { project: {}, docSlug: {}, type: {}, doc: { state: true }, decisions: { state: true }, options: { state: true }, error: { state: true }, busy: { state: true }, live: { state: true } };
  static styles = [base, css`
    form { display: grid; gap: 12px; max-width: 820px; }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    textarea.big { min-height: 380px; }
    @media (max-width: 700px) { .two { grid-template-columns: 1fr; } }
      label.check { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    label.check input { width: auto; margin: 0; }
    fieldset.opts { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px 12px; display: grid; gap: 10px; margin: 0; }
    fieldset.opts legend { padding: 0 4px; font-weight: 600; font-size: 14px; }
    .opt { display: grid; gap: 6px; padding: 10px; border-radius: 8px; background: var(--surface2); }
    .opt .head { display: flex; gap: 8px; align-items: center; }
    .opt .head input { flex: 1; }
    .opt textarea { min-height: 70px; }
    p.hint { margin: 0; font-size: 13px; }
`];
  project = "";
  docSlug = "";
  type: Doc["type"] = "markdown";
  doc?: Doc;
  /** Other decisions of the project, offered as "supersedes" targets. */
  decisions: Decision[] = [];
  /** Options of a proposed decision (ADR-0062). */
  options: DecisionOption[] = [];
  /** Markdown pages are edited live; the editor module is loaded on demand. */
  live = false;
  error = "";
  busy = false;
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.docSlug}/${this.type}`;
    if (this.loaded === key) return;
    this.loaded = key;
    if (this.docSlug) {
      try {
        this.doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
        this.type = this.doc.type;
        this.options = (this.doc.content?.options ?? []).map((o: DecisionOption) => ({ ...o }));
        this.loaded = `${this.project}/${this.docSlug}/${this.type}`;
      } catch (e) {
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
        return;
      }
    }
    if (this.type === "adr") {
      this.decisions = await api<Decision[]>("GET", `/projects/${this.project}/decisions`).catch(() => []);
    }
    if (this.type === "markdown" && this.doc) {
      await import("./live-editor");
      this.live = true;
    }
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const s = (k: string) => String(f.get(k) ?? "");
    const body: Record<string, unknown> = { title: s("title"), category: s("category") };
    if (this.type === "markdown") {
      // Page kinds (process, component, dashboard, database model) start from a template.
      const kind = new URLSearchParams(location.search).get("kind") ?? "";
      body.markdown = ["process", "component", "dashboard", "dbmodel"].includes(kind) ? t(`pagetpl.${kind}` as any) : "";
    }
    if (this.type === "link") body.link = { url: s("url"), description: s("description") };
    if (this.type === "floorplan") {
      // With an open draft the change goes into the draft: start from its plan, not the published one.
      const draft = this.doc ? await api<DraftInfo>("GET", `/projects/${this.project}/documents/${this.doc.id}/draft`).catch(() => undefined) : undefined;
      const base = draft ? JSON.parse(draft.markdown || "{}") : this.doc?.content ?? {};
      body.floorplan = { ...base, description: s("description") };
    }
    if (this.type === "whiteboard" && !this.doc) body.whiteboard = { elements: [] };
    if (this.type === "board") {
      body.board = this.doc
        ? { ...(this.doc.content ?? {}), description: s("description") }
        : { description: s("description"), columns: [
          { id: "todo", name: t("board.col_todo") }, { id: "doing", name: t("board.col_doing") }, { id: "done", name: t("board.col_done"), done: true }] };
    }
    if (this.type === "adr") body.adr = { status: s("status"), date: s("date"), context: s("context"), decision: s("decision"), consequences: s("consequences"), alternatives: s("alternatives"), supersedes: s("supersedes"),
      supersedesPartly: !!s("supersedes") && s("partly") === "1", supersedesScope: s("partly") === "1" ? s("scope") : "",
      options: this.options.filter((o) => o.title.trim() || (o.description ?? "").trim()) };
    this.busy = true;
    this.error = "";
    try {
      let res: DocResult;
      if (this.doc) {
        res = await api<DocResult>("PATCH", `/projects/${this.project}/documents/${this.doc.slug}`, { ...body, expectVersion: this.doc.version });
      } else {
        res = await api<DocResult>("POST", `/projects/${this.project}/documents`, { ...body, type: this.type });
      }
      if (this.type === "markdown" && !this.doc) {
        // New pages continue in the live editor.
        docsVersion.set(docsVersion.get() + 1);
        navigate(`/p/${this.project}/d/${res.document?.slug ?? ""}/edit`);
        return;
      }
      toast(t("doc.saved"));
      const target = this.type === "adr" ? s("supersedes") : "";
      if (target && target !== (this.doc?.content?.supersedes ?? "")) {
        toast(t(s("partly") === "1" ? "adr.marked_partly" : "adr.marked_superseded", { ref: this.decisions.find((d) => d.slug === target)?.ref ?? target }));
      }
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${res.document?.slug ?? ""}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private supersedesField(d: Doc | undefined, current: string, partly: boolean, scope: string) {
    const options = this.decisions.filter((o) => o.slug !== d?.slug);
    const known = !current || options.some((o) => o.slug === current);
    return html`<label class="field">${t("adr.supersedes")}
        <select name="supersedes">
          <option value="" ?selected=${!current}>${t("adr.supersedes_none")}</option>
          ${known ? "" : html`<option value=${current} selected>${current}</option>`}
          ${options.map((o) => html`<option value=${o.slug} ?selected=${o.slug === current}>${o.ref} · ${o.title}</option>`)}
        </select>
      </label>
      <label class="check"><input type="checkbox" name="partly" value="1" ?checked=${partly}> ${t("adr.supersedes_partly")}</label>
      <label class="field">${t("adr.supersedes_scope")}<input name="scope" .value=${scope} placeholder=${t("adr.supersedes_scope_ph")}></label>
      <p class="muted" style="margin:-4px 0 0;font-size:13px">${t("adr.supersedes_hint")}</p>`;
  }

  private setOption(i: number, patch: Partial<DecisionOption>) {
    this.options = this.options.map((o, j) => (j === i ? { ...o, ...patch } : o));
  }

  /** Options people choose from while the decision is proposed. */
  private optionsField() {
    return html`<fieldset class="opts">
      <legend>${t("adr.options")}</legend>
      <p class="muted hint">${t("adr.options_hint")}</p>
      ${this.options.map((o, i) => html`<div class="opt">
        <div class="head">
          <input aria-label=${t("adr.option_title", { n: i + 1 })} placeholder=${t("adr.option_title", { n: i + 1 })} .value=${o.title} maxlength="200"
            @input=${(e: Event) => this.setOption(i, { title: (e.target as HTMLInputElement).value })}>
          <button type="button" class="ghost icon" title=${t("adr.option_remove")} aria-label=${t("adr.option_remove")}
            @click=${() => (this.options = this.options.filter((_, j) => j !== i))}>${icon("Trash2", 14)}</button>
        </div>
        <textarea aria-label=${t("adr.option_description")} placeholder=${t("adr.option_description")} .value=${o.description ?? ""}
          @input=${(e: Event) => this.setOption(i, { description: (e.target as HTMLTextAreaElement).value })}></textarea>
      </div>`)}
      <div><button type="button" class="ghost" ?disabled=${this.options.length >= 12} @click=${() => (this.options = [...this.options, { title: "", description: "" }])}>${icon("Plus", 14)} ${t("adr.option_add")}</button></div>
    </fieldset>`;
  }

  render() {
    if (this.live) return html`<phx-live-editor .project=${this.project} .docSlug=${this.docSlug}></phx-live-editor>`;
    if (this.docSlug && !this.doc && !this.error) return html`<p class="muted">${t("app.loading")}</p>`;
    const d = this.doc;
    const c = d?.content ?? {};
    const cancel = d ? `/p/${this.project}/d/${d.slug}` : `/p/${this.project}`;
    return html`<form class="enter" @submit=${this.submit}>
      <h1>${d ? d.title : t(`doc.type.${this.type}` as any)}</h1>
      <div class="two">
        <label class="field">${t("doc.title")}<input name="title" .value=${d?.title ?? ""} required /></label>
        <label class="field">${t("doc.category")}<input name="category" .value=${d?.category ?? (new URLSearchParams(location.search).get("category") ?? (this.type === "adr" ? "Entscheidungen" : ""))} /></label>
      </div>

      ${this.type === "board" || this.type === "floorplan" ? html`
        <label class="field">${t(this.type === "board" ? "board.description" : "plan.description")}<textarea name="description" .value=${c.description ?? ""}></textarea></label>` : ""}
      ${this.type === "link" ? html`
        <label class="field">${t("link.url")}<input name="url" type="url" .value=${c.url ?? ""} required placeholder="https://" /></label>
        <label class="field">${t("link.description")}<textarea name="description" .value=${c.description ?? ""}></textarea></label>` : ""}
      ${this.type === "adr" ? html`
        <div class="two">
          <label class="field">${t("adr.status")}<select name="status">${decisionStatuses.map((st) => html`<option value=${st} ?selected=${(c.status ?? "proposed") === st}>${t(`adr.status.${st}`)}</option>`)}</select></label>
          <label class="field">${t("adr.date")}<input name="date" type="date" .value=${c.date ?? new Date().toISOString().slice(0, 10)} /></label>
        </div>
        <label class="field">${t("adr.context")}<textarea name="context" .value=${c.context ?? ""}></textarea></label>
        ${this.optionsField()}
        <label class="field">${t("adr.decision")}<textarea name="decision" .value=${c.decision ?? ""}></textarea></label>
        <label class="field">${t("adr.consequences")}<textarea name="consequences" .value=${c.consequences ?? ""}></textarea></label>
        <label class="field">${t("adr.alternatives")}<textarea name="alternatives" .value=${c.alternatives ?? ""}></textarea></label>
        ${this.supersedesField(d, c.supersedes ?? "", !!c.supersedesPartly, c.supersedesScope ?? "")}` : ""}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <div class="row">
        <button type="submit" ?disabled=${this.busy}>${d ? t("doc.save") : t("doc.create")}</button>
        <a class="btn ghost" href=${cancel}>${t("doc.cancel")}</a>
      </div>
    </form>`;
  }
}
customElements.define("phx-doc-form", PhxDocForm);
