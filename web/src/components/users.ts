import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api, type LinkResult, type User } from "../api";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

/** Instance users for system admins: invite, links, admin flag, disable. */
export class PhxUsers extends SignalWatcher(LitElement) {
  static properties = { users: { state: true }, result: { state: true }, error: { state: true }, confirming: { state: true }, busy: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 900px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    h2 { font-size: 17px; margin: 0 0 12px; }
    .invite { display: grid; grid-template-columns: 1.3fr 1fr 140px; gap: 10px; align-items: end; }
    .invite .check { grid-column: 1 / -1; display: flex; gap: 8px; align-items: center; font-size: 14px; }
    .invite .check input { width: auto; }
    @media (max-width: 700px) { .invite { grid-template-columns: 1fr; } }
    /* A link appears once per invite: a short reveal points the eye at it. */
    .result { margin-top: 14px; display: grid; gap: 8px; border-color: var(--accent);
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(-4px); } }
    @media (prefers-reduced-motion: reduce) { .result { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
    .result p { margin: 0; }
    code { font-family: var(--font-mono); font-size: 13px; word-break: break-all; background: var(--surface2); padding: 8px 10px; border-radius: 6px; user-select: all; }
    ul { list-style: none; padding: 0; margin: 22px 0 0; display: flex; flex-direction: column; }
    li { display: flex; gap: 12px; justify-content: space-between; align-items: center; flex-wrap: wrap; padding: 12px 0; border-bottom: 1px solid var(--line); }
    li.off .who { opacity: .6; }
    .who b { font-weight: 600; }
    .who .sub { font-size: 13px; color: var(--muted); }
    .acts { display: flex; gap: 6px; flex-wrap: wrap; }
    .acts button { font-size: 13px; padding: 5px 10px; }
    .chip.admin { color: var(--accent); }
  `];
  users: User[] = [];
  result?: LinkResult;
  error = "";
  confirming = "";
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    try {
      this.users = await api<User[]>("GET", "/admin/users");
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  private fail(err: unknown) {
    toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
  }

  private async invite(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.busy = true;
    this.error = "";
    try {
      this.result = await api<LinkResult>("POST", "/admin/users", { email: f.get("email"), name: f.get("name"), admin: f.get("admin") === "on", locale: locale() });
      form.reset();
      await this.load();
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async patch(u: User, body: Record<string, unknown>) {
    this.confirming = "";
    try {
      await api<User>("PATCH", `/admin/users/${u.id}`, body);
      toast(t("users.updated"));
      await this.load();
    } catch (err) {
      this.fail(err);
    }
  }

  private async link(u: User) {
    try {
      this.result = await api<LinkResult>("POST", `/admin/users/${u.id}/link`);
      this.updateComplete.then(() => this.renderRoot.querySelector(".result")?.scrollIntoView({ block: "nearest" }));
    } catch (err) {
      this.fail(err);
    }
  }

  private async copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast(t("tokens.copied"));
    } catch {
      /* the code block is selectable as a fallback */
    }
  }

  private row(u: User) {
    const me = currentUser.get()?.id === u.id;
    const confirm = this.confirming === u.id;
    return html`<li class=${u.disabled ? "off" : ""}>
      <div class="who">
        <b>${u.name}</b> ${me ? html`<span class="chip">${t("users.you")}</span>` : ""}
        ${u.isAdmin ? html`<span class="chip admin">${t("users.badge_admin")}</span>` : ""}
        ${!u.hasPassword ? html`<span class="chip">${t("users.badge_invited")}</span>` : ""}
        ${u.disabled ? html`<span class="chip status-rejected">${t("users.badge_disabled")}</span>` : ""}
        <div class="sub">${u.email} · ${u.lastLoginAt ? t("users.last_login", { date: formatDate(u.lastLoginAt) }) : t("users.never_logged_in")}</div>
      </div>
      ${me ? "" : html`<div class="acts">
        ${confirm
          ? html`<span class="muted" style="font-size:13px">${t("users.disable_confirm", { name: u.name })}</span>
              <button class="danger" @click=${() => this.patch(u, { disabled: true })}>${t("users.disable")}</button>
              <button class="ghost" @click=${() => (this.confirming = "")}>${t("doc.cancel")}</button>`
          : html`
              ${u.disabled ? "" : html`<button class="ghost" @click=${() => this.link(u)}>${icon("Link", 13)} ${t(u.hasPassword ? "users.new_reset" : "users.new_invite")}</button>`}
              <button class="ghost" @click=${() => this.patch(u, { admin: !u.isAdmin })}>${t(u.isAdmin ? "users.remove_admin" : "users.make_admin")}</button>
              ${u.disabled
                ? html`<button class="ghost" @click=${() => this.patch(u, { disabled: false })}>${t("users.enable")}</button>`
                : html`<button class="danger" @click=${() => (this.confirming = u.id)}>${t("users.disable")}</button>`}`}
      </div>`}
    </li>`;
  }

  render() {
    const r = this.result;
    return html`<div class="enter">
      <h1>${t("users.title")}</h1>
      ${settingsNav("users")}
      <p class="muted" style="margin-top:0">${t("users.intro")}</p>
      <form class="card" @submit=${this.invite}>
        <h2>${t("users.invite_title")}</h2>
        <div class="invite">
          <label class="field">${t("login.email")}<input name="email" type="email" required autocomplete="off" /></label>
          <label class="field">${t("profile.name")}<input name="name" autocomplete="off" /></label>
          <button type="submit" ?disabled=${this.busy}>${icon("UserPlus", 15)} ${t("users.invite")}</button>
          <label class="check"><input type="checkbox" name="admin" /> ${t("users.admin")}</label>
        </div>
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      </form>
      ${r ? html`<div class="card result" role="status">
        <p>${t(r.mailed ? "users.mailed" : "users.not_mailed", { email: r.user.email })}</p>
        <code>${r.link}</code>
        <div class="row"><button class="ghost" @click=${() => this.copy(r.link)}>${icon("Copy", 14)} ${t("tokens.copy")}</button>
          <span class="muted" style="font-size:13px">${t("users.valid_until", { date: formatDate(r.expiresAt) })}</span></div>
      </div>` : ""}
      <ul>${this.users.map((u) => this.row(u))}</ul>
    </div>`;
  }
}
customElements.define("phx-users", PhxUsers);
