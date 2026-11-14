// Live Markdown editor: Lexical (no React) bound to Yjs over y-websocket.
// Loaded on demand (code splitting) when a page is opened for editing.

import { $toggleLink, TOGGLE_LINK_COMMAND } from "@lexical/link";
import { registerCheckList, registerList } from "@lexical/list";
import { $convertFromMarkdownString, $convertToMarkdownString, registerMarkdownShortcuts } from "@lexical/markdown";
import { registerRichText } from "@lexical/rich-text";
import { registerTablePlugin, registerTableSelectionObserver } from "@lexical/table";
import {
  createBinding, createUndoManager, initLocalState, setLocalStateFocus, syncCursorPositions,
  syncLexicalUpdateToYjs, syncYjsChangesToLexical, type Provider,
} from "@lexical/yjs";
import {
  $createParagraphNode, $createTextNode, $getRoot, $getSelection, $insertNodes, BLUR_COMMAND, COMMAND_PRIORITY_EDITOR, COMMAND_PRIORITY_HIGH, createEditor, DROP_COMMAND, FOCUS_COMMAND,
  HISTORY_MERGE_TAG, type LexicalEditor, mergeRegister, PASTE_COMMAND, REDO_COMMAND, SKIP_COLLAB_TAG, UNDO_COMMAND,
} from "lexical";
import { $createLinkNode } from "@lexical/link";
import { $insertNodeToNearestRoot } from "@lexical/utils";
import { $createImageNode } from "./image";
import { NODES, TRANSFORMERS } from "./markdown";
import { MSG_CONTROL, MSG_SNAPSHOT } from "../collab";
import * as decoding from "lib0/decoding";
import * as encoding from "lib0/encoding";
import { WebsocketProvider } from "y-websocket";
import * as Y from "yjs";

export type Status = "connecting" | "connected" | "disconnected";

export interface Uploaded { url: string; name: string; image: boolean }
export interface Peer { name: string; color: string }

export interface LiveEditorOptions {
  element: HTMLElement;
  /** Container for the carets of other editors (positioned like the editor). */
  cursors: HTMLElement;
  /** WebSocket base, e.g. wss://host/rt/doc/<project>; the document id is appended. */
  url: string;
  docId: string;
  /** Project slug, for embedded boards. */
  project?: string;
  /** Draft Markdown, used if this editor fills an empty live document. */
  markdown: string;
  user: Peer;
  editable: boolean;
  onSave: (markdown: string) => Promise<void>;
  onStatus?: (s: Status) => void;
  onSaving?: (saving: boolean) => void;
  onPeers?: (peers: Peer[]) => void;
  onReset?: (by: string) => void;
  /** The draft was published, cancelled or the page deleted (ADR-0027). */
  onEnded?: (reason: string, by: string) => void;
  /** Uploads a pasted or dropped file; images are inserted, other files linked. */
  upload?: (file: File) => Promise<Uploaded>;
  onUploadError?: (err: unknown) => void;
}

export interface LiveEditor {
  editor: LexicalEditor;
  /** Call before changing content from outside the text (toolbar). */
  markInput(): void;
  /** Saves pending changes now. */
  flush(): Promise<void>;
  /** Uploads files and inserts them at the cursor. */
  insertFiles(files: File[]): void;
  destroy(): Promise<void>;
}

const theme = {
  paragraph: "ed-p",
  quote: "ed-quote",
  heading: { h1: "ed-h1", h2: "ed-h2", h3: "ed-h3", h4: "ed-h4", h5: "ed-h5", h6: "ed-h6" },
  list: { ul: "ed-ul", ol: "ed-ol", listitem: "ed-li", nested: { listitem: "ed-nested" }, listitemChecked: "ed-checked", listitemUnchecked: "ed-unchecked" },
  code: "ed-code",
  link: "ed-link",
  image: "ed-img",
  table: "ed-table",
  tableCell: "ed-td",
  tableCellHeader: "ed-th",
  tableSelection: "ed-table-sel",
  hr: "ed-hr",
  text: { bold: "ed-bold", italic: "ed-italic", code: "ed-tcode", strikethrough: "ed-strike", underline: "ed-underline" },
};

const SAVE_DELAY = 1500;

export function markdownOf(editor: LexicalEditor): string {
  return editor.getEditorState().read(() => $convertToMarkdownString(TRANSFORMERS));
}

