import { LitElement, css, html } from "lit";
import { ApiError, api, type Doc } from "../api";
import { diffLines, fold, type Hunk } from "../diff";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface VersionInfo { version: number; title: string; actor: string; actorName?: string; coauthors?: string[]; viaAI?: boolean; createdAt: string; content?: any }

/** Plain text of a version for comparing (sections for decisions). */
function textOf(type: Doc["type"], v: VersionInfo): string[] {
  const c = v.content ?? {};
  const lines = [`${t("history.title_label")}: ${v.title}`, ""];
  if (type === "markdown") lines.push(...String(c.markdown ?? "").split("\n"));
  if (type === "floorplan") lines.push(...String(c.description ?? "").split("\n"), "", ...(c.rooms ?? []).map((r: any) => `▢ ${r.name}${r.entity ? ` (@${r.entity})` : ""}`),
    ...(c.devices ?? []).map((d: any) => `• @${d.entity} ${Math.round(d.x)},${Math.round(d.y)}`));
  if (type === "board") lines.push(...String(c.description ?? "").split("\n"), "", ...(c.columns ?? []).map((col: any) => `- ${col.name}${col.done ? " ✓" : ""}`));
  if (type === "link") lines.push(`URL: ${c.url ?? ""}`, "", ...String(c.description ?? "").split("\n"));
  if (type === "adr") {
    lines.push(`${t("adr.status")}: ${c.status ? t(`adr.status.${c.status}` as any) : ""}`, `${t("adr.date")}: ${c.date ?? ""}`);
    if (c.supersedes) lines.push(`${t("adr.supersedes")}: ${c.supersedes}`);
    for (const [key, field] of [["adr.context", "context"], ["adr.decision", "decision"], ["adr.consequences", "consequences"], ["adr.alternatives", "alternatives"]] as const) {
      if (c[field]) lines.push("", `## ${t(key)}`, ...String(c[field]).split("\n"));
    }
  }
  return lines;
}

