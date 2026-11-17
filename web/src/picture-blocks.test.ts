import { expect, test } from "bun:test";
import { blockDef } from "./blocks";
import { parseAnnotated } from "./blocks/annotate";
import { parseBeforeAfter } from "./blocks/beforeafter";
import { cardItems, ganttSVG, parseGantt } from "./blocks/gantt";
import { layoutMindmap, mindmapElements, mindmapSVG, parseMindmap } from "./blocks/mindmap";

const DAY = 864e5;

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

test("gantt: dates, durations, after, milestones, sections", () => {
  const { model, problems } = parseGantt("title: Bad\ntoday\n## Planung\nAngebote [a]: 2026-01-05, 2w, done\nWahl [w]: after a, 3d, 50%\n## Umbau\nAbriss: 2026-02-02 – 2026-02-06, red\nFertig: 2026-03-20");
  expect(problems).toEqual([]);
  const [a, w, ab, f] = model!.items;
  expect(a.done).toBe(true);
  expect((a.end - a.start) / DAY).toBe(14);
  expect(w.start).toBe(a.end);
  expect(w.progress).toBe(50);
  expect((ab.end - ab.start) / DAY).toBe(5);
  expect(ab.color).toBe("red");
  expect(f.milestone).toBe(true);
  expect(model!.sections).toEqual(["Planung", "Umbau"]);
  const svg = ganttSVG(model!, 700, "de", Date.UTC(2026, 1, 1));
  expect(svg).toContain("marker-end");
  expect(svg).toContain("heute");
  expect(parseGantt("X: after y, 2d").problems[0].key).toBe("gantt.err_after");
  expect(parseGantt("A [a]: after b, 1d\nB [b]: after a, 1d").problems.some((p) => p.key === "gantt.err_cycle")).toBe(true);
  expect(parseGantt("A: morgen").problems[0].key).toBe("gantt.err_date");
  expect(parseGantt("A: 2026-01-10, 2026-01-05").problems[0].key).toBe("gantt.err_order");
  const fromCards = cardItems({ title: "", sections: [], items: [], today: false, board: "roadmap" }, [
    { id: "1", title: "Release", due: "2026-03-01", createdAt: "2026-02-01T10:00:00Z", boardTitle: "Roadmap" },
    { id: "2", title: "No date" },
  ]);
  expect(fromCards.items.length).toBe(1);
  expect(fromCards.sections).toEqual(["Roadmap"]);
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
  for (const k of ["mindmap", "gantt", "beforeafter", "annotate"]) {
    const d = blockDef(k)!;
    const { model } = d.parse(d.example);
    expect(model).toBeTruthy();
    expect(d.svg(model, { width: 500, locale: "de" })).toStartWith("<svg");
  }
});
