import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Git { url: string; branch: string; folder: string; tokenVar: string; enabled: boolean; lastSync: string | null; lastCommit: string; lastError: string }
interface EnvVar { name: string; secret: boolean }
interface ImportResult { project: { slug: string; name: string }; pages: number; entries: number; tasks: number; files: number; problems: string[] }

const errText = (e: unknown) => (e instanceof ApiError ? e.text : t("error.unknown"));

/** Project settings: download the export and set up Git sync (owners). */
export class PhxProjectTransfer extends LitElement {
  static properties = { project: {}, git: { state: true }, secrets: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    form { display: grid; gap: 10px; max-width: 620px; }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    @media (max-width: 640px) { .two { grid-template-columns: 1fr; } }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .state { font-size: 13px; }
    .bad { color: var(--bad); }
    code { font-size: 12.5px; }
  `];
  project = "";
  git?: Git;
  secrets: string[] = [];
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    this.git = await api<Git>("GET", `/projects/${this.project}/git`).catch(() => undefined);
    const env = (await api<EnvVar[]>("GET", `/projects/${this.project}/env`).catch(() => [])) ?? [];
    this.secrets = env.filter((v) => v.secret).map((v) => v.name);
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      this.git = await api<Git>("PUT", `/projects/${this.project}/git`, {
        url: String(f.get("url") ?? ""), branch: String(f.get("branch") ?? ""), folder: String(f.get("folder") ?? ""),
        tokenVar: String(f.get("tokenVar") ?? ""), enabled: f.get("enabled") === "on" });
      toast(t("git.saved"));
    } catch (err) {
      toast(errText(err), "error");
    }
  }

  private async sync() {
    this.busy = true;
    try {
      this.git = await api<Git>("POST", `/projects/${this.project}/git/sync`);
      toast(this.git.lastError ? t("git.failed") : t("git.synced"), this.git.lastError ? "error" : "ok");
    } catch (err) {
      toast(errText(err), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const g = this.git;
    return html`
      <p class="muted">${t("transfer.export_hint")}</p>
      <p><a class="btn ghost" href="/api/v1/projects/${this.project}/export" download>${icon("Download", 14)} ${t("transfer.export")}</a></p>
      <h3>${t("git.title")}</h3>
      <p class="muted">${t("git.hint")}</p>
      ${g ? html`<form @submit=${this.save}>
        <label class="field">${t("git.url")}<input name="url" type="url" required placeholder="https://github.com/org/docs.git" .value=${g.url} /></label>
        <div class="two">
          <label class="field">${t("git.branch")}<input name="branch" .value=${g.branch || "main"} /></label>
          <label class="field">${t("git.folder")}<input name="folder" .value=${g.folder} placeholder=${t("git.folder_root")} /></label>
        </div>
        <label class="field">${t("git.token")}
          <select name="tokenVar"><option value="">${t("git.token_none")}</option>
            ${this.secrets.map((n) => html`<option value=${n} ?selected=${g.tokenVar === n}>{{${n}}}</option>`)}</select>
          <span class="muted" style="font-size:12.5px">${t("git.token_hint")}</span></label>
        <label class="row"><input type="checkbox" name="enabled" ?checked=${g.enabled} style="width:auto" /> ${t("git.enabled")}</label>
        <div class="row"><button type="submit">${t("doc.save")}</button>
          ${g.url ? html`<button type="button" class="ghost" ?disabled=${this.busy} @click=${this.sync}>${icon("RefreshCw", 14)} ${t("git.sync_now")}</button>` : ""}</div>
        ${g.lastSync ? html`<p class="state">${g.lastError
          ? html`<span class="bad">${icon("CircleAlert", 13)} ${t("git.last_error", { date: formatDate(g.lastSync) })} <code>${g.lastError}</code></span>`
          : html`${icon("CircleCheck", 13)} ${t("git.last_sync", { date: formatDate(g.lastSync) })}${g.lastCommit ? html` · <code>${g.lastCommit.slice(0, 10)}</code>` : ""}`}</p>` : ""}
      </form>` : ""}`;
  }
}
customElements.define("phx-project-transfer", PhxProjectTransfer);

/** Projects page: import an export file or move a project from another instance. */
export class PhxProjectImport extends LitElement {
  static properties = { remote: { state: true }, busy: { state: true }, result: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    form { display: grid; gap: 10px; max-width: 620px; margin-bottom: 18px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    ul { margin: 6px 0; padding-left: 18px; font-size: 13px; }
    h3 { font-size: 15px; margin: 14px 0 6px; }
  `];
  remote?: { slug: string; name: string }[];
  busy = false;
  result?: ImportResult;
  private source = { url: "", token: "" };

