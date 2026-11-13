import { $createCodeNode } from "@lexical/code";
import { TOGGLE_LINK_COMMAND } from "@lexical/link";
import { INSERT_CHECK_LIST_COMMAND, INSERT_ORDERED_LIST_COMMAND, INSERT_UNORDERED_LIST_COMMAND } from "@lexical/list";
import { $createHeadingNode, $createQuoteNode, type HeadingTagType } from "@lexical/rich-text";
import { $setBlocksType } from "@lexical/selection";
import { $deleteTableColumnAtSelection, $deleteTableRowAtSelection, $insertTableColumnAtSelection, $insertTableRowAtSelection, $isTableSelection, INSERT_TABLE_COMMAND } from "@lexical/table";
import { $createHorizontalRuleNode } from "@lexical/extension";
import { $insertNodeToNearestRoot } from "@lexical/utils";
import { $createParagraphNode, $getSelection, $isRangeSelection, FORMAT_TEXT_COMMAND, REDO_COMMAND, UNDO_COMMAND } from "lexical";
import { LitElement, css, html, unsafeCSS } from "lit";
import { ApiError, api, type Doc, type DraftInfo } from "../api";
import { createLiveEditor, type LiveEditor, type Peer, type Status } from "../editor/editor";
import { lossless } from "../editor/markdown";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser, docsVersion, navigate } from "../state";
import { prose } from "./doc-view";
import { base } from "./shared";
import { fileURL, isImage, uploadFile } from "../files";
import type { PhxAttachments } from "./attachments";
import "./attachments";
import { toast } from "./toasts";

/** Stable caret color per user from the collaboration tokens. */
function userColor(id: string): string {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  const v = getComputedStyle(document.documentElement).getPropertyValue(`--collab-${(h % 8) + 1}`).trim();
  return v || "#d9480f";
}

