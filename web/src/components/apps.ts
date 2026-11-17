// Apps (apps-definition): the frame that runs an app and answers its bridge
// calls, the app's page, and the editor with live preview and console.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api, type Doc } from "../api";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { docsVersion, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";
import "./store-items";
import type { AppShareView, PhxAppShare } from "./app-share";
import "./app-share";
import type { PhxBotcheck } from "./botcheck";
import "./botcheck";

export interface AppSource { id: string; kind: "query" | "board" | "checks" | "entities"; ref?: string; label?: string }
export interface AppDiag { file: string; line: number; column: number; text: string }
export interface AppContent { files: Record<string, string>; level: number; sources?: AppSource[]; storageMb?: number; ai?: boolean; build?: { errors: AppDiag[] } }
export interface AppFrameInfo { url: string; isolated?: boolean; level: number; granted: number; sources: AppSource[]; ai: boolean; storageMb: number; errors: AppDiag[]; canEdit: boolean; maxLevel?: number; snapshotAt?: string }
export interface ConsoleEntry { level: string; text: string; line?: number; col?: number; file?: string }

type Proposal =
  | { kind: "task"; board: string; title: string; description?: string; column?: string }
  | { kind: "comment"; document: string; body: string }
  | { kind: "entity"; entityKind: string; name: string; attrs?: Record<string, unknown> };

const LEVELS = [0, 1, 2, 3] as const;

