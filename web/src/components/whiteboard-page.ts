import { LitElement, css, html } from "lit";
import { ApiError, api, type Doc, type Project } from "../api";
import { t } from "../i18n";
import { stopWatching, watchProject } from "../project-live";
import { appOrg, currentProject } from "../state";
import { windowTitle } from "../window-title";
import "./whiteboard";

/** A whiteboard as a page of its own (/wb/<project>/<doc>): only the board,
 * filling the window, with live editing as on its document page. */
export class PhxWhiteboardPage extends LitElement {
  static properties = { project: {}, doc: {}, board: { state: true }, error: { state: true } };
  static styles = css`
    :host { display: block; height: 100dvh; background: var(--bg); color: var(--fg); }
    p { padding: 24px; margin: 0; }
    .error { color: var(--bad); }
    .muted { color: var(--muted); }
  `;
  project = "";
  doc = "";
  board?: Doc;
  error = "";
  private loaded = "";

  async willUpdate() {
    const key = `${this.project}/${this.doc}`;
    if (!this.project || !this.doc || key === this.loaded) return;
    this.loaded = key;
    this.board = undefined;
    this.error = "";
    try {
      const [proj, d] = await Promise.all([
        api<Project>("GET", `/projects/${this.project}`),
        api<Doc>("GET", `/projects/${this.project}/documents/${encodeURIComponent(this.doc)}`),
      ]);
      if (this.loaded !== key) return;
      if (d.type !== "whiteboard") {
        this.error = t("wb.not_whiteboard");
        return;
      }
      currentProject.set(proj);
      watchProject(this.project);
      document.title = `${d.title} · ${windowTitle(proj.name, appOrg.get()?.name)}`;
      this.board = d;
    } catch (e) {
      if (this.loaded === key) this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    stopWatching();
  }

  render() {
    if (this.error) return html`<p class="error">${this.error}</p>`;
    const d = this.board;
    if (!d) return html`<p class="muted">${t("app.loading")}</p>`;
    const canEdit = ["editor", "owner"].includes(d.role ?? currentProject.get()?.role ?? "");
    return html`<phx-whiteboard standalone .project=${this.project} .doc=${d} ?canEdit=${canEdit}></phx-whiteboard>`;
  }
}
customElements.define("phx-whiteboard-page", PhxWhiteboardPage);
