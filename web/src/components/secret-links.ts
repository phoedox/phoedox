import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { openSecret, sealSecret } from "../secret-share";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

const styles = css`
  :host { display: block; max-width: 640px; margin: 0 auto; padding: 32px 16px 60px; }
  form { display: grid; gap: 12px; }
  textarea { min-height: 140px; font-family: var(--font-mono); }
  .link { display: flex; gap: 8px; align-items: center; }
  .link input { flex: 1; font-family: var(--font-mono); font-size: 13px; }
  .secret { white-space: pre-wrap; word-break: break-all; font-family: var(--font-mono); background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; }
  .note { font-size: 13px; color: var(--muted); }
`;

/** Creates an end-to-end encrypted one-time link. */
export class PhxSecretNew extends LitElement {
  static properties = { link: { state: true }, expires: { state: true }, busy: { state: true }, error: { state: true } };
  static styles = [base, styles];
  link = "";
  expires = "";
  busy = false;
  error = "";

  connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const text = String(f.get("secret") ?? "");
    const passphrase = String(f.get("passphrase") ?? "");
    if (!text.trim()) return;
    this.busy = true;
    this.error = "";
    try {
      const { ciphertext, fragment } = await sealSecret(text, passphrase);
      const res = await api<{ id: string; expiresAt: string }>("POST", "/secret-links", { ciphertext, passphrase: !!passphrase, ttlHours: Number(f.get("ttl")) });
      this.link = `${location.origin}/secret/${res.id}#${fragment}`;
      this.expires = res.expiresAt;
      form.reset();
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    return html`<div class="enter">
      <h1>${icon("Lock", 22)} ${t("secretlink.title")}</h1>
      <p class="muted">${t("secretlink.intro")}</p>
      ${this.link ? html`<div class="card" style="display:grid;gap:10px">
          <b>${t("secretlink.ready")}</b>
          <div class="link"><input readonly .value=${this.link} aria-label=${t("secretlink.link")} @focus=${(e: Event) => (e.target as HTMLInputElement).select()}>
            <button @click=${() => navigator.clipboard?.writeText(this.link).then(() => toast(t("statuspage.copied")))}>${icon("Copy", 14)} ${t("statuspage.copy")}</button></div>
          <p class="note">${t("secretlink.expires", { date: formatDate(this.expires) })}</p>
          <div><button class="ghost" @click=${() => (this.link = "")}>${t("secretlink.another")}</button></div>
        </div>`
      : html`<form @submit=${this.create}>
          <label class="field">${t("secretlink.secret")}<textarea name="secret" required maxlength="20000" autocomplete="off" spellcheck="false"></textarea></label>
          <label class="field">${t("secretlink.passphrase")}<input name="passphrase" type="password" autocomplete="new-password">
            <span class="note">${t("secretlink.passphrase_hint")}</span></label>
          <label class="field">${t("secretlink.ttl")}<select name="ttl">
            <option value="1">${t("secretlink.ttl_1h")}</option><option value="24" selected>${t("secretlink.ttl_1d")}</option><option value="168">${t("secretlink.ttl_7d")}</option></select></label>
          ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
          <div><button ?disabled=${this.busy}>${icon("Lock", 14)} ${t("secretlink.create")}</button></div>
        </form>`}
    </div>`;
  }
}
customElements.define("phx-secret-new", PhxSecretNew);

/** Opens a one-time link: the ciphertext is fetched (and deleted) only on click. */
export class PhxSecretOpen extends LitElement {
  static properties = { id: {}, state: { state: true }, text: { state: true }, needsPass: { state: true }, error: { state: true } };
  static styles = [base, styles];
  id = "";
  state: "ready" | "pass" | "shown" | "gone" = "ready";
  text = "";
  needsPass = false;
  error = "";
  private ciphertext = "";

  private get fragment() {
    return location.hash.slice(1);
  }

  private async reveal() {
    this.error = "";
    try {
      const r = await api<{ ciphertext: string; passphrase: boolean }>("POST", `/secret-links/${encodeURIComponent(this.id)}/open`);
      this.ciphertext = r.ciphertext;
      // Remove the key from the address bar and history.
      history.replaceState(null, "", location.pathname);
      if (r.passphrase) {
        this.state = "pass";
        return;
      }
      await this.decrypt("");
    } catch (e) {
      this.state = e instanceof ApiError && e.status === 404 ? "gone" : this.state;
      if (this.state !== "gone") this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private key = "";
  private async decrypt(passphrase: string) {
    try {
      this.text = await openSecret(this.ciphertext, this.key, passphrase);
      this.state = "shown";
    } catch {
      this.error = t(passphrase ? "secretlink.wrong_pass" : "secretlink.broken");
    }
  }

  connectedCallback() {
    super.connectedCallback();
    this.key = this.fragment;
  }

  render() {
    return html`<div class="enter">
      <h1>${icon("Lock", 22)} ${t("secretlink.open_title")}</h1>
      ${!this.key && this.state === "ready" ? html`<p class="error">${t("secretlink.no_key")}</p>` : ""}
      ${this.state === "ready" && this.key ? html`<p class="muted">${t("secretlink.open_intro")}</p><button @click=${this.reveal}>${icon("LockOpen", 14)} ${t("secretlink.reveal")}</button>` : ""}
      ${this.state === "pass" ? html`<form @submit=${(e: SubmitEvent) => { e.preventDefault(); void this.decrypt(String(new FormData(e.target as HTMLFormElement).get("pass") ?? "")); }}>
          <p class="muted">${t("secretlink.pass_intro")}</p>
          <label class="field">${t("secretlink.passphrase")}<input name="pass" type="password" required autofocus></label>
          <div><button>${t("secretlink.reveal")}</button></div></form>` : ""}
      ${this.state === "shown" ? html`<div class="secret">${this.text}</div>
        <p class="row" style="margin-top:10px"><button class="ghost" @click=${() => navigator.clipboard?.writeText(this.text).then(() => toast(t("statuspage.copied")))}>${icon("Copy", 14)} ${t("statuspage.copy")}</button></p>
        <p class="note">${t("secretlink.shown_note")}</p>` : ""}
      ${this.state === "gone" ? html`<p class="muted">${t("secretlink.gone")}</p>` : ""}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
    </div>`;
  }
}
customElements.define("phx-secret-open", PhxSecretOpen);