function isDark() {
  const d = document.documentElement.dataset.theme;
  return d === "dark" || (d !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
}

/** Runs an app in a sandboxed frame and answers its bridge calls. */
export class PhxAppFrame extends LitElement {
  static properties = {
    project: {}, doc: {}, info: { attribute: false }, autoHeight: { type: Boolean, attribute: "auto-height" }, width: {}, dark: {}, shared: {},
    asking: { state: true }, proposal: { state: true },
  };
  static styles = [base, css`
    :host { display: block; position: relative; }
    .stage { display: grid; justify-items: center; height: 100%; }
    iframe { display: block; width: 100%; height: 100%; min-height: 240px; border: 0; background: #fff; border-radius: inherit; }
    :host([auto-height]) iframe { height: 400px; }
    dialog { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); padding: 20px; width: min(460px, 92vw); box-shadow: 0 18px 50px rgb(0 0 0 / .2); }
    dialog::backdrop { background: rgb(0 0 0 / .35); }
    dialog h2 { font-size: 18px; margin-bottom: 8px; }
    dialog ul { margin: 8px 0 14px; padding-left: 18px; display: grid; gap: 4px; font-size: 14px; }
    dialog .row { justify-content: flex-end; }
    dialog pre { background: var(--surface2); border-radius: 8px; padding: 10px; font: 12.5px/1.5 var(--font-mono); white-space: pre-wrap; max-height: 260px; overflow: auto; }
    .lvl { display: grid; gap: 8px; margin: 12px 0; }
    .lvl div { display: flex; gap: 10px; align-items: flex-start; font-size: 14px; }
    .lvl div .ico { color: var(--accent); margin-top: 2px; }
  `];

  project = "";
  doc = "";
  /** Address of a public app: reads come from its snapshot, nothing else. */
  shared = "";
  info?: AppFrameInfo;
  autoHeight = false;
  width = "";
  dark?: boolean;
  asking = false;
  proposal?: Proposal;
  private grantWait?: (level: number) => void;
  private proposalWait?: (ok: boolean) => void;
  private consoleBuf: ConsoleEntry[] = [];
  private consoleTimer = 0;
  private themeObserver?: MutationObserver;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("message", this.onMessage);
    this.themeObserver = new MutationObserver(() => this.sendTheme());
    this.themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("message", this.onMessage);
    this.themeObserver?.disconnect();
  }

  updated(ch: Map<string, unknown>) {
    if (ch.has("dark")) this.sendTheme();
  }

  private get frame() {
    return this.renderRoot.querySelector("iframe");
  }

  private sendTheme = () => {
    const w = this.frame?.contentWindow;
    if (!w) return;
    const cs = getComputedStyle(document.documentElement);
    const tokens: Record<string, string> = {};
    for (const k of ["bg", "surface", "surface2", "line", "fg", "muted", "accent", "accent-soft", "good", "bad", "signal", "font-body", "font-mono"]) tokens[k] = cs.getPropertyValue(`--${k}`).trim();
    w.postMessage({ phx: 1, event: "theme", dark: this.dark ?? isDark(), tokens }, "*");
  };

  private reply(id: number | undefined, result?: unknown, error?: string, code?: string) {
    if (id === undefined) return;
    this.frame?.contentWindow?.postMessage({ phx: 1, id, result, error, code }, "*");
  }

  private base() {
    return `/projects/${this.project}/apps/${encodeURIComponent(this.doc)}`;
  }

  private granted() {
    return this.info ? Math.min(this.info.granted, this.info.level) : -1;
  }

  private onMessage = async (e: MessageEvent) => {
    if (!this.frame || e.source !== this.frame.contentWindow) return;
    const m = e.data as { phx?: number; id?: number; op?: string; args?: any };
    if (!m || m.phx !== 1 || !m.op) return;
    const a = m.args ?? {};
    try {
      switch (m.op) {
        case "ready":
          this.sendTheme();
          if (this.autoHeight) this.resize(a.height);
          return;
        case "resize":
          if (this.autoHeight) this.resize(a.height);
          return;
        case "console":
          this.log({ level: String(a.level ?? "error"), text: String(a.text ?? ""), line: a.line, col: a.col, file: a.file });
          return;
        case "info":
          return this.reply(m.id, { level: this.info?.level ?? 0, granted: this.granted(), project: this.shared ? "" : this.project, ...(this.shared ? { public: true, snapshotAt: this.info?.snapshotAt ?? null } : {}), sources: (this.info?.sources ?? []).map((s) => ({ id: s.id, kind: s.kind, label: s.label ?? "" })) });
        case "grant":
          return this.reply(m.id, this.shared ? this.granted() : await this.ask());
        case "read":
          await this.need(1);
          if (this.shared) return this.reply(m.id, await api("POST", `/shared-apps/${encodeURIComponent(this.shared)}/read`, { source: String(a.source ?? "") }));
          return this.reply(m.id, await api("POST", `${this.base()}/read`, { source: String(a.source ?? "") }));
        case "storage.get":
          await this.need(2);
          return this.reply(m.id, await api("GET", `${this.base()}/storage/${encodeURIComponent(a.key)}${a.personal ? "?personal=1" : ""}`));
        case "storage.list":
          await this.need(2);
          return this.reply(m.id, await api("GET", `${this.base()}/storage${a.personal ? "?personal=1" : ""}`));
        case "storage.set":
          await this.need(2);
          await api("PUT", `${this.base()}/storage/${encodeURIComponent(a.key)}${a.personal ? "?personal=1" : ""}`, { value: a.value ?? null });
          return this.reply(m.id, true);
        case "storage.delete":
          await this.need(2);
          await api("DELETE", `${this.base()}/storage/${encodeURIComponent(a.key)}${a.personal ? "?personal=1" : ""}`);
          return this.reply(m.id, true);
        case "write":
          await this.need(3);
          return this.reply(m.id, await this.write(a as Proposal));
        case "open": {
          if (this.shared) return this.reply(m.id, false);
          const ref = String(a.ref ?? "");
          if (/^[\w@.-]+$/.test(ref)) navigate(ref.startsWith("@") ? `/p/${this.project}/e/${ref.slice(1)}` : `/p/${this.project}/d/${ref}`);
          return this.reply(m.id, true);
        }
        case "ai": {
          if (!this.info?.ai) return this.reply(m.id, undefined, t("app.ai_unavailable"), "ai_unavailable");
          if (this.shared) {
            const botToken = await this.renderRoot.querySelector<PhxBotcheck>("phx-botcheck")?.token();
            const r = await api<{ text: string; json?: unknown }>("POST", `/shared-apps/${encodeURIComponent(this.shared)}/ai`, { prompt: String(a.prompt ?? ""), json: !!a.json, botToken });
            return this.reply(m.id, a.json && r.json !== undefined ? r.json : r.text);
          }
          const r = await api<{ text: string; json?: unknown }>("POST", `${this.base()}/ai`, { prompt: String(a.prompt ?? ""), json: !!a.json });
          return this.reply(m.id, a.json && r.json !== undefined ? r.json : r.text);
        }
        default:
          return this.reply(m.id, undefined, "unknown call", "unknown");
      }
    } catch (err) {
      const code = err instanceof ApiError ? err.code : err instanceof Error ? err.message : "error";
      this.reply(m.id, undefined, err instanceof ApiError ? err.text : String(err instanceof Error ? err.message : err), code);
    }
  };

  private resize(h: number) {
    const f = this.frame;
    if (f && Number.isFinite(h)) f.style.height = `${Math.min(Math.max(h, 120), 2000)}px`;
  }

  private log(c: ConsoleEntry) {
    this.dispatchEvent(new CustomEvent<ConsoleEntry>("console", { detail: c, bubbles: true, composed: true }));
    if (!this.doc || (c.level !== "error" && c.level !== "warn")) return;
    this.consoleBuf.push(c);
    clearTimeout(this.consoleTimer);
    this.consoleTimer = window.setTimeout(() => {
      const logs = this.consoleBuf.splice(0, 20);
      void api("POST", `${this.base()}/console`, { logs }).catch(() => undefined);
    }, 1500);
  }

  /** Asks the person for the level the app wants, once. */
  private async ask(): Promise<number> {
    const want = this.info?.level ?? 0;
    if (this.granted() >= want) return this.granted();
    this.asking = true;
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog.grant")?.showModal();
    const lvl = await new Promise<number>((r) => (this.grantWait = r));
    this.asking = false;
    if (lvl >= 0) {
      const res = await api<{ granted: number }>("PUT", `${this.base()}/grant`, { level: lvl });
      if (this.info) this.info = { ...this.info, granted: res.granted };
      this.dispatchEvent(new CustomEvent<number>("granted", { detail: res.granted, bubbles: true, composed: true }));
      return res.granted;
    }
    return this.granted();
  }

  private async need(level: number) {
    if (this.granted() >= level) return;
    if ((this.info?.level ?? 0) < level) throw new Error("not_allowed");
    if ((await this.ask()) < level) throw new Error("not_allowed");
  }

  private async write(p: Proposal) {
    if (!p || !["task", "comment", "entity"].includes((p as any).kind)) throw new Error("bad_proposal");
    this.proposal = p;
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog.propose")?.showModal();
    const ok = await new Promise<boolean>((r) => (this.proposalWait = r));
    this.proposal = undefined;
    if (!ok) throw new Error("declined");
    const pr = `/projects/${this.project}`;
    if (p.kind === "task") return api("POST", `${pr}/boards/${encodeURIComponent(p.board)}/tasks`, { title: p.title, description: p.description ?? "", column: p.column ?? "" });
    if (p.kind === "comment") return api("POST", `${pr}/documents/${encodeURIComponent(p.document)}/comments`, { body: p.body });
    return api("POST", `${pr}/entities`, { kind: p.entityKind, name: p.name, attrs: p.attrs ?? {} });
  }

  private grantDialog() {
    const want = this.info?.level ?? 0;
    const close = (lvl: number) => {
      this.renderRoot.querySelector<HTMLDialogElement>("dialog.grant")?.close();
      this.grantWait?.(lvl);
      this.grantWait = undefined;
    };
    return html`<dialog class="grant" aria-labelledby="gt" @cancel=${(e: Event) => { e.preventDefault(); close(-1); }}>
      <h2 id="gt">${t("app.grant_title")}</h2>
      <p class="muted">${t("app.grant_text")}</p>
      <div class="lvl">${LEVELS.filter((l) => l >= 1 && l <= want).map((l) => html`<div>${icon(["", "Eye", "HardDrive", "Pencil"][l], 16)}<span>${t(`app.level${l}_long` as Key, { mb: this.info?.storageMb ?? 10 })}</span></div>`)}</div>
      ${want >= 1 && this.info?.sources.length ? html`<ul>${this.info.sources.map((s) => html`<li>${s.label || s.id} <span class="muted">(${t(`app.kind.${s.kind}` as Key)}${s.ref ? `: ${s.ref}` : ""})</span></li>`)}</ul>` : nothing}
      <div class="row"><button class="ghost" @click=${() => close(-1)}>${t("app.grant_no")}</button><button @click=${() => close(want)}>${t("app.grant_yes")}</button></div>
    </dialog>`;
  }

  private proposalDialog() {
    const p = this.proposal;
    const close = (ok: boolean) => {
      this.renderRoot.querySelector<HTMLDialogElement>("dialog.propose")?.close();
      this.proposalWait?.(ok);
      this.proposalWait = undefined;
    };
    const what = !p ? "" : p.kind === "task" ? t("app.propose_task", { board: p.board })
      : p.kind === "comment" ? t("app.propose_comment", { document: p.document }) : t("app.propose_entity", { kind: p.entityKind });
    const shown = !p ? {} : p.kind === "task" ? { title: p.title, description: p.description, column: p.column } : p.kind === "comment" ? { body: p.body } : { name: p.name, attrs: p.attrs };
    return html`<dialog class="propose" aria-labelledby="pt" @cancel=${(e: Event) => { e.preventDefault(); close(false); }}>
      <h2 id="pt">${t("app.propose_title")}</h2>
      <p>${what}</p>
      <pre>${JSON.stringify(shown, null, 2)}</pre>
      <div class="row"><button class="ghost" @click=${() => close(false)}>${t("app.propose_no")}</button><button @click=${() => close(true)}>${t("app.propose_yes")}</button></div>
    </dialog>`;
  }

  render() {
    if (!this.info) return nothing;
    return html`<div class="stage">
        <iframe src=${this.info.url} title=${t("app.frame_title")} style=${this.width ? `max-width:${this.width}` : ""}
          sandbox=${this.info.isolated ? "allow-scripts allow-same-origin allow-forms allow-modals allow-popups allow-downloads" : "allow-scripts allow-forms allow-modals allow-popups allow-downloads"} referrerpolicy="no-referrer"
          @load=${this.sendTheme}></iframe>
      </div>
      ${this.shared && this.info.ai ? html`<phx-botcheck place="apps"></phx-botcheck>` : nothing}
      ${this.asking ? this.grantDialog() : nothing}
      ${this.proposal ? this.proposalDialog() : nothing}`;
  }
}

