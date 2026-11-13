import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatSize } from "../files";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { navigate, signedIn } from "../state";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

interface Info { name: string; size: number; createdAt: string }
interface Status { backups: Info[]; target: string; withFiles: boolean; keep: number; everyHours: number; running: boolean; lastError?: string; next: string }

/** Backups for system admins: list, make now, download, upload, restore. */
export class PhxBackups extends SignalWatcher(LitElement) {
  static properties = { s: { state: true }, restoring: { state: true }, busy: { state: true }, error: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 860px; margin: 0 auto; padding: 28px 16px 60px; }
    h1 { margin-bottom: 16px; }
    p { line-height: 1.5; max-width: 680px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; margin-top: 12px; }
    td { padding: 8px 8px 8px 0; border-bottom: 1px solid var(--line); }
    td.r { text-align: right; white-space: nowrap; }
    td button, td .btn { padding: 4px 10px; font-size: 13px; }
    .bar { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .warn { border-left: 3px solid var(--bad); padding: 10px 14px; background: var(--surface); border-radius: 8px; }
    input[type=file] { display: none; }
  `];
  s?: Status;
  restoring = "";
  busy = false;
  error = "";
  private timer?: number;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearTimeout(this.timer);
  }

  private async load() {
    try {
      this.s = await api<Status>("GET", "/admin/backups");
      this.error = "";
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
    clearTimeout(this.timer);
    if (this.s?.running) this.timer = window.setTimeout(() => this.load(), 2000);
  }

  private async create() {
    this.busy = true;
    try {
      const r = await api<{ done: boolean }>("POST", "/admin/backups");
      if (r.done) toast(t("backups.done"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
      await this.load();
    }
  }

  private async upload(files: FileList | null) {
    const f = files?.[0];
    if (!f) return;
    this.busy = true;
    try {
      const r = await fetch("/api/v1/admin/backups/upload", { method: "POST", headers: { "X-Phoedox": "1", "Content-Type": "application/gzip" }, body: f, credentials: "same-origin" });
      if (!r.ok) {
        const d = await r.json().catch(() => ({}));
        throw new ApiError(r.status, d.code ?? "unknown", d.key ?? "error.unknown", d.params ?? {});
      }
      toast(t("backups.uploaded"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
      await this.load();
    }
  }

  private async restore(e: SubmitEvent, b: Info) {
    e.preventDefault();
    const confirm = String(new FormData(e.target as HTMLFormElement).get("confirm") ?? "");
    this.busy = true;
    try {
      await api("POST", `/admin/backups/${b.name}/restore`, { confirm });
      // Sessions came back from the backup: sign in again.
      signedIn(null);
      navigate("/login");
      toast(t("backups.restored"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeBackup(b: Info) {
    if (!confirm(t("backups.delete_confirm", { name: b.name }))) return;
    try {
      await api("DELETE", `/admin/backups/${b.name}`);
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const s = this.s;
    return html`<div class="enter">
      <h1>${t("backups.title")}</h1>
      ${settingsNav("backups")}
      ${this.error ? html`<p class="error">${this.error}</p>` : ""}
      ${s ? html`
        <p class="muted">${s.everyHours ? t("backups.schedule", { hours: s.everyHours, keep: s.keep || "∞", target: t(s.target === "s3" ? "backups.target_s3" : "backups.target_local") }) : t("backups.no_schedule")}
          ${s.withFiles ? t("backups.with_files") : t("backups.without_files")}</p>
        ${s.target === "local" ? html`<p class="warn">${icon("CircleAlert", 14)} ${t("backups.local_warning")}</p>` : ""}
        ${s.lastError ? html`<p class="error">${t("backups.last_error", { error: s.lastError })}</p>` : ""}
        <div class="bar">
          <button ?disabled=${this.busy || s.running} @click=${this.create}>${icon("Archive", 14)} ${s.running ? t("backups.running") : t("backups.create")}</button>
          <button class="ghost" ?disabled=${this.busy} @click=${() => this.renderRoot.querySelector<HTMLInputElement>("input[type=file]")!.click()}>${icon("Upload", 14)} ${t("backups.upload")}</button>
          <input type="file" accept=".gz,application/gzip" @change=${(e: Event) => { const i = e.target as HTMLInputElement; this.upload(i.files); i.value = ""; }}>
        </div>
        ${s.backups.length ? html`<table>${s.backups.map((b) => html`<tr>
          <td class="mono">${b.name}</td><td class="muted">${formatDate(b.createdAt)}</td><td class="muted">${formatSize(b.size, locale())}</td>
          <td class="r"><a class="btn ghost" href="/api/v1/admin/backups/${b.name}" download>${icon("Download", 13)}</a>
            <button class="ghost" @click=${() => (this.restoring = this.restoring === b.name ? "" : b.name)}>${icon("RotateCcw", 13)} ${t("backups.restore")}</button>
            <button class="danger" aria-label=${t("backups.delete")} @click=${() => this.removeBackup(b)}>${icon("Trash2", 13)}</button></td></tr>
          ${this.restoring === b.name ? html`<tr><td colspan="4"><form class="warn" @submit=${(e: SubmitEvent) => this.restore(e, b)}>
            <p style="margin:0 0 8px"><b>${t("backups.restore_title")}</b><br>${t("backups.restore_warning")}</p>
            <div class="bar"><input name="confirm" required placeholder=${b.name} aria-label=${t("backups.restore_confirm")} style="flex:1;min-width:260px" class="mono">
              <button class="danger" ?disabled=${this.busy}>${t("backups.restore_now")}</button></div></form></td></tr>` : ""}`)}</table>`
          : html`<p class="muted">${t("backups.none")}</p>`}
        <p class="muted" style="font-size:13px;margin-top:18px">${t("backups.key_hint")}</p>` : !this.error ? html`<p class="muted">${t("app.loading")}</p>` : ""}
    </div>`;
  }
}
customElements.define("phx-backups", PhxBackups);