/** Live editing of a Markdown page (Lexical + Yjs). */
export class PhxLiveEditor extends LitElement {
  static properties = { project: {}, docSlug: {}, doc: { state: true }, draft: { state: true }, source: { state: true }, status: { state: true }, saving: { state: true },
    peers: { state: true }, error: { state: true }, asking: { state: true }, inTable: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    header { display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 10px; }
    header .title { flex: 2; min-width: 220px; } header .cat { flex: 1; min-width: 160px; }
    .title input { font: 700 26px var(--font-display); padding: 6px 10px; }
    .bar { position: sticky; top: var(--header-h); z-index: 4; display: flex; gap: 2px; align-items: center; flex-wrap: wrap;
      background: var(--bg); padding: 6px 0; border-bottom: 1px solid var(--line); margin-bottom: 14px; }
    .bar button { background: none; border: 1px solid transparent; color: var(--fg); padding: 5px 7px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { .bar button:hover { background: var(--surface2); } }
    .bar .sep { width: 1px; height: 20px; background: var(--line); margin: 0 4px; }
    .bar .state { margin-left: auto; display: flex; gap: 10px; align-items: center; font-size: 13px; color: var(--muted); }
    .avatars { display: flex; } .avatars span { width: 24px; height: 24px; border-radius: 50%; display: grid; place-items: center; color: #fff;
      font: 600 10px var(--font-mono); margin-left: -6px; border: 2px solid var(--bg); }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); display: inline-block; }
    .dot.connected { background: var(--good); } .dot.disconnected { background: var(--bad); }
    .wrap { position: relative; }
    .content { min-height: 50vh; outline: none; padding: 4px 2px 80px; }
    .content:focus-visible { outline: none; }
    .content > :first-child { margin-top: 0; }
    .placeholder { position: absolute; top: 4px; left: 2px; color: var(--muted); pointer-events: none; }
    /* Lexical theme classes */
    .ed-p { margin: 0 0 .7em; } .ed-h1, .ed-h2, .ed-h3 { font-family: var(--font-display); margin: 1.1em 0 .4em; }
    .ed-h1 { font-size: 1.8em; } .ed-h2 { font-size: 1.4em; } .ed-h3 { font-size: 1.15em; }
    .ed-quote { border-left: 3px solid var(--line); margin: 0 0 .7em; padding-left: 12px; color: var(--muted); }
    .ed-ul, .ed-ol { margin: 0 0 .7em; padding-left: 24px; } .ed-nested { list-style: none; }
    .ed-code { display: block; font-family: var(--font-mono); font-size: 13px; background: var(--surface2); padding: 12px 14px; border-radius: 8px; white-space: pre-wrap; margin: 0 0 .7em; }
    .ed-tcode { font-family: var(--font-mono); font-size: .9em; background: var(--surface2); padding: 1px 4px; border-radius: 4px; }
    .ed-bold { font-weight: 700; } .ed-italic { font-style: italic; } .ed-strike { text-decoration: line-through; } .ed-underline { text-decoration: underline; }
    .ed-link { color: var(--accent); }
    .ed-img { display: inline-block; max-width: 100%; } .ed-img img { max-width: 100%; height: auto; border-radius: 8px; vertical-align: middle; }
    .ed-table { border-collapse: collapse; margin: 0 0 .9em; width: max-content; max-width: 100%; display: block; overflow-x: auto; }
    .ed-td, .ed-th { border: 1px solid var(--line); padding: 6px 10px; vertical-align: top; min-width: 60px; text-align: left; }
    .ed-th { background: var(--surface2); font-weight: 600; }
    .ed-td > p, .ed-th > p { margin: 0; }
    .ed-table-sel *::selection { background: none; }
    .ed-hr { border: none; border-top: 1px solid var(--line); margin: 1.2em 0; }
    .ed-hr.selected { outline: 2px solid var(--accent); }
    .ed-checked, .ed-unchecked { list-style: none; position: relative; padding-left: 26px; margin-left: -22px; }
    .ed-checked::before, .ed-unchecked::before { content: ""; position: absolute; left: 2px; top: .25em; width: 14px; height: 14px;
      border: 1.5px solid var(--muted); border-radius: 4px; cursor: pointer; }
    .ed-checked::before { background: var(--accent); border-color: var(--accent); }
    .ed-checked::after { content: ""; position: absolute; left: 7px; top: calc(.25em + 3px); width: 4px; height: 8px;
      border: solid var(--bg); border-width: 0 2px 2px 0; transform: rotate(45deg); pointer-events: none; }
    .ed-checked { color: var(--muted); text-decoration: line-through; }
    .hint { font-size: 13px; color: var(--muted); }
    .actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .banner { display: flex; gap: 10px; align-items: flex-start; padding: 10px 12px; border-radius: 8px; margin-bottom: 12px;
      background: var(--surface2); border: 1px solid var(--line); font-size: 14px; }
    .banner.warn { border-color: var(--warn, var(--accent)); }
    textarea.source { width: 100%; min-height: 60vh; font: 13px/1.55 var(--font-mono); padding: 12px 14px; resize: vertical; }
    dialog { border: 1px solid var(--line); border-radius: 12px; padding: 20px 22px; max-width: 460px; background: var(--bg); color: var(--fg); }
    dialog::backdrop { background: color-mix(in srgb, #000 40%, transparent); }
    dialog h2 { margin: 0 0 8px; font-size: 18px; }
    dialog ul { margin: 6px 0 12px; padding-left: 20px; }
    dialog .row { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
  `];
  project = "";
  docSlug = "";
  doc?: Doc;
  draft?: DraftInfo;
  /** Markdown source mode: the page has content the rich editor would lose. */
  source = false;
  asking: "" | "cancel" | "publish" = "";
  inTable = false;
  status: Status = "connecting";
  saving = false;
  peers: Peer[] = [];
  error = "";
  private live?: LiveEditor;
  private loaded = "";
  private empty = true;
  private ended = false;
  private sourceTimer?: number;
  private sourceDirty = false;

  async willUpdate() {
    const key = `${this.project}/${this.docSlug}`;
    if (key !== this.loaded) {
      this.loaded = key;
      await this.start();
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this.source) this.flushSource();
    this.live?.destroy();
    this.live = undefined;
  }

  private async start() {
    await this.live?.destroy();
    this.live = undefined;
    this.ended = false;
    const base = `/projects/${this.project}/documents/${this.docSlug}`;
    try {
      this.doc = await api<Doc>("GET", base);
      // Opening the editor starts the shared draft or joins it (ADR-0027).
      this.draft = await api<DraftInfo>("POST", `/projects/${this.project}/documents/${this.doc.id}/draft`);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
      return;
    }
    // Content the rich editor cannot show exactly is edited as Markdown source.
    this.source = !lossless(this.draft.markdown);
    await this.updateComplete;
    if (this.source) return;
    const u = currentUser.get()!;
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const element = this.renderRoot.querySelector<HTMLElement>(".content")!;
    this.live = createLiveEditor({
      element, cursors: this.renderRoot.querySelector<HTMLElement>(".cursors")!,
      url: `${proto}//${location.host}/rt/doc/${this.project}`, docId: this.doc.id,
      markdown: this.draft.markdown, user: { name: u.name, color: userColor(u.id) }, editable: true,
      onSave: (markdown) => this.saveDraft({ markdown }),
      onStatus: (s) => (this.status = s),
      onSaving: (s) => (this.saving = s),
      onPeers: (p) => (this.peers = p),
      upload: async (file) => {
        const f = await uploadFile(this.project, file, { document: this.doc!.id });
        this.renderRoot.querySelector<PhxAttachments>("phx-attachments")?.refresh();
        return { url: fileURL(f), name: f.name, image: isImage(f.mime) };
      },
      onUploadError: (err) => toast(err instanceof ApiError ? err.text : t("error.unknown"), "error"),
      onReset: (by) => {
        toast(t("editor.reset", { by }));
        this.loaded = "";
        this.requestUpdate();
      },
      onEnded: (reason, by) => this.onEnded(reason, by),
    });
    this.live.editor.registerUpdateListener(({ editorState }) => {
      const empty = editorState.read(() => (element.textContent ?? "").trim() === "");
      if (empty !== this.empty) {
        this.empty = empty;
        this.requestUpdate();
      }
      const inTable = editorState.read(() => {
        const sel = $getSelection();
        if ($isTableSelection(sel)) return true;
        if (!$isRangeSelection(sel)) return false;
        let n: any = sel.anchor.getNode();
        while (n) {
          if (n.getType?.() === "tablecell") return true;
          n = n.getParent?.();
        }
        return false;
      });
      if (inTable !== this.inTable) this.inTable = inTable;
    });
  }