/** An app's page: the running app with a bar above it. */
/** The store template an app was made from. */
function appOrigin(doc: Doc): { id: string; version: string; name?: string } | undefined {
  return (doc.content as { origin?: { id: string; version: string; name?: string } } | undefined)?.origin;
}

export class PhxAppView extends LitElement {
  static properties = { project: {}, doc: { attribute: false }, info: { state: true }, error: { state: true }, full: { state: true }, isTemplate: { state: true }, share: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .bar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 10px; }
    .bar .sp { flex: 1; }
    .chip.lvl { gap: 5px; }
    .box { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); overflow: hidden; height: calc(100dvh - var(--header-h) - 190px); min-height: 360px; background: var(--surface); }
    :host([full]) .box { position: fixed; inset: var(--header-h) 0 0 0; height: auto; z-index: 40; border-radius: 0; border: 0; }
    phx-app-frame { height: 100%; }
    .errs { color: var(--bad); font: 12.5px/1.5 var(--font-mono); background: color-mix(in srgb, var(--bad) 8%, var(--surface)); padding: 10px 12px; border-radius: 8px; margin-bottom: 10px; white-space: pre-wrap; }
  `];

  project = "";
  doc?: Doc;
  info?: AppFrameInfo;
  error = "";
  full = false;

  // Properties set before the lazy element is defined arrive with the first
  // update, not in connectedCallback.
  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("doc") && this.doc) void this.load();
  }

  private async load() {
    if (!this.doc) return;
    try {
      const info = await api<AppFrameInfo>("POST", `/projects/${this.project}/apps/${this.doc.id}/open`);
      void api<AppShareView>("GET", `/projects/${this.project}/apps/${this.doc.id}/share`).then((v) => (this.share = v)).catch(() => undefined);
      void api<{ id: string }[]>("GET", `/projects/${this.project}/app-templates`).then((l) => (this.isTemplate = l.some((x) => x.id === this.doc?.id))).catch(() => undefined);
      // The instance or organization may allow less than the app asks for.
      if (info.maxLevel !== undefined && info.maxLevel < info.level) info.level = info.maxLevel;
      this.info = info;
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  /** The app is offered as template in its project (New → App from a template). */
  isTemplate = false;

  private async toggleTemplate() {
    if (!this.doc) return;
    try {
      const list = await api<{ id: string }[]>("PUT", `/projects/${this.project}/apps/${this.doc.id}/template`, { on: !this.isTemplate });
      this.isTemplate = list.some((x) => x.id === this.doc!.id);
      toast(t(this.isTemplate ? "app.template_on" : "app.template_off"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  /** Sharing state (owners may release the app publicly). */
  share?: AppShareView;

  private setFull(on: boolean) {
    this.full = on;
    this.toggleAttribute("full", on);
  }

  render() {
    const i = this.info;
    if (this.error) return html`<p class="error">${this.error}</p>`;
    if (!i || !this.doc) return nothing;
    const granted = Math.min(i.granted, i.level);
    return html`<div class="bar">
        <span class="chip lvl" title=${t("app.level_help")}>${icon(["Box", "Eye", "HardDrive", "Pencil"][i.level], 12)} ${t(`app.level${i.level}` as Key)}</span>
        ${i.level > 0 ? html`<span class="chip">${granted >= i.level ? t("app.allowed") : t("app.not_allowed_yet")}</span>` : nothing}
        ${this.share?.shared ? html`<span class="chip" title=${t("appshare.public_help")}>${icon("Globe", 12)} ${t("appshare.public")}</span>` : nothing}
        <span class="sp"></span>
        ${this.share?.canEdit ? html`<button class="ghost" @click=${() => this.renderRoot.querySelector<PhxAppShare>("phx-app-share")?.open()}>${icon("Globe", 14)} ${t("appshare.button")}</button>` : nothing}
        ${i.canEdit ? html`<button class="ghost" aria-pressed=${this.isTemplate ? "true" : "false"} title=${t("app.template_help")} @click=${() => this.toggleTemplate()}>${icon("Copy", 14)} ${t(this.isTemplate ? "app.is_template" : "app.make_template")}</button>
          <a class="btn ghost" href="/p/${this.project}/d/${this.doc.slug}/app-edit">${icon("Code", 14)} ${t("app.edit")}</a>` : nothing}
        <button class="ghost" @click=${() => this.setFull(!this.full)}>${icon(this.full ? "Minimize2" : "Maximize2", 14)} ${t(this.full ? "app.exit_full" : "app.full")}</button>
      </div>
      ${appOrigin(this.doc) ? html`<phx-app-origin style="margin-bottom:10px" .project=${this.project} .doc=${this.doc.id} .origin=${appOrigin(this.doc)} ?canEdit=${i.canEdit}
        @reload=${() => location.reload()}></phx-app-origin>` : nothing}
      ${i.errors.length ? html`<div class="errs" role="alert">${i.errors.map((e) => `${e.file}:${e.line}:${e.column} ${e.text}`).join("\n")}</div>` : nothing}
      <div class="box"><phx-app-frame .project=${this.project} .doc=${this.doc.id} .info=${i}
        @granted=${(e: CustomEvent<number>) => (this.info = { ...i, granted: e.detail })}></phx-app-frame></div>
      ${this.share?.canEdit ? html`<phx-app-share .project=${this.project} .doc=${this.doc.id} .view=${this.share} @changed=${(e: CustomEvent<AppShareView>) => (this.share = e.detail)}></phx-app-share>` : nothing}`;
  }
}

const STARTER: Record<string, string> = {
  "app.tsx": `import { useState } from "react";
