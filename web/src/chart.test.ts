import { describe, expect, test } from "bun:test";
import { CHART_EXAMPLE, XScale, chartItems, chartSVG, parseChart, pointColor, smoothPath, xKind, xNum, xTicks, yScale, type ChartSpec } from "./chart";

// The example Jan sent: pressure of the movement on the old powers, 1815–1849,
// with a compressed Vormärz and stretched revolution years.
const vormaerz: ChartSpec = {
  title: "Pressure of the movement",
  x: { segments: [{ from: "1815", to: "1847-12", weight: 1, label: "compressed" }, { from: "1848-01", to: "1849-10", weight: 1.1, label: "stretched" }] },
  y: { label: "Strength", ticks: [{ value: 0, label: "low" }, { value: 5, label: "medium" }, { value: 10, label: "high" }] },
  bands: [
    { from: "1815", to: "1848-02", label: "Restoration & Vormärz", color: "orange" },
    { from: "1848-02", to: "1848-05", label: "March", color: "red" },
    { from: "1848-05", to: "1848-11", label: "Parliaments", color: "blue" },
    { from: "1848-11", to: "1849-10", label: "Counter-revolution", color: "gray" },
  ],
  series: [
    { points: [
      { x: "1815", y: 1, label: "1", title: "1815 – Congress of Vienna", text: "Restoration." },
      { x: "1832", y: 4, label: "7", title: "1832 – Hambach Festival" },
      { x: "1848-03", y: 9.5, label: "14", title: "March 1848", text: "Barricades." },
      { x: "1848-05-18", y: 9, label: "17", color: "blue", title: "Paulskirche" },
      { x: "1849-08", y: 0.5, label: "25", title: "Hungary capitulates" },
    ] },
    { dashed: true, points: [{ x: "1849-08", y: 0.5 }, { x: "1849-10", y: 2 }] },
  ],
  notes: [{ x: "1849-06", y: 2.5, text: "ideas live on" }],
};

describe("charts", () => {
  test("parsing finds the problems people make", () => {
    expect(parseChart("{").problems[0].key).toBe("chart.err_json");
    expect(parseChart('{"points":[]}').problems[0].key).toBe("chart.err_points");
    expect(parseChart('{"points":[{"x":"2026","y":"high"}]}').problems[0].key).toBe("chart.err_y");
    expect(parseChart('{"type":"pie","points":[{"x":1,"y":1}]}').problems[0].key).toBe("chart.err_type");
    expect(parseChart('{"points":[{"x":1,"y":1,"color":"teal"}]}').problems[0]).toEqual({ key: "chart.err_color", params: { color: "teal" } });
    expect(parseChart('{"x":{"type":"time"},"points":[{"x":"March","y":1}]}').problems[0].key).toBe("chart.err_x_value");
    expect(parseChart('{"x":{"segments":[{"from":"1849","to":"1848"}]},"points":[{"x":"1848","y":1}]}').problems[0].key).toBe("chart.err_segment");
    expect(parseChart(CHART_EXAMPLE).problems).toEqual([]);
    expect(parseChart(JSON.stringify(vormaerz)).problems).toEqual([]);
  });

  test("the x axis kind follows the values", () => {
    expect(xKind(vormaerz)).toBe("time");
    expect(xKind({ points: [{ x: 1, y: 2 }] })).toBe("number");
    expect(xKind({ type: "bar", points: [{ x: "NAS", y: 2 }] })).toBe("category");
    expect(new Date(xNum("1848-03-18", "time")!).toISOString().slice(0, 10)).toBe("1848-03-18");
    expect(new Date(xNum("0800", "time")!).getUTCFullYear()).toBe(800);
  });

  test("segments have their own scale: few years left, two years right", () => {
    const sc = new XScale("time", vormaerz, 0, 1000);
    expect(sc.segs.length).toBe(2);
    expect(Math.round(sc.segs[0].x1)).toBe(476); // weight 1 : 1.1
    expect(sc.at("1815")).toBe(0);
    expect(sc.at("1849-10")).toBeCloseTo(1000);
    // A month in 1848 is far wider than a month in 1830.
    const wide = sc.at("1848-06") - sc.at("1848-05"), narrow = sc.at("1830-06") - sc.at("1830-05");
    expect(wide).toBeGreaterThan(narrow * 10);
    const ticks = xTicks(sc, "de");
    expect(ticks.some((t) => t.label === "1820")).toBe(true);
    expect(ticks.some((t) => /^Jan\.? 48$/.test(t.label))).toBe(true);
    expect(ticks.some((t) => /^(Mär|Apr|Mai|Juni?|Juli?)/.test(t.label))).toBe(true);
    for (let i = 1; i < ticks.length; i++) expect(ticks[i].x).toBeGreaterThan(ticks[i - 1].x);
  });

  test("y ticks can be words", () => {
    expect(yScale(vormaerz).ticks.map((t) => t.label)).toEqual(["low", "medium", "high"]);
    const auto = yScale({ points: [{ x: 1, y: 3 }, { x: 2, y: 47 }] });
    expect(auto.min).toBe(0);
    expect(auto.max).toBeGreaterThanOrEqual(47);
  });

  test("the smooth curve passes through its points without overshooting", () => {
    const d = smoothPath([[0, 10], [10, 0], [20, 0], [30, 10]]);
    const ys = [...d.matchAll(/(-?[\d.]+),(-?[\d.]+)/g)].map((m) => Number(m[2]));
    expect(Math.min(...ys)).toBeGreaterThanOrEqual(0); // the flat part stays flat
    expect(d.startsWith("M0,10")).toBe(true);
    expect(d.endsWith("30,10")).toBe(true);
  });

  test("points take the colour of their phase; the list is numbered by label", () => {
    expect(pointColor(vormaerz, 0, vormaerz.series![0].points[0])).toBe("orange");
    expect(pointColor(vormaerz, 0, vormaerz.series![0].points[2])).toBe("red");
    expect(pointColor(vormaerz, 0, vormaerz.series![0].points[3])).toBe("blue");
    const items = chartItems(vormaerz);
    expect(items.map((i) => i.n)).toEqual(["1", "7", "14", "17", "25"]);
    expect(items[4].color).toBe("gray");
  });

  test("the SVG has bands, markers with their text, notes and no raw input", () => {
    const svg = chartSVG({ ...vormaerz, title: "<script>" }, { width: 700, locale: "de" });
    expect(svg).toContain("var(--tb-red)");
    expect(svg).toContain(">14</text>");
    expect(svg).toContain("stroke-dasharray:6 5");
    expect(svg).toContain("ideas live on");
    expect(svg).toContain("Restoration &amp; Vormärz");
    expect(svg).toContain('data-k="0:2"');
    expect(svg).not.toContain("<script>");
    const bars = chartSVG({ type: "bar", series: [{ name: "GB", points: [{ x: "NAS", y: 4 }, { x: "PC", y: 2 }] }] }, { width: 400 });
    expect((bars.match(/<rect/g) ?? []).length).toBe(2);
  });
});