  private async saveDraft(patch: { markdown?: string; title?: string; category?: string }) {
    if (this.ended || !this.doc) return;
    try {
      this.draft = await api<DraftInfo>("PUT", `/projects/${this.project}/documents/${this.doc.id}/draft`, patch);
    } catch (e) {
      // The draft ended meanwhile (published or cancelled by someone else).
      if (e instanceof ApiError && e.key === "validation.draft_ended") {
        this.onEnded("", "");
        return;
      }
      throw e;
    }
  }

  /** The session ended for everyone: published, cancelled or page deleted. */
  private onEnded(reason: string, by: string) {
    if (this.ended) return;
    this.ended = true;
    const me = currentUser.get()?.name;
    if (by !== me) {
      const key = reason === "published" ? "draft.ended_published" : reason === "deleted" ? "draft.ended_deleted" : reason === "cancelled" ? "draft.ended_cancelled" : "draft.ended";
      toast(t(key, { by }));
    }
    docsVersion.set(docsVersion.get() + 1);
    navigate(reason === "deleted" ? `/p/${this.project}` : `/p/${this.project}/d/${this.doc?.slug ?? this.docSlug}`);
  }

  private onSource(value: string) {
    this.sourceDirty = true;
    window.clearTimeout(this.sourceTimer);
    this.sourceTimer = window.setTimeout(() => this.flushSource(value), 1200);
  }

