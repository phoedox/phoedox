import { signal } from "@lit-labs/signals";
import type { Project, User } from "./api";
import { detectLocale, setLocale } from "./i18n";

/** Shared app state as signals (see docs/decisions.md: signals + context). */
export const currentUser = signal<User | null>(null);
export const currentProject = signal<Project | null>(null);
/** Bumped when documents of the current project change, so the sidebar reloads. */
export const docsVersion = signal(0);

/** Navigates within the app (the router listens to popstate). */
export function navigate(path: string) {
  if (location.pathname === path) return;
  history.pushState({}, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

/** Stores the signed-in user and applies their language (or the browser's). */
export function signedIn(u: User | null) {
  setLocale(u?.locale || detectLocale());
  currentUser.set(u);
}

/** Where to continue after signing in (?next=…), local paths only. */
export function nextPath(): string {
  const n = new URLSearchParams(location.search).get("next") ?? "/";
  return n.startsWith("/") && !n.startsWith("//") && !n.startsWith("/\\") ? n : "/";
}

/** The login URL that returns to the current page. */
export function loginURL(): string {
  const here = location.pathname + location.search;
  return here === "/" || isPublicPath(location.pathname) ? "/login" : `/login?next=${encodeURIComponent(here)}`;
}

/** Pages that work without a session. */
export function isPublicPath(path: string) {
  return path === "/login" || path === "/forgot" || /^\/(invite|reset)\/[^/]+$/.test(path);
}
