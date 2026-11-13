import { LitElement, css, html } from "lit";
import { ApiError, api, decisionStatuses, type Decision, type Doc } from "../api";
import { t } from "../i18n";
import { docsVersion, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type DocResult = { applied: boolean; document?: Doc };

/** Create (type set) or edit (docSlug set) a markdown page, decision or link. */
export class PhxDocForm extends LitElement {
  static properties = { project: {}, docSlug: {}, type: {}, doc: { state: true }, decisions: { state: true }, error: { state: true }, busy: { state: true }, live: { state: true } };
  static styles = [base, css`
    form { display: grid; gap: 12px; max-width: 820px; }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    textarea.big { min-height: 380px; }
    @media (max-width: 700px) { .two { grid-template-columns: 1fr; } }
  `];
  project = "";
  docSlug = "";
  type: Doc["type"] = "markdown";
  doc?: Doc;
  /** Other decisions of the project, offered as "supersedes" targets. */
  decisions: Decision[] = [];
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
    if (this.type === "markdown") body.markdown = "";
    if (this.type === "link") body.link = { url: s("url"), description: s("description") };
    if (this.type === "adr") body.adr = { status: s("status"), date: s("date"), context: s("context"), decision: s("decision"), consequences: s("consequences"), alternatives: s("alternatives"), supersedes: s("supersedes") };
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
        toast(t("adr.marked_superseded", { ref: this.decisions.find((d) => d.slug === target)?.ref ?? target }));
      }
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${res.document?.slug ?? ""}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private supersedesField(d: Doc | undefined, current: string) {
    const options = this.decisions.filter((o) => o.slug !== d?.slug);
    const known = !current || options.some((o) => o.slug === current);
    return html`<label class="field">${t("adr.supersedes")}
        <select name="supersedes">
          <option value="" ?selected=${!current}>${t("adr.supersedes_none")}</option>
          ${known ? "" : html`<option value=${current} selected>${current}</option>`}
          ${options.map((o) => html`<option value=${o.slug} ?selected=${o.slug === current}>${o.ref} · ${o.title}</option>`)}
        </select>
      </label>
      <p class="muted" style="margin:-4px 0 0;font-size:13px">${t("adr.supersedes_hint")}</p>`;
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
        <label class="field">${t("doc.category")}<input name="category" .value=${d?.category ?? (this.type === "adr" ? "Entscheidungen" : "")} /></label>
      </div>

      ${this.type === "link" ? html`
        <label class="field">${t("link.url")}<input name="url" type="url" .value=${c.url ?? ""} required placeholder="https://" /></label>
        <label class="field">${t("link.description")}<textarea name="description" .value=${c.description ?? ""}></textarea></label>` : ""}
      ${this.type === "adr" ? html`
        <div class="two">
          <label class="field">${t("adr.status")}<select name="status">${decisionStatuses.map((st) => html`<option value=${st} ?selected=${(c.status ?? "proposed") === st}>${t(`adr.status.${st}`)}</option>`)}</select></label>
          <label class="field">${t("adr.date")}<input name="date" type="date" .value=${c.date ?? new Date().toISOString().slice(0, 10)} /></label>
        </div>
        <label class="field">${t("adr.context")}<textarea name="context" .value=${c.context ?? ""}></textarea></label>
        <label class="field">${t("adr.decision")}<textarea name="decision" .value=${c.decision ?? ""} required></textarea></label>
        <label class="field">${t("adr.consequences")}<textarea name="consequences" .value=${c.consequences ?? ""}></textarea></label>
        <label class="field">${t("adr.alternatives")}<textarea name="alternatives" .value=${c.alternatives ?? ""}></textarea></label>
        ${this.supersedesField(d, c.supersedes ?? "")}` : ""}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <div class="row">
        <button type="submit" ?disabled=${this.busy}>${d ? t("doc.save") : t("doc.create")}</button>
        <a class="btn ghost" href=${cancel}>${t("doc.cancel")}</a>
      </div>
    </form>`;
  }
}
customElements.define("phx-doc-form", PhxDocForm);
