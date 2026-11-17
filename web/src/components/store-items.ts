// Themes and app templates from the store: install, update notices,
// rollback to compatible versions, requests where the organization asks for
// them, and the organization's store access.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { navigate } from "../state";
import { loadTheme } from "../theme-runtime";
import { scard, settingsUI, srow, sseg, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

export interface StoreInstall {
  id: string;
  name: string;
  scope?: string;
  owner?: string;
  version: string;
  update?: string;
  canEdit: boolean;
}
export interface StoreItem {
  type: string;
  id: string;
  name: string;
  version: string;
  summary: string;
  author: string;
  verified: boolean;
  authorDomain?: string;
  icon?: string;
  installedIn: StoreInstall[];
}
interface StoreItems {
  items: StoreItem[];
  access: "yes" | "request" | "no";
  off?: boolean;
}
interface Version {
  version: string;
  channel?: string;
}
export interface StoreRequest {
  id: string;
  projectSlug?: string;
  projectName?: string;
  itemType: "theme" | "app";
  itemId: string;
  itemName: string;
  version: string;
  title?: string;
  note?: string;
  status: "pending" | "approved" | "rejected";
  answer?: string;
  result?: string;
  userName?: string;
  createdAt: string;
}

const errText = (e: unknown) => (e instanceof ApiError ? e.text : t("error.unknown"));

function activeProjectSlug() {
  const m = location.pathname.match(/^\/p\/([^/]+)/);
  return m ? decodeURIComponent(m[1]) : "";
}

const listStyles = css`
  :host { display: block; }
  .items { display: grid; gap: 0; }
  .it { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 12px; align-items: start; padding: 12px 18px; border-top: 1px solid var(--line); }
  .it .bx { width: 34px; height: 34px; border-radius: 9px; display: grid; place-items: center; background: var(--surface2); color: var(--muted); }
  .it b { font-weight: 500; overflow-wrap: anywhere; }
  .it .by { color: var(--muted); font-size: 13px; display: flex; gap: 4px; align-items: center; flex-wrap: wrap; }
  .it .sum { margin: 2px 0 0; font-size: 13.5px; overflow-wrap: anywhere; }
  .it .inst { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
  .chip { display: inline-flex; gap: 6px; align-items: center; padding: 3px 8px; border-radius: 999px; background: var(--surface2); font-size: 12.5px; }
  .chip.up { background: var(--accent-soft); }
  .chip button { padding: 1px 6px; font-size: 12px; white-space: nowrap; }
  .chip select { padding: 1px 4px; font-size: 12px; width: auto; min-width: 0; }
  .acts { display: flex; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }
  .empty, .off { color: var(--muted); font-size: 13.5px; padding: 0 18px 16px; margin: 0; }
  .note { display: flex; gap: 8px; align-items: flex-start; margin: 0 18px 12px; padding: 8px 12px; border-radius: 10px; background: var(--surface2); font-size: 13px; }
  select { font: inherit; padding: 3px 6px; width: auto; }
  @media (max-width: 560px) { .it { grid-template-columns: minmax(0, 1fr); } .it .bx { display: none; } .acts { justify-content: flex-start; } }
`;

/** Shared parts: loading the list, versions for rollbacks, requests. */
abstract class StoreList extends LitElement {
  static properties = { data: { state: true }, busy: { state: true }, versions: { state: true } };
  data?: StoreItems;
  busy = false;
  /** Versions per item id, loaded when someone opens the rollback list. */
  versions: Record<string, Version[]> = {};

  protected abstract query(): string;

  protected async load() {
    this.data = await api<StoreItems>("GET", `/store/items?${this.query()}`).catch(() => ({ items: [], access: "no" as const, off: true }));
  }

  protected async loadVersions(id: string) {
    if (this.versions[id]) return;
    const vs = await api<Version[]>("GET", `/store/items/${encodeURIComponent(id)}/versions`).catch(() => []);
    this.versions = { ...this.versions, [id]: vs };
  }

  protected async run(fn: () => Promise<unknown>, done: Key) {
    this.busy = true;
    try {
      await fn();
      toast(t(done));
      await this.load();
      this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
    } catch (e) {
      toast(errText(e), "error");
    } finally {
      this.busy = false;
    }
  }

  protected async request(project: string, it: StoreItem, title = "") {
    const note = prompt(t("store.request_note", { name: it.name }));
    if (note === null) return;
    await this.run(() => api("POST", `/projects/${project}/store/requests`, { type: it.type, id: it.id, version: it.version, name: it.name, title, note }), "store.requested");
  }

  protected by(it: StoreItem) {
    return html`<span class="by">${it.author}${it.verified ? html` ${icon("BadgeCheck", 13)} ${it.authorDomain ?? ""}` : nothing} · v${it.version}</span>`;
  }

  /** The rollback list of one installed copy. */
  protected rollback(it: StoreItem, inst: StoreInstall, go: (version: string) => void) {
    const vs = this.versions[it.id];
    if (!vs) return html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.loadVersions(it.id)}>${icon("History", 12)} ${t("store.versions")}</button>`;
    return html`<select aria-label=${t("store.versions")} ?disabled=${this.busy} @change=${(e: Event) => {
      const v = (e.target as HTMLSelectElement).value;
      if (v && v !== inst.version && confirm(t("store.rollback_confirm", { name: inst.name, version: v }))) go(v);
    }}>${vs.map((v) => html`<option value=${v.version} ?selected=${v.version === inst.version}>v${v.version}${v.channel === "beta" ? " (beta)" : ""}</option>`)}</select>`;
  }
}

/** Store themes for one place: the person (scope user), a project, an
 *  organization or the instance. */
export class PhxStoreThemes extends StoreList {
  static properties = { ...StoreList.properties, scope: {}, owner: {} };
  static styles = [base, settingsUI, listStyles];
  scope: "user" | "project" | "org" | "instance" = "user";
  owner = "";

  private loadedFor = "";

  willUpdate() {
    const key = `${this.scope}:${this.owner}`;
    if (key !== this.loadedFor) {
      this.loadedFor = key;
      void this.load();
    }
  }

  protected query() {
    return `type=theme${this.scope === "project" && this.owner ? `&project=${encodeURIComponent(this.owner)}` : ""}`;
  }

  private here(it: StoreItem) {
    return it.installedIn.filter((x) => x.scope === this.scope && (this.scope === "user" || this.scope === "instance" || x.owner === this.owner));
  }

  private install(it: StoreItem, version = it.version, theme = "") {
    return this.run(async () => {
      await api("POST", "/store/themes", { id: it.id, version, scope: this.scope, owner: this.owner, theme });
      await loadTheme(activeProjectSlug(), true);
    }, theme ? "store.updated" : "store.installed");
  }

  render() {
    const d = this.data;
    // Without a store on this instance the card stays away.
    if (!d || d.off) return nothing;
    const body = !d.items.length ? html`<p class="empty">${t("store.no_themes")}</p>`
      : html`${d.access === "request" ? html`<p class="note">${icon("Info", 15)}<span>${t("store.request_hint")}</span></p>` : nothing}
        <div class="items">${d.items.map((it) => {
          const here = this.here(it);
          return html`<div class="it">
            <span class="bx">${icon("Palette", 16)}</span>
            <div><b>${it.name}</b> ${this.by(it)}${it.summary ? html`<p class="sum">${it.summary}</p>` : nothing}
              ${here.length ? html`<div class="inst">${here.map((x) => html`<span class="chip ${x.update ? "up" : ""}">${icon("Check", 12)} v${x.version}
                ${x.update && x.canEdit ? html`<button ?disabled=${this.busy} @click=${() => this.install(it, x.update, x.id)}>${t("store.update_to", { version: x.update })}</button>` : nothing}
                ${x.canEdit ? this.rollback(it, x, (v) => this.install(it, v, x.id)) : nothing}</span>`)}</div>` : nothing}</div>
            <div class="acts">${here.length ? nothing
              : d.access === "yes" ? html`<button ?disabled=${this.busy} @click=${() => this.install(it)}>${icon("Download", 14)} ${t("store.install")}</button>`
              : d.access === "request" ? html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.request(this.owner, it)}>${icon("Send", 14)} ${t("store.request")}</button>`
              : html`<span class="by">${icon("Lock", 13)} ${t("store.no_access")}</span>`}</div>
          </div>`;
        })}</div>`;
    return scard(t("store.themes_title"), t("store.themes_text"), body);
  }
}

/** App templates of the store for a project (create dialog). */
export class PhxStoreApps extends StoreList {
  static properties = { ...StoreList.properties, project: {}, category: {}, local: { state: true } };
  /** App templates of this project. */
  local: { id: string; document: string; title: string }[] = [];
  static styles = [base, settingsUI, listStyles, css`.it { padding: 10px 0; } .note, .empty, .off { margin: 0 0 10px; padding-left: 0; padding-right: 0; }
    h3.sub { font-size: 13px; color: var(--muted); font-weight: 600; margin: 10px 0 2px; text-transform: uppercase; letter-spacing: .04em; }`];
  project = "";
  category = "";

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("project") && this.project) {
      void this.load();
      void api<{ id: string; document: string; title: string }[]>("GET", `/projects/${this.project}/app-templates`).then((l) => (this.local = l)).catch(() => undefined);
    }
  }

  protected query() {
    return `type=app&project=${encodeURIComponent(this.project)}`;
  }

  private async copy(tpl: { id: string; title: string }) {
    this.busy = true;
    try {
      const r = await api<{ document: { slug: string } }>("POST", `/projects/${this.project}/apps/${tpl.id}/copy`, { category: this.category });
      this.dispatchEvent(new CustomEvent("done", { bubbles: true, composed: true }));
      navigate(`/p/${this.project}/d/${r.document.slug}`);
    } catch (e) {
      toast(errText(e), "error");
    } finally {
      this.busy = false;
    }
  }

  private localList() {
    if (!this.local.length) return nothing;
    return html`<h3 class="sub">${t("store.project_templates")}</h3><div class="items">${this.local.map((tpl) => html`<div class="it">
      <span class="bx">${icon("Copy", 16)}</span><div><b>${tpl.title}</b></div>
      <div class="acts"><button ?disabled=${this.busy} @click=${() => this.copy(tpl)}>${icon("Plus", 14)} ${t("store.use_template")}</button></div></div>`)}</div>
      <h3 class="sub">${t("store.from_store_title")}</h3>`;
  }

  private async create(it: StoreItem) {
    this.busy = true;
    try {
      const r = await api<{ document: { slug: string } }>("POST", `/projects/${this.project}/store/apps`, { id: it.id, version: it.version, category: this.category });
      toast(t("store.installed"));
      this.dispatchEvent(new CustomEvent("done", { bubbles: true, composed: true }));
      navigate(`/p/${this.project}/d/${r.document.slug}`);
    } catch (e) {
      toast(errText(e), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const d = this.data;
    if (!d) return html`${this.localList()}<p class="empty">${t("store.loading")}</p>`;
    if (d.off) return html`${this.localList()}<p class="off">${t("store.off")}</p>`;
    if (!d.items.length) return html`${this.localList()}<p class="empty">${t("store.no_apps")}</p>`;
    return html`${this.localList()}${d.access === "request" ? html`<p class="note">${icon("Info", 15)}<span>${t("store.request_hint")}</span></p>` : nothing}
      <div class="items">${d.items.map((it) => html`<div class="it">
        <span class="bx">${icon("AppWindow", 16)}</span>
        <div><b>${it.name}</b> ${this.by(it)}${it.summary ? html`<p class="sum">${it.summary}</p>` : nothing}
          ${it.installedIn.length ? html`<div class="inst"><span class="chip">${icon("Check", 12)} ${t("store.in_project", { n: it.installedIn.length })}</span></div>` : nothing}</div>
        <div class="acts">${d.access === "yes" ? html`<button ?disabled=${this.busy} @click=${() => this.create(it)}>${icon("Plus", 14)} ${t("store.use_template")}</button>`
          : d.access === "request" ? html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.request(this.project, it)}>${icon("Send", 14)} ${t("store.request")}</button>`
          : html`<span class="by">${icon("Lock", 13)} ${t("store.no_access")}</span>`}</div>
      </div>`)}</div>`;
  }
}

/** The update notice on an app made from a store template. */
export class PhxAppOrigin extends StoreList {
  static properties = { ...StoreList.properties, project: {}, doc: {}, origin: { attribute: false }, canEdit: { type: Boolean } };
  static styles = [base, listStyles, css`
    :host { display: block; }
    .bar { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; padding: 6px 10px; border-radius: 10px; background: var(--surface2); font-size: 13px; color: var(--muted); }
    .bar.up { background: var(--accent-soft); color: var(--fg); }
    .bar button { padding: 2px 8px; font-size: 12.5px; }
  `];
  project = "";
  doc = "";
  origin?: { id: string; version: string; name?: string };
  canEdit = false;

  willUpdate(ch: Map<string, unknown>) {
    if ((ch.has("project") || ch.has("origin")) && this.project && this.origin) void this.load();
  }

  protected query() {
    return `type=app&project=${encodeURIComponent(this.project)}`;
  }

  private updateTo(version: string) {
    if (!this.origin || !confirm(t("store.app_update_confirm", { version }))) return;
    const id = this.origin.id;
    void this.run(async () => {
      await api("POST", `/projects/${this.project}/store/apps`, { id, version, document: this.doc });
      this.dispatchEvent(new CustomEvent("reload", { bubbles: true, composed: true }));
    }, "store.updated");
  }

  render() {
    const o = this.origin;
    if (!o) return nothing;
    const it = this.data?.items.find((x) => x.id === o.id);
    const inst = it?.installedIn.find((x) => x.id === this.doc);
    const newer = inst?.update;
    return html`<div class="bar ${newer ? "up" : ""}">${icon("Store", 14)}
      <span>${t("store.app_from", { name: o.name || o.id, version: o.version })}</span>
      ${newer && this.canEdit ? html`<button ?disabled=${this.busy} @click=${() => this.updateTo(newer)}>${t("store.update_to", { version: newer })}</button>` : nothing}
      ${it && inst && this.canEdit ? this.rollback(it, inst, (v) => this.updateTo(v)) : nothing}</div>`;
  }
}

interface Rule { who: "" | "all" | "groups" | "none"; groups?: string[] }
interface Access { themes: Rule; apps: Rule; noRequests?: boolean }

/** Organization settings: who may install themes and app templates from the
 *  store, requests on or off, and the requests to answer. */
export class PhxOrgStore extends LitElement {
  static properties = { org: {}, groups: { attribute: false }, acc: { state: true }, reqs: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .groups { display: flex; flex-wrap: wrap; gap: 6px 14px; padding: 0 18px 12px; font-size: 13.5px; }
    .groups label { display: inline-flex; gap: 6px; align-items: center; }
    .req { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 10px 18px; border-top: 1px solid var(--line); }
    .req .m { color: var(--muted); font-size: 13px; }
    .req q { display: block; font-size: 13.5px; margin-top: 2px; }
    .req .acts { display: flex; gap: 6px; }
    .st { font-size: 12.5px; padding: 2px 8px; border-radius: 999px; background: var(--surface2); }
    .empty { color: var(--muted); font-size: 13.5px; padding: 0 18px 16px; margin: 0; }
    @media (max-width: 560px) { .req { grid-template-columns: minmax(0, 1fr); } }
  `];
  org = "";
  groups: { id: string; name: string }[] = [];
  acc?: Access;
  reqs: StoreRequest[] = [];
  busy = false;

  willUpdate(ch: Map<string, unknown>) {
    if (!ch.has("org") || !this.org) return;
    void this.load();
  }

  private async load() {
    this.acc = await api<Access>("GET", `/orgs/${this.org}/store-access`).catch(() => undefined);
    this.reqs = await api<StoreRequest[]>("GET", `/orgs/${this.org}/store/requests`).catch(() => []);
  }

  private async save(next: Access) {
    this.busy = true;
    try {
      this.acc = await api<Access>("PUT", `/orgs/${this.org}/store-access`, next);
      toast(t("doc.saved"));
    } catch (e) {
      toast(errText(e), "error");
    } finally {
      this.busy = false;
    }
  }

  private async decide(r: StoreRequest, approve: boolean) {
    const answer = approve ? "" : prompt(t("store.reject_note"));
    if (answer === null) return;
    this.busy = true;
    try {
      await api("POST", `/orgs/${this.org}/store/requests/${r.id}`, { approve, answer });
      toast(t(approve ? "store.approved" : "store.rejected"));
      await this.load();
    } catch (e) {
      toast(errText(e), "error");
    } finally {
      this.busy = false;
    }
  }

  private rule(kind: "themes" | "apps") {
    const a = this.acc!;
    const r = a[kind];
    const who = (r.who || "all") as "all" | "groups" | "none";
    const set = (nr: Rule) => this.save({ ...a, [kind]: nr });
    return html`${srow(t(`store.access_${kind}` as Key), t("store.access_help"), sseg(t(`store.access_${kind}` as Key), who,
      [["all", t("store.who_all")], ["groups", t("store.who_groups")], ["none", t("store.who_none")]], (v) => set({ who: v, groups: r.groups ?? [] })))}
      ${who === "groups" ? html`<div class="groups">${this.groups.length ? this.groups.map((g) => html`<label><input type="checkbox" .checked=${(r.groups ?? []).includes(g.id)} ?disabled=${this.busy}
        @change=${(e: Event) => { const on = (e.target as HTMLInputElement).checked; const gs = (r.groups ?? []).filter((x) => x !== g.id); set({ who: "groups", groups: on ? [...gs, g.id] : gs }); }}> ${g.name}</label>`)
        : html`<span class="muted">${t("store.no_groups")}</span>`}</div>` : nothing}`;
  }

  render() {
    if (!this.acc) return nothing;
    const pending = this.reqs.filter((r) => r.status === "pending");
    const done = this.reqs.filter((r) => r.status !== "pending").slice(0, 10);
    const line = (r: StoreRequest) => html`<div class="req">
      <div><b>${r.itemName}</b> <span class="m">· ${t(r.itemType === "app" ? "store.type_app" : "store.type_theme")} · v${r.version} · ${r.projectName ?? r.projectSlug}</span>
        <div class="m">${t("store.requested_by", { name: r.userName ?? "" })}</div>${r.note ? html`<q>${r.note}</q>` : nothing}</div>
      ${r.status === "pending" ? html`<div class="acts"><button ?disabled=${this.busy} @click=${() => this.decide(r, true)}>${icon("Check", 14)} ${t("store.approve")}</button>
        <button class="ghost" ?disabled=${this.busy} @click=${() => this.decide(r, false)}>${t("store.reject")}</button></div>`
        : html`<span class="st">${t(r.status === "approved" ? "store.approved" : "store.rejected")}</span>`}</div>`;
    return html`${scard(t("store.access_title"), t("store.access_text"), html`
        ${this.rule("themes")}${this.rule("apps")}
        ${srow(t("store.requests_on"), t("store.requests_help"), sswitch(!this.acc.noRequests, t("store.requests_on"), (on) => this.save({ ...this.acc!, noRequests: !on }), this.busy))}`)}
      ${scard(t("store.requests_title"), "", html`${pending.length ? pending.map(line) : html`<p class="empty">${t("store.no_requests")}</p>`}${done.map(line)}`)}`;
  }
}

customElements.get("phx-store-themes") || customElements.define("phx-store-themes", PhxStoreThemes);
customElements.get("phx-store-apps") || customElements.define("phx-store-apps", PhxStoreApps);
customElements.get("phx-app-origin") || customElements.define("phx-app-origin", PhxAppOrigin);
customElements.get("phx-org-store") || customElements.define("phx-org-store", PhxOrgStore);
