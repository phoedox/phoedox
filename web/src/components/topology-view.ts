import { LitElement, css, html, svg } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { findKind, groupColor, kindName, loadModel, relLabel, type Model } from "../model";
import { layoutTopology, type Layout, type TopoEdge, type TopoNode } from "../topology";
import { navigate } from "../state";
import { base } from "./shared";

/** The network plan, drawn from uplink, wired_to and runs_on. */
export class PhxTopologyView extends LitElement {
  static properties = { project: {}, layout: { state: true }, model: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .wrap { overflow: auto; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); max-height: calc(100dvh - 220px); }
    svg { display: block; margin: 0 auto; }
    line { stroke: var(--muted); stroke-width: 1.5; }
    line.uplink { stroke-dasharray: 6 5; }
    line.runs_on, line.part_of { stroke-dasharray: 2 4; }
    line.cross { stroke: var(--line); }
    .n { cursor: pointer; }
    .n circle.bg { fill: var(--surface); stroke: var(--c); stroke-width: 1.5; }
    .n .ico { color: var(--c); }
    .n text { font-size: 12px; fill: var(--fg); text-anchor: middle; paint-order: stroke; stroke: var(--surface); stroke-width: 4px; }
    .n text.ip { fill: var(--muted); font-family: var(--font-mono); font-size: 11px; }
    .dot { stroke: var(--surface); stroke-width: 2; }
    .dot.ok { fill: var(--good); } .dot.warn { fill: var(--signal); } .dot.down { fill: var(--bad); } .dot.unknown { fill: var(--line); }
    @media (hover: hover) and (pointer: fine) { .n:hover circle.bg { stroke-width: 2.5; } }
    .n:focus { outline: none; } .n:focus-visible circle.bg { stroke: var(--accent); stroke-width: 3; }
    .legend { display: flex; gap: 16px; flex-wrap: wrap; font-size: 13px; color: var(--muted); margin: 10px 0; align-items: center; }
    .legend svg { margin: 0; }
    ul.list { columns: 2; font-size: 14px; }
    @media (max-width: 700px) { ul.list { columns: 1; } }
  `];
  project = "";
  layout?: Layout;
  model?: Model;
  error = "";

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    try {
      const [topo, model] = await Promise.all([api<{ nodes: TopoNode[]; edges: TopoEdge[] }>("GET", `/projects/${this.project}/topology`), loadModel(this.project)]);
      this.layout = layoutTopology(topo.nodes, topo.edges);
      this.model = model;
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private open(slug: string) {
    navigate(`/p/${this.project}/e/${slug}`);
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const l = this.layout;
    if (!l) return html`<p class="muted">${t("app.loading")}</p>`;
    if (!l.edges.length) return html`<h1>${t("topo.title")}</h1><p class="muted">${t("topo.empty")}</p>`;
    const at = new Map(l.nodes.map((n) => [n.slug, n]));
    const pad = 20;
    const legendLine = (cls: string) => svg`<svg width="34" height="8"><line class=${cls} x1="2" y1="4" x2="32" y2="4"/></svg>`;
    return html`<h1>${t("topo.title")}</h1>
      <p class="muted">${t("topo.intro")}</p>
      <div class="legend">
        <span>${legendLine("wired_to")} ${relLabel("wired_to")}</span><span>${legendLine("uplink")} ${relLabel("uplink")}</span><span>${legendLine("runs_on")} ${relLabel("runs_on")}</span>
        <span>${t("topo.status_hint")}</span></div>
      <div class="wrap">
        <svg width=${l.width + 2 * pad} height=${l.height + pad} viewBox="${-pad} 0 ${l.width + 2 * pad} ${l.height + pad}" role="img" aria-label=${t("topo.title")}>
          ${l.edges.map((e) => {
            const a = at.get(e.from), b = at.get(e.to);
            return a && b ? svg`<line class="${e.rel} ${e.tree ? "" : "cross"}" x1=${a.x} y1=${a.y} x2=${b.x} y2=${b.y}/>` : "";
          })}
          ${l.nodes.map((n) => {
            const k = findKind(this.model, n.kind);
            return svg`<g class="n" style="--c:${groupColor[k?.group ?? "custom"]}" transform="translate(${n.x} ${n.y})" tabindex="0" role="link"
              aria-label="${n.name}, ${kindName(k, n.kind)}" @click=${() => this.open(n.slug)} @keydown=${(ev: KeyboardEvent) => ev.key === "Enter" && this.open(n.slug)}>
              <title>${n.name} · ${kindName(k, n.kind)}${n.ip ? ` · ${n.ip}` : ""}</title>
              <circle class="bg" r="22"/>
              <g transform="translate(-11 -11)">${icon(k?.icon ?? "Box", 22)}</g>
              <circle class="dot ${n.status}" cx="16" cy="-16" r="6"/>
              <text y="40">${n.name.length > 20 ? n.name.slice(0, 19) + "…" : n.name}</text>
              ${n.ip ? svg`<text class="ip" y="55">${n.ip}</text>` : ""}</g>`;
          })}
        </svg>
      </div>
      <h2 style="font-size:16px;margin-top:20px">${t("topo.list")}</h2>
      <ul class="list">${l.nodes.map((n) => html`<li><a href="/p/${this.project}/e/${n.slug}">${n.name}</a> <span class="muted">${kindName(findKind(this.model, n.kind), n.kind)} · ${t(`topo.status.${n.status}` as any)}</span></li>`)}</ul>`;
  }
}
customElements.define("phx-topology-view", PhxTopologyView);
