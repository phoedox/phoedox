import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type Group, type Org, type OrgDetail } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, currentUser, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";
import "./org-domain";

const fail = (e: unknown) => toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");

const pageStyles = css`
  :host { display: block; max-width: 900px; margin: 0 auto; padding: 28px 16px 60px; }
  h1 { margin-bottom: 6px; }
  h2 { font-size: 18px; margin: 28px 0 10px; }
  table { width: 100%; border-collapse: collapse; font-size: 14px; }
  td { padding: 7px 8px 7px 0; border-bottom: 1px solid var(--line); vertical-align: middle; }
  td.r { text-align: right; white-space: nowrap; }
  td button, td select { padding: 3px 8px; font-size: 13px; width: auto; }
  form.inline { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
  form.inline input { flex: 1; min-width: 200px; }
  form.inline select { width: auto; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; margin-top: 16px; }
  a.org { text-decoration: none; color: var(--fg); display: flex; gap: 12px; align-items: center; }
  @media (hover: hover) and (pointer: fine) { a.org:hover { border-color: var(--muted); } }
  .group { margin-bottom: 12px; }
  .group header { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 6px; }
  .group header b { margin-right: auto; }
  .chips { display: flex; gap: 6px; flex-wrap: wrap; }
  .chips .chip button { all: unset; cursor: pointer; display: inline-flex; margin-left: 2px; }
  label.switch { display: flex; gap: 10px; align-items: center; font-weight: 600; font-size: 15px; }
  label.switch input { width: auto; }
`;

/** Organizations of the user; system admins create them here. */
export class PhxOrgs extends SignalWatcher(LitElement) {
  static properties = { orgs: { state: true }, error: { state: true } };
  static styles = [base, pageStyles];
  orgs?: Org[];
  error = "";

  async connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
    this.orgs = (await api<Org[]>("GET", "/orgs").catch(() => [])) ?? [];
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      const o = await api<Org>("POST", "/orgs", { name: f.get("name"), usageType: f.get("usage"), adminEmail: f.get("admin") || undefined });
      navigate(`/orgs/${o.slug}`);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  render() {
    const admin = currentUser.get()?.isAdmin;
    return html`<div class="enter">
      <h1>${t("orgs.title")}</h1>
      <p class="muted">${t("orgs.intro")}</p>
      ${this.orgs?.length ? html`<div class="grid">${this.orgs.map((o) => html`<a class="org card" href="/orgs/${o.slug}">
        ${icon("Building2", 22)}<span><b>${o.name}</b><br><span class="muted" style="font-size:13px">${t(o.role === "admin" ? "orgs.role.admin" : "orgs.role.member")} · ${t(`orgs.usage.${o.usageType}`)}</span></span></a>`)}</div>`
        : html`<p class="muted">${t(this.orgs ? "orgs.empty" : "app.loading")}</p>`}
      ${admin ? html`<h2>${icon("Plus", 16)} ${t("orgs.new")}</h2>
        <form @submit=${this.create} style="display:grid;gap:10px;max-width:480px">
          <label class="field">${t("orgs.name")}<input name="name" required maxlength="100"></label>
          <label class="field">${t("orgs.usage")}<select name="usage"><option value="commercial">${t("orgs.usage.commercial")}</option><option value="noncommercial">${t("orgs.usage.noncommercial")}</option></select></label>
          <label class="field">${t("orgs.first_admin")}<input name="admin" type="email" placeholder=${currentUser.get()?.email ?? ""}></label>
          ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
          <div><button>${t("orgs.create")}</button></div>
        </form>` : ""}
    </div>`;
  }
}
customElements.define("phx-orgs", PhxOrgs);

/** One organization: settings, members and groups (admins edit). */
export class PhxOrg extends SignalWatcher(LitElement) {
  static properties = { slug: {}, o: { state: true }, error: { state: true } };
  static styles = [base, pageStyles];
  slug = "";
  o?: OrgDetail;
  error = "";
  private loaded = "";

