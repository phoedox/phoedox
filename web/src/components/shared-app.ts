// The page of a public app (apps-definition "Sharing"): people without an
// account run the app; reads come from the snapshot the owner released.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { type AppFrameInfo, type AppSource } from "./apps";
import "./apps";
import { signedIn, type PhxAppCopy } from "./app-extras";

interface SharedApp { title: string; project: string; url: string; isolated?: boolean; level: number; sources: AppSource[]; snapshotAt?: string; ai: boolean; locale: string }

export class PhxSharedApp extends LitElement {
  static properties = { token: {}, app: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: grid; grid-template-rows: auto 1fr; height: 100dvh; background: var(--bg); }
    header { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--surface); }
    header h1 { font-size: 16px; margin: 0; }
    header .sp { flex: 1; }
    .muted { color: var(--muted); font-size: 13px; }
    .box { min-height: 0; }
    phx-app-frame { height: 100%; }
    .empty { padding: 48px 16px; text-align: center; }
    a.brand { display: inline-flex; gap: 6px; align-items: center; color: var(--muted); font-size: 13px; text-decoration: none; }
    a.brand img { width: 18px; height: 18px; }
  `];

  token = "";
  app?: SharedApp;
  error = "";

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("token") && this.token) void this.load();
  }

  private async load() {
    try {
      this.app = await api<SharedApp>("GET", `/shared-apps/${encodeURIComponent(this.token)}`);
      document.title = `${this.app.title} · ${this.app.project}`;
    } catch (e) {
      this.error = e instanceof ApiError && e.status !== 404 ? e.text : t("appshare.gone");
    }
  }

  render() {
    if (this.error) return html`<p class="empty muted">${icon("EyeOff", 18)} ${this.error}</p>`;
    const a = this.app;
    if (!a) return nothing;
    const info: AppFrameInfo = { url: a.url, isolated: a.isolated, level: a.level, granted: a.level, sources: a.sources, ai: a.ai, storageMb: 0, errors: [], canEdit: false, snapshotAt: a.snapshotAt };
    return html`<header>
        <h1>${a.title}</h1><span class="muted">${a.project}</span>
        <span class="sp"></span>
        ${a.snapshotAt ? html`<span class="chip" title=${t("appshare.visitor_snapshot_help")}>${icon("Database", 12)} ${t("appshare.visitor_snapshot", { date: formatDate(a.snapshotAt) })}</span>` : nothing}
        ${signedIn() ? html`<button class="ghost" title=${t("appcopy.help")} @click=${() => void this.renderRoot.querySelector<PhxAppCopy>("phx-app-copy")?.open()}>${icon("CopyPlus", 14)} ${t("appcopy.button")}</button>` : nothing}
        <a class="brand" href="/" rel="noopener"><img src="/logo.svg" alt="" />${t("appshare.made_with")}</a>
      </header>
      <div class="box"><phx-app-frame .shared=${this.token} .info=${info}></phx-app-frame></div>
      ${signedIn() ? html`<phx-app-copy .token=${this.token} .appTitle=${a.title}></phx-app-copy>` : nothing}`;
  }
}
customElements.get("phx-shared-app") || customElements.define("phx-shared-app", PhxSharedApp);
