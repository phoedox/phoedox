// Time logged on a card (jobs with time tracking): the entries and a short
// form to add one. Durations like 1:30, 45m or 1.5h.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Entry { id: string; userId: string; userName?: string; day: string; minutes: number; note?: string; billable: boolean }

/** 95 → "1:35 h". */
export const hm = (min: number) => `${Math.floor(min / 60)}:${String(min % 60).padStart(2, "0")} h`;

export class PhxTaskTime extends LitElement {
  static properties = { project: {}, task: {}, canEdit: { type: Boolean }, owner: { type: Boolean }, list: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    fieldset { border: 0; padding: 0; margin: 0; }
    legend { font-size: 13px; font-weight: 600; color: var(--muted); margin-bottom: 4px; }
    ul { list-style: none; padding: 0; margin: 0 0 6px; display: grid; gap: 2px; font-size: 13.5px; }
    li { display: grid; grid-template-columns: 70px 90px minmax(0, 1fr) auto; gap: 8px; align-items: center; padding: 3px 0; border-bottom: 1px solid var(--line); }
    li .n { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    form { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
    form input { width: auto; height: 32px; }
    form input[name=duration] { width: 90px; }
    form input[name=note] { flex: 1; min-width: 140px; }
    .tot { font-variant-numeric: tabular-nums; }
  `];
  project = "";
  task = "";
  canEdit = false;
  owner = false;
  list?: Entry[];
  busy = false;
  private loaded = "";

  willUpdate() {
    if (this.task && this.loaded !== this.task) {
      this.loaded = this.task;
      void this.load();
    }
  }

  private async load() {
    this.list = (await api<Entry[]>("GET", `/projects/${this.project}/tasks/${this.task}/time`).catch(() => [])) ?? [];
    this.dispatchEvent(new CustomEvent("changed", { detail: this.list.reduce((s, e) => s + e.minutes, 0), bubbles: false }));
  }

  private async add(e: SubmitEvent) {
    e.preventDefault();
    e.stopPropagation();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.busy = true;
    try {
      await api("POST", `/projects/${this.project}/tasks/${this.task}/time`, { duration: f.get("duration"), day: f.get("day") || undefined, note: f.get("note") || "" });
      form.reset();
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeEntry(id: string) {
    await api("DELETE", `/projects/${this.project}/time/${id}`).catch((err) => toast(err instanceof ApiError ? err.text : t("error.unknown"), "error"));
    await this.load();
  }

  render() {
    const list = this.list ?? [];
    const total = list.reduce((s, e) => s + e.minutes, 0);
    if (!this.canEdit && !list.length) return nothing;
    const me = currentUser.get()?.id;
    return html`<fieldset><legend>${icon("Timer", 13)} ${t("time.title")}${total ? html` · <span class="tot">${hm(total)}</span>` : nothing}</legend>
      ${list.length ? html`<ul>${list.map((e) => html`<li><span>${formatDay(e.day)}</span><span class="tot">${hm(e.minutes)}</span>
        <span class="n" title=${e.note ?? ""}>${e.userName ?? ""}${e.note ? ` · ${e.note}` : ""}</span>
        ${this.canEdit && (e.userId === me || this.owner) ? html`<button type="button" class="ghost icon" aria-label=${t("time.remove")} title=${t("time.remove")} @click=${() => this.removeEntry(e.id)}>${icon("X", 13)}</button>` : html`<span></span>`}</li>`)}</ul>` : nothing}
      ${this.canEdit ? html`<form @submit=${this.add}>
        <input name="duration" required placeholder="1:30" aria-label=${t("time.duration")} title=${t("time.duration_hint")}>
        <input name="day" type="date" aria-label=${t("time.day")} value=${new Date().toISOString().slice(0, 10)}>
        <input name="note" placeholder=${t("time.note")} aria-label=${t("time.note")} maxlength="500">
        <button type="submit" class="ghost" ?disabled=${this.busy}>${icon("Plus", 14)} ${t("time.add")}</button></form>` : nothing}
    </fieldset>`;
  }
}
customElements.define("phx-task-time", PhxTaskTime);
