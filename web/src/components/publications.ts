import { LitElement, css, html } from "lit";
import { ApiError, api, type Project } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

interface Release { id: string; number: number; note: string; createdAt: string }

interface Publication {
  id: string; slug: string; title: string; description: string; projects: string[]; projectNames: string[];
  access: "public" | "password" | "members"; hasPassword: boolean; domain: string; color: string; css: string; noindex: boolean;
  canEdit: boolean; updatedAt: string;
}

/** Publications (ADR-0006): projects as a public or protected site. */
export class PhxPublications extends LitElement {
  static properties = { list: { state: true }, projects: { state: true }, editing: { state: true }, error: { state: true }, busy: { state: true }, releases: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 900px; margin: 0 auto; padding: 28px 16px; }
    .cards { display: grid; gap: 12px; margin: 16px 0 24px; }
    .card { border: 1px solid var(--line); border-radius: 12px; padding: 14px 16px; background: var(--surface); display: grid; gap: 6px; }
    .card .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .card h2 { font-size: 17px; margin: 0; flex: 1; }
    .url { font-family: var(--font-mono); font-size: 13px; }
    form { display: grid; gap: 12px; max-width: 620px; padding: 16px; border: 1px dashed var(--line); border-radius: 12px; }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
    .checks { display: flex; flex-wrap: wrap; gap: 6px 16px; }
    .checks label { display: inline-flex; gap: 6px; align-items: center; font-weight: 400; }
    textarea { min-height: 90px; font-family: var(--font-mono); font-size: 13px; }
    .hint { font-size: 13px; color: var(--muted); margin: 0; }
    @media (max-width: 640px) { .two { grid-template-columns: 1fr; } }
  `];
  list?: Publication[];
  projects: Project[] = [];
  /** The publication in the form ("new" for a new one). */
  editing?: Publication | "new";
  error = "";
  busy = false;
  /** Releases per publication id (loaded with the list). */
  releases: Record<string, Release[]> = {};

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    try {
      this.list = await api<Publication[]>("GET", "/publications");
      const rel: Record<string, Release[]> = {};
      await Promise.all((this.list ?? []).filter((p) => p.canEdit).map(async (p) => {
        rel[p.id] = (await api<Release[]>("GET", `/publications/${p.id}/releases`).catch(() => [])) ?? [];
      }));
      this.releases = rel;
      this.projects = ((await api<Project[]>("GET", "/projects")) ?? []).filter((p) => p.role === "owner");
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private urlOf(p: Publication) {
    return p.domain ? `https://${p.domain}/` : `${location.origin}/pub/${p.slug}/`;
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const s = (k: string) => String(f.get(k) ?? "").trim();
    const body: Record<string, unknown> = {
      title: s("title"), slug: s("slug") || undefined, description: s("description"), projects: f.getAll("projects"),
      access: s("access"), domain: s("domain"), color: s("color") === "#000000" ? "" : s("color"), css: String(f.get("css") ?? ""), noindex: f.get("noindex") === "on",
    };
    if (s("password")) body.password = s("password");
    this.busy = true;
    this.error = "";
    try {
      if (this.editing === "new") await api("POST", "/publications", body);
      else await api("PATCH", `/publications/${this.editing!.id}`, body);
      toast(t("pub.saved"));
      this.editing = undefined;
      await this.load();
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async release(p: Publication) {
    const note = prompt(t("pub.release_prompt"));
    if (note === null) return;
    try {
      const r = await api<Release>("POST", `/publications/${p.id}/releases`, { note });
      toast(t("pub.release_done", { number: r.number }));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async unpublish(p: Publication) {
    if (!confirm(t("pub.delete_confirm", { title: p.title }))) return;
    try {
      await api("DELETE", `/publications/${p.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private form() {
    const p = this.editing === "new" ? undefined : this.editing;
    return html`<form @submit=${this.save}>
      <h2 style="margin:0;font-size:18px">${p ? t("pub.edit") : t("pub.new")}</h2>
      <div class="two">
        <label class="field">${t("pub.title")}<input name="title" required .value=${p?.title ?? ""}></label>
        <label class="field">${t("pub.slug")}<input name="slug" pattern="[a-z0-9-]+" .value=${p?.slug ?? ""} placeholder=${t("pub.slug_auto")}></label>
      </div>
      <label class="field">${t("pub.description")}<input name="description" .value=${p?.description ?? ""}></label>
      <div class="field">${t("pub.projects")}
        <div class="checks">${this.projects.map((x) => html`<label><input type="checkbox" name="projects" value=${x.id} ?checked=${p?.projects.includes(x.id)}> ${x.name}</label>`)}</div>
        <p class="hint">${t("pub.projects_hint")}</p></div>
      <div class="two">
        <label class="field">${t("pub.access")}<select name="access">
          <option value="public" ?selected=${p?.access !== "password"}>${t("pub.access_public")}</option>
          <option value="password" ?selected=${p?.access === "password"}>${t("pub.access_password")}</option>
          <option value="members" ?selected=${p?.access === "members"}>${t("pub.access_members")}</option></select></label>
        <label class="field">${t("pub.password")}<input name="password" type="password" minlength="8" autocomplete="new-password" placeholder=${p?.hasPassword ? t("pub.password_keep") : ""}></label>
      </div>
      <label class="field">${t("pub.domain")}<input name="domain" .value=${p?.domain ?? ""} placeholder="docs.example.org"></label>
      <p class="hint">${t("pub.domain_hint")}</p>
      <div class="two">
        <label class="field">${t("pub.color")}<input name="color" type="color" .value=${p?.color || "#000000"}></label>
        <label class="field" style="flex-direction:row;align-items:center;gap:8px;align-self:end"><input type="checkbox" name="noindex" ?checked=${p?.noindex}> ${t("pub.noindex")}</label>
      </div>
      <label class="field">${t("pub.css")}<textarea name="css" .value=${p?.css ?? ""} placeholder=".prose h2 { … }"></textarea></label>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <div style="display:flex;gap:8px"><button ?disabled=${this.busy}>${t("doc.save")}</button><button type="button" class="ghost" @click=${() => (this.editing = undefined)}>${t("doc.cancel")}</button></div>
    </form>`;
  }

  render() {
    return html`<div class="enter">
      <h1>${t("pub.title_page")}</h1>
      ${settingsNav("publications")}
      <p class="muted">${t("pub.intro")}</p>
      ${!this.list ? html`<p class="muted">${t("app.loading")}</p>` : html`
        <div class="cards">${this.list.map((p) => html`<div class="card">
          <div class="row"><h2>${p.title}</h2>
            <span class="chip">${p.access === "password" ? html`${icon("Lock", 11)} ${t("pub.access_password")}` : p.access === "members" ? html`${icon("Users", 11)} ${t("pub.access_members")}` : html`${icon("Globe", 11)} ${t("pub.access_public")}`}</span>
            ${p.canEdit ? html`<button class="ghost" @click=${() => (this.editing = p)}>${icon("Pencil", 13)} ${t("doc.edit")}</button>
              <button class="danger" @click=${() => this.unpublish(p)}>${icon("Trash2", 13)}</button>` : ""}</div>
          <a class="url" href=${this.urlOf(p)} target="_blank" rel="noopener">${this.urlOf(p)}</a>
          <span class="muted" style="font-size:12.5px">${t("pub.llms")} <a class="url" href=${this.urlOf(p) + "llms.txt"} target="_blank" rel="noopener">llms.txt</a></span>
          ${p.domain ? html`<a class="url" href=${`${location.origin}/pub/${p.slug}/`} target="_blank" rel="noopener">${location.origin}/pub/${p.slug}/</a>` : ""}
          ${p.access === "public" ? html`<details style="font-size:13px"><summary class="muted" style="cursor:pointer">${icon("Code", 12)} ${t("pub.embed_title")}</summary>
            <p class="muted" style="margin:6px 0">${t("pub.embed_hint")}</p>
            <label>${t("pub.embed_page")}<input readonly class="mono" .value=${`<iframe src="${this.urlOf(p)}_embed/<project>/<page>" style="width:100%;height:480px;border:0" loading="lazy" title="${p.title}"></iframe>`} @focus=${(e: Event) => (e.target as HTMLInputElement).select()}></label>
            <label>${t("pub.embed_status")}<input readonly class="mono" .value=${`<iframe src="${this.urlOf(p)}_embed/status" style="width:100%;height:360px;border:0" loading="lazy" title="${p.title}"></iframe>`} @focus=${(e: Event) => (e.target as HTMLInputElement).select()}></label>
            <label>${t("pub.mcp_url")}<input readonly class="mono" .value=${`${this.urlOf(p)}mcp`} @focus=${(e: Event) => (e.target as HTMLInputElement).select()}></label>
            <p class="muted" style="margin:4px 0 0">${t("pub.mcp_hint")}</p></details>` : ""}
          <span class="muted" style="font-size:13px">${p.projectNames.join(", ")} · ${t("doc.updated", { date: formatDate(p.updatedAt) })}</span>
          ${p.canEdit ? html`<div class="row" style="font-size:13px">
            <button class="ghost" style="padding:3px 9px;font-size:13px" @click=${() => this.release(p)}>${icon("Flag", 13)} ${t("pub.release")}</button>
            ${(this.releases[p.id] ?? []).slice(0, 5).map((r) => html`<a href=${`${this.urlOf(p)}v/${r.number}/`} target="_blank" rel="noopener" title=${r.note}>v${r.number} · ${formatDate(r.createdAt)}</a>`)}
          </div>` : ""}
        </div>`)}
        ${this.list.length === 0 ? html`<p class="muted">${t("pub.empty")}</p>` : ""}</div>`}
      ${this.editing ? this.form() : this.projects.length ? html`<button @click=${() => { this.editing = "new"; this.error = ""; }}>${icon("Plus", 14)} ${t("pub.new")}</button>` : html`<p class="hint">${t("pub.no_projects")}</p>`}
    </div>`;
  }
}
customElements.define("phx-publications", PhxPublications);