  private done(r: ImportResult) {
    this.result = r;
    toast(t("transfer.imported", { name: r.project.name }));
    if (!r.problems.length) navigate(`/p/${r.project.slug}`);
  }

  private async upload(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0];
    if (!file) return;
    this.busy = true;
    try {
      const res = await fetch("/api/v1/projects/import", { method: "POST", headers: { "X-Phoedox": "1", "Content-Type": "application/zip" }, body: file });
      const body = await res.json();
      if (!res.ok) throw new ApiError(res.status, body.code ?? "unknown", body.key ?? "error.unknown", body.params ?? {});
      this.done(body as ImportResult);
    } catch (err) {
      toast(errText(err), "error");
    } finally {
      this.busy = false;
    }
  }

  private async list(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.source = { url: String(f.get("url") ?? ""), token: String(f.get("token") ?? "") };
    this.busy = true;
    try {
      this.remote = await api<{ slug: string; name: string }[]>("POST", "/projects/import-remote/list", this.source);
    } catch (err) {
      toast(errText(err), "error");
    } finally {
      this.busy = false;
    }
  }

  private async move(slug: string) {
    this.busy = true;
    try {
      this.done(await api<ImportResult>("POST", "/projects/import-remote", { ...this.source, project: slug }));
    } catch (err) {
      toast(errText(err), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    return html`
      <h3>${icon("Upload", 15)} ${t("transfer.import_file")}</h3>
      <p class="muted">${t("transfer.import_file_hint")}</p>
      <label class="btn ghost" style="display:inline-flex;gap:6px;cursor:pointer">${icon("FileArchive", 14)} ${t("transfer.choose_file")}
        <input type="file" accept=".zip,application/zip" hidden ?disabled=${this.busy} @change=${this.upload} /></label>
      <h3>${icon("ArrowRightLeft", 15)} ${t("transfer.move")}</h3>
      <p class="muted">${t("transfer.move_hint")}</p>
      <form @submit=${this.list}>
        <label class="field">${t("transfer.instance")}<input name="url" type="url" required placeholder="https://old.example.org" /></label>
        <label class="field">${t("transfer.token")}<input name="token" type="password" required autocomplete="off" /></label>
        <div class="row"><button type="submit" class="ghost" ?disabled=${this.busy}>${t("transfer.load")}</button></div>
      </form>
      ${this.remote ? (this.remote.length ? html`<ul>${this.remote.map((p) => html`<li class="row">${p.name} <span class="muted">(${p.slug})</span>
          <button class="ghost" ?disabled=${this.busy} @click=${() => this.move(p.slug)}>${icon("ArrowRight", 13)} ${t("transfer.move_one")}</button></li>`)}</ul>`
        : html`<p class="muted">${t("transfer.none")}</p>`) : ""}
      ${this.result?.problems.length ? html`<p>${t("transfer.problems", { name: this.result.project.name })}</p><ul>${this.result.problems.map((x) => html`<li>${x}</li>`)}</ul>
        <a href="/p/${this.result.project.slug}">${t("transfer.open")}</a>` : ""}`;
  }
}
customElements.define("phx-project-import", PhxProjectImport);