import { createRoot } from "react-dom/client";

function App() {
  const [count, setCount] = useState(0);
  return (
    <main>
      <h1>My app</h1>
      <button onClick={() => setCount(count + 1)}>Clicked {count} times</button>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(<App />);
`,
  "style.css": `body { font-family: var(--phx-font-body, system-ui, sans-serif); margin: 24px; color: var(--phx-fg, #1f1a17); background: var(--phx-bg, #fff); }
button { font: inherit; padding: 8px 14px; border-radius: 8px; border: 0; background: var(--phx-accent, #b84a0c); color: #fff; cursor: pointer; }
`,
};

/** A new app starts with a small React example. */
export const starterApp = (): AppContent => ({ files: { ...STARTER }, level: 0 });

/** The app editor: files, manifest, live preview with sizes and modes, console. */
export class PhxAppEditor extends LitElement {
  static properties = {
    project: {}, docSlug: {},
    doc: { state: true }, app: { state: true }, file: { state: true }, preview: { state: true }, logs: { state: true },
    size: { state: true }, mode: { state: true }, dirty: { state: true }, busy: { state: true }, tab: { state: true }, error: { state: true },
  };
  static styles = [base, css`
    :host { display: grid; grid-template-rows: auto 1fr; height: calc(100dvh - var(--header-h)); margin: calc(-1 * var(--pad, 0px)); }
    .top { display: flex; gap: 8px; align-items: center; padding: 10px 14px; border-bottom: 1px solid var(--line); background: var(--surface); flex-wrap: wrap; }
    .top h1 { font-size: 17px; margin-right: 8px; }
    .top .sp { flex: 1; }
    .top .state { font-size: 13px; color: var(--muted); }
    .split { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); min-height: 0; }
    .left { display: grid; grid-template-rows: auto 1fr; min-height: 0; border-right: 1px solid var(--line); }
    .tabs { display: flex; gap: 2px; padding: 6px 8px 0; border-bottom: 1px solid var(--line); overflow-x: auto; background: var(--bg); }
    .tabs button { border-radius: 8px 8px 0 0; background: transparent; color: var(--muted); font-weight: 500; font-size: 13px; padding: 6px 10px; border: 1px solid transparent; border-bottom: 0; white-space: nowrap; }
    .tabs button[aria-selected="true"] { background: var(--surface); color: var(--fg); border-color: var(--line); margin-bottom: -1px; }
    .tabs button.add { color: var(--accent); }
    textarea.code { white-space: pre; overflow: auto; border: 0; border-radius: 0; resize: none; min-height: 0; height: 100%; padding: 14px; font: 13px/1.6 var(--font-mono); tab-size: 2; background: var(--surface); outline: none; }
    .manifest { padding: 14px; overflow: auto; display: grid; gap: 14px; align-content: start; background: var(--surface); }
    .manifest h3 { font-size: 14px; }
    .levels { display: grid; gap: 6px; }
    .levels label { display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; padding: 9px 11px; border: 1px solid var(--line); border-radius: 10px; cursor: pointer; font-size: 14px; }
    .levels label:has(input:checked) { border-color: var(--accent); background: color-mix(in srgb, var(--accent-soft) 45%, transparent); }
    .levels input { grid-row: span 2; width: auto; margin-top: 3px; accent-color: var(--accent); }
    .levels span { color: var(--muted); font-size: 12.5px; }
    .src { display: grid; grid-template-columns: 1fr 1fr 1.3fr 1.3fr auto; gap: 6px; align-items: center; }
    .src input, .src select { padding: 6px 8px; font-size: 13px; }
    .right { display: grid; grid-template-rows: auto 1fr auto; min-height: 0; background: var(--bg); }
    .pvbar { display: flex; gap: 6px; align-items: center; padding: 8px 10px; border-bottom: 1px solid var(--line); flex-wrap: wrap; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { height: 30px; padding: 0 9px; border: 0; border-radius: 0; background: var(--surface); color: var(--muted); font-weight: 500; font-size: 12.5px; }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
    .pv { padding: 12px; min-height: 0; overflow: auto; display: grid; }
    .pv phx-app-frame { height: 100%; min-height: 300px; border: 1px solid var(--line); border-radius: 10px; overflow: hidden; background: #fff; justify-self: center; width: 100%; }
    .console { max-height: 180px; overflow: auto; border-top: 1px solid var(--line); background: var(--surface); font: 12px/1.5 var(--font-mono); }
    .console summary { padding: 6px 10px; cursor: pointer; font-family: var(--font-body); font-size: 13px; }
    .console div { padding: 3px 10px; border-top: 1px solid var(--surface2); white-space: pre-wrap; overflow-wrap: anywhere; }
    .console .error { color: var(--bad); } .console .warn { color: var(--signal); }
    .count { display: inline-grid; place-items: center; min-width: 18px; height: 18px; padding: 0 5px; border-radius: 9px; background: var(--bad); color: var(--on-color); font-size: 11px; margin-left: 4px; }
    @media (max-width: 900px) { :host { height: auto; } .split { grid-template-columns: 1fr; } .left { min-height: 60vh; } .right { min-height: 70vh; } }
  `];

  project = "";
  docSlug = "";
  doc?: Doc;
  app: AppContent = starterApp();
  file = "";
  preview?: AppFrameInfo;
  logs: ConsoleEntry[] = [];
  size: "phone" | "tablet" | "desktop" = "desktop";
  mode: "light" | "dark" = isDark() ? "dark" : "light";
  dirty = false;
  busy = false;
  tab: "code" | "manifest" = "code";
  error = "";
  private timer = 0;

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("docSlug") && this.docSlug) void this.load();
  }

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("keydown", this.onKey);
    window.addEventListener("beforeunload", this.onUnload);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("keydown", this.onKey);
    window.removeEventListener("beforeunload", this.onUnload);
  }

  private onKey = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "s") {
      e.preventDefault();
      void this.save();
    }
  };

  private onUnload = (e: BeforeUnloadEvent) => {
    if (this.dirty) e.preventDefault();
  };

  private async load() {
    try {
      this.doc = await api<Doc>("GET", `/projects/${this.project}/documents/${this.docSlug}`);
      const c = (this.doc.content ?? {}) as AppContent;
      this.app = { files: { ...(c.files ?? {}) }, level: c.level ?? 0, sources: [...(c.sources ?? [])], storageMb: c.storageMb ?? 10, ai: !!c.ai };
      this.file = Object.keys(this.app.files).sort((a, b) => order(a) - order(b))[0] ?? "";
      void this.refresh();
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private changed() {
    this.dirty = true;
    clearTimeout(this.timer);
    this.timer = window.setTimeout(() => this.refresh(), 700);
  }

  private async refresh() {
    try {
      this.logs = [];
      this.preview = await api<AppFrameInfo>("POST", `/projects/${this.project}/apps/preview?doc=${this.doc?.id ?? ""}`, { ...this.app, build: undefined });
      this.error = "";
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async save() {
    if (!this.doc || this.busy) return;
    this.busy = true;
    try {
      await api("PATCH", `/projects/${this.project}/documents/${this.doc.id}`, { app: { ...this.app, build: undefined } });
      this.dirty = false;
      toast(t("doc.saved"));
      docsVersion.set(docsVersion.get() + 1);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private addFile() {
    const name = prompt(t("app.file_name"), "util.ts")?.trim();
    if (!name) return;
    if (!/^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,60}\.(html|css|js|mjs|ts|tsx|jsx|json|svg|md|txt)$/.test(name)) {
      toast(t("validation.app_file_name", { name }), "error");
      return;
    }
    if (this.app.files[name] === undefined) this.app = { ...this.app, files: { ...this.app.files, [name]: "" } };
    this.file = name;
    this.tab = "code";
    this.changed();
  }

  private removeFile(name: string) {
    if (!confirm(t("app.remove_file", { name }))) return;
    const files = { ...this.app.files };
    delete files[name];
    this.app = { ...this.app, files };
    this.file = Object.keys(files)[0] ?? "";
    this.changed();
  }

  private setManifest(p: Partial<AppContent>) {
    this.app = { ...this.app, ...p };
    this.changed();
  }

  private setSource(i: number, p: Partial<AppSource>) {
    const sources = [...(this.app.sources ?? [])];
    sources[i] = { ...sources[i], ...p };
    this.setManifest({ sources });
  }

  private onTabKey(e: KeyboardEvent) {
    if (e.key !== "Tab" || e.shiftKey) return;
    e.preventDefault();
    const ta = e.target as HTMLTextAreaElement;
    ta.setRangeText("  ", ta.selectionStart, ta.selectionEnd, "end");
    ta.dispatchEvent(new Event("input"));
  }

  private manifest() {
    const a = this.app;
    return html`<div class="manifest">
      <h3>${t("app.level_title")}</h3>
      <div class="levels" role="radiogroup">${LEVELS.map((l) => html`<label><input type="radio" name="lvl" .checked=${a.level === l} @change=${() => this.setManifest({ level: l })}>
        <b>${t(`app.level${l}` as Key)}</b><span>${t(`app.level${l}_long` as Key, { mb: a.storageMb ?? 10 })}</span></label>`)}</div>
      ${a.level >= 1 ? html`<h3>${t("app.sources")}</h3>
        <p class="muted" style="margin:0;font-size:13px">${t("app.sources_help")}</p>
        ${(a.sources ?? []).map((s, i) => html`<div class="src">
          <input placeholder="id" .value=${s.id} aria-label="id" @change=${(e: Event) => this.setSource(i, { id: (e.target as HTMLInputElement).value.trim() })}>
          <select aria-label=${t("app.source_kind")} @change=${(e: Event) => this.setSource(i, { kind: (e.target as HTMLSelectElement).value as AppSource["kind"] })}>
            ${(["query", "board", "checks", "entities"] as const).map((k) => html`<option value=${k} ?selected=${s.kind === k}>${t(`app.kind.${k}` as Key)}</option>`)}</select>
          <input placeholder=${t("app.source_ref")} .value=${s.ref ?? ""} aria-label=${t("app.source_ref")} @change=${(e: Event) => this.setSource(i, { ref: (e.target as HTMLInputElement).value.trim() })}>
          <input placeholder=${t("app.source_label")} .value=${s.label ?? ""} aria-label=${t("app.source_label")} @change=${(e: Event) => this.setSource(i, { label: (e.target as HTMLInputElement).value })}>
          <button class="danger" aria-label=${t("doc.delete")} @click=${() => this.setManifest({ sources: (a.sources ?? []).filter((_, j) => j !== i) })}>${icon("Trash2", 13)}</button></div>`)}
        <div><button class="ghost" @click=${() => this.setManifest({ sources: [...(a.sources ?? []), { id: `source${(a.sources?.length ?? 0) + 1}`, kind: "query", ref: "" }] })}>${icon("Plus", 14)} ${t("app.source_add")}</button></div>` : nothing}
      <h3>${t("app.ai_title")}</h3>
      <div class="levels"><label><input type="checkbox" .checked=${!!a.ai} @change=${(e: Event) => this.setManifest({ ai: (e.target as HTMLInputElement).checked })}>
        <b>${t("app.ai_on")}</b><span>${t("app.ai_help")}</span></label></div>
      ${a.level >= 2 ? html`<label class="field">${t("app.storage_mb")}<input type="number" min="1" max="100" .value=${String(a.storageMb ?? 10)} style="max-width:140px"
        @change=${(e: Event) => this.setManifest({ storageMb: Number((e.target as HTMLInputElement).value) || 10 })}></label>` : nothing}
    </div>`;
  }

  render() {
    if (this.error && !this.doc) return html`<p class="error" style="padding:16px">${this.error}</p>`;
    if (!this.doc) return nothing;
    const files = Object.keys(this.app.files).sort((a, b) => order(a) - order(b));
    const errs = this.preview?.errors ?? [];
    const consoleErrs = this.logs.filter((l) => l.level === "error").length + errs.length;
    const width = { phone: "390px", tablet: "820px", desktop: "" }[this.size];
    return html`<div class="top">
        <a class="btn ghost" href="/p/${this.project}/d/${this.doc.slug}" @click=${(e: Event) => { if (this.dirty && !confirm(t("theme.leave_unsaved"))) e.preventDefault(); }}>${icon("ArrowLeft", 14)}</a>
        <h1>${this.doc.title}</h1>
        <span class="state">${this.dirty ? t("app.unsaved") : t("app.saved")}</span>
        <span class="sp"></span>
        <button ?disabled=${this.busy || !this.dirty} @click=${this.save}>${icon("Save", 14)} ${t("doc.save")}</button>
      </div>
      <div class="split">
        <div class="left">
          <div class="tabs" role="tablist">
            ${files.map((f) => html`<button role="tab" aria-selected=${this.tab === "code" && this.file === f ? "true" : "false"} @click=${() => { this.file = f; this.tab = "code"; }}
              @dblclick=${() => this.removeFile(f)} title=${t("app.file_tip")}>${f}</button>`)}
            <button class="add" @click=${this.addFile} aria-label=${t("app.add_file")}>${icon("Plus", 13)}</button>
            <button role="tab" aria-selected=${this.tab === "manifest" ? "true" : "false"} @click=${() => (this.tab = "manifest")}>${icon("ShieldCheck", 13)} ${t("app.permissions")}</button>
          </div>
          ${this.tab === "manifest" ? this.manifest() : html`<textarea class="code" spellcheck="false" aria-label=${this.file} .value=${this.app.files[this.file] ?? ""}
            @keydown=${this.onTabKey}
            @input=${(e: Event) => { this.app = { ...this.app, files: { ...this.app.files, [this.file]: (e.target as HTMLTextAreaElement).value } }; this.changed(); }}></textarea>`}
        </div>
        <div class="right">
          <div class="pvbar">
            <div class="seg" role="group" aria-label=${t("app.size")}>${(["phone", "tablet", "desktop"] as const).map((s) => html`<button aria-pressed=${this.size === s ? "true" : "false"} @click=${() => (this.size = s)}>${t(`app.size_${s}` as Key)}</button>`)}</div>
            <div class="seg" role="group" aria-label=${t("theme.mode")}>${(["light", "dark"] as const).map((m) => html`<button aria-pressed=${this.mode === m ? "true" : "false"} @click=${() => (this.mode = m)}>${t(m === "light" ? "sx.me.theme_light" : "sx.me.theme_dark")}</button>`)}</div>
            <button class="ghost" @click=${() => this.refresh()}>${icon("RotateCw", 14)} ${t("app.reload")}</button>
          </div>
          <div class="pv">${this.preview ? html`<phx-app-frame .project=${this.project} .doc=${this.doc.id} .info=${this.preview} .dark=${this.mode === "dark"} style=${width ? `max-width:${width}` : ""}
            @console=${(e: CustomEvent<ConsoleEntry>) => (this.logs = [...this.logs.slice(-99), e.detail])}></phx-app-frame>` : nothing}</div>
          <details class="console" ?open=${consoleErrs > 0}>
            <summary>${t("app.console")}${consoleErrs ? html`<span class="count">${consoleErrs}</span>` : nothing}</summary>
            ${errs.map((e) => html`<div class="error">${e.file}:${e.line}:${e.column} ${e.text}</div>`)}
            ${this.logs.map((l) => html`<div class=${l.level}>${l.file ? `${l.file}:${l.line ?? ""} ` : ""}${l.text}</div>`)}
            ${!errs.length && !this.logs.length ? html`<div class="muted">${t("app.console_empty")}</div>` : nothing}
          </details>
        </div>
      </div>`;
  }
}

const order = (f: string) => (f === "index.html" ? 0 : /^(app|main)\./.test(f) ? 1 : f.endsWith(".css") ? 3 : 2);

customElements.get("phx-app-frame") || customElements.define("phx-app-frame", PhxAppFrame);
customElements.get("phx-app-view") || customElements.define("phx-app-view", PhxAppView);
customElements.get("phx-app-editor") || customElements.define("phx-app-editor", PhxAppEditor);
