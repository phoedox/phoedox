import { css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";

/** Tabs above the personal settings and admin pages. */
export function settingsNav(active: "profile" | "security" | "tokens" | "publications" | "users" | "instance" | "backups" | "plugins" | "audit") {
  const tab = (id: typeof active, href: string, ico: string, label: string) =>
    html`<a href=${href} aria-current=${active === id ? "page" : "false"}>${icon(ico, 15)} ${label}</a>`;
  return html`<nav class="tabs" aria-label=${t("nav.user_menu")}>
    ${tab("profile", "/settings/profile", "UserRound", t("nav.profile"))}
    ${tab("security", "/settings/security", "ShieldCheck", t("nav.security"))}
    ${tab("tokens", "/settings/tokens", "KeyRound", t("nav.tokens"))}
    ${tab("publications", "/settings/publications", "Globe", t("pub.title_page"))}
    ${currentUser.get()?.isAdmin ? tab("users", "/admin/users", "Users", t("users.title")) : ""}
    ${currentUser.get()?.isAdmin ? tab("instance", "/admin/settings", "Settings", t("admin.instance")) : ""}
    ${currentUser.get()?.isAdmin ? tab("backups", "/admin/backups", "Archive", t("backups.title")) : ""}
    ${currentUser.get()?.isAdmin ? tab("plugins", "/admin/plugins", "Puzzle", t("plugins.title")) : ""}
    ${currentUser.get()?.isAdmin ? tab("audit", "/admin/audit", "ScrollText", t("audit.title")) : ""}
  </nav>`;
}

export const settingsNavStyles = css`
  /* Scrolls sideways on narrow screens without showing a scrollbar; the
     underline is drawn inside the tab so nothing overflows vertically. */
  .tabs { display: flex; gap: 4px; box-shadow: inset 0 -1px var(--line); margin-bottom: 22px; overflow-x: auto; overflow-y: hidden;
    scrollbar-width: none; overscroll-behavior-x: contain; }
  .tabs::-webkit-scrollbar { display: none; }
  .tabs a { display: inline-flex; align-items: center; gap: 6px; padding: 8px 12px; color: var(--muted); text-decoration: none; font-size: 14px;
    white-space: nowrap; flex: none; transition: color var(--dur-fast) ease; }
  .tabs a[aria-current="page"] { color: var(--fg); box-shadow: inset 0 -2px var(--accent); }
  @media (hover: hover) and (pointer: fine) { .tabs a:hover { color: var(--fg); } }
`;
