import { css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";

type Tab = "profile" | "account" | "notifications" | "shortcuts" | "security" | "tokens" | "publications" | "users" | "instance" | "backups" | "plugins" | "audit";

/** Tabs above the personal settings and admin pages: two groups (account,
 * administration) that wrap, so every tab stays visible without scrolling. */
export function settingsNav(active: Tab) {
  const tab = (id: Tab, href: string, ico: string, label: string) =>
    html`<a href=${href} aria-current=${active === id ? "page" : "false"}>${icon(ico, 15)} ${label}</a>`;
  const admin = currentUser.get()?.isAdmin;
  return html`<nav class="tabs" aria-label=${t("nav.user_menu")}>
    <div class="group" role="group" aria-label=${t("nav.account")}>
      ${admin ? html`<span class="glabel">${t("nav.account")}</span>` : ""}
      ${tab("profile", "/settings/profile", "UserRound", t("nav.profile"))}
      ${tab("notifications", "/settings/notifications", "Bell", t("notif.title"))}
      ${tab("shortcuts", "/settings/shortcuts", "Keyboard", t("shortcut.title"))}
      ${tab("security", "/settings/security", "ShieldCheck", t("nav.security"))}
      ${tab("tokens", "/settings/tokens", "KeyRound", t("nav.tokens"))}
      ${tab("publications", "/settings/publications", "Globe", t("pub.title_page"))}
      ${tab("account", "/settings/account", "UserRound", t("account.title"))}
    </div>
    ${admin ? html`<div class="group" role="group" aria-label=${t("nav.admin")}>
      <span class="glabel">${t("nav.admin")}</span>
      ${tab("users", "/admin/users", "Users", t("users.title"))}
      ${tab("instance", "/admin/settings", "Settings", t("admin.instance"))}
      ${tab("backups", "/admin/backups", "Archive", t("backups.title"))}
      ${tab("plugins", "/admin/plugins", "Puzzle", t("plugins.title"))}
      ${tab("audit", "/admin/audit", "ScrollText", t("audit.title"))}
    </div>` : ""}
  </nav>`;
}

export const settingsNavStyles = css`
  /* Tabs wrap instead of scrolling: nothing is hidden, and the underline is
     drawn inside each tab so nothing overflows. */
  .tabs { display: grid; gap: 6px; margin-bottom: 22px; }
  .tabs .group { display: flex; flex-wrap: wrap; align-items: center; gap: 2px 4px; box-shadow: inset 0 -1px var(--line); }
  .tabs .glabel { font-size: 11.5px; font-weight: 600; letter-spacing: .04em; text-transform: uppercase; color: var(--muted); padding: 8px 8px 8px 0; min-width: 7.5em; }
  .tabs a { display: inline-flex; align-items: center; gap: 6px; padding: 8px 12px; color: var(--muted); text-decoration: none; font-size: 14px;
    white-space: nowrap; transition: color var(--dur-fast) ease; }
  .tabs a[aria-current="page"] { color: var(--fg); box-shadow: inset 0 -2px var(--accent); }
  @media (hover: hover) and (pointer: fine) { .tabs a:hover { color: var(--fg); } }
  @media (max-width: 640px) { .tabs .glabel { width: 100%; padding-bottom: 0; } }
`;
