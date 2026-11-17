// Project settings → Calendars (ADR-0089): external calendars (CalDAV or an
// iCal URL; writing the project's dates back is a switch per calendar) and
// the project's secret iCal feed.
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDay, t } from "../i18n";
import { icon } from "../icon";
import type { CalendarView, ExternalCalendar } from "./calendar";
import { base } from "./shared";
import { toast } from "./toasts";

const COLORS = ["", "gray", "red", "orange", "yellow", "green", "blue", "purple", "pink"];

export class PhxCalendarSettings extends LitElement {
  static properties = { project: {}, owner: { type: Boolean }, cals: { state: true }, feed: { state: true }, editing: { state: true }, busy: { state: true }, feedURL: { state: true }, kind: { state: true } };
  static styles = [base, css`
    :host { display: grid; gap: 14px; }
    .cal { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
    .cal b { display: block; }
    .cal small { color: var(--muted); display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .cal .err { color: var(--bad); white-space: normal; }
    .dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
    .acts { display: flex; gap: 6px; }
    form { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--line); border-radius: 10px; background: var(--surface); }
    .two { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    @media (max-width: 640px) { .two { grid-template-columns: 1fr; } .cal { grid-template-columns: auto minmax(0, 1fr); } .acts { grid-column: 1 / -1; } }
    .check { display: flex; gap: 8px; align-items: flex-start; }
    .check input { margin-top: 3px; }
    .hint { color: var(--muted); font-size: 12.5px; }
    .feed { display: grid; gap: 8px; }
    .url { display: flex; gap: 6px; }
    .url input { flex: 1; font: 12px var(--font-mono); }
  `];
  project = "";
  owner = false;
  cals: ExternalCalendar[] = [];
  feed?: CalendarView["feed"];
  editing?: ExternalCalendar | "new";
  busy = false;
  feedURL = "";
  kind = "caldav";

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    const today = new Date().toISOString().slice(0, 10);
    const v = await api<CalendarView>("GET", `/projects/${this.project}/calendar?from=${today}&to=${today}T23:59:59Z&sources=external`).catch(() => undefined);
    this.cals = v?.calendars ?? [];
    this.feed = v?.feed;
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const cur = this.editing === "new" ? undefined : this.editing;
    const pw = String(f.get("password") ?? "");
    const body: Record<string, unknown> = {
      name: f.get("name"), kind: f.get("kind"), url: f.get("url"), username: f.get("username") ?? "",
      color: f.get("color") ?? "", writeBack: f.get("writeBack") === "on",
    };
    if (pw || !cur) body.password = pw;
    if (cur && f.get("clearPassword") === "on") body.password = "";
    this.busy = true;
    try {
      await api(cur ? "PATCH" : "POST", `/projects/${this.project}/calendars${cur ? `/${cur.id}` : ""}`, body);
      toast(t("cal.saved"));
      this.editing = undefined;
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async sync(c: ExternalCalendar) {
    this.busy = true;
    try {
      const r = await api<ExternalCalendar>("POST", `/projects/${this.project}/calendars/${c.id}/sync`);
      toast(r.lastError ? t("cal.sync_failed", { error: r.lastError }) : t("cal.synced"), r.lastError ? "error" : undefined);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removeCal(c: ExternalCalendar) {
    if (!confirm(t("cal.delete_confirm", { name: c.name }))) return;
    try {
      await api("DELETE", `/projects/${this.project}/calendars/${c.id}`);
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async setFeed(on: boolean) {
    if (!on && !confirm(t("cal.feed_off_confirm"))) return;
    try {
      const r = await api<{ on: boolean; token?: string }>("PUT", `/projects/${this.project}/calendar/feed`, { on });
      this.feedURL = r.token ? `${location.origin}/cal/${r.token}.ics` : "";
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private form() {
    const c = this.editing === "new" ? undefined : this.editing;
    const kind = this.kind;
    return html`<form @submit=${this.save}>
      <div class="two">
        <label class="field">${t("cal.name")}<input name="name" required maxlength="100" .value=${c?.name ?? ""} /></label>
        <label class="field">${t("cal.kind")}<select name="kind" @change=${(e: Event) => (this.kind = (e.target as HTMLSelectElement).value)}>
          <option value="caldav" ?selected=${kind === "caldav"}>CalDAV</option><option value="ical" ?selected=${kind === "ical"}>${t("cal.kind_ical")}</option></select></label>
      </div>
      <label class="field">${t("cal.url")}<input name="url" type="url" required inputmode="url" placeholder=${kind === "caldav" ? "https://cloud.example.org/remote.php/dav/calendars/me/family/" : "https://… .ics"} .value=${c?.url ?? ""} /></label>
      <p class="hint">${t(kind === "caldav" ? "cal.url_hint_caldav" : "cal.url_hint_ical")}</p>
      <div class="two">
        <label class="field">${t("cal.username")}<input name="username" autocomplete="off" .value=${c?.username ?? ""} /></label>
        <label class="field">${t("cal.password")}<input name="password" type="password" autocomplete="new-password" placeholder=${c?.hasPassword ? t("cal.password_kept") : ""} /></label>
      </div>
      ${c?.hasPassword ? html`<label class="check"><input type="checkbox" name="clearPassword" /> ${t("cal.password_clear")}</label>` : ""}
      <label class="field">${t("cal.color")}<select name="color">${COLORS.map((col) => html`<option value=${col} ?selected=${(c?.color ?? "") === col}>${col ? t(`color.${col}` as any) : t("cal.color_auto")}</option>`)}</select></label>
      ${kind === "caldav" ? html`<label class="check"><input type="checkbox" name="writeBack" ?checked=${c?.writeBack ?? false} />
        <span>${t("cal.write_back")}<br /><span class="hint">${t("cal.write_back_hint")}</span></span></label>` : ""}
      <div class="acts"><button ?disabled=${this.busy}>${icon("Check", 14)} ${t(c ? "doc.save" : "cal.add")}</button>
        <button type="button" class="ghost" @click=${() => (this.editing = undefined)}>${t("doc.cancel")}</button></div>
    </form>`;
  }

  render() {
    return html`
      ${this.cals.map((c) => html`<div class="cal">
        <i class="dot" style="background:var(--tc-${c.color || "gray"})"></i>
        <div><b>${c.name}</b><small>${c.kind === "caldav" ? "CalDAV" : t("cal.kind_ical")} · ${c.url}</small>
          <small>${c.writeBack ? t("cal.writes_back") : t("cal.reads_only")} · ${c.syncedAt ? t("cal.synced_at", { when: formatDay(c.syncedAt) }) : t("cal.never_synced")}</small>
          ${c.lastError ? html`<small class="err">${icon("TriangleAlert", 12)} ${c.lastError}</small>` : ""}</div>
        <div class="acts"><button class="ghost" ?disabled=${this.busy} @click=${() => this.sync(c)} title=${t("cal.sync")} aria-label=${t("cal.sync")}>${icon("RefreshCw", 14)}</button>
          ${this.owner ? html`<button class="ghost" @click=${() => { this.kind = c.kind; this.editing = c; }} title=${t("doc.edit")} aria-label=${t("doc.edit")}>${icon("Pencil", 14)}</button>
            <button class="ghost" @click=${() => this.removeCal(c)} title=${t("doc.delete")} aria-label=${t("doc.delete")}>${icon("Trash2", 14)}</button>` : ""}</div>
      </div>`)}
      ${!this.cals.length ? html`<p class="hint">${t("cal.no_external")}</p>` : ""}
      ${this.editing ? this.form() : this.owner ? html`<div><button class="ghost" @click=${() => { this.kind = "caldav"; this.editing = "new"; }}>${icon("Plus", 14)} ${t("cal.add")}</button></div>` : ""}
      ${this.owner ? html`<div class="feed"><h3>${t("cal.feed")}</h3><p class="hint">${t("cal.feed_hint")}</p>
        ${this.feedURL ? html`<div class="url"><input readonly .value=${this.feedURL} @focus=${(e: Event) => (e.target as HTMLInputElement).select()} aria-label=${t("cal.feed")} />
          <button class="ghost" @click=${() => { void navigator.clipboard?.writeText(this.feedURL); toast(t("cal.copied")); }}>${icon("Copy", 14)} ${t("cal.copy")}</button></div>
          <p class="hint">${t("cal.feed_once")}</p>` : ""}
        <div class="acts">${this.feed?.on
          ? html`<span class="hint">${t("cal.feed_on", { when: formatDay(this.feed.since ?? "") })}</span>
            <button class="ghost" @click=${() => this.setFeed(true)}>${icon("RefreshCw", 14)} ${t("cal.feed_new")}</button>
            <button class="ghost" @click=${() => this.setFeed(false)}>${t("cal.feed_off")}</button>`
          : html`<button class="ghost" @click=${() => this.setFeed(true)}>${icon("Link", 14)} ${t("cal.feed_create")}</button>`}</div></div>` : ""}`;
  }
}
customElements.get("phx-calendar-settings") || customElements.define("phx-calendar-settings", PhxCalendarSettings);
