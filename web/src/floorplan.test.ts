import { describe, expect, test } from "bun:test";
import { area, areaM2, calibrate, clampOpening, inside, labelPoint, nearestWall, normalizePlan, onWall, openingSpan, orthogonal, planDiff, planForSave, planFrom, planItems, planMeta, roomAt, snap, snapToEnds, wallOutline, type Pt } from "./floorplan";

const sq = (x: number, y: number, w: number, h: number): Pt[] => [[x, y], [x + w, y], [x + w, y + h], [x, y + h]];

describe("floor plan geometry", () => {
  test("area and m²", () => {
    expect(area(sq(0, 0, 4000, 3000))).toBe(12e6);
    expect(areaM2(sq(0, 0, 4000, 3000))).toBe(12);
    expect(areaM2([[0, 0], [4, 0], [4, 1], [1, 1], [1, 4], [0, 4]].map(([x, y]) => [x * 1000, y * 1000] as Pt))).toBe(7);
  });
  test("inside and smallest room", () => {
    const plan = normalizePlan({ rooms: [{ id: "a", name: "Haus", points: sq(0, 0, 10000, 10000) }, { id: "b", name: "Bad", points: sq(1000, 1000, 2000, 2000) }] });
    expect(inside(500, 500, plan.rooms[0].points)).toBe(true);
    expect(roomAt(plan, 1500, 1500)?.id).toBe("b");
    expect(roomAt(plan, 5000, 5000)?.id).toBe("a");
    expect(roomAt(plan, 20000, 1)).toBeUndefined();
  });
  test("label point stays inside an L", () => {
    const l: Pt[] = [[0, 0], [4000, 0], [4000, 1000], [1000, 1000], [1000, 4000], [0, 4000]];
    const [x, y] = labelPoint(l);
    expect(inside(x, y, l)).toBe(true);
  });
  test("snap, orthogonal and calibration", () => {
    expect(snap(1234, 100)).toBe(1200);
    expect(snap(1234.4, 0)).toBe(1234);
    expect(orthogonal([0, 0], [500, 40])).toEqual([500, 0]);
    expect(orthogonal([0, 0], [40, 500])).toEqual([0, 500]);
    expect(calibrate(20000, 2000, 3000)).toBe(30000);
    expect(calibrate(20000, 0, 3000)).toBe(20000);
  });
  test("defaults", () => {
    const p = normalizePlan(undefined);
    expect([p.width, p.height, p.rooms.length]).toEqual([20000, 15000, 0]);
  });
});

describe("walls and live entries", () => {
  const base = normalizePlan({
    rooms: [{ id: "r1", name: "Big", points: [[0, 0], [5000, 0], [5000, 5000], [0, 5000]] }, { id: "r0", name: "Small", points: [[0, 0], [1000, 0], [1000, 1000], [0, 1000]] }],
    walls: [{ id: "w1", a: [0, 0], b: [4000, 0], thickness: 0 }],
    openings: [{ id: "o1", wall: "w1", kind: "door", offset: 1000, width: 900 }],
  });

  test("defaults and save shape", () => {
    expect(base.walls[0].thickness).toBe(120);
    expect(planForSave(normalizePlan(undefined))).not.toHaveProperty("walls");
  });

  test("wall geometry", () => {
    const w = base.walls[0];
    expect(onWall(w, [1000, 300])).toEqual({ offset: 1000, dist: 300, at: [1000, 0] });
    expect(onWall(w, [-500, 0]).offset).toBe(0);
    expect(nearestWall(base, [2000, 100], 50)?.wall.id).toBe("w1");
    expect(nearestWall(base, [2000, 500], 50)).toBeUndefined();
    expect(clampOpening(4000, 100, 900)).toBe(450);
    expect(openingSpan(w, base.openings[0])).toEqual([[550, 0], [1450, 0]]);
    expect(wallOutline(w)).toEqual([[0, 60], [4000, 60], [4000, -60], [0, -60]]);
    expect(snapToEnds(base, [3950, 30], 100)).toEqual([4000, 0]);
  });

  test("entries round-trip; diffs touch only what changed", () => {
    const again = planFrom(planMeta(base), planItems(base).values());
    expect(again.rooms.map((r) => r.id)).toEqual(["r1", "r0"]); // largest first
    expect(again.walls).toEqual(base.walls);
    const next = structuredClone(base);
    next.walls[0].thickness = 240;
    next.rooms = next.rooms.filter((r) => r.id !== "r0");
    const d = planDiff(base, next);
    expect(d.set.map((x) => x.id)).toEqual(["w1"]);
    expect(d.del).toEqual(["r0"]);
    expect(d.meta).toBe(false);
    next.width = 30000;
    expect(planDiff(base, next).meta).toBe(true);
  });

  test("openings of removed walls are dropped", () => {
    const items = [...planItems(base).values()].filter((x) => x.id !== "w1");
    expect(planFrom(planMeta(base), items).openings).toEqual([]);
  });
});
