import { LitElement, css, html } from "lit";
import { api } from "../api";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { recentHref, recentIcon, type RecentItem } from "../recent";
import { currentProject } from "../state";
import { base } from "./shared";

/** Personal history: pages and entries opened recently, by day. */
export class PhxRecentHistory extends LitElement {
  static properties = { items: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px 60px; }
    h2 { font-size: 13px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); margin: 22px 0 6px; }
    ul { list-style: none; margin: 0; padding: 0; }
    li { display: flex; gap: 10px; align-items: center; padding: 7px 4px; border-bottom: 1px solid var(--line); }
    li a.title { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg); text-decoration: none; }
    @media (hover: hover) and (pointer: fine) { li a.title:hover { color: var(--accent); } }
    li .proj, li time { color: var(--muted); font-size: 13px; white-space: nowrap; }
    li button { padding: 2px 6px; }
    header { display: flex; align-items: center; gap: 10px; }
    header h1 { margin-right: auto; }
  `];
  items?: RecentItem[];

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    await this.load();
  }

  private async load() {
    this.items = (await api<RecentItem[]>("GET", "/me/recent?limit=200").catch(() => [])) ?? [];
  }

  private async forget(id?: string) {
    if (!id && !confirm(t("history_page.clear_confirm"))) return;
    await api("DELETE", `/me/recent${id ? `?ref=${encodeURIComponent(id)}` : ""}`).catch(() => {});
    await this.load();
  }

  render() {
    const items = this.items;
    const day = (iso: string) => new Date(iso).toLocaleDateString(locale() === "de" ? "de-DE" : "en-GB", { weekday: "long", day: "numeric", month: "long" });
    const groups: [string, RecentItem[]][] = [];
    for (const it of items ?? []) {
      const d = day(it.viewedAt);
      if (groups.at(-1)?.[0] === d) groups.at(-1)![1].push(it);
      else groups.push([d, [it]]);
    }
    return html`<div class="enter">
      <header><h1>${t("history_page.title")}</h1>
        ${items?.length ? html`<button class="ghost" @click=${() => this.forget()}>${icon("Trash2", 14)} ${t("history_page.clear")}</button>` : ""}</header>
      <p class="muted">${t("history_page.intro")}</p>
      ${items === undefined ? html`<p class="muted">${t("app.loading")}</p>` : !items.length ? html`<p class="muted">${t("history_page.empty")}</p>` : ""}
      ${groups.map(([d, list]) => html`<h2>${d}</h2><ul>${list.map((it) => html`<li>
        ${icon(recentIcon(it), 15)}
        <a class="title" href=${recentHref(it)}>${it.title}</a>
        <span class="proj">${it.projectName}</span>
        <time datetime=${it.viewedAt} title=${formatDate(it.viewedAt)}>${new Date(it.viewedAt).toLocaleTimeString(locale() === "de" ? "de-DE" : "en-GB", { hour: "2-digit", minute: "2-digit" })}</time>
        <button class="ghost" aria-label=${t("history_page.forget", { title: it.title })} title=${t("history_page.forget", { title: it.title })} @click=${() => this.forget(it.id)}>${icon("X", 13)}</button>
      </li>`)}</ul>`)}
    </div>`;
  }
}
customElements.define("phx-recent-history", PhxRecentHistory);
