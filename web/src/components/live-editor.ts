import { $createCodeNode } from "@lexical/code";
import { TOGGLE_LINK_COMMAND } from "@lexical/link";
import { INSERT_CHECK_LIST_COMMAND, INSERT_ORDERED_LIST_COMMAND, INSERT_UNORDERED_LIST_COMMAND } from "@lexical/list";
import { $createHeadingNode, $createQuoteNode, type HeadingTagType } from "@lexical/rich-text";
import { $setBlocksType } from "@lexical/selection";
import { $deleteTableColumnAtSelection, $deleteTableRowAtSelection, $insertTableColumnAtSelection, $insertTableRowAtSelection, $isTableSelection, INSERT_TABLE_COMMAND } from "@lexical/table";
import { $createHorizontalRuleNode } from "@lexical/extension";
import { $insertNodeToNearestRoot } from "@lexical/utils";
import { $createParagraphNode, $getRoot, $getNodeByKey, $getSelection, $isRangeSelection, $setSelection, COMMAND_PRIORITY_CRITICAL, FORMAT_TEXT_COMMAND, KEY_DOWN_COMMAND, REDO_COMMAND, UNDO_COMMAND, type BaseSelection } from "lexical";
import { $isFenceNode, InlineMathNode, TimeNode } from "../editor/blocks";
import {
  $applyColors, $currentColors, $insertCallout, $insertDivider, $insertFence, $insertInlineMath, $insertRequest, $insertTime, $removeSlash, $selectedCallout, $setBlock,
  runFormat, runList, runTable, SLASH_ITEMS, type SlashItem,
} from "../editor/commands";
import { CALLOUT_PRESETS, COLORS, type CalloutAttrs, type Color, type FenceKind, type TimeKind } from "../syntax";
import { calloutIcons } from "../icon";
import { LitElement, css, html, unsafeCSS } from "lit";
import { ApiError, api, type Doc, type DraftInfo } from "../api";
import { userColor } from "../collab";
import { createLiveEditor, type LiveEditor, type Peer, type Status } from "../editor/editor";
import { lossless } from "../editor/markdown";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser, docsVersion, navigate, wideReading } from "../state";
import { prose } from "./doc-view";
import { base } from "./shared";
import { fileURL, isImage, uploadFile } from "../files";
import type { PhxAttachments } from "./attachments";
import "./review-actions";
import "./attachments";
import { toast } from "./toasts";

interface FormField { name: string; label: string; type: string; value: string; options?: [string, string][] }
interface FormSpec { title: string; hint?: string; fields: FormField[]; resolve: (v?: Record<string, string>) => void }


