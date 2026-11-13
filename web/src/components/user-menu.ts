import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";
import { base } from "./shared";

function initials(name: string) {
  const parts = name.trim().split(/\s+/);
  return ((parts[0]?.[0] ?? "") + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase() || "?";
}

/** Account menu in the app bar (menu button pattern, WAI-ARIA). */
export class PhxUserMenu extends SignalWatcher(LitElement) {
  static properties = { open: { state: true } };
  static styles = [base, css`
    :host { position: relative; display: inline-block; }
    .trigger { background: var(--surface); color: var(--fg); border-color: var(--line); padding: 4px 8px 4px 4px; gap: 8px; }
    .avatar { width: 26px; height: 26px; border-radius: 50%; display: grid; place-items: center; background: var(--accent-soft); color: var(--fg);
      font: 600 11px var(--font-mono); }
    .name { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
    @media (max-width: 700px) { .name, .chev { display: none; } .trigger { padding: 3px; } }

    /* Opened a few times a day: short, from the trigger corner. */
    [role="menu"] { position: absolute; right: 0; top: calc(100% + 6px); z-index: 40; min-width: 230px; padding: 6px;
      background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 10px 30px rgb(0 0 0 / .14);
      transform-origin: top right; opacity: 0; transform: scale(.97); visibility: hidden;
      transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out), visibility 0s linear var(--dur-fast); }
    :host([data-open]) [role="menu"] { opacity: 1; transform: none; visibility: visible;
      transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out), visibility 0s; }
    @media (prefers-reduced-motion: reduce) { [role="menu"] { transform: none; } }
    .head { padding: 8px 10px 10px; border-bottom: 1px solid var(--line); margin-bottom: 4px; }
    .head b { display: block; font-size: 14px; }
    .head span { font-size: 12.5px; color: var(--muted); word-break: break-all; }
    [role="menuitem"] { display: flex; align-items: center; gap: 9px; width: 100%; padding: 8px 10px; border-radius: 6px; border: 0;
      background: none; color: var(--fg); font: inherit; font-size: 14px; font-weight: 400; text-decoration: none; cursor: pointer; }
    [role="menuitem"]:focus-visible { outline: none; background: var(--surface2); }
    @media (hover: hover) and (pointer: fine) { [role="menuitem"]:hover { background: var(--surface2); } }
    [role="menuitem"]:active { transform: none; }
    hr { border: 0; border-top: 1px solid var(--line); margin: 4px 0; }
  `];
  open = false;

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener("pointerdown", this.outside);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener("pointerdown", this.outside);
  }

  private outside = (e: PointerEvent) => {
    if (this.open && !e.composedPath().includes(this)) this.setOpen(false);
  };

  private items() {
    return [...this.renderRoot.querySelectorAll<HTMLElement>('[role="menuitem"]')];
  }

  private async setOpen(open: boolean, focus: "first" | "last" | "trigger" | "none" = "none") {
    this.open = open;
    this.toggleAttribute("data-open", open);
    await this.updateComplete;
    if (focus === "first") this.items()[0]?.focus();
    if (focus === "last") this.items().at(-1)?.focus();
    if (focus === "trigger") this.renderRoot.querySelector<HTMLElement>(".trigger")?.focus();
  }

  private onTriggerKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      this.setOpen(true, "first");
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      this.setOpen(true, "last");
    }
  }

  private onMenuKey(e: KeyboardEvent) {
    const items = this.items();
    const i = items.indexOf((this.renderRoot as ShadowRoot).activeElement as HTMLElement);
    const go = (n: number) => {
      e.preventDefault();
      items[(n + items.length) % items.length]?.focus();
    };
    switch (e.key) {
      case "ArrowDown": return go(i + 1);
      case "ArrowUp": return go(i - 1);
      case "Home": return go(0);
      case "End": return go(items.length - 1);
      case "Escape":
        e.preventDefault();
        return this.setOpen(false, "trigger");
      case "Tab":
        return this.setOpen(false);
    }
  }

  private logout() {
    this.setOpen(false);
    this.dispatchEvent(new CustomEvent("logout", { bubbles: true, composed: true }));
  }

  render() {
    const u = currentUser.get();
    if (!u) return "";
    const item = (href: string, ico: string, label: string) =>
      html`<a role="menuitem" tabindex="-1" href=${href} @click=${() => this.setOpen(false)}>${icon(ico, 15)} ${label}</a>`;
    return html`
      <button class="trigger" aria-haspopup="menu" aria-expanded=${this.open} aria-controls="menu" aria-label=${t("nav.user_menu")}
        @click=${() => this.setOpen(!this.open, this.open ? "none" : "first")} @keydown=${this.onTriggerKey}>
        <span class="avatar" aria-hidden="true">${initials(u.name)}</span><span class="name">${u.name}</span><span class="chev">${icon("ChevronDown", 14)}</span>
      </button>
      <div id="menu" role="menu" aria-label=${t("nav.user_menu")} @keydown=${this.onMenuKey}>
        <div class="head" role="presentation"><b>${u.name}</b><span>${u.email}</span></div>
        ${item("/settings/profile", "UserRound", t("nav.profile"))}
        ${item("/settings/security", "ShieldCheck", t("nav.security"))}
        ${item("/settings/tokens", "KeyRound", t("nav.tokens"))}
        ${u.isAdmin ? item("/admin/users", "Users", t("users.title")) : ""}
        <hr role="separator" />
        <button role="menuitem" tabindex="-1" @click=${this.logout}>${icon("LogOut", 15)} ${t("nav.logout")}</button>
      </div>`;
  }
}
customElements.define("phx-user-menu", PhxUserMenu);
