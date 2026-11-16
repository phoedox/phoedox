// Documentation snapshots in the project history: named points (e.g. the
// docs at release 0.27.4), by hand or from release tags; a read-only list of
// the documents then and a comparison with another snapshot or now.
import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

interface Snapshot { id: string; name: string; note: string; at: string; source: "manual" | "tag"; sourceRef?: string }
interface SnapDoc { id: string; slug: string; type: string; title: string; category: string; number?: number; version: number }
interface SnapChange extends SnapDoc { change: "changed" | "created" | "deleted"; fromVersion: number; toVersion: number }

export class PhxSnapshots extends LitElement {
  static properties = { project: {}, canEdit: { type: Boolean }, owner: { type: Boolean }, list: { state: true }, open: { state: true }, docs: { state: true }, cmp: { state: true }, changes: { state: true }, adding: { state: true } };
  static styles = [base, css`
    :host { display: block; margin-bottom: 18px; }
    .box { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
    header { display: flex; gap: 10px; align-items: center; justify-content: space-between; padding: 12px 14px; flex-wrap: wrap; }
    header h2 { font-size: 16px; display: flex; gap: 8px; align-items: center; }
    .snaps { display: flex; gap: 8px; flex-wrap: wrap; padding: 0 14px 12px; }
    .snaps button { background: var(--surface2); color: var(--fg); font-weight: 500; font-size: 13px; padding: 5px 10px; border-radius: 999px; }
    .snaps button[aria-pressed="true"] { background: var(--accent); color: var(--on-color); }
    .snaps small { color: inherit; opacity: .75; font-weight: 400; margin-left: 4px; }
    form { display: flex; gap: 8px; flex-wrap: wrap; padding: 0 14px 12px; align-items: flex-end; }
    form label { display: grid; gap: 4px; font-size: 13px; color: var(--muted); }
    form input { width: auto; min-width: 160px; }
    .detail { border-top: 1px solid var(--line); padding: 12px 14px; display: grid; gap: 10px; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .row select { width: auto; }
    ul { list-style: none; margin: 0; padding: 0; display: grid; }
    li { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 6px 0; border-top: 1px solid var(--surface2); font-size: 14px; }
    li a { color: var(--fg); text-decoration: none; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    li .ico { color: var(--muted); }
    .chip.changed { background: var(--tb-blue); color: var(--tc-blue); } .chip.deleted { background: var(--tb-red); color: var(--tc-red); } .chip.created { background: var(--tb-green); color: var(--tc-green); }
    .muted { color: var(--muted); font-size: 13px; }
  `];

  project = "";
  canEdit = false;
  owner = false;
  list: Snapshot[] = [];
  open?: Snapshot;
  docs: SnapDoc[] = [];
  cmp = "now";
  changes?: SnapChange[];
  adding = false;

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("project") && this.project) void this.load();
  }

  private async load() {
    this.list = await api<Snapshot[]>("GET", `/projects/${this.project}/snapshots`).catch(() => []);
  }

  private async show(s: Snapshot) {
    if (this.open?.id === s.id) {
      this.open = undefined;
      return;
    }
    this.open = s;
    this.changes = undefined;
    const v = await api<{ documents: SnapDoc[] }>("GET", `/projects/${this.project}/snapshots/${encodeURIComponent(s.name)}`);
    this.docs = v.documents;
  }

  private async compare() {
    if (!this.open) return;
    const q = this.cmp === "now" ? "" : `?to=${encodeURIComponent(this.cmp)}`;
    const d = await api<{ changes: SnapChange[] }>("GET", `/projects/${this.project}/snapshots/${encodeURIComponent(this.open.name)}/compare${q}`);
    this.changes = d.changes;
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    try {
      await api("POST", `/projects/${this.project}/snapshots`, { name: String(f.get("name") ?? "").trim(), note: String(f.get("note") ?? "").trim() });
      this.adding = false;
      toast(t("snap.created"));
      void this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async removeSnapshot(s: Snapshot) {
    if (!confirm(t("snap.delete_confirm", { name: s.name }))) return;
    await api("DELETE", `/projects/${this.project}/snapshots/${encodeURIComponent(s.name)}`).catch(() => undefined);
    this.open = undefined;
    void this.load();
  }

  private item(d: SnapDoc, version: number, chip?: unknown) {
    return html`<li>${icon(d.type === "adr" ? "Scale" : "FileText", 14)}
      ${version ? html`<a href="/p/${this.project}/d/${d.id}/history?v=${version}">${d.title}</a>` : html`<span>${d.title}</span>`}
      ${chip ?? html`<span class="muted">v${version}</span>`}</li>`;
  }

  render() {
    const s = this.open;
    return html`<div class="box">
      <header><h2>${icon("Tag", 16)} ${t("snap.title")}</h2>
        ${this.canEdit ? html`<button class="ghost" @click=${() => (this.adding = !this.adding)}>${icon("Plus", 14)} ${t("snap.new")}</button>` : nothing}</header>
      ${this.adding ? html`<form @submit=${this.create}>
          <label>${t("snap.name")}<input name="name" required maxlength="60" placeholder="0.27.4" autofocus></label>
          <label>${t("snap.note")}<input name="note" maxlength="500"></label>
          <button>${t("snap.create")}</button></form>` : nothing}
      ${this.list.length ? html`<div class="snaps">${this.list.map((x) => html`<button aria-pressed=${s?.id === x.id ? "true" : "false"} @click=${() => this.show(x)}
          title=${x.sourceRef ?? ""}>${x.source === "tag" ? icon("GitBranch", 12) : nothing} ${x.name}<small>${formatDate(x.at)}</small></button>`)}</div>`
        : html`<p class="muted" style="padding:0 14px 12px;margin:0">${t("snap.none")}</p>`}
      ${s ? html`<div class="detail">
        <div class="row"><b>${s.name}</b><span class="muted">${formatDate(s.at)}${s.note ? ` · ${s.note}` : ""}</span>
          ${this.owner ? html`<button class="danger" style="margin-left:auto" @click=${() => this.removeSnapshot(s)} aria-label=${t("doc.delete")}>${icon("Trash2", 13)}</button>` : nothing}</div>
        <div class="row">${t("snap.compare")}
          <select @change=${(e: Event) => (this.cmp = (e.target as HTMLSelectElement).value)}>
            <option value="now" ?selected=${this.cmp === "now"}>${t("snap.now")}</option>
            ${this.list.filter((x) => x.id !== s.id).map((x) => html`<option value=${x.name} ?selected=${this.cmp === x.name}>${x.name}</option>`)}</select>
          <button class="ghost" @click=${this.compare}>${icon("GitCompare", 14)} ${t("snap.compare_go")}</button></div>
        ${this.changes ? this.changes.length ? html`<ul>${this.changes.map((c) => this.item(c, c.toVersion || c.fromVersion, html`<span class="chip ${c.change}">${t(`phist.${c.change}` as any, { from: c.fromVersion, to: c.toVersion })}</span>`))}</ul>`
            : html`<p class="muted" style="margin:0">${t("snap.same")}</p>`
          : html`<ul>${this.docs.map((d) => this.item(d, d.version))}</ul>`}
      </div>` : nothing}
    </div>`;
  }
}
customElements.get("phx-snapshots") || customElements.define("phx-snapshots", PhxSnapshots);
