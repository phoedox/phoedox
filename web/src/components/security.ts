import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";
import { cancelled, creationOptions, credentialJSON, passkeysSupported } from "../webauthn";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import "./signing-keys";
import { toast } from "./toasts";

interface Passkey { id: string; name: string; createdAt: string; lastUsedAt: string | null }
interface Security { totp: boolean; recoveryCodes: number; passkeys: Passkey[] }
interface Setup { secret: string; uri: string; qr: string }
type Confirming = "" | "disable" | "codes";

/** Two-factor authentication and passkeys of the signed-in user. */
export class PhxSecurity extends SignalWatcher(LitElement) {
  static properties = {
    sec: { state: true }, setup: { state: true }, codes: { state: true }, confirming: { state: true },
    adding: { state: true }, renaming: { state: true }, error: { state: true }, busy: { state: true },
  };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    section { margin-bottom: 30px; }
    h2 { font-size: 17px; margin: 0 0 6px; display: flex; align-items: center; gap: 8px; }
    p { margin: 0 0 12px; line-height: 1.5; }
    .on { color: var(--good); font-size: 13px; font-weight: 600; }
    .setup { display: grid; grid-template-columns: 180px 1fr; gap: 18px; align-items: start; }
    .setup img { width: 180px; height: 180px; border-radius: 8px; background: #fff; }
    @media (max-width: 600px) { .setup { grid-template-columns: 1fr; } }
    .key { font-family: var(--font-mono); font-size: 14px; letter-spacing: .05em; word-break: break-all; background: var(--surface2); padding: 8px 10px; border-radius: 6px; user-select: all; }
    form.inline { display: flex; gap: 8px; align-items: end; flex-wrap: wrap; margin-top: 10px; }
    form.inline label { flex: 1; min-width: 200px; }
    input.code { font-family: var(--font-mono); letter-spacing: .15em; }
    /* Shown once: a short reveal makes sure nobody misses the codes. */
    .codes { border-color: var(--accent); margin-top: 12px;
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(-4px); } }
    @media (prefers-reduced-motion: reduce) { .codes { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
    .codes ol { columns: 2; font-family: var(--font-mono); font-size: 15px; padding-left: 22px; margin: 10px 0 14px; }
    .codes li { padding: 3px 0; }
    ul.pk { list-style: none; padding: 0; margin: 10px 0; display: flex; flex-direction: column; }
    ul.pk li { display: flex; justify-content: space-between; gap: 10px; align-items: center; flex-wrap: wrap; padding: 10px 0; border-bottom: 1px solid var(--line); }
    ul.pk .sub { font-size: 13px; color: var(--muted); }
    .acts { display: flex; gap: 6px; } .acts button { font-size: 13px; padding: 5px 10px; }
  `];
  sec?: Security;
  setup?: Setup;
  codes: string[] = [];
  confirming: Confirming = "";
  adding = false;
  renaming = "";
  error = "";
  busy = false;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    this.sec = await api<Security>("GET", "/me/security").catch(() => undefined);
  }

  private async run(fn: () => Promise<void>) {
    this.busy = true;
    this.error = "";
    try {
      await fn();
    } catch (err) {
      if (!cancelled(err)) this.error = err instanceof ApiError ? err.text : t("error.unknown");
      else toast(t("security.passkey_cancelled"));
    } finally {
      this.busy = false;
    }
  }

  private startSetup = () => this.run(async () => {
    this.setup = await api<Setup>("POST", "/me/totp");
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLInputElement>("input.code")?.focus();
  });

  private confirmSetup = (e: SubmitEvent) => {
    e.preventDefault();
    const code = new FormData(e.target as HTMLFormElement).get("code");
    return this.run(async () => {
      const res = await api<{ recoveryCodes: string[] }>("POST", "/me/totp/confirm", { code });
      this.setup = undefined;
      this.codes = res.recoveryCodes;
      toast(t("security.enabled"));
      await this.load();
    });
  };

  private confirmAction = (e: SubmitEvent) => {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const body = { password: String(f.get("password") ?? ""), code: String(f.get("code") ?? "") };
    return this.run(async () => {
      if (this.confirming === "disable") {
        await api("POST", "/me/totp/disable", body);
        toast(t("security.disabled"));
      } else {
        this.codes = (await api<{ recoveryCodes: string[] }>("POST", "/me/recovery-codes", body)).recoveryCodes;
      }
      this.confirming = "";
      await this.load();
    });
  };

  private download() {
    const u = currentUser.get();
    const text = `Phoedox – ${t("security.recovery_title")}\n${u?.email ?? ""} · ${location.host}\n\n${this.codes.join("\n")}\n`;
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    a.download = `phoedox-recovery-codes-${location.host}.txt`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  private addPasskey = (e: SubmitEvent) => {
    e.preventDefault();
    const name = String(new FormData(e.target as HTMLFormElement).get("name") ?? "");
    return this.run(async () => {
      const { options, token } = await api<{ options: any; token: string }>("POST", "/me/passkeys/begin");
      const cred = (await navigator.credentials.create(creationOptions(options))) as PublicKeyCredential;
      await api("POST", `/me/passkeys/finish?token=${encodeURIComponent(token)}&name=${encodeURIComponent(name)}`, credentialJSON(cred));
      this.adding = false;
      toast(t("security.passkey_added"));
      await this.load();
    });
  };

  private rename = (e: SubmitEvent) => {
    e.preventDefault();
    const name = new FormData(e.target as HTMLFormElement).get("name");
    const id = this.renaming;
    return this.run(async () => {
      await api("PATCH", `/me/passkeys/${encodeURIComponent(id)}`, { name });
      this.renaming = "";
      await this.load();
    });
  };

  private removePasskey(id: string) {
    return this.run(async () => {
      await api("DELETE", `/me/passkeys/${encodeURIComponent(id)}`);
      await this.load();
    });
  }

  private confirmForm() {
    const hasPassword = currentUser.get()?.hasPassword;
    return html`<form class="inline" @submit=${this.confirmAction}>
      ${hasPassword
        ? html`<label class="field">${t("security.confirm_password")}<input name="password" type="password" autocomplete="current-password" required /></label>`
        : html`<label class="field">${t("security.confirm_code")}<input class="code" name="code" inputmode="numeric" autocomplete="one-time-code" required /></label>`}
      <button type="submit" class=${this.confirming === "disable" ? "danger" : ""} ?disabled=${this.busy}>${t("security.confirm")}</button>
      <button type="button" class="ghost" @click=${() => (this.confirming = "")}>${t("doc.cancel")}</button>
    </form>`;
  }

  private totpSection(s: Security) {
    const setup = this.setup;
    return html`<section>
      <h2>${t("security.totp_title")} ${s.totp ? html`<span class="on">${icon("Check", 14)} ${t("security.enabled")}</span>` : ""}</h2>
      <p class="muted">${t(s.totp ? "security.totp_on" : "security.totp_off")}</p>
      ${s.totp ? html`
        <p>${t("security.recovery_left", { count: s.recoveryCodes })}</p>
        ${this.confirming ? this.confirmForm() : html`<div class="row">
          <button class="ghost" @click=${() => (this.confirming = "codes")}>${t("security.recovery_new")}</button>
          <button class="danger" @click=${() => (this.confirming = "disable")}>${t("security.totp_disable")}</button></div>`}`
      : setup ? html`<div class="setup card">
          <img src=${"data:image/svg+xml;base64," + btoa(setup.qr)} alt=${t("security.qr_alt")} />
          <div>
            <p>${t("security.totp_scan")}</p>
            <div class="key">${setup.secret.replace(/(.{4})/g, "$1 ").trim()}</div>
            <form class="inline" @submit=${this.confirmSetup}>
              <label class="field">${t("security.totp_code")}<input class="code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9 ]{6,7}" maxlength="7" required /></label>
              <button type="submit" ?disabled=${this.busy}>${t("security.totp_confirm")}</button>
            </form>
          </div>
        </div>`
      : html`<button @click=${this.startSetup} ?disabled=${this.busy}>${t("security.totp_setup")}</button>`}
      ${this.codes.length ? html`<div class="card codes" role="status">
        <h2>${t("security.recovery_title")}</h2>
        <p class="muted">${t("security.recovery_intro")}</p>
        <ol>${this.codes.map((c) => html`<li>${c}</li>`)}</ol>
        <div class="row">
          <button class="ghost" @click=${this.download}>${t("security.recovery_download")}</button>
          <button @click=${() => (this.codes = [])}>${t("security.recovery_done")}</button>
        </div>
      </div>` : ""}
    </section>`;
  }

  private passkeySection(s: Security) {
    return html`<section>
      <h2>${t("security.passkeys_title")}</h2>
      <p class="muted">${t("security.passkeys_intro")}</p>
      ${s.passkeys.length === 0 ? html`<p class="muted">${t("security.passkey_none")}</p>` : html`<ul class="pk">
        ${s.passkeys.map((p) => html`<li>
          ${this.renaming === p.id
            ? html`<form class="inline" style="margin:0;flex:1" @submit=${this.rename}>
                <label class="field">${t("profile.name")}<input name="name" .value=${p.name} required /></label>
                <button type="submit">${t("profile.save")}</button>
                <button type="button" class="ghost" @click=${() => (this.renaming = "")}>${t("doc.cancel")}</button></form>`
            : html`<div><b>${p.name}</b><div class="sub">${t("security.passkey_created", { date: formatDate(p.createdAt) })}${p.lastUsedAt ? html` · ${t("security.passkey_used", { date: formatDate(p.lastUsedAt) })}` : ""}</div></div>
              <div class="acts">
                <button class="ghost" @click=${() => (this.renaming = p.id)}>${t("security.passkey_rename")}</button>
                <button class="danger" @click=${() => this.removePasskey(p.id)}>${t("security.passkey_delete")}</button>
              </div>`}
        </li>`)}</ul>`}
      ${!passkeysSupported() ? html`<p class="muted">${t("security.passkey_unsupported")}</p>`
        : this.adding ? html`<form class="inline" @submit=${this.addPasskey}>
            <label class="field">${t("security.passkey_name")}<input name="name" required autocomplete="off" /></label>
            <button type="submit" ?disabled=${this.busy}>${t("security.passkey_add")}</button>
            <button type="button" class="ghost" @click=${() => (this.adding = false)}>${t("doc.cancel")}</button></form>`
        : html`<button class="ghost" @click=${() => (this.adding = true)}>${icon("Plus", 15)} ${t("security.passkey_add")}</button>`}
    </section>`;
  }

  render() {
    const s = this.sec;
    return html`<div class="enter">
      <h1>${t("security.title")}</h1>
      ${settingsNav("security")}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      ${s ? html`${this.totpSection(s)}${this.passkeySection(s)}
        <section><h2>${t("signing.title")}</h2><phx-signing-keys></phx-signing-keys></section>` : html`<p class="muted">${t("app.loading")}</p>`}
    </div>`;
  }
}
customElements.define("phx-security", PhxSecurity);
