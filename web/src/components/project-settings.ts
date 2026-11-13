import { LitElement, css, html } from "lit";
import { ApiError, api, type Project } from "../api";
import { applyProjectColor } from "../color";
import { t } from "../i18n";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

type Member = { userId: string; email: string; name: string; role: string };

export class PhxProjectSettings extends LitElement {
  static properties = { project: {}, p: { state: true }, members: { state: true }, error: { state: true } };
  static styles = [base, css`
    form { display: grid; gap: 12px; max-width: 560px; margin-bottom: 28px; }
    .color { display: flex; gap: 8px; align-items: center; } .color input[type=color] { width: 48px; padding: 2px; height: 38px; }
    table { border-collapse: collapse; width: 100%; max-width: 560px; } td { padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); }
  `];
  project = "";
  p?: Project;
  members: Member[] = [];
  error = "";

  async connectedCallback() {
    super.connectedCallback();
    this.p = await api<Project>("GET", `/projects/${this.project}`).catch(() => undefined);
    this.members = (await api<Member[]>("GET", `/projects/${this.project}/members`).catch(() => [])) ?? [];
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
    </div>`;
  }
}
customElements.define("phx-project-settings", PhxProjectSettings);
