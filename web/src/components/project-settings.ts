import { LitElement, css, html } from "lit";
import { ApiError, api, hasFeature, projectFeatures, shareURL, type Member, type Org, type Project, type ProjectGroup, type Group, type OrgDetail, type ShareLink } from "../api";
import { applyProjectColor } from "../color";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentProject, currentUser, docsVersion, navigate } from "../state";
import { base } from "./shared";
import { pageHead, scard, settingsUI, srow, sswitch } from "./settings-ui";
import "./usage";
import "./feedback-settings";
import "./themes";
import "./autolink-rules";
import "./glossary";
import "./settings-shell";
import type { Storage } from "../files";
import { storageBar, storageStyles } from "./project-files";
import "./webhooks";
import "./agents";
import "./audit-log";
import "./checks";
import "./project-transfer";
import "./project-plugins";
import "./data-model";
import "./rules";
import { toast } from "./toasts";
import "./code-repos";
import { contentLocales, languageName } from "../languages";
import "./picture-picker";
import { projectBadge } from "../pictures";

type EnvVar = { name: string; value?: string; secret: boolean; vaultRef?: string };

export class PhxProjectSettings extends LitElement {
  static properties = {
    project: {}, section: {}, p: { state: true }, members: { state: true }, env: { state: true }, storage: { state: true }, error: { state: true },
    deleting: { state: true }, groups: { state: true }, orgGroups: { state: true }, orgs: { state: true }, links: { state: true }, newLink: { state: true }, mainDraft: { state: true }, approval: { state: true },
  };
  static styles = [base, storageStyles, settingsUI, css`
    :host { display: block; }
    .scard > .body > p:first-child { margin-top: 0; }
    .scard table { max-width: none; }
    .scard .foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 18px; border-top: 1px solid var(--line); }
    form.grid { display: grid; gap: 12px; max-width: 560px; }
    form.danger-form { padding: 4px 18px 18px; }
    form.danger-form p { margin: 0; }
    .color { display: flex; gap: 8px; align-items: center; } .color input[type=color] { width: 48px; padding: 2px; height: 38px; }
    table { border-collapse: collapse; width: 100%; max-width: 760px; } td { padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); }
    .chip.bad { color: var(--bad); }
    .secret { border-color: var(--accent); }
    fieldset.langs { border: 1px solid var(--line); border-radius: 10px; padding: 10px 14px 12px; margin: 0; display: grid; gap: 10px; }
    fieldset.langs legend { font-weight: 600; padding: 0 4px; }
    fieldset.langs p { margin: 0; font-size: 13px; }
    .checks { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-top: 4px; }
    .checks label { display: inline-flex; gap: 6px; align-items: center; font-weight: 400; white-space: nowrap; }
  `];
  project = "";
  /** The page (ADR-0070): "" (rail on phones), general, people, ai, rules, model, env, code, integrations, data, activity, transfer. */
  section = "";
  p?: Project;
  /** Main language chosen in the form before saving. */
  mainDraft?: string;
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
  /** Deleting the project: "" closed, "open" asks to confirm, "busy" while it runs. */
  deleting: "" | "open" | "busy" = "";
  /** Page changes need an owner's approval (ADR-0042). */
  approval = false;

  async connectedCallback() {
    super.connectedCallback();
    this.p = await api<Project>("GET", `/projects/${this.project}`).catch(() => undefined);
    await this.loadEnv();
    this.approval = (await api<{ enabled: boolean }>("GET", `/projects/${this.project}/approval`).catch(() => undefined))?.enabled ?? false;
    this.storage = await api<Storage>("GET", `/projects/${this.project}/storage`).catch(() => undefined);
    await this.loadAccess();
  }

