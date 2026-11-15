import { LitElement, css, html } from "lit";
import { ApiError, api, type CodeRepo } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

type EnvVar = { name: string; secret: boolean };

/** Project settings → code: GitHub or Gitea/Forgejo repositories whose commits and releases cards link (ADR-0063). */
export class PhxCodeRepos extends LitElement {
  static properties = { project: {}, owner: { type: Boolean }, repos: { state: true }, secrets: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 760px; }
    ul { list-style: none; padding: 0; margin: 12px 0; display: grid; gap: 8px; }
    li { display: flex; gap: 10px; align-items: center; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
    li .t { flex: 1; min-width: 0; display: grid; gap: 2px; }
    li .t a { font-weight: 600; overflow-wrap: anywhere; }
    li .t small { color: var(--muted); font-size: 12px; }
    li .t small.bad { color: var(--bad); }
    form { display: grid; grid-template-columns: 1fr 220px auto; gap: 8px; align-items: end; }
    @media (max-width: 700px) { form { grid-template-columns: 1fr; } }
    p.hint { font-size: 13px; }
  `];
  project = "";
  owner = false;
  repos: CodeRepo[] = [];
  secrets: string[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.repos = (await api<CodeRepo[]>("GET", `/projects/${this.project}/code/repos`).catch(() => [])) ?? [];
    if (this.owner) {
      const env = (await api<EnvVar[]>("GET", `/projects/${this.project}/env`).catch(() => [])) ?? [];
      this.secrets = env.filter((v) => v.secret).map((v) => v.name);
    }
  }

  private async add(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.busy = true;
    try {
      await api("POST", `/projects/${this.project}/code/repos`, { url: String(f.get("url") ?? "").trim(), tokenVar: String(f.get("token") ?? "") });
      form.reset();
      toast(t("code.repo_added"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async disconnect(r: CodeRepo) {
    if (!confirm(t("code.repo_remove_confirm", { repo: `${r.owner}/${r.name}` }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/code/repos/${r.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    return html`
      <p class="muted hint">${t("code.intro")}</p>
      ${this.repos.length ? html`<ul>${this.repos.map((r) => html`<li>
        ${icon("GitBranch", 18)}
        <span class="t"><a href=${r.webUrl} target="_blank" rel="noopener external">${r.owner}/${r.name}</a>
          <small>${r.provider === "github" ? "GitHub" : "Gitea / Forgejo"}${r.tokenVar ? html` · ${icon("KeyRound", 11)} ${r.tokenVar}` : ` · ${t("code.public")}`}${r.lastCheck ? ` · ${t("code.checked", { date: formatDate(r.lastCheck) })}` : ""}</small>
          ${r.lastError ? html`<small class="bad">${icon("TriangleAlert", 11)} ${r.lastError}</small>` : ""}</span>
        ${this.owner ? html`<button class="ghost icon danger" title=${t("code.repo_remove")} aria-label=${t("code.repo_remove")} @click=${() => this.disconnect(r)}>${icon("Trash2", 15)}</button>` : ""}
      </li>`)}</ul>` : html`<p class="muted">${t("code.none")}</p>`}
      ${this.owner ? html`<form @submit=${this.add}>
        <label class="field">${t("code.repo_url")}<input name="url" type="url" required placeholder="https://github.com/owner/repo" autocomplete="off" spellcheck="false"></label>
        <label class="field">${t("code.token")}<select name="token"><option value="">${t("code.token_none")}</option>${this.secrets.map((n) => html`<option value=${n}>${n}</option>`)}</select></label>
        <button ?disabled=${this.busy}>${icon("Plus", 14)} ${t("code.repo_add")}</button>
      </form>
      <p class="muted hint">${t("code.token_hint")}</p>` : ""}`;
  }
}
customElements.define("phx-code-repos", PhxCodeRepos);
