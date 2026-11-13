import { LitElement, css, html, unsafeCSS } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, api, type Doc, type TranslationInfo } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { languageName, rememberLocale } from "../languages";
import { contentLocale } from "../state";
import { renderMarkdown } from "../md";
import { currentProject, docsVersion, navigate } from "../state";
import { prose } from "./doc-view";
import { base } from "./shared";
import { toast } from "./toasts";


type Field = "markdown" | "context" | "decision" | "consequences" | "alternatives" | "description";
const fieldsOf: Record<Doc["type"], Field[]> = {
  markdown: ["markdown"],
  adr: ["context", "decision", "consequences", "alternatives"],
  link: ["description"],
  board: ["description"],
};

/** Side-by-side translation of a page or decision (ADR-0028). */
export class PhxTranslate extends LitElement {
  static properties = { project: {}, docSlug: {}, lang: {}, doc: { state: true }, tr: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    .cols { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 20px; align-items: start; }
    @media (max-width: 900px) { .cols { grid-template-columns: 1fr; } }
    .src { background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 14px 16px; }
    .src h2 { font-size: 13px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); font-family: var(--font-body); margin: 0 0 8px; }
    .src h3 { font-size: 12px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); margin: 14px 0 4px; }
    form { display: grid; gap: 12px; }
    textarea { width: 100%; min-height: 160px; font: 13px/1.55 var(--font-mono); resize: vertical; }
    textarea.big { min-height: 55vh; }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .actions .danger { margin-left: auto; }
    .note { font-size: 13px; color: var(--muted); }
  `];
  project = "";
  docSlug = "";
  lang = "";
  doc?: Doc;
  tr?: TranslationInfo;
  error = "";
  busy = false;
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.docSlug}/${this.lang}`;
    if (key === this.loaded) return;
    this.loaded = key;
    try {
      this.doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
      this.tr = await api<TranslationInfo>("GET", `/projects/${this.project}/documents/${this.doc.id}/translations/${this.lang}`).catch(() => undefined);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private copy() {
    const d = this.doc!;
    const form = this.renderRoot.querySelector("form")!;
    (form.elements.namedItem("title") as HTMLInputElement).value = d.title;
    for (const f of fieldsOf[d.type]) (form.elements.namedItem(f) as HTMLTextAreaElement).value = d.content?.[f] ?? "";
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const d = this.doc!;
    const f = new FormData(e.target as HTMLFormElement);
    const body: Record<string, unknown> = { title: f.get("title") };
    for (const k of fieldsOf[d.type]) body[k] = f.get(k) ?? "";
    this.busy = true;
    try {
      await api("PUT", `/projects/${this.project}/documents/${d.id}/translations/${this.lang}`, body);
      toast(t("lang.saved"));
      rememberLocale(this.project, this.lang);
      contentLocale.set(this.lang);
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${d.slug}`);
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async deleteTranslation() {
    if (!confirm(t("lang.delete_confirm", { lang: languageName(this.lang) }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/documents/${this.doc!.id}/translations/${this.lang}`);
      toast(t("lang.deleted"));
      navigate(`/p/${this.project}/d/${this.doc!.slug}`);
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const d = this.doc;
    if (!d) return html`<p class="muted">${t("app.loading")}</p>`;
    const main = currentProject.get()?.mainLocale ?? "";
    const tc = this.tr?.content ?? {};
    const label = (f: Field) => (f === "markdown" ? "" : f === "description" ? t("link.description") : t(`adr.${f}` as any));
    return html`<div class="enter">
      <a href="/p/${this.project}/d/${d.slug}" class="muted" style="font-size:14px;text-decoration:none">${icon("ArrowLeft", 14)} ${d.title}</a>
      <h1 style="margin:8px 0 14px">${t("lang.translate_title", { lang: languageName(this.lang) })}</h1>
      ${this.tr?.outdated ? html`<p class="notice" role="note">${icon("TriangleAlert", 14)} ${t("lang.outdated", { main: languageName(main) })}</p>` : ""}
      <div class="cols">
        <section class="src" aria-label=${t("lang.source", { lang: languageName(main) })}>
          <h2>${t("lang.source", { lang: languageName(main) })}</h2>
          <h1 style="font-size:22px;margin:0 0 10px">${d.title}</h1>
          ${fieldsOf[d.type].map((f) => html`${label(f) ? html`<h3>${label(f)}</h3>` : ""}<div class="prose">${unsafeHTML(renderMarkdown(d.content?.[f] ?? "", this.project))}</div>`)}
        </section>
        <form @submit=${this.save} aria-label=${t("lang.target", { lang: languageName(this.lang) })}>
          <label class="field">${t("doc.title")}<input name="title" required .value=${this.tr?.title ?? ""} lang=${this.lang}></label>
          ${fieldsOf[d.type].map((f) => html`<label class="field">${label(f) || t("lang.target", { lang: languageName(this.lang) })}
            <textarea name=${f} class=${f === "markdown" ? "big" : ""} lang=${this.lang} spellcheck="true" .value=${tc[f] ?? ""}></textarea></label>`)}
          <p class="note">${t("lang.hint", { version: d.version })}</p>
          <div class="actions">
            <button ?disabled=${this.busy}>${icon("Check", 14)} ${this.tr?.machine ? t("lang.save_reviewed") : t("doc.save")}</button>
            <button type="button" class="ghost" @click=${this.copy}>${icon("Copy", 14)} ${t("lang.copy_source")}</button>
            ${this.tr ? html`<button type="button" class="danger" @click=${this.deleteTranslation}>${icon("Trash2", 14)} ${t("lang.delete")}</button>` : ""}
          </div>
        </form>
      </div>
    </div>`;
  }
}
customElements.define("phx-translate", PhxTranslate);