  private async flushSource(value?: string) {
    window.clearTimeout(this.sourceTimer);
    if (!this.sourceDirty) return;
    const md = value ?? this.renderRoot.querySelector<HTMLTextAreaElement>("textarea.source")?.value ?? "";
    this.sourceDirty = false;
    this.saving = true;
    try {
      await this.saveDraft({ markdown: md });
    } catch (e) {
      this.sourceDirty = true;
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.saving = false;
    }
  }

  private async flushAll() {
    if (this.source) await this.flushSource();
    else await this.live?.flush();
  }

  private format(kind: "bold" | "italic" | "code") {
    this.live?.markInput();
    this.live?.editor.dispatchCommand(FORMAT_TEXT_COMMAND, kind);
  }

  private block(kind: HeadingTagType | "quote" | "code" | "paragraph") {
    this.live?.markInput();
    this.live?.editor.update(() => {
      const sel = $getSelection();
      if (!$isRangeSelection(sel)) return;
      $setBlocksType(sel, () => kind === "quote" ? $createQuoteNode() : kind === "code" ? $createCodeNode() : kind === "paragraph" ? $createParagraphNode() : $createHeadingNode(kind));
    });
  }

  private link() {
    const url = prompt(t("editor.link_url"), "https://");
    if (url) {
      this.live?.markInput();
      this.live?.editor.dispatchCommand(TOGGLE_LINK_COMMAND, url);
    }
  }

