import { LitElement, css, html, nothing } from "lit";
import { ApiError, api, type Board } from "../api";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { label, stateLabel, type PortalField } from "./feedback-portal";
import { base } from "./shared";
import { toast } from "./toasts";

interface PortalSettings { types: string[]; categories: string[]; states: string[]; defaultState: string; finishedState: string; fields: PortalField[]; publicView?: boolean; publicComments?: boolean }
interface Portal { id: string; handle: string; name: string; settings: PortalSettings }
interface Topic { id: string; portalId: string; title: string; summary: string; type: string; category: string; tags: string[]; priority: number; state: string; answer: string; reportCount: number; updatedAt: string; tickets: number; aiNote?: string; duplicateOf?: string; spam?: boolean }
interface FileMeta { id: string; name: string; mime: string; size: number }
interface Reply { id: string; body: string; createdAt: string }
interface Report { replies?: Reply[]; id: string; type: string; title: string; body: string; fields: Record<string, unknown>; context: Record<string, string>; userName?: string; userId?: string; email?: string; via: string; check?: string; deleted: boolean; createdAt: string; attachments?: FileMeta[] }
interface Ticket { taskId: string; title: string; boardSlug: string; board: string; column: string; done: boolean }
interface TopicEntity { id: string; slug: string; name: string; kind: string; source: "person" | "ai" }
interface PubComment { id: string; authorName: string; body: string; status: "pending" | "approved" | "rejected"; team: boolean; createdAt: string }
interface Detail extends Omit<Topic, "tickets"> { portal: Portal; reports: Report[]; tickets: Ticket[]; entities: TopicEntity[]; fixedIn?: string; question?: string; answerAuto?: boolean;
  public: boolean; votes: number; comments: PubComment[]; followers: number; forwards: Forward[]; trackers: { id: string; name: string; kind: string }[] }
interface Forward { id: string; trackerId: string; tracker: string; kind: string; ref: string; url: string; reportsSent: number; closed: boolean; lastError?: string }
interface List { portals: Portal[]; topics: Topic[]; owner: boolean }
interface Doc { id: string; slug: string; title: string; type: string; content?: Board }

const PRIO = ["", "P1", "P2", "P3", "P4"];

