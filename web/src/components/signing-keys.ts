import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { createSigningKey, deleteSigningKey, localKeys, signingSupported, type ServerKey } from "../signing";
import { base } from "./shared";
import { toast } from "./toasts";

/** Keys for signing agent jobs (ADR-0036), in the security settings. */
export class PhxSigningKeys extends LitElement {
  static properties = { keys: { state: true }, here: { state: true }, supported: { state: true }, adding: { state: true }, busy: { state: true }, copied: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    ul { list-style: none; padding: 0; margin: 8px 0; display: grid; gap: 8px; max-width: 760px; }
    li { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: grid; gap: 6px; background: var(--surface); }
    .top { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .top b { margin-right: auto; }
    code { font-size: 12px; word-break: break-all; }
    .here { font-size: 12px; color: var(--good); }
    .sub { font-size: 13px; color: var(--muted); }
    pre { margin: 0; font-size: 12px; white-space: pre-wrap; word-break: break-all; }
    form { display: flex; gap: 8px; align-items: end; flex-wrap: wrap; }
  `];
  keys: ServerKey[] = [];
  here = new Set<string>();
  supported = true;
  adding = false;
  busy = false;
  copied = "";

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.supported = await signingSupported();
    this.keys = (await api<ServerKey[]>("GET", "/me/signing-keys").catch(() => [])) ?? [];
    this.here = new Set((await localKeys()).map((k) => k.id));
  }

  private async add(e: SubmitEvent) {
    e.preventDefault();
    const name = String(new FormData(e.target as HTMLFormElement).get("name") ?? "").trim();
    this.busy = true;
    try {
      await createSigningKey(name);
      this.adding = false;
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async deleteKey(k: ServerKey) {
    if (!confirm(t("signing.delete_confirm", { name: k.name }))) return;
    await deleteSigningKey(k.id);
    await this.load();
  }

  private async copy(k: ServerKey) {
    await navigator.clipboard.writeText(k.publicKey).catch(() => undefined);
    this.copied = k.id;
  }

  render() {
    return html`<p class="muted">${t("signing.intro")}</p>
      ${this.keys.length ? html`<ul>${this.keys.map((k) => html`<li>
        <div class="top"><b>${k.name}</b>${this.here.has(k.id) ? html`<span class="here">${icon("Check", 12)} ${t("signing.this_device")}</span>` : ""}
          <button class="ghost" @click=${() => this.copy(k)}>${icon(this.copied === k.id ? "Check" : "Copy", 13)} ${t("signing.copy")}</button>
          <button class="danger" @click=${() => this.deleteKey(k)}>${icon("Trash2", 13)}</button></div>
        <code>${k.publicKey}</code>
        <span class="sub">${t("signing.created", { date: formatDate(k.createdAt) })}</span></li>`)}</ul>
        <p class="sub">${t("signing.agent_hint")}</p>
        <pre>adminKeys:\n${this.keys.map((k) => `  - ${k.publicKey}   # ${k.name}`).join("\n")}</pre>` : ""}
      ${!this.supported ? html`<p class="muted">${t("signing.unsupported")}</p>`
        : this.adding ? html`<form @submit=${this.add}><label class="field">${t("signing.name")}<input name="name" required maxlength="80" autocomplete="off" placeholder=${t("signing.name_placeholder")}></label>
            <button ?disabled=${this.busy}>${t("signing.create")}</button><button type="button" class="ghost" @click=${() => (this.adding = false)}>${t("doc.cancel")}</button></form>`
        : html`<button class="ghost" @click=${() => (this.adding = true)}>${icon("KeyRound", 14)} ${t("signing.create")}</button>`}`;
  }
}
customElements.define("phx-signing-keys", PhxSigningKeys);
