// Apps like artifacts, around the frame: the gallery of a project's apps,
// copying an app (also from a share link), the person's answers about
// device features, and the app's assets in the editor.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api, apiRaw, type Doc, type Project } from "../api";
import { formatDate, t, type Key } from "../i18n";
import { icon } from "../icon";
import { currentUser, navigate } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";
import type { AppDevice } from "./apps";

interface AppCard { id: string; slug: string; title: string; category?: string; updatedAt: string; preview: boolean; previewAt?: string; stale?: boolean; shared: boolean; level: number }

/** The project's apps as cards with their preview pictures. */
export class PhxAppGallery extends LitElement {
  static properties = { project: {}, apps: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    h1 { font-size: 24px; margin-bottom: 4px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 14px; margin-top: 16px; }
    a.card { display: grid; grid-template-rows: auto auto; border: 1px solid var(--line); border-radius: var(--radius-card, 12px); overflow: hidden; background: var(--surface); color: var(--fg); text-decoration: none;
      transition: box-shadow 160ms ease, transform 160ms cubic-bezier(0.23, 1, 0.32, 1); }
    @media (hover: hover) and (pointer: fine) { a.card:hover { box-shadow: 0 10px 28px -16px rgb(0 0 0 / .4); transform: translateY(-2px); } }
    @media (prefers-reduced-motion: reduce) { a.card { transition: box-shadow 160ms ease; } a.card:hover { transform: none; } }
    .pic { aspect-ratio: 16 / 10; background: var(--surface2); display: grid; place-items: center; color: var(--muted); overflow: hidden; }
    .pic img { width: 100%; height: 100%; object-fit: cover; object-position: top; display: block; }
    .meta { padding: 10px 12px; display: grid; gap: 4px; }
    .meta b { font-size: 15px; }
    .meta span { color: var(--muted); font-size: 12.5px; display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }
    .empty { color: var(--muted); padding: 32px 0; }
  `];
  project = "";
  apps?: AppCard[];
  error = "";

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("project") && this.project) {
      api<AppCard[]>("GET", `/projects/${this.project}/app-gallery`).then((l) => (this.apps = l)).catch((e) => (this.error = e instanceof ApiError ? e.text : t("error.unknown")));
    }
  }

  render() {
    return html`<h1>${t("appgal.title")}</h1>
      <p class="muted">${t("appgal.intro")}</p>
      ${this.error ? html`<p class="error">${this.error}</p>` : nothing}
      ${this.apps && !this.apps.length ? html`<p class="empty">${t("appgal.empty")}</p>` : nothing}
      <div class="grid">${(this.apps ?? []).map((a) => html`<a class="card" href="/p/${this.project}/d/${a.slug}">
        <div class="pic">${a.preview ? html`<img loading="lazy" alt="" src="/api/v1/projects/${this.project}/apps/${a.id}/preview.png?t=${encodeURIComponent(a.previewAt ?? "")}">` : icon("AppWindow", 32)}</div>
        <div class="meta"><b>${a.title}</b><span>${a.category ? html`${a.category} · ` : nothing}${formatDate(a.updatedAt)}
          ${a.shared ? html`<span class="chip">${icon("Globe", 11)} ${t("appshare.public")}</span>` : nothing}
          ${a.stale ? html`<span title=${t("appgal.stale_help")}>${icon("Clock", 11)}</span>` : nothing}</span></div>
      </a>`)}</div>`;
  }
}
customElements.get("phx-app-gallery") || customElements.define("phx-app-gallery", PhxAppGallery);

/** "Make a copy": into a project where the person may write (from an app or a share link). */
export class PhxAppCopy extends LitElement {
  static properties = { project: {}, doc: {}, token: {}, appTitle: {}, projects: { state: true }, target: { state: true }, busy: { state: true } };
  static styles = [base, css`
    dialog { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); padding: 20px; width: min(460px, 92vw); box-shadow: 0 18px 50px rgb(0 0 0 / .2); }
    dialog::backdrop { background: rgb(0 0 0 / .35); }
    dialog h2 { font-size: 18px; margin-bottom: 8px; }
    label { display: grid; gap: 4px; margin: 10px 0; font-size: 14px; }
    .row { justify-content: flex-end; }
  `];
  project = "";
  doc = "";
  /** A public app's address (copy from a share link). */
  token = "";
  appTitle = "";
  projects: Project[] = [];
  target = "";
  busy = false;

  async open() {
    this.projects = (await api<Project[]>("GET", "/projects").catch(() => [] as Project[])).filter((p) => p.role === "owner" || p.role === "editor");
    this.target = this.projects.find((p) => p.slug === this.project)?.slug ?? this.projects[0]?.slug ?? "";
    await this.updateComplete;
    this.renderRoot.querySelector("dialog")?.showModal();
  }

  private async copy() {
    if (!this.target) return;
    this.busy = true;
    try {
      const name = (this.renderRoot.querySelector("input") as HTMLInputElement | null)?.value.trim() ?? "";
      const res = this.token
        ? await api<{ document?: Doc }>("POST", `/app-shares/${encodeURIComponent(this.token)}/copy`, { project: this.target, title: name })
        : await api<{ document?: Doc }>("POST", `/projects/${this.project}/apps/${encodeURIComponent(this.doc)}/copy`, { project: this.target, title: name });
      this.renderRoot.querySelector("dialog")?.close();
      toast(t("appcopy.done"));
      if (res.document) navigate(`/p/${this.target}/d/${res.document.slug}`);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    return html`<dialog aria-labelledby="cpt" @close=${() => (this.busy = false)}>
      <h2 id="cpt">${t("appcopy.title")}</h2>
      <p class="muted">${t(this.token ? "appcopy.text_shared" : "appcopy.text")}</p>
      ${this.projects.length ? html`<label>${t("appcopy.project")}<select @change=${(e: Event) => (this.target = (e.target as HTMLSelectElement).value)}>
        ${this.projects.map((p) => html`<option value=${p.slug} ?selected=${p.slug === this.target}>${p.name}</option>`)}</select></label>
        <label>${t("appcopy.name")}<input .value=${this.appTitle}></label>` : html`<p class="error">${t("appcopy.no_project")}</p>`}
      <div class="row"><button class="ghost" @click=${() => this.renderRoot.querySelector("dialog")?.close()}>${t("doc.cancel")}</button>
        <button ?disabled=${this.busy || !this.target} @click=${() => void this.copy()}>${icon("Copy", 14)} ${t("appcopy.button")}</button></div>
    </dialog>`;
  }
}
customElements.get("phx-app-copy") || customElements.define("phx-app-copy", PhxAppCopy);

/** Whether the shared page offers a copy: signed-in people. */
export const signedIn = () => !!currentUser.get();

/** The person's answers about an app's device features; each can be changed or forgotten. */
export class PhxAppDevices extends LitElement {
  static properties = { project: {}, doc: {}, devices: { attribute: false }, grants: { attribute: false } };
  static styles = [base, css`
    dialog { border: 1px solid var(--line); border-radius: var(--radius-card, 12px); background: var(--surface); color: var(--fg); padding: 20px; width: min(480px, 92vw); box-shadow: 0 18px 50px rgb(0 0 0 / .2); }
    dialog::backdrop { background: rgb(0 0 0 / .35); }
    dialog h2 { font-size: 18px; margin-bottom: 8px; }
    ul { list-style: none; padding: 0; margin: 12px 0; display: grid; gap: 8px; }
    li { display: grid; grid-template-columns: 1fr auto; gap: 8px; align-items: center; padding: 8px 10px; border: 1px solid var(--line); border-radius: 10px; }
    li b { font-size: 14px; } li span { color: var(--muted); font-size: 12.5px; display: block; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { height: 30px; padding: 0 10px; border: 0; border-radius: 0; background: var(--surface); color: var(--muted); font-size: 12.5px; }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
    .row { justify-content: flex-end; }
  `];
  project = "";
  doc = "";
  devices: AppDevice[] = [];
  grants: Partial<Record<AppDevice, boolean>> = {};

  open() {
    this.renderRoot.querySelector("dialog")?.showModal();
  }

  private async set(d: AppDevice, v: "yes" | "no" | "ask") {
    try {
      this.grants = await api<Partial<Record<AppDevice, boolean>>>("PUT", `/projects/${this.project}/apps/${encodeURIComponent(this.doc)}/devices/${d}`, { allowed: v === "yes", forget: v === "ask" });
      this.dispatchEvent(new CustomEvent("devices", { detail: this.grants, bubbles: true, composed: true }));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    return html`<dialog aria-labelledby="dvt">
      <h2 id="dvt">${t("app.devices_title")}</h2>
      <p class="muted">${t("app.devices_text")}</p>
      <ul>${this.devices.map((d) => {
        const v = this.grants[d] === undefined ? "ask" : this.grants[d] ? "yes" : "no";
        return html`<li><div><b>${t(`app.device_${d}` as Key)}</b><span>${t(`app.device_state_${v}` as Key)}</span></div>
          <div class="seg" role="group" aria-label=${t(`app.device_${d}` as Key)}>${(["yes", "no", "ask"] as const).map((x) => html`<button aria-pressed=${v === x ? "true" : "false"} @click=${() => void this.set(d, x)}>${t(`app.device_set_${x}` as Key)}</button>`)}</div></li>`;
      })}</ul>
      <div class="row"><button @click=${() => { this.renderRoot.querySelector("dialog")?.close(); location.reload(); }}>${t("app.devices_done")}</button></div>
    </dialog>`;
  }
}
customElements.get("phx-app-devices") || customElements.define("phx-app-devices", PhxAppDevices);

interface AppAsset { id: string; name: string; mime: string; size: number; path: string }

/** The app's assets in the editor: upload, delete, copy from another app. */
export class PhxAppAssets extends LitElement {
  static properties = { project: {}, doc: {}, assets: { state: true }, apps: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: grid; gap: 8px; }
    ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 6px; }
    li { display: grid; grid-template-columns: auto 1fr auto auto; gap: 8px; align-items: center; font-size: 13px; }
    li code { font-size: 12px; }
    li .sz { color: var(--muted); font-size: 12px; }
    .row { gap: 8px; flex-wrap: wrap; }
    input[type=file] { display: none; }
    select { max-width: 220px; }
  `];
  project = "";
  doc = "";
  assets: AppAsset[] = [];
  apps: Doc[] = [];
  busy = false;