  private async saveMeta(field: "title" | "category", value: string) {
    if (!this.draft || value === this.draft[field]) return;
    try {
      await this.saveDraft({ [field]: value });
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private insertTable() {
    this.live?.markInput();
    this.live?.editor.dispatchCommand(INSERT_TABLE_COMMAND, { rows: "3", columns: "3", includeHeaders: { rows: true, columns: false } });
  }

  private tableEdit(op: "row" | "col" | "delrow" | "delcol") {
    this.live?.markInput();
    this.live?.editor.update(() => {
      if (op === "row") $insertTableRowAtSelection(true);
      if (op === "col") $insertTableColumnAtSelection(true);
      if (op === "delrow") $deleteTableRowAtSelection();
      if (op === "delcol") $deleteTableColumnAtSelection();
    });
  }

  private insertRule() {
    this.live?.markInput();
    this.live?.editor.update(() => { $insertNodeToNearestRoot($createHorizontalRuleNode()); });
  }

  /** Speichern: keeps the draft (nothing is published) and leaves the editor. */
  private async save() {
    try {
      await this.flushAll();
    } catch {
      toast(t("error.unknown"), "error");
      return;
    }
    toast(t("draft.saved_toast"));
    docsVersion.set(docsVersion.get() + 1);
    navigate(`/p/${this.project}/d/${this.doc?.slug ?? this.docSlug}`);
  }

  private async publish() {
    this.asking = "";
    try {
      await this.flushAll();
      const res = await api<{ document?: Doc }>("POST", `/projects/${this.project}/documents/${this.doc!.id}/draft/publish`);
      this.ended = true;
      toast(t("draft.published_toast"));
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${res.document?.slug ?? this.doc?.slug ?? this.docSlug}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async cancel() {
    this.asking = "";
    try {
      this.ended = true;
      await api("DELETE", `/projects/${this.project}/documents/${this.doc!.id}/draft`);
      toast(t("draft.cancelled_toast"));
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${this.doc?.slug ?? this.docSlug}`);
    } catch (e) {
      this.ended = false;
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private ask(what: "cancel" | "publish") {
    // Publishing alone needs no question; with others present it ends their session.
    if (what === "publish" && !this.peers.length) {
      this.publish();
      return;
    }
    this.asking = what;
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.showModal());
  }

  private dialog() {
    if (!this.asking) return "";
    const me = currentUser.get()?.name;
    const others = [...new Set([...(this.draft?.participants ?? []).map((p) => p.name), ...this.peers.map((p) => p.name)])].filter((n) => n !== me);
    const close = () => { this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close(); this.asking = ""; };
    if (this.asking === "publish") {
      return html`<dialog @close=${close} aria-labelledby="dlg-title">
        <h2 id="dlg-title">${t("draft.publish_title")}</h2>
        <p>${t("draft.publish_text")}</p>
        ${this.peers.length ? html`<ul>${this.peers.map((p) => html`<li>${p.name}</li>`)}</ul>` : ""}
        <div class="row"><button class="ghost" @click=${close}>${t("doc.cancel")}</button>
          <button @click=${() => this.publish()}>${icon("Send", 14)} ${t("draft.publish")}</button></div>
      </dialog>`;
    }
    return html`<dialog @close=${close} aria-labelledby="dlg-title">
      <h2 id="dlg-title">${t("draft.cancel_title")}</h2>
      <p><strong>${t("draft.cancel_warning")}</strong></p>
      ${others.length ? html`<p>${t("draft.cancel_others")}</p><ul>${others.map((n) => html`<li>${n}</li>`)}</ul>` : ""}
      <p class="muted">${t("draft.cancel_hint")}</p>
      <div class="row"><button class="ghost" autofocus @click=${close}>${t("draft.keep")}</button>
        <button class="danger" @click=${() => this.cancel()}>${icon("Trash2", 14)} ${t("draft.cancel_confirm")}</button></div>
    </dialog>`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const d = this.doc;
    const dr = this.draft;
    const tool = (ico: string, label: string, run: () => void) =>
      html`<button type="button" title=${label} aria-label=${label} @mousedown=${(e: Event) => e.preventDefault()} @click=${run}>${icon(ico, 16)}</button>`;
    const state = this.status === "connected" ? t("editor.connected") : this.status === "connecting" ? t("editor.connecting") : t("editor.disconnected");
    const saved = html`<span>${this.saving ? t("editor.saving") : t("draft.saved_state")}</span>`;
    return html`
      <header>
        <label class="field title">${t("doc.title")}<input .value=${dr?.title ?? d?.title ?? ""} @change=${(e: Event) => this.saveMeta("title", (e.target as HTMLInputElement).value)} /></label>
        <label class="field cat">${t("doc.category")}<input .value=${dr?.category ?? d?.category ?? ""} @change=${(e: Event) => this.saveMeta("category", (e.target as HTMLInputElement).value)} /></label>
        <div class="actions">
          ${dr?.canEnd ? html`<button class="ghost" @click=${() => this.ask("cancel")}>${icon("Undo2", 15)} ${t("draft.cancel")}</button>` : ""}
          <button class="ghost" title=${t("draft.save_hint")} @click=${this.save}>${icon("Save", 15)} ${t("draft.save")}</button>
          ${dr?.canEnd ? html`<button @click=${() => this.ask("publish")}>${icon("Send", 15)} ${t("draft.publish")}</button>` : ""}
        </div>
      </header>
      ${dr?.outdated ? html`<div class="banner warn" role="note">${icon("TriangleAlert", 16)} <span>${t("draft.outdated")}</span></div>` : ""}
      ${this.source ? html`
        <div class="banner" role="note">${icon("FileCode", 16)} <span>${t("draft.source_mode")}</span></div>
        <div class="bar"><span class="state" aria-live="polite">${saved}</span></div>
        <textarea class="source" aria-label=${dr?.title ?? ""} spellcheck="true" .value=${dr?.markdown ?? ""}
          @input=${(e: Event) => this.onSource((e.target as HTMLTextAreaElement).value)}></textarea>` : html`
      <div class="bar" role="toolbar" aria-label=${t("editor.toolbar")}>
        ${tool("Undo2", t("editor.undo"), () => this.live?.editor.dispatchCommand(UNDO_COMMAND, undefined))}
        ${tool("Redo2", t("editor.redo"), () => this.live?.editor.dispatchCommand(REDO_COMMAND, undefined))}
        <span class="sep"></span>
        ${tool("Bold", t("editor.bold"), () => this.format("bold"))}
        ${tool("Italic", t("editor.italic"), () => this.format("italic"))}
        ${tool("Code", t("editor.code"), () => this.format("code"))}
        ${tool("Link2", t("editor.link"), () => this.link())}
        <span class="sep"></span>
        ${tool("Heading1", t("editor.h1"), () => this.block("h1"))}
        ${tool("Heading2", t("editor.h2"), () => this.block("h2"))}
        ${tool("Heading3", t("editor.h3"), () => this.block("h3"))}
        ${tool("List", t("editor.ul"), () => { this.live?.markInput(); this.live?.editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined); })}
        ${tool("ListOrdered", t("editor.ol"), () => { this.live?.markInput(); this.live?.editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined); })}
        ${tool("ListChecks", t("editor.checklist"), () => { this.live?.markInput(); this.live?.editor.dispatchCommand(INSERT_CHECK_LIST_COMMAND, undefined); })}
        ${tool("Quote", t("editor.quote"), () => this.block("quote"))}
        ${tool("SquareCode", t("editor.codeblock"), () => this.block("code"))}
        ${tool("Minus", t("editor.hr"), () => this.insertRule())}
        <span class="sep"></span>
        ${tool("Table", t("editor.table"), () => this.insertTable())}
        ${this.inTable ? html`
          ${tool("BetweenHorizontalEnd", t("editor.table_row"), () => this.tableEdit("row"))}
          ${tool("BetweenVerticalEnd", t("editor.table_col"), () => this.tableEdit("col"))}
          ${tool("TableRowsSplit", t("editor.table_delrow"), () => this.tableEdit("delrow"))}
          ${tool("TableColumnsSplit", t("editor.table_delcol"), () => this.tableEdit("delcol"))}` : ""}
        <span class="sep"></span>
        ${tool("Image", t("editor.insert_file"), () => this.renderRoot.querySelector<HTMLInputElement>("input.pick")!.click())}
        <input class="pick" type="file" multiple hidden @change=${(e: Event) => { const i = e.target as HTMLInputElement; if (i.files) this.live?.insertFiles(Array.from(i.files)); i.value = ""; }}>
        <span class="state" aria-live="polite">
          ${this.peers.length ? html`<span class="avatars" title=${t("editor.peers", { names: this.peers.map((p) => p.name).join(", ") })}>
            ${this.peers.map((p) => html`<span style="background:${p.color}">${p.name.slice(0, 2).toUpperCase()}</span>`)}</span>` : ""}
          <span><span class="dot ${this.status}"></span> ${state}</span>
          ${saved}
        </span>
      </div>
      <div class="wrap prose">
        <div class="content" contenteditable="true" role="textbox" aria-multiline="true" aria-label=${d?.title ?? ""} spellcheck="true"></div>
        ${this.empty ? html`<div class="placeholder">${t("editor.placeholder")}</div>` : ""}
        <div class="cursors"></div>
      </div>`}
      <p class="hint">${t("draft.hint")} ${t("doc.mention_hint")} ${t("editor.files_hint")}</p>
      ${d ? html`<phx-attachments style="margin-top:20px" .project=${this.project} .document=${d.id} .editable=${true}></phx-attachments>` : ""}
      ${this.dialog()}`;
  }
}
customElements.define("phx-live-editor", PhxLiveEditor);
