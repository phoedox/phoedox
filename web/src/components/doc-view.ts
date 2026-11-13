import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, unsafeCSS } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, adrRef, api, type Decision, type Doc, type DraftInfo } from "../api";
import { diffLines, fold, type Hunk } from "../diff";
import { languageName, preferredLocale, rememberLocale } from "../languages";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown, withoutTitle } from "../md";
import { currentProject, docsVersion, favorites, navigate, setWideReading, wideReading } from "../state";
import "./request-block";
import { base } from "./shared";
import "./attachments";
import "./doc-share";
import type { PhxDocShare } from "./doc-share";
import { toast } from "./toasts";

export const prose = `
  .prose { line-height: 1.65; }
  .prose h1, .prose h2, .prose h3 { margin: 1.4em 0 .5em; }
  .prose pre { background: var(--surface2); padding: 12px 14px; border-radius: 8px; overflow-x: auto; }
  .prose code { font-family: var(--font-mono); font-size: .9em; }
  .prose a[title="mention"] { background: var(--accent-soft); padding: 0 5px; border-radius: 4px; text-decoration: none; }
  .prose table { border-collapse: collapse; } .prose td, .prose th { border: 1px solid var(--line); padding: 6px 10px; }
  .prose img, .prose .ed-img img { max-width: 100%; height: auto; border-radius: 8px; vertical-align: middle; }
  .prose .ed-img { display: inline-block; max-width: 100%; }
`;

