import { describe, expect, test } from "bun:test";
import { heatGrid, quality, roomStats } from "./wifi-heat";
import { normalizePlan, planFrom, planItems, planMeta, planForSave } from "./floorplan";

describe("wifi heat map", () => {
  const ms = [{ id: "a", x: 1000, y: 1000, rssi: -45, down: 400 }, { id: "b", x: 9000, y: 1000, rssi: -85, down: 8 }];

  test("interpolates between measurements", () => {
    const cells = heatGrid(ms, "rssi", { x: 0, y: 0, w: 10000, h: 2000 }, 10);
    const left = cells.find((c) => c.x === 0)!, right = cells.find((c) => c.x >= 9000)!;
    expect(left.v).toBeGreaterThan(-50);
    expect(right.v).toBeLessThan(-80);
    expect(heatGrid([], "rssi", { x: 0, y: 0, w: 10, h: 10 })).toEqual([]);
    // Far away cells stay empty.
    expect(heatGrid(ms, "down", { x: 0, y: 0, w: 100000, h: 1000 }, 10, 5000).length).toBeLessThan(10);
  });

  test("quality scale", () => {
    expect(quality("rssi", -40)).toBe(1);
    expect(quality("rssi", -95)).toBe(0);
    expect(quality("down", 300)).toBe(1);
    expect(quality("down", 5)).toBe(0);
  });

  test("averages per room", () => {
    const rooms = [{ id: "r1", name: "Office", points: [[0, 0], [5000, 0], [5000, 5000], [0, 5000]] as [number, number][] }];
    const st = roomStats(rooms, [...ms, { id: "c", x: 2000, y: 2000, rssi: -55, down: 200, ping: 4 }]);
    expect(st).toEqual([{ room: "r1", name: "Office", count: 2, rssi: -50, down: 300, up: undefined, ping: 4 }]);
  });

  test("measurements travel through plan items", () => {
    const p = normalizePlan({ measurements: ms });
    const back = planFrom(planMeta(p), planItems(p).values());
    expect(back.measurements.map((m) => m.id)).toEqual(["a", "b"]);
    expect("measurements" in planForSave(normalizePlan({}))).toBe(false);
  });
});

import { median } from "./speedtest";
test("median", () => {
  expect(median([5, 1, 3])).toBe(3);
  expect(median([4, 1, 3, 2])).toBe(2.5);
  expect(median([])).toBe(0);
});
