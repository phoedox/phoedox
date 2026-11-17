import { expect, test } from "bun:test";
import { blockDef } from "./blocks";
import { parseAnnotated } from "./blocks/annotate";
import { parseBeforeAfter } from "./blocks/beforeafter";
import { layoutMindmap, mindmapElements, mindmapSVG, parseMindmap } from "./blocks/mindmap";

test("mind map: outline, balance, collapse, whiteboard shapes", () => {
  const { model } = parseMindmap("Umzug\n  Wohnung\n    Kündigen\n    Übergabe\n  Internet\n    Vertrag\n  - Kartons\n    - Küche");
  expect(model!.children.map((c) => c.text)).toEqual(["Wohnung", "Internet", "Kartons"]);
  expect(model!.children[1].children[0].text).toBe("Vertrag");
  const l = layoutMindmap(model!);
  const sides = new Set(l.nodes.filter((n) => n.depth === 1).map((n) => n.side));
  expect(sides).toEqual(new Set([1, -1]));
  // No two boxes on the same side overlap.
  for (const a of l.nodes) for (const b of l.nodes) {
    if (a === b) continue;
    const overlap = a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
    expect(overlap).toBe(false);
  }
  const c = layoutMindmap(model!, new Set(["0.0"]));
  expect(c.nodes.length).toBe(l.nodes.length - 2);
  expect(c.nodes.find((n) => n.node.id === "0.0")!.hidden).toBe(2);
  expect(mindmapSVG(model!, { interactive: true })).toContain('aria-expanded="true"');
  const els = mindmapElements(model!);
  expect(els.filter((e) => e.type === "line").length).toBe(els.length - 1 - (els.length - 1) / 2);
  expect(parseMindmap("  \n").problems[0].key).toBe("mindmap.err_empty");
});

test("before/after and annotated images", () => {
  const ba = parseBeforeAfter("title: Rack\nbefore: /api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXS Vorher\nafter: 01M7ND6Y5KRMY1R09EK7Q7NMXT Nachher\nstart: 30");
  expect(ba.model).toMatchObject({ before: "/api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXS", after: "/api/v1/files/01M7ND6Y5KRMY1R09EK7Q7NMXT", beforeLabel: "Vorher", start: 30 });
  expect(parseBeforeAfter("![a](/api/v1/files/AB12)\n![b](https://example.org/b.jpg)").model).toMatchObject({ beforeLabel: "a", after: "https://example.org/b.jpg" });
  expect(parseBeforeAfter("before: javascript:alert(1)").problems[0].key).toBe("image.err_src");
  expect(parseBeforeAfter("title: x\nbefore:\nafter:").model?.before).toBeUndefined();
  const an = parseAnnotated("image: /api/v1/files/AB12 Kasten\n- 22, 30: F1 Küche und @herd\n- 74,5% ; 64 blue: FI");
  expect(an.problems).toEqual([]);
  expect(an.model!.markers[0]).toMatchObject({ x: 22, y: 30, color: "red", entities: ["herd"] });
  expect(an.model!.markers[1]).toMatchObject({ x: 74.5, y: 64, color: "blue" });
  expect(parseAnnotated("image: /api/v1/files/AB12\n- 120, 3: x").problems[0].key).toBe("annotate.err_pos");
  expect(parseAnnotated("image: /api/v1/files/AB12\n- 1, 3 teal: x").problems[0].key).toBe("annotate.err_color");
  for (const k of ["mindmap", "beforeafter", "annotate"]) {
    const d = blockDef(k)!;
    const { model } = d.parse(d.example);
    expect(model).toBeTruthy();
    expect(d.svg(model, { width: 500, locale: "de" })).toStartWith("<svg");
  }
});
