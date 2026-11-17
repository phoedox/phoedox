import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";
import "./botcheck";
import type { PhxBotcheck } from "./botcheck";
import { base } from "./shared";
import { toast } from "./toasts";

export interface PortalField { id: string; label: Record<string, string>; type: string; options?: string[]; required: boolean; reportTypes?: string[] }
export interface AttachRules { images: boolean; videos: boolean; logs: boolean; other: boolean; endings: Record<string, string[]>; maxMb: Record<string, number>; textOnly: boolean; maxFiles: number }
interface Portal {
  handle: string; name: string; project: string; intro: string; privacy: string; types: string[]; fields: PortalField[]; attachments: AttachRules;
  anonymous: boolean; accounts: boolean; askEmail: boolean; theme: { accent?: string; accentDark?: string; logo?: string }; accepting: boolean; filesFull: boolean;
}
interface Status { portal: string; handle: string; type: string; title: string; body: string; createdAt: string; state: string; finished: boolean; answer?: string; fixedIn?: string; answerAuto?: boolean; question?: string; replies?: { id: string; body: string; createdAt: string }[]; reports: number; deleted: boolean; files: number }

export const label = (m: Record<string, string> | undefined) => (m ? m[locale()] || m.en || m.de || Object.values(m)[0] || "" : "");

/** The portal's colours as custom properties; the component's styles pick
 * the light or dark one. */
function themeStyle(th: Portal["theme"]) {
  return `${th.accent ? `--pl:${th.accent};` : ""}${th.accentDark || th.accent ? `--pd:${th.accentDark || th.accent};` : ""}`;
}

/** Public page of a feedback portal: /feedback/<handle>. */
export class PhxFeedbackPortal extends LitElement {
  static properties = { handle: {}, portal: { state: true }, type: { state: true }, files: { state: true }, busy: { state: true }, error: { state: true }, done: { state: true }, missing: { state: true } };
  static styles = [base, css`
    :host { display: block; min-height: 100vh; background: var(--bg); }
    .wrap { max-width: 680px; margin: 0 auto; padding: 32px 16px 64px; }
    header { display: flex; gap: 12px; align-items: center; margin-bottom: 18px; }
    header img { width: 44px; height: 44px; border-radius: 10px; object-fit: contain; background: var(--surface); }
    h1 { margin: 0; font-size: 24px; }
    .sub { color: var(--muted); font-size: 14px; }
    .card { background: var(--surface); border: 1px solid var(--line); border-radius: 16px; padding: 18px; display: grid; gap: 14px; }
    .types { display: flex; gap: 8px; flex-wrap: wrap; }
    .types button { border-radius: 99px; }
    .types button[aria-pressed=true] { background: var(--accent); color: var(--on-color); border-color: var(--accent); }
    .intro { white-space: pre-wrap; color: var(--fg); }
    .files { display: grid; gap: 6px; }
    .file { display: flex; gap: 8px; align-items: center; font-size: 14px; border: 1px solid var(--line); border-radius: 8px; padding: 6px 10px; }
    .file span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .privacy { font-size: 12.5px; color: var(--muted); white-space: pre-wrap; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .ok { display: grid; gap: 10px; text-align: left; }
    .ok code { word-break: break-all; }
    label.check { display: flex; gap: 8px; align-items: center; }
    label.check input { width: auto; }
    .powered { text-align: center; color: var(--muted); font-size: 12px; margin-top: 18px; }
    .wrap.themed { --accent: var(--pl); --accent-soft: color-mix(in srgb, var(--pl) 14%, var(--surface)); }
    @media (prefers-color-scheme: dark) { :host-context(html:not([data-theme=light])) .wrap.themed { --accent: var(--pd); --accent-soft: color-mix(in srgb, var(--pd) 20%, var(--surface)); } }
    :host-context(html[data-theme=dark]) .wrap.themed { --accent: var(--pd); --accent-soft: color-mix(in srgb, var(--pd) 20%, var(--surface)); }
    .powered a { color: inherit; }
  `];

  handle = "";
  portal?: Portal;
  type = "";
  files: File[] = [];
  busy = false;
  error = "";
  done?: { statusToken: string };
  missing = false;

  async connectedCallback() {
    super.connectedCallback();
    try {
      this.portal = await api<Portal>("GET", `/feedback/${this.handle}?lang=${locale()}`);
      this.type = this.portal.types[0] ?? "bug";
      document.title = this.portal.name;
    } catch {
      this.missing = true;
    }
  }

