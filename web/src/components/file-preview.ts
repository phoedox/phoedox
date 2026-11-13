import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { fileIcon, fileURL, formatSize, isImage, previewURL, uploadFile, versionURL, type FileInfo, type FileVersion } from "../files";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

const textLike = (m: string) => /^(text\/|application\/(json|xml|yaml))/.test(m);

/**
 * Modal preview of a file with its versions. Opened from attachments, the
 * files page and search; emits "changed" after a new or restored version.
 */
export class PhxFilePreview extends LitElement {
  static properties = {
    project: {}, file: { attribute: false }, editable: { type: Boolean },
    versions: { state: true }, text: { state: true }, progress: { state: true },
  };
  static styles = [base, css`
    dialog { width: min(1100px, calc(100vw - 32px)); height: min(820px, calc(100vh - 32px)); padding: 0; border: 1px solid var(--line);
      border-radius: 14px; background: var(--surface); color: var(--fg); box-shadow: 0 24px 80px rgb(0 0 0 / .25);
      opacity: 1; transform: none; transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out), overlay var(--dur) allow-discrete, display var(--dur) allow-discrete;
      @starting-style { opacity: 0; transform: scale(.97); } }
    dialog:not([open]) { opacity: 0; transform: scale(.97); }
    dialog::backdrop { background: rgb(0 0 0 / .45); transition: opacity var(--dur) var(--ease-out); @starting-style { opacity: 0; } }
    .wrap { display: grid; grid-template-rows: auto 1fr; height: 100%; }
    header { display: flex; align-items: center; gap: 10px; padding: 10px 14px; border-bottom: 1px solid var(--line); }
    header h2 { font-size: 16px; margin-right: auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    header .btn, header button { padding: 5px 10px; }
    .body { display: grid; grid-template-columns: minmax(0, 1fr) 260px; min-height: 0; }
    .view { display: grid; place-items: center; overflow: auto; background: var(--surface2); min-height: 0; }
    .view img { max-width: 100%; max-height: 100%; object-fit: contain; }
    .view iframe { width: 100%; height: 100%; border: 0; background: #fff; }
    .view video { max-width: 100%; max-height: 100%; }
    .view pre { align-self: start; justify-self: stretch; margin: 0; padding: 16px; font: 13px/1.55 var(--font-mono); white-space: pre-wrap; overflow-wrap: anywhere; background: var(--surface); min-height: 100%; }
    .none { text-align: center; color: var(--muted); display: grid; gap: 10px; justify-items: center; }
    aside { border-left: 1px solid var(--line); padding: 12px; overflow: auto; display: flex; flex-direction: column; gap: 12px; font-size: 13px; }
    aside h3 { font-size: 12px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); font-family: var(--font-body); }
    ol { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
    li { border: 1px solid var(--line); border-radius: 8px; padding: 6px 8px; display: grid; gap: 4px; }
    li.cur { border-color: var(--accent); }
    li .row { gap: 6px; }
    li button, li .btn { padding: 2px 8px; font-size: 12px; }
    .bar { height: 4px; border-radius: 2px; background: var(--surface2); overflow: hidden; }
    .bar span { display: block; height: 100%; background: var(--accent); transform-origin: left; transition: transform var(--dur) var(--ease-out); }
    input[type=file] { display: none; }
    @media (max-width: 760px) { .body { grid-template-columns: 1fr; grid-template-rows: minmax(0, 1fr) auto; } aside { border-left: 0; border-top: 1px solid var(--line); max-height: 40%; } }
    @media (prefers-reduced-motion: reduce) {
      dialog, dialog:not([open]) { transform: none; @starting-style { opacity: 0; transform: none; } }
    }
  `];
  project = "";
  file?: FileInfo;
  editable = false;
  versions?: FileVersion[];
  text?: string;
  progress = -1;
  private shownFor = "";

  /** Opens the dialog for a file. */
  async open(f: FileInfo) {
    this.file = f;
    this.versions = undefined;
    this.text = undefined;
    await this.updateComplete;
    const d = this.renderRoot.querySelector("dialog")!;
    if (!d.open) d.showModal();
    await this.load();
  }

  close() {
    this.renderRoot.querySelector("dialog")?.close();
  }

  private async load() {
    const f = this.file!;
    this.shownFor = `${f.id}/${f.version}`;
    this.versions = await api<FileVersion[]>("GET", `/projects/${this.project}/files/${f.id}/versions`).catch(() => []);
    if (textLike(f.mime) && f.size < 512 << 10) {
      this.text = await fetch(fileURL(f), { credentials: "same-origin" }).then((r) => r.text()).catch(() => "");
    }
  }

  private async refresh(f: FileInfo) {
    this.file = f;
    this.text = undefined;
    await this.load();
    this.dispatchEvent(new CustomEvent("changed", { detail: f, bubbles: true, composed: true }));
  }

