import { $createCodeNode } from "@lexical/code";
import { TOGGLE_LINK_COMMAND } from "@lexical/link";
import { INSERT_ORDERED_LIST_COMMAND, INSERT_UNORDERED_LIST_COMMAND } from "@lexical/list";
import { $createHeadingNode, $createQuoteNode, type HeadingTagType } from "@lexical/rich-text";
import { $setBlocksType } from "@lexical/selection";
import { $createParagraphNode, $getSelection, $isRangeSelection, FORMAT_TEXT_COMMAND, REDO_COMMAND, UNDO_COMMAND } from "lexical";
import { LitElement, css, html, unsafeCSS } from "lit";
import { ApiError, api, type Doc } from "../api";
import { createLiveEditor, type LiveEditor, type Peer, type Status } from "../editor/editor";
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
  static properties = { project: {}, docSlug: {}, doc: { state: true }, status: { state: true }, saving: { state: true }, peers: { state: true }, error: { state: true } };
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
    .hint { font-size: 13px; color: var(--muted); }
  `];
  project = "";
  docSlug = "";
  doc?: Doc;
  status: Status = "connecting";
  saving = false;
  peers: Peer[] = [];
  error = "";
  private live?: LiveEditor;
  private loaded = "";
  private empty = true;

  async willUpdate() {
    const key = `${this.project}/${this.docSlug}`;
    if (key !== this.loaded) {
      this.loaded = key;
      await this.start();
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.live?.destroy();
    this.live = undefined;
  }

  private async start() {
    await this.live?.destroy();
    this.live = undefined;
    try {
      this.doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
      return;
    }
    await this.updateComplete;
    const u = currentUser.get()!;
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const element = this.renderRoot.querySelector<HTMLElement>(".content")!;
    this.live = createLiveEditor({
      element, cursors: this.renderRoot.querySelector<HTMLElement>(".cursors")!,
      url: `${proto}//${location.host}/rt/doc/${this.project}`, docId: this.doc.id,
      markdown: this.doc.content?.markdown ?? "", user: { name: u.name, color: userColor(u.id) }, editable: true,
      onSave: async (markdown) => {
        await api("PUT", `/projects/${this.project}/documents/${this.doc!.id}/live`, { markdown });
      },
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
    });
    this.live.editor.registerUpdateListener(({ editorState }) => {
      const empty = editorState.read(() => (element.textContent ?? "").trim() === "");
      if (empty !== this.empty) {
        this.empty = empty;
        this.requestUpdate();
      }
    });
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
    if (!this.doc || value === (this.doc as any)[field]) return;
    try {
      const res = await api<{ document?: Doc }>("PATCH", `/projects/${this.project}/documents/${this.doc.id}`, { [field]: value });
      if (res.document) this.doc = { ...this.doc, title: res.document.title, category: res.document.category, slug: res.document.slug };
      docsVersion.set(docsVersion.get() + 1);
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async done() {
    await this.live?.flush();
    docsVersion.set(docsVersion.get() + 1);
    navigate(`/p/${this.project}/d/${this.doc?.slug ?? this.docSlug}`);
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const d = this.doc;
    const tool = (ico: string, label: string, run: () => void) =>
      html`<button type="button" title=${label} aria-label=${label} @mousedown=${(e: Event) => e.preventDefault()} @click=${run}>${icon(ico, 16)}</button>`;
    const state = this.status === "connected" ? t("editor.connected") : this.status === "connecting" ? t("editor.connecting") : t("editor.disconnected");
    return html`
      <header>
        <label class="field title">${t("doc.title")}<input .value=${d?.title ?? ""} @change=${(e: Event) => this.saveMeta("title", (e.target as HTMLInputElement).value)} /></label>
        <label class="field cat">${t("doc.category")}<input .value=${d?.category ?? ""} @change=${(e: Event) => this.saveMeta("category", (e.target as HTMLInputElement).value)} /></label>
        <button @click=${this.done}>${icon("Check", 15)} ${t("editor.done")}</button>
      </header>
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
        ${tool("Quote", t("editor.quote"), () => this.block("quote"))}
        ${tool("SquareCode", t("editor.codeblock"), () => this.block("code"))}
        <span class="sep"></span>
        ${tool("Image", t("editor.insert_file"), () => this.renderRoot.querySelector<HTMLInputElement>("input.pick")!.click())}
        <input class="pick" type="file" multiple hidden @change=${(e: Event) => { const i = e.target as HTMLInputElement; if (i.files) this.live?.insertFiles(Array.from(i.files)); i.value = ""; }}>
        <span class="state" aria-live="polite">
          ${this.peers.length ? html`<span class="avatars" title=${t("editor.peers", { names: this.peers.map((p) => p.name).join(", ") })}>
            ${this.peers.map((p) => html`<span style="background:${p.color}">${p.name.slice(0, 2).toUpperCase()}</span>`)}</span>` : ""}
          <span><span class="dot ${this.status}"></span> ${state}</span>
          <span>${this.saving ? t("editor.saving") : t("editor.saved")}</span>
        </span>
      </div>
      <div class="wrap prose">
        <div class="content" contenteditable="true" role="textbox" aria-multiline="true" aria-label=${d?.title ?? ""} spellcheck="true"></div>
        ${this.empty ? html`<div class="placeholder">${t("editor.placeholder")}</div>` : ""}
        <div class="cursors"></div>
      </div>
      <p class="hint">${t("doc.mention_hint")} ${t("editor.files_hint")}</p>
      ${d ? html`<phx-attachments style="margin-top:20px" .project=${this.project} .document=${d.id} .editable=${true}></phx-attachments>` : ""}`;
  }
}
customElements.define("phx-live-editor", PhxLiveEditor);
