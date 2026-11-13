import { expect, test } from "bun:test";
import { bounds, layout } from "./graph-layout";

const dist = (a: { x: number; y: number }, b: { x: number; y: number }) => Math.hypot(a.x - b.x, a.y - b.y);

test("connected nodes end up closer than unconnected ones", () => {
  const ids = ["a", "b", "c", "d", "e", "f"];
  const pos = layout(ids, [["a", "b"], ["b", "c"], ["a", "c"], ["d", "e"], ["e", "f"], ["d", "f"]]);
  expect(pos.size).toBe(6);
  expect(dist(pos.get("a")!, pos.get("b")!)).toBeLessThan(dist(pos.get("a")!, pos.get("e")!));
  for (const p of pos.values()) expect(Number.isFinite(p.x) && Number.isFinite(p.y)).toBe(true);
});

test("disconnected parts stay on screen", () => {
  const ids = Array.from({ length: 30 }, (_, i) => `n${i}`);
  const links: [string, string][] = [];
  for (let i = 0; i < 20; i += 2) links.push([ids[i], ids[i + 1]]);
  const pos = layout(ids, links);
  const b = bounds(pos.values(), 0);
  expect(b.w).toBeLessThan(1600);
  expect(b.h).toBeLessThan(1600);
  const P = [...pos.values()];
  for (let i = 0; i < P.length; i++) for (let j = i + 1; j < P.length; j++) expect(dist(P[i], P[j])).toBeGreaterThan(40);
});

test("deterministic and handles edge cases", () => {
  const a = layout(["x", "y"], [["x", "y"]]);
  const b = layout(["x", "y"], [["x", "y"]]);
  expect(a.get("x")).toEqual(b.get("x")!);
  expect(layout([], []).size).toBe(0);
  expect(layout(["solo"], [["solo", "missing"]]).size).toBe(1);
  expect(bounds([]).w).toBe(100);
});
