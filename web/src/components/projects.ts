import { LitElement, css, html } from "lit";
import { ApiError, api, type Project } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, navigate } from "../state";
import { base } from "./shared";

export class PhxProjects extends LitElement {
  static properties = { projects: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 980px; margin: 0 auto; padding: 28px 16px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; margin: 18px 0 28px; }
    a.p { text-decoration: none; color: var(--fg); display: flex; gap: 12px; align-items: flex-start;
      transition: border-color var(--dur-fast) ease, transform var(--dur-press) var(--ease-out); }
    a.p:active { transform: scale(0.98); }
    @media (hover: hover) and (pointer: fine) { a.p:hover { border-color: var(--muted); } }
    .pi { width: 36px; height: 36px; border-radius: 9px; display: grid; place-items: center; color: #fff; font: 600 12px var(--font-mono); flex: none; }
    form { display: grid; gap: 10px; max-width: 480px; }
  `];
  projects: Project[] = [];
  error = "";

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    this.projects = await api<Project[]>("GET", "/projects").catch(() => []) ?? [];
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      const p = await api<Project>("POST", "/projects", { name: f.get("name"), description: f.get("description") });
      navigate(`/p/${p.slug}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  render() {
    return html`<div class="enter">
      <h1>${t("projects.title")}</h1>
      ${this.projects.length === 0 ? html`<p class="muted">${t("projects.empty")}</p>` : ""}
      <div class="grid">
        ${this.projects.map((p) => html`<a class="p card" href="/p/${p.slug}">
          <span class="pi" style="background:${p.primaryColor}">${p.short}</span>
          <span><b>${p.name}</b><br /><span class="muted" style="font-size:13px">${p.description || t(`settings.role.${p.role || "reader"}` as any)}</span></span>
        </a>`)}
      </div>
      <h2 style="font-size:18px;margin-bottom:10px">${icon("Plus", 16)} ${t("projects.new")}</h2>
      <form @submit=${this.create}>
        <label class="field">${t("projects.name")}<input name="name" required /></label>
        <label class="field">${t("projects.description")}<input name="description" /></label>
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
        <div><button type="submit">${t("projects.create")}</button></div>
      </form>
    </div>`;
  }
}
customElements.define("phx-projects", PhxProjects);
