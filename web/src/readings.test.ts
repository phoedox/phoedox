import { expect, test } from "bun:test";
import { isBinary, nearest, niceTicks, plot, type Point } from "./readings";

test("ticks", () => {
  expect(niceTicks(18.2, 21.7)).toEqual([18, 19, 20, 21, 22]);
  expect(niceTicks(0, 1000)).toEqual([0, 250, 500, 750, 1000]);
  expect(niceTicks(5, 5).length).toBeGreaterThan(1);
});

test("plot lines, bands and steps", () => {
  const pts: Point[] = [[0, 18, 17, 19], [50, 20, 19, 21], [100, 19, 19, 19]];
  const p = plot(pts, 0, 100, 200, 100);
  expect(p.line.startsWith("M0.0,")).toBe(true);
  expect(p.band.endsWith("Z")).toBe(true);
  expect(p.binary).toBe(false);
  const s = plot([[0, 1, 1, 1], [50, 0, 0, 0]], 0, 100, 200, 100);
  expect(s.binary).toBe(true);
  expect(s.line).toContain("H100.0V");
  expect(s.line.endsWith("H200.0")).toBe(true);
  expect(s.band).toBe("");
  expect(isBinary([[0, 2, 2, 2]])).toBe(false);
  expect(nearest(pts, 60)?.[0]).toBe(50);
  expect(nearest(pts, 90)?.[0]).toBe(100);
});
