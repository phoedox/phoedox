import { LitElement, css, html } from "lit";
import { ApiError, api, type Suggestion } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { docsVersion } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

export class PhxSuggestions extends LitElement {
  static properties = { project: {}, items: { state: true }, leaving: { state: true } };
  static styles = [base, css`
    ul { list-style: none; padding: 0; margin: 16px 0 0; display: flex; flex-direction: column; gap: 8px; max-width: 760px; }
    /* State indication: a decided suggestion shrinks slightly and fades out. */
    li { display: flex; gap: 12px; align-items: center; justify-content: space-between; flex-wrap: wrap;
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out); }
    li.leaving { opacity: 0; transform: scale(0.97); }
    @media (prefers-reduced-motion: reduce) { li.leaving { transform: none; } }
  `];
  project = "";
  items: Suggestion[] = [];
  leaving = new Set<string>();

  async connectedCallback() {
    super.connectedCallback();
    this.items = (await api<Suggestion[]>("GET", `/projects/${this.project}/suggestions?status=pending`).catch(() => [])) ?? [];
  }

  private async decide(s: Suggestion, accept: boolean) {
    try {
      await api("POST", `/suggestions/${s.id}/${accept ? "accept" : "reject"}`);
      this.leaving = new Set([...this.leaving, s.id]);
      setTimeout(() => (this.items = this.items.filter((x) => x.id !== s.id)), 210);
      if (accept) docsVersion.set(docsVersion.get() + 1);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    return html`<div class="enter">
      <h1>${t("sugg.title")}</h1>
      ${this.items.length === 0 ? html`<p class="muted">${t("sugg.empty")}</p>` : ""}
      <ul>${this.items.map((s) => html`<li class="card ${this.leaving.has(s.id) ? "leaving" : ""}">
        <span><b>${s.summary}</b><br /><span class="muted" style="font-size:13px">${t("sugg.by", { actor: s.actor.startsWith("mcp:") ? "MCP" : s.actor })} · ${formatDate(s.createdAt)}</span></span>
        <span class="row">
          <button @click=${() => this.decide(s, true)}>${icon("Check", 14)} ${t("sugg.accept")}</button>
          <button class="ghost" @click=${() => this.decide(s, false)}>${icon("X", 14)} ${t("sugg.reject")}</button>
        </span>
      </li>`)}</ul>
    </div>`;
  }
}
customElements.define("phx-suggestions", PhxSuggestions);