  private async setApproval(on: boolean) {
    try {
      await api("PUT", `/projects/${this.project}/approval`, { enabled: on });
      this.approval = on;
      toast(t("admin.saved"), "ok");
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
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
      <section class="scard"><header><h2>${t("settings.members")}</h2></header><div class="body">
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
      </form>` : ""}</div></section>

      ${p.orgId ? html`<section class="scard"><header><h2>${t("settings.groups")}</h2></header><div class="body">
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
          <button class="ghost">${icon("Plus", 13)}</button></form>` : ""}</div></section>` : ""}

      ${canEdit ? html`<section class="scard"><header><h2>${t("share.title")}</h2><p>${t("share.intro")}</p></header><div class="body">
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
          <button>${icon("Share2", 13)} ${t("share.create_project")}</button></form>` : ""}</div></section>` : ""}`;
  }

  /** Move the project into another organization (owners). */
  private orgSection() {
    const p = this.p!;
    return html`${this.orgs.length || p.orgId ? html`<section class="scard"><header><h2>${t("settings.org")}</h2><p>${t("settings.org_hint")}</p></header><div class="body">
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
          <button class="ghost">${t("settings.move")}</button></form></div></section>` : ""}`;
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
    return html`<section class="scard"><header><h2>${t("storage.title")}</h2></header><div class="body">
      ${storageBar(s)}
      ${owner ? html`<form class="grid" @submit=${this.setQuota} style="margin-top:12px">
        <label class="field">${t("storage.project_quota")}
          <span class="row"><input name="quota" type="number" min="0" max=${s.maxMb || 100000000} .value=${String(s.projectMb || "")} placeholder=${s.maxMb ? String(s.maxMb) : "∞"} style="width:160px"> MB
          <button class="ghost" type="submit">${t("doc.save")}</button></span></label>
        <p class="muted" style="font-size:13px;margin:0">${s.maxMb ? t("storage.max_hint", { max: s.maxMb }) : t("storage.no_max_hint")}</p>
      </form>` : ""}</div></section>`;
  }

  private async loadEnv() {
    this.env = (await api<EnvVar[]>("GET", `/projects/${this.project}/env`).catch(() => [])) ?? [];
  }

