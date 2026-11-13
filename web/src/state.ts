import { signal } from "@lit-labs/signals";
import type { Project, User } from "./api";
import { detectLocale, setLocale } from "./i18n";

/** Shared app state as signals (decision ADR-0007: signals + context). */
export const currentUser = signal<User | null>(null);
export const currentProject = signal<Project | null>(null);
/** Favorites of the current project (project-wide and the user's own). */
export const favorites = signal<import("./api").Favorite[]>([]);
/** Bumped when documents of the current project change, so the sidebar reloads. */
export const docsVersion = signal(0);

/** Pages use the full width instead of a centred reading column (per browser). */
export const wideReading = signal(readWide());
function readWide(): boolean {
  try {
    return localStorage.getItem("phx.wide") === "1";
  } catch {
    return false;
  }
}
export function setWideReading(on: boolean) {
  wideReading.set(on);
  try {
    localStorage.setItem("phx.wide", on ? "1" : "0");
  } catch {
    /* private mode: only for this visit */
  }
}

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
  return path === "/login" || path === "/forgot" || /^\/(invite|reset)\/[^/]+$/.test(path) || /^\/s\/phxs_[^/]+(\/[^/]+)?$/.test(path);
}
