// Live whiteboards (ADR-0068): every element and comment is one entry of a
// shared Yjs map, so people working on different elements never overwrite
// each other. There is no draft: the editor saves the board after a short
// pause and it is published at once. Viewers watch read-only. Cursors and
// the laser pointer go through awareness only and are never stored.

import * as decoding from "lib0/decoding";
import * as encoding from "lib0/encoding";
import { WebsocketProvider } from "y-websocket";
import * as Y from "yjs";
import { api } from "./api";
import { MSG_CONTROL, MSG_SNAPSHOT, type Control } from "./collab";
import { diffBoards, normalizeBoard, type Background, type Pt, type WBComment, type WBElement, type Whiteboard } from "./whiteboard";

export type Status = "connecting" | "connected" | "disconnected";
export interface Peer { id: number; name: string; color: string; cursor?: Pt; laser?: Pt[]; laserAt?: number; selection?: string }

export interface LiveOptions {
  url: string;
  project: string;
  doc: string;
  initial: Whiteboard;
  readOnly: boolean;
  user: { name: string; color: string };
  onChange: (board: Whiteboard, remote: boolean) => void;
  onPeers?: (peers: Peer[]) => void;
  onStatus?: (s: Status) => void;
  /** Saving state: "saving", "saved" (with the time) or "error". */
  onSaved?: (state: "saving" | "saved" | "error", at?: Date) => void;
  /** The board changed outside the editor (e.g. the AI) and was reloaded. */
  onReset?: (by: string, ai: boolean, changed: string[]) => void;
}

export interface Live {
  apply(before: Whiteboard, after: Whiteboard): void;
  board(): Whiteboard;
  setCursor(p?: Pt): void;
  setLaser(points?: Pt[]): void;
  setSelection(id?: string): void;
  undo(): void;
  redo(): void;
  canUndo(): boolean;
  flush(): Promise<void>;
  destroy(): Promise<void>;
}

const SAVE_DELAY = 900;

