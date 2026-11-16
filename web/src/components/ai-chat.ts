import { LitElement, css, html, nothing } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, api } from "../api";
import { WRITE_TOOLS, dollars, sendMessage, toolResults, userText, type Chat, type ChatEvent, type ChatModels, type ChatStatus, type ChatUsage, type ChatView, type Message, type Part } from "../ai-chat";
import { hasKey, locale, t, type Key } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown } from "../md";
import { docsVersion } from "../state";
import { base } from "./shared";
import "./member-budgets";
import type { PhxBudgetRequest } from "./member-budgets";
import { toast } from "./toasts";

interface Choice { picked: number[]; other: string }
interface AskInput { questions: { question: string; header?: string; options: { label: string; description?: string }[]; multiSelect?: boolean }[] }

/** What is streaming in while the model answers. */
interface Live {
  text: string;
  tools: { name: string; done?: boolean; ok?: boolean }[];
  notice?: string;
}

const tr = (key: string, vars: Record<string, unknown> = {}) => (hasKey(key) ? t(key as Key, vars) : key);

/** AI chat panel of a project (ADR-0066): work out concepts, let the AI write
 * into the open page's draft, and turn a concept into cards. */
export class PhxAIChat extends LitElement {
  static properties = {
    project: {}, doc: {}, status: { state: true }, chats: { state: true }, view: { state: true }, draft: { state: true }, busy: { state: true },
    live: { state: true }, picks: { state: true }, choices: { state: true }, error: { state: true }, pending: { state: true }, models: { state: true }, model: { state: true }, usage: { state: true },
  };
  static styles = [base, css`
    :host { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--surface); border-left: 1px solid var(--line); }
    header { display: flex; gap: 6px; align-items: center; padding: 10px 12px; border-bottom: 1px solid var(--line); }
    header h2 { font-size: 15px; flex: none; display: flex; gap: 6px; align-items: center; }
    header select { flex: 1; min-width: 0; padding: 4px 8px; font-size: 13px; }
    header button { padding: 5px 7px; }
    .budget { font-size: 12px; color: var(--muted); padding: 6px 12px; border-bottom: 1px solid var(--line); display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
    .budget select.model { width: auto; max-width: 100%; padding: 3px 6px; font-size: 12px; }
    .log { flex: 1; overflow: auto; padding: 12px; display: flex; flex-direction: column; gap: 10px; overscroll-behavior: contain; }
    .msg { max-width: 100%; font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
    .msg.user { align-self: flex-end; max-width: 88%; background: var(--accent-soft); padding: 8px 12px; border-radius: 12px 12px 4px 12px; white-space: pre-wrap; }
    .msg.ai :first-child { margin-top: 0; }
    .msg.ai :last-child { margin-bottom: 0; }
    .msg.ai pre { overflow: auto; background: var(--surface2); padding: 8px; border-radius: 8px; font-size: 12px; }
    .msg.ai table { border-collapse: collapse; font-size: 13px; }
    .msg.ai td, .msg.ai th { border: 1px solid var(--line); padding: 3px 6px; }
    .tool { display: inline-flex; gap: 6px; align-items: center; font-size: 12px; color: var(--muted); padding: 2px 8px; border-radius: 999px; background: var(--surface2); width: fit-content; }
    .tool.err { color: var(--bad); }
    .tool a { color: inherit; }
    .proposal { border: 1px solid var(--line); border-radius: 10px; padding: 10px; display: grid; gap: 8px; background: var(--bg); }
    .proposal h3 { font-size: 13px; font-family: var(--font-body); display: flex; gap: 6px; align-items: center; }
    .proposal label { display: grid; grid-template-columns: auto 1fr; gap: 8px; align-items: start; font-size: 13px; padding: 6px; border-radius: 8px; background: var(--surface); cursor: pointer; }
    .proposal label input { width: auto; margin-top: 3px; }
    .proposal .desc { color: var(--muted); font-size: 12px; display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
    .proposal .done { font-size: 13px; }
    .proposal .done ul { margin: 4px 0 0; padding-left: 18px; }
    .ask { border: 1px solid var(--line); border-radius: 10px; padding: 10px; display: grid; gap: 10px; background: var(--bg); }
    .ask fieldset { border: 0; margin: 0; padding: 0; display: grid; gap: 6px; min-width: 0; }
    .ask legend { font-size: 14px; font-weight: 600; padding: 0; margin-bottom: 4px; display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }
    .ask .opts { display: grid; gap: 4px; }
    .ask .opt { display: grid; gap: 2px; justify-items: start; text-align: left; font-weight: 400; background: var(--surface); color: var(--fg); border-color: var(--line); padding: 7px 10px; }
    .ask .opt span { font-size: 12px; color: var(--muted); }
    .ask .opt.on { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); }
    .ask input { font-size: 13px; padding: 6px 8px; }
    .ask .hint { font-size: 11px; color: var(--muted); }
    .ask .answered { margin: 0; font-size: 13px; white-space: pre-wrap; color: var(--muted); display: flex; gap: 6px; }
    .ask fieldset:disabled .opt { cursor: default; opacity: 0.85; }
    .notice { font-size: 13px; color: var(--muted); border-left: 3px solid var(--warn, var(--accent)); padding: 4px 10px; }
    .empty { margin: auto 0; display: grid; gap: 10px; color: var(--muted); font-size: 14px; }
    .empty .starters { display: grid; gap: 6px; }
    .empty .starters button { justify-content: flex-start; font-weight: 400; text-align: left; }
    form { border-top: 1px solid var(--line); padding: 10px 12px; display: grid; gap: 6px; }
    form textarea { min-height: 64px; max-height: 40vh; font-family: var(--font-body); font-size: 14px; resize: none; field-sizing: content; }
    form .row { justify-content: space-between; }
    form .hint { font-size: 11px; color: var(--muted); }
    .off { padding: 16px; display: grid; gap: 8px; font-size: 14px; color: var(--muted); }
    .typing { display: inline-flex; gap: 3px; }
    .typing i { width: 5px; height: 5px; border-radius: 50%; background: var(--muted); animation: blink 1.2s infinite ease-in-out; }
    .typing i:nth-child(2) { animation-delay: .15s; }
    .typing i:nth-child(3) { animation-delay: .3s; }
    @keyframes blink { 0%, 80%, 100% { opacity: .25; } 40% { opacity: 1; } }
    @media (prefers-reduced-motion: reduce) { .typing i { animation: none; opacity: .6; } }
    /* Phones: 16px keeps iOS from zooming into the field. */
    @media (max-width: 800px) { form textarea { font-size: 16px; } form .hint { display: none; } }
  `];