/** Version history of a document with a line diff and restore. */
export class PhxHistory extends LitElement {
  static properties = { project: {}, docSlug: {}, doc: { state: true }, versions: { state: true }, selected: { state: true }, base: { state: true }, hunks: { state: true }, error: { state: true }, busy: { state: true } };
  static styles = [base, css`
    .layout { display: grid; grid-template-columns: 260px minmax(0, 1fr); gap: 24px; margin-top: 16px; }
    @media (max-width: 900px) { .layout { grid-template-columns: 1fr; } }
    ol { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 2px; max-height: 70vh; overflow-y: auto; }
    ol button { width: 100%; text-align: left; display: block; background: none; border: 0; border-radius: 8px; padding: 8px 10px; color: var(--fg); font-weight: 400; }
    ol button[aria-current="true"] { background: var(--accent-soft); }
    @media (hover: hover) and (pointer: fine) { ol button:hover { background: var(--surface2); } ol button[aria-current="true"]:hover { background: var(--accent-soft); } }
    ol .v { font-weight: 600; font-size: 14px; } ol .meta { font-size: 12.5px; color: var(--muted); }
    .bar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; justify-content: space-between; margin-bottom: 10px; }
    .bar label { display: flex; gap: 8px; align-items: center; font-size: 14px; color: var(--muted); }
    .bar select { width: auto; }
    .diff { font-family: var(--font-mono); font-size: 13px; line-height: 1.55; border: 1px solid var(--line); border-radius: 8px; overflow-x: auto; background: var(--surface); }
    .diff div { white-space: pre-wrap; word-break: break-word; padding: 0 12px 0 28px; position: relative; min-height: 1.55em; }
    .diff div::before { position: absolute; left: 10px; color: var(--muted); }
    .diff .add { background: color-mix(in srgb, var(--good) 14%, transparent); } .diff .add::before { content: "+"; color: var(--good); }
    .diff .del { background: color-mix(in srgb, var(--bad) 12%, transparent); } .diff .del::before { content: "−"; color: var(--bad); }
    .diff .fold { color: var(--muted); font-family: var(--font-body); font-size: 12.5px; padding: 4px 12px; background: var(--surface2); }
    .stats { font-size: 13px; color: var(--muted); }
  `];
  project = "";
  docSlug = "";
  doc?: Doc;
  versions: VersionInfo[] = [];
  selected = 0;
  base = 0;
  hunks: Hunk[] = [];
  error = "";
  busy = false;
  private cache = new Map<number, VersionInfo>();
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.docSlug}`;
    if (key === this.loaded) return;
    this.loaded = key;
    this.cache.clear();
    try {
      this.doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
      this.versions = await api<VersionInfo[]>("GET", `/projects/${this.project}/documents/${this.docSlug}/versions`);
      await this.select(this.versions[0]?.version ?? 0);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async version(n: number): Promise<VersionInfo> {
    let v = this.cache.get(n);
    if (!v) {
      v = await api<VersionInfo>("GET", `/projects/${this.project}/documents/${this.docSlug}/versions/${n}`);
      this.cache.set(n, v);
    }
    return v;
  }

  private async select(n: number, against?: number) {
    if (!n || !this.doc) return;
    this.selected = n;
    this.base = against ?? (n > 1 ? n - 1 : 0);
    const to = await this.version(n);
    const from = this.base ? await this.version(this.base) : undefined;
    this.hunks = fold(diffLines(from ? textOf(this.doc.type, from) : [], textOf(this.doc.type, to)));
  }

  private async restore() {
    this.busy = true;
    try {
      const res = await api<{ applied: boolean; document?: Doc }>("POST", `/projects/${this.project}/documents/${this.docSlug}/versions/${this.selected}/restore`);
      toast(t("history.restored", { version: this.selected }));
      docsVersion.set(docsVersion.get() + 1);
      if (res.applied) navigate(`/p/${this.project}/d/${res.document?.slug ?? this.docSlug}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private diff() {
    const changes = this.hunks.filter((h) => h.op === "+" || h.op === "-");
    if (!changes.length) return html`<p class="muted">${t("history.no_changes")}</p>`;
    return html`<div class="diff" role="region" aria-label=${t("history.title")}>${this.hunks.map((h) =>
      h.op === "…" ? html`<div class="fold">${h.count === 1 ? t("history.unchanged_one") : t("history.unchanged", { count: h.count })}</div>`
      : html`<div class=${h.op === "+" ? "add" : h.op === "-" ? "del" : ""}>${h.text}</div>`)}</div>`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const d = this.doc;
    if (!d) return html`<p class="muted">${t("app.loading")}</p>`;
    const latest = this.versions[0]?.version;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    const added = this.hunks.filter((h) => h.op === "+").length;
    const removed = this.hunks.filter((h) => h.op === "-").length;
    return html`<div class="enter">
      <a href="/p/${this.project}/d/${d.slug}" class="muted" style="font-size:14px;text-decoration:none">${icon("ArrowLeft", 14)} ${t("history.back")}</a>
      <h1 style="margin-top:8px">${t("history.title")}: ${d.title}</h1>
      <div class="layout">
        <ol aria-label=${t("history.title")}>
          ${this.versions.map((v) => html`<li><button aria-current=${v.version === this.selected ? "true" : "false"} @click=${() => this.select(v.version)}>
            <span class="v">${t("history.version", { version: v.version })}${v.version === latest ? html` <span class="chip">${t("history.current")}</span>` : ""}</span>
            <div class="meta">${formatDate(v.createdAt)}${v.actorName ? html` · ${t("history.by", { name: [v.actorName, ...(v.coauthors ?? [])].join(", ") })}` : ""}${v.viaAI ? html` · ${icon("Bot", 12)} ${t("history.via_ai")}` : ""}</div>
          </button></li>`)}
        </ol>
        <div>
          <div class="bar">
            <label>${t("history.compare")}
              <select @change=${(e: Event) => this.select(this.selected, Number((e.target as HTMLSelectElement).value))}>
                ${this.versions.filter((v) => v.version < this.selected).map((v) => html`<option value=${v.version} ?selected=${v.version === this.base}>${t("history.version", { version: v.version })}</option>`)}
                <option value="0" ?selected=${this.base === 0}>–</option>
              </select>
            </label>
            <span class="stats">${t("history.changes", { added, removed })}</span>
            ${canEdit && this.selected !== latest ? html`<button ?disabled=${this.busy} @click=${this.restore}>${icon("ArrowLeft", 14)} ${t("history.restore")}</button>` : ""}
          </div>
          ${this.diff()}
        </div>
      </div>
    </div>`;
  }
}
customElements.define("phx-history", PhxHistory);
