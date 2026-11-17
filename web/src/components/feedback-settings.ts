import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import type { AttachRules, PortalField } from "./feedback-portal";
import { stateLabel } from "./feedback-portal";
import "./feedback-trackers";
import "./feedback-storage";
import { pageHead, settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Settings {
  intro: Record<string, string>; privacy: Record<string, string>; languages: string[]; types: string[]; categories: string[]; tags?: string[]; aiTriage?: boolean; autoAnswers?: boolean; publicView?: boolean; publicComments?: boolean; widget?: boolean; widgetOrigins?: string[]; reporterLogin?: { issuer: string; clientId: string; clientSecret?: string; hasSecret?: boolean; label?: string; required?: boolean } | null; fields: PortalField[];
  attachments: AttachRules; anonymous: boolean; accounts: boolean; askEmail: boolean; states: string[]; defaultState: string; finishedState: string;
  storageFull: string; theme: { accent?: string; accentDark?: string; logo?: string }; publicKeyOn: boolean; serverKeyOn: boolean; retentionMonths: number;
}
interface Portal { id: string; handle: string; name: string; enabled: boolean; settings: Settings; publicKey?: string; hasServerKey: boolean; serverKey?: string; hasPlayerKey?: boolean; playerKey?: string; moderators: { email: string; name: string }[] }

const TYPES = ["bug", "idea", "question", "praise"];
const KINDS = ["images", "videos", "logs", "other"] as const;
const FIELD_TYPES = ["text", "number", "choice", "multi", "version", "bool", "url", "date"];
const csv = (v: string) => v.split(",").map((x) => x.trim()).filter(Boolean);

/** Project settings → Feedback portals (owners). */
export class PhxFeedbackSettings extends LitElement {
  static properties = { project: {}, portals: { state: true }, edit: { state: true }, name: { state: true }, handle: { state: true }, busy: { state: true }, shownKey: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .card { border: 1px solid var(--line); border-radius: 14px; background: var(--surface); padding: 16px; display: grid; gap: 12px; margin-bottom: 14px; }
    .card h3 { margin: 0; font-size: 15px; display: flex; gap: 8px; align-items: center; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px; }
    label.check { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    label.check input { width: auto; }
    .checks { display: flex; gap: 14px; flex-wrap: wrap; }
    .field-row { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 6px 8px; align-items: end; border-top: 1px solid var(--surface2); padding-top: 8px; }
    .field-row > button { justify-self: start; width: auto; }
    input[type=color] { height: 38px; padding: 3px; cursor: pointer; }
    .list { display: grid; gap: 8px; }
    .item { display: flex; gap: 10px; align-items: center; justify-content: space-between; border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; background: var(--surface); flex-wrap: wrap; }
    .muted { color: var(--muted); font-size: 13px; }
    code.key { word-break: break-all; background: var(--surface2); padding: 4px 8px; border-radius: 6px; }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; }
  `];

  project = "";
  portals: Portal[] = [];
  edit?: Portal;
  busy = false;
  shownKey = "";

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.portals = await api<Portal[]>("GET", `/projects/${this.project}/feedback/portals`).catch(() => []);
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      const p = await api<Portal>("POST", `/projects/${this.project}/feedback/portals`, { name: String(f.get("name")), handle: String(f.get("handle")) });
      await this.load();
      this.edit = structuredClone(p);
      toast(t("fb.portal_created"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async save() {
    const p = this.edit!;
    this.busy = true;
    try {
      const out = await api<Portal>("PUT", `/projects/${this.project}/feedback/portals/${p.id}`, { name: p.name, handle: p.handle, enabled: p.enabled, settings: p.settings });
      const mods = (this.renderRoot.querySelector("#mods") as HTMLTextAreaElement | null)?.value;
      if (mods !== undefined && mods !== p.moderators.map((m) => m.email).join("\n")) {
        await api("PUT", `/projects/${this.project}/feedback/portals/${p.id}/moderators`, { emails: csv(mods.replaceAll("\n", ",")) });
      }
      if (out.serverKey) this.shownKey = out.serverKey;
      await this.load();
      this.edit = structuredClone(this.portals.find((x) => x.id === p.id) ?? out);
      toast(t("settings.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async rotate(kind: "public" | "server" | "player") {
    if ((kind !== "player" || this.edit?.hasPlayerKey) && !confirm(t("fb.rotate_confirm"))) return;
    try {
      const out = await api<Portal>("POST", `/projects/${this.project}/feedback/portals/${this.edit!.id}/keys`, { kind });
      if (out.serverKey) this.shownKey = out.serverKey;
      if (out.playerKey) this.shownKey = out.playerKey;
      this.edit = { ...this.edit!, publicKey: out.publicKey, hasServerKey: out.hasServerKey, hasPlayerKey: out.hasPlayerKey };
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async removePortal() {
    if (!confirm(t("fb.delete_portal_confirm", { name: this.edit!.name }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/feedback/portals/${this.edit!.id}`);
      this.edit = undefined;
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private up() {
    this.edit = { ...this.edit! };
  }

  private fieldEditor(s: Settings) {
    return html`${s.fields.map((f, i) => html`<div class="field-row">
        <label class="field">${t("fb.f_id")}<input .value=${f.id} @input=${(e: Event) => (f.id = (e.target as HTMLInputElement).value.trim())}></label>
        <label class="field">${t("fb.f_label_en")}<input .value=${f.label.en ?? ""} @input=${(e: Event) => (f.label.en = (e.target as HTMLInputElement).value)}></label>
        <label class="field">${t("fb.f_label_de")}<input .value=${f.label.de ?? ""} @input=${(e: Event) => (f.label.de = (e.target as HTMLInputElement).value)}></label>
        <label class="field">${t("fb.f_type")}<select @change=${(e: Event) => { f.type = (e.target as HTMLSelectElement).value; this.up(); }}>${FIELD_TYPES.map((x) => html`<option value=${x} ?selected=${x === f.type}>${t(`fb.ftype.${x}` as any)}</option>`)}</select></label>
        ${f.type === "choice" || f.type === "multi" ? html`<label class="field">${t("fb.f_options")}<input .value=${(f.options ?? []).join(", ")} @change=${(e: Event) => (f.options = csv((e.target as HTMLInputElement).value))}></label>` : nothing}
        <label class="field">${t("fb.f_types")}<input .value=${(f.reportTypes ?? []).join(", ")} placeholder=${t("fb.all")} @change=${(e: Event) => (f.reportTypes = csv((e.target as HTMLInputElement).value))}></label>
        <label class="check"><input type="checkbox" .checked=${f.required} @change=${(e: Event) => (f.required = (e.target as HTMLInputElement).checked)}> ${t("fb.f_required")}</label>
        <button class="ghost" aria-label=${t("fb.f_remove")} @click=${() => { s.fields.splice(i, 1); this.up(); }}>${icon("Trash2", 14)}</button>
      </div>`)}
      <div><button class="ghost" @click=${() => { s.fields.push({ id: `field${s.fields.length + 1}`, label: { en: "", de: "" }, type: "text", required: false }); this.up(); }}>${icon("Plus", 14)} ${t("fb.f_add")}</button></div>`;
  }

  private editor(p: Portal) {
    const s = p.settings;
    const a = s.attachments;
    const lang = (m: Record<string, string>, l: string) => (e: Event) => (m[l] = (e.target as HTMLTextAreaElement).value);
    return html`
      <div class="actions" style="margin-bottom:12px"><button class="ghost" @click=${() => (this.edit = undefined)}>${icon("ArrowLeft", 14)} ${t("fb.back_portals")}</button>
        <a class="btn ghost" href=${`/feedback/${p.handle}`} target="_blank" rel="noopener">${icon("ExternalLink", 14)} /feedback/${p.handle}</a></div>
      <div class="card"><h3>${icon("MessageSquare", 15)} ${t("fb.basics")}</h3>
        <div class="grid">
          <label class="field">${t("fb.portal_name")}<input .value=${p.name} @input=${(e: Event) => (p.name = (e.target as HTMLInputElement).value)}></label>
          <label class="field">${t("fb.handle")}<input .value=${p.handle} @input=${(e: Event) => (p.handle = (e.target as HTMLInputElement).value)}></label>
        </div>
        <label class="check"><input type="checkbox" .checked=${p.enabled} @change=${(e: Event) => (p.enabled = (e.target as HTMLInputElement).checked)}> ${t("fb.enabled")}</label>
        <div class="grid">
          <label class="field">${t("fb.intro")} (EN)<textarea rows="3" .value=${s.intro.en ?? ""} @input=${lang(s.intro, "en")}></textarea></label>
          <label class="field">${t("fb.intro")} (DE)<textarea rows="3" .value=${s.intro.de ?? ""} @input=${lang(s.intro, "de")}></textarea></label>
        </div>
      </div>
      <div class="card"><h3>${icon("Users", 15)} ${t("fb.who")}</h3>
        <div class="checks">
          <label class="check"><input type="checkbox" .checked=${s.accounts} @change=${(e: Event) => (s.accounts = (e.target as HTMLInputElement).checked)}> ${t("fb.who_accounts")}</label>
          <label class="check"><input type="checkbox" .checked=${s.anonymous} @change=${(e: Event) => { s.anonymous = (e.target as HTMLInputElement).checked; this.up(); }}> ${t("fb.who_anonymous")}</label>
          ${s.anonymous ? html`<label class="check"><input type="checkbox" .checked=${s.askEmail} @change=${(e: Event) => (s.askEmail = (e.target as HTMLInputElement).checked)}> ${t("fb.ask_email")}</label>` : nothing}
        </div>
        <p class="muted">${t("fb.who_hint")}</p>
        <div class="checks">
          <label class="check"><input type="checkbox" .checked=${s.publicKeyOn} @change=${(e: Event) => { s.publicKeyOn = (e.target as HTMLInputElement).checked; this.up(); }}> ${t("fb.public_key")}</label>
          <label class="check"><input type="checkbox" .checked=${s.serverKeyOn} @change=${(e: Event) => { s.serverKeyOn = (e.target as HTMLInputElement).checked; this.up(); }}> ${t("fb.server_key")}</label>
        </div>
        ${p.publicKey ? html`<div class="muted">${t("fb.public_key_is")} <code class="key">${p.publicKey}</code> <button class="ghost" @click=${() => void this.rotate("public")}>${t("fb.rotate")}</button></div>` : nothing}
        ${p.hasServerKey ? html`<div class="muted">${t("fb.server_key_set")} <button class="ghost" @click=${() => void this.rotate("server")}>${t("fb.rotate")}</button></div>` : nothing}
        ${this.shownKey ? html`<div role="status"><strong>${t("fb.server_key_once")}</strong><br><code class="key">${this.shownKey}</code></div>` : nothing}
        <p class="muted">${t("fb.keys_hint", { handle: p.handle })}</p>
        <div class="muted"><strong>${t("fb.player_key")}</strong> – ${t("fb.player_key_help")}
          <button class="ghost" @click=${() => void this.rotate("player")}>${t(p.hasPlayerKey ? "fb.rotate" : "fb.player_key_create")}</button></div>
      </div>
      ${this.widgetCard(p, s)}
      ${this.loginCard(s)}
      <div class="card"><h3>${icon("ListChecks", 15)} ${t("fb.form")}</h3>
        <div class="checks">${TYPES.map((ty) => html`<label class="check"><input type="checkbox" .checked=${s.types.includes(ty)} @change=${(e: Event) => { s.types = (e.target as HTMLInputElement).checked ? [...s.types, ty] : s.types.filter((x) => x !== ty); }}> ${t(`fb.type.${ty}` as any)}</label>`)}</div>
        <label class="field">${t("fb.categories")}<input .value=${s.categories.join(", ")} placeholder="UI, Server, Audio" @change=${(e: Event) => (s.categories = csv((e.target as HTMLInputElement).value))}></label>
        <label class="field">${t("fb.tags_list")}<input .value=${(s.tags ?? []).join(", ")} placeholder="crash, map, save" @change=${(e: Event) => (s.tags = csv((e.target as HTMLInputElement).value))}></label>
        <label class="check"><input type="checkbox" .checked=${!!s.aiTriage} @change=${(e: Event) => (s.aiTriage = (e.target as HTMLInputElement).checked)}> ${t("fb.ai_triage")}</label>
        <p class="muted" style="margin:0">${t("fb.ai_triage_help")}</p>
        <label class="check"><input type="checkbox" .checked=${!!s.autoAnswers} @change=${(e: Event) => (s.autoAnswers = (e.target as HTMLInputElement).checked)}> ${t("fb.auto_answers")}</label>
        <p class="muted" style="margin:0">${t("fb.auto_answers_help")}</p>
        <label class="check"><input type="checkbox" .checked=${!!s.publicView} @change=${(e: Event) => { s.publicView = (e.target as HTMLInputElement).checked; this.requestUpdate(); }}> ${t("fbpub.settings_public")}</label>
        <p class="muted" style="margin:0 0 0 24px;font-size:13px">${t("fbpub.settings_public_help")}</p>
        ${s.publicView ? html`<label class="check"><input type="checkbox" .checked=${!!s.publicComments} @change=${(e: Event) => (s.publicComments = (e.target as HTMLInputElement).checked)}> ${t("fbpub.settings_comments")}</label>
          <p class="muted" style="margin:0 0 0 24px;font-size:13px">${t("fbpub.settings_comments_help")}</p>` : nothing}
        <strong style="font-size:14px">${t("fb.fields")}</strong>
        ${this.fieldEditor(s)}
      </div>
      <div class="card"><h3>${icon("CircleDot", 15)} ${t("fb.states")}</h3>
        <label class="field">${t("fb.states_list")}<input .value=${s.states.join(", ")} @change=${(e: Event) => { s.states = csv((e.target as HTMLInputElement).value); this.up(); }}></label>
        <div class="grid">
          <label class="field">${t("fb.default_state")}<select @change=${(e: Event) => (s.defaultState = (e.target as HTMLSelectElement).value)}>${s.states.map((x) => html`<option value=${x} ?selected=${x === s.defaultState}>${stateLabel(x)}</option>`)}</select></label>
          <label class="field">${t("fb.finished_state")}<select @change=${(e: Event) => (s.finishedState = (e.target as HTMLSelectElement).value)}>${s.states.map((x) => html`<option value=${x} ?selected=${x === s.finishedState}>${stateLabel(x)}</option>`)}</select></label>
        </div>
      </div>
      <div class="card"><h3>${icon("Paperclip", 15)} ${t("fb.attachments")}</h3>
        ${KINDS.map((k) => html`<div class="grid">
          <label class="check"><input type="checkbox" .checked=${a[k]} @change=${(e: Event) => { a[k] = (e.target as HTMLInputElement).checked; this.up(); }}> ${t(`fb.kind.${k}` as any)}</label>
          ${a[k] ? html`<label class="field">${t("fb.endings")}<input .value=${(a.endings[k] ?? []).join(", ")} @change=${(e: Event) => (a.endings[k] = csv((e.target as HTMLInputElement).value))}></label>
            <label class="field">${t("fb.max_mb")}<input type="number" min="1" max="2048" .value=${String(a.maxMb[k] ?? 10)} @input=${(e: Event) => (a.maxMb[k] = Number((e.target as HTMLInputElement).value) || 1)}></label>` : nothing}
        </div>`)}
        <div class="grid">
          <label class="check"><input type="checkbox" .checked=${a.textOnly} @change=${(e: Event) => (a.textOnly = (e.target as HTMLInputElement).checked)}> ${t("fb.text_only")}</label>
          <label class="field">${t("fb.max_files")}<input type="number" min="0" max="20" .value=${String(a.maxFiles)} @input=${(e: Event) => (a.maxFiles = Number((e.target as HTMLInputElement).value) || 0)}></label>
          <label class="field">${t("fb.storage_full")}<select @change=${(e: Event) => (s.storageFull = (e.target as HTMLSelectElement).value)}>${["no_files", "drop_oldest", "pause"].map((x) => html`<option value=${x} ?selected=${x === s.storageFull}>${t(`fb.full.${x}` as any)}</option>`)}</select></label>
        </div>
        <p class="muted">${t("fb.attachments_hint")}</p>
      </div>
      <div class="card"><h3>${icon("Palette", 15)} ${t("fb.look")}</h3>
        <div class="grid">
          <label class="field">${t("fb.accent")}<input type="color" .value=${s.theme.accent || "#b84a0c"} @input=${(e: Event) => (s.theme.accent = (e.target as HTMLInputElement).value)}></label>
          <label class="field">${t("fb.accent_dark")}<input type="color" .value=${s.theme.accentDark || s.theme.accent || "#f26a1b"} @input=${(e: Event) => (s.theme.accentDark = (e.target as HTMLInputElement).value)}></label>
          <label class="field">${t("fb.logo")}<input .value=${s.theme.logo ?? ""} placeholder="https://…" @change=${(e: Event) => (s.theme.logo = (e.target as HTMLInputElement).value.trim())}></label>
        </div>
      </div>
      <div class="card"><h3>${icon("ShieldCheck", 15)} ${t("fb.privacy")}</h3>
        <div class="grid">
          <label class="field">${t("fb.privacy")} (EN)<textarea rows="3" .value=${s.privacy.en ?? ""} placeholder=${t("fb.privacy_default")} @input=${lang(s.privacy, "en")}></textarea></label>
          <label class="field">${t("fb.privacy")} (DE)<textarea rows="3" .value=${s.privacy.de ?? ""} placeholder=${t("fb.privacy_default")} @input=${lang(s.privacy, "de")}></textarea></label>
        </div>
        <label class="field" style="max-width:260px">${t("fb.retention")}<input type="number" min="1" max="120" .value=${String(s.retentionMonths)} @input=${(e: Event) => (s.retentionMonths = Number((e.target as HTMLInputElement).value) || 12)}></label>
      </div>
      <div class="card"><h3>${icon("UserCheck", 15)} ${t("fb.moderators")}</h3>
        <p class="muted">${t("fb.moderators_hint")}</p>
        <textarea id="mods" rows="3" placeholder="name@example.org" .value=${p.moderators.map((m) => m.email).join("\n")}></textarea>
      </div>
      <div class="actions"><button ?disabled=${this.busy} @click=${this.save}>${t("doc.save")}</button>
        <button class="ghost danger" @click=${this.removePortal}>${icon("Trash2", 14)} ${t("fb.delete_portal")}</button></div>`;
  }

  private loginCard(s: Settings) {
    const rl = s.reporterLogin;
    const set = (k: "issuer" | "clientId" | "clientSecret" | "label", v: string) => {
      s.reporterLogin = { issuer: "", clientId: "", ...(s.reporterLogin ?? {}), [k]: v.trim() };
    };
    return html`<div class="card"><h3>${icon("LogIn", 15)} ${t("fbl.settings_title")}</h3>
      <p class="muted" style="margin:0">${t("fbl.settings_hint")}</p>
      <div class="grid">
        <label class="field">${t("fbl.issuer")}<input type="url" placeholder="https://login.example.com" .value=${rl?.issuer ?? ""} @change=${(e: Event) => set("issuer", (e.target as HTMLInputElement).value)}></label>
        <label class="field">${t("fbl.client_id")}<input .value=${rl?.clientId ?? ""} @change=${(e: Event) => set("clientId", (e.target as HTMLInputElement).value)}></label>
        <label class="field">${t("fbl.client_secret")}<input type="password" autocomplete="new-password" placeholder=${rl?.hasSecret ? t("fbl.secret_kept") : ""} @change=${(e: Event) => set("clientSecret", (e.target as HTMLInputElement).value)}></label>
        <label class="field">${t("fbl.label")}<input placeholder=${t("fbl.label_ph")} .value=${rl?.label ?? ""} @change=${(e: Event) => set("label", (e.target as HTMLInputElement).value)}></label>
      </div>
      <label class="check"><input type="checkbox" .checked=${!!rl?.required} @change=${(e: Event) => { if (s.reporterLogin) s.reporterLogin.required = (e.target as HTMLInputElement).checked; }}> ${t("fbl.required")}</label>
      <div class="muted">${t("fbl.redirect")}</div>
      <code class="key" style="display:block;word-break:break-all">${location.origin}/api/v1/feedback/login/callback</code>
      ${rl ? html`<div><button class="ghost" @click=${() => { s.reporterLogin = null; this.up(); }}>${icon("Trash2", 14)} ${t("fbl.remove")}</button></div>` : nothing}
    </div>`;
  }

  private widgetCard(p: Portal, s: Settings) {
    const snippet = `<script src="${location.origin}/feedback-widget.js" data-portal="${p.handle}" async></script>`;
    return html`<div class="card"><h3>${icon("MessageSquare", 15)} ${t("fbw.settings_title")}</h3>
      <label class="check"><input type="checkbox" .checked=${!!s.widget} @change=${(e: Event) => { s.widget = (e.target as HTMLInputElement).checked; this.up(); }}> ${t("fbw.settings_on")}</label>
      <p class="muted" style="margin:0">${t("fbw.settings_hint")}</p>
      ${s.widget ? html`
        ${s.anonymous ? nothing : html`<p class="muted" role="note" style="margin:0">${icon("Info", 13)} ${t("fbw.settings_needs_anon")}</p>`}
        <label class="field">${t("fbw.settings_origins")}<textarea rows="3" placeholder="https://example.com" .value=${(s.widgetOrigins ?? []).join("\n")}
          @change=${(e: Event) => (s.widgetOrigins = (e.target as HTMLTextAreaElement).value.split(/\s+/).map((x) => x.trim()).filter(Boolean))}></textarea></label>
        <p class="muted" style="margin:0">${t("fbw.settings_origins_hint")}</p>
        <div class="muted">${t("fbw.settings_snippet")}</div>
        <code class="key" style="display:block;white-space:pre-wrap;word-break:break-all">${snippet}</code>
        <div><button class="ghost" @click=${() => { void navigator.clipboard.writeText(snippet); toast(t("fb.copied")); }}>${icon("Copy", 14)} ${t("fbw.settings_copy")}</button></div>
        <p class="muted" style="margin:0">${t("fbw.settings_api")}</p>` : nothing}
    </div>`;
  }

  render() {
    return html`${pageHead(t("fb.settings_title"), t("fb.settings_lead"))}
      ${this.edit ? this.editor(this.edit) : html`
        <div class="list">${this.portals.map((p) => html`<div class="item"><div><strong>${p.name}</strong> <span class="muted">/feedback/${p.handle}${p.enabled ? "" : ` · ${t("fb.off")}`}</span></div>
          <div class="actions"><a class="btn ghost" href=${`/feedback/${p.handle}`} target="_blank" rel="noopener">${icon("ExternalLink", 14)}</a><button class="ghost" @click=${() => { this.edit = structuredClone(p); this.shownKey = ""; }}>${icon("Pencil", 14)} ${t("fb.edit")}</button></div></div>`)}</div>
        <form class="card" style="margin-top:14px" @submit=${this.create}><h3>${icon("Plus", 15)} ${t("fb.create_portal")}</h3>
          <div class="grid"><label class="field">${t("fb.portal_name")}<input name="name" required maxlength="100" placeholder="Starfall"></label>
            <label class="field">${t("fb.handle")}<input name="handle" required pattern="[a-z0-9][a-z0-9-]{1,38}[a-z0-9]" placeholder="starfall"></label></div>
          <p class="muted">${t("fb.handle_hint")}</p>
          <div><button>${t("fb.create")}</button></div></form>
        <div style="margin-top:14px"><phx-feedback-trackers .project=${this.project}></phx-feedback-trackers>
          <phx-feedback-storage .project=${this.project}></phx-feedback-storage></div>`}`;
  }
}
customElements.get("phx-feedback-settings") || customElements.define("phx-feedback-settings", PhxFeedbackSettings);

/** Settings → My feedback: reports the person sent to portals. */
export class PhxMyFeedback extends LitElement {
  static properties = { items: { state: true } };
  static styles = [base, settingsUI, css`
    .item { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 12px 14px; display: grid; gap: 4px; margin-bottom: 8px; }
    .muted { color: var(--muted); font-size: 13px; }
    .answer { border-left: 3px solid var(--accent); padding-left: 10px; white-space: pre-wrap; font-size: 14px; }
  `];
  items?: { portal: string; handle: string; type: string; title: string; createdAt: string; state: string; finished: boolean; answer?: string }[];

  async connectedCallback() {
    super.connectedCallback();
    this.items = await api<PhxMyFeedback["items"]>("GET", "/me/feedback").catch(() => []);
  }

  render() {
    return html`${pageHead(t("fb.mine"), t("fb.mine_lead"))}
      ${!this.items ? html`<p class="muted">${t("app.loading")}</p>` : !this.items.length ? html`<p class="muted">${t("fb.mine_none")}</p>`
        : this.items.map((r) => html`<div class="item"><div class="muted">${r.portal} · ${t(`fb.type.${r.type}` as any)} · ${new Date(r.createdAt).toLocaleDateString()}</div>
          <strong>${r.title}</strong><div>${icon(r.finished ? "CircleCheck" : "Clock", 14)} ${stateLabel(r.state)}</div>
          ${r.answer ? html`<div class="answer">${r.answer}</div>` : nothing}</div>`)}`;
  }
}
customElements.get("phx-my-feedback") || customElements.define("phx-my-feedback", PhxMyFeedback);
