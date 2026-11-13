import { LitElement, css, html } from "lit";
import type { Doc } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { navigate } from "../state";
import { base } from "./shared";

type Step = "what" | "type" | "folder";

/**
 * Creating from the sidebar: "+" on a category asks for a folder or a page;
 * a page then asks for its type with a small preview of each.
 */
export class PhxCreateDialog extends LitElement {
  static properties = { project: {}, step: { state: true }, category: { state: true }, folderError: { state: true } };
  static styles = [base, css`
    dialog { border: 1px solid var(--line); border-radius: 14px; padding: 20px 22px; width: min(640px, calc(100vw - 32px)); background: var(--bg); color: var(--fg);
      opacity: 1; transform: none; transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out), overlay var(--dur) allow-discrete, display var(--dur) allow-discrete;
      @starting-style { opacity: 0; transform: scale(.97); } }
    dialog::backdrop { background: color-mix(in srgb, #000 35%, transparent); }
    h2 { margin: 0 0 4px; font-size: 18px; }
    .where { color: var(--muted); font-size: 13px; margin: 0 0 14px; }
    .choices { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 10px; }
    .choice { display: flex; flex-direction: column; align-items: stretch; gap: 8px; text-align: left; padding: 12px; border-radius: 10px;
      border: 1px solid var(--line); background: var(--surface); color: var(--fg); font: inherit; cursor: pointer;
      transition: border-color var(--dur-fast) ease, background-color var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { .choice:hover { border-color: var(--accent); } }
    .choice:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .choice .name { display: flex; gap: 8px; align-items: center; font-weight: 600; }
    .choice .desc { color: var(--muted); font-size: 13px; line-height: 1.4; }
    /* Miniature previews of each type. */
    .pv { height: 92px; border-radius: 6px; background: var(--bg); border: 1px solid var(--line); padding: 9px 10px; display: flex; flex-direction: column; gap: 5px; overflow: hidden; }
    .pv i { display: block; height: 6px; border-radius: 3px; background: var(--line); }
    .pv i.h { height: 9px; width: 55%; background: var(--muted); opacity: .55; }
    .pv i.s { width: 70%; } .pv i.xs { width: 40%; }
    .pv .chips { display: flex; gap: 4px; } .pv .chips b { height: 9px; width: 34px; border-radius: 5px; background: var(--accent-soft); }
    .pv .chips b.ok { background: color-mix(in srgb, var(--good) 30%, transparent); }
    .pv .sec { font: 600 7px var(--font-body); letter-spacing: .08em; color: var(--muted); text-transform: uppercase; }
    .pv .tbl { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 2px; } .pv .tbl i { height: 7px; border-radius: 1px; }
    .pv.link { align-items: center; justify-content: center; gap: 8px; color: var(--accent); }
    .pv.link i { width: 60%; }
    .pv.folder { flex-direction: row; flex-wrap: wrap; align-content: flex-start; gap: 6px; color: var(--muted); }
    form { display: grid; gap: 10px; }
    .row { display: flex; gap: 8px; justify-content: flex-end; margin-top: 16px; }
    @media (prefers-reduced-motion: reduce) { dialog { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
  `];
  project = "";
  step: Step = "what";
  category = "";
  folderError = "";

  /** Opens the dialog for a category ("" = no category); "type" skips the folder question. */
  open(category: string, step: Step = "what") {
    this.category = category;
    this.step = step;
    this.folderError = "";
    this.updateComplete.then(() => this.renderRoot.querySelector("dialog")!.showModal());
  }

  private close() {
    this.renderRoot.querySelector("dialog")?.close();
  }

  private create(type: Doc["type"] | "entity") {
    this.close();
    const q = this.category ? `?category=${encodeURIComponent(this.category)}` : "";
    navigate(type === "entity" ? `/p/${this.project}/new-entity` : `/p/${this.project}/new/${type}${q}`);
  }

  private folder(e: SubmitEvent) {
    e.preventDefault();
    const name = (new FormData(e.target as HTMLFormElement).get("name") as string).trim().replace(/\//g, "–");
    if (!name) {
      this.folderError = t("create.folder_required");
      return;
    }
    // A folder is a category path; it exists once its first page is created.
    this.category = this.category ? `${this.category}/${name}` : name;
    this.step = "type";
  }

  private choice(ico: string, name: string, desc: string, preview: unknown, run: () => void) {
    return html`<button class="choice" @click=${run}>
      ${preview}
      <span class="name">${icon(ico, 16)} ${name}</span>
      <span class="desc">${desc}</span>
    </button>`;
  }

  private body() {
    if (this.step === "folder") {
      return html`<h2 id="ct">${t("create.folder_title")}</h2>
        <p class="where">${this.category ? t("create.inside", { category: this.category.replace(/\//g, " › ") }) : t("create.top_level")}</p>
        <form @submit=${this.folder}>
          <label class="field">${t("create.folder_name")}<input name="name" required maxlength="80" autofocus autocomplete="off"></label>
          <p class="muted" style="margin:0;font-size:13px">${t("create.folder_hint")}</p>
          ${this.folderError ? html`<p class="error" role="alert">${this.folderError}</p>` : ""}
          <div class="row"><button type="button" class="ghost" @click=${this.close}>${t("doc.cancel")}</button><button>${t("create.next")}</button></div>
        </form>`;
    }
    const where = html`<p class="where">${this.category ? t("create.inside", { category: this.category.replace(/\//g, " › ") }) : t("create.top_level")}</p>`;
    if (this.step === "what") {
      return html`<h2 id="ct">${t("create.title")}</h2>${where}
        <div class="choices">
          ${this.choice("FilePlus", t("create.page"), t("create.page_desc"), html`<span class="pv"><i class="h"></i><i></i><i class="s"></i><i></i><i class="xs"></i></span>`, () => (this.step = "type"))}
          ${this.choice("FolderPlus", t("create.folder"), t("create.folder_desc"), html`<span class="pv folder">${icon("Folder", 22)}${icon("FileText", 22)}${icon("FileText", 22)}</span>`, () => (this.step = "folder"))}
        </div>
        <div class="row"><button class="ghost" @click=${this.close}>${t("doc.cancel")}</button></div>`;
    }
    return html`<h2 id="ct">${t("create.type_title")}</h2>${where}
      <div class="choices">
        ${this.choice("FileText", t("doc.type.markdown"), t("create.markdown_desc"),
          html`<span class="pv"><i class="h"></i><i></i><i class="s"></i><span class="tbl"><i></i><i></i><i></i><i></i><i></i><i></i></span><i class="xs"></i></span>`, () => this.create("markdown"))}
        ${this.choice("Scale", t("doc.type.adr"), t("create.adr_desc"),
          html`<span class="pv"><i class="h"></i><span class="chips"><b></b><b class="ok"></b></span><span class="sec">${t("adr.context")}</span><i class="s"></i><span class="sec">${t("adr.decision")}</span><i></i></span>`, () => this.create("adr"))}
        ${this.choice("Link", t("doc.type.link"), t("create.link_desc"),
          html`<span class="pv link">${icon("ExternalLink", 22)}<i></i></span>`, () => this.create("link"))}
      </div>
      <div class="row"><button class="ghost" @click=${this.close}>${t("doc.cancel")}</button></div>`;
  }

  render() {
    return html`<dialog aria-labelledby="ct" @close=${() => (this.step = "what")}>${this.body()}</dialog>`;
  }
}
customElements.define("phx-create-dialog", PhxCreateDialog);
