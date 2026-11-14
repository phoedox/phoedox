import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { mentionQuery, mentionedIds, splitMentions, type CommentThread, type Mentionable, type PageComment } from "../comments";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { userAvatar } from "../pictures";
import { currentProject, currentUser } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

/** A text box that suggests people after "@". */
export class PhxCommentBox extends LitElement {
  static properties = { members: { attribute: false }, placeholder: {}, label: {}, value: {}, open: { state: true }, active: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; position: relative; }
    textarea { width: 100%; min-height: 64px; resize: vertical; box-sizing: border-box; font: inherit; }
    ul { position: absolute; z-index: 5; left: 0; top: 100%; margin: 4px 0 0; padding: 4px; list-style: none; min-width: 240px; max-width: 100%;
      background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: var(--shadow, 0 6px 24px rgb(0 0 0 / 12%)); }
    li { padding: 6px 8px; border-radius: 7px; cursor: pointer; display: flex; gap: 8px; align-items: baseline; }
    li[aria-selected="true"] { background: var(--accent-soft); }
    li small { color: var(--muted); }
    .row { display: flex; gap: 6px; justify-content: flex-end; margin-top: 6px; }
  `];
  members: Mentionable[] = [];
  placeholder = "";
  label = "";
  value = "";
  open: Mentionable[] = [];
  active = 0;
  busy = false;
  private picked = new Map<string, string>();

  private get area() {
    return this.renderRoot.querySelector("textarea")!;
  }

  focus() {
    this.updateComplete.then(() => this.area?.focus());
  }

  private onInput() {
    const ta = this.area;
    const q = mentionQuery(ta.value, ta.selectionStart);
    if (!q) {
      this.open = [];
      return;
    }
    const l = q.query.toLowerCase();
    this.open = this.members.filter((m) => m.name.toLowerCase().includes(l) || m.email.toLowerCase().startsWith(l)).slice(0, 6);
    this.active = 0;
  }

  private pick(m: Mentionable) {
    const ta = this.area;
    const q = mentionQuery(ta.value, ta.selectionStart);
    if (!q) return;
    const insert = `@${m.name} `;
    ta.value = ta.value.slice(0, q.start) + insert + ta.value.slice(ta.selectionStart);
    const pos = q.start + insert.length;
    ta.setSelectionRange(pos, pos);
    this.picked.set(m.name, m.id);
    this.open = [];
    ta.focus();
  }

  private onKey(e: KeyboardEvent) {
    if (this.open.length) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        this.active = (this.active + (e.key === "ArrowDown" ? 1 : this.open.length - 1)) % this.open.length;
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        this.pick(this.open[this.active]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        this.open = [];
        return;
      }
    }
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      this.submit();
    }
    if (e.key === "Escape") this.dispatchEvent(new CustomEvent("cancel"));
  }

  private submit() {
    const body = this.area.value.trim();
    if (!body) return;
    this.dispatchEvent(new CustomEvent("submit-comment", { detail: { body, mentions: mentionedIds(body, this.picked) } }));
  }

  /** Empties the box after a successful save. */
  clear() {
    this.area.value = "";
    this.picked.clear();
  }

  render() {
    return html`<textarea aria-label=${this.label || this.placeholder} placeholder=${this.placeholder} .value=${this.value}
        role="combobox" aria-autocomplete="list" aria-expanded=${this.open.length ? "true" : "false"} aria-controls="people"
        @input=${this.onInput} @keydown=${this.onKey} @blur=${() => setTimeout(() => (this.open = []), 150)}></textarea>
      ${this.open.length ? html`<ul id="people" role="listbox">${this.open.map((m, i) => html`
        <li role="option" aria-selected=${i === this.active ? "true" : "false"} @mousedown=${(e: Event) => { e.preventDefault(); this.pick(m); }}>
          <b>${m.name}</b><small>${m.email}</small></li>`)}</ul>` : ""}
      <div class="row"><slot></slot><button ?disabled=${this.busy} @click=${this.submit}>${icon("Send", 14)} ${t("comment.send")}</button></div>`;
  }
}
customElements.define("phx-comment-box", PhxCommentBox);

/** Comment threads of a page: on the page or on one of its blocks. */
export class PhxComments extends SignalWatcher(LitElement) {
  static properties = { project: {}, document: {}, threads: { state: true }, members: { state: true }, anchor: { state: true },
    replying: { state: true }, editing: { state: true }, showDone: { state: true }, loaded: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-top: 32px; }
    h2 { font-size: 18px; display: flex; gap: 8px; align-items: center; margin: 0 0 10px; }
    .thread { padding: 12px 14px; margin-bottom: 10px; scroll-margin-top: 80px; }
    .thread.done { opacity: .75; }
    .thread.flash { box-shadow: 0 0 0 2px var(--accent); transition: box-shadow var(--dur) var(--ease-out); }
    .quote { display: flex; gap: 6px; align-items: baseline; font-size: 13px; color: var(--muted); border-left: 3px solid var(--accent); padding: 2px 8px; margin-bottom: 8px;
      background: none; border-top: 0; border-right: 0; border-bottom: 0; border-radius: 0; text-align: left; cursor: pointer; width: 100%; }
    .quote span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .quote.gone { cursor: default; }
    .c { display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; padding: 6px 0; }
    .c + .c { border-top: 1px solid var(--line); }
    .c .head { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap; font-size: 13px; }
    .c .head .muted { font-size: 12px; }
    .c .body { grid-column: 2; white-space: pre-wrap; word-break: break-word; font-size: 14.5px; }
    .mention { color: var(--accent); font-weight: 600; }
    .actions { display: flex; gap: 4px; flex-wrap: wrap; margin-top: 6px; }
    .actions button, .c .head button { padding: 2px 8px; font-size: 12.5px; }
    .c .head .tools { margin-left: auto; display: flex; gap: 2px; }
    .c .head .tools button { padding: 2px 5px; }
    .chip.done { color: var(--good); }
    .new { margin-top: 12px; }
    .new .quote { cursor: default; }
  `];
  project = "";
  document = "";
  threads: CommentThread[] = [];
  members: Mentionable[] = [];
  anchor = "";
  replying = "";
  editing = "";
  showDone = false;
  loaded = false;
  /** Texts of the page's blocks, set by the page so threads can point at them. */
  blockTexts: string[] = [];

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
    api<{ userId: string; name: string; email: string }[]>("GET", `/projects/${this.project}/members`)
      .then((m) => (this.members = (m ?? []).map((x) => ({ id: x.userId, name: x.name || x.email, email: x.email })))).catch(() => {});
  }

  async load() {
    this.threads = (await api<CommentThread[]>("GET", `/projects/${this.project}/documents/${this.document}/comments`).catch(() => [])) ?? [];
    this.loaded = true;
    this.dispatchEvent(new CustomEvent("comments-changed", { detail: this.threads, bubbles: true, composed: true }));
    const id = location.hash.startsWith("#comment-") ? location.hash.slice(9) : "";
    if (id) this.reveal(id);
  }

  /** Opens the box for a new thread on a block. */
  startOnBlock(anchor: string) {
    this.anchor = anchor;
    this.replying = "";
    this.updateComplete.then(() => {
      const box = this.renderRoot.querySelector<PhxCommentBox>(".new phx-comment-box");
      box?.scrollIntoView({ block: "center", behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
      box?.focus();
    });
  }

  /** Scrolls to a thread (or the thread of a reply) and highlights it briefly. */
  reveal(id: string) {
    const th = this.threads.find((x) => x.id === id || x.replies.some((r) => r.id === id));
    if (!th) return;
    if (th.resolvedAt) this.showDone = true;
    this.updateComplete.then(() => {
      const el = this.renderRoot.querySelector<HTMLElement>(`#t-${th.id}`);
      if (!el) return;
      el.scrollIntoView({ block: "center" });
      el.classList.add("flash");
      setTimeout(() => el.classList.remove("flash"), 1600);
    });
  }

  private async call(fn: () => Promise<unknown>): Promise<boolean> {
    try {
      await fn();
      await this.load();
      return true;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return false;
    }
  }

  private async add(e: CustomEvent<{ body: string; mentions: string[] }>, parent = "") {
    const box = e.target as PhxCommentBox;
    box.busy = true;
    const ok = await this.call(() => api("POST", `/projects/${this.project}/documents/${this.document}/comments`,
      { body: e.detail.body, mentions: e.detail.mentions, parent: parent || undefined, anchor: parent ? undefined : this.anchor || undefined }));
    box.busy = false;
    if (!ok) return;
    box.clear();
    if (parent) this.replying = "";
    else this.anchor = "";
  }

  private async edit(c: PageComment, e: CustomEvent<{ body: string; mentions: string[] }>) {
    if (await this.call(() => api("PATCH", `/projects/${this.project}/comments/${c.id}`, { body: e.detail.body, mentions: e.detail.mentions }))) this.editing = "";
  }

  private canModerate() {
    return ["editor", "owner"].includes(currentProject.get()?.role ?? "");
  }

  private nameOf(id: string) {
    return this.members.find((m) => m.id === id)?.name ?? "";
  }

  private comment(c: PageComment, thread: CommentThread) {
    const me = currentUser.get()?.id;
    const owner = currentProject.get()?.role === "owner";
    const names = c.mentions.map((id) => this.nameOf(id));
    return html`<div class="c" id="comment-${c.id}">
      ${userAvatar({ id: c.authorId, name: c.authorName ?? "?", avatar: c.authorAvatar }, 26)}
      <div class="head"><b>${c.authorName}</b><span class="muted">${formatDate(c.createdAt)}${c.updatedAt !== c.createdAt ? ` · ${t("comment.edited")}` : ""}</span>
        <span class="tools">
          ${c.authorId === me && !thread.resolvedAt ? html`<button class="ghost" title=${t("comment.edit")} aria-label=${t("comment.edit")} @click=${() => (this.editing = c.id)}>${icon("Pencil", 13)}</button>` : ""}
          ${c.authorId === me || owner ? html`<button class="ghost" title=${t(c.id === thread.id ? "comment.delete_thread" : "comment.delete")} aria-label=${t(c.id === thread.id ? "comment.delete_thread" : "comment.delete")}
            @click=${() => this.call(() => api("DELETE", `/projects/${this.project}/comments/${c.id}`))}>${icon("Trash2", 13)}</button>` : ""}
        </span></div>
      ${this.editing === c.id
        ? html`<phx-comment-box class="body" .members=${this.members} .value=${c.body} label=${t("comment.edit")}
            @submit-comment=${(e: CustomEvent) => this.edit(c, e)} @cancel=${() => (this.editing = "")}>
            <button class="ghost" @click=${() => (this.editing = "")}>${t("doc.cancel")}</button></phx-comment-box>`
        : html`<div class="body">${splitMentions(c.body, names).map((p) => (p.mention ? html`<span class="mention">${p.text}</span>` : p.text))}</div>`}
    </div>`;
  }

  private thread(th: CommentThread) {
    const me = currentUser.get()?.id;
    const done = !!th.resolvedAt;
    const gone = th.anchor ? this.blockTexts.length > 0 && !this.blockTexts.some((b) => b.replace(/\s+/g, " ").includes(th.anchor!.slice(0, 40))) : false;
    return html`<div class="thread card ${done ? "done" : ""}" id="t-${th.id}">
      ${th.anchor ? html`<button class="quote ${gone ? "gone" : ""}" title=${gone ? t("comment.block_changed") : t("comment.show_block")}
          @click=${() => !gone && this.dispatchEvent(new CustomEvent("show-block", { detail: th.anchor, bubbles: true, composed: true }))}>
          ${icon("Quote", 12)}<span>${th.anchor}</span></button>` : ""}
      ${this.comment(th, th)}
      ${th.replies.map((r) => this.comment(r, th))}
      ${this.replying === th.id
        ? html`<phx-comment-box .members=${this.members} placeholder=${t("comment.reply_placeholder")} @submit-comment=${(e: CustomEvent) => this.add(e, th.id)}
            @cancel=${() => (this.replying = "")}><button class="ghost" @click=${() => (this.replying = "")}>${t("doc.cancel")}</button></phx-comment-box>`
        : html`<div class="actions">
          ${done ? html`<span class="chip done">${icon("Check", 12)} ${t("comment.done")}</span>` : html`<button class="ghost" @click=${() => { this.replying = th.id; this.updateComplete.then(() => this.renderRoot.querySelector<PhxCommentBox>(`#t-${th.id} phx-comment-box`)?.focus()); }}>${icon("Reply", 13)} ${t("comment.reply")}</button>`}
          ${th.authorId === me || this.canModerate()
            ? html`<button class="ghost" @click=${() => this.call(() => api("PATCH", `/projects/${this.project}/comments/${th.id}`, { resolved: !done }))}>
                ${icon(done ? "RotateCcw" : "CircleCheck", 13)} ${t(done ? "comment.reopen" : "comment.resolve")}</button>` : ""}
        </div>`}
    </div>`;
  }

  render() {
    if (!this.loaded) return "";
    const open = this.threads.filter((x) => !x.resolvedAt);
    const done = this.threads.filter((x) => x.resolvedAt);
    return html`<section aria-labelledby="ch">
      <h2 id="ch">${icon("MessageSquare", 17)} ${t("comment.title")} ${open.length ? html`<span class="chip">${open.length}</span>` : ""}</h2>
      ${open.map((x) => this.thread(x))}
      ${done.length ? html`<button class="ghost" aria-expanded=${this.showDone ? "true" : "false"} @click=${() => (this.showDone = !this.showDone)}>
        ${icon(this.showDone ? "ChevronDown" : "ChevronRight", 14)} ${done.length === 1 ? t("comment.done_one") : t("comment.done_count", { count: done.length })}</button>
        ${this.showDone ? done.map((x) => this.thread(x)) : ""}` : ""}
      <div class="new">
        ${this.anchor ? html`<div class="quote">${icon("Quote", 12)}<span>${this.anchor}</span>
          <button class="ghost" style="margin-left:auto;padding:0 6px" aria-label=${t("comment.unanchor")} title=${t("comment.unanchor")} @click=${() => (this.anchor = "")}>${icon("X", 12)}</button></div>` : ""}
        <phx-comment-box .members=${this.members} placeholder=${t(this.anchor ? "comment.block_placeholder" : "comment.placeholder")} @submit-comment=${(e: CustomEvent) => this.add(e)}
          @cancel=${() => (this.anchor = "")}></phx-comment-box>
      </div>
    </section>`;
  }
}
customElements.define("phx-comments", PhxComments);
