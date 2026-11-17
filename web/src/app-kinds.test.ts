import { describe, expect, test } from "bun:test";
// @ts-expect-error plain JS of the built-in sheet app (resources/apps/sheet)
import { evaluate, parseRef } from "../../resources/apps/sheet/formula.js";

const sheet: Record<string, string | number> = { A1: 2, A2: 3, A3: "x", B1: "Hello" };
const get = (r: string) => sheet[r] ?? "";
const ev = (f: string) => evaluate(f, get);

describe("sheet formulas", () => {
  test("arithmetic and precedence", () => {
    expect(ev("1+2*3")).toBe(7);
    expect(ev("(1+2)*3")).toBe(9);
    expect(ev("2^3^1")).toBe(8);
    expect(ev("-A1+10")).toBe(8);
    expect(ev("A1/A2*3")).toBe(2);
  });
  test("functions and ranges", () => {
    expect(ev("SUM(A1:A3)")).toBe(5);
    expect(ev("average(A1:A2)")).toBe(2.5);
    expect(ev("COUNT(A1:A3)")).toBe(2);
    expect(ev("COUNTA(A1:A3)")).toBe(3);
    expect(ev("MAX(A1:A2, 10)")).toBe(10);
    expect(ev("ROUND(10/3, 2)")).toBe(3.33);
    expect(ev('IF(A1>1, "big", "small")')).toBe("big");
    expect(ev('B1&" "&A1')).toBe("Hello 2");
  });
  test("errors", () => {
    expect(() => ev("1/0")).toThrow("#DIV/0!");
    expect(() => ev("NOPE(1)")).toThrow("#NAME?");
    expect(() => ev("A3*2")).toThrow("#VALUE!");
    expect(() => ev("1+")).toThrow();
    expect(() => ev("alert(1)")).toThrow("#NAME?");
  });
  test("refs", () => {
    expect(parseRef("C12")).toEqual([2, 11]);
    expect(parseRef("A0")).toBeNull();
  });
});
