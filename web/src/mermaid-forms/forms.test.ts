import { describe, expect, test } from "bun:test";
import { GALLERY, MERMAID_KINDS, kindOf, parseMermaid, writeMermaid } from "./index";
import { splitSource } from "./core";
import { parseLink, parseNode } from "./flow";

describe("mermaid forms", () => {
  for (const kind of MERMAID_KINDS) {
    test(`${kind.id}: the example parses fully and survives a round trip`, () => {
      expect(kindOf(kind.example)?.id).toBe(kind.id);
      const a = parseMermaid(kind.example)!;
      expect(a.kind.id).toBe(kind.id);
      // Everything of the example is understood by the form (no further lines), except where kept on purpose.
      if (!["usecase"].includes(kind.id)) expect(String(a.model.extra ?? "")).toBe("");
      const written = writeMermaid(a.kind, a.model);
      const b = parseMermaid(written)!;
      expect(b.model).toEqual(a.model);
      // Writing again gives the same text (stable).
      expect(writeMermaid(b.kind, b.model)).toBe(written);
    });
  }

  test("every gallery entry has a unique id and an example", () => {
    expect(new Set(GALLERY.map((g) => g.id)).size).toBe(GALLERY.length);
    for (const g of GALLERY) expect(g.example.length).toBeGreaterThan(10);
  });

  test("front matter and directives before the keyword are kept", () => {
    const src = "---\ntitle: Net\n---\n%%{init: {'theme':'forest'}}%%\nflowchart LR\n  A --> B";
    expect(splitSource(src).head).toBe("flowchart LR");
    const p = parseMermaid(src)!;
    expect(writeMermaid(p.kind, p.model).startsWith("---\ntitle: Net\n---\n%%{init")).toBe(true);
  });

  test("unknown lines are kept", () => {
    const p = parseMermaid("flowchart TD\n  A --> B\n  classDef hot fill:#f00\n  class A hot")!;
    expect(p.model.extra).toBe("classDef hot fill:#f00\nclass A hot");
    expect(writeMermaid(p.kind, p.model)).toContain("    classDef hot fill:#f00");
  });

  test("flowchart nodes and links", () => {
    expect(parseNode("db[(Database)] --> x")).toEqual({ node: { id: "db", text: "Database", shape: "cylinder" }, len: 14 });
    expect(parseNode('q{"Is it ok?"}')!.node).toEqual({ id: "q", text: "Is it ok?", shape: "rhombus" });
    expect(parseNode("A-->B")!.node.id).toBe("A");
    expect(parseLink(" -->|yes| B")!.link).toEqual({ line: "solid", head: "arrow", text: "yes" });
    expect(parseLink(" -- maybe --> B")!.link).toEqual({ line: "solid", head: "arrow", text: "maybe" });
    expect(parseLink(" -.-> B")!.link).toEqual({ line: "dotted", head: "arrow" });
    expect(parseLink(" ==> B")!.link).toEqual({ line: "thick", head: "arrow" });
    expect(parseLink(" <--> B")!.link).toEqual({ line: "solid", head: "both" });
    const p = parseMermaid("graph TD\n  A[Start] --> B{Ok?} -->|yes| C\n  subgraph net [Network]\n    C\n  end")!;
    expect((p.model.nodes as { id: string }[]).map((n) => n.id)).toEqual(["A", "B", "C"]);
    expect((p.model.edges as unknown[]).length).toBe(2);
    expect((p.model.groups as { id: string; title: string }[])[0]).toMatchObject({ id: "net", title: "Network" });
  });
});
