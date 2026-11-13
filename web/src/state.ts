import { signal } from "@lit-labs/signals";
import type { Project, User } from "./api";

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
