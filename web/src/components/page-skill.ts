import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { skillName, validSkillName } from "../skill";
import { base } from "./shared";
import { toast } from "./toasts";

interface Skill { name: string; description: string; updatedAt: string }

/** Marks a text page as an AI skill (editors) and shows that it is one. AI
 * clients find skills with list_skills over MCP; the download is a SKILL.md
 * folder for clients that load skills from disk. */
export class PhxPageSkill extends LitElement {
  static properties = { project: {}, doc: {}, pageTitle: {}, canEdit: { type: Boolean }, skill: { state: true }, loaded: { state: true }, editing: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    details { margin: 12px 0 0; font-size: 14px; }
    summary { cursor: pointer; color: var(--muted); display: inline-flex; gap: 6px; align-items: center; }
    form { display: grid; gap: 10px; margin: 12px 0; max-width: 640px; }
    textarea { min-height: 72px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .pill { display: inline-flex; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); }
    code { font-family: var(--font-mono); }
  `];
  project = "";
  doc = "";
  pageTitle = "";
  canEdit = false;
  skill: Skill | null = null;
  loaded = false;
  editing = false;
  busy = false;

  updated(changed: Map<string, unknown>) {
    if (changed.has("doc")) this.load();
  }

  private get base() {
    return `/projects/${this.project}/documents/${this.doc}/skill`;
  }

  private async load() {
    if (!this.doc) return;
    this.loaded = false;
    this.skill = (await api<{ skill: Skill | null }>("GET", this.base).catch(() => ({ skill: null }))).skill;
    this.loaded = true;
  }

  private async save(e: Event) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const name = String(f.get("name") ?? "").trim();
    if (!validSkillName(name)) {
      toast(t("validation.skill_name", { max: 64 }), "error");
      return;
    }
    this.busy = true;
    try {
      this.skill = (await api<{ skill: Skill }>("PUT", this.base, { name, description: String(f.get("description") ?? "") })).skill;
      this.editing = false;
      toast(t("skill.saved"), "ok");
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async unmark() {
    this.busy = true;
    try {
      await api("DELETE", this.base);
      this.skill = null;
      this.editing = false;
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private download() {
    return html`<a class="btn ghost" href="/api/v1/projects/${this.project}/skills/${this.skill!.name}/download" download>${icon("Download", 14)} ${t("skill.download")}</a>`;
  }

  render() {
    if (!this.loaded) return "";
    const s = this.skill;
    if (!this.canEdit) {
      return s ? html`<p class="pill">${icon("Sparkles", 14)} ${t("skill.is_skill", { name: s.name })} ${this.download()}</p>` : "";
    }
    return html`<details ?open=${this.editing}>
      <summary>${icon("Sparkles", 14)} ${t("skill.title")}${s ? html` · <code>${s.name}</code>` : ""}</summary>
      ${s && !this.editing ? html`<p>${s.description}</p>
        <div class="row">
          <button class="ghost" @click=${() => (this.editing = true)}>${icon("Pencil", 14)} ${t("skill.edit")}</button>
          ${this.download()}
          <button class="ghost" ?disabled=${this.busy} @click=${this.unmark}>${icon("X", 14)} ${t("skill.remove")}</button>
        </div>`
      : html`<p class="muted">${t("skill.intro")}</p>
        <form @submit=${this.save}>
          <label>${t("skill.name")}<input name="name" required maxlength="64" pattern="[a-z0-9]+(-[a-z0-9]+)*" .value=${s?.name ?? skillName(this.pageTitle)}></label>
          <label>${t("skill.description")}<textarea name="description" required maxlength="1024" .value=${s?.description ?? ""} placeholder=${t("skill.description_hint")}></textarea></label>
          <div class="row">
            ${s ? html`<button type="button" class="ghost" @click=${() => (this.editing = false)}>${t("doc.cancel")}</button>` : ""}
            <button ?disabled=${this.busy}>${icon("Sparkles", 14)} ${s ? t("skill.save") : t("skill.mark")}</button>
          </div>
        </form>`}
    </details>`;
  }
}
customElements.define("phx-page-skill", PhxPageSkill);
