import { LitElement, css, html } from "lit";
import { api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { findKind, loadModel, type Model } from "../model";
import { base } from "./shared";

interface Node { slug: string; name: string; kind: string; version?: string; state?: string; via?: string; update?: boolean; children?: Node[] }

const DOWN = ["exited", "stopped", "dead", "offline"];

/** What runs on a host: software, VMs, stacks and containers as a tree. */
export class PhxSoftwareTree extends LitElement {
  static properties = { project: {}, entity: {}, tree: { state: true }, model: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    :host([hidden]) { display: none; }
    h2 { font-size: 16px; margin: 0 0 8px; }
    ul { list-style: none; margin: 0; padding: 0 0 0 18px; border-left: 1px solid var(--line); }
    ul.root { padding-left: 0; border-left: 0; }
    li { padding: 3px 0; }
    .row { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    .row .ico { color: var(--muted); flex: none; }
    .ver { font-family: var(--font-mono); font-size: 12px; color: var(--muted); }
    .st { font-size: 12px; color: var(--good); } .st.down { color: var(--bad); }
    .upd { font-size: 12px; color: var(--accent); display: inline-flex; gap: 3px; align-items: center; }
    details > summary { list-style: none; cursor: pointer; } details > summary::-webkit-details-marker { display: none; }
    details > summary .chev { transition: transform 150ms var(--ease-out); }
    details[open] > summary .chev { transform: rotate(90deg); }
    @media (prefers-reduced-motion: reduce) { details > summary .chev { transition: none; } }
  `];
  project = "";
  entity = "";
  tree?: Node;
  model?: Model;
  private loaded = "";

  willUpdate() {
    const key = `${this.project}/${this.entity}`;
    if (key === this.loaded) return;
    this.loaded = key;
    this.hidden = true;
    void Promise.all([api<Node>("GET", `/projects/${this.project}/entities/${this.entity}/tree`), loadModel(this.project)]).then(([tree, model]) => {
      this.tree = tree;
      this.model = model;
      this.hidden = !tree.children?.length;
    }).catch(() => undefined);
  }

  private row(n: Node) {
    const k = findKind(this.model, n.kind);
    return html`<span class="row">${icon(k?.icon ?? "Box", 14)}<a href="/p/${this.project}/e/${n.slug}">${n.name}</a>
      ${n.version ? html`<span class="ver">${n.version}</span>` : ""}
      ${n.state ? html`<span class="st ${DOWN.includes(n.state) ? "down" : ""}">${n.state}</span>` : ""}
      ${n.update ? html`<span class="upd" title=${t("tree.update_hint")}>${icon("CircleArrowUp", 12)} ${t("tree.update")}</span>` : ""}</span>`;
  }

  private node(n: Node): unknown {
    if (!n.children?.length) return html`<li>${this.row(n)}</li>`;
    return html`<li><details open><summary class="row"><span class="chev">${icon("ChevronRight", 12)}</span>${this.row(n)}</summary>
      <ul>${n.children.map((c) => this.node(c))}</ul></details></li>`;
  }

  render() {
    if (!this.tree?.children?.length) return html``;
    return html`<h2>${t("tree.title")}</h2><ul class="root">${this.tree.children.map((c) => this.node(c))}</ul>`;
  }
}
customElements.define("phx-software-tree", PhxSoftwareTree);
