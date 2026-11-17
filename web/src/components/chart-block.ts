// <phx-chart>: a chart block (```chart in pages, chart elements on
// whiteboards). The source is the <pre> inside (pages), data-src (the editor)
// or the spec property (whiteboards). Draws with chartSVG and adds the
// legend, the numbered list of points and a tooltip for each point.

import { chartItems, chartSVG, parseChart, seriesColor, seriesOf, type ChartSpec } from "../chart";
import { locale, t, type Key } from "../i18n";

const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

const STYLE = `
:host { display: block; margin: 1em 0; color: var(--fg); font-family: var(--font-body, system-ui, sans-serif); position: relative; }
.card { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 16px 18px; }
:host([data-bare]) .card { border: 0; border-radius: 0; padding: 10px 12px; height: 100%; box-sizing: border-box; overflow: hidden; }
h3 { margin: 0 0 4px; font: 600 17px/1.3 var(--font-display, var(--font-body)); }
.desc { margin: 0 0 10px; color: var(--muted); font-size: 13px; line-height: 1.45; }
.plot { position: relative; }
.mk { cursor: pointer; outline: none; }
.mk:focus-visible circle, .mk:focus-visible rect { stroke: var(--accent); stroke-width: 3px; }
.legend { display: flex; flex-wrap: wrap; gap: 6px 16px; margin: 8px 0 0; padding: 0; list-style: none; font-size: 12px; color: var(--muted); }
.legend li { display: flex; align-items: center; gap: 6px; }
.sw { width: 12px; height: 12px; border-radius: 3px; flex: none; }
.ln { width: 16px; height: 3px; border-radius: 2px; flex: none; }
ol { margin: 14px 0 0; padding: 0; list-style: none; columns: 2 300px; column-gap: 28px; font-size: 13px; line-height: 1.45; }
ol li { display: flex; gap: 8px; align-items: flex-start; break-inside: avoid; margin-bottom: 9px; border-radius: 6px; }
ol li.hl { background: var(--accent-soft); }
.n { flex: none; min-width: 20px; height: 20px; padding: 0 4px; box-sizing: border-box; border-radius: 10px; display: grid; place-items: center; color: var(--surface); font-size: 10.5px; font-weight: 700; margin-top: 1px; }
.tip { position: absolute; z-index: 2; max-width: 280px; padding: 8px 10px; border-radius: 8px; background: var(--fg); color: var(--bg); font-size: 12.5px; line-height: 1.4; pointer-events: none; box-shadow: 0 4px 14px rgb(0 0 0 / .18); }
.tip b { display: block; }
.err { border: 1px solid var(--line); border-left: 3px solid var(--bad); border-radius: 8px; padding: 8px 12px; background: var(--surface); }
.err b { display: block; font-size: 13px; color: var(--bad); margin-bottom: 4px; }
.err pre { margin: 0; white-space: pre-wrap; font: 12px/1.45 var(--font-mono, monospace); color: var(--muted); }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
`;

export class PhxChart extends HTMLElement {
  /** Set by whiteboards instead of a source text. */
  spec?: ChartSpec;
  private ro?: ResizeObserver;
  private width = 0;
  private parsed?: ChartSpec;
  private src = "";

  static get observedAttributes() {
    return ["data-src"];
  }

  connectedCallback() {
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    root.addEventListener("pointerover", this.over);
    root.addEventListener("focusin", this.over);
    root.addEventListener("pointerout", this.out);
    root.addEventListener("focusout", this.out);
    this.ro = new ResizeObserver(() => {
      const w = Math.floor(this.getBoundingClientRect().width);
      if (w && Math.abs(w - this.width) > 2) {
        this.width = w;
        this.draw();
      }
    });
    this.ro.observe(this);
    this.update();
  }

  disconnectedCallback() {
    this.ro?.disconnect();
  }

  attributeChangedCallback() {
    if (this.isConnected) this.update();
  }

  /** Reads the source again (e.g. after the spec property changed). */
  update() {
    const root = this.shadowRoot!;
    if (this.spec) {
      this.parsed = this.spec;
    } else {
      this.src = this.dataset.src ?? this.querySelector("pre")?.textContent ?? "";
      const { spec, problems } = parseChart(this.src);
      if (!spec) {
        const p = problems[0];
        const detail = p ? [t(p.key as Key, p.params ?? {}), p.detail].filter(Boolean).join(" – ") : "";
        root.innerHTML = `<style>${STYLE}</style><div class="err" role="alert"><b>${esc(t("chart.error"))}</b>${esc(detail)}<pre>${esc(this.src)}</pre></div>`;
        this.parsed = undefined;
        return;
      }
      this.parsed = spec;
    }
    this.width = Math.floor(this.getBoundingClientRect().width) || this.width;
    this.draw();
  }

