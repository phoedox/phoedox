import { signal } from "@lit-labs/signals";
import type { Project, User } from "./api";
import { detectLocale, setLocale } from "./i18n";

/** Shared app state as signals (decision ADR-0007: signals + context). */
export const currentUser = signal<User | null>(null);
export const currentProject = signal<Project | null>(null);
/** The organization whose own app domain this is (null on the instance's address). */
export const appOrg = signal<{ id: string; slug: string; name: string } | null>(null);
/** Favorites of the current project (project-wide and the user's own). */
export const favorites = signal<import("./api").Favorite[]>([]);
/** Bumped when documents of the current project change, so the sidebar reloads. */
export const docsVersion = signal(0);

/** Content language chosen for the current multilingual project (ADR-0028); "" = main language. */
export const contentLocale = signal("");

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

export type Theme = "system" | "light" | "dark";
/** Colour scheme of this browser; "system" follows the operating system. */
export const theme = signal<Theme>(readTheme());
function readTheme(): Theme {
  try {
    const v = localStorage.getItem("phx.theme");
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}
function applyTheme(v: Theme) {
  if (typeof document === "undefined") return;
  if (v === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = v;
}
applyTheme(theme.get());
export function setTheme(v: Theme) {
  theme.set(v);
  applyTheme(v);
  try {
    localStorage.setItem("phx.theme", v);
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
  return path === "/login" || path === "/forgot" || path === "/signup" || /^\/(invite|reset)\/[^/]+$/.test(path) || /^\/s\/phxs_[^/]+(\/[^/]+)?$/.test(path) || /^\/secret\/[a-z0-9]+$/.test(path);
}

/** Focus mode: no top bar and no sidebar, for reading and writing (per device). */
function readFocus(): boolean {
  try {
    return localStorage.getItem("phx.focus") === "1";
  } catch {
    return false;
  }
}
export const focusMode = signal(readFocus());
export function setFocusMode(on: boolean) {
  focusMode.set(on);
  document.documentElement.classList.toggle("focus", on);
  try {
    localStorage.setItem("phx.focus", on ? "1" : "0");
  } catch {
    // private windows: only for this page
  }
}
document.documentElement.classList.toggle("focus", focusMode.get());

/** System admins can also list other people's projects (per device, off by default). */
const ALL_PROJECTS = "phx.projects.all";
export function showAllProjects(): boolean {
  if (!currentUser.get()?.isAdmin) return false;
  try {
    return localStorage.getItem(ALL_PROJECTS) === "1";
  } catch {
    return false;
  }
}
export function setShowAllProjects(on: boolean) {
  try {
    if (on) localStorage.setItem(ALL_PROJECTS, "1");
    else localStorage.removeItem(ALL_PROJECTS);
  } catch {
    /* storage blocked */
  }
}
/** Address of the project list, with other people's projects when an admin asked for them. */
export const projectsPath = () => (showAllProjects() ? "/projects?all=1" : "/projects");
/** Label of the own role in a project; "System admin" where the rights only come from that. */
export const roleKey = (p: { role: string; adminAccess?: boolean }) =>
  p.adminAccess ? "settings.role.sysadmin" : `settings.role.${["owner", "editor", "reader"].includes(p.role) ? p.role : "reader"}`;
