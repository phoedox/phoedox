// Live editing of a floor plan (ADR-0027 drafts, ADR-0034 plans): the draft
// is shared over the same Yjs connection as text pages. Every room, device,
// wall, opening and photo is one entry of a shared map, so people working on
// different elements never overwrite each other; plan settings are one entry.

import * as decoding from "lib0/decoding";
import * as encoding from "lib0/encoding";
import { WebsocketProvider } from "y-websocket";
import * as Y from "yjs";
import { MSG_CONTROL, MSG_SNAPSHOT, type Control } from "./collab";
import { planDiff, planFrom, planItems, planMeta, type Floorplan, type PlanItem, type PlanMeta, type Pt } from "./floorplan";

export type Status = "connecting" | "connected" | "disconnected";
export interface PlanPeer { id: number; name: string; color: string; cursor?: Pt }

export interface PlanLiveOptions {
  url: string;
  docId: string;
  /** The draft's plan, used when the shared state is still empty. */
  initial: Floorplan;
  user: { name: string; color: string };
  onChange: (plan: Floorplan) => void;
  onSave: (plan: Floorplan) => Promise<void>;
  onPeers?: (peers: PlanPeer[]) => void;
  onStatus?: (s: Status) => void;
  onSaving?: (saving: boolean) => void;
  onReset?: (by: string) => void;
  onEnded?: (reason: string, by: string) => void;
}

export interface PlanLive {
  /** Records a local change (before → after). */
  apply(before: Floorplan, after: Floorplan): void;
  plan(): Floorplan;
  setCursor(p?: Pt): void;
  undo(): void;
  redo(): void;
  flush(): Promise<void>;
  destroy(): Promise<void>;
}

const SAVE_DELAY = 1200;

export function connectPlan(o: PlanLiveOptions): PlanLive {
  const doc = new Y.Doc();
  const items = doc.getMap<PlanItem>("items");
  const meta = doc.getMap<PlanMeta>("meta");
  const LOCAL = { local: true };
  const undo = new Y.UndoManager([items, meta], { trackedOrigins: new Set([LOCAL]), captureTimeout: 400 });
  const provider = new WebsocketProvider(o.url, o.docId, doc, { connect: false, disableBc: true });
  const current = () => planFrom(meta.get("v") ?? planMeta(o.initial), items.values());

  // Saving: only after this person's changes, debounced.
  let dirty = false;
  let timer: number | undefined;
  let saving: Promise<void> | undefined;
  const save = async () => {
    window.clearTimeout(timer);
    if (!dirty) return;
    dirty = false;
    o.onSaving?.(true);
    saving = o.onSave(current()).catch(() => {
      dirty = true;
      timer = window.setTimeout(save, 5000);
    }).finally(() => o.onSaving?.(false));
    await saving;
  };
  const schedule = () => {
    dirty = true;
    window.clearTimeout(timer);
    timer = window.setTimeout(save, SAVE_DELAY);
  };

  const write = (set: PlanItem[], del: string[], m?: PlanMeta) => {
    for (const id of del) items.delete(id);
    for (const it of set) items.set(it.id, it);
    if (m) meta.set("v", m);
  };

  doc.on("afterTransaction", (tx: Y.Transaction) => {
    if (!tx.changed.size) return;
    if (tx.origin === LOCAL || tx.origin instanceof Y.UndoManager) schedule();
    if (tx.origin !== LOCAL) o.onChange(current());
  });

  let shouldBootstrap = false;
  provider.messageHandlers[MSG_CONTROL] = (_enc, decoder) => {
    const ctl = JSON.parse(decoding.readVarString(decoder)) as Control;
    if (ctl.type === "bootstrap") shouldBootstrap = true;
    if (ctl.type === "ended") {
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
  provider.on("sync", (synced: boolean) => {
    if (!synced || !shouldBootstrap) return;
    shouldBootstrap = false;
    if (items.size || meta.size) return;
    // The first editor fills the shared state from the draft (not undoable, nothing to save).
    doc.transact(() => write([...planItems(o.initial).values()], [], planMeta(o.initial)), "bootstrap");
  });
  provider.on("status", (e: { status: Status }) => o.onStatus?.(e.status));

  provider.awareness.setLocalState({ name: o.user.name, color: o.user.color });
  const emitPeers = () => {
    const peers: PlanPeer[] = [];
    provider.awareness.getStates().forEach((s, id) => {
      if (id !== doc.clientID && s?.name) peers.push({ id, name: s.name, color: s.color, cursor: s.cursor });
    });
    o.onPeers?.(peers);
  };
  provider.awareness.on("change", emitPeers);
  const leave = () => provider.awareness.setLocalState(null);
  window.addEventListener("pagehide", leave);
  provider.connect();

  return {
    apply(before, after) {
      const d = planDiff(before, after);
      if (!d.set.length && !d.del.length && !d.meta) return;
      doc.transact(() => write(d.set, d.del, d.meta ? planMeta(after) : undefined), LOCAL);
    },
    plan: current,
    setCursor(p) {
      provider.awareness.setLocalStateField("cursor", p ? [Math.round(p[0]), Math.round(p[1])] : null);
    },
    undo: () => undo.undo(),
    redo: () => undo.redo(),
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
