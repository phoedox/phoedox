// Pages the server-side browser draws (ADR-0095): only a whiteboard or an
// app, filling the window, without the app's frame. When everything is
// drawn, <html data-ready="1"> tells the browser to take the picture.
// /render/wb/<project>/<doc>?frame=…&theme=dark, /render/app/<project>/<doc>.
import { LitElement, css, html, nothing } from "lit";
import { api, type Doc, type Project } from "../api";
import { currentProject } from "../state";
import type { AppFrameInfo } from "./apps";
import "./whiteboard";
import "./apps";

export class PhxRenderView extends LitElement {
  static properties = { kind: {}, project: {}, doc: {}, d: { state: true }, info: { state: true }, error: { state: true } };
  static styles = css`
    :host { display: block; width: 100vw; height: 100vh; overflow: hidden; background: var(--bg); color: var(--fg); }
    phx-whiteboard { display: block; width: 100%; height: 100%; }
    phx-app-frame { display: block; width: 100%; height: 100%; }
    p { margin: 0; padding: 24px; color: var(--bad); }
  `;
  kind = "";
  project = "";
  doc = "";
  d?: Doc;
  info?: AppFrameInfo;
  error = "";
  private started = false;

  connectedCallback() {
    super.connectedCallback();
    const theme = new URLSearchParams(location.search).get("theme");
    document.documentElement.dataset.theme = theme === "dark" ? "dark" : "light";
  }

  willUpdate() {
    if (this.started || !this.kind || !this.project || !this.doc) return;
    this.started = true;
    void this.load();
  }

  private async load() {
    try {
      const [proj, d] = await Promise.all([
        api<Project>("GET", `/projects/${this.project}`),
        api<Doc>("GET", `/projects/${this.project}/documents/${encodeURIComponent(this.doc)}`),
      ]);
      currentProject.set(proj);
      this.d = d;
      if (this.kind === "app") this.info = await api<AppFrameInfo>("POST", `/projects/${this.project}/apps/${d.id}/open`);
      await this.updateComplete;
      await this.settle();
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      document.documentElement.dataset.error = `${(e as { status?: number }).status ?? ""} ${this.error}`.trim();
      await this.updateComplete;
    }
    document.documentElement.dataset.ready = "1";
  }

  /** Waits until fonts, blocks, diagrams and frames are drawn. */
  private async settle() {
    await document.fonts.ready;
    if (this.kind === "app") {
      const frame = await this.find<HTMLIFrameElement>("phx-app-frame", "iframe");
      if (frame) await new Promise((ok) => { frame.addEventListener("load", ok, { once: true }); setTimeout(ok, 8000); });
      await new Promise((ok) => setTimeout(ok, 1200)); // the app draws its first screen
      return;
    }
    await new Promise((ok) => setTimeout(ok, 1500)); // diagrams and blocks render on their own
    await new Promise((ok) => requestAnimationFrame(() => requestAnimationFrame(ok)));
  }

  private async find<T extends Element>(host: string, sel: string): Promise<T | null> {
    for (let i = 0; i < 50; i++) {
      const h = this.renderRoot.querySelector(host);
      const el = h?.shadowRoot?.querySelector<T>(sel);
      if (el) return el;
      await new Promise((ok) => setTimeout(ok, 100));
    }
    return null;
  }

  render() {
    if (this.error) return html`<p>${this.error}</p>`;
    if (!this.d) return nothing;
    if (this.kind === "app") return this.info ? html`<phx-app-frame .project=${this.project} .doc=${this.d.id} .info=${this.info}
      .dark=${document.documentElement.dataset.theme === "dark"}></phx-app-frame>` : nothing;
    const frame = new URLSearchParams(location.search).get("frame") || "all";
    return html`<phx-whiteboard still .fitTo=${frame} .project=${this.project} .doc=${this.d}></phx-whiteboard>`;
  }
}
customElements.get("phx-render-view") || customElements.define("phx-render-view", PhxRenderView);
