import "./doc-view";
import { LitElement, css, html } from "lit";
import { api, type Project } from "../api";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { stopWatching, watchProject } from "../project-live";
import { currentProject } from "../state";
import { base } from "./shared";

/** Wall display (kiosk): one page full screen without the app around it.
 * It updates live, keeps the screen awake and reloads now and then in case
 * the connection was lost. ?scale=1.4 enlarges the text, ?refresh=15 sets
 * the reload in minutes. */
export class PhxKiosk extends LitElement {
  static properties = { project: {}, doc: {}, now: { state: true }, idle: { state: true }, stamp: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; min-height: 100vh; background: var(--bg); }
    .page { zoom: var(--scale, 1.25); padding: 18px 28px 40px; max-width: none; }
    /* A whiteboard fills the screen itself: no zoom, no margins, no page scrolling. */
    .page:has(> phx-doc-view[fill]) { zoom: 1; padding: 0; }
    :host(:has(phx-doc-view[fill])) { height: 100dvh; overflow: hidden; }
    .bar { position: fixed; top: 10px; right: 12px; z-index: 30; display: flex; gap: 10px; align-items: center; font-size: 13px; color: var(--muted);
      background: color-mix(in srgb, var(--surface) 88%, transparent); border: 1px solid var(--line); border-radius: 999px; padding: 4px 6px 4px 12px;
      transition: opacity var(--dur, 200ms) ease; }
    .bar.idle { opacity: 0; pointer-events: none; }
    .bar b { font-variant-numeric: tabular-nums; color: var(--fg); font-size: 15px; }
    .bar a { display: inline-flex; padding: 4px 8px; border-radius: 999px; }
    :host(.hide-cursor) { cursor: none; }
    @media (prefers-reduced-motion: reduce) { .bar { transition: none; } }
  `];
  project = "";
  doc = "";
  now = new Date();
  idle = false;
  stamp = new Date();
  error = "";
  private timers: ReturnType<typeof setInterval>[] = [];
  private idleTimer?: ReturnType<typeof setTimeout>;
  private lock?: { release(): Promise<void> };
  private q = new URLSearchParams(location.search);

  async connectedCallback() {
    super.connectedCallback();
    const scale = Number(this.q.get("scale"));
    if (scale >= 0.5 && scale <= 3) this.style.setProperty("--scale", String(scale));
    try {
      currentProject.set(await api<Project>("GET", `/projects/${this.project}`));
    } catch {
      this.error = t("error.not_found");
      return;
    }
    watchProject(this.project);
    this.timers.push(setInterval(() => (this.now = new Date()), 15_000));
    const minutes = Math.min(Math.max(Number(this.q.get("refresh")) || 10, 1), 240);
    this.timers.push(setInterval(() => this.refresh(), minutes * 60_000));
    void this.keepAwake();
    document.addEventListener("visibilitychange", this.onVisible);
    window.addEventListener("pointermove", this.wake);
    window.addEventListener("pointerdown", this.wake);
    this.wake();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.timers.forEach(clearInterval);
    clearTimeout(this.idleTimer);
    void this.lock?.release().catch(() => {});
    stopWatching();
    document.removeEventListener("visibilitychange", this.onVisible);
    window.removeEventListener("pointermove", this.wake);
    window.removeEventListener("pointerdown", this.wake);
  }

  /** The screen stays on while the display is visible (Screen Wake Lock). */
  private async keepAwake() {
    try {
      const wl = (navigator as Navigator & { wakeLock?: { request(type: "screen"): Promise<{ release(): Promise<void> }> } }).wakeLock;
      this.lock = await wl?.request("screen");
    } catch {
      /* not supported or not allowed: the display may dim */
    }
  }

  private onVisible = () => {
    if (document.visibilityState === "visible") {
      void this.keepAwake();
      this.refresh();
    }
  };

  /** Controls and the pointer disappear after a few seconds without input. */
  private wake = () => {
    this.idle = false;
    this.classList.remove("hide-cursor");
    clearTimeout(this.idleTimer);
    this.idleTimer = setTimeout(() => {
      this.idle = true;
      this.classList.add("hide-cursor");
    }, 4000);
  };

  private refresh() {
    void this.renderRoot.querySelector<HTMLElement & { reload(): Promise<void> }>("phx-doc-view")?.reload().then(() => (this.stamp = new Date()));
  }

  render() {
    if (this.error) return html`<p class="error" style="padding:24px">${this.error}</p>`;
    const time = this.now.toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit" });
    return html`
      <div class=${this.idle ? "bar idle" : "bar"} role="status">
        <b>${time}</b><span>${t("kiosk.as_of", { date: formatDate(this.stamp.toISOString()) })}</span>
        <a href="/p/${this.project}/d/${this.doc}" title=${t("kiosk.leave")} aria-label=${t("kiosk.leave")}>${icon("X", 15)}</a>
      </div>
      <div class="page"><phx-doc-view kiosk .project=${this.project} .docSlug=${this.doc}></phx-doc-view></div>`;
  }
}
customElements.define("phx-kiosk", PhxKiosk);
