import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, unsafeCSS } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, adrRef, api, type Decision, type Doc, type DraftInfo } from "../api";
import { diffLines, fold, type Hunk } from "../diff";
import { languageName } from "../languages";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown, withoutTitle } from "../md";
import { contentLocale, currentProject, docsVersion, favorites, navigate, setWideReading, wideReading } from "../state";
import "./request-block";
import "./board";
import "./floorplan";
import { recordView } from "../recent";
import { blockStyles } from "./blocks";
import { base } from "./shared";
import "./attachments";
import "./page-rules";
import "./doc-share";
import "./comments";
import type { PhxComments } from "./comments";
import { anchorOf, blockFor, type CommentThread } from "../comments";
import type { PhxDocShare } from "./doc-share";
import { toast } from "./toasts";
import { fileURL } from "../files";

export const prose = `
  .prose { line-height: 1.65; }
  .prose h1, .prose h2, .prose h3 { margin: 1.4em 0 .5em; }
  .prose pre { background: var(--surface2); padding: 12px 14px; border-radius: 8px; overflow-x: auto; }
  .prose code { font-family: var(--font-mono); font-size: .9em; }
  .prose a[title="mention"] { background: var(--accent-soft); padding: 0 5px; border-radius: 4px; text-decoration: none; }
  .prose table { border-collapse: collapse; } .prose td, .prose th { border: 1px solid var(--line); padding: 6px 10px; }
  .prose img, .prose .ed-img img { max-width: 100%; height: auto; border-radius: 8px; vertical-align: middle; }
  .prose .ed-img { display: inline-block; max-width: 100%; }
  ${blockStyles}
`;