  async willUpdate() {
    if (this.loaded !== this.slug) {
      this.loaded = this.slug;
      currentProject.set(null);
      await this.load();
    }
  }

  private async load() {
    try {
      this.o = await api<OrgDetail>("GET", `/orgs/${this.slug}`);
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async run(fn: () => Promise<unknown>, done?: string) {
    try {
      await fn();
      await this.load();
      if (done) toast(done);
    } catch (e) {
      fail(e);
    }
  }

  private save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const body: Record<string, unknown> = { name: f.get("name"), aiEnabled: f.get("ai") === "on", quotaMb: Number(f.get("quota") || 0) };
    if (currentUser.get()?.isAdmin) body.usageType = f.get("usage");
    this.run(() => api("PATCH", `/orgs/${this.slug}`, body), t("settings.saved"));
  }

  private addMember(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.run(async () => { await api("PUT", `/orgs/${this.slug}/members`, { email: f.get("email"), role: f.get("role") }); form.reset(); });
  }

  private addGroup(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.run(async () => { await api("POST", `/orgs/${this.slug}/groups`, { name: f.get("name"), oidcGroup: f.get("oidc") ?? "" }); form.reset(); });
  }

  private group(g: Group, admin: boolean) {
    return html`<div class="group card">
      <header><b>${g.name}</b>
        ${g.oidcGroup ? html`<span class="chip" title=${t("orgs.oidc_hint")}>${icon("ShieldCheck", 12)} ${g.oidcGroup}</span>` : ""}
        ${admin ? html`<button class="ghost" style="padding:3px 8px" @click=${() => {
          const oidc = prompt(t("orgs.oidc_prompt"), g.oidcGroup);
          if (oidc !== null) this.run(() => api("PATCH", `/orgs/${this.slug}/groups/${g.id}`, { name: g.name, oidcGroup: oidc }));
        }}>${t("orgs.oidc_set")}</button>
        <button class="danger" style="padding:3px 8px" @click=${() => confirm(t("orgs.group_delete_confirm", { name: g.name })) && this.run(() => api("DELETE", `/orgs/${this.slug}/groups/${g.id}`))}>${icon("Trash2", 13)}</button>` : ""}
      </header>
      <div class="chips">${g.members.length ? g.members.map((m) => html`<span class="chip">${m.name}${m.role === "oidc" ? html` <span title=${t("orgs.from_oidc")}>${icon("ShieldCheck", 11)}</span>` : ""}
        ${admin && m.role !== "oidc" ? html`<button aria-label=${t("orgs.remove_from_group", { name: m.name })} @click=${() => this.run(() => api("POST", `/orgs/${this.slug}/groups/${g.id}/members/remove`, { email: m.email }))}>${icon("X", 11)}</button>` : ""}</span>`)
        : html`<span class="muted" style="font-size:13px">${t("orgs.group_empty")}</span>`}</div>
      ${admin ? html`<form class="inline" @submit=${(e: SubmitEvent) => {
        e.preventDefault();
        const form = e.target as HTMLFormElement;
        const email = new FormData(form).get("email");
        this.run(async () => { await api("POST", `/orgs/${this.slug}/groups/${g.id}/members`, { email }); form.reset(); });
      }}><select name="email" required aria-label=${t("orgs.add_to_group")}><option value="">${t("orgs.add_to_group")}</option>
        ${this.o!.members.filter((m) => !g.members.some((x) => x.userId === m.userId)).map((m) => html`<option value=${m.email}>${m.name}</option>`)}</select>
        <button class="ghost">${icon("Plus", 13)}</button></form>` : ""}
    </div>`;
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const o = this.o;
    if (!o) return html`<p class="muted">${t("app.loading")}</p>`;
    const admin = o.role === "admin";
    const sys = currentUser.get()?.isAdmin;
    const me = currentUser.get()?.id;
    return html`<div class="enter">
      <p class="muted" style="margin:0"><a href="/orgs">${t("orgs.title")}</a></p>
      <h1>${o.name}</h1>
      <p class="row"><span class="chip">${t(`orgs.usage.${o.usageType}`)}</span><span class="chip">${t(admin ? "orgs.role.admin" : "orgs.role.member")}</span></p>

      ${admin ? html`<h2>${t("orgs.settings")}</h2>
        <form @submit=${this.save} style="display:grid;gap:12px;max-width:520px">
          <label class="field">${t("orgs.name")}<input name="name" .value=${o.name} required maxlength="100"></label>
          ${sys ? html`<label class="field">${t("orgs.usage")}<select name="usage">
            <option value="commercial" ?selected=${o.usageType === "commercial"}>${t("orgs.usage.commercial")}</option>
            <option value="noncommercial" ?selected=${o.usageType === "noncommercial"}>${t("orgs.usage.noncommercial")}</option></select></label>` : ""}
          <label class="switch"><input type="checkbox" name="ai" .checked=${o.aiEnabled}> ${t("orgs.ai")}</label>
          <p class="muted" style="margin:0;font-size:13px">${t("orgs.ai_hint")}</p>
          <label class="field">${t("orgs.quota")}<input name="quota" type="number" min="0" .value=${String(o.quotaMb || "")} placeholder="∞" style="max-width:180px"></label>
          <div><button>${t("doc.save")}</button></div>
        </form>
        <h2>${icon("Globe", 16)} ${t("orgdomain.title")}</h2>
        <phx-org-domain .org=${this.slug}></phx-org-domain>` : ""}

      <h2>${t("orgs.members")}</h2>
      <table>${o.members.map((m) => html`<tr><td>${m.name}</td><td class="muted">${m.email}</td>
        <td class="r">${admin ? html`<select aria-label=${t("orgs.role")} @change=${(e: Event) => this.run(() => api("PUT", `/orgs/${this.slug}/members`, { email: m.email, role: (e.target as HTMLSelectElement).value }))}>
            <option value="member" ?selected=${m.role === "member"}>${t("orgs.role.member")}</option><option value="admin" ?selected=${m.role === "admin"}>${t("orgs.role.admin")}</option></select>
          <button class="danger" aria-label=${t("orgs.remove", { name: m.name })} @click=${() => confirm(t("orgs.remove_confirm", { name: m.name })) && this.run(() => api("DELETE", `/orgs/${this.slug}/members/${m.userId}`))}>${icon(m.userId === me ? "LogOut" : "UserMinus", 13)}</button>`
          : t(m.role === "admin" ? "orgs.role.admin" : "orgs.role.member")}</td></tr>`)}</table>
      ${admin ? html`<form class="inline" @submit=${this.addMember}>
        <input name="email" type="email" required placeholder=${t("login.email")} aria-label=${t("login.email")}>
        <select name="role"><option value="member">${t("orgs.role.member")}</option><option value="admin">${t("orgs.role.admin")}</option></select>
        <button>${t("orgs.add_member")}</button></form>` : ""}

      <h2>${t("orgs.groups")}</h2>
      <p class="muted" style="margin-top:0">${t("orgs.groups_intro")}</p>
      ${o.groups.map((g) => this.group(g, admin))}
      ${admin ? html`<form class="inline" @submit=${this.addGroup}>
        <input name="name" required maxlength="100" placeholder=${t("orgs.group_name")} aria-label=${t("orgs.group_name")}>
        <input name="oidc" maxlength="200" placeholder=${t("orgs.oidc_optional")} aria-label=${t("orgs.oidc_optional")}>
        <button>${icon("Plus", 13)} ${t("orgs.add_group")}</button></form>` : ""}
    </div>`;
  }
}
customElements.define("phx-org", PhxOrg);
