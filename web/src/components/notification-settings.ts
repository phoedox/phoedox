import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentSubscription, disablePush, enablePush, pushSupported, type NotificationSettings } from "../notifications";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

/** Personal channels: mail mode, ntfy, web push and muted kinds. */
export class PhxNotificationSettings extends LitElement {
  static properties = { s: { state: true }, pushOn: { state: true }, busy: { state: true }, error: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    h1 { margin-bottom: 16px; }
    h2 { font-size: 17px; margin: 28px 0 8px; }
    form { display: grid; gap: 14px; max-width: 560px; }
    fieldset { border: 0; padding: 0; margin: 0; display: grid; gap: 6px; }
    legend { font-weight: 600; margin-bottom: 4px; }
    fieldset label { display: flex; gap: 8px; align-items: baseline; }
    fieldset input { width: auto; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  `];
  s?: NotificationSettings;
  pushOn = false;
  busy = false;
  error = "";

  async connectedCallback() {
    super.connectedCallback();
    this.s = await api<NotificationSettings>("GET", "/me/notification-settings").catch(() => undefined);
    const sub = await currentSubscription().catch(() => null);
    this.pushOn = !!sub && !!this.s?.subscriptions.includes(sub.endpoint);
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.error = "";
    try {
      this.s = await api<NotificationSettings>("PUT", "/me/notification-settings", {
        email: f.get("email"), ntfyUrl: String(f.get("ntfy") ?? "").trim(),
        matrixHomeserver: String(f.get("mhs") ?? "").trim(), matrixRoom: String(f.get("mroom") ?? "").trim(),
        // The token is write-only: empty keeps the stored one, the remove button sends "".
        ...(String(f.get("mtoken") ?? "").trim() ? { matrixToken: String(f.get("mtoken")).trim() } : {}),
        muted: this.s!.kinds.filter((k) => !f.getAll("kind").includes(k)),
      });
      toast(t("notif.saved"));
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  }

  private removeMatrix = async () => {
    try {
      this.s = await api<NotificationSettings>("PUT", "/me/notification-settings", { ...this.s, matrixHomeserver: "", matrixRoom: "", matrixToken: "" });
      toast(t("notif.saved"));
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    }
  };

  private async togglePush() {
    this.busy = true;
    try {
      if (this.pushOn) {
        await disablePush();
        this.pushOn = false;
      } else {
        await enablePush(this.s!.pushKey!);
        this.pushOn = true;
      }
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("notif.push_denied"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async test() {
    try {
      await api("POST", "/me/notifications/test");
      toast(t("notif.test_sent"));
      document.querySelector("phx-app")?.shadowRoot?.querySelector<any>("phx-notification-bell")?.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    const s = this.s;
    return html`<div class="enter">
      <h1>${t("notif.title")}</h1>
      ${settingsNav("notifications")}
      ${!s ? html`<p class="muted">${t("app.loading")}</p>` : html`
      <form @submit=${this.save}>
        <fieldset>
          <legend>${t("notif.email")}</legend>
          ${!s.mailEnabled ? html`<p class="muted" style="margin:0">${t("notif.mail_off_instance")}</p>` : ""}
          ${(["instant", "daily", "off"] as const).map((m) => html`<label><input type="radio" name="email" value=${m} ?checked=${s.email === m}> ${t(`notif.email.${m}`)}</label>`)}
        </fieldset>
        <fieldset>
          <legend>${t("notif.kinds")}</legend>
          <p class="muted" style="margin:0">${t("notif.kinds_hint")}</p>
          ${s.kinds.map((k) => html`<label><input type="checkbox" name="kind" value=${k} ?checked=${!s.muted.includes(k)}> ${t(`notif.kind.${k}` as any)}</label>`)}
        </fieldset>
        <label class="field">${t("notif.ntfy")}
          <input name="ntfy" type="url" inputmode="url" placeholder="https://ntfy.sh/mein-thema" .value=${s.ntfyUrl} />
          <span class="muted" style="font-size:12.5px">${t("notif.ntfy_hint")}</span></label>
        <fieldset>
          <legend>Matrix</legend>
          <p class="muted" style="margin:0">${t("notif.matrix_hint")}</p>
          <label class="field">${t("notif.matrix_homeserver")}<input name="mhs" type="url" inputmode="url" placeholder="https://matrix.org" .value=${s.matrixHomeserver ?? ""} /></label>
          <label class="field">${t("notif.matrix_room")}<input name="mroom" placeholder="!abcdef:matrix.org" .value=${s.matrixRoom ?? ""} /></label>
          <label class="field">${t("notif.matrix_token")}<input name="mtoken" type="password" autocomplete="off" placeholder=${s.matrixConnected ? t("notif.matrix_token_kept") : ""} /></label>
          ${s.matrixConnected ? html`<div><button type="button" class="ghost" @click=${this.removeMatrix}>${t("notif.matrix_remove")}</button></div>` : ""}
        </fieldset>
        ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
        <div class="row"><button type="submit">${t("profile.save")}</button>
          <button type="button" class="ghost" @click=${this.test}>${icon("Send", 14)} ${t("notif.test")}</button></div>
      </form>
      <h2>${t("notif.push")}</h2>
      <p class="muted">${t("notif.push_hint")}</p>
      ${!pushSupported() ? html`<p class="muted">${t("notif.push_unsupported")}</p>`
        : !s.pushKey ? html`<p class="muted">${t("notif.push_unavailable")}</p>`
        : html`<div class="row"><button class=${this.pushOn ? "ghost" : ""} ?disabled=${this.busy} @click=${this.togglePush}>
            ${icon(this.pushOn ? "BellOff" : "BellRing", 14)} ${t(this.pushOn ? "notif.push_off" : "notif.push_on")}</button>
            ${s.subscriptions.length ? html`<span class="muted">${t("notif.push_devices", { count: s.subscriptions.length })}</span>` : ""}</div>`}
      `}
    </div>`;
  }
}
customElements.define("phx-notification-settings", PhxNotificationSettings);