/** Live editing of a Markdown page (Lexical + Yjs). */
export class PhxLiveEditor extends LitElement {
  static properties = { project: {}, docSlug: {}, doc: { state: true }, draft: { state: true }, source: { state: true }, status: { state: true }, saving: { state: true },
    peers: { state: true }, error: { state: true }, asking: { state: true }, inTable: { state: true },
    slash: { state: true }, pop: { state: true }, form: { state: true }, callout: { state: true }, colors: { state: true } };
  static styles = [base, unsafeCSS(prose), css`
    /* Embedded blocks (boards, embeds, formulas) must not inherit the editor's pre-wrap. */
    [data-lexical-decorator] { white-space: normal; }
    :host { display: block; max-width: var(--reading-width, 820px); margin-inline: auto; }
    :host([wide]) { max-width: none; }
    header { display: flex; gap: 12px; align-items: flex-end; flex-wrap: wrap; margin-bottom: 10px; }
    header .title { flex: 2; min-width: 220px; } header .cat { flex: 1; min-width: 160px; }
    .title input { font: 700 26px var(--font-display); padding: 6px 10px; }
    .bar { position: sticky; top: var(--header-h); z-index: 4; display: flex; gap: 2px; align-items: center; flex-wrap: wrap;
      background: var(--bg); padding: 6px 0; border-bottom: 1px solid var(--line); margin-bottom: 14px; }
    .bar button { background: none; border: 1px solid transparent; color: var(--fg); padding: 5px 7px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { .bar button:hover { background: var(--surface2); } }
    .bar .sep { width: 1px; height: 20px; background: var(--line); margin: 0 4px; }
    .bar .state { margin-left: auto; display: flex; gap: 10px; align-items: center; font-size: 13px; color: var(--muted); }
    .avatars { display: flex; } .avatars span { width: 24px; height: 24px; border-radius: 50%; display: grid; place-items: center; color: var(--on-color);
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
    /* Building blocks in the editor (ADR-0031) */
    .ed-callout { position: relative; display: block; padding-left: 40px; }
    .ed-callout::before { content: ""; position: absolute; left: 13px; top: 12px; width: 18px; height: 18px; background: var(--tc-gray);
      -webkit-mask: var(--ci) center / contain no-repeat; mask: var(--ci) center / contain no-repeat; }
    ${unsafeCSS(COLORS.map((c) => `.ed-callout.cb-${c}::before { background: var(--tc-${c}); } .ed-callout.icx-${c}::before { background: var(--tc-${c}) !important; }`).join("\n"))}
    .ed-callout > .ed-p:last-child { margin-bottom: 0; }
    .ed-fence { position: relative; margin: .6em 0; border-radius: 10px; outline: 1px dashed transparent; }
    @media (hover: hover) and (pointer: fine) { .ed-fence:hover { outline-color: var(--line); } .ed-fence:hover .ed-fence-edit { opacity: 1; } }
    .ed-fence-edit { position: absolute; top: 6px; right: 6px; opacity: 0; font: 600 11px var(--font-mono); padding: 3px 7px; background: var(--surface); border: 1px solid var(--line); border-radius: 6px; color: var(--fg); }
    .ed-fence-edit:focus-visible { opacity: 1; }
    @media (hover: none) { .ed-fence-edit { opacity: 1; } }
    .ed-imath, .ed-time { cursor: pointer; }
    .slash { position: absolute; z-index: 20; width: 260px; max-height: 320px; overflow-y: auto; padding: 4px; background: var(--surface); border: 1px solid var(--line);
      border-radius: 10px; box-shadow: 0 14px 34px rgb(0 0 0 / .18); transform-origin: top left;
      transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out); @starting-style { opacity: 0; transform: scale(.97); } }
    .slash .grp { font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); padding: 6px 8px 2px; }
    .slash .it { display: flex; gap: 8px; align-items: center; padding: 6px 8px; border-radius: 6px; cursor: pointer; font-size: 13.5px; }
    .slash .it.act { background: var(--surface2); }
    .popwrap { position: relative; display: inline-flex; }
    .pop { position: absolute; top: calc(100% + 6px); left: 0; z-index: 20; width: 248px; padding: 10px; background: var(--surface); border: 1px solid var(--line);
      border-radius: 10px; box-shadow: 0 14px 34px rgb(0 0 0 / .18); display: grid; gap: 6px; }
    .pop.wide { width: 320px; }
    .pop .lbl { font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); }
    .pop .sws { display: flex; flex-wrap: wrap; gap: 4px; }
    .pop .sw { width: 24px; height: 24px; padding: 0; border-radius: 6px; border: 1px solid var(--line); font: 600 12px var(--font-body); display: grid; place-items: center; background: var(--surface); }
    .pop .sw.on, .pop .ib.on, .pop .chipb.on { outline: 2px solid var(--accent); outline-offset: 1px; }
    .pop .ib { width: 28px; height: 28px; padding: 0; display: grid; place-items: center; background: none; border: 1px solid transparent; border-radius: 6px; color: var(--fg); }
    .pop .chipb { font-size: 12px; padding: 3px 8px; border-radius: 999px; background: var(--surface2); border: none; color: var(--fg); }
    ${unsafeCSS(COLORS.map((c) => `.sw.fg.tc-${c} { color: var(--tc-${c}); } .sw.bg.tb-${c} { background: var(--tb-${c}); }`).join("\n"))}
    dialog.form form { display: grid; gap: 10px; min-width: min(420px, 80vw); }
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
  /** The open "/" menu: typed query, position inside .wrap, active item. */
  slash?: { query: string; x: number; y: number; active: number };
  /** Open toolbar popover. */
  pop: "" | "color" | "callout" = "";
  /** Open input dialog (formulas, embeds, dates, …). */
  form?: FormSpec;
  /** Attributes of the callout around the cursor. */
  callout?: CalloutAttrs;
  colors: { color?: Color; bg?: Color } = {};
  status: Status = "connecting";
  saving = false;
  peers: Peer[] = [];
  error = "";
  private live?: LiveEditor;
  private loaded = "";
  private empty = true;
  private ended = false;
  /** Set while publishing or cancelling from here. */
  private leaving = false;
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
      url: `${proto}//${location.host}/rt/doc/${this.project}`, docId: this.doc.id, project: this.project,
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
        // Publishing or cancelling here also resets the room; do not reopen a draft.
        if (this.ended || this.leaving) return;
        toast(t("editor.reset", { by }));
        this.loaded = "";
        this.requestUpdate();
      },
      onEnded: (reason, by) => this.onEnded(reason, by),
    });
    this.setupBlocks(element);
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

  // ---------- Building blocks (ADR-0031) ----------

  private setupBlocks(element: HTMLElement) {
    const ed = this.live!.editor;
    ed.registerUpdateListener(({ editorState }) => {
      editorState.read(() => {
        const sel = $getSelection();
        const c = $selectedCallout();
        const next = c ? c.attrs() : undefined;
        if (JSON.stringify(next) !== JSON.stringify(this.callout)) this.callout = next;
        const cols = $currentColors();
        if (cols.color !== this.colors.color || cols.bg !== this.colors.bg) this.colors = cols;
        // "/" at the start of a line or after a space opens the menu.
        if (!$isRangeSelection(sel) || !sel.isCollapsed()) return this.closeSlash();
        const before = sel.anchor.getNode().getTextContent().slice(0, sel.anchor.offset);
        // The query may contain spaces ("formel im"), the menu closes once nothing matches.
        const m = /(?:^|\s)\/((?:[\p{L}\d-][\p{L}\d -]{0,23})?)$/u.exec(before);
        if (!m || (m[1].includes(" ") && !this.slashItems(m[1]).length)) return this.closeSlash();
        const rect = this.caretRect();
        const wrap = this.renderRoot.querySelector<HTMLElement>(".wrap")!.getBoundingClientRect();
        if (!rect) return;
        const prev = this.slash;
        this.slash = { query: m[1], x: Math.max(0, rect.left - wrap.left), y: rect.bottom - wrap.top + 6, active: prev && prev.query === m[1] ? prev.active : 0 };
      });
    });
    ed.registerCommand(KEY_DOWN_COMMAND, (e) => {
      if (!this.slash) return false;
      const items = this.slashItems();
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        if (items.length) this.slash = { ...this.slash, active: (this.slash.active + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length };
        this.updateComplete.then(() => this.renderRoot.querySelector(".slash .act")?.scrollIntoView({ block: "nearest" }));
        return true;
      }
      if ((e.key === "Enter" || e.key === "Tab") && items.length) {
        e.preventDefault();
        // Run after this key event: commands inside it would share Lexical's running update.
        const item = items[this.slash.active] ?? items[0];
        window.setTimeout(() => void this.runSlash(item));
        return true;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        this.slash = undefined;
        return true;
      }
      return false;
    }, COMMAND_PRIORITY_CRITICAL);
    // Clicks on blocks edit them: formulas, embeds, boards, dates.
    element.addEventListener("click", (e) => {
      const target = e.target as HTMLElement;
      const fence = target.closest<HTMLElement>(".ed-fence-edit");
      if (fence?.dataset.key) return void this.editFence(fence.dataset.key);
      const inline = target.closest<HTMLElement>(".ed-imath, .ed-time");
      if (inline) {
        const key = ed.getEditorState().read(() => {
          const sel = $getSelection();
          return sel?.getNodes().find((n) => n instanceof InlineMathNode || n instanceof TimeNode)?.getKey();
        });
        if (key) void this.editInline(key);
      }
    });
  }

  private closeSlash() {
    if (this.slash) this.slash = undefined;
  }

  private caretRect(): DOMRect | undefined {
    const root = this.renderRoot as ShadowRoot & { getSelection?: () => Selection | null };
    const sel = root.getSelection?.() ?? window.getSelection();
    if (!sel || !sel.rangeCount) return undefined;
    const r = sel.getRangeAt(0).getBoundingClientRect();
    if (r.width || r.height) return r;
    const node = sel.anchorNode instanceof Element ? sel.anchorNode : sel.anchorNode?.parentElement;
    return node?.getBoundingClientRect();
  }

  private slashItems(query = this.slash?.query ?? ""): SlashItem[] {
    const q = query.toLowerCase().trim();
    if (!q) return SLASH_ITEMS;
    const words = q.split(/\s+/);
    return SLASH_ITEMS.filter((i) => { const hay = `${i.id} ${i.words} ${t(`slash.${i.id}` as any)}`.toLowerCase(); return words.every((w) => hay.includes(w)); });
  }

  /** Remembers the selection so dialogs can insert where the cursor was. */
  private keep(): BaseSelection | null {
    return this.live!.editor.getEditorState().read(() => $getSelection()?.clone() ?? null);
  }

  private at(saved: BaseSelection | null, fn: () => void) {
    this.live!.markInput();
    this.live!.editor.update(() => {
      if (saved) {
        try {
          $setSelection(saved.clone());
          saved.getNodes();
        } catch {
          $setSelection(null);
        }
      }
      if (!$getSelection()) $getRoot().selectEnd();
      fn();
    });
  }

  private async runSlash(item: SlashItem) {
    const q = this.slash?.query ?? "";
    this.slash = undefined;
    const ed = this.live!.editor;
    this.live!.markInput();
    // Commit at once, so the remembered selection already sees the removal.
    ed.update(() => $removeSlash(q), { discrete: true });
    const [kind, arg] = item.run.split(":");
    if (kind === "block") ed.update(() => $setBlock(arg));
    else if (kind === "list") runList(ed, arg);
    else if (kind === "format") runFormat(ed, arg as any);
    else if (kind === "table") runTable(ed);
    else if (kind === "divider") ed.update(() => $insertDivider());
    else if (kind === "callout") ed.update(() => $insertCallout({ kind: "note", ...CALLOUT_PRESETS.note }));
    else if (kind === "request") ed.update(() => $insertRequest());
    else if (kind === "color") this.pop = "color";
    else if (kind === "file") this.renderRoot.querySelector<HTMLInputElement>("input.pick")!.click();
    else if (kind === "fence") {
      const saved = this.keep();
      const src = await this.askFence(arg as FenceKind, "");
      if (src) this.at(saved, () => $insertFence(arg as FenceKind, src));
    } else if (kind === "imath") {
      const saved = this.keep();
      const v = await this.askForm({ title: t("slash.imath"), fields: [{ name: "tex", label: t("block.tex"), type: "text", value: "" }] });
      if (v?.tex.trim()) this.at(saved, () => $insertInlineMath(v.tex.trim()));
    } else if (kind === "time") {
      const saved = this.keep();
      const v = await this.askTime(arg as TimeKind, "");
      if (v) this.at(saved, () => $insertTime(v.datetime, arg as TimeKind, v.text));
    }
    ed.focus();
  }

  private async askFence(kind: FenceKind, current: string): Promise<string | undefined> {
    if (kind === "board") {
      const boards = (await api<Doc[]>("GET", `/projects/${this.project}/documents?type=board`).catch(() => [])) ?? [];
      const NEW = "__new__";
      const v = await this.askForm({ title: t("slash.board"), hint: t("block.board_new_hint"), fields: [
        { name: "board", label: t("block.board_pick"), type: "select", value: current || NEW,
          options: [[NEW, t("block.board_new")] as [string, string], ...boards.map((b) => [b.slug, b.title] as [string, string])] },
        { name: "title", label: t("block.board_new_title"), type: "text", value: this.doc ? t("block.board_default_title", { page: this.doc.title }) : "" }] });
      if (!v) return undefined;
      if (v.board !== NEW) return v.board;
      // A board of its own: it belongs to this page and stays out of the sidebar.
      try {
        const res = await api<{ document?: Doc }>("POST", `/projects/${this.project}/documents`, {
          type: "board", title: v.title?.trim() || t("doc.type.board"), parent: this.doc?.id,
          board: { columns: [{ id: "todo", name: t("board.col_todo") }, { id: "doing", name: t("board.col_doing") }, { id: "done", name: t("board.col_done"), done: true }] },
        });
        return res.document?.slug;
      } catch (e) {
        toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
        return undefined;
      }
    }
    if (kind === "include") {
      const pages = ((await api<Doc[]>("GET", `/projects/${this.project}/documents?type=markdown`).catch(() => [])) ?? []).filter((d) => d.id !== this.doc?.id);
      if (!pages.length) {
        toast(t("block.include_none"), "error");
        return undefined;
      }
      const v = await this.askForm({ title: t("slash.include"), hint: t("block.include_hint"), fields: [
        { name: "page", label: t("block.include_pick"), type: "select", value: current || pages[0].slug, options: pages.map((d) => [d.slug, d.title] as [string, string]) }] });
      return v?.page || undefined;
    }
    if (kind === "query") {
      const saved = (await api<{ slug: string; name: string }[]>("GET", `/projects/${this.project}/queries`).catch(() => [])) ?? [];
      if (!saved.length) {
        toast(t("block.query_none"), "error");
        return undefined;
      }
      const v = await this.askForm({ title: t("slash.query"), hint: t("block.query_hint"), fields: [
        { name: "q", label: t("block.query_pick"), type: "select", value: current || saved[0].slug, options: saved.map((q) => [q.slug, q.name] as [string, string]) }] });
      return v?.q || undefined;
    }
    if (kind === "mermaid") {
      const v = await this.askForm({ title: t("slash.mermaid"), hint: t("block.mermaid_hint"),
        fields: [{ name: "v", label: t("block.mermaid_source"), type: "textarea", value: current || "flowchart LR\n  Internet --> Router --> Switch\n  Switch --> NAS" }] });
      return v?.v.trim() || undefined;
    }
    if (kind === "dbmodel" || kind === "status" || kind === "entity") {
      const preset = kind === "dbmodel" ? "CREATE TABLE customers (\n  id INTEGER PRIMARY KEY,\n  name TEXT NOT NULL\n);\n\nCREATE TABLE orders (\n  id INTEGER PRIMARY KEY,\n  customer_id INTEGER REFERENCES customers(id),\n  total NUMERIC(10,2)\n);"
        : kind === "status" ? "@router\n@nas\nchecks" : "";
      const v = await this.askForm({ title: t(`slash.${kind}` as any), hint: t(`block.${kind}_hint` as any),
        fields: [{ name: "v", label: t(`block.${kind}_source` as any), type: kind === "entity" ? "text" : "textarea", value: current || preset }] });
      return v?.v.trim() || undefined;
    }
    const label = kind === "math" ? t("block.tex") : kind === "iframe" ? t("block.url_iframe") : t("block.url_embed");
    const v = await this.askForm({ title: t(`slash.${kind}` as any), hint: kind === "iframe" ? t("block.iframe_hint") : kind === "embed" ? t("block.embed_hint") : t("block.math_hint"),
      fields: [{ name: "v", label, type: kind === "math" ? "textarea" : "url", value: current }] });
    const out = v?.v.trim();
    if (out && kind !== "math" && !/^https:\/\/\S+$/.test(out)) {
      toast(t("block.https_only"), "error");
      return undefined;
    }
    return out || undefined;
  }

  private async askTime(kind: TimeKind, current: string): Promise<{ datetime: string; text: string } | undefined> {
    const type = kind === "date" ? "date" : kind === "time" ? "time" : "datetime-local";
    const now = new Date();
    const pad = (n: number) => String(n).padStart(2, "0");
    const today = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
    const def = kind === "date" ? today : kind === "time" ? `${today}T${pad(now.getHours())}:${pad(now.getMinutes())}` : `${today}T${pad(now.getHours())}:00`;
    const v = await this.askForm({ title: t(`slash.${kind}` as any), fields: [{ name: "d", label: t(`slash.${kind}` as any), type, value: current ? (kind === "time" ? current.slice(11, 16) : current) : (kind === "time" ? def.slice(11) : def) }] });
    if (!v?.d) return undefined;
    const datetime = kind === "time" ? `${today}T${v.d}` : v.d;
    const loc = navigator.language || "de-DE";
    const d = new Date(kind === "date" ? `${datetime}T00:00:00` : datetime);
    const text = kind === "time" ? d.toLocaleTimeString(loc, { hour: "2-digit", minute: "2-digit" }) : kind === "date" ? d.toLocaleDateString(loc) : d.toLocaleString(loc, { dateStyle: "medium", timeStyle: "short" });
    return { datetime, text };
  }

  private async editFence(key: string) {
    const ed = this.live!.editor;
    const info = ed.getEditorState().read(() => {
      const n = $getNodeByKey(key);
      return $isFenceNode(n) ? { kind: n.getKind(), source: n.getSource() } : undefined;
    });
    if (!info) return;
    const src = await this.askFence(info.kind, info.source);
    if (src === undefined) return;
    this.live!.markInput();
    ed.update(() => {
      const n = $getNodeByKey(key);
      if ($isFenceNode(n)) n.setSource(src);
    });
  }

  private async editInline(key: string) {
    const ed = this.live!.editor;
    const info = ed.getEditorState().read(() => {
      const n = $getNodeByKey(key);
      if (n instanceof InlineMathNode) return { math: n.getTex() };
      if (n instanceof TimeNode) return { time: n.__datetime, kind: n.__kind };
      return undefined;
    });
    if (!info) return;
    if ("math" in info) {
      const v = await this.askForm({ title: t("slash.imath"), fields: [{ name: "tex", label: t("block.tex"), type: "text", value: info.math ?? "" }] });
      if (v?.tex.trim()) {
        this.live!.markInput();
        ed.update(() => ($getNodeByKey(key) as InlineMathNode | null)?.setTex(v.tex.trim()));
      }
      return;
    }
    const v = await this.askTime(info.kind!, info.time!);
    if (!v) return;
    this.live!.markInput();
    ed.update(() => {
      const n = $getNodeByKey(key);
      if (n instanceof TimeNode) {
        const w = n.getWritable();
        w.__datetime = v.datetime;
        w.__text = v.text;
      }
    });
  }

  /** Opens the input dialog and resolves with the values (undefined = cancelled). */
  private askForm(spec: Omit<FormSpec, "resolve">): Promise<Record<string, string> | undefined> {
    return new Promise((resolve) => {
      this.form = { ...spec, resolve };
      this.updateComplete.then(() => {
        const d = this.renderRoot.querySelector<HTMLDialogElement>("dialog.form");
        d?.showModal();
        d?.querySelector<HTMLElement>("input, textarea, select")?.focus();
      });
    });
  }

  private closeForm(values?: Record<string, string>) {
    const f = this.form;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog.form")?.close();
    this.form = undefined;
    f?.resolve(values);
  }

  private formDialog() {
    const f = this.form;
    if (!f) return "";
    return html`<dialog class="form" aria-labelledby="form-title" @cancel=${(e: Event) => { e.preventDefault(); this.closeForm(); }}>
      <form @submit=${(e: SubmitEvent) => { e.preventDefault(); const d = new FormData(e.target as HTMLFormElement); this.closeForm(Object.fromEntries(f.fields.map((x) => [x.name, String(d.get(x.name) ?? "")]))); }}>
        <h2 id="form-title">${f.title}</h2>
        ${f.hint ? html`<p class="muted" style="margin:0 0 4px;font-size:13px">${f.hint}</p>` : ""}
        ${f.fields.map((x) => html`<label class="field">${x.label}
          ${x.type === "textarea" ? html`<textarea name=${x.name} .value=${x.value} rows="4" style="font-family:var(--font-mono)"></textarea>`
            : x.type === "select" ? html`<select name=${x.name}>${(x.options ?? []).map(([v, l]) => html`<option value=${v} ?selected=${v === x.value}>${l}</option>`)}</select>`
            : html`<input name=${x.name} type=${x.type} .value=${x.value} required>`}</label>`)}
        <div class="row"><button type="button" class="ghost" @click=${() => this.closeForm()}>${t("doc.cancel")}</button><button>${t("block.apply")}</button></div>
      </form>
    </dialog>`;
  }

  private slashMenu() {
    const s = this.slash;
    if (!s) return "";
    const items = this.slashItems();
    let group = "";
    return html`<div class="slash" role="listbox" aria-label=${t("slash.title")} style="left:${s.x}px;top:${s.y}px" @mousedown=${(e: Event) => e.preventDefault()}>
      ${items.length ? items.map((it, i) => {
        const head = it.group !== group ? ((group = it.group), html`<div class="grp">${t(`slash.group.${it.group}` as any)}</div>`) : "";
        return html`${head}<div role="option" aria-selected=${i === s.active ? "true" : "false"} class=${i === s.active ? "it act" : "it"}
          @pointerenter=${() => (this.slash = { ...s, active: i })} @click=${() => this.runSlash(it)}>${icon(it.icon, 15)} ${t(`slash.${it.id}` as any)}</div>`;
      }) : html`<div class="grp">${t("slash.none")}</div>`}
    </div>`;
  }

  private colorPopover() {
    const sw = (c: Color | null, bg: boolean) => html`<button type="button" class=${["sw", bg ? "bg" : "fg", c ? `${bg ? "tb" : "tc"}-${c}` : "none",
      (bg ? this.colors.bg : this.colors.color) === (c ?? undefined) ? "on" : ""].join(" ")}
      title=${c ? t(`color.${c}` as any) : t("color.none")} aria-label=${`${t(bg ? "color.bg" : "color.text")}: ${c ? t(`color.${c}` as any) : t("color.none")}`}
      @mousedown=${(e: Event) => e.preventDefault()} @click=${() => { this.live?.markInput(); this.live?.editor.update(() => $applyColors(bg ? undefined : c, bg ? c : undefined)); this.pop = ""; }}>A</button>`;
    return html`<div class="pop" role="dialog" aria-label=${t("color.title")} @mousedown=${(e: Event) => e.preventDefault()}>
      <div class="lbl">${t("color.text")}</div><div class="sws">${sw(null, false)}${COLORS.map((c) => sw(c, false))}</div>
      <div class="lbl">${t("color.bg")}</div><div class="sws">${sw(null, true)}${COLORS.map((c) => sw(c, true))}</div>
    </div>`;
  }

  private calloutPopover() {
    const a = this.callout;
    if (!a) return "";
    const set = (p: Partial<CalloutAttrs>) => { this.live?.markInput(); this.live?.editor.update(() => $selectedCallout()?.setAttrs(p)); };
    return html`<div class="pop wide" role="dialog" aria-label=${t("callout.title")} @mousedown=${(e: Event) => e.preventDefault()}>
      <div class="lbl">${t("callout.kind")}</div>
      <div class="sws">${Object.keys(CALLOUT_PRESETS).map((k) => html`<button type="button" class=${a.kind === k ? "chipb on" : "chipb"}
        @click=${() => set({ kind: k, ...(k === "callout" ? {} : CALLOUT_PRESETS[k]) })}>${t(`callout.${k}` as any)}</button>`)}</div>
      <div class="lbl">${t("callout.icon")}</div>
      <div class="sws icons">${calloutIcons.map((n) => html`<button type="button" class=${a.icon === n ? "ib on" : "ib"} title=${n} aria-label=${n} @click=${() => set({ icon: n })}>${icon(n, 15)}</button>`)}</div>
      <div class="lbl">${t("callout.color")}</div>
      <div class="sws">${COLORS.map((c) => html`<button type="button" class=${`sw bg tb-${c} ${a.color === c ? "on" : ""}`} title=${t(`color.${c}` as any)} aria-label=${t(`color.${c}` as any)} @click=${() => set({ color: c })}></button>`)}</div>
      <div class="lbl">${t("callout.icon_color")}</div>
      <div class="sws"><button type="button" class=${!a.iconColor ? "sw none on" : "sw none"} title=${t("callout.icon_color_same")} aria-label=${t("callout.icon_color_same")} @click=${() => set({ iconColor: undefined })}>–</button>
        ${COLORS.map((c) => html`<button type="button" class=${`sw fg tc-${c} ${a.iconColor === c ? "on" : ""}`} title=${t(`color.${c}` as any)} aria-label=${t(`color.${c}` as any)} @click=${() => set({ iconColor: c })}>●</button>`)}</div>
    </div>`;
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
    this.leaving = true;
    try {
      await this.flushAll();
      const res = await api<{ document?: Doc }>("POST", `/projects/${this.project}/documents/${this.doc!.id}/draft/publish`);
      this.ended = true;
      toast(t("draft.published_toast"));
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${res.document?.slug ?? this.doc?.slug ?? this.docSlug}`);
    } catch (e) {
      this.leaving = false;
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async cancel() {
    this.asking = "";
    this.leaving = true;
    try {
      this.ended = true;
      await api("DELETE", `/projects/${this.project}/documents/${this.doc!.id}/draft`);
      toast(t("draft.cancelled_toast"));
      docsVersion.set(docsVersion.get() + 1);
      navigate(`/p/${this.project}/d/${this.doc?.slug ?? this.docSlug}`);
    } catch (e) {
      this.ended = false;
      this.leaving = false;
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
    this.toggleAttribute("wide", wideReading.get());
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
          <phx-review-actions .project=${this.project} .doc=${this.doc?.id ?? ""} .draft=${dr} @draft-changed=${(e: CustomEvent) => (this.draft = e.detail)}></phx-review-actions>
          ${dr?.canPublish ?? dr?.canEnd ? html`<button @click=${() => this.ask("publish")}>${icon("Send", 15)} ${dr?.reviewBy ? t("review.approve") : t("draft.publish")}</button>` : ""}
        </div>
      </header>
      ${dr?.approval && dr.reviewNote && !dr.reviewBy ? html`<div class="banner warn" role="note">${icon("MessageSquareWarning", 16)} <span>${t("review.changes_requested", { note: dr.reviewNote })}</span></div>` : ""}
      ${dr?.approval && !dr.canPublish && !dr.reviewBy ? html`<div class="banner" role="note">${icon("ShieldCheck", 16)} <span>${t("review.hint")}</span></div>` : ""}
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
        <span class="popwrap">${tool("Palette", t("color.title"), () => (this.pop = this.pop === "color" ? "" : "color"))}${this.pop === "color" ? this.colorPopover() : ""}</span>
        ${this.callout ? html`<span class="popwrap">${tool("MessageSquareWarning", t("callout.title"), () => (this.pop = this.pop === "callout" ? "" : "callout"))}${this.pop === "callout" ? this.calloutPopover() : ""}</span>` : ""}
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
        ${this.slashMenu()}
      </div>`}
      <p class="hint">${t("draft.hint")} ${t("editor.slash_hint")} ${t("doc.mention_hint")} ${t("editor.files_hint")}</p>
      ${d ? html`<phx-attachments style="margin-top:20px" .project=${this.project} .document=${d.id} .editable=${true}></phx-attachments>` : ""}
      ${this.dialog()}
      ${this.formDialog()}`;
  }
}
customElements.define("phx-live-editor", PhxLiveEditor);
