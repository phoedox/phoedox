import { LitElement, css, html } from "lit";
import { api, type Task } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import { base } from "./shared";

interface MyTask extends Task { projectSlug: string; projectName: string }

/** Open tasks assigned to me in all projects, due ones first. */
export class PhxMyTasks extends LitElement {
  static properties = { tasks: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 860px; margin: 0 auto; padding: 28px 16px; }
    h1 { display: flex; gap: 10px; align-items: center; }
    ul { list-style: none; padding: 0; margin: 16px 0 0; display: grid; gap: 8px; }
    a.card { display: grid; grid-template-columns: auto 1fr auto; gap: 4px 12px; align-items: baseline; padding: 12px 14px; color: var(--fg); text-decoration: none;
      transition: border-color var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { a.card:hover { border-color: var(--muted); } }
    a.card .ico { color: var(--muted); align-self: center; }
    .title { font-weight: 600; }
    .where { grid-column: 2 / 4; font-size: 13px; color: var(--muted); }
    .due { font-size: 13px; color: var(--muted); white-space: nowrap; display: inline-flex; gap: 4px; align-items: center; }
    .due.over { color: var(--bad); font-weight: 600; }
    .empty { margin-top: 16px; }
  `];
  tasks?: MyTask[];

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    this.tasks = (await api<MyTask[]>("GET", "/me/tasks").catch(() => [])) ?? [];
  }

  render() {
    const today = new Date().toISOString().slice(0, 10);
    return html`<div class="enter">
      <h1>${icon("ListChecks", 22)} ${t("mytasks.title")}</h1>
      <p class="muted">${t("mytasks.hint")}</p>
      ${!this.tasks ? html`<p class="muted">${t("app.loading")}</p>`
        : !this.tasks.length ? html`<p class="muted empty">${t("mytasks.empty")}</p>`
        : html`<ul>${this.tasks.map((x) => html`<li><a class="card" href="/p/${x.projectSlug}/d/${x.boardSlug}?task=${x.id}">
            ${icon("SquareCheckBig", 16)}<span class="title">${x.title}</span>
            ${x.due ? html`<span class="due ${x.due < today ? "over" : ""}">${icon("CalendarClock", 13)} ${formatDay(x.due)}</span>` : html`<span></span>`}
            <span class="where">${x.projectName} · ${x.boardTitle}</span></a></li>`)}</ul>`}
    </div>`;
  }
}
customElements.define("phx-my-tasks", PhxMyTasks);
