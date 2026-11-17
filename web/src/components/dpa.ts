// Data processing agreement (DPA, German AVV): organization admins accept
// the current text electronically, everyone in the organization sees the
// history and downloads the PDF of an acceptance; system admins publish new
// versions (until then a placeholder applies).

import { LitElement, css, html, nothing } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { ApiError, api } from "../api";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { renderMarkdown } from "../md";
import { pageHead, scard, settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface DPAText { id: string; version: string; placeholder: boolean; publishedAt: string; lang: string; body: string }
interface Acceptance { id: string; versionId: string; version: string; lang: string; signerName: string; signerRole: string; company: string; acceptedAt: string; textSha256: string }
interface DPAInfo { current: DPAText; accepted?: Acceptance; history: Acceptance[]; canAccept: boolean; company: string }
interface DPAVersion { id: string; version: string; bodyEn: string; bodyDe: string; publishedAt: string }
interface DPAAdmin { versions: DPAVersion[]; placeholder: boolean; placeholderEn: string; placeholderDe: string }

const text = css`
  .doc { max-height: 360px; overflow: auto; border: 1px solid var(--line); border-radius: 10px; padding: 4px 18px; background: var(--surface); font-size: 14px; line-height: 1.55; }
  .doc h1 { font-size: 18px; } .doc h2 { font-size: 15px; }
  .muted { color: var(--muted); font-size: 13px; }
  .row { display: flex; gap: 10px; flex-wrap: wrap; align-items: end; }
  .state { display: flex; gap: 8px; align-items: center; padding: 10px 12px; border-radius: 10px; background: var(--surface2); font-size: 14px; }
  .state.ok { background: color-mix(in srgb, var(--good) 12%, var(--surface)); }
  .state.todo { background: color-mix(in srgb, var(--signal) 14%, var(--surface)); }
  table { width: 100%; border-collapse: collapse; font-size: 13.5px; }
  th, td { text-align: left; padding: 7px 8px; border-bottom: 1px solid var(--line); vertical-align: top; }
  th { color: var(--muted); font-weight: 600; font-size: 12.5px; }
  .wrap { overflow-x: auto; }
  .chip { font-size: 12px; padding: 1px 8px; border-radius: 99px; background: var(--surface2); }
`;

/** The agreement in an organization's settings. */
export class PhxOrgDpa extends LitElement {
  static properties = { org: {}, info: { state: true }, busy: { state: true }, lang: { state: true } };
  static styles = [base, text, css`:host { display: grid; gap: 12px; } form { display: grid; gap: 10px; }`];
  org = "";
  info?: DPAInfo;
  busy = false;
  lang = locale();

  willUpdate(ch: Map<string, unknown>) {
    if ((ch.has("org") || ch.has("lang")) && this.org) void this.load();
  }

  private async load() {
    this.info = await api<DPAInfo>("GET", `/orgs/${this.org}/dpa?lang=${this.lang}`).catch(() => undefined);
  }

  private async accept(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    this.busy = true;
    try {
      await api("POST", `/orgs/${this.org}/dpa/accept`, {
        versionId: this.info!.current.id, lang: this.info!.current.lang, signerName: String(f.get("name") ?? ""), signerRole: String(f.get("role") ?? ""),
        company: String(f.get("company") ?? ""), confirm: f.get("confirm") === "on",
      });
      toast(t("dpa.accepted_toast"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
      await this.load();
    } finally {
      this.busy = false;
    }
  }

  private pdf(a: Acceptance) {
    return `/api/v1/orgs/${encodeURIComponent(this.org)}/dpa/acceptances/${a.id}/pdf`;
  }

  render() {
    const i = this.info;
    if (!i) return nothing;
    const c = i.current;
    return html`<p class="muted" style="margin:0">${t("dpa.intro")}</p>
      ${i.accepted
        ? html`<div class="state ok" role="status">${icon("BadgeCheck", 16)} <span>${t("dpa.accepted_by", { name: i.accepted.signerName, date: formatDate(i.accepted.acceptedAt), version: i.accepted.version })}</span>
            <a class="btn ghost" href=${this.pdf(i.accepted)} download>${icon("Download", 14)} PDF</a></div>`
        : html`<div class="state todo" role="status">${icon("FileSignature", 16)} ${t(i.history.length ? "dpa.new_version" : "dpa.not_accepted")}</div>`}
      <div class="row"><b>${t("dpa.version", { version: c.version })}</b>${c.placeholder ? html`<span class="chip">${t("dpa.placeholder")}</span>` : html`<span class="muted">${formatDate(c.publishedAt)}</span>`}
        <span style="flex:1"></span>
        <label class="muted">${t("dpa.language")} <select @change=${(e: Event) => (this.lang = (e.target as HTMLSelectElement).value as "de" | "en")}>
          <option value="en" ?selected=${this.lang === "en"}>English</option><option value="de" ?selected=${this.lang === "de"}>Deutsch</option></select></label></div>
      <div class="doc" tabindex="0" aria-label=${t("dpa.title")}>${unsafeHTML(renderMarkdown(c.body, ""))}</div>
      ${!i.accepted && i.canAccept ? html`<form @submit=${this.accept}>
          <div class="row">
            <label class="field" style="flex:1 1 200px">${t("dpa.signer")}<input name="name" required maxlength="120" autocomplete="name"></label>
            <label class="field" style="flex:1 1 200px">${t("dpa.role")}<input name="role" maxlength="120" autocomplete="organization-title"></label>
            <label class="field" style="flex:1 1 220px">${t("dpa.company")}<input name="company" maxlength="200" .value=${i.company} autocomplete="organization"></label>
          </div>
          <label class="inline-check"><input type="checkbox" name="confirm" required> ${t("dpa.confirm", { org: i.company })}</label>
          <div><button ?disabled=${this.busy}>${icon("FileSignature", 14)} ${t("dpa.accept")}</button></div>
        </form>` : nothing}
      ${!i.accepted && !i.canAccept ? html`<p class="muted">${t("dpa.admins_only")}</p>` : nothing}
      ${i.history.length ? html`<h3 style="margin:8px 0 0;font-size:14px">${t("dpa.history")}</h3><div class="wrap"><table>
        <tr><th>${t("dpa.col.date")}</th><th>${t("dpa.col.version")}</th><th>${t("dpa.col.signer")}</th><th></th></tr>
        ${i.history.map((a) => html`<tr><td>${formatDate(a.acceptedAt)}</td><td>${a.version} <span class="muted">(${a.lang})</span></td>
          <td>${a.signerName}${a.signerRole ? html`<br><span class="muted">${a.signerRole}</span>` : nothing}</td>
          <td><a href=${this.pdf(a)} download>${icon("Download", 13)} PDF</a></td></tr>`)}</table></div>` : nothing}`;
  }
}
customElements.get("phx-org-dpa") || customElements.define("phx-org-dpa", PhxOrgDpa);

/** System admins: published versions and a form for the next one. */
export class PhxDpaAdmin extends LitElement {
  static properties = { data: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, text, css`
    :host { display: block; }
    .body { padding: 0 18px 16px; display: grid; gap: 10px; }
    textarea { min-height: 220px; font-family: var(--font-mono); font-size: 13px; width: 100%; box-sizing: border-box; }
  `];
  data?: DPAAdmin;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.data = await api<DPAAdmin>("GET", "/admin/dpa").catch(() => undefined);
  }

  private async publish(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    if (!confirm(t("dpa.publish_confirm"))) return;
    this.busy = true;
    try {
      await api("POST", "/admin/dpa", { version: f.get("version"), bodyEn: f.get("bodyEn"), bodyDe: f.get("bodyDe") });
      form.reset();
      toast(t("dpa.published"));
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const d = this.data;
    if (!d) return nothing;
    const latest = d.versions[0];
    return html`${pageHead(t("dpa.title"), t("dpa.admin_intro"))}
      ${scard(t("dpa.current"), undefined, html`<div class="body">
        ${d.placeholder ? html`<div class="state todo">${icon("FileSignature", 16)} ${t("dpa.placeholder_active")}</div>`
          : html`<div class="state ok">${icon("BadgeCheck", 16)} ${t("dpa.version", { version: latest.version })} · ${formatDate(latest.publishedAt)}</div>`}
        <div class="doc">${unsafeHTML(renderMarkdown(d.placeholder ? (locale() === "de" ? d.placeholderDe : d.placeholderEn) : (locale() === "de" && latest.bodyDe ? latest.bodyDe : latest.bodyEn), ""))}</div>
      </div>`)}
      ${scard(t("dpa.publish"), t("dpa.publish_help"), html`<form class="body" @submit=${this.publish}>
        <label class="field">${t("dpa.version_name")}<input name="version" required maxlength="40" placeholder="2026-11" style="max-width:200px"></label>
        <label class="field">${t("dpa.body_en")}<textarea name="bodyEn" required .value=${latest?.bodyEn ?? ""}></textarea></label>
        <label class="field">${t("dpa.body_de")}<textarea name="bodyDe" .value=${latest?.bodyDe ?? ""}></textarea></label>
        <div><button ?disabled=${this.busy}>${icon("FileSignature", 14)} ${t("dpa.publish")}</button></div>
      </form>`)}
      ${d.versions.length ? scard(t("dpa.versions"), undefined, html`<div class="body"><table>
        <tr><th>${t("dpa.col.version")}</th><th>${t("dpa.col.date")}</th></tr>
        ${d.versions.map((v) => html`<tr><td>${v.version}</td><td>${formatDate(v.publishedAt)}</td></tr>`)}</table></div>`) : nothing}`;
  }
}
customElements.get("phx-dpa-admin") || customElements.define("phx-dpa-admin", PhxDpaAdmin);
