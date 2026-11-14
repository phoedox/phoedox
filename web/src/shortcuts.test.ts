import { describe, expect, test } from "bun:test";
import { comboFor, comboLabel, comboOf } from "./shortcuts";

const ev = (key: string, code: string, m: Partial<Record<"ctrlKey" | "metaKey" | "altKey" | "shiftKey", boolean>> = {}) =>
  ({ key, code, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...m });

describe("shortcuts", () => {
  test("combinations from key events", () => {
    expect(comboOf(ev("k", "KeyK", { ctrlKey: true }), false)).toBe("Mod+K");
    expect(comboOf(ev("k", "KeyK", { metaKey: true }), true)).toBe("Mod+K");
    expect(comboOf(ev("F", "KeyF", { ctrlKey: true, shiftKey: true }), false)).toBe("Mod+Shift+F");
    // Alt on a Mac types special characters: the physical key counts.
    expect(comboOf(ev("˙", "KeyH", { altKey: true }), true)).toBe("Alt+H");
    expect(comboOf(ev("Control", "ControlLeft", { ctrlKey: true }), false)).toBe("");
    expect(comboOf(ev("Escape", "Escape"), false)).toBe("Escape");
    expect(comboOf(ev("1", "Digit1", { altKey: true }), false)).toBe("Alt+1");
  });
  test("labels and own combinations", () => {
    expect(comboLabel("Mod+Shift+F", true)).toBe("⌘⇧F");
    expect(comboLabel("Mod+Shift+F", false)).toBe("Ctrl+Shift+F");
    expect(comboFor("palette", {})).toBe("Mod+K");
    expect(comboFor("palette", { palette: "Alt+P" })).toBe("Alt+P");
    expect(comboFor("history", { history: "" })).toBe("");
  });
});
