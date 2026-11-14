import { LitElement, css, html } from "lit";
import { ApiError, api, type Doc } from "../api";
import { formatDate, formatDay, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Plan {
  id: string; title: string; description: string; every: number; unit: "day" | "week" | "month" | "year"; leadDays: number;
  assignee?: string; nextDue: string; openTaskId?: string; lastDone: string | null; boardSlug?: string; boardId: string;
}
const UNITS = ["day", "week", "month", "year"] as const;
type Key = Parameters<typeof t>[0];

/** Maintenance plans of an entry: recurring cards on a board. */
export class PhxMaintenance extends LitElement {
  static properties = { project: {}, entity: {}, canEdit: { type: Boolean }, plans: { state: true }, boards: { state: true }, editing: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-top: 12px; }
    h2 { display: flex; gap: 8px; align-items: center; }
    ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 8px; }
    li { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; background: var(--surface); }
    li header { display: flex; gap: 8px; align-items: center; }
    li header b { margin-right: auto; }
    .meta { font-size: 13px; color: var(--muted); }
    form { display: grid; gap: 8px; border: 1px solid var(--line); border-radius: 10px; padding: 12px; margin-top: 8px; }
    .row select, .row input[type=number] { width: auto; }
    .row input[type=number] { max-width: 90px; }
  `];
  project = "";
  entity = "";
  canEdit = false;
  plans?: Plan[];
  boards: Doc[] = [];
  editing?: Plan | "new";
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    this.load();
  }

  private async load() {
    this.plans = await api<Plan[]>("GET", `/projects/${this.project}/maintenance?entity=${encodeURIComponent(this.entity)}`).catch(() => []);
  }

  private async edit(p: Plan | "new") {
    if (!this.boards.length) {
      const docs = await api<Doc[]>("GET", `/projects/${this.project}/documents?type=board`).catch(() => []);
      this.boards = docs.filter((d) => d.type === "board");
    }
    this.editing = p;
  }

  private async save(e: Event) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const id = this.editing !== "new" ? this.editing?.id : "";
    this.busy = true;
    try {
      await api(id ? "PUT" : "POST", `/projects/${this.project}/maintenance${id ? "/" + id : ""}`, {
        entity: this.entity, board: f.get("board"), title: f.get("title"), description: f.get("description"),
        every: Number(f.get("every")), unit: f.get("unit"), leadDays: Number(f.get("lead")), assignee: f.get("assignee"), nextDue: f.get("next"),
      });
      this.editing = undefined;
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async deletePlan(p: Plan) {
    if (!confirm(t("maint.delete_confirm", { title: p.title }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/maintenance/${p.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private form() {
    const p = this.editing === "new" ? undefined : this.editing;
    if (!this.boards.length) return html`<p class="muted">${t("maint.no_board")}</p>`;
    return html`<form @submit=${this.save}>
      <label>${t("maint.what")}<input name="title" required .value=${p?.title ?? ""} placeholder=${t("maint.what_placeholder")}></label>
      <div class="row">
        <label>${t("maint.every")} <input type="number" name="every" min="1" max="1000" required .value=${String(p?.every ?? 6)}></label>
        <select name="unit" aria-label=${t("maint.unit")}>${UNITS.map((u) => html`<option value=${u} ?selected=${u === (p?.unit ?? "month")}>${t(`maint.unit.${u}` as Key)}</option>`)}</select>
        <label>${t("maint.next")} <input type="date" name="next" .value=${p?.nextDue ?? new Date().toISOString().slice(0, 10)}></label>
      </div>
      <div class="row">
        <label>${t("maint.board")} <select name="board">${this.boards.map((b) => html`<option value=${b.slug} ?selected=${b.id === p?.boardId}>${b.title}</option>`)}</select></label>
        <label>${t("maint.lead")} <input type="number" name="lead" min="0" max="365" .value=${String(p?.leadDays ?? 7)}></label>
        <label>${t("board.assignee")} <input name="assignee" .value=${p?.assignee ?? ""}></label>
      </div>
      <label>${t("maint.notes")}<textarea name="description" .value=${p?.description ?? ""}></textarea></label>
      <div class="row"><button ?disabled=${this.busy}>${t("doc.save")}</button><button type="button" class="ghost" @click=${() => (this.editing = undefined)}>${t("doc.cancel")}</button></div>
    </form>`;
  }

  render() {
    const list = this.plans ?? [];
    if (!list.length && !this.canEdit) return "";
    return html`
      <h2>${icon("Wrench", 15)} ${t("maint.title")}</h2>
      ${list.length ? html`<ul>${list.map((p) => html`<li>
        <header><b>${p.title}</b>
          ${this.canEdit ? html`<button class="ghost icon" title=${t("doc.edit")} aria-label=${t("doc.edit")} @click=${() => this.edit(p)}>${icon("Pencil", 13)}</button>
          <button class="ghost icon" title=${t("doc.delete")} aria-label=${t("doc.delete")} @click=${() => this.deletePlan(p)}>${icon("Trash2", 13)}</button>` : ""}</header>
        <div class="meta">${t("maint.interval", { n: p.every, unit: t(`maint.unit.${p.unit}` as Key) })} · ${p.openTaskId
          ? html`<a href="/p/${this.project}/d/${p.boardSlug}">${t("maint.open_card", { date: formatDay(p.nextDue) })}</a>`
          : t("maint.next_due", { date: formatDay(p.nextDue) })}${p.lastDone ? html` · ${t("maint.last_done", { date: formatDate(p.lastDone) })}` : ""}</div>
      </li>`)}</ul>` : html`<p class="muted">${t("maint.none")}</p>`}
      ${this.editing ? this.form() : this.canEdit ? html`<button class="ghost" style="margin-top:8px" @click=${() => this.edit("new")}>${icon("Plus", 14)} ${t("maint.add")}</button>` : ""}`;
  }
}
customElements.define("phx-maintenance", PhxMaintenance);
