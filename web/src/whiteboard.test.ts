import { describe, expect, test } from "bun:test";
import { backKey, bindingAt, bindTarget, codeLabel, diffBoards, edgePoint, embedKind, frontKey, hitTest, inFrame, linePts, playerURL, removeElement, sorted, wrapText, type WBElement } from "./whiteboard";

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
