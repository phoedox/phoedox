import { LitElement, css, html } from "lit";
import { ApiError, api, type Org, type Project } from "../api";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { currentProject, navigate } from "../state";
import { base } from "./shared";
import { projectBadge } from "../pictures";

interface Template { id: string; name: string; description: string; icon: string }

export class PhxProjects extends LitElement {
  static properties = { projects: { state: true }, orgs: { state: true }, templates: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 980px; margin: 0 auto; padding: 28px 16px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; margin: 18px 0 28px; }
    a.p { text-decoration: none; color: var(--fg); display: flex; gap: 12px; align-items: flex-start;
      transition: border-color var(--dur-fast) ease, transform var(--dur-press) var(--ease-out); }
    a.p:active { transform: scale(0.98); }
    @media (hover: hover) and (pointer: fine) { a.p:hover { border-color: var(--muted); } }
    .pi { width: 36px; height: 36px; border-radius: 9px; display: grid; place-items: center; color: var(--on-color); font: 600 12px var(--font-mono); flex: none; }
    form { display: grid; gap: 10px; max-width: 760px; }
    fieldset.tpl { border: 0; padding: 0; margin: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 8px; }
    fieldset.tpl legend { font-size: 14px; font-weight: 600; margin-bottom: 6px; }
    fieldset.tpl label { position: relative; cursor: pointer; }
    fieldset.tpl input { position: absolute; opacity: 0; }
    fieldset.tpl span { display: grid; gap: 2px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); height: 100%; box-sizing: border-box; }
    fieldset.tpl span .ico { color: var(--accent); }
    fieldset.tpl small { color: var(--muted); font-size: 12.5px; }
    fieldset.tpl input:checked + span { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent); }
    fieldset.tpl input:focus-visible + span { outline: 2px solid var(--accent); outline-offset: 2px; }
    h2.org { font-size: 15px; display: flex; gap: 8px; align-items: center; margin: 22px 0 0; color: var(--muted); }
    h2.org a { color: inherit; text-decoration: none; }
  `];
  projects: Project[] = [];
  orgs: Org[] = [];
  templates: Template[] = [];
  error = "";

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    [this.projects, this.orgs] = await Promise.all([
      api<Project[]>("GET", "/projects").catch(() => []).then((x) => x ?? []),
      api<Org[]>("GET", "/orgs").catch(() => []).then((x) => x ?? []),
    ]);
    this.templates = (await api<Template[]>("GET", `/templates?lang=${locale()}`).catch(() => [])) ?? [];
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      const p = await api<Project>("POST", "/projects", { name: f.get("name"), description: f.get("description"), org: f.get("org") || undefined,
        template: f.get("template") || undefined, templateLocale: locale() });
      navigate(`/p/${p.slug}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  /** Projects by organization; personal projects first. Without organizations no headings. */
  private groups(): [string, string, Project[]][] {
    const orgOf = new Map(this.orgs.map((o) => [o.id, o]));
    const by = new Map<string, Project[]>();
    for (const p of this.projects) by.set(p.orgId ?? "", [...(by.get(p.orgId ?? "") ?? []), p]);
    if (by.size === 1 && by.has("")) return [["", "", this.projects]];
    return [...by].sort(([a], [b]) => (a === "" ? -1 : b === "" ? 1 : 0)).map(([id, ps]) =>
      [id ? orgOf.get(id)?.name ?? ps[0].orgName ?? "" : t("projects.personal"), id ? orgOf.get(id)?.slug ?? "" : "", ps]);
  }

  render() {
    return html`<div class="enter">
      <h1>${t("projects.title")}</h1>
      ${this.projects.length === 0 ? html`<p class="muted">${t("projects.empty")}</p>` : ""}
      ${this.groups().map(([name, slug, ps]) => html`
        ${name ? html`<h2 class="org">${icon(slug ? "Building2" : "UserRound", 15)} ${slug ? html`<a href="/orgs/${slug}">${name}</a>` : name}</h2>` : ""}
        <div class="grid">
          ${ps.map((p) => html`<a class="p card" href="/p/${p.slug}">
            ${projectBadge(p, 36)}
            <span><b>${p.name}</b><br /><span class="muted" style="font-size:13px">${p.description || t(`settings.role.${p.role || "reader"}` as any)}</span></span>
          </a>`)}
        </div>`)}
      <h2 style="font-size:18px;margin-bottom:10px">${icon("Plus", 16)} ${t("projects.new")}</h2>
      <form @submit=${this.create}>
        <label class="field">${t("projects.name")}<input name="name" required /></label>
        <label class="field">${t("projects.description")}<input name="description" /></label>
        <fieldset class="tpl"><legend>${t("projects.template")}</legend>
          <label><input type="radio" name="template" value="" checked> <span>${icon("FilePlus", 18)}<b>${t("projects.template_empty")}</b><small>${t("projects.template_empty_desc")}</small></span></label>
          ${this.templates.map((tp) => html`<label><input type="radio" name="template" value=${tp.id}> <span>${icon(tp.icon, 18)}<b>${tp.name}</b><small>${tp.description}</small></span></label>`)}
        </fieldset>
        ${this.orgs.length ? html`<label class="field">${t("projects.org")}<select name="org">
          <option value="">${t("projects.personal")}</option>
          ${this.orgs.map((o) => html`<option value=${o.slug}>${o.name}</option>`)}</select></label>` : ""}
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
        <div><button type="submit">${t("projects.create")}</button></div>
      </form>
    </div>`;
  }
}
customElements.define("phx-projects", PhxProjects);
