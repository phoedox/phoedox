import { LitElement, css, html } from "lit";
import { ApiError, api, type Doc } from "../api";
import { t } from "../i18n";
import { docsVersion, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type DocResult = { applied: boolean; document?: Doc };
const statuses = ["proposed", "accepted", "rejected", "deprecated", "superseded"] as const;

/** Create (type set) or edit (docSlug set) a markdown page, decision or link. */
export class PhxDocForm extends LitElement {
  static properties = { project: {}, docSlug: {}, type: {}, doc: { state: true }, error: { state: true }, busy: { state: true } };
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
  error = "";
  busy = false;
  private loaded = "";

  async willUpdate() {
    if (this.docSlug && this.loaded !== this.docSlug) {
      this.loaded = this.docSlug;
      try {
        this.doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
        this.type = this.doc.type;
      } catch (e) {
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
      }
    }
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const s = (k: string) => String(f.get(k) ?? "");
    const body: Record<string, unknown> = { title: s("title"), category: s("category") };
    if (this.type === "markdown") body.markdown = s("markdown");
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
      toast(t("doc.saved"));
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${res.document?.slug ?? ""}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
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
      ${this.type === "markdown" ? html`
        <label class="field">${t("doc.content")}<textarea class="big" name="markdown" .value=${c.markdown ?? ""}></textarea></label>
        <p class="muted" style="margin:0;font-size:13px">${t("doc.mention_hint")}</p>` : ""}
      ${this.type === "link" ? html`
        <label class="field">${t("link.url")}<input name="url" type="url" .value=${c.url ?? ""} required placeholder="https://" /></label>
        <label class="field">${t("link.description")}<textarea name="description" .value=${c.description ?? ""}></textarea></label>` : ""}
      ${this.type === "adr" ? html`
        <div class="two">
          <label class="field">${t("adr.status")}<select name="status">${statuses.map((st) => html`<option value=${st} ?selected=${(c.status ?? "proposed") === st}>${t(`adr.status.${st}`)}</option>`)}</select></label>
          <label class="field">${t("adr.date")}<input name="date" type="date" .value=${c.date ?? new Date().toISOString().slice(0, 10)} /></label>
        </div>
        <label class="field">${t("adr.context")}<textarea name="context" .value=${c.context ?? ""}></textarea></label>
        <label class="field">${t("adr.decision")}<textarea name="decision" .value=${c.decision ?? ""} required></textarea></label>
        <label class="field">${t("adr.consequences")}<textarea name="consequences" .value=${c.consequences ?? ""}></textarea></label>
        <label class="field">${t("adr.alternatives")}<textarea name="alternatives" .value=${c.alternatives ?? ""}></textarea></label>
        <label class="field">${t("adr.supersedes")}<input name="supersedes" .value=${c.supersedes ?? ""} /></label>` : ""}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <div class="row">
        <button type="submit" ?disabled=${this.busy}>${d ? t("doc.save") : t("doc.create")}</button>
        <a class="btn ghost" href=${cancel}>${t("doc.cancel")}</a>
      </div>
    </form>`;
  }
}
customElements.define("phx-doc-form", PhxDocForm);
