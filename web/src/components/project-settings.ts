import { LitElement, css, html } from "lit";
import { ApiError, api, shareURL, type Member, type Org, type Project, type ProjectGroup, type Group, type OrgDetail, type ShareLink } from "../api";
import { applyProjectColor } from "../color";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import type { Storage } from "../files";
import { storageBar, storageStyles } from "./project-files";
import "./webhooks";
import "./project-plugins";
import { toast } from "./toasts";

type EnvVar = { name: string; value?: string; secret: boolean };

export class PhxProjectSettings extends LitElement {
  static properties = {
    project: {}, p: { state: true }, members: { state: true }, env: { state: true }, storage: { state: true }, error: { state: true },
    groups: { state: true }, orgGroups: { state: true }, orgs: { state: true }, links: { state: true }, newLink: { state: true },
  };
  static styles = [base, storageStyles, css`
    form { display: grid; gap: 12px; max-width: 560px; margin-bottom: 28px; }
    .color { display: flex; gap: 8px; align-items: center; } .color input[type=color] { width: 48px; padding: 2px; height: 38px; }
    table { border-collapse: collapse; width: 100%; max-width: 760px; } td { padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); }
    .chip.bad { color: var(--bad); }
    .secret { border-color: var(--accent); }
  `];
  project = "";
  p?: Project;
  members: Member[] = [];
  env: EnvVar[] = [];
  storage?: Storage;
  error = "";
  groups: ProjectGroup[] = [];
  /** Groups of the project's organization (to add). */
  orgGroups: Group[] = [];
  orgs: Org[] = [];
  links: ShareLink[] = [];
  /** A link just created: its token is shown once. */
  newLink?: ShareLink;

  async connectedCallback() {
    super.connectedCallback();
    this.p = await api<Project>("GET", `/projects/${this.project}`).catch(() => undefined);
    await this.loadEnv();
    this.storage = await api<Storage>("GET", `/projects/${this.project}/storage`).catch(() => undefined);
    await this.loadAccess();
  }

  private async loadAccess() {
    const p = this.p;
    if (!p) return;
    const canEdit = p.role === "owner" || p.role === "editor";
    [this.members, this.groups, this.links, this.orgs] = await Promise.all([
      api<Member[]>("GET", `/projects/${this.project}/members`).catch(() => []).then((x) => x ?? []),
      api<ProjectGroup[]>("GET", `/projects/${this.project}/groups`).catch(() => []).then((x) => x ?? []),
      canEdit ? api<ShareLink[]>("GET", `/projects/${this.project}/share-links`).catch(() => []).then((x) => x ?? []) : Promise.resolve([]),
      p.role === "owner" ? api<Org[]>("GET", "/orgs").catch(() => []).then((x) => x ?? []) : Promise.resolve([]),
    ]);
    if (p.orgId && p.role === "owner") {
      const o = await api<OrgDetail>("GET", `/orgs/${p.orgId}`).catch(() => undefined);
      this.orgGroups = o?.groups ?? [];
    }
  }