  private accept() {
    const a = this.portal!.attachments;
    const kinds = (["images", "videos", "logs", "other"] as const).filter((k) => a[k]);
    return kinds.flatMap((k) => (a.endings[k] ?? []).map((e) => "." + e)).join(",");
  }

  private addFiles(list: FileList | null) {
    if (!list) return;
    const max = this.portal!.attachments.maxFiles;
    this.files = [...this.files, ...Array.from(list)].slice(0, max);
  }

  private field(f: PortalField) {
    const name = `f_${f.id}`;
    const lbl = html`${label(f.label)}${f.required ? " *" : ""}`;
    switch (f.type) {
      case "choice":
        return html`<label class="field">${lbl}<select name=${name} ?required=${f.required}><option value=""></option>${(f.options ?? []).map((o) => html`<option>${o}</option>`)}</select></label>`;
      case "multi":
        return html`<fieldset style="border:0;padding:0;margin:0"><legend class="sub">${lbl}</legend><div class="row">${(f.options ?? []).map((o) => html`<label class="check"><input type="checkbox" name=${name} value=${o}> ${o}</label>`)}</div></fieldset>`;
      case "bool":
        return html`<label class="check"><input type="checkbox" name=${name}> ${lbl}</label>`;
      case "number":
        return html`<label class="field">${lbl}<input type="number" name=${name} ?required=${f.required}></label>`;
      case "date":
        return html`<label class="field">${lbl}<input type="date" name=${name} ?required=${f.required}></label>`;
      case "url":
        return html`<label class="field">${lbl}<input type="url" name=${name} ?required=${f.required} placeholder="https://"></label>`;
    }
    return html`<label class="field">${lbl}<input name=${name} maxlength="500" ?required=${f.required} placeholder=${f.type === "version" ? "1.2.3" : ""}></label>`;
  }