export function createLiveEditor(o: LiveEditorOptions): LiveEditor {
  const editor = createEditor({
    namespace: "phoedox",
    nodes: NODES,
    // The project lets embedded boards load (FenceNode).
    theme: { ...theme, project: o.project ?? "" },
    editable: o.editable,
    onError: (e) => console.error(e),
  });
  editor.setRootElement(o.element);

  const doc = new Y.Doc();
  const docMap = new Map([[o.docId, doc]]);
  const provider = new WebsocketProvider(o.url, o.docId, doc, { connect: false, disableBc: true });
  const binding = createBinding(editor, provider as unknown as Provider, o.docId, doc, docMap);
  binding.cursorsContainer = o.cursors;
  const shared = binding.root.getSharedType();
  const undo = createUndoManager(binding, shared);

  // Phoedox control messages from the server.
  let shouldBootstrap = false;
  provider.messageHandlers[MSG_CONTROL] = (_enc, decoder) => {
    const ctl = JSON.parse(decoding.readVarString(decoder)) as { type: string; upto?: number; by?: string; reason?: string };
    if (ctl.type === "bootstrap") shouldBootstrap = true;
    if (ctl.type === "ended") {
      // Nothing left to save: the draft is gone.
      dirty = false;
      window.clearTimeout(timer);
      o.onEnded?.(ctl.reason ?? "", ctl.by ?? "");
    }
    if (ctl.type === "reset") o.onReset?.(ctl.by ?? "");
    if (ctl.type === "compact" && ctl.upto) {
      const enc = encoding.createEncoder();
      encoding.writeVarUint(enc, MSG_SNAPSHOT);
      encoding.writeVarUint(enc, ctl.upto);
      encoding.writeVarUint8Array(enc, Y.encodeStateAsUpdate(doc));
      provider.ws?.send(encoding.toUint8Array(enc));
    }
  };

  // Saving: only after local edits, debounced.
  let dirty = false;
  let timer: number | undefined;
  let saving: Promise<void> | undefined;
  const save = async () => {
    window.clearTimeout(timer);
    if (!dirty || !o.editable) return;
    dirty = false;
    o.onSaving?.(true);
    saving = o.onSave(markdownOf(editor)).catch(() => {
      dirty = true; // try again later
      timer = window.setTimeout(save, 5000);
    }).finally(() => o.onSaving?.(false));
    await saving;
  };
  const schedule = () => {
    dirty = true;
    window.clearTimeout(timer);
    timer = window.setTimeout(save, SAVE_DELAY);
  };

  // Only edits by this person are saved here. Lexical also writes to Yjs
  // after remote changes (it merges text nodes), so a Yjs update from this
  // editor counts only shortly after real input (typing, paste, toolbar).
  let bootstrapping = false;
  let lastInput = 0;
  const markInput = () => { lastInput = Date.now(); };
  const onDocUpdate = (_update: Uint8Array, origin: unknown) => {
    const mine = origin === binding || origin instanceof Y.UndoManager;
    if (mine && !bootstrapping && Date.now() - lastInput < 1000) schedule();
  };
  doc.on("update", onDocUpdate);
  const inputEvents = ["beforeinput", "paste", "cut", "drop"] as const;
  for (const ev of inputEvents) o.element.addEventListener(ev, markInput, true);

  const onYjs = (events: Y.YEvent<any>[], tx: Y.Transaction) => {
    if (tx.origin !== binding) syncYjsChangesToLexical(binding, provider as unknown as Provider, events, tx.origin instanceof Y.UndoManager);
  };
  shared.observeDeep(onYjs);

  const emitPeers = () => {
    const peers: Peer[] = [];
    provider.awareness.getStates().forEach((s, id) => {
      if (id !== doc.clientID && s?.name) peers.push({ name: s.name, color: s.color });
    });
    o.onPeers?.(peers);
  };
  const onAwareness = () => {
    syncCursorPositions(binding, provider as unknown as Provider);
    emitPeers();
  };
  provider.awareness.on("update", onAwareness);

  const onSync = (synced: boolean) => {
    if (!synced || !shouldBootstrap || !o.editable) return;
    shouldBootstrap = false;
    if (!binding.root.isEmpty() || binding.root._xmlText._length !== 0) return;
    binding.isBootstrapping = true;
    bootstrapping = true;
    editor.update(() => {
      if ($getRoot().getTextContentSize() === 0) $convertFromMarkdownString(o.markdown, TRANSFORMERS);
    }, { tag: HISTORY_MERGE_TAG, onUpdate: () => { binding.isBootstrapping = false; bootstrapping = false; } });
  };
  provider.on("sync", onSync);
  provider.on("status", (e: { status: Status }) => o.onStatus?.(e.status));

  // Files pasted or dropped into the text are uploaded and inserted.
  const insertFiles = (files: File[]) => {
    if (!o.upload || !o.editable) return;
    for (const file of files) {
      o.upload(file).then((u) => {
        markInput();
        editor.update(() => {
          if (!$getSelection()) $getRoot().selectEnd();
          // Images get their own paragraph; other files become a link in the text.
          if (u.image) $insertNodeToNearestRoot($createParagraphNode().append($createImageNode(u.url, u.name.replace(/\.[^.]+$/, ""))));
          else $insertNodes([$createLinkNode(u.url).append($createTextNode(u.name))]);
        });
      }, (err) => o.onUploadError?.(err));
    }
  };
  const filesOf = (dt: DataTransfer | null) => Array.from(dt?.files ?? []);

  const unregister = mergeRegister(
    editor.registerCommand(PASTE_COMMAND, (e) => {
      const files = filesOf((e as ClipboardEvent).clipboardData ?? null);
      if (!files.length || !o.upload) return false;
      e.preventDefault();
      insertFiles(files);
      return true;
    }, COMMAND_PRIORITY_HIGH),
    editor.registerCommand(DROP_COMMAND, (e) => {
      const files = filesOf(e.dataTransfer);
      if (!files.length || !o.upload) return false;
      e.preventDefault();
      insertFiles(files);
      return true;
    }, COMMAND_PRIORITY_HIGH),
    registerRichText(editor),
    registerList(editor),
    registerCheckList(editor),
    registerTablePlugin(editor),
    registerTableSelectionObserver(editor, true),
    registerMarkdownShortcuts(editor, TRANSFORMERS),
    editor.registerCommand(TOGGLE_LINK_COMMAND, (payload) => {
      $toggleLink(typeof payload === "string" || payload === null ? payload : payload.url);
      return true;
    }, COMMAND_PRIORITY_EDITOR),
    editor.registerCommand(UNDO_COMMAND, () => { markInput(); undo.undo(); return true; }, COMMAND_PRIORITY_EDITOR),
    editor.registerCommand(REDO_COMMAND, () => { markInput(); undo.redo(); return true; }, COMMAND_PRIORITY_EDITOR),
    editor.registerCommand(FOCUS_COMMAND, () => { setLocalStateFocus(provider as unknown as Provider, o.user.name, o.user.color, true, {}); return false; }, COMMAND_PRIORITY_EDITOR),
    editor.registerCommand(BLUR_COMMAND, () => { setLocalStateFocus(provider as unknown as Provider, o.user.name, o.user.color, false, {}); return false; }, COMMAND_PRIORITY_EDITOR),
    editor.registerUpdateListener(({ prevEditorState, editorState, dirtyLeaves, dirtyElements, normalizedNodes, tags }) => {
      // Lexical → Yjs (the Yjs origin is the binding).
      if (!tags.has(SKIP_COLLAB_TAG)) {
        syncLexicalUpdateToYjs(binding, provider as unknown as Provider, prevEditorState, editorState, dirtyElements, dirtyLeaves, normalizedNodes, tags);
      }
    }),
  );

  initLocalState(provider as unknown as Provider, o.user.name, o.user.color, false, {});
  provider.connect();
  const leave = () => provider.awareness.setLocalState(null);
  window.addEventListener("pagehide", leave);

  return {
    editor,
    markInput,
    flush: save,
    insertFiles,
    async destroy() {
      await save().catch(() => {});
      window.removeEventListener("pagehide", leave);
      provider.awareness.off("update", onAwareness);
      doc.off("update", onDocUpdate);
      for (const ev of inputEvents) o.element.removeEventListener(ev, markInput, true);
      shared.unobserveDeep(onYjs);
      unregister();
      provider.awareness.setLocalState(null);
      provider.destroy();
      doc.destroy();
      editor.setRootElement(null);
    },
  };
}
