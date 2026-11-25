import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { pageHead, scard, settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Keys { enabled: boolean; siteKey: string; secret?: string; hasSecret: boolean; dpaConfirmed: boolean }
interface Policy { id: string; name: string; preset?: string; provider: string; passAbove: number; blockBelow: number; visible: string; emailFallback: boolean; difficulty?: number }
interface Config { providers: Record<string, Keys>; policies: Policy[]; places: Record<string, string>; signinFailures: number }
interface Row { place: string; policy: string; passed: number; challenged: number; solved: number; email: number; blocked: number; avgSolveMs: number }
interface Ev { place: string; policy: string; outcome: string; reason?: string; ipHash?: string; at: string }
interface Settings { config: Config; stats: { rows: Row[]; blocked: Ev[]; hourly: number[] }; mail: boolean }

const PLACES = ["signup", "signin", "reset", "public_forms", "feedback", "apps"];
const PROVIDERS: [string, string, string][] = [
  ["turnstile", "Cloudflare Turnstile", "https://www.cloudflare.com/privacypolicy/"],
  ["hcaptcha", "hCaptcha", "https://www.hcaptcha.com/privacy"],
  ["recaptcha", "Google reCAPTCHA", "https://policies.google.com/privacy"],
];

/** Administration → Bot protection: providers, policies, places, and what
 * happened in the last 7 days (usage-prices-concept). */
export class PhxBotAdmin extends LitElement {
  static properties = { s: { state: true }, cfg: { state: true }, advanced: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .grid { display: grid; gap: 10px; }
    /* Tables and grids in a card keep the card's inner margin. */
    .scard > .tbl { margin: 0 18px; }
    .scard > .grid { padding: 0 18px 16px; }
    .scard > .tbl + .pad, .scard > .bars + .tbl { margin-top: 0; }
    .scard > .tbl:last-child { margin-bottom: 16px; }
    .pad { padding: 12px 18px 16px; }
    .prov { border: 1px solid var(--line); border-radius: 12px; padding: 12px; display: grid; gap: 8px; background: var(--surface); }
    .prov header { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
    .row2 { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 8px; }
    label.check { display: flex; gap: 8px; align-items: flex-start; font-size: 14px; }
    label.check input { width: auto; margin-top: 3px; }
    .tbl { overflow-x: auto; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
    table { border-collapse: collapse; width: 100%; font-size: 14px; }
    th { text-align: left; font-size: 12px; font-weight: 500; color: var(--muted); padding: 8px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; }
    td { padding: 6px 10px; border-bottom: 1px solid var(--surface2); }
    tr:last-child td { border-bottom: 0; }
    td.n, th.n { text-align: right; font-variant-numeric: tabular-nums; }
    td select { width: auto; }
    .bars { display: flex; align-items: flex-end; gap: 3px; height: 60px; padding: 6px 0; }
    .bars span { flex: 1; background: var(--bad); border-radius: 2px 2px 0 0; min-height: 1px; opacity: 0.8; }
    .snippet { font-size: 13px; background: var(--surface2); border-radius: 8px; padding: 8px 10px; white-space: pre-wrap; }
    .muted { color: var(--muted); font-size: 13px; }
    .pol { border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; display: grid; gap: 8px; background: var(--surface); }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; }
  `];

  s?: Settings;
  cfg?: Config;
  advanced = false;
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    try {
      this.s = await api<Settings>("GET", "/admin/bot-protection");
      this.cfg = structuredClone(this.s.config);
      this.advanced = this.cfg.policies.some((p) => !p.preset);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async save() {
    if (!this.cfg) return;
    this.busy = true;
    try {
      this.s = await api<Settings>("PUT", "/admin/bot-protection", this.cfg);
      this.cfg = structuredClone(this.s.config);
      toast(t("settings.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private change() {
    this.cfg = { ...this.cfg! };
  }

  private provider(id: string, name: string, privacy: string) {
    const c = this.cfg!;
    const k = (c.providers[id] ??= { enabled: false, siteKey: "", hasSecret: false, dpaConfirmed: false });
    return html`<div class="prov">
      <header><strong>${name}</strong>
        <label class="check"><input type="checkbox" .checked=${k.enabled} @change=${(e: Event) => { k.enabled = (e.target as HTMLInputElement).checked; this.change(); }}> ${t("botadmin.enabled")}</label></header>
      ${k.enabled ? html`<div class="row2">
          <label class="field">${t("botadmin.site_key")}<input .value=${k.siteKey} @input=${(e: Event) => { k.siteKey = (e.target as HTMLInputElement).value.trim(); }}></label>
          <label class="field">${t("botadmin.secret")}<input type="password" autocomplete="off" placeholder=${k.hasSecret ? t("botadmin.secret_stored") : ""} @input=${(e: Event) => { k.secret = (e.target as HTMLInputElement).value.trim(); }}></label>
        </div>
        <label class="check"><input type="checkbox" .checked=${k.dpaConfirmed} @change=${(e: Event) => { k.dpaConfirmed = (e.target as HTMLInputElement).checked; this.change(); }}>
          <span>${t("botadmin.dpa", { provider: name })}</span></label>
        <div><span class="muted">${t("botadmin.snippet_title")}</span><div class="snippet">${t("botadmin.snippet", { provider: name, url: privacy })}</div></div>` : nothing}
    </div>`;
  }

  private policyEditor(p: Policy) {
    const c = this.cfg!;
    const num = (v: string) => Math.max(0, Number(v.replace(",", ".")) || 0);
    return html`<div class="pol">
      <div class="row2">
        <label class="field">${t("botadmin.policy_name")}<input .value=${p.name} @input=${(e: Event) => { p.name = (e.target as HTMLInputElement).value; }}></label>
        <label class="field">${t("botadmin.provider")}<select @change=${(e: Event) => { p.provider = (e.target as HTMLSelectElement).value; this.change(); }}>
          <option value="own" ?selected=${p.provider === "own"}>${t("botadmin.own")}</option>
          ${PROVIDERS.filter(([id]) => c.providers[id]?.enabled).map(([id, name]) => html`<option value=${id} ?selected=${p.provider === id}>${name}</option>`)}
        </select></label>
      </div>
      ${p.provider === "own" ? html`<div class="row2">
        <label class="field">${t("botadmin.pass_above")}<input type="number" min="0" max="2" step="0.05" .value=${String(p.passAbove)} @input=${(e: Event) => (p.passAbove = num((e.target as HTMLInputElement).value))}></label>
        <label class="field">${t("botadmin.block_below")}<input type="number" min="0" max="1" step="0.05" .value=${String(p.blockBelow)} @input=${(e: Event) => (p.blockBelow = num((e.target as HTMLInputElement).value))}></label>
        <label class="field">${t("botadmin.visible")}<select @change=${(e: Event) => (p.visible = (e.target as HTMLSelectElement).value)}>
          ${["any", "slider", "rotate", "none"].map((v) => html`<option value=${v} ?selected=${p.visible === v}>${t(`botadmin.visible.${v}` as any)}</option>`)}</select></label>
        <label class="field">${t("botadmin.difficulty")}<input type="number" min="8" max="24" .value=${String(p.difficulty || 16)} @input=${(e: Event) => (p.difficulty = Math.round(num((e.target as HTMLInputElement).value)))}></label>
      </div>
      <p class="muted">${t("botadmin.steps_hint")}</p>` : nothing}
      <label class="check"><input type="checkbox" .checked=${p.emailFallback} @change=${(e: Event) => (p.emailFallback = (e.target as HTMLInputElement).checked)}> ${t("botadmin.email_fallback")}</label>
      <div><button class="ghost" @click=${() => { c.policies = c.policies.filter((x) => x !== p); for (const k of Object.keys(c.places)) if (c.places[k] === p.id) c.places[k] = "normal"; this.change(); }}>${icon("Trash2", 14)} ${t("botadmin.remove_policy")}</button></div>
    </div>`;
  }

  render() {
    const s = this.s, c = this.cfg;
    if (!s || !c) return html`${pageHead(t("botadmin.title"), t("botadmin.lead"))}`;
    const loc = locale();
    const time = (v: string) => new Date(v).toLocaleString(loc, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
    const max = Math.max(1, ...s.stats.hourly);
    const presets = c.policies.filter((p) => p.preset);
    const custom = c.policies.filter((p) => !p.preset);
    return html`${pageHead(t("botadmin.title"), t("botadmin.lead"))}
      ${scard(t("botadmin.places"), t("botadmin.places_hint"), html`<div class="tbl"><table>
        <thead><tr><th>${t("botadmin.place")}</th><th>${t("botadmin.policy")}</th></tr></thead>
        <tbody>${PLACES.map((pl) => html`<tr><td>${t(`botadmin.place.${pl}` as any)}${pl === "signin" ? html`<div class="muted">${t("botadmin.signin_hint", { n: c.signinFailures })}</div>` : nothing}</td>
          <td><select aria-label=${t(`botadmin.place.${pl}` as any)} @change=${(e: Event) => { c.places[pl] = (e.target as HTMLSelectElement).value; this.change(); }}>
            <option value="" ?selected=${!c.places[pl]}>${t("botadmin.off")}</option>
            ${c.policies.map((p) => html`<option value=${p.id} ?selected=${c.places[pl] === p.id}>${p.preset ? t(`botadmin.preset.${p.preset}` as any) : p.name}</option>`)}
          </select></td></tr>`)}</tbody></table></div>
        <div class="pad"><label class="field" style="max-width:260px">${t("botadmin.signin_failures")}<input type="number" min="1" max="20" .value=${String(c.signinFailures)} @input=${(e: Event) => (c.signinFailures = Math.max(1, Number((e.target as HTMLInputElement).value) || 3))}></label>
        ${!s.mail ? html`<p class="muted">${icon("CircleAlert", 13)} ${t("botadmin.no_mail")}</p>` : nothing}</div>`)}
      ${scard(t("botadmin.policies"), t("botadmin.policies_hint"), html`<div class="grid">
        <div class="tbl"><table><thead><tr><th>${t("botadmin.policy")}</th><th>${t("botadmin.how")}</th></tr></thead>
          <tbody>${presets.map((p) => html`<tr><td>${t(`botadmin.preset.${p.preset}` as any)}</td><td class="muted">${t(`botadmin.preset_hint.${p.preset}` as any)}</td></tr>`)}</tbody></table></div>
        <label class="check"><input type="checkbox" .checked=${this.advanced} @change=${(e: Event) => (this.advanced = (e.target as HTMLInputElement).checked)}> ${t("botadmin.advanced")}</label>
        ${this.advanced ? html`${custom.map((p) => this.policyEditor(p))}
          <div><button class="ghost" @click=${() => { c.policies = [...c.policies, { id: "p" + Date.now().toString(36), name: t("botadmin.new_policy"), provider: "own", passAbove: 0.6, blockBelow: 0.15, visible: "any", emailFallback: true, difficulty: 16 }]; this.change(); }}>${icon("Plus", 14)} ${t("botadmin.add_policy")}</button></div>` : nothing}
      </div>`)}
      ${scard(t("botadmin.providers"), t("botadmin.providers_hint"), html`<div class="grid">${PROVIDERS.map(([id, name, url]) => this.provider(id, name, url))}</div>`)}
      <div class="actions" style="margin:4px 0 22px"><button ?disabled=${this.busy} @click=${this.save}>${t("doc.save")}</button></div>
      ${scard(t("botadmin.stats"), t("botadmin.stats_hint"), html`
        <div class="muted" style="padding:0 18px">${t("botadmin.hourly")}</div>
        <div class="bars" style="padding:6px 18px 10px" role="img" aria-label=${t("botadmin.hourly")}>${s.stats.hourly.map((n) => html`<span style="height:${(n / max) * 100}%" title=${String(n)}></span>`)}</div>
        <div class="tbl"><table><thead><tr><th>${t("botadmin.place")}</th><th>${t("botadmin.policy")}</th><th class="n">${t("botadmin.passed")}</th><th class="n">${t("botadmin.challenged")}</th><th class="n">${t("botadmin.solved")}</th><th class="n">${t("botadmin.by_email")}</th><th class="n">${t("botadmin.blocked")}</th><th class="n">${t("botadmin.avg")}</th></tr></thead>
          <tbody>${s.stats.rows.length ? s.stats.rows.map((r) => html`<tr><td>${t(`botadmin.place.${r.place}` as any)}</td><td>${r.policy}</td><td class="n">${r.passed}</td><td class="n">${r.challenged}</td><td class="n">${r.solved}</td><td class="n">${r.email}</td><td class="n">${r.blocked}</td><td class="n">${r.avgSolveMs ? `${(r.avgSolveMs / 1000).toFixed(1)} s` : "–"}</td></tr>`)
            : html`<tr><td colspan="8" class="muted">${t("botadmin.no_events")}</td></tr>`}</tbody></table></div>
        ${s.stats.blocked.length ? html`<h4 style="margin:14px 18px 6px">${t("botadmin.blocked_log")}</h4><div class="tbl"><table><tbody>${s.stats.blocked.slice(0, 50).map((e) => html`<tr><td>${time(e.at)}</td><td>${t(`botadmin.place.${e.place}` as any)}</td><td>${e.reason ? t(`botadmin.reason.${e.reason}` as any) : e.outcome}</td><td class="muted"><code>${e.ipHash}</code></td></tr>`)}</tbody></table></div>` : nothing}`)}`;
  }
}
customElements.get("phx-bot-admin") || customElements.define("phx-bot-admin", PhxBotAdmin);