  private async setVar(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    try {
      // "From vault": the value field holds the reference (e.g. secret/data/nas#password or op://Infra/NAS/password).
      const body = f.get("vault") === "on" ? { vaultRef: String(f.get("value") ?? "").trim() } : { value: f.get("value") ?? "", secret: f.get("secret") === "on" };
      await api("PUT", `/projects/${this.project}/env/${encodeURIComponent(String(f.get("envname")))}`, body);
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
    return html`<section class="scard"><header><h2>${t("sx.p.variables")}</h2></header><div class="body">
      ${this.env.length === 0 ? html`<p class="muted">${t("env.empty")}</p>` : html`<table>${this.env.map((v) => html`<tr>
        <td class="mono">${v.name}</td>
        <td class="mono">${v.vaultRef ? html`<span class="chip">${icon("Lock", 12)} ${t("env.vault")}</span> ${v.vaultRef}`
          : v.secret ? html`<span class="chip">${icon("KeyRound", 12)} ${t("env.secret")} · ${t("env.secret_set")}</span>` : v.value}</td>
        <td style="text-align:right">${(v.secret ? owner : canEdit) ? html`<button class="danger" style="font-size:13px;padding:4px 10px" @click=${() => this.deleteVar(v.name)}>${t("env.delete")}</button>` : ""}</td></tr>`)}</table>`}
      ${canEdit ? html`<form @submit=${this.setVar} style="margin-top:14px">
        <div class="row">
          <input name="envname" required pattern="[A-Za-z_][A-Za-z0-9_]{0,63}" placeholder=${t("env.name")} style="width:180px" class="mono" autocomplete="off" />
          <input name="value" placeholder=${t("env.value")} style="flex:1;min-width:200px" autocomplete="off" />
          ${owner ? html`<label class="row" style="gap:6px;font-size:14px"><input type="checkbox" name="secret" style="width:auto" /> ${t("env.secret")}</label>
            <label class="row" style="gap:6px;font-size:14px" title=${t("env.vault_hint")}><input type="checkbox" name="vault" style="width:auto" /> ${t("env.from_vault")}</label>` : ""}
          <button type="submit">${t("env.add")}</button>
        </div>
        ${owner ? "" : html`<p class="muted" style="font-size:13px">${t("env.owner_only")}</p>`}
      </form>` : ""}</div></section>`;
  }

  /** Saves project fields; the rest keeps its current value. */
  private async patch(changes: Partial<Project> & { mainLocale?: string; locales?: string[]; features?: string[] }) {
    const p = this.p!;
    try {
      const np = await api<Project>("PATCH", `/projects/${this.project}`, {
        name: p.name, description: p.description, primaryColor: p.primaryColor, aiWriteMode: p.aiWriteMode, short: p.short,
        mainLocale: p.mainLocale ?? "", locales: p.locales ?? [], features: p.features, ...changes,
      });
      this.p = { ...np, role: p.role };
      currentProject.set(this.p);
      applyProjectColor(document.documentElement, np.primaryColor);
      docsVersion.set(docsVersion.get() + 1);
      toast(t("settings.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private saveDetails(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    void this.patch({ name: String(f.get("name")), description: String(f.get("description") ?? ""), primaryColor: String(f.get("color")) });
  }

  private saveLanguages(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const main = String(f.get("main") ?? "");
    void this.patch({ mainLocale: main, locales: main ? f.getAll("locales").map(String) : [] });
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

  private general(owner: boolean) {
    const p = this.p!;
    const main = this.mainDraft ?? p.mainLocale;
    return html`${pageHead(t("settings.general"), t("sx.p.general_text"))}
      <form @submit=${this.saveDetails}>${scard(t("sx.p.details"), "", html`
        ${srow(t("picture.project"), t("sx.p.picture_help"), owner ? html`<phx-picture-picker .project=${p.slug} .has=${!!p.image}
          @changed=${(e: CustomEvent<string>) => { this.p = { ...p, image: e.detail || undefined }; currentProject.set(this.p); }}>${projectBadge(p, 48)}</phx-picture-picker>` : projectBadge(p, 48))}
        ${srow(t("projects.name"), "", html`<input name="name" .value=${p.name} ?disabled=${!owner} required aria-label=${t("projects.name")} />`)}
        ${srow(t("projects.description"), t("sx.p.description_help"), html`<input name="description" .value=${p.description} ?disabled=${!owner} aria-label=${t("projects.description")} />`)}
        ${srow(t("settings.color"), t("sx.p.color_help"), html`<span class="color"><input type="color" name="color" .value=${p.primaryColor.toLowerCase()} ?disabled=${!owner} aria-label=${t("settings.color")}
            @input=${(e: Event) => applyProjectColor(document.documentElement, (e.target as HTMLInputElement).value)} /><span class="mono">${p.primaryColor}</span></span>`)}
        ${owner ? html`<div class="foot"><button type="submit">${t("doc.save")}</button></div>` : ""}`)}</form>
      ${scard(t("settings.features"), t("settings.features_hint"), projectFeatures.filter((x) => x !== "model").map((x) =>
        srow(t(`feature.${x}` as any), t(`sx.p.feature.${x}` as any), sswitch(p.features.includes(x), t(`feature.${x}` as any),
          (on) => this.patch({ features: on ? [...p.features, x] : p.features.filter((f) => f !== x) }), !owner))))}
      <form @submit=${this.saveLanguages}>${scard(t("settings.languages"), t("settings.languages_hint"), html`
        ${srow(t("settings.main_locale"), t("sx.p.main_locale_help"), html`<select name="main" ?disabled=${!owner} aria-label=${t("settings.main_locale")} @change=${(e: Event) => { this.mainDraft = (e.target as HTMLSelectElement).value; }}>
            <option value="" ?selected=${!p.mainLocale}>${t("settings.single_language")}</option>
            ${contentLocales.map((c) => html`<option value=${c} ?selected=${p.mainLocale === c}>${languageName(c)}</option>`)}
          </select>`)}
        ${main ? srow(t("settings.locales"), t("sx.p.locales_help"), html`<div class="checks">${contentLocales.filter((c) => c !== main).map((c) => html`
              <label><input type="checkbox" name="locales" value=${c} ?checked=${p.locales.includes(c)} ?disabled=${!owner}> ${languageName(c)}</label>`)}</div>`) : ""}
        ${owner ? html`<div class="foot"><button type="submit">${t("doc.save")}</button></div>` : ""}`)}</form>`;
  }

  private ai(owner: boolean) {
    const p = this.p!;
    return html`${pageHead(t("sx.p.ai"), t("sx.p.ai_text"))}
      ${scard(t("settings.ai_mode"), "", html`<div class="choices" role="radiogroup" aria-label=${t("settings.ai_mode")}>
        ${(["off", "read", "suggest", "direct"] as const).map((m) => html`<label class="choice"><input type="radio" name="aimode" value=${m} .checked=${p.aiWriteMode === m} ?disabled=${!owner}
          @change=${() => this.patch({ aiWriteMode: m })}><b>${t(`settings.ai_mode.${m}`)}</b><span>${t(`sx.p.ai.${m}`)}</span></label>`)}</div>`)}
      ${scard(t("approval.title"), "", srow(t("approval.enable"), t("approval.hint"), sswitch(this.approval, t("approval.enable"), (on) => this.setApproval(on), !owner)))}
      ${scard(t("autolink.title"), "", html`<phx-autolink-rules .project=${p.slug} ?owner=${owner}></phx-autolink-rules>`)}
      ${scard(t("glossary.title"), t("glossary.text"), html`<phx-glossary .project=${p.slug} .mainLocale=${p.mainLocale ?? ""} .locales=${p.locales ?? []} ?canEdit=${["editor", "owner"].includes(p.role ?? "")}></phx-glossary>`)}`;
  }

  private page(owner: boolean, canEdit: boolean, withModel: boolean) {
    const card = (title: string, sub: string, body: unknown) => scard(title, sub, html`<div class="body">${body}</div>`);
    switch (this.section) {
      case "general": return this.general(owner);
      case "people": return html`${pageHead(t("sx.p.people"), t("sx.p.people_text"))}${this.accessSection(owner, canEdit)}`;
      case "ai": return this.ai(owner);
      case "rules": return canEdit ? html`${pageHead(t("sx.p.auto"), t("sx.p.auto_text"))}
        ${card(t("auto.title"), "", html`<phx-rules .project=${this.project} ?owner=${owner}></phx-rules>`)}
        ${card(t("check.title"), t("check.settings_hint"), html`<phx-checks .project=${this.project}></phx-checks>`)}` : "";
      case "model": return withModel ? html`${pageHead(t("kg.model"), "")}<phx-data-model .project=${this.project}></phx-data-model>` : "";
      case "env": return html`${pageHead(t("sx.p.env"), t("env.intro"))}${this.envSection(owner, canEdit)}`;
      case "code": return canEdit ? html`${pageHead(t("sx.p.code"), t("sx.p.code_text"))}
        ${card(t("code.title"), "", html`<phx-code-repos .project=${this.project} ?owner=${owner}></phx-code-repos>`)}
        ${owner ? card(t("git.title"), t("git.hint"), html`<phx-project-transfer .project=${this.project}></phx-project-transfer>`) : ""}` : "";
      case "integrations": return owner ? html`${pageHead(t("sx.p.int"), "")}
        ${card(t("agents.title"), "", html`<phx-agents .project=${this.project}></phx-agents>`)}
        ${card(t("plugins.title"), "", html`<phx-project-plugins .project=${this.project}></phx-project-plugins>`)}
        ${card(t("hooks.title"), "", html`<phx-webhooks .project=${this.project}></phx-webhooks>`)}` : "";
      case "usage": return owner ? html`<phx-usage level="project" .ref=${this.project}></phx-usage>` : "";
      case "feedback": return owner ? html`<phx-feedback-settings .project=${this.project}></phx-feedback-settings>` : "";
      case "appearance": return owner ? html`${pageHead(t("theme.appearance"))}<phx-theme-list scope="project" .owner=${this.project}></phx-theme-list>` : "";
      case "data": return html`${pageHead(t("sx.p.data"), "")}
        ${this.storageSection(owner)}
        ${scard(t("sx.p.take_along"), "", html`
          ${srow(t("print.project"), t("print.project_hint"), html`<a class="btn ghost" href="/print/${this.project}" target="_blank" rel="noopener">${icon("FileDown", 14)} ${t("print.save")}</a>`)}
          ${srow(t("offline.title"), t("offline.hint"), html`<a class="btn ghost" href=${`/api/v1/projects/${this.project}/offline`} download>${icon("HardDriveDownload", 14)} ${t("offline.download")}</a>`)}
          ${owner ? srow(t("transfer.settings_title"), t("transfer.export_hint"), html`<a class="btn ghost" href="/api/v1/projects/${this.project}/export" download>${icon("Download", 14)} ${t("transfer.export")}</a>`) : ""}`)}`;
      case "activity": return owner ? html`${pageHead(t("audit.project_title"), t("sx.p.activity_text"))}
        ${card(t("audit.project_title"), "", html`<phx-audit-log .project=${this.project}></phx-audit-log>`)}` : "";
      case "transfer": return owner ? html`${pageHead(t("sx.p.transfer"), "")}${this.orgSection()}${this.deleteSection()}` : "";
    }
    return "";
  }

  /** Danger zone: delete the project (ADR-0071). */
  private deleteSection() {
    const p = this.p!;
    const u = currentUser.get();
    const days = this.deleting === "open";
    return scard(t("ptrash.danger"), "", html`${srow(t("ptrash.delete"), t("ptrash.delete_help"), days ? "" : html`<button class="danger" @click=${() => (this.deleting = "open")}>${icon("Trash2", 14)} ${t("ptrash.delete")}…</button>`)}
      ${days ? html`<form class="grid danger-form" @submit=${this.deleteProject}>
        <p>${t("ptrash.confirm_text", { slug: p.slug })}</p>
        <label class="field">${t("ptrash.confirm_label")}<input name="confirm" required autocomplete="off" spellcheck="false" placeholder=${p.slug} /></label>
        ${u?.hasPassword ? html`<label class="field">${t("security.confirm_password")}<input name="password" type="password" autocomplete="current-password" required /></label>`
          : u?.secondFactor ? html`<label class="field">${t("security.confirm_code")}<input name="code" inputmode="numeric" autocomplete="one-time-code" required /></label>` : ""}
        <div class="row"><button type="submit" class="danger" ?disabled=${this.deleting === "busy"}>${icon("Trash2", 14)} ${t("ptrash.delete_final")}</button>
          <button type="button" class="ghost" @click=${() => (this.deleting = "")}>${t("doc.cancel")}</button></div>
      </form>` : ""}`, "danger");
  }

  private async deleteProject(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.deleting = "busy";
    try {
      await api("DELETE", `/projects/${this.project}`, { confirm: f.get("confirm"), password: f.get("password") ?? "", code: f.get("code") ?? "" });
      currentProject.set(null);
      toast(t("ptrash.deleted_toast", { name: this.p!.name }));
      navigate("/");
    } catch (err) {
      this.deleting = "open";
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    const p = this.p;
    if (!p) return html`<p class="muted" style="padding:28px">${t("app.loading")}</p>`;
    const owner = p.role === "owner";
    const canEdit = owner || p.role === "editor";
    const withModel = hasFeature(p, "graph") || hasFeature(p, "model");
    return html`<phx-settings-shell area="project" active=${this.section} .ctx=${{ slug: p.slug, name: p.name, owner, canEdit, withModel }}>
      <div class="enter">${this.page(owner, canEdit, withModel) || (this.section ? html`<p class="muted">${t("sx.p.unavailable")}</p>` : "")}</div></phx-settings-shell>`;
  }
}
customElements.define("phx-project-settings", PhxProjectSettings);
