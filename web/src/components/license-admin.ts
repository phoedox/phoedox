// Administration → License (ADR-0092): the instance license from the store
// unlocks enterprise features. System admins see its state, upload the
// signed file (or paste it) and remove it.
import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDay, hasKey, t } from "../i18n";
import { icon } from "../icon";
import { pageHead, scard, settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

export interface LicenseInfo {
  present: boolean; active: boolean; inGrace: boolean; expiring: boolean; graceEnd?: string; problem?: string;
  fromFile: boolean; domain?: string; seatsUsed: number;
  id?: string; licensee?: string; domains?: string[]; features?: string[]; seats?: number; issuedAt?: string; expires?: string; graceDays?: number;
}

/** Whether the license needs the admins' attention (overview). */
export function licenseNotice(l: LicenseInfo | undefined): string | undefined {
  if (!l?.present) return undefined;
  if (l.problem) return t("lic.notice.problem", { problem: t(`lic.problem.${l.problem}` as "lic.problem.invalid", { domain: l.domain ?? "" }) });
  if (l.inGrace && l.graceEnd) return t("lic.notice.grace", { date: formatDay(l.graceEnd) });
  if (l.expiring && l.expires) return t("lic.notice.expiring", { date: formatDay(l.expires) });
  if (l.seats && l.seatsUsed > l.seats) return t("lic.notice.seats", { used: l.seatsUsed, seats: l.seats });
  return undefined;
}

const featureLabel = (f: string) => (hasKey(`lic.f.${f}`) ? t(`lic.f.${f}` as "lic.f.*") : f);

export class PhxLicenseAdmin extends LitElement {
  static properties = { data: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .body { padding: 0 18px 16px; display: grid; gap: 12px; }
    .state { display: flex; gap: 8px; align-items: center; padding: 10px 12px; border-radius: 10px; background: var(--surface2); font-size: 14px; }
    .state.ok { background: color-mix(in srgb, var(--good) 12%, var(--surface)); }
    .state.warn { background: color-mix(in srgb, var(--signal) 14%, var(--surface)); }
    .state.bad { background: color-mix(in srgb, var(--bad) 12%, var(--surface)); }
    dl { display: grid; grid-template-columns: max-content 1fr; gap: 6px 16px; margin: 0; font-size: 14px; }
    dt { color: var(--muted); }
    dd { margin: 0; }
    textarea { min-height: 120px; font-family: var(--font-mono); font-size: 12px; width: 100%; box-sizing: border-box; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  `];
  data?: LicenseInfo;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.data = await api<LicenseInfo>("GET", "/admin/license").catch(() => undefined);
  }

  private async upload(text: string) {
    if (!text.trim()) return;
    this.busy = true;
    try {
      this.data = await api<LicenseInfo>("PUT", "/admin/license", JSON.parse(text));
      toast(t("lic.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : err instanceof SyntaxError ? t("validation.license_invalid") : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeLicense() {
    if (!confirm(t("lic.remove_confirm"))) return;
    this.busy = true;
    try {
      this.data = await api<LicenseInfo>("DELETE", "/admin/license");
      toast(t("lic.removed"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private state(l: LicenseInfo) {
    if (!l.present) return html`<div class="state">${icon("FileKey", 16)} ${t("lic.none")}</div>`;
    if (l.problem) return html`<div class="state bad">${icon("TriangleAlert", 16)} ${t(`lic.problem.${l.problem}` as "lic.problem.invalid", { domain: l.domain ?? "" })}</div>`;
    if (l.inGrace) return html`<div class="state warn">${icon("Clock", 16)} ${t("lic.notice.grace", { date: formatDay(l.graceEnd ?? "") })}</div>`;
    if (l.expiring) return html`<div class="state warn">${icon("Clock", 16)} ${t("lic.notice.expiring", { date: formatDay(l.expires ?? "") })}</div>`;
    return html`<div class="state ok">${icon("BadgeCheck", 16)} ${t("lic.active", { licensee: l.licensee ?? "" })}</div>`;
  }

  render() {
    const l = this.data;
    if (!l) return nothing;
    const ends = l.expires && !l.expires.startsWith("0001") ? formatDay(l.expires) : t("lic.perpetual");
    return html`${pageHead(t("lic.title"), t("lic.intro"))}
      ${scard(t("lic.current"), undefined, html`<div class="body">
        ${this.state(l)}
        ${l.present && l.licensee ? html`<dl>
          <dt>${t("lic.licensee")}</dt><dd>${l.licensee}</dd>
          <dt>${t("lic.features")}</dt><dd>${(l.features ?? []).map(featureLabel).join(", ")}</dd>
          <dt>${t("lic.domains")}</dt><dd>${(l.domains ?? []).join(", ")}${l.domain ? html` <span class="muted">· ${t("lic.this_domain", { domain: l.domain })}</span>` : nothing}</dd>
          <dt>${t("lic.valid_until")}</dt><dd>${ends}${l.graceDays ? html` <span class="muted">· ${t("lic.grace_days", { days: l.graceDays })}</span>` : nothing}</dd>
          <dt>${t("lic.seats")}</dt><dd>${l.seats ? t("lic.seats_used", { used: l.seatsUsed, seats: l.seats }) : t("lic.seats_unlimited", { used: l.seatsUsed })}</dd>
          ${l.issuedAt ? html`<dt>${t("lic.issued")}</dt><dd>${formatDay(l.issuedAt)} · <code>${l.id}</code></dd>` : nothing}
        </dl>` : nothing}
        ${l.fromFile ? html`<p class="muted" style="margin:0">${icon("Lock", 13)} ${t("lic.from_file")}</p>`
          : l.present ? html`<div class="row"><button class="danger" ?disabled=${this.busy} @click=${this.removeLicense}>${icon("Trash2", 14)} ${t("lic.remove")}</button></div>` : nothing}
      </div>`)}
      ${l.fromFile ? nothing : scard(t("lic.upload"), t("lic.upload_help"), html`<div class="body">
        <label class="field">${t("lic.upload_file")}
          <input type="file" accept=".json,application/json" ?disabled=${this.busy} @change=${async (e: Event) => {
            const input = e.target as HTMLInputElement;
            const f = input.files?.[0];
            input.value = "";
            if (f) await this.upload(await f.text());
          }}></label>
        <form @submit=${(e: SubmitEvent) => { e.preventDefault(); void this.upload(String(new FormData(e.target as HTMLFormElement).get("text") ?? "")); }} style="display:grid;gap:8px">
          <label class="field">${t("lic.paste")}<textarea name="text" spellcheck="false" placeholder='{"license": {…}, "keyId": "…", "signature": "…"}'></textarea></label>
          <div><button ?disabled=${this.busy}>${icon("FileKey", 14)} ${t("lic.save")}</button></div>
        </form>
      </div>`)}`;
  }
}
customElements.get("phx-license-admin") || customElements.define("phx-license-admin", PhxLicenseAdmin);
