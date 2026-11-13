import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, unsafeCSS } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, adrRef, api, type Decision, type Doc } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown } from "../md";
import { currentProject, docsVersion, favorites, navigate } from "../state";
import "./request-block";
import { base } from "./shared";
import "./attachments";
import { toast } from "./toasts";

export const prose = `
  .prose { max-width: 72ch; line-height: 1.65; }
  .prose h1, .prose h2, .prose h3 { margin: 1.4em 0 .5em; }
  .prose pre { background: var(--surface2); padding: 12px 14px; border-radius: 8px; overflow-x: auto; }
  .prose code { font-family: var(--font-mono); font-size: .9em; }
  .prose a[title="mention"] { background: var(--accent-soft); padding: 0 5px; border-radius: 4px; text-decoration: none; }
  .prose table { border-collapse: collapse; } .prose td, .prose th { border: 1px solid var(--line); padding: 6px 10px; }
  .prose img, .prose .ed-img img { max-width: 100%; height: auto; border-radius: 8px; vertical-align: middle; }
  .prose .ed-img { display: inline-block; max-width: 100%; }
`;

export class PhxDocView extends SignalWatcher(LitElement) {
  static properties = { project: {}, docSlug: {}, doc: { state: true }, related: { state: true }, error: { state: true }, confirming: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    header { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; flex-wrap: wrap; margin-bottom: 14px; }
    .meta { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
    section { margin-top: 18px; }
    section h2 { font-size: 15px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); font-family: var(--font-body); margin-bottom: 4px; }
    .notice { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; padding: 10px 14px; margin: 0 0 16px; border-radius: 8px;
      background: var(--surface2); border-left: 3px solid var(--accent); font-size: 14px; }
    .ref { font-family: var(--font-mono); font-size: 13px; }
    button.icon { padding: 7px 9px; }
    button.icon[aria-pressed="true"] { color: var(--accent); border-color: var(--accent); }
    button.icon[aria-pressed="true"] .ico { fill: currentColor; }
  `];
  project = "";
  docSlug = "";
  doc?: Doc;
  /** Other decisions by slug, to show numbers and titles of linked ones. */
  related = new Map<string, Decision>();
  error = "";
  confirming = false;
  private loaded = "";

  async willUpdate() {
    const key = this.project + "/" + this.docSlug;
    if (key !== this.loaded) {
      this.loaded = key;
      this.doc = undefined;
      this.confirming = false;
      try {
        const doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
        if (doc.type === "adr" && (doc.content?.supersedes || doc.content?.supersededBy)) {
          const all = await api<Decision[]>("GET", `/projects/${this.project}/decisions`).catch(() => []);
          this.related = new Map(all.map((d) => [d.slug, d]));
        }
        this.doc = doc;
      } catch (e) {
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
      }
    }
  }

  private async deleteDoc() {
    const doc = this.doc;
    try {
      await api("DELETE", `/projects/${this.project}/documents/${this.docSlug}`);
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}`);
      if (doc) {
        const project = this.project;
        toast(t("doc.trashed", { title: doc.title }), "ok", {
          label: t("doc.undo"),
          run: async () => {
            try {
              await api("POST", `/projects/${project}/trash/${doc.id}/restore`);
              docsVersion.set(docsVersion.get() + 1);
              navigate(`/p/${project}/d/${doc.slug}`);
            } catch (err) {
              toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
            }
          },
        });
      }
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async toggleFavorite(personal: boolean, on: boolean) {
    try {
      await api(on ? "PUT" : "DELETE", `/projects/${this.project}/favorites/${this.doc!.id}?scope=${personal ? "me" : "project"}`);
      docsVersion.set(docsVersion.get() + 1);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private body(d: Doc) {
    const c = d.content ?? {};
    if (d.type === "markdown") return html`<div class="prose">${unsafeHTML(renderMarkdown(c.markdown ?? "", this.project, d.id))}</div>`;
    if (d.type === "link") return html`<p><a class="btn" href=${c.url} target="_blank" rel="noopener external">${icon("ExternalLink", 15)} ${t("doc.open_link")}</a></p>
      <p class="mono muted">${c.url}</p>${c.description ? html`<div class="prose">${unsafeHTML(renderMarkdown(c.description, this.project))}</div>` : ""}`;
    const part = (key: "adr.context" | "adr.decision" | "adr.consequences" | "adr.alternatives", md: string) =>
      md ? html`<section><h2>${t(key)}</h2><div class="prose">${unsafeHTML(renderMarkdown(md, this.project))}</div></section>` : "";
    return html`
      ${part("adr.context", c.context)}${part("adr.decision", c.decision)}${part("adr.consequences", c.consequences)}${part("adr.alternatives", c.alternatives)}
      ${c.supersedes ? html`<p class="muted">${t("adr.supersedes")}: ${this.decisionLink(c.supersedes)}</p>` : ""}`;
  }

  /** Link to another decision as "ADR-0003 Title" (slug if unknown). */
  private decisionLink(slug: string) {
    const d = this.related.get(slug);
    return html`<a href="/p/${this.project}/d/${slug}">${d ? html`<span class="ref">${d.ref}</span> ${d.title}` : slug}</a>`;
  }

  private favoriteButtons(d: Doc) {
    const favs = favorites.get();
    const mine = favs.some((f) => f.documentId === d.id && f.personal);
    const pinned = favs.some((f) => f.documentId === d.id && !f.personal);
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`
      <button class="ghost icon" aria-pressed=${mine ? "true" : "false"} title=${t(mine ? "fav.remove_me" : "fav.add_me")} aria-label=${t(mine ? "fav.remove_me" : "fav.add_me")}
        @click=${() => this.toggleFavorite(true, !mine)}>${icon("Star", 15)}</button>
      ${canEdit ? html`<button class="ghost icon" aria-pressed=${pinned ? "true" : "false"} title=${t(pinned ? "fav.remove_project" : "fav.add_project")} aria-label=${t(pinned ? "fav.remove_project" : "fav.add_project")}
        @click=${() => this.toggleFavorite(false, !pinned)}>${icon("Pin", 15)}</button>` : ""}`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const d = this.doc;
    if (!d) return html`<p class="muted">${t("app.loading")}</p>`;
    const c = d.content ?? {};
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`<article class="enter">
      <header>
        <div>
          <h1>${d.title}</h1>
          <div class="meta">
            ${d.number ? html`<span class="chip ref">${adrRef(d.number)}</span>` : ""}
            <span class="chip">${t(`doc.type.${d.type}` as any)}</span>
            ${d.type === "adr" ? html`<span class="chip status-${c.status}">${t(`adr.status.${c.status}` as any)}</span>${c.date ? html`<span class="chip">${c.date}</span>` : ""}` : ""}
            <span class="chip">${t("doc.version", { version: d.version })}</span>
            <span class="chip">${t("doc.updated", { date: formatDate(d.updatedAt) })}</span>
          </div>
        </div>
        <div class="row">
          ${this.favoriteButtons(d)}
          <a class="btn ghost" href="/p/${this.project}/d/${d.slug}/history">${icon("History", 14)} ${t("doc.history")}</a>
          <a class="btn ghost" href="/p/${this.project}/d/${d.slug}/edit">${icon("Pencil", 14)} ${t("doc.edit")}</a>
          ${this.confirming
            ? html`<span class="muted">${t("doc.delete_confirm")}</span><button class="danger" @click=${this.deleteDoc}>${t("doc.delete")}</button><button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button>`
            : html`<button class="danger" @click=${() => (this.confirming = true)}>${icon("Trash2", 14)} ${t("doc.delete")}</button>`}
        </div>
      </header>
      ${d.type === "adr" && c.status === "superseded" && c.supersededBy
        ? html`<p class="notice" role="note">${t("adr.superseded_notice")} ${t("adr.superseded_by")}: ${this.decisionLink(c.supersededBy)}</p>` : ""}
      ${this.body(d)}
      ${d.type !== "link" ? html`<phx-attachments style="margin-top:28px" .project=${this.project} .document=${d.id} .editable=${canEdit}></phx-attachments>` : ""}
    </article>`;
  }
}
customElements.define("phx-doc-view", PhxDocView);
