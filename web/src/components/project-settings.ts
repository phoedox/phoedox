import { LitElement, css, html } from "lit";
import { ApiError, api, type Project } from "../api";
import { applyProjectColor } from "../color";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import type { Storage } from "../files";
import { storageBar, storageStyles } from "./project-files";
import { toast } from "./toasts";

type Member = { userId: string; email: string; name: string; role: string };
type EnvVar = { name: string; value?: string; secret: boolean };

export class PhxProjectSettings extends LitElement {
  static properties = { project: {}, p: { state: true }, members: { state: true }, env: { state: true }, storage: { state: true }, error: { state: true } };
  static styles = [base, storageStyles, css`
    form { display: grid; gap: 12px; max-width: 560px; margin-bottom: 28px; }
    .color { display: flex; gap: 8px; align-items: center; } .color input[type=color] { width: 48px; padding: 2px; height: 38px; }
    table { border-collapse: collapse; width: 100%; max-width: 560px; } td { padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); }
  `];
  project = "";
  p?: Project;
  members: Member[] = [];
  env: EnvVar[] = [];
  storage?: Storage;
  error = "";

  async connectedCallback() {
    super.connectedCallback();
    this.p = await api<Project>("GET", `/projects/${this.project}`).catch(() => undefined);
    this.members = (await api<Member[]>("GET", `/projects/${this.project}/members`).catch(() => [])) ?? [];
    await this.loadEnv();
    this.storage = await api<Storage>("GET", `/projects/${this.project}/storage`).catch(() => undefined);
  }

