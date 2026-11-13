import { css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentUser } from "../state";

/** Tabs above the personal settings and admin pages. */
export function settingsNav(active: "profile" | "tokens" | "users") {
  const tab = (id: typeof active, href: string, ico: string, label: string) =>
    html`<a href=${href} aria-current=${active === id ? "page" : "false"}>${icon(ico, 15)} ${label}</a>`;
  return html`<nav class="tabs" aria-label=${t("nav.user_menu")}>
    ${tab("profile", "/settings/profile", "UserRound", t("nav.profile"))}
    ${tab("tokens", "/settings/tokens", "KeyRound", t("nav.tokens"))}
    ${currentUser.get()?.isAdmin ? tab("users", "/admin/users", "Users", t("users.title")) : ""}
  </nav>`;
}

export const settingsNavStyles = css`
  .tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin-bottom: 22px; overflow-x: auto; }
  .tabs a { display: inline-flex; align-items: center; gap: 6px; padding: 8px 12px; color: var(--muted); text-decoration: none; font-size: 14px;
    border-bottom: 2px solid transparent; margin-bottom: -1px; white-space: nowrap; transition: color var(--dur-fast) ease; }
  .tabs a[aria-current="page"] { color: var(--fg); border-bottom-color: var(--accent); }
  @media (hover: hover) and (pointer: fine) { .tabs a:hover { color: var(--fg); } }
`;
