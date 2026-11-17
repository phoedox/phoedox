import { describe, expect, test } from "bun:test";
import { backKey, bindingAt, bindTarget, codeLabel, diffBoards, edgePoint, embedKind, frontKey, hitTest, inFrame, linePts, playerURL, removeElement, sorted, wrapText, type WBElement, inBox, groupOf, TEMPLATES, templateEls, slides, toExcalidraw, fromExcalidraw, alignEls, distributeEls, cloneEls, snapBox, normalizeBoard, type Whiteboard } from "./whiteboard";

const box: WBElement = { id: "a", type: "rect", x: 0, y: 0, w: 100, h: 50 };
const oval: WBElement = { id: "b", type: "ellipse", x: 300, y: 0, w: 100, h: 50 };
const arrow: WBElement = { id: "l", type: "arrow", points: [[0, 0], [0, 0]], start: { element: "a" }, end: { element: "b", fx: 0, fy: 0.5 } };
const els = [box, oval, arrow];
const byId = (id: string) => els.find((e) => e.id === id);

describe("whiteboard geometry", () => {
  test("bound ends: towards the centre stop at the outline, fixed anchors stay", () => {
    const pts = linePts(arrow, byId);
    // End: the left middle of the ellipse, pushed 6 out.
    expect(pts[1]).toEqual([294, 25]);
    // Start: from the box centre towards the end, 8 outside the right edge.
    expect(pts[0][0]).toBeCloseTo(108);
    expect(pts[0][1]).toBeCloseTo(25);
    // Ellipses and diamonds use their own outline.
    expect(edgePoint(oval, 350, 200, 0)).toEqual([350, 50]);
  });

  test("hit test, bind targets and anchors", () => {
    expect(hitTest(els, 50, 25, 4, byId)?.id).toBe("a");
    expect(hitTest(els, 200, 25, 4, byId)?.id).toBe("l");
    expect(hitTest(els, 200, 200, 4, byId)).toBeUndefined();
    expect(bindTarget(els, 350, 25, 4)?.id).toBe("b");
    expect(bindTarget(els, 350, 25, 4, "b")).toBeUndefined();
    // Near the outline: a fixed point snapped to that side; inside: the centre.
    expect(bindingAt(box, 98, 20, 6)).toEqual({ element: "a", fx: 1, fy: 0.4 });
    expect(bindingAt(box, 50, 25, 6)).toEqual({ element: "a" });
  });

  test("deleting a shape unbinds arrows at their last position", () => {
    const rest = removeElement(els, "b");
    const a = rest.find((e) => e.id === "l")!;
    expect(a.end).toBeUndefined();
    expect(a.points![1]).toEqual([294, 25]);
    expect(a.start).toEqual({ element: "a" });
  });

  test("order keys: in front, behind, stable sort", () => {
    const k1 = frontKey([], 1_700_000_000_000), k2 = frontKey([{ id: "x", type: "rect", order: k1 }], 1_700_000_000_000);
    expect(k2 > k1).toBe(true);
    expect(backKey([{ id: "x", type: "rect", order: k1 }]) < k1).toBe(true);
    expect(sorted([{ id: "b", type: "rect", order: "2" }, { id: "a", type: "rect", order: "1" }, { id: "c", type: "rect" }]).map((e) => e.id)).toEqual(["c", "a", "b"]);
    expect(frontKey([])).toMatch(/^[0-9a-z]{1,40}$/);
  });

  test("frames: members by box or by frame id", () => {
    const f: WBElement = { id: "f", type: "frame", x: -10, y: -10, w: 130, h: 80 };
    expect(inFrame([f, ...els, { id: "z", type: "rect", x: 900, y: 0, w: 10, h: 10, frame: "f" }], f, byId).map((e) => e.id)).toEqual(["a", "z"]);
  });

  test("embeds: kinds, players, code labels", () => {
    expect(embedKind("https://youtu.be/dQw4w9WgXcQ")).toBe("video");
    expect(embedKind("https://github.com/o/r/pull/4")).toBe("code");
    expect(embedKind("https://example.org")).toBe("link");
    expect(playerURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ")).toBe("https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ");
    expect(playerURL("https://vimeo.com/123456")).toBe("https://player.vimeo.com/video/123456?dnt=1");
    expect(playerURL("https://example.org/watch?v=1")).toBe("");
    expect(codeLabel("https://git.example.org/family/infra/pulls/42")).toBe("family/infra #42");
  });

  test("text wrapping and board diffs", () => {
    expect(wrapText("one two three four", 80, 20)).toEqual(["one two", "three", "four"]);
    expect(wrapText("a\nb", 500, 20)).toEqual(["a", "b"]);
    const d = diffBoards({ elements: [box, oval] }, { elements: [{ ...box, text: "x" }, arrow], background: "dots" });
    expect(d.set.map((e) => e.id)).toEqual(["a", "l"]);
    expect(d.del).toEqual(["b"]);
    expect(d.bg).toBe(true);
  });
});

describe("whiteboard selection", () => {
  const els = (): WBElement[] => [
    { id: "a", type: "rect", x: 0, y: 0, w: 100, h: 50 },
    { id: "b", type: "rect", x: 300, y: 40, w: 60, h: 60, groups: ["g1"] },
    { id: "c", type: "rect", x: 140, y: 200, w: 40, h: 20, groups: ["g1"] },
    { id: "l", type: "arrow", points: [[0, 0], [10, 10]], start: { element: "a" }, end: { element: "z" } },
  ];
  const by = (list: WBElement[]) => (id: string) => list.find((e) => e.id === id);

  test("box selection takes whole elements only", () => {
    const l = els();
    expect(inBox(l, { x: -5, y: -5, w: 200, h: 100 }, by(l)).map((e) => e.id)).toEqual(["a"]);
  });
  test("groups select together", () => {
    const l = els();
    expect(groupOf(l, l[1]).map((e) => e.id)).toEqual(["b", "c"]);
    expect(groupOf(l, l[0]).map((e) => e.id)).toEqual(["a"]);
  });
  test("align and distribute", () => {
    const l = els().slice(0, 3);
    alignEls(l, "left", by(l));
    expect(l.map((e) => e.x)).toEqual([0, 0, 0]);
    alignEls(l, "vcenter", by(l));
    expect(l.map((e) => (e.y ?? 0) + (e.h ?? 0) / 2)).toEqual([110, 110, 110]);
    const d: WBElement[] = [{ id: "1", type: "rect", x: 0, y: 0, w: 10, h: 10 }, { id: "2", type: "rect", x: 15, y: 0, w: 10, h: 10 }, { id: "3", type: "rect", x: 100, y: 0, w: 10, h: 10 }];
    distributeEls(d, "x", by(d));
    expect(d.map((e) => e.x)).toEqual([0, 50, 100]);
  });
  test("copies get new ids and keep inner bindings", () => {
    const l = els();
    const c = cloneEls([l[0], l[1], l[2], l[3]], 24);
    expect(c.every((e, i) => e.id !== l[i].id)).toBe(true);
    expect(c[0].x).toBe(24);
    expect(c[3].start?.element).toBe(c[0].id);
    expect(c[3].end).toBeUndefined();
    expect(c[1].groups?.[0]).toBe(c[2].groups?.[0]);
    expect(c[1].groups?.[0]).not.toBe("g1");
  });
  test("snap to grid", () => {
    const e: WBElement = { id: "s", type: "rect", x: 13, y: 7, w: 101, h: 5 };
    snapBox(e);
    expect([e.x, e.y, e.w, e.h]).toEqual([12, 12, 96, 12]);
  });
});

describe("whiteboard templates and Excalidraw", () => {
  test("every template has elements with bound arrows inside", () => {
    for (const k of TEMPLATES) {
      const els = templateEls(k, {});
      expect(els.length).toBeGreaterThan(2);
      const ids = new Set(els.map((e) => e.id));
      for (const e of els) {
        if (e.start) expect(ids.has(e.start.element)).toBe(true);
        if (e.end) expect(ids.has(e.end.element)).toBe(true);
      }
    }
  });
  test("slides are frames in reading order", () => {
    const f = (id: string, x: number, y: number): WBElement => ({ id, type: "frame", x, y, w: 100, h: 100 });
    expect(slides([f("c", 0, 500), f("b", 400, 10), f("a", 0, 0), { id: "r", type: "rect" }]).map((e) => e.id)).toEqual(["a", "b", "c"]);
  });
  test("round trip through .excalidraw keeps shapes, labels, arrows and colours", () => {
    const board = { elements: [
      { id: "a", type: "rect", x: 0, y: 0, w: 100, h: 50, text: "Router", fill: "blue", stroke: "red", order: "a0" },
      { id: "b", type: "ellipse", x: 300, y: 0, w: 100, h: 50, order: "a1" },
      { id: "l", type: "arrow", points: [[0, 0], [0, 0]], start: { element: "a" }, end: { element: "b" }, order: "a2" },
      { id: "t", type: "text", x: 0, y: 200, w: 100, h: 30, text: "Note", order: "a3" },
      { id: "p", type: "pen", points: [[0, 0], [5, 5], [9, 2]], order: "a4" },
    ] as WBElement[] };
    const file = JSON.parse(toExcalidraw(board));
    expect(file.type).toBe("excalidraw");
    expect(file.elements.find((x: any) => x.containerId === "a").text).toBe("Router");
    const back = fromExcalidraw(JSON.stringify(file));
    const rect = back.find((e) => e.type === "rect")!;
    expect([rect.text, rect.fill, rect.stroke]).toEqual(["Router", "blue", "red"]);
    const arrow = back.find((e) => e.type === "arrow")!;
    expect(arrow.start?.element).toBe(rect.id);
    expect(back.filter((e) => e.type === "text").map((e) => e.text)).toEqual(["Note"]);
    expect(back.find((e) => e.type === "pen")?.points?.length).toBe(3);
    expect(() => fromExcalidraw("{}")).toThrow();
  });
});

test("prototypes: diff, normalize and screens hit by their label", () => {
  const a: Whiteboard = { elements: [], prototypes: [{ id: "p", name: "Shop" }] };
  const b: Whiteboard = { elements: [], prototypes: [{ id: "p", name: "Shop 2" }, { id: "q", name: "New" }] };
  const d = diffBoards(a, b);
  expect(d.protos.map((p) => p.id)).toEqual(["p", "q"]);
  expect(diffBoards(b, a).delProtos).toEqual(["q"]);
  expect(normalizeBoard({ elements: [], prototypes: [{ id: "p", name: "x" }] }).prototypes).toEqual([{ id: "p", name: "x" }]);
  const screen = { id: "s", type: "frame" as const, x: 0, y: 0, w: 390, h: 844, screen: { proto: "p" } };
  const frame = { id: "f", type: "frame" as const, x: 500, y: 0, w: 200, h: 200 };
  const byId = (id: string) => [screen, frame].find((e) => e.id === id);
  expect(hitTest([screen, frame], 100, -12, 4, byId)?.id).toBe("s"); // the label above a screen
  expect(hitTest([screen, frame], 100, 12, 4, byId)).toBeUndefined(); // inside: what is on it, not the screen
  expect(hitTest([screen, frame], 600, 12, 4, byId)?.id).toBe("f"); // plain frames by their title strip
});
