import { describe, expect, test } from "bun:test";
import { diffLines, fold } from "./diff";

const apply = (a: string[], d: ReturnType<typeof diffLines>) => {
  // Reconstruct both sides from the diff.
  const left = d.filter((l) => l.op !== "+").map((l) => l.text);
  const right = d.filter((l) => l.op !== "-").map((l) => l.text);
  return { left, right };
};

describe("diff", () => {
  test("finds minimal line changes", () => {
    const a = ["# Netz", "Router im Keller", "NAS im Flur", "Ende"];
    const b = ["# Netz", "Router im Flur", "NAS im Flur", "Switch", "Ende"];
    const d = diffLines(a, b);
    expect(d.filter((l) => l.op === "-").map((l) => l.text)).toEqual(["Router im Keller"]);
    expect(d.filter((l) => l.op === "+").map((l) => l.text)).toEqual(["Router im Flur", "Switch"]);
    const { left, right } = apply(a, d);
    expect(left).toEqual(a);
    expect(right).toEqual(b);
  });
  test("edge cases", () => {
    expect(diffLines([], [])).toEqual([]);
    expect(diffLines([], ["a"])).toEqual([{ op: "+", text: "a" }]);
    expect(diffLines(["a"], [])).toEqual([{ op: "-", text: "a" }]);
    expect(diffLines(["a", "b"], ["a", "b"]).every((l) => l.op === " ")).toBe(true);
  });
  test("random texts reconstruct", () => {
    for (let r = 0; r < 50; r++) {
      const rnd = () => Array.from({ length: Math.floor(Math.random() * 30) }, () => "l" + Math.floor(Math.random() * 6));
      const a = rnd();
      const b = rnd();
      const { left, right } = apply(a, diffLines(a, b));
      expect(left).toEqual(a);
      expect(right).toEqual(b);
    }
  });
  test("folds unchanged runs", () => {
    const a = Array.from({ length: 20 }, (_, i) => `z${i}`);
    const b = [...a];
    b[10] = "neu";
    const h = fold(diffLines(a, b), 2);
    expect(h[0]).toEqual({ op: "…", count: 8 });
    expect(h.filter((x) => x.op === "+" || x.op === "-").length).toBe(2);
    expect(h.at(-1)).toEqual({ op: "…", count: 7 });
  });
});
