import { LitElement, css, html } from "lit";
import { repeat } from "lit/directives/repeat.js";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, api, type Board, type BoardColumn, type Doc, type Member, type Task } from "../api";
import { formatDate, formatDay, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown } from "../md";
import { currentProject } from "../state";
import { base } from "./shared";
import { userAvatar } from "../pictures";
import { toast } from "./toasts";

/** Fallback poll; changes normally arrive at once through the event stream. */
const REFRESH = 60000;

/** Position between two neighbours (cards are ordered by a number). */
function between(before?: number, after?: number): number {
  if (before === undefined && after === undefined) return 1;
  if (before === undefined) return after! - 1;
  if (after === undefined) return before + 1;
  return (before + after) / 2;
}

/**
 * A task board (ADR-0030): columns from the board document, cards from the
 * tasks API. Cards move by drag and drop or with Alt+arrow keys.
 */
export class PhxBoard extends LitElement {
  static properties = {
    project: {}, doc: {}, tasks: { state: true }, members: { state: true }, open: { state: true }, editingColumns: { state: true },
    dragging: { state: true }, dropAt: { state: true }, adding: { state: true },
  };
  static styles = [base, css`
    :host { display: block; margin-top: 8px; white-space: normal; }
    .tools { display: flex; gap: 8px; align-items: center; margin-bottom: 10px; flex-wrap: wrap; }
    .tools .muted { font-size: 13px; }
    .cols { display: flex; gap: 12px; overflow-x: auto; padding-bottom: 8px; align-items: flex-start; scrollbar-width: thin; }
    .col { flex: 0 0 272px; background: var(--surface2); border-radius: 10px; padding: 8px; display: flex; flex-direction: column; gap: 6px; max-height: 75vh; }
    .col.drop { box-shadow: inset 0 0 0 2px var(--accent); }
    .col header { display: flex; align-items: center; gap: 6px; padding: 2px 4px 4px; font-weight: 600; font-size: 14px; }
    .col header .n { color: var(--muted); font-weight: 500; font-variant-numeric: tabular-nums; }
    .col header .ok { color: var(--good); display: inline-flex; }
    .list { display: flex; flex-direction: column; gap: 6px; overflow-y: auto; min-height: 8px; }
    .card { background: var(--surface); border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; cursor: pointer; text-align: left; font: inherit; color: var(--fg);
      display: flex; flex-direction: column; gap: 6px; transition: border-color var(--dur-fast) ease, opacity var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { .card:hover { border-color: var(--muted); } }
    .card:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
    .card.ghost { opacity: .4; }
    .card.before { box-shadow: 0 -3px 0 -1px var(--accent); }
    .card.done .title { color: var(--muted); text-decoration: line-through; }
    .title { font-size: 14px; line-height: 1.35; overflow-wrap: anywhere; }
    .meta { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; font-size: 12px; color: var(--muted); }
    .meta .late { color: var(--bad); }
    .label { background: var(--accent-soft); color: var(--fg); border-radius: 999px; padding: 0 7px; font-size: 11.5px; }
    .prio { display: inline-flex; align-items: center; gap: 2px; }
    .prio.p3 { color: var(--bad); } .prio.p2 { color: var(--warn, var(--accent)); }
    .who { margin-left: auto; display: inline-flex; }
    .add { display: flex; gap: 6px; }
    .add input { flex: 1; font-size: 13.5px; padding: 6px 8px; }
    .addbtn { background: none; border: none; color: var(--muted); justify-content: flex-start; padding: 6px 4px; font-size: 13.5px; }
    @media (hover: hover) and (pointer: fine) { .addbtn:hover { color: var(--fg); } }
    .colform { display: grid; gap: 8px; max-width: 640px; margin-bottom: 14px; padding: 12px; border: 1px dashed var(--line); border-radius: 10px; }
    .colrow { display: grid; grid-template-columns: minmax(0, 1fr) auto auto auto auto; gap: 6px; align-items: center; }
    .colrow label { display: inline-flex; gap: 4px; align-items: center; font-size: 13px; white-space: nowrap; }
    .colrow button { padding: 5px 7px; }
    dialog { border: 1px solid var(--line); border-radius: 14px; padding: 18px 20px; width: min(620px, calc(100vw - 32px)); background: var(--bg); color: var(--fg);
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out); @starting-style { opacity: 0; transform: scale(.97); } }
    dialog::backdrop { background: color-mix(in srgb, #000 35%, transparent); }
    dialog form { display: grid; gap: 10px; }
    dialog .two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    dialog textarea { min-height: 140px; }
    dialog .row { display: flex; gap: 8px; align-items: center; margin-top: 4px; }
    dialog .row .danger { margin-right: auto; }
    dialog .prose { font-size: 14px; }
    .hint { font-size: 12.5px; color: var(--muted); }
    @media (max-width: 600px) { dialog .two { grid-template-columns: 1fr; } }
    @media (prefers-reduced-motion: reduce) { dialog { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
  `];
  project = "";
  doc!: Doc;
  tasks?: Task[];
  members: Member[] = [];
  /** The card in the dialog ("new:<column>" for a new one). */
  open?: Task;
  editingColumns = false;
  dragging?: Task;
  dropAt?: { column: string; before?: string };
  /** Column with an open "add card" field. */
  adding = "";
  private timer?: number;
  private loadedFor = "";
  private events?: EventSource;
  private reloadPending = false;
  /** Card to open once loaded (?task=<id> in the address, e.g. from a notification). */
  private wanted = new URLSearchParams(location.search).get("task") ?? "";

