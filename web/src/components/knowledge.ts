import "./entity-table";
import "./graph-view";
import "./topology-view";
import "./ipam";
import "./locations";
import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import { base } from "./shared";

export type KnowledgeView = "entries" | "map" | "topology" | "ipam" | "locations";

/** Paths of the views below /p/<project>/ (the first is the landing view). */
export const knowledgePaths: Record<KnowledgeView, string> = {
  entries: "graph", map: "graph/map", topology: "topology", ipam: "ipam", locations: "locations",
};

const tabs: { view: KnowledgeView; icon: string; label: () => string }[] = [
  { view: "entries", icon: "Table", label: () => t("kg.entries") },
  { view: "map", icon: "Waypoints", label: () => t("kg.map") },
  { view: "topology", icon: "Network", label: () => t("topo.title") },
  { view: "ipam", icon: "Binary", label: () => t("ipam.title") },
  { view: "locations", icon: "MapPin", label: () => t("loc.title") },
];

/** The knowledge graph of a project: the entries and their connections,
 * as lists per kind, as a drawing and in special views. The rules behind
 * it (kinds, fields, allowed relations) live in the project settings. */
export class PhxKnowledge extends SignalWatcher(LitElement) {
  static properties = { project: {}, view: {} };
  static styles = [base, css`
    :host { display: block; }
    .head { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
    .head h1 { margin-right: auto; }
    nav.tabs { display: flex; gap: 2px; border-bottom: 1px solid var(--line); margin-bottom: 18px; overflow-x: auto; scrollbar-width: none; }
    nav.tabs a { display: inline-flex; align-items: center; gap: 7px; padding: 8px 12px; color: var(--muted); text-decoration: none; font-size: 14px;
      border-bottom: 2px solid transparent; margin-bottom: -1px; white-space: nowrap; transition: color var(--dur-fast) ease; }
    nav.tabs a[aria-current="page"] { color: var(--fg); border-bottom-color: var(--accent); font-weight: 600; }
    nav.tabs a:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { nav.tabs a:hover { color: var(--fg); } }
  `];
  project = "";
  view: KnowledgeView = "entries";

  private sub() {
    const p = this.project;
    switch (this.view) {
      case "map": return html`<phx-graph-view embedded .project=${p}></phx-graph-view>`;
      case "topology": return html`<phx-topology-view embedded .project=${p}></phx-topology-view>`;
      case "ipam": return html`<phx-ipam embedded .project=${p}></phx-ipam>`;
      case "locations": return html`<phx-locations embedded .project=${p}></phx-locations>`;
      default: return html`<phx-entity-table embedded .project=${p}></phx-entity-table>`;
    }
  }

  render() {
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`<div class="enter">
      <div class="head"><h1>${t("graph.title")}</h1>
        <a class="btn ghost" href="/p/${this.project}/settings/model" title=${t("kg.model_hint")}>${icon("Shapes", 14)} ${t("kg.model")}</a>
        ${canEdit ? html`<a class="btn" href="/p/${this.project}/new-entity">${icon("Plus", 14)} ${t("entity.new")}</a>` : ""}
      </div>
      <nav class="tabs" aria-label=${t("kg.views")}>
        ${tabs.map((x) => html`<a href="/p/${this.project}/${knowledgePaths[x.view]}" aria-current=${x.view === this.view ? "page" : "false"}>${icon(x.icon, 15)} ${x.label()}</a>`)}
      </nav>
      ${this.sub()}
    </div>`;
  }
}
customElements.define("phx-knowledge", PhxKnowledge);