  private async submit(e: SubmitEvent) {
    e.preventDefault();
    const p = this.portal!;
    const form = e.target as HTMLFormElement;
    const fd = new FormData(form);
    const fields: Record<string, unknown> = {};
    for (const f of p.fields) {
      if (f.reportTypes?.length && !f.reportTypes.includes(this.type)) continue;
      const k = `f_${f.id}`;
      if (f.type === "multi") fields[f.id] = fd.getAll(k).map(String);
      else if (f.type === "bool") fields[f.id] = fd.get(k) === "on";
      else if (f.type === "number") { const v = String(fd.get(k) ?? ""); if (v) fields[f.id] = Number(v); }
      else { const v = String(fd.get(k) ?? "").trim(); if (v) fields[f.id] = v; }
    }
    this.busy = true;
    this.error = "";
    try {
      const signedIn = !!currentUser.get();
      const botToken = signedIn ? undefined : await this.renderRoot.querySelector<PhxBotcheck>("phx-botcheck")?.token();
      const report = {
        type: this.type, title: String(fd.get("title") ?? ""), body: String(fd.get("body") ?? ""), fields, lang: locale(),
        email: String(fd.get("email") ?? "") || undefined, botToken,
        context: { page: document.referrer || "", userAgent: navigator.userAgent.slice(0, 200), screen: `${screen.width}×${screen.height}` },
      };
      const body = new FormData();
      body.append("report", new Blob([JSON.stringify(report)], { type: "application/json" }));
      for (const f of this.files) body.append("file", f, f.name);
      const res = await fetch(`/api/v1/feedback/${p.handle}/reports`, { method: "POST", body, headers: { "X-Phoedox": "1" }, credentials: "same-origin" });
      const json = await res.json().catch(() => ({}));
      if (!res.ok) throw new ApiError(res.status, json.code ?? "error", json.key ?? "error.unknown", json.params ?? {});
      this.done = json;
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : err instanceof Error && err.message ? err.message : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  render() {
    if (this.missing) return html`<div class="wrap"><div class="card"><h1>${t("fb.not_found")}</h1></div></div>`;
    const p = this.portal;
    if (!p) return html`<div class="wrap"><p class="sub">${t("app.loading")}</p></div>`;
    const user = currentUser.get();
    const canReport = p.accepting && (user ? p.accounts || p.anonymous : p.anonymous);
    const status = this.done ? `${location.origin}/feedback/status/${this.done.statusToken}` : "";
    return html`<div class=${p.theme.accent ? "wrap themed" : "wrap"} style=${themeStyle(p.theme)}>
      <header>${p.theme.logo ? html`<img src=${p.theme.logo} alt="">` : nothing}<div><h1>${p.name}</h1><div class="sub">${t("fb.portal_sub", { project: p.project })}</div></div></header>
      ${this.done ? html`<div class="card ok" role="status">
          <h2 style="margin:0;font-size:18px">${icon("CircleCheck", 18)} ${t("fb.thanks")}</h2>
          <p style="margin:0">${t("fb.status_hint")}</p>
          <code>${status}</code>
          <div class="row"><button class="ghost" @click=${() => { void navigator.clipboard.writeText(status); toast(t("fb.copied")); }}>${icon("Copy", 14)} ${t("fb.copy_link")}</button>
            <a class="btn ghost" href=${status}>${t("fb.open_status")}</a>
            <button class="ghost" @click=${() => { this.done = undefined; this.files = []; }}>${t("fb.another")}</button></div>
        </div>`
      : !canReport ? html`<div class="card"><p>${!p.accepting ? t("fb.closed") : t("fb.signin_needed")}</p>
          ${p.accepting && !user ? html`<a class="btn" href=${`/login?next=${encodeURIComponent(location.pathname)}`}>${t("login.submit")}</a>` : nothing}</div>`
      : html`<form class="card" @submit=${this.submit}>
          ${p.intro ? html`<div class="intro">${p.intro}</div>` : nothing}
          ${p.types.length > 1 ? html`<div class="types" role="group" aria-label=${t("fb.type")}>${p.types.map((ty) => html`<button type="button" class="ghost" aria-pressed=${this.type === ty} @click=${() => (this.type = ty)}>${icon(TYPE_ICON[ty] ?? "MessageSquare", 14)} ${t(`fb.type.${ty}` as any)}</button>`)}</div>` : nothing}
          <label class="field">${t("fb.title")} *<input name="title" required maxlength="200" placeholder=${t(`fb.title_ph.${this.type}` as any)}></label>
          <label class="field">${t("fb.body")}<textarea name="body" rows="6" maxlength="20000" placeholder=${t(`fb.body_ph.${this.type}` as any)}></textarea></label>
          ${p.fields.filter((f) => !f.reportTypes?.length || f.reportTypes.includes(this.type)).map((f) => this.field(f))}
          ${p.attachments.maxFiles > 0 && (p.attachments.images || p.attachments.videos || p.attachments.logs || p.attachments.other) ? html`
            <div class="files">
              <label class="field">${t("fb.files", { n: p.attachments.maxFiles })}
                <input type="file" multiple accept=${this.accept()} ?disabled=${p.filesFull || this.files.length >= p.attachments.maxFiles} @change=${(e: Event) => { this.addFiles((e.target as HTMLInputElement).files); (e.target as HTMLInputElement).value = ""; }}></label>
              ${p.filesFull ? html`<p class="sub">${t("fb.files_full")}</p>` : nothing}
              ${this.files.map((f, i) => html`<div class="file">${icon("Paperclip", 14)}<span>${f.name}</span><small class="sub">${Math.ceil(f.size / 1024)} KB</small>
                <button type="button" class="ghost" aria-label=${t("fb.remove_file")} @click=${() => (this.files = this.files.filter((_, j) => j !== i))}>${icon("X", 13)}</button></div>`)}
            </div>` : nothing}
          ${!user && p.askEmail ? html`<label class="field">${t("fb.email")}<input name="email" type="email" autocomplete="email" placeholder=${t("fb.email_ph")}></label>` : nothing}
          ${user ? html`<p class="sub">${t("fb.as_user", { name: user.name })}</p>` : html`<phx-botcheck place="feedback"></phx-botcheck>`}
          ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : nothing}
          <div class="row"><button ?disabled=${this.busy}>${icon("Send", 14)} ${t("fb.send")}</button></div>
          <details><summary class="sub">${t("fb.privacy")}</summary><p class="privacy">${p.privacy}</p></details>
        </form>`}
      <p class="powered">${t("fb.powered")} <a href="https://phoedox.com" rel="noopener">Phoedox</a></p>
    </div>`;
  }
}
const TYPE_ICON: Record<string, string> = { bug: "Bug", idea: "Lightbulb", question: "CircleHelp", praise: "Heart" };
customElements.get("phx-feedback-portal") || customElements.define("phx-feedback-portal", PhxFeedbackPortal);

/** Status link of a report: /feedback/status/<token>. */
export class PhxFeedbackStatus extends LitElement {
  static properties = { token: {}, st: { state: true }, gone: { state: true } };
  static styles = [base, css`
    :host { display: block; min-height: 100vh; background: var(--bg); }
    .wrap { max-width: 640px; margin: 0 auto; padding: 32px 16px; }
    .card { background: var(--surface); border: 1px solid var(--line); border-radius: 16px; padding: 18px; display: grid; gap: 12px; }
    .state { display: inline-flex; gap: 6px; align-items: center; border-radius: 99px; padding: 3px 10px; background: var(--accent-soft); color: var(--accent); font-weight: 600; width: fit-content; }
    .state.done { background: color-mix(in srgb, var(--good) 15%, var(--surface)); color: var(--good); }
    .answer { border-left: 3px solid var(--accent); padding: 6px 12px; white-space: pre-wrap; }
    .muted { color: var(--muted); font-size: 13.5px; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; }
    .body { white-space: pre-wrap; }
  `];
  token = "";
  st?: Status;
  gone = false;

