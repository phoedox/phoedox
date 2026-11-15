// Deleted projects the person can restore (ADR-0071), below the project
// list; hidden while there are none.

import { LitElement, css, html } from "lit";
import { ApiError, api, type Project } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Trashed extends Project { deletedAt: string; purgeAt?: string }

export class PhxTrashedProjects extends LitElement {
  static properties = { list: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-top: 26px; }
    summary { cursor: pointer; font-weight: 600; display: inline-flex; gap: 6px; align-items: center; color: var(--muted); }
    ul { list-style: none; padding: 0; margin: 10px 0 0; display: grid; gap: 8px; max-width: 760px; }
    li { display: flex; gap: 12px; align-items: center; justify-content: space-between; flex-wrap: wrap; padding: 10px 14px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
    li b { display: block; }
    li span { color: var(--muted); font-size: 13px; }
  `];
  list: Trashed[] = [];
  busy = "";

  async connectedCallback() {
    super.connectedCallback();
    this.list = (await api<Trashed[]>("GET", "/trashed-projects").catch(() => [])) ?? [];
  }

  private async restore(p: Trashed) {
    this.busy = p.id;
    try {
      const r = await api<Project>("POST", `/trashed-projects/${p.id}/restore`);
      toast(t("ptrash.restored", { name: r.name }));
      navigate(`/p/${r.slug}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = "";
    }
  }

  render() {
    if (!this.list.length) return "";
    return html`<details>
      <summary>${icon("Trash2", 14)} ${t("ptrash.title", { count: this.list.length })}</summary>
      <ul>${this.list.map((p) => html`<li><div><b>${p.name}</b>
        <span>${t("ptrash.deleted", { date: formatDate(p.deletedAt) })}${p.purgeAt ? html` · ${t("ptrash.purge", { date: formatDate(p.purgeAt) })}` : ""}</span></div>
        <button class="ghost" ?disabled=${this.busy === p.id} @click=${() => this.restore(p)}>${icon("RotateCcw", 14)} ${t("ptrash.restore")}</button></li>`)}</ul>
    </details>`;
  }
}
customElements.define("phx-trashed-projects", PhxTrashedProjects);