  private async act(fn: () => Promise<unknown>, done?: string) {
    try {
      await fn();
      await this.loadAccess();
      if (done) toast(done);
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private accessSection(owner: boolean, canEdit: boolean) {
    const p = this.p!;
    const now = Date.now();
    return html`
      <h2 style="font-size:18px;margin:28px 0 8px">${t("settings.members")}</h2>
      <table>${this.members.map((m) => {
        const expired = m.expiresAt && Date.parse(m.expiresAt) < now;
        return html`<tr><td>${m.name}</td><td class="muted">${m.email}</td>
          <td>${t(`settings.role.${m.role}` as any)}${m.expiresAt ? html` <span class="chip ${expired ? "bad" : ""}" title=${t("settings.guest_hint")}>${icon("Clock", 11)} ${t(expired ? "settings.guest_expired" : "settings.guest_until", { date: formatDate(m.expiresAt) })}</span>` : ""}</td>
          <td style="text-align:right">${owner ? html`<button class="ghost" style="padding:3px 8px" aria-label=${t("settings.remove_member", { name: m.name })}
            @click=${() => confirm(t("settings.remove_member_confirm", { name: m.name })) && this.act(() => api("DELETE", `/projects/${this.project}/members/${m.userId}`))}>${icon("UserMinus", 13)}</button>` : ""}</td></tr>`;
      })}</table>
      ${owner ? html`<form @submit=${this.addMember} style="margin-top:14px">
        <div class="row"><input name="email" type="email" required placeholder=${t("login.email")} style="flex:1;min-width:200px" aria-label=${t("login.email")} />
          <select name="role" style="width:auto"><option value="reader">${t("settings.role.reader")}</option><option value="editor">${t("settings.role.editor")}</option><option value="owner">${t("settings.role.owner")}</option></select>
          <label class="row" style="gap:6px;font-size:13px;color:var(--muted)">${t("settings.guest_until_label")}<input name="until" type="date" style="width:auto" min=${new Date().toISOString().slice(0, 10)}></label>
          <button type="submit">${t("settings.add_member")}</button></div>
        <p class="muted" style="font-size:13px;margin:0">${t("settings.guest_hint")}</p>
      </form>` : ""}

      ${p.orgId ? html`<h2 style="font-size:18px;margin:28px 0 8px">${t("settings.groups")}</h2>
        ${this.groups.length ? html`<table>${this.groups.map((g) => html`<tr><td>${icon("Users", 13)} ${g.name}</td>
          <td>${owner ? html`<select style="width:auto" aria-label=${t("settings.group_role", { name: g.name })} @change=${(e: Event) => this.act(() => api("PUT", `/projects/${this.project}/groups/${g.groupId}`, { role: (e.target as HTMLSelectElement).value }))}>
            ${["reader", "editor", "owner"].map((r) => html`<option value=${r} ?selected=${g.role === r}>${t(`settings.role.${r}` as any)}</option>`)}</select>` : t(`settings.role.${g.role}` as any)}</td>
          <td style="text-align:right">${owner ? html`<button class="ghost" style="padding:3px 8px" aria-label=${t("settings.remove_group", { name: g.name })} @click=${() => this.act(() => api("DELETE", `/projects/${this.project}/groups/${g.groupId}`))}>${icon("X", 13)}</button>` : ""}</td></tr>`)}</table>`
          : html`<p class="muted">${t("settings.no_groups")}</p>`}
        ${owner && this.orgGroups.some((g) => !this.groups.some((x) => x.groupId === g.id)) ? html`<form class="row" style="margin-top:10px" @submit=${(e: SubmitEvent) => {
          e.preventDefault();
          const f = new FormData(e.target as HTMLFormElement);
          this.act(() => api("PUT", `/projects/${this.project}/groups/${f.get("group")}`, { role: f.get("role") }));
        }}><select name="group" required style="width:auto"><option value="">${t("settings.add_group")}</option>
          ${this.orgGroups.filter((g) => !this.groups.some((x) => x.groupId === g.id)).map((g) => html`<option value=${g.id}>${g.name}</option>`)}</select>
          <select name="role" style="width:auto"><option value="reader">${t("settings.role.reader")}</option><option value="editor">${t("settings.role.editor")}</option><option value="owner">${t("settings.role.owner")}</option></select>
          <button class="ghost">${icon("Plus", 13)}</button></form>` : ""}` : ""}

      ${canEdit ? html`<h2 style="font-size:18px;margin:28px 0 6px">${t("share.title")}</h2>
        <p class="muted" style="max-width:640px;margin-top:0">${t("share.intro")}</p>
        ${this.newLink?.token ? html`<div class="card secret" style="margin-bottom:12px"><b>${t("share.created")}</b>
          <p class="row" style="margin:8px 0 0"><code style="word-break:break-all">${shareURL(this.newLink.token)}</code>
          <button class="ghost" @click=${() => navigator.clipboard?.writeText(shareURL(this.newLink!.token!)).then(() => toast(t("tokens.copied")))}>${icon("Copy", 13)} ${t("tokens.copy")}</button></p></div>` : ""}
        ${this.links.length ? html`<table>${this.links.map((l) => html`<tr>
          <td>${icon(l.documentId ? "FileText" : "FolderOpen", 13)} ${l.label || t(l.documentId ? "share.one_doc" : "share.whole_project")}</td>
          <td class="muted" style="font-size:13px">${l.expiresAt ? t("share.until", { date: formatDate(l.expiresAt) }) : t("share.no_expiry")} · ${l.lastUsedAt ? t("share.used", { date: formatDate(l.lastUsedAt) }) : t("tokens.never_used")}</td>
          <td style="text-align:right"><button class="danger" style="padding:3px 8px;font-size:13px" @click=${() => this.act(() => api("DELETE", `/projects/${this.project}/share-links/${l.id}`), t("share.revoked"))}>${t("share.revoke")}</button></td></tr>`)}</table>`
          : html`<p class="muted">${t("share.none")}</p>`}
        ${owner ? html`<form class="row" style="margin-top:10px" @submit=${async (e: SubmitEvent) => {
          e.preventDefault();
          const f = new FormData(e.target as HTMLFormElement);
          try {
            this.newLink = await api<ShareLink>("POST", `/projects/${this.project}/share-links`, { label: f.get("label"), days: Number(f.get("days") || 0) });
            await this.loadAccess();
          } catch (err) { toast(err instanceof ApiError ? err.text : t("error.unknown"), "error"); }
        }}><input name="label" maxlength="100" placeholder=${t("share.label")} aria-label=${t("share.label")} style="flex:1;min-width:180px">
          <select name="days" style="width:auto" aria-label=${t("share.validity")}><option value="7">${t("share.days", { days: 7 })}</option><option value="30">${t("share.days", { days: 30 })}</option><option value="365">${t("share.days", { days: 365 })}</option><option value="0">${t("share.no_expiry")}</option></select>
          <button>${icon("Share2", 13)} ${t("share.create_project")}</button></form>` : ""}` : ""}

      ${owner && (this.orgs.length || p.orgId) ? html`<h2 style="font-size:18px;margin:28px 0 8px">${t("settings.org")}</h2>
        <form class="row" @submit=${(e: SubmitEvent) => {
          e.preventDefault();
          const org = String(new FormData(e.target as HTMLFormElement).get("org") ?? "");
          this.act(async () => {
            const np = await api<Project>("PATCH", `/projects/${this.project}`, { name: p.name, short: p.short, description: p.description, primaryColor: p.primaryColor, aiWriteMode: p.aiWriteMode, org });
            this.p = { ...np, role: p.role };
            currentProject.set(this.p);
          }, t("settings.saved"));
        }}><select name="org" style="width:auto"><option value="" ?selected=${!p.orgId}>${t("projects.personal")}</option>
          ${this.orgs.map((o) => html`<option value=${o.slug} ?selected=${o.id === p.orgId}>${o.name}</option>`)}</select>
          <button class="ghost">${t("settings.move")}</button></form>
        <p class="muted" style="font-size:13px">${t("settings.org_hint")}</p>` : ""}`;
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
      await api("PUT", `/projects/${this.project}/members`, { email: f.get("email"), role: f.get("role"), until: f.get("until") || undefined });
      await this.loadAccess();
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
      ${this.accessSection(owner, canEdit)}
      ${this.envSection(owner, canEdit)}
      ${this.storageSection(owner)}
      ${owner ? html`<h2 style="font-size:18px;margin:28px 0 6px">${t("plugins.title")}</h2><phx-project-plugins .project=${this.project}></phx-project-plugins>` : ""}
      ${owner ? html`<h2 style="font-size:18px;margin:28px 0 6px">${t("hooks.title")}</h2><phx-webhooks .project=${this.project}></phx-webhooks>` : ""}
    </div>`;
  }
}
customElements.define("phx-project-settings", PhxProjectSettings);
