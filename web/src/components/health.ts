import { LitElement, css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import "./monitoring";
import "./insights";

type Tab = "checks" | "insights";

/** Health of a project (ADR-0076): monitoring (checks, incidents, status
 * page) and insights (changes, documentation gaps) as two tabs. */
export class PhxHealth extends LitElement {
  static properties = { project: {}, tab: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    [role="tablist"] { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin: 6px 0 18px; overflow-x: auto; }
    [role="tab"] { all: unset; display: inline-flex; gap: 7px; align-items: center; padding: 8px 12px; border-bottom: 2px solid transparent; margin-bottom: -1px;
      color: var(--muted); font-size: 14.5px; font-weight: 500; cursor: pointer; white-space: nowrap; }
    [role="tab"][aria-selected="true"] { color: var(--fg); border-bottom-color: var(--accent); }
    [role="tab"]:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; border-radius: 6px; }
    @media (hover: hover) and (pointer: fine) { [role="tab"]:hover { color: var(--fg); } }
  `];

  project = "";
  tab: Tab = new URLSearchParams(location.search).get("tab") === "insights" ? "insights" : "checks";

  private pick(tab: Tab) {
    this.tab = tab;
    const u = new URL(location.href);
    u.searchParams.set("tab", tab);
    history.replaceState(history.state, "", u);
  }

  private key(e: KeyboardEvent) {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    this.pick(this.tab === "checks" ? "insights" : "checks");
    this.renderRoot.querySelector<HTMLElement>('[aria-selected="true"]')?.focus();
  }

  render() {
    const tab = (id: Tab, ic: string, label: string) => html`<button role="tab" id="tab-${id}" aria-controls="panel" aria-selected=${this.tab === id ? "true" : "false"}
      tabindex=${this.tab === id ? 0 : -1} @click=${() => this.pick(id)}>${icon(ic, 15)} ${label}</button>`;
    return html`<h1>${t("health.title")}</h1>
      <div role="tablist" aria-label=${t("health.title")} @keydown=${this.key}>
        ${tab("checks", "Activity", t("health.tab_checks"))}${tab("insights", "Sparkles", t("health.tab_insights"))}</div>
      <div id="panel" role="tabpanel" aria-labelledby="tab-${this.tab}">
        ${this.tab === "checks" ? html`<phx-monitoring .project=${this.project} embedded></phx-monitoring>` : html`<phx-insights .project=${this.project} embedded></phx-insights>`}
      </div>`;
  }
}
customElements.get("phx-health") || customElements.define("phx-health", PhxHealth);
