// Public apps (apps-definition "Sharing"): the owner's dialog to release an
// app to people without an account – without data or with a snapshot of
// its sources, a refresh schedule, an AI limit – after looking at the data
// that becomes public.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t, type Key } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

export interface AppShare { token: string; mode: "static" | "snapshot"; schedule: string; aiLimit: number; aiMonth: string; aiUsed: number; snapshotAt: string; snapshotError?: string; publishedAt: string }
export interface AppShareView {
  shared: boolean; share?: AppShare; url?: string; allowed: boolean; canEdit: boolean; level: number; ai: boolean;
  sources: { id: string; kind: string; label?: string }[];
  preview?: Record<string, unknown>; previewErrors?: Record<string, string>;
}

const SCHEDULES = ["manual", "hourly", "daily", "weekly"] as const;

function summary(v: unknown): string {
  if (Array.isArray(v)) return t("appshare.items", { n: v.length });
  if (v && typeof v === "object") {
    const rows = (v as { rows?: unknown[]; items?: unknown[] }).rows ?? (v as { items?: unknown[] }).items;
    if (Array.isArray(rows)) return t("appshare.items", { n: rows.length });
  }
  return "";
}

export class PhxAppShare extends LitElement {
  static properties = {
    project: {}, doc: {}, view: { attribute: false },
    mode: { state: true }, schedule: { state: true }, aiLimit: { state: true }, confirmed: { state: true }, busy: { state: true }, preview: { state: true },
  };
  static styles = [base, css`
    dialog { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); padding: 20px; width: min(620px, 94vw); max-height: 90dvh; box-shadow: 0 18px 50px rgb(0 0 0 / .2); }
    dialog::backdrop { background: rgb(0 0 0 / .35); }
    h2 { font-size: 18px; margin: 0 0 6px; }
    .grid { display: grid; gap: 14px; margin-top: 14px; }
    .modes { display: grid; gap: 8px; }
    .modes label { display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; cursor: pointer; font-size: 14px; }
    .modes label:has(input:checked) { border-color: var(--accent); background: var(--accent-soft); }
    .modes label:has(input:disabled) { opacity: .55; cursor: not-allowed; }
    .modes small { grid-column: 2; color: var(--muted); font-size: 13px; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .end { justify-content: flex-end; }
    .field { display: grid; gap: 4px; font-size: 14px; }
    .field small { color: var(--muted); font-size: 12.5px; }
    .warn { display: grid; grid-template-columns: auto 1fr; gap: 4px 10px; padding: 12px; border-radius: 10px; background: color-mix(in srgb, var(--signal) 12%, var(--surface)); font-size: 14px; }
    .warn .ico { color: var(--signal); }
    .warn label { grid-column: 2; display: flex; gap: 8px; align-items: center; justify-content: flex-start; font-weight: 600; }
    .warn input { width: auto; margin: 0; }
    .src { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
    .src header { display: flex; justify-content: space-between; gap: 8px; padding: 8px 10px; background: var(--surface2); font-size: 13.5px; }
    .src pre { margin: 0; padding: 10px; font: 12px/1.5 var(--font-mono); max-height: 180px; overflow: auto; white-space: pre-wrap; word-break: break-word; }
    .src .err { color: var(--bad); padding: 8px 10px; font-size: 13px; }
    .link { display: flex; gap: 6px; }
    .link input { flex: 1; font: 13px var(--font-mono); }
    .muted { color: var(--muted); font-size: 13px; }
    .bad { color: var(--bad); font-size: 13px; }
  `];

  project = "";
  doc = "";
  view?: AppShareView;
  mode: "static" | "snapshot" = "snapshot";
  schedule = "manual";
  aiLimit = 0;
  confirmed = false;
  busy = false;
  preview?: AppShareView;

  /** Opens the dialog with the current settings. */
  open() {
    const v = this.view;
    this.mode = v?.share?.mode ?? (v && v.level >= 1 && v.sources.length ? "snapshot" : "static");
    this.schedule = v?.share?.schedule ?? "manual";
    this.aiLimit = v?.share?.aiLimit ?? 0;
    this.confirmed = false;
    this.preview = undefined;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.showModal();
    if (this.mode === "snapshot") void this.loadPreview();
  }

  private close() {
    this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close();
  }

  private base() {
    return `/projects/${this.project}/apps/${encodeURIComponent(this.doc)}/share`;
  }

  private changed(v: AppShareView) {
    this.view = v;
    this.dispatchEvent(new CustomEvent<AppShareView>("changed", { detail: v }));
  }