export class PhxDocView extends SignalWatcher(LitElement) {
  static properties = { project: {}, docSlug: {}, doc: { state: true }, draft: { state: true }, showDiff: { state: true }, lang: { state: true }, mainDiff: { state: true }, related: { state: true }, error: { state: true }, confirming: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    /* Centred reading column; "full width" lifts the limit. */
    article { max-width: var(--reading-width, 820px); margin-inline: auto; }
    article.wide { max-width: none; }
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
    .langs { display: flex; gap: 4px; align-items: center; flex-wrap: wrap; margin: -4px 0 14px; color: var(--muted); }
    .langs .lang { font-size: 13px; font-weight: 500; padding: 3px 10px; border-radius: 999px; background: none; border: 1px solid var(--line); color: var(--muted); display: inline-flex; gap: 6px; align-items: center; }
    .langs .lang[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); border-color: transparent; }
    .langs .lang[data-missing] { border-style: dashed; }
    .langs .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--warn, var(--accent)); }
    .langs .btn { margin-left: auto; }
    .small { font-size: 13px; padding: 3px 9px; }
    .draft { border-left-color: var(--warn, var(--accent)); justify-content: space-between; }
    .draft .who { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .diff { font-family: var(--font-mono); font-size: 13px; line-height: 1.55; border: 1px solid var(--line); border-radius: 8px; overflow-x: auto; background: var(--surface); margin: -6px 0 18px; }
    .diff div { white-space: pre-wrap; word-break: break-word; padding: 0 12px 0 28px; position: relative; min-height: 1.55em; }
    .diff div::before { position: absolute; left: 10px; color: var(--muted); }
    .diff .add { background: color-mix(in srgb, var(--good) 14%, transparent); } .diff .add::before { content: "+"; color: var(--good); }
    .diff .del { background: color-mix(in srgb, var(--bad) 12%, transparent); } .diff .del::before { content: "−"; color: var(--bad); }
    .diff .fold { color: var(--muted); font-family: var(--font-body); font-size: 12.5px; padding: 4px 12px; background: var(--surface2); }
  `];
  project = "";
  docSlug = "";
  doc?: Doc;
  /** The open draft, shown to editors (ADR-0027). */
  draft?: DraftInfo;
  showDiff = false;
  /** Requested language in multilingual projects. */
  lang = "";
  /** Changes of the main language since the shown translation was made. */
  mainDiff?: Hunk[];
  /** Other decisions by slug, to show numbers and titles of linked ones. */
  related = new Map<string, Decision>();
  error = "";
  confirming = false;
  private loaded = "";

  /** Loads the page again in place (after its access changed); open dialogs stay. */
  async reload() {
    const doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}${this.lang ? `?lang=${this.lang}` : ""}`).catch(() => undefined);
    if (doc) this.doc = doc;
    docsVersion.set(docsVersion.get() + 1);
  }

  async willUpdate() {
    // The project's languages arrive with the project; reload once they are known.
    const proj0 = currentProject.get();
    const key = this.project + "/" + this.docSlug + "/" + (proj0?.slug === this.project ? proj0.mainLocale + proj0.locales.join() : "?");
    if (key !== this.loaded) {
      this.loaded = key;
      this.doc = undefined;
      this.confirming = false;
      try {
        const proj = currentProject.get();
        this.lang = proj?.mainLocale ? preferredLocale(proj) : "";
        this.mainDiff = undefined;
        const doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}${this.lang ? `?lang=${this.lang}` : ""}`);
        if (doc.type === "adr" && (doc.content?.supersedes || doc.content?.supersededBy)) {
          const all = await api<Decision[]>("GET", `/projects/${this.project}/decisions`).catch(() => []);
          this.related = new Map(all.map((d) => [d.slug, d]));
        }
        this.draft = undefined;
        this.showDiff = false;
        if (doc.type === "markdown" && ["editor", "owner"].includes(doc.role ?? currentProject.get()?.role ?? "")) {
          this.draft = await api<DraftInfo>("GET", `/projects/${this.project}/documents/${doc.id}/draft`).catch(() => undefined);
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

  private async switchLang(code: string) {
    const proj = currentProject.get();
    if (!proj) return;
    rememberLocale(proj.slug, code);
    this.lang = code;
    this.mainDiff = undefined;
    await this.reload();
  }

  /** Lines of a version's text for comparing. */
  private static lines(type: Doc["type"], c: any): string[] {
    if (type === "markdown") return (c?.markdown ?? "").replace(/\n$/, "").split("\n");
    if (type === "link") return (c?.description ?? "").split("\n");
    return ["context", "decision", "consequences", "alternatives"].flatMap((k) => [`## ${t(`adr.${k}` as any)}`, ...(c?.[k] ?? "").split("\n"), ""]);
  }

  private async toggleMainDiff(d: Doc) {
    if (this.mainDiff) {
      this.mainDiff = undefined;
      return;
    }
    const base = `/projects/${this.project}/documents/${d.id}/versions`;
    try {
      const [from, to] = await Promise.all([api<any>("GET", `${base}/${d.translation!.sourceVersion}`), api<any>("GET", `${base}/${d.version}`)]);
      this.mainDiff = fold(diffLines(PhxDocView.lines(d.type, from.content), PhxDocView.lines(d.type, to.content)));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  /** Language switcher and notes on the shown translation (ADR-0028). */
  private languageBar(d: Doc, canEdit: boolean) {
    const proj = currentProject.get();
    if (!proj?.mainLocale) return "";
    const main = proj.mainLocale;
    const all = [main, ...proj.locales];
    const has = new Map((d.translations ?? []).map((x) => [x.locale, x]));
    const tr = d.translation;
    const wanted = this.lang || main;
    const outdated = tr && tr.sourceVersion < d.version;
    return html`<div class="langs" role="group" aria-label=${t("lang.switch")}>
        ${icon("Globe", 14)}
        ${all.map((code) => {
          const x = has.get(code);
          const stale = x && x.sourceVersion < d.version;
          return html`<button class="lang" aria-pressed=${wanted === code ? "true" : "false"} ?data-missing=${code !== main && !x} @click=${() => this.switchLang(code)}>
            ${languageName(code)}${stale ? html`<span class="dot" title=${t("lang.outdated_short")}></span>` : ""}</button>`;
        })}
        ${canEdit && wanted !== main ? html`<a class="btn ghost small" href="/p/${this.project}/d/${d.slug}/translate/${wanted}">${icon("Pencil", 13)} ${tr ? t("lang.edit_translation") : t("lang.translate")}</a>` : ""}
      </div>
      ${wanted !== main && !tr ? html`<p class="notice" role="note">${icon("Globe", 14)} ${t("lang.missing", { lang: languageName(wanted), main: languageName(main) })}</p>` : ""}
      ${outdated ? html`<div class="notice" role="note">${icon("TriangleAlert", 14)} <span>${t("lang.outdated", { main: languageName(main) })}</span>
          <button class="ghost small" aria-expanded=${this.mainDiff ? "true" : "false"} @click=${() => this.toggleMainDiff(d)}>${t(this.mainDiff ? "lang.hide_main_changes" : "lang.show_main_changes")}</button>
          <button class="ghost small" @click=${() => this.switchLang(main)}>${languageName(main)}</button></div>
        ${this.mainDiff ? html`<div class="diff" role="region" aria-label=${t("lang.show_main_changes")}>${this.mainDiff.map((h) =>
          h.op === "…" ? html`<div class="fold">${h.count === 1 ? t("history.unchanged_one") : t("history.unchanged", { count: h.count })}</div>`
          : html`<div class=${h.op === "+" ? "add" : h.op === "-" ? "del" : ""}>${h.text}</div>`)}</div>` : ""}` : ""}
      ${tr?.machine ? html`<p class="notice" role="note">${icon("Bot", 14)} ${t("lang.machine")}</p>` : ""}`;
  }

  private draftNotice(d: Doc) {
    const dr = this.draft;
    // Drafts belong to the main language.
    if (!dr || d.translation) return "";
    const names = dr.participants.map((p) => p.name).join(", ");
    let hunks: Hunk[] = [];
    if (this.showDiff) {
      const lines = (title: string, md: string) => [`${t("history.title_label")}: ${title}`, "", ...md.replace(/\n$/, "").split("\n")];
      hunks = fold(diffLines(lines(d.title, d.content?.markdown ?? ""), lines(dr.title, dr.markdown)));
    }
    return html`<div class="notice draft" role="note">
        <span class="who">${icon("FilePen", 15)} ${t("draft.open_notice", { names, date: formatDate(dr.updatedAt) })}</span>
        <span class="row">
          <button class="ghost" aria-expanded=${this.showDiff ? "true" : "false"} @click=${() => (this.showDiff = !this.showDiff)}>${t(this.showDiff ? "draft.hide_changes" : "draft.show_changes")}</button>
          <a class="btn" href="/p/${this.project}/d/${d.slug}/edit">${icon("Pencil", 14)} ${t("draft.continue")}</a>
        </span>
      </div>
      ${this.showDiff ? html`<div class="diff" role="region" aria-label=${t("draft.changes")}>${hunks.some((h) => h.op === "+" || h.op === "-") ? hunks.map((h) =>
        h.op === "…" ? html`<div class="fold">${h.count === 1 ? t("history.unchanged_one") : t("history.unchanged", { count: h.count })}</div>`
        : html`<div class=${h.op === "+" ? "add" : h.op === "-" ? "del" : ""}>${h.text}</div>`) : html`<div class="fold">${t("history.no_changes")}</div>`}</div>` : ""}`;
  }

  private body(d: Doc) {
    const c = d.content ?? {};
    if (d.type === "markdown") return html`<div class="prose">${unsafeHTML(renderMarkdown(withoutTitle(c.markdown ?? "", d.title), this.project, d.id))}</div>`;
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
    // The role for this page can differ from the project role (restricted or shared pages).
    const canEdit = ["editor", "owner"].includes(d.role ?? currentProject.get()?.role ?? "");
    const wide = wideReading.get();
    return html`<article class=${wide ? "enter wide" : "enter"}>
      <header>
        <div>
          <h1>${d.title}</h1>
          <div class="meta">
            ${d.number ? html`<span class="chip ref">${adrRef(d.number)}</span>` : ""}
            <span class="chip">${t(`doc.type.${d.type}` as any)}</span>
            ${d.type === "adr" ? html`<span class="chip status-${c.status}">${t(`adr.status.${c.status}` as any)}</span>${c.date ? html`<span class="chip">${c.date}</span>` : ""}` : ""}
            <span class="chip">${t("doc.version", { version: d.version })}</span>
            <span class="chip">${t("doc.updated", { date: formatDate(d.updatedAt) })}</span>
            ${d.restricted ? html`<span class="chip" title=${t("share.restrict_hint")}>${icon("Lock", 11)} ${t("share.restricted")}</span>` : ""}
          </div>
        </div>
        <div class="row">
          ${this.favoriteButtons(d)}
          <button class="ghost icon" aria-pressed=${wide ? "true" : "false"} title=${t(wide ? "doc.narrow" : "doc.wide")} aria-label=${t(wide ? "doc.narrow" : "doc.wide")}
            @click=${() => setWideReading(!wide)}>${icon(wide ? "Minimize2" : "Maximize2", 15)}</button>
          <a class="btn ghost" href="/p/${this.project}/d/${d.slug}/history">${icon("History", 14)} ${t("doc.history")}</a>
          ${canEdit ? html`<button class="ghost" @click=${() => this.renderRoot.querySelector<PhxDocShare>("phx-doc-share")!.open()}>${icon("Share2", 14)} ${t("share.button")}</button>
          ${this.draft ? "" : html`<a class="btn ghost" href="/p/${this.project}/d/${d.slug}/edit">${icon("Pencil", 14)} ${t("doc.edit")}</a>`}
          ${this.confirming
            ? html`<span class="muted">${t("doc.delete_confirm")}</span><button class="danger" @click=${this.deleteDoc}>${t("doc.delete")}</button><button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button>`
            : html`<button class="danger" @click=${() => (this.confirming = true)}>${icon("Trash2", 14)} ${t("doc.delete")}</button>`}` : ""}
        </div>
      </header>
      ${d.type === "adr" && c.status === "superseded" && c.supersededBy
        ? html`<p class="notice" role="note">${t("adr.superseded_notice")} ${t("adr.superseded_by")}: ${this.decisionLink(c.supersededBy)}</p>` : ""}
      ${this.languageBar(d, canEdit)}
      ${this.draftNotice(d)}
      ${this.body(d)}
      ${canEdit ? html`<phx-doc-share .project=${currentProject.get()} .doc=${d} @changed=${() => this.reload()}></phx-doc-share>` : ""}
      ${d.type !== "link" ? html`<phx-attachments style="margin-top:28px" .project=${this.project} .document=${d.id} .editable=${canEdit}></phx-attachments>` : ""}
    </article>`;
  }
}
customElements.define("phx-doc-view", PhxDocView);