  private draw() {
    const spec = this.parsed, root = this.shadowRoot;
    if (!spec || !root) return;
    const bare = this.hasAttribute("data-bare");
    const inner = Math.max(240, (this.width || 640) - (bare ? 24 : 38));
    const items = chartItems(spec);
    const showList = spec.list ?? items.length > 0;
    const legend = [
      ...(spec.bands ?? []).filter((b) => b.label).map((b) => `<li><span class="sw" style="background:var(--tb-${b.color ?? "gray"});border:1px solid var(--tc-${b.color ?? "gray"})"></span>${esc(b.label)}</li>`),
      ...seriesOf(spec).map((s, i) => (s.name ? `<li><span class="ln" style="background:var(--tc-${seriesColor(spec, i)})"></span>${esc(s.name)}</li>` : "")).filter(Boolean),
    ];
    const height = bare ? Math.max(140, this.getBoundingClientRect().height - 24 - (spec.title ? 26 : 0) - (legend.length ? 30 : 0)) : undefined;
    const svg = chartSVG(spec, { width: inner, height, locale: locale() });
    const table = `<table class="sr"><caption>${esc(spec.title ?? "")}</caption>${seriesOf(spec).map((s) => s.points.map((p) => `<tr><th>${esc(p.x)}</th><td>${esc(p.y)}</td><td>${esc([p.title, p.text].filter(Boolean).join(": "))}</td></tr>`).join("")).join("")}</table>`;
    root.innerHTML = `<style>${STYLE}</style><div class="card">
      ${spec.title ? `<h3>${esc(spec.title)}</h3>` : ""}${spec.description && !bare ? `<p class="desc">${esc(spec.description)}</p>` : ""}
      <div class="plot">${svg}</div>
      ${legend.length ? `<ul class="legend">${legend.join("")}</ul>` : ""}
      ${showList && !bare ? `<ol>${items.map((i) => `<li data-k="${i.key}"><span class="n" style="background:var(--tc-${i.color})">${esc(i.n)}</span><span>${i.title ? `<b>${esc(i.title)}</b>${i.text ? ": " : ""}` : ""}${esc(i.text)}</span></li>`).join("")}</ol>` : ""}
      ${table}</div>`;
  }

  private over = (e: Event) => {
    const mk = (e.target as Element).closest?.(".mk") as SVGGElement | null;
    if (!mk || !this.parsed) return;
    const [si, pi] = (mk.dataset.k ?? "").split(":").map(Number);
    const p = seriesOf(this.parsed)[si]?.points[pi];
    if (!p) return;
    const root = this.shadowRoot!;
    root.querySelector(".tip")?.remove();
    root.querySelectorAll("ol li.hl").forEach((n) => n.classList.remove("hl"));
    root.querySelector(`ol li[data-k="${si}:${pi}"]`)?.classList.add("hl");
    const plot = root.querySelector<HTMLElement>(".plot")!;
    const tip = document.createElement("div");
    tip.className = "tip";
    tip.setAttribute("role", "tooltip");
    const head = [p.label, p.title].filter(Boolean).join(" · ");
    const s = seriesOf(this.parsed)[si];
    tip.innerHTML = head || p.text ? `${head ? `<b>${esc(head)}</b>` : ""}${esc(p.text ?? "")}` : `<b>${esc(s.name ?? "")}</b>${esc(p.x)}: ${esc(p.y)}`;
    plot.append(tip);
    const box = mk.getBoundingClientRect(), pb = plot.getBoundingClientRect();
    const left = Math.min(Math.max(0, box.left - pb.left + box.width / 2 - 140), Math.max(0, pb.width - 280));
    const above = box.top - pb.top > 70;
    tip.style.left = `${left}px`;
    tip.style.top = above ? "" : `${box.bottom - pb.top + 8}px`;
    if (above) tip.style.bottom = `${pb.bottom - box.top + 8}px`;
  };

  private out = (e: Event) => {
    if (!(e.target as Element).closest?.(".mk")) return;
    this.shadowRoot?.querySelector(".tip")?.remove();
    this.shadowRoot?.querySelectorAll("ol li.hl").forEach((n) => n.classList.remove("hl"));
  };
}
customElements.get("phx-chart") || customElements.define("phx-chart", PhxChart);