  private async newVersion(files: FileList | null) {
    const file = files?.[0];
    if (!file || !this.file) return;
    this.progress = 0;
    try {
      const f = await uploadFile(this.project, file, { file: this.file.id }, (p) => (this.progress = p));
      toast(t("files.version_saved", { version: f.version }));
      await this.refresh(f);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.progress = -1;
    }
  }

  private async restore(v: FileVersion) {
    try {
      const f = await api<FileInfo>("POST", `/projects/${this.project}/files/${this.file!.id}/versions/${v.version}/restore`);
      toast(t("files.version_restored", { version: v.version }));
      await this.refresh(f);
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private view(f: FileInfo) {
    const url = fileURL(f);
    if (isImage(f.mime)) return html`<img src=${url} alt=${f.name}>`;
    // Browsers without a PDF viewer (Android, headless) get the preview image.
    if (f.mime === "application/pdf") {
      if (navigator.pdfViewerEnabled) return html`<iframe src=${url} title=${f.name}></iframe>`;
      return html`<div class="none">${f.hasPreview ? html`<img src=${previewURL(f)} alt=${f.name} style="max-height:60vh;box-shadow:0 2px 12px rgb(0 0 0 / .15)">` : icon("FileText", 40)}
        <a class="btn" href=${url} target="_blank" rel="noopener">${icon("ExternalLink", 14)} ${t("files.open_pdf")}</a></div>`;
    }
    if (f.mime.startsWith("video/")) return html`<video src=${url} controls preload="metadata"></video>`;
    if (f.mime.startsWith("audio/")) return html`<audio src=${url} controls preload="metadata"></audio>`;
    if (this.text !== undefined) return html`<pre>${this.text}</pre>`;
    return html`<div class="none">${icon(fileIcon(f.mime), 40)}<p>${t("files.no_preview")}</p>
      <a class="btn" href=${fileURL(f, true)}>${icon("Download", 14)} ${t("files.download")}</a></div>`;
  }

  render() {
    const f = this.file;
    return html`<dialog aria-label=${f?.name ?? ""} @close=${() => (this.shownFor = "")}
      @click=${(e: MouseEvent) => e.target === e.currentTarget && this.close()}>
      ${f ? html`<div class="wrap">
        <header>
          <span class="muted">${icon(fileIcon(f.mime), 16)}</span><h2>${f.name}</h2>
          <a class="btn ghost" href=${fileURL(f)} target="_blank" rel="noopener" title=${t("files.open_tab")} aria-label=${t("files.open_tab")}>${icon("ExternalLink", 14)}</a>
          <a class="btn ghost" href=${fileURL(f, true)} title=${t("files.download")} aria-label=${t("files.download")}>${icon("Download", 14)}</a>
          <button class="ghost" @click=${() => this.close()} aria-label=${t("files.close")}>${icon("X", 16)}</button>
        </header>
        <div class="body">
          <div class="view">${this.view(f)}</div>
          <aside>
            <div><h3>${t("files.details")}</h3>
              <p style="margin:4px 0">${f.mime} · ${formatSize(f.size, locale())}</p>
              <p class="muted" style="margin:0">${t(f.textStatus === "done" ? "files.text_done" : f.textStatus === "" ? "files.text_pending" : "files.text_none")}</p></div>
            <div><h3>${t("files.versions")}</h3>
              ${this.editable ? html`<button class="ghost" style="margin:6px 0 8px" ?disabled=${this.progress >= 0}
                  @click=${() => this.renderRoot.querySelector<HTMLInputElement>("input.ver")!.click()}>${icon("Upload", 13)} ${t("files.new_version")}</button>
                <input class="ver" type="file" @change=${(e: Event) => { const i = e.target as HTMLInputElement; this.newVersion(i.files); i.value = ""; }}>
                ${this.progress >= 0 ? html`<div class="bar" role="progressbar" aria-valuenow=${Math.round(this.progress * 100)}><span style="transform: scaleX(${this.progress})"></span></div>` : ""}` : ""}
              <ol>${(this.versions ?? []).map((v) => html`<li class=${v.version === f.version ? "cur" : ""}>
                <b>${t("files.version", { version: v.version })}${v.version === f.version ? ` · ${t("files.current")}` : ""}</b>
                <span class="muted">${formatDate(v.createdAt)} · ${formatSize(v.size, locale())}</span>
                ${v.version !== f.version ? html`<span class="row">
                  <a class="btn ghost" href=${versionURL(f, v.version)} target="_blank" rel="noopener">${t("files.show")}</a>
                  ${this.editable ? html`<button class="ghost" @click=${() => this.restore(v)}>${t("files.restore")}</button>` : ""}</span>` : ""}
              </li>`)}</ol></div>
          </aside>
        </div>
      </div>` : ""}
    </dialog>`;
  }
}
customElements.define("phx-file-preview", PhxFilePreview);
