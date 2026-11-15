import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentUser, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface UserExport { id: string; status: "queued" | "done" | "failed"; size: number; createdAt: string; expiresAt: string }
interface ExportState { mailEnabled: boolean; latest?: UserExport; nextAt?: string }
interface Ref { kind: "project" | "org" | "org_projects" | "last_admin"; slug?: string; name?: string }
interface Deletion {
  blockers: Ref[]; deletes: Ref[]; backupDays: number;
  needsPassword: boolean; needsCode: boolean; needsRecentLogin: boolean;
}

/** Settings → Account: export my data, delete my account (ADR-0055). */
export class PhxAccount extends LitElement {
  static properties = {
    exp: { state: true }, del: { state: true }, confirming: { state: true }, error: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    section { margin-bottom: 34px; }
    h2 { font-size: 17px; margin: 0 0 6px; display: flex; align-items: center; gap: 8px; }
    p { margin: 0 0 12px; line-height: 1.55; }
    ul.facts { margin: 0 0 14px; padding-left: 20px; line-height: 1.55; }
    .hint { font-size: 13.5px; color: var(--muted); }
    .box { border: 1px solid var(--line); border-radius: 10px; padding: 14px 16px; margin: 10px 0; display: flex; flex-direction: column; gap: 8px; }
    .box.bad { border-color: var(--bad); }
    details.box summary { cursor: pointer; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    form.confirm { display: grid; gap: 12px; max-width: 420px; }
    input.code { font-family: var(--font-mono); letter-spacing: .15em; }
    .danger-zone { border-top: 1px solid var(--line); padding-top: 22px; }
    button.danger.solid { background: var(--bad); color: var(--surface); border-color: var(--bad); }
  `];
  exp?: ExportState;
  del?: Deletion;
  confirming = false;
  error = "";
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    [this.exp, this.del] = await Promise.all([
      api<ExportState>("GET", "/me/export").catch(() => undefined),
      api<Deletion>("GET", "/me/deletion").catch(() => undefined),
    ]);
  }

  private async requestExport() {
    this.busy = true;
    try {
      await api("POST", "/me/export");
      toast(t("account.export_started"));
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private exportSection() {
    const s = this.exp;
    if (!s) return html`<p class="hint">${t("app.loading")}</p>`;
    const l = s.latest;
    const waiting = !!s.nextAt;
    return html`
      <p>${t("account.export_intro")}</p>
      ${l?.status === "done" ? html`<div class="box">
        <b>${icon("FileArchive", 15)} ${t("account.export_ready", { date: formatDate(l.createdAt) })}</b>
        <span class="hint">${t("account.export_until", { date: formatDate(l.expiresAt) })}</span>
        <div class="row"><a class="btn" href=${`/api/v1/me/exports/${l.id}/download`} download>${icon("Download", 14)} ${t("account.export_download")}</a></div>
      </div>` : l?.status === "queued" ? html`<div class="box"><span>${icon("Loader", 15)} ${t("account.export_running")}</span></div>` : ""}
      <div class="row">
        <button ?disabled=${!s.mailEnabled || waiting || this.busy} @click=${this.requestExport}>${icon("PackageOpen", 14)} ${t("account.export_button")}</button>
      </div>
      ${!s.mailEnabled ? html`<p class="hint">${t("account.export_no_mail")}</p>`
        : waiting ? html`<p class="hint">${t("account.export_next", { date: formatDate(s.nextAt!) })}</p>`
        : html`<p class="hint">${t("account.export_how")}</p>`}`;
  }

  private blockerText(b: Ref) {
    switch (b.kind) {
      case "last_admin": return t("account.block_last_admin");
      case "org": return t("account.block_org", { name: b.name ?? "" });
      case "org_projects": return t("account.block_org_projects", { name: b.name ?? "" });
      default: return t("account.block_project", { name: b.name ?? "" });
    }
  }

  private deleteSection() {
    const d = this.del;
    if (!d) return html`<p class="hint">${t("app.loading")}</p>`;
    const blocked = d.blockers.length > 0;
    return html`
      <p>${t("account.delete_intro")}</p>
      <ul class="facts">
        <li>${t("account.delete_fact_shared")}</li>
        <li>${t("account.delete_fact_gone")}</li>
        ${d.backupDays > 0 ? html`<li>${t("account.delete_fact_backups", { days: d.backupDays })}</li>` : ""}
      </ul>
      ${d.deletes.length ? html`<details class="box" ?open=${d.deletes.length <= 5}><summary><b>${t("account.delete_with")} (${d.deletes.length})</b></summary>
        <ul class="facts">${d.deletes.map((x) => html`<li>${x.kind === "org" ? t("account.org_label", { name: x.name ?? "" }) : x.name}</li>`)}</ul></details>` : ""}
      ${blocked ? html`<div class="box bad" role="alert"><b>${icon("TriangleAlert", 15)} ${t("account.delete_blocked")}</b>
        <ul class="facts">${d.blockers.map((b) => html`<li>${this.blockerText(b)}</li>`)}</ul></div>` : ""}
      ${this.confirming && !blocked ? this.confirmForm(d)
        : html`<button class="danger" ?disabled=${blocked} @click=${() => { this.confirming = true; this.error = ""; }}>${icon("Trash2", 14)} ${t("account.delete_button")}</button>`}`;
  }

  private confirmForm(d: Deletion) {
    const email = currentUser.get()?.email ?? "";
    return html`<form class="confirm" @submit=${this.deleteAccount}>
      <label>${t("account.confirm_email", { email })}<input name="email" type="email" autocomplete="off" required></label>
      ${d.needsPassword ? html`<label>${t("security.confirm_password")}<input name="password" type="password" autocomplete="current-password" required></label>` : ""}
      ${d.needsCode ? html`<label>${t("security.totp_code")}<input class="code" name="code" inputmode="numeric" autocomplete="one-time-code" required></label>` : ""}
      ${d.needsRecentLogin ? html`<p class="hint">${t("account.confirm_recent")}</p>` : ""}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <div class="row">
        <button class="danger solid" ?disabled=${this.busy}>${t("account.delete_final")}</button>
        <button type="button" class="ghost" @click=${() => { this.confirming = false; }}>${t("doc.cancel")}</button>
      </div>
    </form>`;
  }

  private deleteAccount = async (e: SubmitEvent) => {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.busy = true;
    this.error = "";
    try {
      await api("POST", "/me/delete", { email: String(f.get("email") ?? ""), password: String(f.get("password") ?? ""), code: String(f.get("code") ?? "") });
      currentUser.set(null);
      navigate("/login?deleted=1");
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  };

  render() {
    return html`
      <h1>${t("account.title")}</h1>
      <section aria-labelledby="exp"><h2 id="exp">${icon("FileArchive", 17)} ${t("account.export_title")}</h2>${this.exportSection()}</section>
      <section class="danger-zone" aria-labelledby="del"><h2 id="del">${icon("UserX", 17)} ${t("account.delete_title")}</h2>${this.deleteSection()}</section>`;
  }
}
customElements.define("phx-account", PhxAccount);
