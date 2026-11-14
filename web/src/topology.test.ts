import { describe, expect, test } from "bun:test";
import { COL, ROW, layoutTopology, type TopoNode } from "./topology";

const n = (slug: string, kind = "device"): TopoNode => ({ slug, name: slug, kind, status: "unknown" });

describe("topology layout", () => {
  test("tree from the router down, parents centred", () => {
    const l = layoutTopology([n("r", "router"), n("s", "switch"), n("a"), n("b")],
      [{ from: "s", to: "r", rel: "wired_to" }, { from: "a", to: "s", rel: "wired_to" }, { from: "b", to: "s", rel: "wired_to" }]);
    const at = (s: string) => l.nodes.find((x) => x.slug === s)!;
    expect(at("r").depth).toBe(0);
    expect(at("a").depth).toBe(2);
    expect(at("s").x).toBe((at("a").x + at("b").x) / 2);
    expect(at("r").x).toBe(at("s").x);
    expect(l.height).toBe(3 * ROW);
    expect(l.width).toBe(2 * COL);
  });
  test("second parent is a cross link, cycles are broken", () => {
    const l = layoutTopology([n("r", "router"), n("x"), n("y")],
      [{ from: "x", to: "r", rel: "uplink" }, { from: "x", to: "y", rel: "wired_to" }, { from: "y", to: "x", rel: "wired_to" }]);
    expect(l.nodes.length).toBe(3);
    expect(l.edges.filter((e) => e.tree).length).toBe(2);
    expect(l.edges.find((e) => e.from === "x" && e.to === "y")?.tree).toBe(false);
  });
  test("gateways come first", () => {
    const l = layoutTopology([n("lonely"), n("r", "router")], []);
    expect(l.nodes.find((x) => x.slug === "r")!.x).toBeLessThan(l.nodes.find((x) => x.slug === "lonely")!.x);
  });
});
