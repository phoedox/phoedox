import { expect, test } from "bun:test";
import { rackSlots } from "./rack";

test("rack slots from top, devices span their units, overlaps are reported", () => {
  const r = rackSlots(6, [
    { slug: "sw", name: "Switch", kind: "switch", u: 6, height: 1 },
    { slug: "nas", name: "NAS", kind: "server", u: 2, height: 2 },
    { slug: "x", name: "X", kind: "server", u: 3, height: 1 },
    { slug: "loose", name: "Loose", kind: "server", height: 1 },
    { slug: "big", name: "Big", kind: "server", u: 6, height: 4 },
  ]);
  expect(r.slots[0]).toMatchObject({ top: 6, bottom: 6, device: { slug: "sw" } });
  expect(r.unplaced.map((d) => d.slug).sort()).toEqual(["big", "loose"]);
  expect(r.conflicts.sort()).toEqual(["nas", "x"]);
  expect(r.slots.reduce((n, s) => n + (s.top - s.bottom + 1), 0)).toBe(6);
});