  private get board(): Board {
    const b = (this.doc.content ?? {}) as Board;
    return { ...b, columns: b.columns?.length ? b.columns : [] };
  }

  private get canEdit() {
    return ["editor", "owner"].includes(this.doc.role ?? currentProject.get()?.role ?? "");
  }

  connectedCallback() {
    super.connectedCallback();
    this.timer = window.setInterval(() => {
      if (document.visibilityState === "visible" && !this.dragging && !this.open) void this.load();
    }, REFRESH);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.clearInterval(this.timer);
    this.events?.close();
    this.events = undefined;
  }

  /** Reloads when someone else changes the board (cards or columns). */
  private listen() {
    this.events?.close();
    if (typeof EventSource === "undefined") return;
    const es = new EventSource(`/api/v1/projects/${this.project}/boards/${this.doc.id}/events`);
    es.addEventListener("change", (e) => {
      let kind = "";
      try {
        kind = JSON.parse((e as MessageEvent).data).kind;
      } catch {
        /* ignore */
      }
      if (kind === "board.update") this.dispatchEvent(new CustomEvent("changed"));
      else this.reloadSoon();
    });
    this.events = es;
  }

  /** Several changes in a row cause one reload; never while dragging. */
  private reloadSoon() {
    if (this.reloadPending) return;
    this.reloadPending = true;
    setTimeout(() => {
      this.reloadPending = false;
      if (this.dragging) this.reloadSoon();
      else void this.load();
    }, 250);
  }

  willUpdate() {
    if (this.doc && this.loadedFor !== this.doc.id) {
      this.loadedFor = this.doc.id;
      void this.load();
      this.listen();
      api<Member[]>("GET", `/projects/${this.project}/members`).then((m) => (this.members = m ?? []), () => {});
    }
  }

