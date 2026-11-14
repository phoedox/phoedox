// Live updates of a project: one server-sent event stream per open project
// (GET /projects/{p}/live). Components subscribe to kinds of changes and
// reload their data through the API, which checks access again.

export interface ProjectChange { kind: string; subject: string; actor: string }

/** Changes that alter the page tree, decisions, suggestions or the project itself. */
export const LIST_KINDS = /^(document\.(create|delete|restore|archive|update|draft_start|draft_cancel|draft_publish|restrict|translation|translation_delete|skill|skill_removed|share|unshare|restore_version)|decision\.|suggestion\.|project\.(update|image|image_delete|member|member_remove|template)|favorite\.)/;

interface Sub { re: RegExp; fn: (changes: ProjectChange[]) => void; delay: number; pending: ProjectChange[]; timer?: ReturnType<typeof setTimeout> }

const subs = new Set<Sub>();
let source: EventSource | undefined;
let watching = "";

/** Opens the stream for a project (closing the one of another project). */
export function watchProject(slug: string) {
  if (slug === watching) return;
  stopWatching();
  if (!slug || typeof EventSource === "undefined") return;
  watching = slug;
  source = new EventSource(`/api/v1/projects/${encodeURIComponent(slug)}/live`);
  source.addEventListener("change", (e) => {
    try {
      deliver(JSON.parse((e as MessageEvent).data) as ProjectChange);
    } catch {
      /* ignore malformed events */
    }
  });
}

export function stopWatching() {
  source?.close();
  source = undefined;
  watching = "";
}

/** Calls fn with the changes whose kind matches re, at most every delay ms;
 * while the tab is hidden, changes wait until it is visible again.
 * Returns a function that unsubscribes. */
export function onProjectChange(re: RegExp, fn: (changes: ProjectChange[]) => void, delay = 400): () => void {
  const sub: Sub = { re, fn, delay, pending: [] };
  subs.add(sub);
  return () => {
    clearTimeout(sub.timer);
    subs.delete(sub);
  };
}

/** Hands a change to matching subscribers (exported for tests). */
export function deliver(c: ProjectChange) {
  for (const s of subs) {
    if (!s.re.test(c.kind)) continue;
    s.pending.push(c);
    schedule(s);
  }
}

function schedule(s: Sub) {
  if (s.timer !== undefined) return;
  s.timer = setTimeout(() => {
    s.timer = undefined;
    if (typeof document !== "undefined" && document.visibilityState === "hidden") {
      document.addEventListener("visibilitychange", () => schedule(s), { once: true });
      return;
    }
    const batch = s.pending;
    s.pending = [];
    if (batch.length && subs.has(s)) s.fn(batch);
  }, s.delay);
}
