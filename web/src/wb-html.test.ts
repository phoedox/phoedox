import { describe, expect, test } from "bun:test";
import { holds } from "./wb-html";

describe("data-show conditions of HTML screens", () => {
  test("var, !var and var=value", () => {
    const v = { on: true, off: false, n: 0, list: [], items: ["a"], mode: "dark" };
    expect(holds("on", v)).toBe(true);
    expect(holds("off", v)).toBe(false);
    expect(holds("!off", v)).toBe(true);
    expect(holds("n", v)).toBe(false);
    expect(holds("list", v)).toBe(false);
    expect(holds("items", v)).toBe(true);
    expect(holds("mode=dark", v)).toBe(true);
    expect(holds("!mode=dark", v)).toBe(false);
    expect(holds("", v)).toBe(true);
  });
});
