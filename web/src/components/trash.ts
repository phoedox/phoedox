import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import { fileIcon } from "../files";
import { toast } from "./toasts";

interface TrashItem { id: string; kind: "document" | "entity" | "file"; type: string; slug: string; title: string; deletedAt: string }

const typeIcon: Record<string, string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot" };

/** Deleted documents and entries of a project. */
export class PhxTrash extends LitElement {
  static properties = { project: {}, items: { state: true }, days: { state: true }, confirming: { state: true } };
  static styles = [base, css`
    ul { list-style: none; padding: 0; margin: 16px 0 0; }
    li { display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap; padding: 12px 0; border-bottom: 1px solid var(--line); }
    .what { display: flex; gap: 10px; align-items: center; min-width: 0; }
    .what .sub { font-size: 13px; color: var(--muted); }
    .acts { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; } .acts button { font-size: 13px; padding: 5px 10px; }
  `];
  project = "";
  items?: TrashItem[];
  days = 30;
  confirming = "";
  private loaded = "";

  async willUpdate() {
    if (this.project && this.project !== this.loaded) {
      this.loaded = this.project;
      await this.load();
    }
  }

  private async load() {
    try {
      const res = await api<{ items: TrashItem[]; days: number }>("GET", `/projects/${this.project}/trash`);
      this.items = res.items;
      this.days = res.days;
    } catch (e) {
      this.items = [];
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async restore(it: TrashItem) {
    try {
      await api("POST", `/projects/${this.project}/trash/${it.id}/restore`);
      toast(t("doc.restored"));
      docsVersion.set(docsVersion.get() + 1);
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private async purge(it: TrashItem) {
    this.confirming = "";
    try {
      await api("DELETE", `/projects/${this.project}/trash/${it.id}`);
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const owner = currentProject.get()?.role === "owner";
    return html`<div class="enter">
      <h1>${t("trash.title")}</h1>
      <p class="muted">${this.days > 0 ? t("trash.intro", { days: this.days }) : t("trash.intro_forever")}</p>
      ${!this.items ? html`<p class="muted">${t("app.loading")}</p>` : this.items.length === 0 ? html`<p class="muted">${t("trash.empty")}</p>` : html`<ul>
        ${this.items.map((it) => html`<li>
          <div class="what">${icon(it.kind === "entity" ? "Box" : it.kind === "file" ? fileIcon(it.type) : typeIcon[it.type] ?? "FileText", 16)}
            <div><b>${it.title}</b><div class="sub">${it.kind === "entity" ? `${t("trash.entity")} · ${it.type} · ` : it.kind === "file" ? `${t("trash.file")} · ` : ""}${t("trash.deleted", { date: formatDate(it.deletedAt) })}</div></div></div>
          <div class="acts">
            ${this.confirming === it.id
              ? html`<span class="muted" style="font-size:13px">${t("trash.purge_confirm", { title: it.title })}</span>
                  <button class="danger" @click=${() => this.purge(it)}>${t("trash.purge")}</button>
                  <button class="ghost" @click=${() => (this.confirming = "")}>${t("doc.cancel")}</button>`
              : html`<button class="ghost" @click=${() => this.restore(it)}>${t("trash.restore")}</button>
                  ${owner ? html`<button class="danger" @click=${() => (this.confirming = it.id)}>${t("trash.purge")}</button>` : ""}`}
          </div>
        </li>`)}
      </ul>`}
    </div>`;
  }
}
customElements.define("phx-trash", PhxTrash);
