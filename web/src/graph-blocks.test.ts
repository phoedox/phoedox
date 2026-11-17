import { expect, test } from "bun:test";
import { blockDef } from "./blocks";
import { findRoom, parsePlanBlock, planDevices, planSVG } from "./blocks/floorplanblock";
import { parseRack } from "./blocks/rackblock";
import { buildTree, layoutTree, parseTree, treeSVG } from "./blocks/tree";
import { normalizePlan } from "./floorplan";

test("tree: source, building from hops, layout", () => {
  expect(parseTree("root: @router\nrelation: uplink\ndirection: in\ndepth: 3").model).toEqual({ root: "router", relation: "uplink", direction: "in", depth: 3, title: "" });
  expect(parseTree("@oma\nrelation: parent_of\nrichtung: out").model?.direction).toBe("out");
  expect(parseTree("relation: x").problems[0].key).toBe("tree.err_root");
  expect(parseTree("root: @a\ndirection: sideways").problems[0].key).toBe("tree.err_direction");
  const e = (id: string, name: string) => ({ id, slug: id, name, kind: "device" });
  const root = buildTree(e("r", "Router"), [
    { entity: e("s1", "Switch Keller"), parent: "r" }, { entity: e("s2", "Switch Büro"), parent: "r" },
    { entity: e("a", "NAS"), parent: "s1" }, { entity: e("b", "Drucker"), parent: "s2" }, { entity: e("c", "Laptop"), parent: "s2" },
  ]);
  expect(root.children.map((c) => c.name)).toEqual(["Switch Büro", "Switch Keller"]);
  const { boxes } = layoutTree(root);
  for (const a of boxes) for (const b of boxes) {
    if (a === b) continue;
    expect(a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h).toBe(false);
  }
  // Parents sit centred over their children.
  const buero = boxes.find((b) => b.node.name === "Switch Büro")!;
  const kids = boxes.filter((b) => b.parent === buero);
  expect(buero.x).toBe((kids[0].x + kids[1].x) / 2);
  expect(treeSVG(root, "haus", new Map([["a", { status: "down" }]]), true)).toContain('href="/p/haus/e/a"');
});

test("rack and floor plan sources", () => {
  expect(parseRack("@rack-keller").model?.rack).toBe("rack-keller");
  expect(parseRack("rack: rack-keller\ntitle: Keller").model).toEqual({ rack: "rack-keller", title: "Keller" });
  expect(parseRack("").problems[0].key).toBe("rack.err_missing");
  expect(parseRack("Rack im Keller").problems[0].key).toBe("rack.err_line");
  expect(parsePlanBlock("plan: erdgeschoss\nroom: Küche\ndevices: nein").model).toEqual({ plan: "erdgeschoss", room: "Küche", title: "", devices: false });
  expect(parsePlanBlock("erdgeschoss").model?.plan).toBe("erdgeschoss");
  expect(parsePlanBlock("room: Küche").problems[0].key).toBe("floorplan.err_plan");
  const plan = normalizePlan({
    width: 10000, height: 8000,
    rooms: [{ id: "k", name: "Küche", points: [[0, 0], [4000, 0], [4000, 4000], [0, 4000]] }, { id: "b", name: "Bad", entity: "bad", points: [[4000, 0], [8000, 0], [8000, 4000], [4000, 4000]] }],
    devices: [{ id: "d1", entity: "herd", x: 1000, y: 1000 }, { id: "d2", entity: "waschmaschine", x: 5000, y: 1000 }],
    walls: [{ id: "w", a: [0, 0], b: [8000, 0], thickness: 120 }], openings: [{ id: "o", wall: "w", kind: "window", offset: 2000, width: 1200 }],
  });
  const kitchen = findRoom(plan, "küche")!;
  expect(kitchen.id).toBe("k");
  expect(findRoom(plan, "@bad")!.id).toBe("b");
  expect(planDevices(plan, kitchen).map((d) => d.entity)).toEqual(["herd"]);
  const svg = planSVG(plan, { room: kitchen, project: "haus", interactive: true, label: "Küche" });
  expect(svg).toContain('href="/p/haus/e/herd"');
  expect(svg).not.toContain("waschmaschine");
  for (const k of ["tree", "rack", "floorplan"]) {
    const d = blockDef(k)!;
    const { model } = d.parse(d.example);
    expect(model).toBeTruthy();
    expect(d.svg(model, { width: 400, locale: "de" })).toStartWith("<svg");
  }
});
