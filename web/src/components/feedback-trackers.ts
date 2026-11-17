import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

export interface Tracker { id: string; name: string; kind: string; url: string; jiraProject?: string; issueType?: string; username?: string; tokenVar?: string; labels: string[] }
interface EnvVar { name: string; secret: boolean }

export const TRACKER_KINDS: [string, string][] = [["github", "GitHub"], ["gitea", "Gitea / Forgejo"], ["gitlab", "GitLab"], ["jira", "Jira"]];
export const trackerKind = (k: string) => TRACKER_KINDS.find(([id]) => id === k)?.[1] ?? k;

/** Project settings → Feedback portals: external trackers topics can be
 * forwarded to (owners). */
export class PhxFeedbackTrackers extends LitElement {
  static properties = { project: {}, trackers: { state: true }, secrets: { state: true }, kind: { state: true }, busy: { state: true }, open: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .card { border: 1px solid var(--line); border-radius: 14px; background: var(--surface); padding: 16px; display: grid; gap: 12px; margin-bottom: 14px; }
    .card h3 { margin: 0; font-size: 15px; display: flex; gap: 8px; align-items: center; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; }
    .item { display: flex; gap: 10px; align-items: center; border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; }
    .item div { flex: 1; min-width: 0; }
    .item small { color: var(--muted); display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .muted { color: var(--muted); font-size: 13px; margin: 0; }
  `];

  project = "";
  trackers: Tracker[] = [];
  secrets: string[] = [];
  kind = "github";
  busy = false;
  open = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.trackers = (await api<Tracker[]>("GET", `/projects/${this.project}/feedback/trackers`).catch(() => [])) ?? [];
    const env = (await api<EnvVar[]>("GET", `/projects/${this.project}/env`).catch(() => [])) ?? [];
    this.secrets = env.filter((v) => v.secret).map((v) => v.name);
  }

  private async add(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const s = (k: string) => String(f.get(k) ?? "").trim();
    this.busy = true;
    try {
      await api("POST", `/projects/${this.project}/feedback/trackers`, {
        kind: this.kind, name: s("name"), url: s("url"), tokenVar: s("token"), jiraProject: s("jiraProject"), issueType: s("issueType"), username: s("username"),
        labels: s("labels").split(",").map((x) => x.trim()).filter(Boolean),
      });
      form.reset();
      this.open = false;
      toast(t("fbt.added"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeTracker(tr: Tracker) {
    if (!confirm(t("fbt.remove_confirm", { name: tr.name }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/feedback/trackers/${tr.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    const jira = this.kind === "jira";
    return html`<div class="card"><h3>${icon("Share2", 15)} ${t("fbt.title")}</h3>
      <p class="muted">${t("fbt.hint")}</p>
      ${this.trackers.map((tr) => html`<div class="item">${icon(tr.kind === "jira" ? "SquareKanban" : "GitBranch", 16)}<div><strong>${tr.name}</strong>
          <small>${trackerKind(tr.kind)} · ${tr.url}${tr.jiraProject ? ` · ${tr.jiraProject}` : ""}${tr.tokenVar ? html` · ${icon("KeyRound", 11)} ${tr.tokenVar}` : nothing}</small></div>
        <button class="ghost" aria-label=${t("fbt.remove")} @click=${() => this.removeTracker(tr)}>${icon("Trash2", 14)}</button></div>`)}
      ${this.open ? html`<form @submit=${this.add} style="display:grid;gap:10px">
          <div class="grid">
            <label class="field">${t("fbt.kind")}<select @change=${(e: Event) => (this.kind = (e.target as HTMLSelectElement).value)}>
              ${TRACKER_KINDS.map(([id, name]) => html`<option value=${id} ?selected=${id === this.kind}>${name}</option>`)}</select></label>
            <label class="field">${t("fbt.name")}<input name="name" maxlength="80" placeholder=${t("fbt.name_ph")}></label>
          </div>
          <label class="field">${jira ? t("fbt.url_jira") : t("fbt.url")}<input name="url" type="url" required placeholder=${jira ? "https://example.atlassian.net" : this.kind === "gitlab" ? "https://gitlab.com/group/project" : "https://github.com/owner/repo"}></label>
          ${jira ? html`<div class="grid">
              <label class="field">${t("fbt.jira_project")}<input name="jiraProject" required placeholder="GAME" pattern="[A-Za-z][A-Za-z0-9_]{1,30}"></label>
              <label class="field">${t("fbt.issue_type")}<input name="issueType" placeholder="Bug"></label>
              <label class="field">${t("fbt.username")}<input name="username" type="email" placeholder="me@example.com"></label>
            </div>` : nothing}
          <div class="grid">
            <label class="field">${t("fbt.token")}<select name="token" required><option value=""></option>${this.secrets.map((n) => html`<option value=${n}>{{${n}}}</option>`)}</select></label>
            <label class="field">${t("fbt.labels")}<input name="labels" placeholder="feedback"></label>
          </div>
          <p class="muted">${this.secrets.length ? t(jira ? "fbt.token_hint_jira" : "fbt.token_hint") : t("fbt.no_secrets")}</p>
          <div class="row"><button ?disabled=${this.busy}>${icon("Plus", 14)} ${t("fbt.add")}</button>
            <button type="button" class="ghost" @click=${() => (this.open = false)}>${t("doc.cancel")}</button></div>
        </form>` : html`<div><button class="ghost" @click=${() => (this.open = true)}>${icon("Plus", 14)} ${t("fbt.add")}</button></div>`}
    </div>`;
  }
}
customElements.get("phx-feedback-trackers") || customElements.define("phx-feedback-trackers", PhxFeedbackTrackers);