  private async setQuota(e: SubmitEvent) {
    e.preventDefault();
    const mb = Number(new FormData(e.target as HTMLFormElement).get("quota") || 0);
    try {
      this.storage = await api<Storage>("PUT", `/projects/${this.project}/storage`, { quotaMb: mb });
      toast(t("settings.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private storageSection(owner: boolean) {
    const s = this.storage;
    if (!s) return "";
    return html`<h2 style="font-size:18px;margin:28px 0 8px">${t("storage.title")}</h2>
      ${storageBar(s)}
      ${owner ? html`<form @submit=${this.setQuota} style="margin-top:12px">
        <label class="field">${t("storage.project_quota")}
          <span class="row"><input name="quota" type="number" min="0" max=${s.maxMb || 100000000} .value=${String(s.projectMb || "")} placeholder=${s.maxMb ? String(s.maxMb) : "∞"} style="width:160px"> MB
          <button class="ghost" type="submit">${t("doc.save")}</button></span></label>
        <p class="muted" style="font-size:13px;margin:0">${s.maxMb ? t("storage.max_hint", { max: s.maxMb }) : t("storage.no_max_hint")}</p>
      </form>` : ""}`;
  }

  private async loadEnv() {
    this.env = (await api<EnvVar[]>("GET", `/projects/${this.project}/env`).catch(() => [])) ?? [];
  }

  private async setVar(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    try {
      await api("PUT", `/projects/${this.project}/env/${encodeURIComponent(String(f.get("envname")))}`, { value: f.get("value") ?? "", secret: f.get("secret") === "on" });
      form.reset();
      toast(t("env.saved"));
      await this.loadEnv();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async deleteVar(name: string) {
    try {
      await api("DELETE", `/projects/${this.project}/env/${encodeURIComponent(name)}`);
      await this.loadEnv();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private envSection(owner: boolean, canEdit: boolean) {
    return html`<h2 style="font-size:18px;margin:28px 0 6px">${t("env.title")}</h2>
      <p class="muted" style="max-width:640px;margin-top:0">${t("env.intro")}</p>
      ${this.env.length === 0 ? html`<p class="muted">${t("env.empty")}</p>` : html`<table>${this.env.map((v) => html`<tr>
        <td class="mono">${v.name}</td>
        <td class="mono">${v.secret ? html`<span class="chip">${icon("KeyRound", 12)} ${t("env.secret")} · ${t("env.secret_set")}</span>` : v.value}</td>
        <td style="text-align:right">${(v.secret ? owner : canEdit) ? html`<button class="danger" style="font-size:13px;padding:4px 10px" @click=${() => this.deleteVar(v.name)}>${t("env.delete")}</button>` : ""}</td></tr>`)}</table>`}
      ${canEdit ? html`<form @submit=${this.setVar} style="margin-top:14px">
        <div class="row">
          <input name="envname" required pattern="[A-Za-z_][A-Za-z0-9_]{0,63}" placeholder=${t("env.name")} style="width:180px" class="mono" autocomplete="off" />
          <input name="value" placeholder=${t("env.value")} style="flex:1;min-width:200px" autocomplete="off" />
          ${owner ? html`<label class="row" style="gap:6px;font-size:14px"><input type="checkbox" name="secret" style="width:auto" /> ${t("env.secret")}</label>` : ""}
          <button type="submit">${t("env.add")}</button>
        </div>
        ${owner ? "" : html`<p class="muted" style="font-size:13px">${t("env.owner_only")}</p>`}
      </form>` : ""}`;
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      const p = await api<Project>("PATCH", `/projects/${this.project}`, {
        name: f.get("name"), description: f.get("description"), primaryColor: f.get("color"), aiWriteMode: f.get("mode"), short: this.p?.short,
      });
      this.p = { ...p, role: this.p?.role ?? "owner" };
      currentProject.set(this.p);
      applyProjectColor(document.documentElement, p.primaryColor);
      docsVersion.set(docsVersion.get() + 1);
      toast(t("settings.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async addMember(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    try {
      await api("PUT", `/projects/${this.project}/members`, { email: f.get("email"), role: f.get("role") });
      this.members = (await api<Member[]>("GET", `/projects/${this.project}/members`)) ?? [];
      form.reset();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    const p = this.p;
    if (!p) return html`<p class="muted">${t("app.loading")}</p>`;
    const owner = p.role === "owner";
    const canEdit = owner || p.role === "editor";
    return html`<div class="enter">
      <h1 style="margin-bottom:16px">${t("settings.title")}</h1>
      <form @submit=${this.save}>
        <label class="field">${t("projects.name")}<input name="name" .value=${p.name} ?disabled=${!owner} required /></label>
        <label class="field">${t("projects.description")}<input name="description" .value=${p.description} ?disabled=${!owner} /></label>
        <label class="field">${t("settings.color")}
          <span class="color"><input type="color" name="color" .value=${p.primaryColor.toLowerCase()} ?disabled=${!owner}
            @input=${(e: Event) => applyProjectColor(document.documentElement, (e.target as HTMLInputElement).value)} /><span class="mono">${p.primaryColor}</span></span></label>
        <label class="field">${t("settings.ai_mode")}
          <select name="mode" ?disabled=${!owner}>
            <option value="off" ?selected=${p.aiWriteMode === "off"}>${t("settings.ai_mode.off")}</option>
            <option value="read" ?selected=${p.aiWriteMode === "read"}>${t("settings.ai_mode.read")}</option>
            <option value="suggest" ?selected=${p.aiWriteMode === "suggest"}>${t("settings.ai_mode.suggest")}</option>
            <option value="direct" ?selected=${p.aiWriteMode === "direct"}>${t("settings.ai_mode.direct")}</option>
          </select></label>
        ${owner ? html`<div><button type="submit">${t("doc.save")}</button></div>` : ""}
      </form>
      <h2 style="font-size:18px;margin-bottom:8px">${t("settings.members")}</h2>
      <table>${this.members.map((m) => html`<tr><td>${m.name}</td><td class="muted">${m.email}</td><td>${t(`settings.role.${m.role}` as any)}</td></tr>`)}</table>
      ${owner ? html`<form @submit=${this.addMember} style="margin-top:14px">
        <div class="row"><input name="email" type="email" required placeholder=${t("login.email")} style="flex:1;min-width:200px" />
          <select name="role" style="width:auto"><option value="reader">${t("settings.role.reader")}</option><option value="editor">${t("settings.role.editor")}</option><option value="owner">${t("settings.role.owner")}</option></select>
          <button type="submit">${t("settings.add_member")}</button></div>
      </form>` : ""}
      ${this.envSection(owner, canEdit)}
      ${this.storageSection(owner)}
    </div>`;
  }
}
customElements.define("phx-project-settings", PhxProjectSettings);
