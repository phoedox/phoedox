import { LitElement, css, html } from "lit";
import { api } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import type { AppNotification } from "../notifications";
import { navigate } from "../state";
import { base } from "./shared";

const kindIcon: Record<string, string> = {
  "comment.mention": "AtSign", "comment.reply": "MessageSquare", "check.fail": "TriangleAlert", "check.recover": "CircleCheck", "task.assigned": "ListChecks",
};

/** Bell in the top bar: unread count and the newest notifications. */
export class PhxNotificationBell extends LitElement {
  static properties = { items: { state: true }, unread: { state: true }, open: { state: true } };
  static styles = [base, css`
    :host { position: relative; display: inline-flex; }
    button.bell { position: relative; padding: 6px 8px; color: var(--muted); }
    .count { position: absolute; top: 1px; right: 1px; min-width: 16px; height: 16px; padding: 0 4px; box-sizing: border-box; border-radius: 999px;
      background: var(--accent); color: var(--on-color); font: 600 10px/16px var(--font-body); text-align: center; }
    .panel { position: absolute; right: 0; top: calc(100% + 6px); width: min(380px, calc(100vw - 24px)); max-height: min(520px, 70vh); overflow-y: auto; z-index: 20;
      background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: var(--shadow, 0 10px 30px rgb(0 0 0 / 14%));
      transform-origin: top right; transition: opacity 150ms var(--ease-out), transform 150ms var(--ease-out);
      @starting-style { opacity: 0; transform: scale(.97); } }
    @media (prefers-reduced-motion: reduce) { .panel { transition: opacity 150ms ease; @starting-style { opacity: 0; transform: none; } } }
    .head { display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-bottom: 1px solid var(--line); position: sticky; top: 0; background: var(--surface); }
    .head b { margin-right: auto; font-size: 14px; }
    .head button, .head a { font-size: 12.5px; padding: 3px 8px; }
    ul { list-style: none; margin: 0; padding: 4px; }
    li a { display: grid; grid-template-columns: 18px 1fr; gap: 2px 10px; padding: 8px; border-radius: 8px; color: var(--fg); text-decoration: none; }
    @media (hover: hover) and (pointer: fine) { li a:hover { background: var(--surface2); } }
    li a .ico { margin-top: 2px; color: var(--muted); }
    li.unread a .ico { color: var(--accent); }
    li.unread .what { font-weight: 600; }
    .what { font-size: 13.5px; }
    .sub { grid-column: 2; font-size: 12.5px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .empty { padding: 18px 14px; color: var(--muted); font-size: 13.5px; }
  `];
  items: AppNotification[] = [];
  unread = 0;
  open = false;
  private timer?: number;
  private onVisible = () => document.visibilityState === "visible" && this.load();
  private onOutside = (e: Event) => {
    if (this.open && !e.composedPath().includes(this)) this.open = false;
  };
  private onKey = (e: KeyboardEvent) => {
    if (this.open && e.key === "Escape") {
      this.open = false;
      this.renderRoot.querySelector<HTMLButtonElement>("button.bell")?.focus();
    }
  };

  connectedCallback() {
    super.connectedCallback();
    this.load();
    this.timer = window.setInterval(() => this.load(), 60_000);
    document.addEventListener("visibilitychange", this.onVisible);
    document.addEventListener("click", this.onOutside);
    document.addEventListener("keydown", this.onKey);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearInterval(this.timer);
    document.removeEventListener("visibilitychange", this.onVisible);
    document.removeEventListener("click", this.onOutside);
    document.removeEventListener("keydown", this.onKey);
  }

  async load() {
    const r = await api<{ items: AppNotification[]; unread: number }>("GET", "/me/notifications?limit=20").catch(() => undefined);
    if (r) [this.items, this.unread] = [r.items, r.unread];
  }

  private async readAll() {
    await api("POST", "/me/notifications/read", { ids: [] }).catch(() => {});
    await this.load();
  }

  private async go(e: Event, n: AppNotification) {
    e.preventDefault();
    this.open = false;
    if (!n.readAt) {
      await api("POST", "/me/notifications/read", { ids: [n.id] }).catch(() => {});
      this.load();
    }
    if (!n.link) return;
    const [path, hash] = n.link.split("#");
    navigate(path);
    if (hash) history.replaceState(history.state, "", `${path}#${hash}`);
  }

  private label(n: AppNotification) {
    return t(`notif.${n.kind}` as any, { actor: n.actorName ?? "" });
  }

  render() {
    return html`<button class="ghost bell" aria-haspopup="true" aria-expanded=${this.open ? "true" : "false"}
        aria-label=${this.unread ? t("notif.button_unread", { count: this.unread }) : t("notif.button")} title=${t("notif.button")}
        @click=${() => { this.open = !this.open; if (this.open) this.load(); }}>
        ${icon("Bell", 16)}${this.unread ? html`<span class="count" aria-hidden="true">${this.unread > 99 ? "99+" : this.unread}</span>` : ""}</button>
      ${this.open ? html`<div class="panel" role="region" aria-label=${t("notif.button")}>
        <div class="head"><b>${t("notif.title")}</b>
          ${this.unread ? html`<button class="ghost" @click=${this.readAll}>${t("notif.read_all")}</button>` : ""}
          <a class="btn ghost" href="/settings/notifications" @click=${() => (this.open = false)} aria-label=${t("notif.settings")} title=${t("notif.settings")}>${icon("Settings", 14)}</a></div>
        ${this.items.length ? html`<ul>${this.items.map((n) => html`<li class=${n.readAt ? "" : "unread"}>
          <a href=${n.link ?? "#"} @click=${(e: Event) => this.go(e, n)}>${icon(kindIcon[n.kind] ?? "Bell", 15)}
            <span class="what">${this.label(n)}: ${n.title}</span>
            <span class="sub">${n.body ? html`${n.body} · ` : ""}${formatDate(n.createdAt)}</span></a></li>`)}</ul>`
          : html`<p class="empty">${t("notif.empty")}</p>`}
      </div>` : ""}`;
  }
}
customElements.define("phx-notification-bell", PhxNotificationBell);
