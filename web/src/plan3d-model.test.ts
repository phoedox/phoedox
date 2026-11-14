import { describe, expect, test } from "bun:test";
import { normalizePlan } from "./floorplan";
import { RACK_UNIT, SLAB, buildScene, mountedByRack, stackLevels, wallPieces } from "./plan3d-model";

const plan = normalizePlan({
  walls: [{ id: "w1", a: [0, 0], b: [5000, 0], thickness: 200 }, { id: "w2", a: [5000, 0], b: [5000, 4000], thickness: 100, height: 3000 }],
  openings: [
    { id: "d", wall: "w1", kind: "door", offset: 1000, width: 1000 },
    { id: "f", wall: "w1", kind: "window", offset: 3500, width: 1000 },
  ],
  rooms: [{ id: "r1", name: "Kitchen", entity: "kitchen", color: "green", points: [[0, 0], [5000, 0], [5000, 4000], [0, 4000]] }],
  devices: [{ id: "d1", entity: "rack1", x: 1000, y: 2000 }, { id: "d2", entity: "printer", x: 4000, y: 3000 }],
});

describe("3D model", () => {
  test("walls are split around doors and windows", () => {
    const pieces = wallPieces(plan, "w1", 2500);
    expect(pieces).toEqual([
      { from: 0, to: 500, bottom: 0, top: 2500, kind: "wall" },
      { from: 500, to: 1500, bottom: 2100, top: 2500, kind: "wall" }, // lintel over the door
      { from: 1500, to: 3000, bottom: 0, top: 2500, kind: "wall" },
      { from: 3000, to: 4000, bottom: 0, top: 900, kind: "wall" }, // parapet
      { from: 3000, to: 4000, bottom: 900, top: 2100, kind: "glass" },
      { from: 3000, to: 4000, bottom: 2100, top: 2500, kind: "wall" },
      { from: 4000, to: 5000, bottom: 0, top: 2500, kind: "wall" },
    ]);
    // Cut away at 1.2 m: no lintels, parapets and glass end there too.
    const cut = wallPieces(plan, "w1", 2500, 1200);
    expect(cut.map((x) => [x.from, x.top, x.kind])).toEqual([[0, 1200, "wall"], [1500, 1200, "wall"], [3000, 900, "wall"], [3000, 1200, "glass"], [4000, 1200, "wall"]]);
    // A wall's own height wins.
    expect(wallPieces(plan, "w2", 2500)[0].top).toBe(3000);
  });

  test("scene in metres, racks with their units, labels", () => {
    const entries = new Map([
      ["rack1", { slug: "rack1", name: "Rack", kind: "rack", attrs: { height_units: 12, depth: 600 } }],
      ["printer", { slug: "printer", name: "Printer", kind: "device", attrs: {} }],
      ["kitchen", { slug: "kitchen", name: "Kitchen", kind: "room", attrs: {} }],
    ]);
    const mounted = mountedByRack([
      { rel: "mounted_in", fromSlug: "sw", fromName: "Switch", toSlug: "rack1", attrs: { u: 1, height: 1 } },
      { rel: "mounted_in", fromSlug: "nas", fromName: "NAS", toSlug: "rack1", attrs: { u: 11, height: 4 } }, // does not fit
      { rel: "located_in", fromSlug: "x", fromName: "X", toSlug: "rack1" },
    ]);
    expect(mounted.get("rack1")).toHaveLength(2);
    const s = buildScene([{ plan, elevation: 0, wallHeight: 2500 }], entries, mounted);
    const first = s.boxes[0];
    expect(first).toMatchObject({ kind: "wall", x: 0.25, z: 0, y: 1.25, w: 0.5, h: 2.5, d: 0.2 });
    const w2 = s.boxes.find((b) => b.kind === "wall" && b.d === 0.1)!;
    expect(w2.rot).toBeCloseTo(-Math.PI / 2);
    expect(w2.x).toBe(5);
    const rack = s.boxes.find((b) => b.kind === "rack")!;
    expect(rack.h).toBeCloseTo((12 * RACK_UNIT + 100) / 1000);
    const units = s.boxes.filter((b) => b.kind === "unit");
    expect(units.map((b) => b.slug)).toEqual(["sw"]);
    expect(units[0].z + units[0].d / 2).toBeCloseTo(rack.z + rack.d / 2 + 0.01); // flush with the front, 1 cm out
    expect(s.boxes.find((b) => b.kind === "device")?.slug).toBe("printer");
    expect(s.slabs[0]).toMatchObject({ slug: "kitchen", color: "green", y: 0 });
    expect(s.labels.map((l) => l.text)).toEqual(["Kitchen", "Rack", "Printer"]);
    expect(s.min[0]).toBeCloseTo(0);
    expect(s.max[1]).toBeCloseTo(3); // the 3 m wall
  });

  test("floors are stacked by level", () => {
    const upper = normalizePlan({ walls: [{ id: "w", a: [0, 0], b: [1000, 0], thickness: 100 }] });
    const levels = stackLevels([
      { plan: upper, floor: { slug: "og", name: "Upper", kind: "floor", attrs: { level: 1 } } },
      { plan, floor: { slug: "eg", name: "Ground", kind: "floor", attrs: { level: 0, ceiling_height: 2.6 } } },
    ]);
    expect(levels.map((l) => l.name)).toEqual(["Ground", "Upper"]);
    expect(levels[0].wallHeight).toBe(2600);
    // The ground floor has a 3 m wall, so the next storey starts above it.
    expect(levels[1].elevation).toBe(3000 + SLAB);
    const s = buildScene(levels, new Map(), new Map());
    expect(s.labels.filter((l) => l.kind === "level").map((l) => l.text)).toEqual(["Ground", "Upper"]);
  });
});