  project = "";
  /** Slug of the page that is open (context for the AI). */
  doc = "";
  status?: ChatStatus;
  usage?: ChatUsage;
  chats: Chat[] = [];
  view?: ChatView;
  draft = "";
  busy = false;
  live?: Live;
  /** Chosen cards per proposal (indexes). */
  picks: Record<string, Set<number>> = {};
  /** Answers being chosen, by ask_user call id and question index. */
  choices: Record<string, Record<number, Choice>> = {};
  error = "";
  /** The message being sent, shown until the chat reloads. */
  pending = "";
  models?: ChatModels;
  /** The chosen model ref. */
  model = "";
  private abort?: AbortController;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.abort?.abort();
  }

  private async load() {
    try {
      this.status = await api<ChatStatus>("GET", `/projects/${this.project}/ai/status`);
      this.models = await api<ChatModels>("GET", `/projects/${this.project}/ai/models`).catch(() => undefined);
      this.model ||= this.models?.default ?? "";
      this.chats = await api<Chat[]>("GET", `/projects/${this.project}/ai/chats`);
      if (this.chats.length && !this.view) await this.open(this.chats[0].id);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async open(id: string) {
    this.view = await api<ChatView>("GET", `/projects/${this.project}/ai/chats/${id}`);
    this.status = this.view.status;
    this.usage = this.view.usage;
    if (this.view.model && this.models?.models.some((m) => m.ref === this.view!.model)) this.model = this.view.model;
    this.picks = {};
    await this.updateComplete;
    this.scrollDown();
  }

  private newChat() {
    this.view = undefined;
    this.live = undefined;
    this.draft = "";
    this.renderRoot.querySelector("textarea")?.focus();
  }

  private async removeChat() {
    if (!this.view || !confirm(t("ai_chat.delete_confirm"))) return;
    await api("DELETE", `/projects/${this.project}/ai/chats/${this.view.id}`).catch(() => undefined);
    this.chats = this.chats.filter((c) => c.id !== this.view?.id);
    this.view = undefined;
    if (this.chats.length) await this.open(this.chats[0].id);
  }

  private scrollDown() {
    const log = this.renderRoot.querySelector(".log");
    if (log) log.scrollTop = log.scrollHeight;
  }

  private async send(e?: Event, answers?: Record<string, string>) {
    e?.preventDefault();
    const text = this.draft.trim();
    // Typed text answers open questions in the person's own words.
    if ((!text && !answers) || this.busy) return;
    this.busy = true;
    this.error = "";
    try {
      let chat = this.view;
      if (!chat) {
        const c = await api<Chat>("POST", `/projects/${this.project}/ai/chats`, { document: this.doc || undefined });
        this.chats = [c, ...this.chats];
        chat = { ...c, messages: [], proposals: {}, status: this.status! };
        this.usage = undefined;
        this.view = chat;
      }
      this.draft = "";
      this.pending = text || Object.values(answers ?? {}).join("\n\n");
      this.live = { text: "", tools: [] };
      this.abort = new AbortController();
      let wrote = false;
      await sendMessage(this.project, chat.id, { text, document: this.doc || undefined, language: locale(), model: this.model || undefined, answers }, (ev) => {
        wrote = this.onEvent(ev) || wrote;
      }, this.abort.signal);
      if (wrote) docsVersion.set(docsVersion.get() + 1);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
      this.draft ||= text;
    } finally {
      this.busy = false;
      this.abort = undefined;
      const notice = this.live?.notice;
      this.pending = "";
      if (this.view) await this.open(this.view.id).catch(() => undefined);
      this.live = notice ? { text: "", tools: [], notice } : undefined;
      this.chats = await api<Chat[]>("GET", `/projects/${this.project}/ai/chats`).catch(() => this.chats);
    }
  }

  /** Handles one streamed event; true when a tool changed content. */
  private onEvent(ev: ChatEvent): boolean {
    const live = this.live ?? { text: "", tools: [] };
    let wrote = false;
    switch (ev.type) {
      case "text":
        live.text += ev.text ?? "";
        break;
      case "tool":
        live.tools = [...live.tools, { name: ev.tool ?? "" }];
        break;
      case "tool_done": {
        const i = live.tools.findIndex((x) => x.name === ev.tool && !x.done);
        if (i >= 0) live.tools[i] = { ...live.tools[i], done: true, ok: ev.ok };
        wrote = !!ev.ok && WRITE_TOOLS.has(ev.tool ?? "");
        break;
      }
      case "notice":
        live.notice = ev.key;
        break;
      case "error":
        this.error = tr(ev.key ?? "error.unknown", ev.params ?? {});
        break;
      case "done":
        if (ev.chat) this.usage = ev.chat;
        break;
    }
    this.live = { ...live };
    void this.updateComplete.then(() => this.scrollDown());
    return wrote;
  }

  private stop() {
    this.abort?.abort();
  }

  private async decide(id: string, accept: boolean, count: number) {
    const chosen = this.picks[id] ?? new Set(Array.from({ length: count }, (_, i) => i));
    if (accept && chosen.size === 0) {
      toast(t("ai_chat.pick_cards"), "error");
      return;
    }
    try {
      const created = await api<{ id: string }[]>("POST", `/projects/${this.project}/ai/chats/${this.view!.id}/proposals/${id}`, { accept, cards: [...chosen] });
      if (accept) toast(t("ai_chat.cards_created", { n: created.length }));
      await this.open(this.view!.id);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  /** Chosen options and own words per question of an open ask_user call. */
  private choiceOf(id: string, q: number): Choice {
    return this.choices[id]?.[q] ?? { picked: [], other: "" };
  }

  private setChoice(id: string, q: number, c: Choice) {
    this.choices = { ...this.choices, [id]: { ...(this.choices[id] ?? {}), [q]: c } };
  }

  private pick(id: string, q: number, i: number, multi: boolean) {
    const c = this.choiceOf(id, q);
    const picked = multi ? (c.picked.includes(i) ? c.picked.filter((x) => x !== i) : [...c.picked, i]) : [i];
    this.setChoice(id, q, { ...c, picked });
  }

  private answer(p: Part) {
    const qs = (p.input as AskInput | undefined)?.questions ?? [];
    const id = p.id ?? "";
    const parts = qs.map((q, k) => {
      const c = this.choiceOf(id, k);
      const labels = c.picked.map((i) => q.options[i]?.label).filter(Boolean);
      const own = c.other.trim();
      const a = [labels.join(", "), own ? `${t("ai_chat.ask_own")}: ${own}` : ""].filter(Boolean).join(" – ");
      return a ? `${q.question}\n${a}` : "";
    });
    if (parts.some((x) => !x)) {
      toast(t("ai_chat.ask_incomplete"), "error");
      return;
    }
    void this.send(undefined, { [id]: parts.join("\n\n") });
  }

  /** Questions of the model with suggested answers (ask_user). */
  private questions(p: Part, results: Map<string, Part>) {
    const id = p.id ?? "";
    const qs = (p.input as AskInput | undefined)?.questions ?? [];
    const res = results.get(id);
    if (res?.isError) return this.toolChip(p, results);
    const open = !res && !this.busy;
    return html`<div class="ask" role="group" aria-label=${t("ai_chat.ask")}>
      ${qs.map((q, k) => {
        const c = this.choiceOf(id, k);
        return html`<fieldset ?disabled=${!open}>
          <legend>${q.header ? html`<span class="chip">${q.header}</span>` : nothing} ${q.question}</legend>
          <div class="opts">${q.options.map((o, i) => html`<button type="button" class=${c.picked.includes(i) ? "opt on" : "opt"} aria-pressed=${c.picked.includes(i) ? "true" : "false"}
            @click=${() => this.pick(id, k, i, !!q.multiSelect)}><strong>${o.label}</strong>${o.description ? html`<span>${o.description}</span>` : nothing}</button>`)}</div>
          ${open ? html`<input .value=${c.other} placeholder=${t("ai_chat.ask_other")} aria-label=${t("ai_chat.ask_other")}
            @input=${(e: Event) => this.setChoice(id, k, { ...c, other: (e.target as HTMLInputElement).value })}
            @keydown=${(e: KeyboardEvent) => e.key === "Enter" && (e.preventDefault(), this.answer(p))}>` : nothing}
        </fieldset>`;
      })}
      ${open ? html`<div class="row"><button @click=${() => this.answer(p)}>${icon("Send", 14)} ${t("ai_chat.ask_send")}</button>
        <span class="hint">${t("ai_chat.ask_hint")}</span></div>`
      : res ? html`<p class="answered">${icon("CircleCheck", 14)} ${(res.text ?? "").replace(/^The person answered:\n/, "").replace(/^The person did not pick an answer; their message follows\.$/, t("ai_chat.ask_free"))}</p>` : nothing}
    </div>`;
  }

  private toggle(id: string, i: number, count: number) {
    const set = new Set(this.picks[id] ?? Array.from({ length: count }, (_, k) => k));
    if (set.has(i)) set.delete(i);
    else set.add(i);
    this.picks = { ...this.picks, [id]: set };
  }

  private onKey(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      void this.send();
    }
  }

  private toolLabel(name: string, input?: Record<string, unknown>) {
    const key = `ai_chat.tool.${name}`;
    const target = String(input?.document ?? input?.title ?? input?.query ?? input?.entity ?? "");
    return hasKey(key) ? t(key as Key, { target }) : t("ai_chat.tool.other", { name });
  }

  private toolChip(p: Part, results: Map<string, Part>) {
    const res = p.id ? results.get(p.id) : undefined;
    const failed = res?.isError;
    const doc = p.name === "edit_page" && !failed ? String(p.input?.document ?? "") : "";
    return html`<span class=${failed ? "tool err" : "tool"} title=${failed ? res?.text ?? "" : ""}>
      ${icon(failed ? "CircleAlert" : p.name === "edit_page" ? "FilePen" : "Wrench", 12)}
      ${doc ? html`<a href="/p/${this.project}/d/${doc}">${this.toolLabel(p.name ?? "", p.input)}</a>` : this.toolLabel(p.name ?? "", p.input)}
    </span>`;
  }

  private proposal(p: Part, results: Map<string, Part>) {
    const id = p.id ?? "";
    const input = p.input as { board?: string; cards?: { title: string; description?: string }[] } | undefined;
    const cards = input?.cards ?? [];
    const decided = this.view?.proposals[id];
    if (results.get(id)?.isError) return this.toolChip(p, results);
    const chosen = this.picks[id];
    return html`<div class="proposal" role="group" aria-label=${t("ai_chat.proposal")}>
      <h3>${icon("SquareKanban", 14)} ${t("ai_chat.proposal_title", { n: cards.length, board: input?.board ?? "" })}</h3>
      ${decided ? html`<div class="done">
          ${decided.status === "accepted" ? html`${icon("CircleCheck", 14)} ${t("ai_chat.cards_created", { n: decided.result?.length ?? 0 })}
            <ul>${(decided.result ?? []).map((c) => html`<li><a href="/p/${this.project}/d/${input?.board}?task=${c.id}">${c.title}</a></li>`)}</ul>`
          : html`${icon("X", 14)} ${t("ai_chat.proposal_rejected")}`}
        </div>`
      : html`${cards.map((c, i) => html`<label>
          <input type="checkbox" .checked=${chosen ? chosen.has(i) : true} @change=${() => this.toggle(id, i, cards.length)}>
          <span><strong>${c.title}</strong>${c.description ? html`<span class="desc">${c.description}</span>` : nothing}</span>
        </label>`)}
        <div class="row">
          <button @click=${() => this.decide(id, true, cards.length)}>${icon("Plus", 14)} ${t("ai_chat.create_cards", { n: chosen ? chosen.size : cards.length })}</button>
          <button class="ghost" @click=${() => this.decide(id, false, cards.length)}>${t("ai_chat.reject")}</button>
        </div>`}
    </div>`;
  }

  private message(m: Message, results: Map<string, Part>) {
    if (m.role === "user") {
      const text = userText(m.parts);
      return text ? html`<div class="msg user">${text}</div>` : nothing;
    }
    return m.parts.map((p) => {
      if (p.type === "text" && p.text?.trim()) return html`<div class="msg ai">${unsafeHTML(renderMarkdown(p.text, this.project))}</div>`;
      if (p.type === "tool_use" && p.name === "propose_cards") return this.proposal(p, results);
      if (p.type === "tool_use" && p.name === "ask_user") return this.questions(p, results);
      if (p.type === "tool_use") return this.toolChip(p, results);
      return nothing;
    });
  }

  private budget() {
    const s = this.status;
    if (!s) return nothing;
    const loc = locale();
    const pct = (p?: number) => new Intl.NumberFormat(loc, { maximumFractionDigits: 1 }).format(p ?? 0);
    const part = (label: string, used: number, limit: number, percent?: number) => s.pricesHidden
      ? (limit ? html`<span>${label}: ${t("ai_chat.used_percent", { p: pct(percent) })}</span>` : nothing)
      : html`<span>${label}: ${limit ? t("ai_chat.used_of", { used: dollars(used, loc), limit: dollars(limit, loc) }) : t("ai_chat.used", { used: dollars(used, loc) })}</span>`;
    const u = this.usage;
    const conv = u && u.tokens ? html`<span>${t("ai_chat.this_chat")}: ${s.pricesHidden ? (u.percent ? t("ai_chat.chat_percent", { p: pct(u.percent) }) : t("ai_chat.chat_tokens", { n: u.tokens.toLocaleString(loc) })) : dollars(u.cents ?? 0, loc)}</span>` : nothing;
    const ms = this.models?.models ?? [];
    return html`<div class="budget" aria-label=${t("ai_chat.budget")}>
      ${ms.length > 1 ? html`<select class="model" aria-label=${t("ai_chat.model")} @change=${(e: Event) => (this.model = (e.target as HTMLSelectElement).value)}>
        ${ms.map((m) => html`<option value=${m.ref} ?selected=${m.ref === this.model} ?disabled=${!m.own && s.instanceBlocked}>${m.label} · ${m.provider}${m.own ? ` (${t("ai_chat.own_key")})` : ""}${m.price && (m.price.input || m.price.output) ? ` · ${t("ai_chat.model_price", { in: m.price.input, out: m.price.output })}` : ""}</option>`)}
      </select>` : ms.length === 1 ? html`<span>${ms[0].label}</span>` : nothing}
      ${conv}
      ${part(t("ai_chat.budget_you"), s.userUsed, s.userLimit, s.userPercent)}
      ${s.orgLimit || s.orgUsed ? part(t("ai_chat.budget_org"), s.orgUsed, s.orgLimit, s.orgPercent) : nothing}
      ${s.orgBudget ? part(t("ai_chat.budget_org_all"), s.orgSpent, s.orgBudget, s.orgBudgetPercent) : nothing}
      ${s.canRequest ? (s.requestPending ? html`<span>${t("budget_req.pending_mine")}</span>`
        : html`<button class="ghost" style="padding:1px 8px;font-size:12px" @click=${this.askMore}>${t("budget_req.ask")}</button>`) : nothing}
    </div>`;
  }

  private askMore = () => {
    const d = this.renderRoot.querySelector<PhxBudgetRequest>("phx-budget-request");
    if (!d || !this.status?.orgSlug) return;
    d.org = this.status.orgSlug;
    d.orgName = this.status.orgName ?? "";
    d.show();
  };

  private empty() {
    const starters = this.doc ? ["ai_chat.start.concept", "ai_chat.start.improve", "ai_chat.start.cards"] : ["ai_chat.start.new_concept", "ai_chat.start.plan", "ai_chat.start.overview"];
    return html`<div class="empty">
      <p>${t(this.doc ? "ai_chat.empty_page" : "ai_chat.empty")}</p>
      <div class="starters">${starters.map((k) => html`<button class="ghost" @click=${() => { this.draft = t(k as Key); this.renderRoot.querySelector("textarea")?.focus(); }}>${t(k as Key)}</button>`)}</div>
      ${this.status?.writeMode === "read" ? html`<p class="notice">${t("ai_chat.read_only")}</p>` : nothing}
    </div>`;
  }

  render() {
    const close = html`<button class="ghost" title=${t("ai_chat.close")} aria-label=${t("ai_chat.close")} @click=${() => this.dispatchEvent(new CustomEvent("close"))}>${icon("X", 15)}</button>`;
    if (this.status && !this.status.available && !this.view && !this.chats.length) {
      return html`<header><h2>${icon("BotMessageSquare", 16)} ${t("ai_chat.title")}</h2><span style="flex:1"></span>${close}</header>
        <div class="off">${icon("CircleAlert", 18)}<p>${tr(this.status.reason ?? "")}</p></div>`;
    }
    const results = toolResults(this.view?.messages ?? []);
    const live = this.live;
    const canSend = !!this.status?.available && !this.busy;
    return html`
      <header>
        <h2 title=${t("ai_chat.title")} aria-label=${t("ai_chat.title")}>${icon("BotMessageSquare", 16)}</h2>
        <select aria-label=${t("ai_chat.history")} @change=${(e: Event) => { const v = (e.target as HTMLSelectElement).value; v ? void this.open(v) : this.newChat(); }}>
          <option value="" ?selected=${!this.view}>${t("ai_chat.new")}</option>
          ${this.chats.map((c) => html`<option value=${c.id} ?selected=${this.view?.id === c.id}>${c.title || t("ai_chat.untitled")}</option>`)}
        </select>
        <button class="ghost" title=${t("ai_chat.new")} aria-label=${t("ai_chat.new")} @click=${this.newChat}>${icon("SquarePen", 15)}</button>
        ${this.view ? html`<button class="ghost" title=${t("ai_chat.delete")} aria-label=${t("ai_chat.delete")} @click=${this.removeChat}>${icon("Trash2", 15)}</button>` : nothing}
        ${close}
      </header>
      ${this.budget()}
      <phx-budget-request @sent=${() => { if (this.status) this.status = { ...this.status, requestPending: true }; }}></phx-budget-request>
      <div class="log" role="log" aria-live="polite" aria-busy=${this.busy ? "true" : "false"}>
        ${!this.view?.messages.length && !this.pending ? this.empty() : nothing}
        ${(this.view?.messages ?? []).map((m) => this.message(m, results))}
        ${this.pending ? html`<div class="msg user">${this.pending}</div>` : nothing}
        ${live ? html`
          ${live.tools.map((x) => html`<span class=${x.done && !x.ok ? "tool err" : "tool"}>${icon(x.done ? (x.ok ? "Check" : "CircleAlert") : "Loader", 12)} ${this.toolLabel(x.name)}</span>`)}
          ${live.text ? html`<div class="msg ai">${unsafeHTML(renderMarkdown(live.text, this.project))}</div>` : nothing}
          ${this.busy && !live.text ? html`<span class="typing" aria-label=${t("ai_chat.thinking")}><i></i><i></i><i></i></span>` : nothing}
          ${live.notice ? html`<p class="notice">${tr(live.notice)}</p>` : nothing}` : nothing}
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : nothing}
        ${this.status && !this.status.available ? html`<p class="notice">${tr(this.status.reason ?? "")}</p>` : nothing}
      </div>
      <form @submit=${this.send}>
        <textarea .value=${this.draft} placeholder=${t(this.doc ? "ai_chat.placeholder_page" : "ai_chat.placeholder")} aria-label=${t("ai_chat.message")}
          ?disabled=${!this.status?.available} @input=${(e: Event) => (this.draft = (e.target as HTMLTextAreaElement).value)} @keydown=${this.onKey}></textarea>
        <div class="row">
          <span class="hint">${t("ai_chat.hint")}</span>
          ${this.busy ? html`<button type="button" class="ghost" @click=${this.stop}>${icon("Pause", 14)} ${t("ai_chat.stop")}</button>`
            : html`<button type="submit" ?disabled=${!canSend || !this.draft.trim()}>${icon("Send", 14)} ${t("ai_chat.send")}</button>`}
        </div>
      </form>`;
  }
}
customElements.define("phx-ai-chat", PhxAIChat);
