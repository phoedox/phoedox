import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { api } from "../api";
import { fileIcon, formatSize, previewURL, type FileInfo, type Storage } from "../files";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import type { PhxFilePreview } from "./file-preview";
import { base } from "./shared";
import "./file-preview";

/** Storage bar: used of limit (or just the use without a limit). */
export function storageBar(s: Storage) {
  const lim = s.limitBytes;
  const frac = lim ? Math.min(1, s.usedBytes / lim) : 0;
  return html`<div class="storage">
    <div class="sbar" role=${lim ? "meter" : "presentation"} aria-valuenow=${Math.round(frac * 100)} aria-valuemin="0" aria-valuemax="100"
      aria-label=${t("storage.label")}><span class=${frac > 0.9 ? "full" : ""} style="transform: scaleX(${frac})"></span></div>
    <span class="muted">${lim
      ? t("storage.used_of", { used: formatSize(s.usedBytes, locale()), limit: formatSize(lim, locale()) })
      : t("storage.used", { used: formatSize(s.usedBytes, locale()) })}</span>
  </div>`;
}

export const storageStyles = css`
  .storage { display: flex; align-items: center; gap: 10px; font-size: 13px; }
  .sbar { width: 160px; height: 6px; border-radius: 3px; background: var(--surface2); overflow: hidden; }
  .sbar span { display: block; height: 100%; background: var(--accent); transform-origin: left; }
  .sbar span.full { background: var(--bad); }
`;

/** All files of a project with search, previews and storage use. */
export class PhxProjectFiles extends SignalWatcher(LitElement) {
  static properties = { project: {}, files: { state: true }, storage: { state: true }, query: { state: true } };
  static styles = [base, storageStyles, css`
    .head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
    .head h1 { margin-right: auto; }
    input[type=search] { max-width: 320px; margin-bottom: 14px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(170px, 1fr)); gap: 12px; }
    .tile { all: unset; box-sizing: border-box; cursor: pointer; display: grid; grid-template-rows: 120px auto; border: 1px solid var(--line);
      border-radius: 10px; overflow: hidden; background: var(--surface); transition: border-color var(--dur-fast) ease; }
    .tile:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    @media (hover: hover) and (pointer: fine) { .tile:hover { border-color: var(--muted); } }
    .pv { display: grid; place-items: center; background: var(--surface2); color: var(--muted); overflow: hidden; }
    .pv img { width: 100%; height: 100%; object-fit: cover; }
    .cap { padding: 8px 10px; font-size: 13px; min-width: 0; }
    .cap b { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
    .cap span { color: var(--muted); font-size: 12px; }
  `];
  project = "";
  files?: FileInfo[];
  storage?: Storage;
  query = "";
  private loaded = "";

  async willUpdate() {
    if (this.loaded === this.project) return;
    this.loaded = this.project;
    this.query = new URLSearchParams(location.search).get("q") ?? "";
    await this.load();
    const open = new URLSearchParams(location.search).get("open");
    const f = this.files?.find((x) => x.id === open);
    if (f) {
      await this.updateComplete;
      this.open(f);
    }
  }

  private async load() {
    [this.files, this.storage] = await Promise.all([
      api<FileInfo[]>("GET", `/projects/${this.project}/files`).catch(() => []),
      api<Storage>("GET", `/projects/${this.project}/storage`).catch(() => undefined),
    ]);
  }

  private open(f: FileInfo) {
    this.renderRoot.querySelector<PhxFilePreview>("phx-file-preview")!.open(f);
  }

  render() {
    if (!this.files) return html`<p class="muted">${t("app.loading")}</p>`;
    const q = this.query.trim().toLowerCase();
    const shown = q ? this.files.filter((f) => f.name.toLowerCase().includes(q)) : this.files;
    const canEdit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    return html`<div class="enter">
      <div class="head"><h1>${t("files.page_title")}</h1>${this.storage ? storageBar(this.storage) : ""}</div>
      <input type="search" .value=${this.query} placeholder=${t("files.filter")} aria-label=${t("files.filter")}
        @input=${(e: Event) => (this.query = (e.target as HTMLInputElement).value)}>
      ${shown.length ? html`<div class="grid">${shown.map((f) => html`<button class="tile" @click=${() => this.open(f)} aria-label=${t("files.open", { name: f.name })}>
        <span class="pv">${f.hasPreview ? html`<img src=${previewURL(f)} alt="" loading="lazy">` : icon(fileIcon(f.mime), 28)}</span>
        <span class="cap"><b>${f.name}</b><span>${formatSize(f.size, locale())} · ${formatDate(f.createdAt)}</span></span>
      </button>`)}</div>` : html`<p class="muted">${t(q ? "files.no_match" : "files.empty")}</p>`}
      <phx-file-preview .project=${this.project} .editable=${canEdit} @changed=${() => this.load()}></phx-file-preview>
    </div>`;
  }
}
customElements.define("phx-project-files", PhxProjectFiles);