  async connectedCallback() {
    super.connectedCallback();
    this.st = await api<Status>("GET", `/feedback/status/${this.token}`).catch(() => { this.gone = true; return undefined; });
  }

  private async removeReport() {
    if (!confirm(t("fb.delete_confirm"))) return;
    try {
      await api("DELETE", `/feedback/status/${this.token}`);
      this.gone = true;
      this.st = undefined;
      toast(t("fb.deleted"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private reply = async (e: SubmitEvent) => {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const body = String(new FormData(form).get("body") ?? "").trim();
    if (!body) return;
    try {
      this.st = await api<Status>("POST", `/feedback/status/${this.token}/reply`, { body });
      form.reset();
      toast(t("fb.reply_sent"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  };

  render() {
    if (this.gone) return html`<div class="wrap"><div class="card"><h1 style="margin:0;font-size:20px">${t("fb.status_gone")}</h1><p class="muted">${t("fb.status_gone_hint")}</p></div></div>`;
    const s = this.st;
    if (!s) return html`<div class="wrap"><p class="muted">${t("app.loading")}</p></div>`;
    const date = new Date(s.createdAt).toLocaleDateString(locale(), { year: "numeric", month: "long", day: "numeric" });
    return html`<div class="wrap"><div class="card">
      <div class="muted">${s.portal} · ${t(`fb.type.${s.type}` as any)} · ${date}</div>
      <h1 style="margin:0;font-size:22px">${s.title}</h1>
      <span class=${s.finished ? "state done" : "state"}>${icon(s.finished ? "CircleCheck" : "Clock", 14)} ${stateLabel(s.state)}</span>
      ${s.reports > 1 ? html`<p class="muted">${t("fb.others", { n: s.reports - 1 })}</p>` : nothing}
      ${s.fixedIn ? html`<p><strong>${t("fb.fixed_in_status", { version: s.fixedIn })}</strong></p>` : nothing}
      ${s.answer ? html`<div><div class="muted">${t(s.answerAuto ? "fb.auto_answer" : "fb.team_answer")}</div><div class="answer">${s.answer}</div></div>` : nothing}
      ${s.question && !s.finished ? html`<div><div class="muted">${t("fb.team_question")}</div><div class="answer">${s.question}</div>
        ${(s.replies ?? []).map((r) => html`<p class="body" style="margin:8px 0 0"><span class="muted">${t("fb.your_reply")}</span><br>${r.body}</p>`)}
        <form @submit=${this.reply} style="display:grid;gap:8px;margin-top:8px">
          <textarea name="body" rows="3" required maxlength="4000" aria-label=${t("fb.reply")} placeholder=${t("fb.reply_ph")}></textarea>
          <div class="row"><button type="submit">${icon("Send", 14)} ${t("fb.reply_send")}</button></div></form></div>` : nothing}
      ${s.body ? html`<details><summary class="muted">${t("fb.your_text")}</summary><p class="body">${s.body}</p></details>` : nothing}
      <div class="row">
        <a class="btn ghost" href=${`/api/v1/feedback/status/${this.token}/export`} download>${icon("Download", 14)} ${t("fb.export")}</a>
        <button class="ghost danger" @click=${this.removeReport}>${icon("Trash2", 14)} ${t("fb.delete")}</button>
        <a class="btn ghost" href=${`/feedback/${s.handle}`}>${t("fb.new_report")}</a>
      </div>
      <p class="muted">${t("fb.delete_hint")}</p>
    </div></div>`;
  }
}
customElements.get("phx-feedback-status") || customElements.define("phx-feedback-status", PhxFeedbackStatus);

/** A topic state as text: built-in states are translated, own ones shown as written. */
export function stateLabel(s: string) {
  const k = `fb.state.${s}`;
  const v = t(k as any);
  return v === k ? s : v;
}
