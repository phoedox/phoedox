import { LitElement, css, html } from "lit";
import { ApiError, api, shareURL, type Doc, type DocSharing, type Group, type OrgDetail, type Project, type ShareLink } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

/** Who sees a page: restrict it, share it with people or groups, make a read-only link. */
export class PhxDocShare extends LitElement {
  static properties = { project: { attribute: false }, doc: { attribute: false }, sharing: { state: true }, groups: { state: true }, links: { state: true }, created: { state: true } };
  static styles = [base, css`
    dialog { width: min(560px, calc(100vw - 32px)); padding: 0; border: 1px solid var(--line); border-radius: 14px; background: var(--surface); color: var(--fg);
      box-shadow: 0 24px 80px rgb(0 0 0 / .25); transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out), overlay var(--dur) allow-discrete, display var(--dur) allow-discrete;
      @starting-style { opacity: 0; transform: scale(.97); } }
    dialog:not([open]) { opacity: 0; transform: scale(.97); }
    dialog::backdrop { background: rgb(0 0 0 / .4); }
    header { display: flex; align-items: center; gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
    header h2 { font-size: 17px; margin-right: auto; }
    .body { padding: 14px 16px 18px; display: grid; gap: 16px; }
    h3 { font-size: 12px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); font-family: var(--font-body); margin-bottom: 6px; }
    label.switch { display: flex; gap: 10px; align-items: flex-start; font-size: 14px; }
    label.switch input { width: auto; margin-top: 3px; }
    ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
    li { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    li .who { margin-right: auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
    li button { padding: 3px 8px; }
    form.row input { flex: 1; min-width: 160px; }
    form.row select { width: auto; }
    code { word-break: break-all; font-size: 12px; }
    @media (prefers-reduced-motion: reduce) { dialog, dialog:not([open]) { transform: none; @starting-style { opacity: 0; transform: none; } } }
  `];
  project?: Project;
  doc?: Doc;
  sharing?: DocSharing;
  groups: Group[] = [];
  links: ShareLink[] = [];
  created?: ShareLink;

  private get owner() {
    return this.project?.role === "owner";
  }

  async open() {
    this.created = undefined;
    await this.updateComplete;
    this.renderRoot.querySelector("dialog")!.showModal();
    await this.load();
  }

  private async load() {
    const p = this.project!.slug, d = this.doc!.slug;
    if (this.owner) {
      this.sharing = await api<DocSharing>("GET", `/projects/${p}/documents/${d}/sharing`).catch(() => undefined);
      if (this.project!.orgId && !this.groups.length) {
        this.groups = (await api<OrgDetail>("GET", `/orgs/${this.project!.orgId}`).catch(() => undefined))?.groups ?? [];
      }
    }
    const all = (await api<ShareLink[]>("GET", `/projects/${p}/share-links`).catch(() => [])) ?? [];
    this.links = all.filter((l) => l.documentId === this.doc!.id);
  }

  private async call<T>(fn: () => Promise<T>): Promise<T | undefined> {
    try {
      return await fn();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return undefined;
    }
  }

  private base() {
    return `/projects/${this.project!.slug}/documents/${this.doc!.slug}/sharing`;
  }

  private async grant(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const who = String(f.get("who") ?? "").trim();
    const body = who.startsWith("group:") ? { group: who.slice(6), role: f.get("role") } : { email: who, role: f.get("role") };
    const s = await this.call(() => api<DocSharing>("POST", `${this.base()}/grants`, body));
    if (s) {
      this.sharing = s;
      form.reset();
    }
  }

  private async link(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const l = await this.call(() => api<ShareLink>("POST", `/projects/${this.project!.slug}/share-links`, { document: this.doc!.id, days: Number(f.get("days") || 0) }));
    if (l) {
      this.created = l;
      await this.load();
    }
  }