export function connectBoard(o: LiveOptions): Live {
  const doc = new Y.Doc();
  const els = doc.getMap<WBElement>("els");
  const notes = doc.getMap<WBComment>("notes");
  const meta = doc.getMap<string>("meta");
  const LOCAL = { local: true };
  const undo = new Y.UndoManager([els, notes, meta], { trackedOrigins: new Set([LOCAL]), captureTimeout: 500 });
  const provider = new WebsocketProvider(o.url, o.doc, doc, { connect: false, disableBc: true });
  const current = (): Whiteboard => ({
    version: 1, background: (meta.get("bg") ?? "") as Background,
    elements: [...els.values()].map((e) => ({ ...e })),
    comments: [...notes.values()].map((c) => ({ ...c })),
  });

  const write = (before: Whiteboard, after: Whiteboard) => {
    const d = diffBoards(before, after);
    for (const id of d.del) els.delete(id);
    for (const e of d.set) els.set(e.id, e);
    for (const id of d.delNotes) notes.delete(id);
    for (const c of d.notes) notes.set(c.id, c);
    if (d.bg) meta.set("bg", after.background ?? "");
  };
  const fill = (b: Whiteboard) => {
    els.clear();
    notes.clear();
    meta.clear();
    write({ elements: [], comments: [] }, b);
  };

  // Saving: only this person's changes, after a pause.
  let dirty = false;
  let timer: number | undefined;
  let saving: Promise<void> | undefined;
  const save = async () => {
    window.clearTimeout(timer);
    if (!dirty || o.readOnly) return;
    dirty = false;
    o.onSaved?.("saving");
    saving = api("PUT", `/projects/${o.project}/documents/${o.doc}/whiteboard`, current())
      .then(() => o.onSaved?.("saved", new Date()))
      .catch(() => {
        dirty = true;
        o.onSaved?.("error");
        timer = window.setTimeout(save, 5000);
      });
    await saving;
  };
  const schedule = () => {
    dirty = true;
    window.clearTimeout(timer);
    timer = window.setTimeout(save, SAVE_DELAY);
  };

  doc.on("afterTransaction", (tx: Y.Transaction) => {
    if (!tx.changed.size) return;
    if (tx.origin === LOCAL || tx.origin instanceof Y.UndoManager) schedule();
    if (tx.origin !== LOCAL) o.onChange(current(), true);
  });

  // The board changed outside (API, AI, restore): load it again for everyone
  // here. The first editor to see the reset fills the empty shared state.
  const reload = async (by: string, ai: boolean) => {
    try {
      const before = new Map(current().elements.map((e) => [e.id, JSON.stringify(e)]));
      const d = await api<{ content: Partial<Whiteboard> }>("GET", `/projects/${o.project}/documents/${o.doc}`);
      const b = normalizeBoard(d.content);
      if (!o.readOnly) doc.transact(() => fill(b), "reset");
      o.onChange(b, true);
      o.onReset?.(by, ai, b.elements.filter((e) => before.get(e.id) !== JSON.stringify(e)).map((e) => e.id));
    } catch { /* offline: the next sync brings it */ }
  };

  let shouldBootstrap = false;
  provider.messageHandlers[MSG_CONTROL] = (_enc, decoder) => {
    const ctl = JSON.parse(decoding.readVarString(decoder)) as Control;
    if (ctl.type === "bootstrap") shouldBootstrap = true;
    if (ctl.type === "reset") {
      dirty = false;
      window.clearTimeout(timer);
      undo.clear();
      void reload(ctl.by ?? "", !!ctl.ai);
    }
    if (ctl.type === "compact" && ctl.upto && !o.readOnly) {
      const enc = encoding.createEncoder();
      encoding.writeVarUint(enc, MSG_SNAPSHOT);
      encoding.writeVarUint(enc, ctl.upto);
      encoding.writeVarUint8Array(enc, Y.encodeStateAsUpdate(doc));
      provider.ws?.send(encoding.toUint8Array(enc));
    }
  };
  provider.on("sync", (synced: boolean) => {
    if (!synced || !shouldBootstrap) return;
    shouldBootstrap = false;
    if (els.size || notes.size || meta.size) return;
    // The first editor fills the shared state from the published board.
    doc.transact(() => fill(o.initial), "bootstrap");
  });
  provider.on("status", (e: { status: Status }) => o.onStatus?.(e.status));

  provider.awareness.setLocalState({ name: o.user.name, color: o.user.color });
  const emitPeers = () => {
    const peers: Peer[] = [];
    provider.awareness.getStates().forEach((s, id) => {
      if (id !== doc.clientID && s?.name) peers.push({ id, name: s.name, color: s.color, cursor: s.cursor, laser: s.laser, laserAt: s.laserAt, selection: s.selection });
    });
    o.onPeers?.(peers);
  };
  provider.awareness.on("change", emitPeers);
  const leave = () => provider.awareness.setLocalState(null);
  window.addEventListener("pagehide", leave);
  provider.connect();

  // Awareness updates are throttled to one per animation frame.
  let pending: Record<string, unknown> | undefined;
  const push = (field: string, v: unknown) => {
    if (!pending) {
      pending = {};
      requestAnimationFrame(() => {
        const p = pending!;
        pending = undefined;
        const cur = provider.awareness.getLocalState() ?? {};
        provider.awareness.setLocalState({ ...cur, ...p });
      });
    }
    pending[field] = v;
  };

  return {
    apply(before, after) {
      if (o.readOnly) return;
      doc.transact(() => write(before, after), LOCAL);
    },
    board: current,
    setCursor(p) {
      push("cursor", p ? [Math.round(p[0]), Math.round(p[1])] : null);
    },
    setLaser(points) {
      push("laser", points?.length ? points.slice(-40).map((p) => [Math.round(p[0]), Math.round(p[1])]) : null);
      push("laserAt", Date.now());
    },
    setSelection(id) {
      push("selection", id ?? null);
    },
    undo: () => undo.undo(),
    redo: () => undo.redo(),
    canUndo: () => undo.undoStack.length > 0,
    async flush() {
      await save();
      await saving;
    },
    async destroy() {
      await this.flush().catch(() => {});
      window.removeEventListener("pagehide", leave);
      leave();
      undo.destroy();
      provider.destroy();
      doc.destroy();
    },
  };
}