export class PhxDocView extends SignalWatcher(LitElement) {
  static properties = { project: {}, docSlug: {}, zoom: { state: true }, doc: { state: true }, draft: { state: true }, showDiff: { state: true }, lang: { state: true }, mainDiff: { state: true }, related: { state: true }, error: { state: true }, confirming: { state: true }, archiving: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    /* Centred reading column; "full width" lifts the limit. */
    article { max-width: var(--reading-width, 820px); margin-inline: auto; }
    article.wide { max-width: none; }
    header { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; flex-wrap: wrap; margin-bottom: 14px; }
    .meta { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
    section { margin-top: 18px; }
    section h2 { font-size: 15px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); font-family: var(--font-body); margin-bottom: 4px; }
    .prose img { cursor: zoom-in; }
    dialog.zoom { border: none; padding: 0; background: transparent; max-width: 94vw; max-height: 92vh; overflow: visible;
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out); @starting-style { opacity: 0; transform: scale(.97); } }
    dialog.zoom::backdrop { background: color-mix(in srgb, #000 70%, transparent); }
    dialog.zoom figure { margin: 0; display: grid; gap: 8px; justify-items: center; }
    dialog.zoom img { max-width: 94vw; max-height: 86vh; border-radius: 8px; object-fit: contain; cursor: zoom-out; }
    dialog.zoom figcaption { color: var(--on-color); font-size: 14px; }
    dialog.zoom .close { position: fixed; top: 16px; right: 16px; background: var(--surface); }
    @media (prefers-reduced-motion: reduce) { dialog.zoom { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
    .notice.small-note { padding: 6px 12px; font-size: 13px; color: var(--muted); }
    .notice { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; padding: 10px 14px; margin: 0 0 16px; border-radius: 8px;
      background: var(--surface2); border-left: 3px solid var(--accent); font-size: 14px; }
    .ref { font-family: var(--font-mono); font-size: 13px; }
    button.icon, a.btn.icon { padding: 7px 9px; }
    button.icon[aria-pressed="true"] { color: var(--accent); border-color: var(--accent); }
    button.icon[aria-pressed="true"] .ico { fill: currentColor; }
    .small { font-size: 13px; padding: 3px 9px; }
    .draft { border-left-color: var(--warn, var(--accent)); justify-content: space-between; }
    .draft .who { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .diff { font-family: var(--font-mono); font-size: 13px; line-height: 1.55; border: 1px solid var(--line); border-radius: 8px; overflow-x: auto; background: var(--surface); margin: -6px 0 18px; }
    .diff div { white-space: pre-wrap; word-break: break-word; padding: 0 12px 0 28px; position: relative; min-height: 1.55em; }
    .diff div::before { position: absolute; left: 10px; color: var(--muted); }
    .diff .add { background: color-mix(in srgb, var(--good) 14%, transparent); } .diff .add::before { content: "+"; color: var(--good); }
    .diff .del { background: color-mix(in srgb, var(--bad) 12%, transparent); } .diff .del::before { content: "−"; color: var(--bad); }
    .diff .fold { color: var(--muted); font-family: var(--font-body); font-size: 12.5px; padding: 4px 12px; background: var(--surface2); }
    /* Comments on blocks: a bar marks commented blocks, buttons appear in the gutter next to the hovered block. */
    article { position: relative; }
    .prose > .commented { position: relative; }
    .prose > .commented::before { content: ""; position: absolute; left: -14px; top: 3px; bottom: 3px; width: 3px; border-radius: 2px; background: var(--accent); }
    .prose > phx-request-block.commented { box-shadow: -14px 0 0 -11px var(--accent); }
    .prose > .flash { background: var(--accent-soft); border-radius: 6px; transition: background var(--dur) var(--ease-out); }
    .cbtns { position: absolute; left: -44px; display: grid; gap: 4px; opacity: 0; pointer-events: none; transition: opacity var(--dur-fast) ease; }
    .cbtns.on { opacity: 1; pointer-events: auto; }
    .cbtns button { padding: 4px 6px; font-size: 12px; gap: 3px; }
    @media (max-width: 1100px) { .cbtns { left: auto; right: -6px; grid-auto-flow: column; transform: translateY(-100%); } }
    @media (prefers-reduced-motion: reduce) { .prose > .flash { transition: none; } }
  `];
  project = "";
  docSlug = "";
  doc?: Doc;
  /** The open draft, shown to editors (ADR-0027). */
  draft?: DraftInfo;
  archiving = false;
  showDiff = false;
  /** Image shown large (click on a picture in the page). */
  zoom?: { src: string; alt: string };
  /** Requested language in multilingual projects. */
  lang = "";
  /** Changes of the main language since the shown translation was made. */
  mainDiff?: Hunk[];
  /** Other decisions by slug, to show numbers and titles of linked ones. */
  related = new Map<string, Decision>();
  error = "";
  confirming = false;
  private loaded = "";
  /** Open comment threads, to mark the blocks they are about. */
  private threads: CommentThread[] = [];
  private hovered?: HTMLElement;

  /** Loads the page again in place (after its access changed); open dialogs stay. */
  async reload() {
    const doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}${this.lang ? `?lang=${this.lang}` : ""}`).catch(() => undefined);
    if (doc) {
      // A draft may have been opened, published or discarded meanwhile.
      this.draft = (doc.type === "markdown" || doc.type === "floorplan") && ["editor", "owner"].includes(doc.role ?? currentProject.get()?.role ?? "")
        ? await api<DraftInfo>("GET", `/projects/${this.project}/documents/${doc.id}/draft`).catch(() => undefined)
        : undefined;
      this.doc = doc;
    }
    docsVersion.set(docsVersion.get() + 1);
  }

  async willUpdate() {
    // The project's languages arrive with the project; reload once they are known.
    const proj0 = currentProject.get();
    const key = this.project + "/" + this.docSlug + "/" + (proj0?.slug === this.project ? proj0.mainLocale + proj0.locales.join() : "?") + "/" + contentLocale.get();
    if (key !== this.loaded) {
      this.loaded = key;
      this.doc = undefined;
      this.error = ""; // from a page loaded before (e.g. with the previous project)
      this.confirming = false;
      try {
        const proj = currentProject.get();
        this.lang = proj?.mainLocale ? contentLocale.get() || proj.mainLocale : "";
        this.mainDiff = undefined;
        const doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}${this.lang ? `?lang=${this.lang}` : ""}`);
        if (doc.type === "adr" && (doc.content?.supersedes || doc.content?.supersededBy)) {
          const all = await api<Decision[]>("GET", `/projects/${this.project}/decisions`).catch(() => []);
          this.related = new Map(all.map((d) => [d.slug, d]));
        }
        this.draft = undefined;
        this.showDiff = false;
        if ((doc.type === "markdown" || doc.type === "floorplan") && ["editor", "owner"].includes(doc.role ?? currentProject.get()?.role ?? "")) {
          this.draft = await api<DraftInfo>("GET", `/projects/${this.project}/documents/${doc.id}/draft`).catch(() => undefined);
        }
        if (this.loaded !== key) return; // a newer load started meanwhile
        this.doc = doc;
        recordView(this.project, "document", doc.id);
      } catch (e) {
        if (this.loaded !== key) return;
        this.error = e instanceof ApiError ? e.text : t("error.unknown");
      }
    }
  }

  /** Top-level blocks of the rendered text (paragraphs, headings, lists, code, request blocks …). */
  private blocks(): HTMLElement[] {
    return [...this.renderRoot.querySelectorAll<HTMLElement>(".prose > *")].filter((b) => (b.textContent ?? "").trim() !== "");
  }

  /** Threads per block index. */
  private threadsByBlock(blocks: HTMLElement[]): Map<number, CommentThread[]> {
    const texts = blocks.map((b) => b.textContent ?? "");
    const by = new Map<number, CommentThread[]>();
    for (const th of this.threads) {
      if (th.resolvedAt || !th.anchor) continue;
      const i = blockFor(th.anchor, texts);
      if (i >= 0) by.set(i, [...(by.get(i) ?? []), th]);
    }
    return by;
  }

  private markBlocks() {
    const blocks = this.blocks();
    const by = this.threadsByBlock(blocks);
    blocks.forEach((b, i) => b.classList.toggle("commented", by.has(i)));
    const c = this.renderRoot.querySelector<PhxComments>("phx-comments");
    if (c) c.blockTexts = blocks.map((b) => b.textContent ?? "");
  }

  updated() {
    this.markBlocks();
  }

  private onComments(e: CustomEvent<CommentThread[]>) {
    this.threads = e.detail;
    this.markBlocks();
  }

  /** Shows the gutter buttons next to the block under the pointer. */
  private onHover(e: MouseEvent) {
    const block = (e.target as HTMLElement).closest<HTMLElement>(".prose > *");
    const btns = this.renderRoot.querySelector<HTMLElement>(".cbtns");
    if (!btns || !block || (block.textContent ?? "").trim() === "") return;
    if (block === this.hovered) return;
    this.hovered = block;
    const article = this.renderRoot.querySelector("article")!;
    btns.style.top = `${block.getBoundingClientRect().top - article.getBoundingClientRect().top}px`;
    const blocks = this.blocks();
    const list = this.threadsByBlock(blocks).get(blocks.indexOf(block)) ?? [];
    const count = btns.querySelector<HTMLButtonElement>(".count")!;
    count.style.display = list.length ? "" : "none";
    count.querySelector("span")!.textContent = String(list.length);
    count.dataset.thread = list[0]?.id ?? "";
    btns.classList.add("on");
  }

  private hideGutter(e: MouseEvent) {
    const to = e.relatedTarget as HTMLElement | null;
    if (to && (to.closest(".cbtns") || to.closest(".prose"))) return;
    this.hovered = undefined;
    this.renderRoot.querySelector(".cbtns")?.classList.remove("on");
  }

  private commentBlock() {
    if (!this.hovered) return;
    this.renderRoot.querySelector<PhxComments>("phx-comments")?.startOnBlock(anchorOf(this.hovered.textContent ?? ""));
  }

  private showThread(e: Event) {
    const id = (e.currentTarget as HTMLElement).dataset.thread;
    if (id) this.renderRoot.querySelector<PhxComments>("phx-comments")?.reveal(id);
  }

  /** Scrolls to the block a thread quotes and highlights it. */
  private showBlock(e: CustomEvent<string>) {
    const blocks = this.blocks();
    const b = blocks[blockFor(e.detail, blocks.map((x) => x.textContent ?? ""))];
    if (!b) return;
    b.scrollIntoView({ block: "center", behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
    b.classList.add("flash");
    setTimeout(() => b.classList.remove("flash"), 1600);
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

  /** Keeps a copy of the link's target (a manual, a data sheet) as a project file. */
  private async archive(d: Doc) {
    this.archiving = true;
    try {
      await api("POST", `/projects/${this.project}/documents/${d.slug}/archive`);
      toast(t("link.archived"), "ok");
      await this.reload();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.archiving = false;
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

  /** Notes on the shown language (ADR-0028); the switcher sits in the header. */
  private languageNotes(d: Doc) {
    const proj = currentProject.get();
    if (!proj?.mainLocale) return "";
    const main = proj.mainLocale;
    const tr = d.translation;
    const wanted = this.lang || main;
    const outdated = tr && tr.sourceVersion < d.version;
    return html`
      ${wanted !== main && !tr ? html`<p class="notice small-note" role="note">${icon("Globe", 14)} ${t("lang.not_translated", { lang: languageName(wanted), main: languageName(main) })}</p>` : ""}
      ${outdated ? html`<div class="notice" role="note">${icon("TriangleAlert", 14)} <span>${t("lang.outdated", { main: languageName(main) })}</span>
          <button class="ghost small" aria-expanded=${this.mainDiff ? "true" : "false"} @click=${() => this.toggleMainDiff(d)}>${t(this.mainDiff ? "lang.hide_main_changes" : "lang.show_main_changes")}</button></div>
        ${this.mainDiff ? html`<div class="diff" role="region" aria-label=${t("lang.show_main_changes")}>${this.mainDiff.map((h) =>
          h.op === "…" ? html`<div class="fold">${h.count === 1 ? t("history.unchanged_one") : t("history.unchanged", { count: h.count })}</div>`
          : html`<div class=${h.op === "+" ? "add" : h.op === "-" ? "del" : ""}>${h.text}</div>`)}</div>` : ""}` : ""}
      ${tr?.machine ? html`<p class="notice" role="note">${icon("Bot", 14)} ${t("lang.machine")}</p>` : ""}`;
  }

  /** "Translate" next to "Edit" when a further language is chosen. */
  private translateButton(d: Doc) {
    const proj = currentProject.get();
    if (!proj?.mainLocale || !this.lang || this.lang === proj.mainLocale) return "";
    const label = d.translation ? t("lang.edit_translation") : t("lang.translate");
    return html`<a class="btn ghost icon" href="/p/${this.project}/d/${d.slug}/translate/${this.lang}" title=${label} aria-label=${label}>${icon("Languages", 15)}</a>`;
  }

  /** Pictures in the text open large. */
  private onClick = (e: Event) => {
    const img = e.composedPath().find((n): n is HTMLImageElement => n instanceof HTMLImageElement);
    if (!img || !img.closest(".prose") || img.closest("a")) return;
    this.zoom = { src: img.currentSrc || img.src, alt: img.alt };
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLDialogElement>("dialog.zoom")?.showModal());
  };

  private zoomDialog() {
    const z = this.zoom;
    if (!z) return "";
    const close = () => { this.renderRoot.querySelector<HTMLDialogElement>("dialog.zoom")?.close(); };
    return html`<dialog class="zoom" aria-label=${z.alt || t("picture.zoom")} @close=${() => (this.zoom = undefined)} @click=${close}>
      <figure><img src=${z.src} alt=${z.alt}>${z.alt ? html`<figcaption>${z.alt}</figcaption>` : ""}</figure>
      <button class="ghost icon close" aria-label=${t("board.close")} @click=${close}>${icon("X", 16)}</button>
    </dialog>`;
  }

  private draftNotice(d: Doc) {
    const dr = this.draft;
    // Drafts belong to the main language; floor plans show their own note.
    if (!dr || d.translation || d.type === "floorplan") return "";
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
    if (d.type === "board") return html`${c.description ? html`<div class="prose">${unsafeHTML(renderMarkdown(c.description, this.project, d.id))}</div>` : ""}
      <phx-board .project=${this.project} .doc=${d} @changed=${() => this.reload()}></phx-board>`;
    if (d.type === "floorplan") return html`${c.description ? html`<div class="prose">${unsafeHTML(renderMarkdown(c.description, this.project, d.id))}</div>` : ""}
      <phx-floorplan .project=${this.project} .doc=${d} .draft=${this.draft} ?canEdit=${["editor", "owner"].includes(d.role ?? currentProject.get()?.role ?? "")} @changed=${() => this.reload()}></phx-floorplan>`;
    if (d.type === "markdown") return html`<div class="prose">${unsafeHTML(renderMarkdown(withoutTitle(c.markdown ?? "", d.title), this.project, d.id))}</div>`;
    if (d.type === "link") return html`<p><a class="btn" href=${c.url} target="_blank" rel="noopener external">${icon("ExternalLink", 15)} ${t("doc.open_link")}</a></p>
      <p class="mono muted">${c.url}</p>
      <p class="row">${c.archive ? html`<a class="btn ghost" href=${fileURL({ id: c.archive.file, name: d.title })} target="_blank">${icon("Archive", 15)} ${t("link.archived_copy", { date: formatDate(c.archive.at) })}</a>` : ""}
        ${["editor", "owner"].includes(d.role ?? currentProject.get()?.role ?? "") ? html`<button class="ghost" ?disabled=${this.archiving} @click=${() => this.archive(d)}>${icon("Download", 15)} ${c.archive ? t("link.archive_again") : t("link.archive")}</button>` : ""}</p>
      ${c.description ? html`<div class="prose">${unsafeHTML(renderMarkdown(c.description, this.project))}</div>` : ""}`;
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
    // Boards need the room of the whole page.
    return html`<article class=${wide || d.type === "board" || d.type === "floorplan" ? "enter wide" : "enter"} @click=${this.onClick}
      @mouseover=${this.onHover} @mouseout=${this.hideGutter} @comments-changed=${this.onComments} @show-block=${this.showBlock}>
      ${d.type === "link" ? "" : html`<div class="cbtns" role="group" aria-label=${t("comment.block")}>
        <button class="ghost" title=${t("comment.on_block")} aria-label=${t("comment.on_block")} @click=${this.commentBlock}>${icon("MessageSquarePlus", 15)}</button>
        <button class="ghost count" style="display:none" title=${t("comment.show_threads")} @click=${this.showThread}>${icon("MessageSquare", 13)}<span></span></button>
      </div>`}
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
          ${d.type === "board" || d.type === "floorplan" ? "" : html`<button class="ghost icon" aria-pressed=${wide ? "true" : "false"} title=${t(wide ? "doc.narrow" : "doc.wide")} aria-label=${t(wide ? "doc.narrow" : "doc.wide")}
            @click=${() => setWideReading(!wide)}>${icon(wide ? "Minimize2" : "Maximize2", 15)}</button>`}
          <a class="btn ghost" href="/p/${this.project}/d/${d.slug}/history">${icon("History", 14)} ${t("doc.history")}</a>
          ${canEdit ? html`<button class="ghost" @click=${() => this.renderRoot.querySelector<PhxDocShare>("phx-doc-share")!.open()}>${icon("Share2", 14)} ${t("share.button")}</button>
          ${this.translateButton(d)}
          ${this.draft && d.type !== "floorplan" ? "" : html`<a class="btn ghost" href="/p/${this.project}/d/${d.slug}/edit">${icon("Pencil", 14)} ${t("doc.edit")}</a>`}
          ${this.confirming
            ? html`<span class="muted">${t("doc.delete_confirm")}</span><button class="danger" @click=${this.deleteDoc}>${t("doc.delete")}</button><button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button>`
            : html`<button class="danger" @click=${() => (this.confirming = true)}>${icon("Trash2", 14)} ${t("doc.delete")}</button>`}` : ""}
        </div>
      </header>
      ${d.type === "adr" && c.status === "superseded" && c.supersededBy
        ? html`<p class="notice" role="note">${t("adr.superseded_notice")} ${t("adr.superseded_by")}: ${this.decisionLink(c.supersededBy)}</p>` : ""}
      ${this.languageNotes(d)}
      ${this.draftNotice(d)}
      <phx-page-rules .project=${this.project} .doc=${d.id} .version=${d.version} ?canEdit=${canEdit}></phx-page-rules>
      ${this.body(d)}
      <phx-page-rules mode="settings" .project=${this.project} .doc=${d.id} .version=${d.version} ?canEdit=${canEdit}></phx-page-rules>
      ${canEdit ? html`<phx-doc-share .project=${currentProject.get()} .doc=${d} @changed=${() => this.reload()}></phx-doc-share>` : ""}
      ${d.type !== "link" ? html`<phx-comments .project=${this.project} .document=${d.id}></phx-comments>` : ""}
      ${d.type !== "link" ? html`<phx-attachments style="margin-top:28px" .project=${this.project} .document=${d.id} .editable=${canEdit}></phx-attachments>` : ""}
      ${this.zoomDialog()}
    </article>`;
  }
}
customElements.define("phx-doc-view", PhxDocView);
