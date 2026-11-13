import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { fileIcon, fileURL, formatSize, previewURL, uploadFile, type FileInfo } from "../files";
import type { PhxFilePreview } from "./file-preview";
import "./file-preview";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

type Pending = { name: string; progress: number };

/** Files attached to a document or entry: list, upload (button or drop), rename, delete. */
export class PhxAttachments extends LitElement {
  static properties = {
    project: {}, document: {}, entity: {}, editable: { type: Boolean },
    files: { state: true }, pending: { state: true }, over: { state: true }, renaming: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    section { border: 1px dashed transparent; border-radius: 12px; padding: 4px; margin: -4px;
      transition: border-color var(--dur-fast) ease, background-color var(--dur-fast) ease; }
    section.over { border-color: var(--accent); background: var(--accent-soft); }
    .head { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
    h2 { font-size: 16px; margin-right: auto; }
    ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
    li { display: grid; grid-template-columns: 44px minmax(0, 1fr) auto; gap: 10px; align-items: center;
      background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 6px 8px; }
    .thumb { width: 44px; height: 44px; border-radius: 6px; display: grid; place-items: center; overflow: hidden; background: var(--surface2); color: var(--muted); }
    .thumb img { width: 100%; height: 100%; object-fit: cover; }
    .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
    .meta { font-size: 12px; color: var(--muted); }
    .acts { display: flex; gap: 4px; }
    .acts button, .acts a { padding: 4px 7px; }
    .bar { height: 4px; border-radius: 2px; background: var(--surface2); overflow: hidden; margin-top: 4px; }
    .bar span { display: block; height: 100%; background: var(--accent); transform-origin: left; transition: transform var(--dur) var(--ease-out); }
    .hint { font-size: 12px; color: var(--muted); margin: 6px 0 0; }
    input[type=file] { display: none; }
    form.rn { display: flex; gap: 6px; }
    @media (prefers-reduced-motion: reduce) { .bar span { transition: none; } }
  `];
  project = "";
  /** Document id (attachments of a page) … */
  document = "";
  /** … or entity id (attachments of an entry). */
  entity = "";
  editable = false;
  files?: FileInfo[];
  pending: Pending[] = [];
  over = false;
  renaming = "";
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.document}/${this.entity}`;
    if (key === this.loaded || (!this.document && !this.entity)) return;
    this.loaded = key;
    await this.load();
  }

  /** Reloads the list (after uploads from elsewhere, e.g. the editor). */
  refresh() {
    return this.load();
  }

  private polls = 0;

  private async load() {
    const q = this.document ? `document=${this.document}` : `entity=${this.entity}`;
    this.files = (await api<FileInfo[]>("GET", `/projects/${this.project}/files?${q}`).catch(() => [])) ?? [];
    // Previews and text are made in the background: look again a few times.
    if (this.files.some((f) => f.textStatus === "") && this.polls < 6) {
      this.polls++;
      setTimeout(() => this.isConnected && this.load(), 1500 * this.polls);
    } else this.polls = 0;
  }

  private preview(e: Event, f: FileInfo) {
    e.preventDefault();
    this.renderRoot.querySelector<PhxFilePreview>("phx-file-preview")!.open(f);
  }

  /** Uploads files one after another and shows their progress. */
  async upload(list: FileList | File[]) {
    const files = Array.from(list);
    this.pending = [...this.pending, ...files.map((f) => ({ name: f.name, progress: 0 }))];
    for (const f of files) {
      const p = this.pending.find((x) => x.name === f.name && x.progress === 0)!;
      try {
        await uploadFile(this.project, f, { document: this.document, entity: this.entity }, (v) => {
          p.progress = v;
          this.pending = [...this.pending];
        });
      } catch (e) {
        toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      }
      this.pending = this.pending.filter((x) => x !== p);
    }
    await this.load();
  }

  private async rename(e: SubmitEvent, f: FileInfo) {
    e.preventDefault();
    const name = String(new FormData(e.target as HTMLFormElement).get("name") ?? "").trim();
    this.renaming = "";
    if (!name || name === f.name) return;
    try {
      await api("PATCH", `/projects/${this.project}/files/${f.id}`, { name });
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async removeFile(f: FileInfo) {
    const project = this.project;
    try {
      await api("DELETE", `/projects/${project}/files/${f.id}`);
      await this.load();
      toast(t("files.trashed", { name: f.name }), "ok", {
        label: t("doc.undo"),
        run: async () => {
          await api("POST", `/projects/${project}/trash/${f.id}/restore`).catch(() => {});
          await this.load();
        },
      });
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private onDrop(e: DragEvent) {
    e.preventDefault();
    this.over = false;
    if (this.editable && e.dataTransfer?.files.length) this.upload(e.dataTransfer.files);
  }

  render() {
    const files = this.files ?? [];
    if (!this.editable && !files.length) return "";
    return html`<section class=${this.over ? "over" : ""}
      @dragover=${(e: DragEvent) => { if (this.editable && e.dataTransfer?.types.includes("Files")) { e.preventDefault(); this.over = true; } }}
      @dragleave=${() => (this.over = false)} @drop=${this.onDrop}>
      <div class="head"><h2>${t("files.title")}</h2>
        ${this.editable ? html`<button class="ghost" @click=${() => this.renderRoot.querySelector<HTMLInputElement>("input[type=file]")!.click()}>${icon("Upload", 14)} ${t("files.add")}</button>
          <input type="file" multiple @change=${(e: Event) => { const i = e.target as HTMLInputElement; if (i.files) this.upload(i.files); i.value = ""; }}>` : ""}
      </div>
      <ul>
        ${files.map((f) => html`<li>
          <a class="thumb" href=${fileURL(f)} @click=${(e: Event) => this.preview(e, f)} aria-label=${t("files.open", { name: f.name })}>
            ${f.hasPreview ? html`<img src=${previewURL(f)} alt="" loading="lazy">` : icon(fileIcon(f.mime), 18)}</a>
          <div style="min-width:0">
            ${this.renaming === f.id
              ? html`<form class="rn" @submit=${(e: SubmitEvent) => this.rename(e, f)}><input name="name" .value=${f.name} maxlength="200" aria-label=${t("files.rename")}
                  @keydown=${(e: KeyboardEvent) => e.key === "Escape" && (this.renaming = "")}><button class="ghost">${icon("Check", 13)}</button></form>`
              : html`<div class="name"><a href=${fileURL(f)} @click=${(e: Event) => this.preview(e, f)}>${f.name}</a></div>`}
            <div class="meta">${formatSize(f.size, locale())} · ${formatDate(f.createdAt)}${f.version > 1 ? ` · ${t("files.version", { version: f.version })}` : ""}</div>
          </div>
          <div class="acts">
            <a class="btn ghost" href=${fileURL(f, true)} aria-label=${t("files.download")} title=${t("files.download")}>${icon("Download", 14)}</a>
            ${this.editable ? html`
              <button class="ghost" aria-label=${t("files.rename")} title=${t("files.rename")} @click=${async () => { this.renaming = f.id; await this.updateComplete; this.renderRoot.querySelector<HTMLInputElement>("form.rn input")?.select(); }}>${icon("Pencil", 14)}</button>
              <button class="ghost" aria-label=${t("files.delete")} title=${t("files.delete")} @click=${() => this.removeFile(f)}>${icon("Trash2", 14)}</button>` : ""}
          </div>
        </li>`)}
        ${this.pending.map((p) => html`<li><span class="thumb">${icon("Upload", 18)}</span>
          <div><div class="name">${p.name}</div><div class="bar" role="progressbar" aria-valuenow=${Math.round(p.progress * 100)} aria-valuemin="0" aria-valuemax="100"><span style="transform: scaleX(${p.progress})"></span></div></div><span></span></li>`)}
      </ul>
      ${this.editable && !files.length && !this.pending.length ? html`<p class="hint">${t("files.drop_hint")}</p>` : ""}
    </section>
    <phx-file-preview .project=${this.project} .editable=${this.editable} @changed=${() => this.load()}></phx-file-preview>`;
  }
}
customElements.define("phx-attachments", PhxAttachments);
