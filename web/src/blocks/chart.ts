// The chart block (ADR-0084) in the registry.
import { chartSVG, parseChart, type ChartSpec } from "../chart";
import { registerBlock } from "./registry";
import { CHART_EXAMPLE } from "../chart";

const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

/** A chart with its title and the legend of its phases, as one picture. */
export function chartPicture(spec: ChartSpec, width: number, height: number, locale: string): string {
  const top = spec.title ? 30 : 4;
  const bands = (spec.bands ?? []).filter((b) => b.label);
  const legend = bands.length ? 24 : 0;
  const plot = chartSVG(spec, { width, height: Math.max(140, height - top - legend), locale })
    .replace("<svg ", `<svg x="0" y="${top}" `).replace(/ style="[^"]*"/, "");
  let lx = 6;
  const items = bands.map((b) => {
    const c = b.color ?? "gray";
    const x = lx;
    lx += 16 + (b.label ?? "").length * 6.2 + 14;
    return x > width - 60 ? "" : `<rect x="${x}" y="${height - 18}" width="11" height="11" rx="2" style="fill:var(--tb-${c});stroke:var(--tc-${c})"/><text x="${x + 16}" y="${height - 8.5}" style="font-size:11px;fill:var(--muted)">${esc(b.label)}</text>`;
  }).join("");
  const title = spec.title ? `<text x="6" y="20" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(spec.title)}</text>` : "";
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)">${title}${plot}${items}</svg>`;
}

registerBlock<ChartSpec>({
  kind: "chart",
  icon: "ChartSpline",
  label: "slash.chart",
  hint: "block.chart_hint",
  example: CHART_EXAMPLE,
  parse: (src) => {
    const { spec, problems } = parseChart(src);
    return { model: spec, problems };
  },
  svg: (spec, size) => chartPicture(spec, size.width, size.height ?? 360, size.locale),
  async mount(host, spec) {
    await import("../components/chart-block");
    const el = document.createElement("phx-chart") as HTMLElement & { spec?: ChartSpec; update?: () => void };
    el.spec = spec;
    host.replaceChildren(el);
  },
  board: { w: 560, h: 360 },
});