  private async loadPreview() {
    try {
      this.preview = await api<AppShareView>("GET", `${this.base()}?preview=1`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async run(f: () => Promise<AppShareView | void>, done: Key) {
    this.busy = true;
    try {
      const v = await f();
      if (v) this.changed(v);
      toast(t(done));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private publish() {
    return this.run(() => api<AppShareView>("PUT", this.base(), { mode: this.mode, schedule: this.schedule, aiLimit: this.aiLimit, confirm: this.confirmed }), this.view?.shared ? "appshare.updated" : "appshare.published");
  }

  private refresh() {
    return this.run(() => api<AppShareView>("POST", `${this.base()}/refresh`), "appshare.refreshed");
  }

  private async withdraw() {
    if (!confirm(t("appshare.withdraw_confirm"))) return;
    await this.run(async () => {
      await api("DELETE", this.base());
      const v = await api<AppShareView>("GET", this.base());
      this.close();
      return v;
    }, "appshare.withdrawn");
  }

  /** The public address (absolute, also where the instance has no public URL set). */
  private link() {
    const u = this.view?.url ?? "";
    return u.startsWith("/") ? location.origin + u : u;
  }

  private copy() {
    if (this.view?.url) void navigator.clipboard?.writeText(this.link()).then(() => toast(t("appshare.copied")));
  }

  private previewBlock() {
    const p = this.preview;
    if (!p) return html`<p class="muted">${t("appshare.preview_loading")}</p>`;
    return html`${p.sources.map((s) => {
      const err = p.previewErrors?.[s.id];
      const data = p.preview?.[s.id];
      const text = JSON.stringify(data, null, 2) ?? "";
      return html`<div class="src">
        <header><strong>${s.label || s.id}</strong><span class="muted">${t(`app.kind.${s.kind}` as Key)} · ${summary(data)}</span></header>
        ${err ? html`<div class="err">${t("appshare.source_failed")}</div>` : html`<pre>${text.length > 6000 ? text.slice(0, 6000) + "\n…" : text}</pre>`}
      </div>`;
    })}`;
  }

  render() {
    const v = this.view;
    if (!v) return nothing;
    const sh = v.share;
    const canSnapshot = v.level >= 1 && v.sources.length > 0;
    return html`<dialog aria-labelledby="st">
      <h2 id="st">${t("appshare.title")}</h2>
      <p class="muted">${t("appshare.intro")}</p>
      ${!v.allowed ? html`<p class="bad" role="alert">${t("appshare.forbidden")}</p>` : html`<div class="grid">
        ${v.shared && sh ? html`<div class="field">
            <span>${t("appshare.link")}</span>
            <div class="link"><input readonly .value=${this.link()} aria-label=${t("appshare.link")} @focus=${(e: Event) => (e.target as HTMLInputElement).select()} />
              <button class="ghost" @click=${this.copy}>${icon("Copy", 14)} ${t("appshare.copy")}</button></div>
            ${sh.mode === "snapshot" ? html`<small>${t("appshare.snapshot_at", { date: formatDate(sh.snapshotAt) })}${sh.snapshotError ? html` · <span class="bad">${t(`appshare.err.${sh.snapshotError}` as Key)}</span>` : nothing}</small>` : nothing}
            ${sh.aiLimit > 0 ? html`<small>${t("appshare.ai_used", { used: sh.aiMonth === new Date().toISOString().slice(0, 7) ? sh.aiUsed : 0, limit: sh.aiLimit })}</small>` : nothing}
          </div>` : nothing}
        <div class="modes" role="radiogroup" aria-label=${t("appshare.mode")}>
          <label><input type="radio" name="mode" .checked=${this.mode === "static"} @change=${() => (this.mode = "static")} />
            <span>${t("appshare.mode_static")}</span><small>${t("appshare.mode_static_help")}</small></label>
          <label><input type="radio" name="mode" ?disabled=${!canSnapshot} .checked=${this.mode === "snapshot"} @change=${() => { this.mode = "snapshot"; if (!this.preview) void this.loadPreview(); }} />
            <span>${t("appshare.mode_snapshot")}</span><small>${canSnapshot ? t("appshare.mode_snapshot_help") : t("appshare.mode_snapshot_none")}</small></label>
        </div>
        ${this.mode === "snapshot" ? html`
          <label class="field">${t("appshare.schedule")}
            <select .value=${this.schedule} @change=${(e: Event) => (this.schedule = (e.target as HTMLSelectElement).value)}>
              ${SCHEDULES.map((x) => html`<option value=${x} ?selected=${x === this.schedule}>${t(`appshare.schedule_${x}` as Key)}</option>`)}
            </select><small>${t("appshare.schedule_help")}</small></label>
          <div class="field"><span>${t("appshare.preview")}</span><small>${t("appshare.preview_help")}</small></div>
          ${this.previewBlock()}` : nothing}
        ${v.ai ? html`<label class="field">${t("appshare.ai_limit")}
            <input type="number" min="0" max="10000" step="10" .value=${String(this.aiLimit)} @input=${(e: Event) => (this.aiLimit = Math.max(0, Number((e.target as HTMLInputElement).value) || 0))} />
            <small>${t("appshare.ai_limit_help")}</small></label>` : nothing}
        <div class="warn" role="note">${icon("TriangleAlert", 18)}<span>${t("appshare.warning")}</span>
          <label><input type="checkbox" .checked=${this.confirmed} @change=${(e: Event) => (this.confirmed = (e.target as HTMLInputElement).checked)} /> ${t("appshare.confirm")}</label></div>
      </div>`}
      <div class="row end" style="margin-top:16px">
        ${v.shared ? html`<button class="ghost danger" ?disabled=${this.busy} @click=${this.withdraw}>${t("appshare.withdraw")}</button>` : nothing}
        ${v.shared && sh?.mode === "snapshot" ? html`<button class="ghost" ?disabled=${this.busy} @click=${this.refresh}>${icon("RefreshCw", 14)} ${t("appshare.refresh")}</button>` : nothing}
        <span style="flex:1"></span>
        <button class="ghost" @click=${this.close}>${t("appshare.close")}</button>
        ${v.allowed ? html`<button ?disabled=${this.busy || !this.confirmed} @click=${this.publish}>${icon("Globe", 14)} ${t(v.shared ? "appshare.update" : "appshare.publish")}</button>` : nothing}
      </div>
    </dialog>`;
  }
}
customElements.get("phx-app-share") || customElements.define("phx-app-share", PhxAppShare);
