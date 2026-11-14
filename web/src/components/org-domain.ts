import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface DomainView {
  domain: string;
  verified: boolean;
  recordName: string;
  recordText: string;
  target?: string;
}

/** An organization's own app domain: choose, show DNS records, verify (org admins). */
export class PhxOrgDomain extends LitElement {
  static properties = { org: {}, d: { state: true }, busy: { state: true }, loaded: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 620px; }
    form { display: flex; gap: 8px; flex-wrap: wrap; }
    form input { flex: 1; min-width: 220px; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; margin: 10px 0; }
    th, td { text-align: left; padding: 6px 8px 6px 0; border-bottom: 1px solid var(--line); vertical-align: top; }
    th { color: var(--muted); font-weight: 600; }
    code { word-break: break-all; }
    .state { display: inline-flex; gap: 6px; align-items: center; font-size: 13px; }
    .ok { color: var(--good); }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-top: 8px; }
    p.hint { font-size: 13px; margin: 6px 0; }
  `];
  org = "";
  d: DomainView | null = null;
  busy = false;
  loaded = false;

  async connectedCallback() {
    super.connectedCallback();
    try {
      this.d = (await api<{ domain: DomainView | null }>("GET", `/orgs/${this.org}/domain`)).domain;
    } catch {
      this.d = null;
    }
    this.loaded = true;
  }

  private async call(method: string, path: string, body?: unknown, done?: string) {
    this.busy = true;
    try {
      this.d = (await api<{ domain: DomainView | null }>(method, path, body)).domain;
      if (done) toast(done);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private save(e: SubmitEvent) {
    e.preventDefault();
    const domain = String(new FormData(e.target as HTMLFormElement).get("domain") ?? "").trim();
    this.call("PUT", `/orgs/${this.org}/domain`, { domain }, t("settings.saved"));
  }

  render() {
    if (!this.loaded) return html`<p class="muted">${t("app.loading")}</p>`;
    const d = this.d;
    return html`
      <p class="muted hint">${t("orgdomain.intro")}</p>
      <form @submit=${this.save}>
        <input name="domain" .value=${d?.domain ?? ""} placeholder="docs.example.com" aria-label=${t("orgdomain.domain")} autocomplete="off" spellcheck="false">
        <button ?disabled=${this.busy}>${t("doc.save")}</button>
      </form>
      ${d ? html`
        <div class="row">
          ${d.verified
            ? html`<span class="state ok">${icon("CircleCheck", 15)} ${t("orgdomain.verified")}</span>
                <a href=${`https://${d.domain}/`} rel="external" target="_blank">${icon("ExternalLink", 13)} ${d.domain}</a>`
            : html`<span class="state">${icon("Clock", 15)} ${t("orgdomain.unverified")}</span>`}
        </div>
        ${d.verified ? "" : html`<p class="hint">${t("orgdomain.dns_intro")}</p>`}
        <table>
          <tr><th>${t("orgdomain.type")}</th><th>${t("orgdomain.name")}</th><th>${t("orgdomain.value")}</th></tr>
          ${d.target ? html`<tr><td>CNAME</td><td><code>${d.domain}</code></td><td><code>${d.target}</code></td></tr>` : ""}
          <tr><td>TXT</td><td><code>${d.recordName}</code></td><td><code>${d.recordText}</code></td></tr>
        </table>
        <div class="row">
          ${d.verified ? "" : html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.call("POST", `/orgs/${this.org}/domain/verify`, undefined, t("orgdomain.verified"))}>${icon("RefreshCw", 14)} ${t("orgdomain.check")}</button>`}
          <button class="danger" ?disabled=${this.busy} @click=${() => confirm(t("orgdomain.remove_confirm", { name: d.domain })) && this.call("PUT", `/orgs/${this.org}/domain`, { domain: "" })}>${icon("Trash2", 14)} ${t("orgdomain.remove")}</button>
        </div>
        <p class="muted hint">${t("orgdomain.admin_hint")}</p>
        <p class="muted hint">${t("orgdomain.passkey_hint")}</p>` : ""}
    `;
  }
}
customElements.define("phx-org-domain", PhxOrgDomain);