  willUpdate(ch: Map<string, unknown>) {
    if ((ch.has("doc") || ch.has("project")) && this.doc) void this.load();
  }

  private base() {
    return `/projects/${this.project}/apps/${encodeURIComponent(this.doc)}/assets`;
  }

  private async load() {
    this.assets = await api<AppAsset[]>("GET", this.base()).catch(() => []);
    this.apps = (await api<Doc[]>("GET", `/projects/${this.project}/documents?type=app`).catch(() => [] as Doc[])).filter((d) => d.id !== this.doc && d.slug !== this.doc);
  }

  private async upload(files: FileList | null) {
    if (!files?.length) return;
    this.busy = true;
    try {
      for (const f of Array.from(files)) await apiRaw("POST", `${this.base()}?name=${encodeURIComponent(f.name)}`, f);
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeAsset(a: AppAsset) {
    if (!confirm(t("appasset.remove", { name: a.name }))) return;
    await api("DELETE", `${this.base()}/${encodeURIComponent(a.name)}`).catch((e) => toast(e instanceof ApiError ? e.text : t("error.unknown"), "error"));
    await this.load();
  }

  private async copyFrom(slug: string) {
    if (!slug) return;
    try {
      const l = await api<AppAsset[]>("POST", `${this.base()}/copy`, { document: slug });
      toast(t("appasset.copied", { count: l.length }));
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const kb = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB`);
    return html`<p class="muted" style="margin:0;font-size:13px">${t("appasset.help")}</p>
      ${this.assets.length ? html`<ul>${this.assets.map((a) => html`<li>${icon(a.mime.startsWith("image/") ? "Image" : a.mime.startsWith("video/") ? "Video" : "FileText", 14)}
        <code>${a.path}</code><span class="sz">${kb(a.size)}</span>
        <button class="ghost danger" aria-label=${t("doc.delete")} @click=${() => void this.removeAsset(a)}>${icon("Trash2", 13)}</button></li>`)}</ul>` : html`<p class="muted" style="margin:0;font-size:13px">${t("appasset.none")}</p>`}
      <div class="row">
        <label class="btn ghost">${icon("Upload", 14)} ${t("appasset.upload")}<input type="file" multiple ?disabled=${this.busy} @change=${(e: Event) => void this.upload((e.target as HTMLInputElement).files)}></label>
        ${this.apps.length ? html`<select aria-label=${t("appasset.copy_from")} @change=${(e: Event) => void this.copyFrom((e.target as HTMLSelectElement).value)}>
          <option value="">${t("appasset.copy_from")}</option>${this.apps.map((d) => html`<option value=${d.slug}>${d.title}</option>`)}</select>` : nothing}
      </div>`;
  }
}
customElements.get("phx-app-assets") || customElements.define("phx-app-assets", PhxAppAssets);
