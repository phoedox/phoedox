import { describe, expect, test } from "bun:test";
import { parseGantt, serializeGantt, isGantt } from "../mermaid-gantt";
import { readBarColors, readFlowColor, readThemeCss, writeBarColors, writeFlowColors, writeThemeCss } from "./colors";
import { parseMermaid, writeMermaid } from "./index";
import { zoomStep } from "../zoom";

describe("flowchart node colours", () => {
  test("palette colours become classDef + class, custom ones style", () => {
    const lines = writeFlowColors([{ id: "a", color: "green" }, { id: "b", color: "#ff8800" }, { id: "c", color: "green" }, { id: "d" }], "  ");
    expect(lines).toEqual([
      "  classDef phx-green fill:#dcefe1,stroke:#2c8a4b",
      "  class a,c phx-green",
      "  style b fill:#ff8800,stroke:#ff8800,color:#000000",
    ]);
    expect(readFlowColor("class a,c phx-green")).toEqual({ ids: ["a", "c"], color: "green" });
    expect(readFlowColor("classDef phx-green fill:#dcefe1,stroke:#2c8a4b")).toEqual({ ids: [] });
    expect(readFlowColor("style b fill:#123456,stroke:#123456,color:#ffffff")).toEqual({ ids: ["b"], color: "#123456" });
    expect(readFlowColor("style b fill:#ff8800,stroke:#ff8800")).toEqual({ ids: ["b"], color: "#ff8800" });
    expect(readFlowColor("style b fill:#f00,stroke:#000,stroke-width:4px")).toBeNull();
    expect(readFlowColor("classDef hot fill:#f00")).toBeNull();
  });

  test("round trip through the flowchart form; foreign styles stay", () => {
    const src = "flowchart LR\n  a[One] --> b[Two]\n  classDef hot fill:#f00\n  class a phx-blue\n  style b fill:#123456,stroke:#123456\n  class x phx-red";
    const r = parseMermaid(src)!;
    const nodes = r.model.nodes as { id: string; color?: string }[];
    expect(nodes.find((n) => n.id === "a")!.color).toBe("blue");
    expect(nodes.find((n) => n.id === "b")!.color).toBe("#123456");
    expect(String(r.model.extra)).toContain("classDef hot fill:#f00");
    expect(String(r.model.extra)).toContain("class x phx-red");
    const out = writeMermaid(r.kind, r.model);
    expect(out).toContain("class a phx-blue");
    expect(out).toContain("classDef phx-blue");
    expect(out).toContain("style b fill:#123456,stroke:#123456,color:#ffffff");
    const again = parseMermaid(out)!;
    expect(again.model.nodes).toEqual(r.model.nodes);
  });
});

describe("front matter themeCSS", () => {
  test("adds, replaces and removes", () => {
    const a = writeThemeCss([], "x { fill: red; }");
    expect(a).toEqual(["---", "config:", '  themeCSS: "x { fill: red; }"', "---"]);
    expect(readThemeCss(a)).toBe("x { fill: red; }");
    const b = writeThemeCss(["---", "title: Plan", "config:", "  theme: forest", "---"], "y {}");
    expect(b).toEqual(["---", "title: Plan", "config:", '  themeCSS: "y {}"', "  theme: forest", "---"]);
    expect(writeThemeCss(a, "")).toEqual([]);
    expect(writeThemeCss(["---", "title: Plan", "config:", '  themeCSS: "y {}"', "---"], "")).toEqual(["---", "title: Plan", "---"]);
  });

  test("bar rules are split from other CSS", () => {
    const css = writeBarColors({ a2: "green", b1: "#ff0000" }, ".grid .tick { stroke: #ccc; }");
    expect(css).toContain("rect[id$='-a2'] { fill: var(--tb-green, #dcefe1) !important; stroke: var(--tc-green, #2c8a4b) !important; } text[id$='-a2-text'] { fill: var(--tc-green, #2c8a4b) !important; }");
    const back = readBarColors(css);
    expect(back.colors).toEqual({ a2: "green", b1: "#ff0000" });
    expect(back.rest).toBe(".grid .tick { stroke: #ccc; }");
  });
});

describe("gantt colours", () => {
  test("task colours are written to the front matter and read back", () => {
    const p = parseGantt("gantt\n  dateFormat YYYY-MM-DD\n  section A\n    One :a1, 2026-01-05, 3d\n    Two :b1, after a1, 2d");
    p.sections[0].tasks[1].color = "purple";
    const src = serializeGantt(p);
    expect(src.startsWith("---\nconfig:\n  themeCSS: ")).toBe(true);
    expect(isGantt(src)).toBe(true);
    const again = parseGantt(src);
    expect(again.sections[0].tasks[1].color).toBe("purple");
    expect(again.sections[0].tasks[0].color).toBeUndefined();
    again.sections[0].tasks[1].color = undefined;
    expect(serializeGantt(again).startsWith("gantt\n")).toBe(true);
  });

  test("keeps other front matter and colours of unknown ids", () => {
    const src = `---\ntitle: Plan\nconfig:\n  themeCSS: "rect[id$='-zz'] { fill: #00ff00 !important; stroke: #00ff00 !important; } .x{}"\n---\ngantt\n  A :a1, 2026-01-05, 3d`;
    const p = parseGantt(src);
    expect(p.css).toContain("-zz'");
    expect(serializeGantt(p)).toBe(src.replace("\n  A :", "\n        A                      :"));
  });
});

describe("zoom steps", () => {
  test("go through the steps and stop at the ends", () => {
    expect(zoomStep(1, 1)).toBe(1.25);
    expect(zoomStep(1, -1)).toBe(0.8);
    expect(zoomStep(1.1, 1)).toBe(1.25);
    expect(zoomStep(6, 1)).toBe(6);
    expect(zoomStep(0.25, -1)).toBe(0.25);
  });
});
