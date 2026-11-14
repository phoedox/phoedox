import { describe, expect, test } from "bun:test";
import { area, areaM2, calibrate, inside, labelPoint, normalizePlan, orthogonal, roomAt, snap, type Pt } from "./floorplan";

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
