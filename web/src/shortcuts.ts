// Keyboard shortcuts: every action has a default combination that people
// can change (stored on the server, so it follows them across devices).
// "Mod" is ⌘ on Apple devices and Ctrl elsewhere.

import { signal } from "@lit-labs/signals";
import { api } from "./api";

export interface ShortcutAction { id: string; combo: string; label: string }

/** Actions with their default combinations (labels are i18n keys). */
export const SHORTCUT_ACTIONS: ShortcutAction[] = [
  { id: "palette", combo: "Mod+K", label: "shortcut.palette" },
  { id: "focus", combo: "Mod+Shift+F", label: "shortcut.focus" },
  { id: "history", combo: "Alt+H", label: "shortcut.history" },
  { id: "tasks", combo: "Alt+T", label: "shortcut.tasks" },
  { id: "newPage", combo: "Alt+N", label: "shortcut.new_page" },
  { id: "wide", combo: "Alt+W", label: "shortcut.wide" },
  { id: "projects", combo: "Alt+P", label: "shortcut.projects" },
];

/** The person's own combinations (action → combo, "" = off). */
export const customShortcuts = signal<Record<string, string>>({});

export const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform);

/** The combination an action currently has ("" = none). */
export function comboFor(id: string, custom = customShortcuts.get()): string {
  if (id in custom) return custom[id];
  return SHORTCUT_ACTIONS.find((a) => a.id === id)?.combo ?? "";
}

const KEY_NAMES: Record<string, string> = { " ": "Space", Esc: "Escape" };

/** The combination of a key event in the stored notation, e.g. "Mod+Shift+F". */
export function comboOf(e: Pick<KeyboardEvent, "key" | "code" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey">, mac = isMac()): string {
  if (["Control", "Meta", "Alt", "Shift"].includes(e.key)) return "";
  const parts: string[] = [];
  const mod = mac ? e.metaKey : e.ctrlKey;
  const extra = mac ? e.ctrlKey : e.metaKey;
  if (mod) parts.push("Mod");
  if (extra) parts.push(mac ? "Ctrl" : "Meta");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  // With Alt (and on some layouts Shift) e.key is a special character: use the physical key.
  let key = e.code?.startsWith("Key") ? e.code.slice(3) : e.code?.startsWith("Digit") ? e.code.slice(5) : KEY_NAMES[e.key] ?? e.key;
  if (key.length === 1) key = key.toUpperCase();
  parts.push(key);
  return parts.join("+");
}

/** How a combination is shown, e.g. "⌘⇧F" or "Ctrl+Shift+F". */
export function comboLabel(combo: string, mac = isMac()): string {
  if (!combo) return "";
  const names: Record<string, string> = mac
    ? { Mod: "⌘", Ctrl: "⌃", Alt: "⌥", Shift: "⇧", Meta: "⌘", Enter: "↩", Escape: "Esc" }
    : { Mod: "Ctrl", Meta: "Win" };
  const parts = combo.split("+").map((p) => names[p] ?? p);
  return mac ? parts.join("") : parts.join("+");
}

type Handler = (e: KeyboardEvent) => void;
const handlers = new Map<string, Handler>();

/** Registers what an action does; returns a function that removes it. */
export function onShortcut(id: string, fn: Handler): () => void {
  handlers.set(id, fn);
  return () => {
    if (handlers.get(id) === fn) handlers.delete(id);
  };
}

function typing(e: KeyboardEvent): boolean {
  const el = e.composedPath()[0] as HTMLElement | undefined;
  return !!el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName));
}

/** Runs the action of a key event, if any. Plain keys never fire while typing. */
export function dispatchShortcut(e: KeyboardEvent): boolean {
  const combo = comboOf(e);
  if (!combo) return false;
  const custom = customShortcuts.get();
  for (const a of SHORTCUT_ACTIONS) {
    if (comboFor(a.id, custom) !== combo) continue;
    const plain = !/^(Mod|Ctrl|Meta|Alt)\+/.test(combo);
    if (plain && typing(e)) return false;
    const fn = handlers.get(a.id);
    if (!fn) return false;
    e.preventDefault();
    fn(e);
    return true;
  }
  return false;
}

let installed = false;
/** Loads the person's shortcuts and starts listening (once). */
export async function installShortcuts() {
  if (!installed) {
    installed = true;
    window.addEventListener("keydown", (e) => dispatchShortcut(e));
  }
  customShortcuts.set((await api<Record<string, string>>("GET", "/me/shortcuts").catch(() => ({}))) ?? {});
}