/** Feedback tab of a project: topics from its portals for moderators. */
export class PhxFeedback extends LitElement {
  static properties = { project: {}, list: { state: true }, detail: { state: true }, filter: { state: true }, boards: { state: true }, boardCols: { state: true }, busy: { state: true }, ents: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .pub { border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; display: grid; gap: 6px; }
    .comments { display: grid; gap: 8px; }
    .pcomment { border-top: 1px solid var(--line); padding-top: 8px; display: grid; gap: 4px; }
    .pcomment.pending { background: color-mix(in srgb, var(--signal) 8%, transparent); border-radius: 8px; padding: 8px; }
    .pcomment .body { white-space: pre-wrap; font-size: 14px; }
    .chip.warn { background: color-mix(in srgb, var(--signal) 18%, var(--surface)); }
    .bar { display: flex; gap: 8px; flex-wrap: wrap; align-items: end; margin-bottom: 14px; }
    .bar select, .bar input { width: auto; }
    .split { display: grid; grid-template-columns: minmax(0, 360px) minmax(0, 1fr); gap: 16px; align-items: start; }
    @media (max-width: 900px) { .split { grid-template-columns: minmax(0, 1fr); } }
    .list { display: grid; gap: 6px; }
    .row { text-align: left; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 8px; align-items: center; padding: 9px 10px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); color: var(--fg); font-weight: 400; }
    .row[aria-current=true] { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
    .row .t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .row small { color: var(--muted); }
    .prio { font: 600 11px var(--font-mono); padding: 2px 6px; border-radius: 6px; background: var(--surface2); }
    .prio.p1 { background: color-mix(in srgb, var(--bad) 18%, var(--surface)); color: var(--bad); }
    .prio.p2 { background: color-mix(in srgb, var(--signal) 20%, var(--surface)); color: var(--signal); }
    .count { font-size: 12px; color: var(--muted); display: inline-flex; gap: 4px; align-items: center; }
    .pane { border: 1px solid var(--line); border-radius: 14px; background: var(--surface); padding: 16px; display: grid; gap: 14px; min-width: 0; }
    .pane h2 { margin: 0; font-size: 19px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 8px; }
    .report { border-top: 1px solid var(--line); padding-top: 12px; display: grid; gap: 6px; }
    .report .meta { font-size: 12.5px; color: var(--muted); display: flex; gap: 8px; flex-wrap: wrap; }
    .report .body { white-space: pre-wrap; overflow-wrap: anywhere; }
    .report .reply { display: flex; gap: 8px; align-items: flex-start; padding: 8px 10px; border-left: 3px solid var(--accent); background: var(--surface2); border-radius: 0 8px 8px 0; }
    .fields { font-size: 13px; display: flex; gap: 6px; flex-wrap: wrap; }
    .fields span { background: var(--surface2); border-radius: 6px; padding: 2px 8px; }
    .att { display: flex; gap: 8px; flex-wrap: wrap; }
    .att img { max-width: 160px; max-height: 120px; border-radius: 8px; border: 1px solid var(--line); }
    .att video { max-width: 240px; border-radius: 8px; }
    .att a.file { display: inline-flex; gap: 6px; align-items: center; font-size: 13px; border: 1px solid var(--line); border-radius: 8px; padding: 4px 8px; }
    .tickets { display: grid; gap: 6px; }
    .ticket { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    .ticket .done { color: var(--good); }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .muted { color: var(--muted); font-size: 13px; }
    .empty { color: var(--muted); padding: 24px 0; }
    details summary { cursor: pointer; }
    .ai { display: flex; gap: 8px; align-items: flex-start; padding: 8px 10px; border-radius: 10px; background: var(--surface2); font-size: 13.5px; }
    .ai > div { display: grid; gap: 6px; }
    .chip.spam { background: var(--tb-red); color: var(--tc-red); }
    .report select, .actions select { width: auto; max-width: 100%; }
    .ents { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; margin-top: 6px; }
    .ent { display: inline-flex; gap: 4px; align-items: center; padding: 2px 4px 2px 8px; border-radius: 999px; background: var(--surface2); font-size: 13px; }
    .ent.ai { background: var(--accent-soft); }
    .ent button.icon { padding: 2px; min-height: 0; height: auto; border: 0; }
    input.entadd { width: 180px; }
  `];

  project = "";
  list?: List;
  detail?: Detail;
  filter = { portal: "", state: "", category: "", q: "" };
  boards: Doc[] = [];
  boardCols: { id: string; name: string }[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load().then(() => {
      const id = new URLSearchParams(location.search).get("topic");
      if (id) void this.open(id);
    });
    void api<Doc[]>("GET", `/projects/${this.project}/documents?type=board`).then((b) => (this.boards = b ?? [])).catch(() => {});
  }

  private async load() {
    const q = new URLSearchParams(Object.entries(this.filter).filter(([, v]) => v) as [string, string][]);
    try {
      this.list = await api<List>("GET", `/projects/${this.project}/feedback/topics?${q}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async open(id: string) {
    try {
      this.detail = await api<Detail>("GET", `/projects/${this.project}/feedback/topics/${id}`);
      const u = new URL(location.href);
      u.searchParams.set("topic", id);
      history.replaceState(history.state, "", u);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async patch(body: Record<string, unknown>, msg = t("settings.saved")) {
    if (!this.detail) return;
    try {
      this.detail = await api<Detail>("PATCH", `/projects/${this.project}/feedback/topics/${this.detail.id}`, body);
      toast(msg);
      void this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async post(path: string, body: Record<string, unknown>, msg: string) {
    try {
      this.detail = await api<Detail>("POST", `/projects/${this.project}/feedback/${path}`, body);
      toast(msg);
      void this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  /** Entries of the project for linking (loaded on first use). */
  ents: { slug: string; name: string }[] = [];

  private async loadEnts() {
    if (this.ents.length) return;
    this.ents = ((await api<{ slug: string; name: string }[]>("GET", `/projects/${this.project}/entities`).catch(() => [])) ?? []).slice(0, 500);
  }

  private async draftAnswer(d: Detail) {
    this.busy = true;
    try {
      const r = await api<{ text: string }>("POST", `/projects/${this.project}/feedback/topics/${d.id}/answer-draft`, { lang: "" });
      const ta = this.renderRoot.querySelector<HTMLTextAreaElement>("#answer");
      if (ta) { ta.value = r.text; ta.focus(); }
      toast(t("fb.answer_drafted"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async pickBoard(slug: string) {
    this.boardCols = [];
    if (!slug) return;
    const d = await api<Doc>("GET", `/projects/${this.project}/documents/${slug}`).catch(() => undefined);
    this.boardCols = d?.content?.columns ?? [];
  }

  private filters() {
    const l = this.list!;
    const portal = l.portals.find((p) => p.id === this.filter.portal);
    const states = [...new Set((portal ? [portal] : l.portals).flatMap((p) => p.settings.states))];
    const cats = [...new Set((portal ? [portal] : l.portals).flatMap((p) => p.settings.categories))];
    const set = (k: keyof PhxFeedback["filter"]) => (e: Event) => { this.filter = { ...this.filter, [k]: (e.target as HTMLSelectElement).value }; void this.load(); };
    return html`<div class="bar">
      ${l.portals.length > 1 ? html`<label class="field">${t("fb.portal")}<select @change=${set("portal")}><option value="">${t("fb.all")}</option>${l.portals.map((p) => html`<option value=${p.id}>${p.name}</option>`)}</select></label>` : nothing}
      <label class="field">${t("fb.state")}<select @change=${set("state")}><option value="">${t("fb.all")}</option>${states.map((s) => html`<option value=${s}>${stateLabel(s)}</option>`)}</select></label>
      ${cats.length ? html`<label class="field">${t("fb.category")}<select @change=${set("category")}><option value="">${t("fb.all")}</option>${cats.map((c) => html`<option>${c}</option>`)}</select></label>` : nothing}
      <label class="field">${t("fb.search")}<input type="search" .value=${this.filter.q} @change=${set("q")} placeholder=${t("fb.search_ph")}></label>
    </div>`;
  }

  /** Public view: release, votes, followers and comments to moderate. */
  private publicPart(d: Detail) {
    const s = d.portal.settings;
    return html`<div class="pub">
      <label class="row" style="gap:8px"><input type="checkbox" .checked=${d.public} @change=${(e: Event) => void this.patch({ public: (e.target as HTMLInputElement).checked })}>
        <strong>${t("fbpub.release")}</strong></label>
      <p class="muted" style="margin:2px 0 6px">${t("fbpub.release_help")} ${d.public ? html`<a href=${`/feedback/${d.portal.handle}?topic=${d.id}`} target="_blank" rel="noopener">${icon("ExternalLink", 12)}</a>` : nothing}</p>
      ${d.public ? html`<div class="muted">${t("fbpub.votes", { n: d.votes })} · ${t("fbpub.followers", { n: d.followers })}</div>` : nothing}
      ${s.publicComments && d.comments.length ? html`<div class="comments">${d.comments.filter((c) => c.status !== "rejected").map((c) => html`<div class="pcomment ${c.status}">
          <div><strong>${c.authorName}</strong>${c.team ? html` <span class="chip">${t("fbpub.team")}</span>` : nothing}${c.status === "pending" ? html` <span class="chip warn">${t("fbpub.pending")}</span>` : nothing}</div>
          <div class="body">${c.body}</div>
          ${c.status === "pending" ? html`<div class="row"><button class="ghost" @click=${() => void this.post(`topics/${d.id}/comments/${c.id}/approve`, {}, t("doc.saved"))}>${icon("Check", 13)} ${t("fbpub.approve")}</button>
            <button class="ghost" @click=${() => void this.post(`topics/${d.id}/comments/${c.id}/reject`, {}, t("doc.saved"))}>${icon("X", 13)} ${t("fbpub.reject")}</button></div>` : nothing}
        </div>`)}</div>` : nothing}
      ${s.publicComments && d.public ? html`<div class="row" style="align-items:flex-end">
        <label class="field" style="flex:1">${t("fbpub.team_comment")}<textarea rows="2" id="teamc" placeholder=${t("fbpub.team_comment_ph")}></textarea></label>
        <button class="ghost" @click=${() => { const el = this.renderRoot.querySelector("#teamc") as HTMLTextAreaElement; if (el.value.trim()) void this.post(`topics/${d.id}/comments`, { body: el.value }, t("doc.saved")); }}>${icon("Send", 13)}</button></div>` : nothing}
    </div>`;
  }

  private detailView(d: Detail) {
    const s = d.portal.settings;
    const others = (this.list?.topics ?? []).filter((x) => x.id !== d.id && x.portalId === d.portalId);
    const date = (v: string) => new Date(v).toLocaleString(locale(), { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
    const fieldLabel = (id: string) => label(s.fields.find((f) => f.id === id)?.label) || id;
    return html`<div class="pane">
      <div class="muted">${d.portal.name} · ${t(`fb.type.${d.type}` as any)} · ${t("fb.reports_n", { n: d.reportCount })}</div>
      <input aria-label=${t("fb.title")} .value=${d.title} style="font-size:18px;font-weight:600" @change=${(e: Event) => void this.patch({ title: (e.target as HTMLInputElement).value })}>
      <textarea aria-label=${t("fb.summary")} rows="3" .value=${d.summary} placeholder=${t("fb.summary")} @change=${(e: Event) => void this.patch({ summary: (e.target as HTMLTextAreaElement).value })}></textarea>
      ${d.aiNote || d.duplicateOf || d.spam ? html`<div class="ai">${icon("Sparkles", 14)}<div>
        ${d.aiNote ? html`<div>${t("fb.ai_note", { note: d.aiNote })}</div>` : nothing}
        ${d.spam ? html`<div class="row"><span class="chip spam">${t("fb.spam")}</span><button class="ghost" @click=${() => void this.patch({ spam: false })}>${t("fb.not_spam")}</button></div>` : nothing}
        ${d.duplicateOf ? html`<div class="row">${t("fb.duplicate_of", { title: others.find((o) => o.id === d.duplicateOf)?.title ?? "…" })}
          <button class="ghost" @click=${() => void this.post(`topics/${d.duplicateOf}/merge`, { from: d.id }, t("fb.merged"))}>${icon("GitMerge", 14)} ${t("fb.merge_into")}</button>
          <button class="ghost" @click=${() => void this.patch({ dismissDuplicate: true })}>${t("fb.not_duplicate")}</button></div>` : nothing}
      </div></div>` : nothing}
      <div class="grid">
        <label class="field">${t("fb.state")}<select @change=${(e: Event) => void this.patch({ state: (e.target as HTMLSelectElement).value })}>${s.states.map((x) => html`<option value=${x} ?selected=${x === d.state}>${stateLabel(x)}</option>`)}</select></label>
        <label class="field">${t("fb.priority")}<select @change=${(e: Event) => void this.patch({ priority: Number((e.target as HTMLSelectElement).value) })}>${[1, 2, 3, 4].map((p) => html`<option value=${p} ?selected=${p === d.priority}>${PRIO[p]} – ${t(`fb.prio.${p}` as any)}</option>`)}</select></label>
        <label class="field">${t("fb.category")}<select @change=${(e: Event) => void this.patch({ category: (e.target as HTMLSelectElement).value })}><option value="">–</option>${s.categories.map((c) => html`<option ?selected=${c === d.category}>${c}</option>`)}</select></label>
        <label class="field">${t("fb.tags")}<input .value=${d.tags.join(", ")} @change=${(e: Event) => void this.patch({ tags: (e.target as HTMLInputElement).value.split(",").map((x) => x.trim()).filter(Boolean) })}></label>
      </div>
      <label class="field">${t("fb.answer")}${d.answerAuto ? html` <span class="chip">${icon("Bot", 12)} ${t("fb.answer_was_auto")}</span>` : nothing}<textarea rows="3" .value=${d.answer} placeholder=${t("fb.answer_ph")} id="answer"></textarea></label>
      <div class="actions"><button @click=${() => void this.patch({ answer: (this.renderRoot.querySelector("#answer") as HTMLTextAreaElement).value }, t("fb.answer_sent"))}>${icon("Send", 14)} ${t("fb.answer_send")}</button>
        <button class="ghost" ?disabled=${this.busy} @click=${() => void this.draftAnswer(d)}>${icon("Sparkles", 14)} ${t("fb.answer_draft")}</button>
        <span class="muted">${t("fb.answer_hint")}</span></div>
      <label class="field">${t("fb.question")}<textarea rows="2" .value=${d.question ?? ""} placeholder=${t("fb.question_ph")} id="question"></textarea></label>
      <div class="actions"><button class="ghost" @click=${() => void this.patch({ question: (this.renderRoot.querySelector("#question") as HTMLTextAreaElement).value }, t("fb.question_sent"))}>${icon("CircleHelp", 14)} ${t("fb.question_send")}</button>
        <span class="muted">${t("fb.question_hint")}</span></div>
      <label class="field" style="max-width:260px">${t("fb.fixed_in")}<input .value=${d.fixedIn ?? ""} placeholder="1.5.0" @change=${(e: Event) => void this.patch({ fixedIn: (e.target as HTMLInputElement).value }, t("fb.fixed_in_saved"))}></label>
      ${s.publicView ? this.publicPart(d) : nothing}
      <div><strong>${t("fb.entities")}</strong>
        <div class="ents">${d.entities.map((x) => html`<span class="ent ${x.source}">${x.source === "ai" ? icon("Sparkles", 12) : nothing}<a href="/p/${this.project}/e/${x.slug}">${x.name}</a>
          <button class="ghost icon" aria-label=${t("fb.entity_remove", { name: x.name })} title=${t("fb.entity_remove", { name: x.name })} @click=${() => void this.patch({ entities: d.entities.filter((y) => y.id !== x.id).map((y) => y.slug) })}>${icon("X", 12)}</button></span>`)}
          <input class="entadd" list="fb-ents" aria-label=${t("fb.entity_add")} placeholder=${t("fb.entity_add")} @focus=${() => void this.loadEnts()}
            @change=${(e: Event) => { const v = (e.target as HTMLInputElement).value.trim().replace(/^@/, ""); if (!v) return; (e.target as HTMLInputElement).value = ""; void this.patch({ entities: [...d.entities.map((y) => y.slug), v] }); }}>
          <datalist id="fb-ents">${this.ents.map((x) => html`<option value=${x.slug}>${x.name}</option>`)}</datalist></div>
        ${d.entities.some((x) => x.source === "ai") ? html`<p class="muted" style="margin:4px 0 0">${t("fb.entities_ai")}</p>` : nothing}
      </div>

      <div><strong>${t("fb.tickets")}</strong>
        <div class="tickets">${d.tickets.length ? d.tickets.map((k) => html`<div class="ticket">${icon(k.done ? "CircleCheck" : "SquareKanban", 15)}
            <a href=${`/p/${this.project}/d/${k.boardSlug}?task=${k.taskId}`}>${k.title}</a><span class="muted">${k.board}</span>${k.done ? html`<span class="done">${t("fb.ticket_done")}</span>` : nothing}</div>`)
          : html`<p class="muted" style="margin:4px 0">${t("fb.no_tickets")}</p>`}</div>
        ${this.boards.length ? html`<div class="actions" style="margin-top:8px">
          <select id="board" aria-label=${t("fb.board")} @change=${(e: Event) => void this.pickBoard((e.target as HTMLSelectElement).value)}><option value="">${t("fb.pick_board")}</option>${this.boards.map((b) => html`<option value=${b.slug}>${b.title}</option>`)}</select>
          ${this.boardCols.length ? html`<select id="col" aria-label=${t("board.column")}>${this.boardCols.map((c) => html`<option value=${c.id}>${c.name}</option>`)}</select>` : nothing}
          <button class="ghost" @click=${() => { const b = (this.renderRoot.querySelector("#board") as HTMLSelectElement).value; if (!b) return; void this.post(`topics/${d.id}/tickets`, { board: b, column: (this.renderRoot.querySelector("#col") as HTMLSelectElement | null)?.value }, t("fb.ticket_created")); }}>${icon("SquareKanban", 14)} ${t("fb.to_board")}</button>
        </div><p class="muted">${t("fb.tickets_hint")}</p>` : html`<p class="muted">${t("fb.no_boards")}</p>`}
      </div>

      ${d.trackers.length || d.forwards.length ? html`<div><strong>${t("fbt.issues")}</strong>
        <div class="tickets">${d.forwards.map((f) => html`<div class="ticket">${icon(f.closed ? "CircleCheck" : f.kind === "jira" ? "SquareKanban" : "GitBranch", 15)}
            <a href=${f.url} target="_blank" rel="noopener">${f.kind === "jira" ? f.ref : `#${f.ref}`}</a><span class="muted">${f.tracker}</span>
            ${f.closed ? html`<span class="done">${t("fb.ticket_done")}</span>` : d.reportCount > f.reportsSent ? html`<span class="muted">${t("fbt.pending", { n: d.reportCount - f.reportsSent })}</span>` : nothing}
            ${f.lastError ? html`<span class="muted" title=${f.lastError}>${icon("TriangleAlert", 13)} ${t("fbt.sync_failed")}</span>` : nothing}</div>`)}</div>
        ${d.trackers.some((k) => !d.forwards.some((f) => f.trackerId === k.id)) ? html`<div class="actions" style="margin-top:8px">
          <select id="tracker" aria-label=${t("fbt.tracker")}>${d.trackers.filter((k) => !d.forwards.some((f) => f.trackerId === k.id)).map((k) => html`<option value=${k.id}>${k.name}</option>`)}</select>
          <button class="ghost" @click=${() => { const id = (this.renderRoot.querySelector("#tracker") as HTMLSelectElement).value; if (id) void this.post(`topics/${d.id}/forward`, { tracker: id }, t("fbt.forwarded")); }}>${icon("Send", 14)} ${t("fbt.forward")}</button>
        </div>` : nothing}
        ${d.forwards.length ? html`<div class="actions" style="margin-top:6px"><button class="ghost" @click=${() => void this.post(`topics/${d.id}/forwards/refresh`, {}, t("fbt.refreshed"))}>${icon("RefreshCw", 14)} ${t("fbt.refresh")}</button></div>` : nothing}
        <p class="muted">${t("fbt.forward_hint")}</p>
      </div>` : nothing}

      ${others.length ? html`<details><summary class="muted">${t("fb.merge_title")}</summary><div class="actions" style="margin-top:8px">
        <select id="merge" aria-label=${t("fb.merge_title")}>${others.map((o) => html`<option value=${o.id}>${o.title} (${o.reportCount})</option>`)}</select>
        <button class="ghost" @click=${() => void this.post(`topics/${d.id}/merge`, { from: (this.renderRoot.querySelector("#merge") as HTMLSelectElement).value }, t("fb.merged"))}>${icon("GitMerge", 14)} ${t("fb.merge")}</button></div></details>` : nothing}

      <div><strong>${t("fb.reports")}</strong>
        ${d.reports.map((r) => html`<div class="report">
          <div class="meta"><span>${r.deleted ? t("fb.reporter_deleted") : r.userName || (r.email ? r.email : t("fb.anonymous"))}</span><span>${date(r.createdAt)}</span>
            <span>${t(`fb.via.${r.via}` as any)}${r.check ? ` · ${t("fb.checked", { policy: r.check })}` : ""}</span></div>
          <strong>${r.title}</strong>
          ${r.body ? html`<div class="body">${r.body}</div>` : nothing}
          ${(r.replies ?? []).map((x) => html`<div class="reply">${icon("MessageSquare", 13)}<div><div class="muted">${t("fb.reporter_reply", { date: date(x.createdAt) })}</div><div class="body">${x.body}</div></div></div>`)}
          ${Object.keys(r.fields ?? {}).length ? html`<div class="fields">${Object.entries(r.fields).map(([k, v]) => html`<span>${fieldLabel(k)}: ${Array.isArray(v) ? v.join(", ") : String(v)}</span>`)}</div>` : nothing}
          ${r.attachments?.length ? html`<div class="att">${r.attachments.map((f) => {
            const url = `/api/v1/projects/${this.project}/feedback/attachments/${f.id}`;
            if (f.mime.startsWith("image/") && f.mime !== "image/svg+xml") return html`<a href=${url} target="_blank" rel="noopener"><img src=${url} alt=${f.name} loading="lazy"></a>`;
            if (f.mime.startsWith("video/")) return html`<video src=${url} controls preload="metadata"></video>`;
            return html`<a class="file" href=${url} target="_blank" rel="noopener">${icon("FileText", 14)} ${f.name} <small class="muted">${Math.ceil(f.size / 1024)} KB</small></a>`;
          })}</div>` : nothing}
          ${Object.keys(r.context ?? {}).length ? html`<details><summary class="muted">${t("fb.context")}</summary><div class="fields">${Object.entries(r.context).map(([k, v]) => html`<span>${k}: ${v}</span>`)}</div></details>` : nothing}
          ${d.reports.length > 1 || others.length ? html`<div class="actions">
            <select aria-label=${t("fb.move_to")} @change=${(e: Event) => { const v = (e.target as HTMLSelectElement).value; if (v !== "-") void this.post(`reports/${r.id}/move`, { topic: v === "new" ? "" : v }, t("fb.moved")); }}>
              <option value="-">${t("fb.move_to")}</option>${d.reports.length > 1 ? html`<option value="new">${t("fb.split")}</option>` : nothing}${others.map((o) => html`<option value=${o.id}>${o.title}</option>`)}</select></div>` : nothing}
        </div>`)}
      </div>
    </div>`;
  }

  render() {
    const l = this.list;
    if (!l) return html`<p class="muted">${t("app.loading")}</p>`;
    if (!l.portals.length) {
      return html`<div class="pane"><h2>${t("fb.nav")}</h2><p>${t("fb.no_portals")}</p>
        ${l.owner ? html`<a class="btn" href=${`/p/${this.project}/settings/feedback`}>${icon("Plus", 14)} ${t("fb.create_portal")}</a>` : nothing}</div>`;
    }
    return html`
      <div class="actions" style="margin-bottom:10px;justify-content:space-between">
        <div class="muted">${l.portals.map((p) => html`<a href=${`/feedback/${p.handle}`} target="_blank" rel="noopener">${icon("ExternalLink", 13)} /feedback/${p.handle}</a> `)}</div>
        ${l.owner ? html`<a class="btn ghost" href=${`/p/${this.project}/settings/feedback`}>${icon("Settings", 14)} ${t("fb.portal_settings")}</a>` : nothing}
      </div>
      ${this.filters()}
      <div class="split">
        <div class="list" role="list">${l.topics.length ? l.topics.map((x) => html`<button class="row" role="listitem" aria-current=${this.detail?.id === x.id} @click=${() => void this.open(x.id)}>
            <span class="prio p${x.priority}">${PRIO[x.priority]}</span>
            <span class="t">${x.title}${x.spam ? html` <span class="chip spam">${t("fb.spam")}</span>` : nothing}<br><small>${stateLabel(x.state)}${x.category ? ` · ${x.category}` : ""}</small></span>
            <span class="count" title=${t("fb.reports_n", { n: x.reportCount })}>${icon("Users", 12)} ${x.reportCount}${x.tickets ? html` ${icon("SquareKanban", 12)} ${x.tickets}` : nothing}</span></button>`)
          : html`<p class="empty">${t("fb.no_topics")}</p>`}</div>
        ${this.detail ? this.detailView(this.detail) : html`<div class="pane"><p class="muted">${t("fb.pick_topic")}</p></div>`}
      </div>`;
  }
}
customElements.get("phx-feedback") || customElements.define("phx-feedback", PhxFeedback);
