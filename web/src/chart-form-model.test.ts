import { describe, expect, test } from "bun:test";
import { parseChart } from "./chart";
import { chartSource, editableSeries, numFrom, tidy, withSeries, xFrom, yFrom } from "./chart-form-model";

describe("chart editor model", () => {
  test("short form points edit as one series and stay short", () => {
    const spec = { title: "T", points: [{ x: "2026-01", y: 1 }] };
    const s = editableSeries(spec);
    expect(s).toHaveLength(1);
    const back = withSeries(spec, [{ points: [...s[0].points, { x: "2026-02", y: 2 }] }]);
    expect(back.points).toHaveLength(2);
    expect(back.series).toBeUndefined();
  });

  test("a second series or a named one switches to series", () => {
    const spec = { points: [{ x: 1, y: 1 }] };
    const two = withSeries(spec, [{ points: spec.points }, { name: "B", points: [{ x: 1, y: 2 }] }]);
    expect(two.series).toHaveLength(2);
    expect(two.points).toBeUndefined();
    expect(withSeries(spec, [{ name: "A", points: spec.points }]).series?.[0].name).toBe("A");
  });

  test("typed values", () => {
    expect(xFrom("2026-03", { points: [{ x: "2026-01", y: 1 }] })).toBe("2026-03");
    expect(xFrom("5", { x: { type: "number" }, points: [] })).toBe(5);
    expect(xFrom("Mon", { points: [{ x: "Sun", y: 1 }] })).toBe("Mon");
    expect(yFrom("2,5")).toBe(2.5);
    expect(yFrom("")).toBeNull();
    expect(numFrom("")).toBeUndefined();
    expect(numFrom("10")).toBe(10);
  });

  test("tidy leaves out empty values and the source parses again", () => {
    expect(tidy<Record<string, unknown>>({ a: "", b: undefined, c: [], d: {}, e: 0, f: { g: "" }, list: false })).toEqual({ e: 0, list: false });
    const src = chartSource({ title: "", type: "line", y: { label: "", unit: "%" }, points: [{ x: "2026-01", y: 1, title: "" }] });
    expect(JSON.parse(src)).toEqual({ type: "line", y: { unit: "%" }, points: [{ x: "2026-01", y: 1 }] });
    expect(parseChart(src).problems).toEqual([]);
  });
});
