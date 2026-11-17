// Feedback topics about an entry (feedback-concept): shown on the entry's
// page to people who moderate one of the project's portals.

import { LitElement, css, html, nothing } from "lit";
import { api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

interface EntityTopic { id: string; title: string; state: string; open: boolean; priority: number; reports: number; portal: string }

export class PhxEntityFeedback extends LitElement {
  static properties = { project: {}, entity: {}, topics: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    h2 { font-size: 16px; }
    ul { list-style: none; padding: 0; margin: 6px 0 0; display: grid; gap: 4px; }
    li a { display: flex; gap: 8px; align-items: center; }
    li a.closed { color: var(--muted); }
    .p1 { color: var(--bad); }
    .muted { color: var(--muted); font-size: 12.5px; }
  `];
  project = "";
  entity = "";
  topics: EntityTopic[] = [];
  private loaded = "";

  willUpdate() {
    const key = `${this.project}/${this.entity}`;
    if (!this.project || !this.entity || key === this.loaded) return;
    this.loaded = key;
    void api<EntityTopic[]>("GET", `/projects/${this.project}/feedback/entities/${encodeURIComponent(this.entity)}`).then((l) => (this.topics = l ?? [])).catch(() => (this.topics = []));
  }

  render() {
    if (!this.topics.length) return nothing;
    return html`<h2 style="margin-top:12px">${t("fb.entity_topics")}</h2>
      <ul>${this.topics.map((x) => html`<li><a class=${x.open ? "" : "closed"} href="/p/${this.project}/feedback?topic=${x.id}">
        ${icon(x.open ? "MessageSquareWarning" : "CircleCheck", 14)}<span class=${x.priority === 1 && x.open ? "p1" : ""}>${x.title}</span>
        <span class="muted">${x.portal} · ${t("fb.reports_n", { n: x.reports })}</span></a></li>`)}</ul>`;
  }
}
customElements.get("phx-entity-feedback") || customElements.define("phx-entity-feedback", PhxEntityFeedback);