  private async load() {
    try {
      this.tasks = await api<Task[]>("GET", `/projects/${this.project}/tasks?board=${this.doc.id}`);
      if (this.wanted) {
        const card = this.tasks?.find((x) => x.id === this.wanted);
        this.wanted = "";
        if (card) this.show(card);
      }
    } catch (e) {
      if (!this.tasks) this.tasks = [];
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private cardsOf(col: string) {
    return (this.tasks ?? []).filter((x) => x.column === col).sort((a, b) => a.position - b.position);
  }

  private async patch(task: Task, body: Record<string, unknown>) {
    // Optimistic: show the change at once, roll back on error.
    const before = this.tasks;
    this.tasks = (this.tasks ?? []).map((x) => (x.id === task.id ? { ...x, ...body } as Task : x));
    try {
      const saved = await api<Task>("PATCH", `/projects/${this.project}/tasks/${task.id}`, body);
      this.tasks = (this.tasks ?? []).map((x) => (x.id === saved.id ? saved : x));
      return saved;
    } catch (e) {
      this.tasks = before;
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return undefined;
    }
  }

  /** Moves a card into a column before another card (or to the end). */
  private move(task: Task, column: string, beforeId?: string) {
    const cards = this.cardsOf(column).filter((x) => x.id !== task.id);
    const i = beforeId ? cards.findIndex((x) => x.id === beforeId) : cards.length;
    const at = i < 0 ? cards.length : i;
    const position = between(cards[at - 1]?.position, cards[at]?.position);
    if (task.column === column && task.position === position) return;
    void this.patch(task, { column, position });
  }

  private async create(column: string, title: string) {
    title = title.trim();
    if (!title) return;
    try {
      const task = await api<Task>("POST", `/projects/${this.project}/boards/${this.doc.id}/tasks`, { title, column });
      this.tasks = [...(this.tasks ?? []), task];
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private onKey(e: KeyboardEvent, task: Task) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      this.show(task);
      return;
    }
    if (!e.altKey || !this.canEdit) return;
    const cols = this.board.columns.map((c) => c.id);
    const ci = cols.indexOf(task.column);
    const cards = this.cardsOf(task.column);
    const i = cards.findIndex((x) => x.id === task.id);
    let done = true;
    if (e.key === "ArrowLeft" && ci > 0) this.move(task, cols[ci - 1]);
    else if (e.key === "ArrowRight" && ci < cols.length - 1) this.move(task, cols[ci + 1]);
    else if (e.key === "ArrowUp" && i > 0) this.move(task, task.column, cards[i - 1].id);
    else if (e.key === "ArrowDown" && i < cards.length - 1) this.move(task, task.column, cards[i + 2]?.id);
    else done = false;
    if (done) {
      e.preventDefault();
      this.updateComplete.then(() => this.renderRoot.querySelector<HTMLElement>(`[data-id="${task.id}"]`)?.focus());
    }
  }

  private show(task: Task) {
    this.open = task;
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.showModal());
  }

  private close() {
    this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close();
    this.open = undefined;
  }

  /** Members are picked by name or e-mail; anything else is a free-text assignee (e.g. "Claude"). */
  private assigneeRef(value: string): string {
    const v = value.trim();
    const m = this.members.find((x) => x.name === v || x.email.toLowerCase() === v.toLowerCase());
    return m ? m.userId : v;
  }

  private async saveCard(e: SubmitEvent) {
    e.preventDefault();
    const task = this.open!;
    const f = new FormData(e.target as HTMLFormElement);
    const s = (k: string) => String(f.get(k) ?? "");
    const saved = await this.patch(task, {
      title: s("title"), description: s("description"), column: s("column"), assignee: this.assigneeRef(s("assignee")), due: s("due"),
      priority: Number(s("priority")), labels: s("labels").split(",").map((x) => x.trim()).filter(Boolean),
    });
    if (saved) this.close();
  }

  private async deleteCard() {
    const task = this.open!;
    if (!confirm(t("board.delete_confirm", { title: task.title }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/tasks/${task.id}`);
      this.tasks = (this.tasks ?? []).filter((x) => x.id !== task.id);
      this.close();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async saveColumns(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const rows = Array.from(form.querySelectorAll<HTMLElement>(".colrow"));
    const columns: BoardColumn[] = rows.map((r) => ({
      id: r.dataset.id || "",
      name: (r.querySelector("input[name=name]") as HTMLInputElement).value,
      done: (r.querySelector("input[name=done]") as HTMLInputElement).checked,
    })).filter((c) => c.name.trim());
    try {
      await api("PATCH", `/projects/${this.project}/documents/${this.doc.id}`, { board: { ...this.board, columns }, expectVersion: this.doc.version });
      this.editingColumns = false;
      this.dispatchEvent(new CustomEvent("changed"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private columnsForm() {
    const cols = this.board.columns;
    const moveRow = (btn: HTMLElement, dir: -1 | 1) => {
      const row = btn.closest(".colrow")!;
      const sib = dir < 0 ? row.previousElementSibling : row.nextElementSibling;
      if (sib?.classList.contains("colrow")) dir < 0 ? sib.before(row) : sib.after(row);
    };
    return html`<form class="colform" @submit=${this.saveColumns}>
      <div class="hint">${t("board.columns_hint")}</div>
      <div class="rows">${cols.map((c) => html`<div class="colrow" data-id=${c.id}>
        <input name="name" .value=${c.name} aria-label=${t("board.column_name")} required maxlength="60">
        <label><input type="checkbox" name="done" ?checked=${c.done}> ${t("board.column_done")}</label>
        <button type="button" class="ghost" aria-label=${t("board.move_up")} @click=${(e: Event) => moveRow(e.currentTarget as HTMLElement, -1)}>${icon("ArrowUp", 13)}</button>
        <button type="button" class="ghost" aria-label=${t("board.move_down")} @click=${(e: Event) => moveRow(e.currentTarget as HTMLElement, 1)}>${icon("ArrowDown", 13)}</button>
        <button type="button" class="ghost" aria-label=${t("board.remove_column")} title=${t("board.remove_column_hint")} @click=${(e: Event) => (e.currentTarget as HTMLElement).closest(".colrow")!.remove()}>${icon("X", 13)}</button>
      </div>`)}</div>
      <div class="row" style="display:flex;gap:8px">
        <button type="button" class="ghost" @click=${(e: Event) => {
          const rows = (e.currentTarget as HTMLElement).closest("form")!.querySelector(".rows")!;
          const tpl = rows.querySelector(".colrow")!.cloneNode(true) as HTMLElement;
          tpl.dataset.id = "";
          (tpl.querySelector("input[name=name]") as HTMLInputElement).value = "";
          (tpl.querySelector("input[name=done]") as HTMLInputElement).checked = false;
          tpl.querySelectorAll("button").forEach((b, i) => b.addEventListener("click", (ev) => {
            const r = (ev.currentTarget as HTMLElement).closest(".colrow")!;
            if (i === 2) r.remove(); else moveRow(ev.currentTarget as HTMLElement, i === 0 ? -1 : 1);
          }));
          rows.append(tpl);
          (tpl.querySelector("input[name=name]") as HTMLInputElement).focus();
        }}>${icon("Plus", 13)} ${t("board.add_column")}</button>
        <span style="flex:1"></span>
        <button type="button" class="ghost" @click=${() => (this.editingColumns = false)}>${t("doc.cancel")}</button>
        <button>${t("doc.save")}</button>
      </div>
    </form>`;
  }

  private card(task: Task, col: BoardColumn) {
    const late = task.due && !col.done && task.due < new Date().toISOString().slice(0, 10);
    const drop = this.dropAt?.before === task.id && this.dragging?.id !== task.id;
    return html`<div class=${["card", col.done ? "done" : "", this.dragging?.id === task.id ? "ghost" : "", drop ? "before" : ""].join(" ")}
      data-id=${task.id} role="listitem" tabindex="0" draggable=${this.canEdit ? "true" : "false"}
      aria-label=${task.title} aria-keyshortcuts=${this.canEdit ? "Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown" : ""}
      @click=${() => this.show(task)} @keydown=${(e: KeyboardEvent) => this.onKey(e, task)}
      @dragstart=${(e: DragEvent) => { this.dragging = task; e.dataTransfer!.effectAllowed = "move"; e.dataTransfer!.setData("text/plain", task.title); }}
      @dragend=${() => { this.dragging = undefined; this.dropAt = undefined; }}
      @dragover=${(e: DragEvent) => { if (!this.dragging) return; e.preventDefault(); e.stopPropagation(); this.dropAt = { column: col.id, before: task.id }; }}
      @drop=${(e: DragEvent) => { e.preventDefault(); e.stopPropagation(); if (this.dragging) this.move(this.dragging, col.id, task.id); this.dragging = undefined; this.dropAt = undefined; }}>
      <span class="title">${task.title}</span>
      ${task.labels.length || task.due || task.priority || task.assigneeName || task.assigneeText ? html`<span class="meta">
        ${task.labels.map((l) => html`<span class="label">${l}</span>`)}
        ${task.priority ? html`<span class="prio p${task.priority}" title=${t(`board.priority.${task.priority}` as any)}>${icon("Flag", 12)}</span>` : ""}
        ${task.due ? html`<span class=${late ? "late" : ""} title=${t("board.due")}>${icon("CalendarDays", 12)} ${formatDay(task.due)}</span>` : ""}
        ${task.assigneeName ? html`<span class="who" title=${task.assigneeName}>${userAvatar({ id: task.assigneeId ?? "", name: task.assigneeName, avatar: task.assigneeAvatar }, 22)}</span>`
          : task.assigneeText ? html`<span class="chip who-text" title=${t("board.assignee") + ": " + task.assigneeText}>${icon("UserRound", 11)} ${task.assigneeText}</span>` : ""}
      </span>` : ""}
    </div>`;
  }

  private column(col: BoardColumn) {
    const cards = this.cardsOf(col.id);
    const drop = this.dropAt?.column === col.id && !this.dropAt.before;
    return html`<section class=${drop ? "col drop" : "col"} aria-label=${col.name}
      @dragover=${(e: DragEvent) => { if (!this.dragging) return; e.preventDefault(); this.dropAt = { column: col.id }; }}
      @drop=${(e: DragEvent) => { e.preventDefault(); if (this.dragging) this.move(this.dragging, col.id); this.dragging = undefined; this.dropAt = undefined; }}>
      <header>${col.done ? html`<span class="ok" title=${t("board.column_done")}>${icon("Check", 14)}</span>` : ""}<span>${col.name}</span><span class="n">${cards.length}</span></header>
      <div class="list" role="list">${repeat(cards, (x) => x.id, (x) => this.card(x, col))}</div>
      ${this.canEdit ? (this.adding === col.id
        ? html`<form class="add" @submit=${(e: SubmitEvent) => { e.preventDefault(); const i = (e.target as HTMLFormElement).querySelector("input")!; void this.create(col.id, i.value); i.value = ""; }}>
            <input aria-label=${t("board.new_card")} placeholder=${t("board.new_card")} autofocus @keydown=${(e: KeyboardEvent) => e.key === "Escape" && (this.adding = "")}>
            <button class="ghost" aria-label=${t("board.add_card")}>${icon("Plus", 14)}</button></form>`
        : html`<button class="addbtn" @click=${() => { this.adding = col.id; this.updateComplete.then(() => this.renderRoot.querySelector<HTMLInputElement>(".add input")?.focus()); }}>${icon("Plus", 14)} ${t("board.add_card")}</button>`) : ""}
    </section>`;
  }

  private dialog() {
    const task = this.open;
    if (!task) return "";
    const edit = this.canEdit;
    return html`<dialog aria-labelledby="card-title" @close=${() => (this.open = undefined)}>
      <form @submit=${this.saveCard}>
        <label class="field" id="card-title">${t("doc.title")}<input name="title" required .value=${task.title} ?disabled=${!edit}></label>
        <label class="field">${t("board.card_description")}<textarea name="description" .value=${task.description} ?disabled=${!edit}></textarea></label>
        ${!edit && task.description ? html`<div class="prose">${unsafeHTML(renderMarkdown(task.description, this.project))}</div>` : ""}
        <div class="two">
          <label class="field">${t("board.column")}<select name="column" ?disabled=${!edit}>${this.board.columns.map((c) => html`<option value=${c.id} ?selected=${c.id === task.column}>${c.name}</option>`)}</select></label>
          <label class="field">${t("board.assignee")}<input name="assignee" list="assignees" autocomplete="off" ?disabled=${!edit} placeholder=${t("board.assignee_hint")}
            .value=${task.assigneeId ? (this.members.find((m) => m.userId === task.assigneeId)?.name || task.assigneeName || "") : (task.assigneeText ?? "")}>
            <datalist id="assignees">${this.members.map((m) => html`<option value=${m.name || m.email}>${m.email}</option>`)}</datalist></label>
          <label class="field">${t("board.due")}<input type="date" name="due" .value=${task.due ?? ""} ?disabled=${!edit}></label>
          <label class="field">${t("board.priority")}<select name="priority" ?disabled=${!edit}>${[0, 1, 2, 3].map((p) => html`<option value=${p} ?selected=${p === task.priority}>${t(`board.priority.${p}` as any)}</option>`)}</select></label>
        </div>
        <label class="field">${t("board.labels")}<input name="labels" .value=${task.labels.join(", ")} ?disabled=${!edit} placeholder=${t("board.labels_hint")}></label>
        <p class="hint">${t("board.created", { date: formatDate(task.createdAt) })}${task.doneAt ? ` · ${t("board.done_at", { date: formatDate(task.doneAt) })}` : ""}</p>
        <div class="row">
          ${edit ? html`<button type="button" class="danger" @click=${this.deleteCard}>${icon("Trash2", 14)} ${t("board.delete")}</button>` : ""}
          <span style="flex:1"></span>
          <button type="button" class="ghost" @click=${this.close}>${edit ? t("doc.cancel") : t("board.close")}</button>
          ${edit ? html`<button>${t("doc.save")}</button>` : ""}
        </div>
      </form>
    </dialog>`;
  }

  render() {
    if (!this.tasks) return html`<p class="muted">${t("app.loading")}</p>`;
    const cols = this.board.columns;
    const open = this.tasks.filter((x) => !x.doneAt).length;
    return html`
      <div class="tools">
        <span class="muted">${t("board.summary", { open, total: this.tasks.length })}</span>
        <span style="flex:1"></span>
        ${this.canEdit && !this.editingColumns ? html`<button class="ghost" @click=${() => (this.editingColumns = true)}>${icon("Columns3", 14)} ${t("board.edit_columns")}</button>` : ""}
      </div>
      ${this.editingColumns ? this.columnsForm() : ""}
      <div class="cols" @dragleave=${(e: DragEvent) => { if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) this.dropAt = undefined; }}>
        ${cols.map((c) => this.column(c))}
      </div>
      ${this.canEdit ? html`<p class="hint">${t("board.keys_hint")}</p>` : ""}
      ${this.dialog()}`;
  }
}
customElements.define("phx-board", PhxBoard);
