import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import "./checks";
import { base } from "./shared";
import { toast } from "./toasts";

type IncidentStatus = "investigating" | "identified" | "monitoring" | "resolved";
interface IncidentUpdate { id: string; status: IncidentStatus; body: string; auto: boolean; createdAt: string }
interface Incident {
  id: string; checkId?: string; title: string; status: IncidentStatus; public: boolean;
  startedAt: string; resolvedAt: string | null; updates: IncidentUpdate[];
}
const STATES: IncidentStatus[] = ["investigating", "identified", "monitoring", "resolved"];

/** Monitoring of a project: checks, incidents with their timeline, public status page. */
export class PhxMonitoring extends LitElement {
  static properties = { project: {}, incidents: { state: true }, statusPath: { state: true }, busy: { state: true }, creating: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 980px; margin: 0 auto; padding: 24px 16px 60px; }
    h2 { font-size: 18px; margin: 28px 0 10px; display: flex; gap: 8px; align-items: center; }
    .card { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; background: var(--surface); margin-bottom: 12px; }
    .inc.open { border-left: 3px solid var(--bad); }
    .inc header { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .inc header b { margin-right: auto; }
    .inc ol { list-style: none; padding: 0; margin: 10px 0 0; display: grid; gap: 6px; font-size: 14px; }
    .inc li time { color: var(--muted); font-size: 12.5px; margin-left: 6px; }
    .inc form { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 10px; }
    .inc form input { flex: 1; min-width: 200px; }
    .inc form select { width: auto; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    label.switch { display: inline-flex; gap: 8px; align-items: center; }
    label.switch input { width: auto; }
    code { word-break: break-all; }
  `];
  project = "";
  incidents?: Incident[];
  statusPath = "";
  busy = false;
  creating = false;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private get projectRole() {
    return currentProject.get()?.role ?? "";
  }

  private async load() {
    this.incidents = (await api<Incident[]>("GET", `/projects/${this.project}/incidents`).catch(() => [])) ?? [];
    if (["editor", "owner"].includes(this.projectRole)) {
      const s = await api<{ enabled: boolean; path?: string }>("GET", `/projects/${this.project}/status-page`).catch(() => undefined);
      this.statusPath = s?.path ?? "";
    }
  }

  private async call(fn: () => Promise<unknown>, done?: string) {
    this.busy = true;
    try {
      await fn();
      await this.load();
      if (done) toast(done);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private create(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    this.call(async () => {
      await api("POST", `/projects/${this.project}/incidents`, { title: f.get("title"), body: f.get("body"), status: f.get("status"), public: f.get("public") === "on" });
      form.reset();
      this.creating = false;
    });
  }

  private incident(i: Incident, canEdit: boolean) {
    const open = !i.resolvedAt;
    return html`<section class="card inc ${open ? "open" : ""}">
      <header><b>${i.title}</b>
        <span class="chip">${t(`incident.${i.status}` as any)}</span>
        ${i.public ? html`<span class="chip">${icon("Globe", 12)} ${t("incident.public")}</span>` : ""}
        ${i.checkId ? html`<span class="chip" title=${t("incident.from_check")}>${icon("Activity", 12)}</span>` : ""}
        ${canEdit ? html`<label class="switch" style="font-size:13px"><input type="checkbox" .checked=${i.public}
            @change=${(e: Event) => this.call(() => api("PATCH", `/projects/${this.project}/incidents/${i.id}`, { title: i.title, public: (e.target as HTMLInputElement).checked }))}> ${t("incident.show_public")}</label>
          <button class="ghost" aria-label=${t("incident.delete")} title=${t("incident.delete")} @click=${() => confirm(t("incident.delete_confirm")) && this.call(() => api("DELETE", `/projects/${this.project}/incidents/${i.id}`))}>${icon("Trash2", 14)}</button>` : ""}
      </header>
      <p class="muted" style="margin:4px 0 0;font-size:13px">${formatDate(i.startedAt)} – ${i.resolvedAt ? formatDate(i.resolvedAt) : t("incident.ongoing")}</p>
      <ol>${i.updates.map((u) => html`<li><b>${t(`incident.${u.status}` as any)}</b> ${u.body}<time>${formatDate(u.createdAt)}${u.auto ? ` · ${t("incident.auto")}` : ""}</time></li>`)}</ol>
      ${canEdit ? html`<form @submit=${(e: SubmitEvent) => {
        e.preventDefault();
        const form = e.target as HTMLFormElement;
        const f = new FormData(form);
        this.call(async () => { await api("POST", `/projects/${this.project}/incidents/${i.id}/updates`, { status: f.get("status"), body: f.get("body") }); form.reset(); });
      }}>
        <select name="status" aria-label=${t("incident.status")}>${STATES.map((s) => html`<option value=${s} ?selected=${s === (open ? i.status : "resolved")}>${t(`incident.${s}` as any)}</option>`)}</select>
        <input name="body" maxlength="10000" placeholder=${t("incident.update_placeholder")} aria-label=${t("incident.update_placeholder")}>
        <button ?disabled=${this.busy}>${t("incident.add_update")}</button>
      </form>` : ""}
    </section>`;
  }

  render() {
    const role = this.projectRole;
    const canEdit = role === "editor" || role === "owner";
    const list = this.incidents ?? [];
    const open = list.filter((i) => !i.resolvedAt);
    const closed = list.filter((i) => i.resolvedAt);
    const url = this.statusPath ? `${location.origin}${this.statusPath}` : "";
    return html`<div class="enter">
      <h1>${t("monitoring.title")}</h1>
      <p class="muted">${t("monitoring.intro")}</p>

      ${canEdit ? html`<h2>${icon("Globe", 17)} ${t("statuspage.title")}</h2>
        <div class="card">
          <p class="muted" style="margin-top:0">${t("statuspage.intro")}</p>
          ${url ? html`<div class="row"><a href=${url} target="_blank" rel="noopener">${icon("ExternalLink", 13)} <code>${url}</code></a>
            <button class="ghost" @click=${() => navigator.clipboard?.writeText(url).then(() => toast(t("statuspage.copied")))}>${icon("Copy", 13)} ${t("statuspage.copy")}</button></div>` : ""}
          ${role === "owner" ? html`<div class="row" style="margin-top:8px">
            <label class="switch"><input type="checkbox" .checked=${!!url} ?disabled=${this.busy}
              @change=${(e: Event) => this.call(() => api("PUT", `/projects/${this.project}/status-page`, { enabled: (e.target as HTMLInputElement).checked }))}> ${t("statuspage.enable")}</label></div>` : ""}
          <p class="muted" style="font-size:13px;margin-bottom:0">${t("statuspage.publication_hint")}</p>
        </div>` : ""}

      <h2>${icon("Activity", 17)} ${t("check.title")}</h2>
      <phx-checks .project=${this.project} public-toggle></phx-checks>

      <h2>${icon("TriangleAlert", 17)} ${t("incident.title")}</h2>
      ${canEdit ? (this.creating ? html`<form class="card" @submit=${this.create} style="display:grid;gap:8px">
          <label class="field">${t("incident.name")}<input name="title" required maxlength="200" autofocus></label>
          <label class="field">${t("incident.status")}<select name="status">${STATES.map((s) => html`<option value=${s}>${t(`incident.${s}` as any)}</option>`)}</select></label>
          <label class="field">${t("incident.first_update")}<input name="body" maxlength="10000"></label>
          <label class="switch"><input type="checkbox" name="public"> ${t("incident.show_public")}</label>
          <div class="row"><button ?disabled=${this.busy}>${t("incident.create")}</button><button type="button" class="ghost" @click=${() => (this.creating = false)}>${t("doc.cancel")}</button></div>
        </form>` : html`<p><button class="ghost" @click=${() => (this.creating = true)}>${icon("Plus", 14)} ${t("incident.new")}</button></p>`) : ""}
      ${this.incidents === undefined ? html`<p class="muted">${t("app.loading")}</p>`
        : list.length ? html`${open.map((i) => this.incident(i, canEdit))}${closed.map((i) => this.incident(i, canEdit))}`
        : html`<p class="muted">${t("incident.none")}</p>`}
    </div>`;
  }
}
customElements.define("phx-monitoring", PhxMonitoring);