  render() {
    const s = this.sharing;
    return html`<dialog aria-labelledby="t" @click=${(e: MouseEvent) => e.target === e.currentTarget && (e.currentTarget as HTMLDialogElement).close()}>
      <header><h2 id="t">${t("share.dialog_title", { title: this.doc?.title ?? "" })}</h2>
        <button class="ghost" aria-label=${t("files.close")} @click=${() => this.renderRoot.querySelector("dialog")!.close()}>${icon("X", 16)}</button></header>
      <div class="body">
        ${this.owner && s ? html`
          <div><h3>${t("share.access")}</h3>
            <label class="switch"><input type="checkbox" .checked=${s.restricted} @change=${async (e: Event) => {
              const on = (e.target as HTMLInputElement).checked;
              const r = await this.call(() => api<DocSharing>("PUT", this.base(), { restricted: on }));
              if (r) this.sharing = r;
              this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
            }}><span><b>${icon("Lock", 13)} ${t("share.restrict")}</b><br><span class="muted" style="font-size:13px">${t("share.restrict_hint")}</span></span></label></div>
          <div><h3>${t("share.people")}</h3>
            ${s.grants.length ? html`<ul>${s.grants.map((g) => html`<li>${icon(g.groupId ? "Users" : "UserRound", 14)}
              <span class="who">${g.name}${g.email ? html` <span class="muted">${g.email}</span>` : ""}</span>
              <span class="chip">${t(`settings.role.${g.role}` as any)}</span>
              <button class="ghost" aria-label=${t("share.unshare", { name: g.name })} @click=${async () => { const r = await this.call(() => api<DocSharing>("DELETE", `${this.base()}/grants/${g.id}`)); if (r) this.sharing = r; }}>${icon("X", 13)}</button></li>`)}</ul>`
              : html`<p class="muted" style="margin:0;font-size:13px">${t("share.nobody")}</p>`}
            <form class="row" style="margin-top:8px" @submit=${this.grant}>
              <input name="who" required list="share-groups" placeholder=${t("share.who")} aria-label=${t("share.who")}>
              <datalist id="share-groups">${this.groups.map((g) => html`<option value="group:${g.id}">${t("share.group", { name: g.name })}</option>`)}</datalist>
              <select name="role"><option value="reader">${t("settings.role.reader")}</option><option value="editor">${t("settings.role.editor")}</option></select>
              <button>${t("share.add")}</button></form>
            <p class="muted" style="margin:6px 0 0;font-size:12px">${t("share.people_hint")}</p></div>` : ""}
        <div><h3>${t("share.link")}</h3>
          ${this.created?.token ? html`<p style="margin:0 0 8px"><code>${shareURL(this.created.token)}</code>
            <button class="ghost" style="padding:3px 8px" @click=${() => navigator.clipboard?.writeText(shareURL(this.created!.token!)).then(() => toast(t("tokens.copied")))}>${icon("Copy", 13)} ${t("tokens.copy")}</button></p>` : ""}
          ${this.links.length ? html`<ul>${this.links.map((l) => html`<li>${icon("Globe", 14)}<span class="who muted" style="font-size:13px">
            ${l.expiresAt ? t("share.until", { date: formatDate(l.expiresAt) }) : t("share.no_expiry")} · ${l.lastUsedAt ? t("share.used", { date: formatDate(l.lastUsedAt) }) : t("tokens.never_used")}</span>
            <button class="danger" @click=${async () => { await this.call(() => api("DELETE", `/projects/${this.project!.slug}/share-links/${l.id}`)); await this.load(); }}>${t("share.revoke")}</button></li>`)}</ul>` : ""}
          ${this.doc?.restricted && !this.owner ? html`<p class="muted" style="font-size:13px">${t("share.link_owner_only")}</p>` : html`<form class="row" style="margin-top:8px" @submit=${this.link}>
            <select name="days" aria-label=${t("share.validity")}><option value="7">${t("share.days", { days: 7 })}</option><option value="30">${t("share.days", { days: 30 })}</option><option value="0">${t("share.no_expiry")}</option></select>
            <button class="ghost">${icon("Share2", 13)} ${t("share.create_link")}</button></form>
            <p class="muted" style="margin:6px 0 0;font-size:12px">${t("share.link_hint")}</p>`}
        </div>
      </div>
    </dialog>`;
  }
}
customElements.define("phx-doc-share", PhxDocShare);
