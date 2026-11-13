import { signal } from "@lit-labs/signals";
import { api, type ProjectPlugin } from "./api";
import { icon } from "./icon";
import { toast } from "./components/toasts";
import { currentProject } from "./state";

/**
 * Loads the frontend modules of a project's plugins and gives them the SDK
 * (phoedox-sdk: js/src/index.ts). Plugin code runs in the page with the
 * rights of the signed-in user; the instance admin decides which plugins are
 * installed. Elements are defined once per plugin version; the SDK always
 * acts in the current project.
 */

interface Entity { id: string; kind: string; slug: string; name: string; attrs: Record<string, unknown> }
interface Command { id: string; label: string; run(): void | Promise<void> }
interface PluginDef {
  elements?: Record<string, CustomElementConstructor>;
  blocks?: { id: string; element: string; icon?: string }[];
  /** Elements shown on entries of a kind: kind -> element tag. */
  views?: Record<string, string>;
  onReady?(sdk: unknown): void | Promise<void>;
}

export interface PluginCommand extends Command { plugin: string; project: string }

/** Commands plugins add to the command palette (current project). */
export const pluginCommands = signal<PluginCommand[]>([]);
/** kind -> element tag of the current project's plugins. */
export const pluginViews = signal<Record<string, string>>({});

const tagRe = /^phx-[a-z0-9]+(-[a-z0-9]+)*$/;
const loaded = new Map<string, Promise<PluginDef | undefined>>();
const sdks = new Map<string, unknown>();

const project = () => {
  const p = currentProject.get();
  if (!p) throw new Error("no project");
  return p.slug;
};

const pascal = (name: string) => name.split("-").filter(Boolean).map((s) => s[0].toUpperCase() + s.slice(1)).join("");

function makeSdk(pluginId: string) {
  return {
    graph: {
      get: async (id: string) => (await api<{ entity: Entity }>("GET", `/projects/${project()}/entities/${encodeURIComponent(id)}`)).entity,
      watch(id: string, cb: (e: Entity) => void) {
        let stopped = false;
        const tick = async () => {
          if (stopped || document.hidden) return;
          try {
            cb((await api<{ entity: Entity }>("GET", `/projects/${project()}/entities/${encodeURIComponent(id)}`)).entity);
          } catch { /* keep the last value */ }
        };
        void tick();
        const timer = window.setInterval(tick, 10_000);
        return () => {
          stopped = true;
          clearInterval(timer);
        };
      },
    },
    docs: {
      create: async (type: string, title?: string) =>
        (await api<{ document: { id: string } }>("POST", `/projects/${project()}/documents`, { type, title: title ?? "" })).document,
    },
    tool: <T>(name: string, input: unknown) => api<T>("POST", `/projects/${project()}/plugins/${pluginId}/tools/${encodeURIComponent(name)}`, input ?? {}),
    ui: { toast: (message: string) => toast(String(message)) },
    commands: {
      add(cmd: Command) {
        const p = project();
        pluginCommands.set([...pluginCommands.get().filter((c) => !(c.plugin === pluginId && c.id === cmd.id)), { ...cmd, plugin: pluginId, project: p }]);
      },
    },
    icons: { render: (name: string, size = 16) => icon(pascal(name), size) },
  };
}

async function load(p: ProjectPlugin): Promise<PluginDef | undefined> {
  const key = `${p.id}@${p.version}`;
  if (!loaded.has(key)) {
    loaded.set(key, (async () => {
      try {
        const mod = await import(/* @vite-ignore */ p.frontend!);
        const def: PluginDef = mod.default ?? {};
        const sdk = makeSdk(p.id);
        sdks.set(p.id, sdk);
        for (const [tag, ctor] of Object.entries(def.elements ?? {})) {
          if (!tagRe.test(tag) || customElements.get(tag)) continue;
          // Inject the SDK before the element connects.
          customElements.define(tag, class extends (ctor as unknown as { new (): HTMLElement }) {
            constructor() {
              super();
              (this as unknown as { sdk: unknown }).sdk = sdk;
            }
          });
        }
        await def.onReady?.(sdk);
        return def;
      } catch (e) {
        console.warn(`plugin ${p.id} failed to load`, e);
        return undefined;
      }
    })());
  }
  return loaded.get(key)!;
}

let current = "";

/** Loads the plugins of a project (call when the project changes). */
export async function loadProjectPlugins(slug: string) {
  if (slug === current) return;
  current = slug;
  pluginCommands.set([]);
  pluginViews.set({});
  const list = await api<ProjectPlugin[]>("GET", `/projects/${slug}/plugins`).catch(() => [] as ProjectPlugin[]);
  const views: Record<string, string> = {};
  for (const p of (list ?? []).filter((x) => x.enabled && x.frontend)) {
    const def = await load(p);
    if (current !== slug) return;
    for (const [kind, tag] of Object.entries(def?.views ?? {})) {
      if (tagRe.test(tag) && customElements.get(tag) && p.kinds.includes(kind)) views[kind] = tag;
    }
  }
  pluginViews.set(views);
}

/** Forget the loaded project (e.g. after enabling a plugin). */
export function reloadProjectPlugins() {
  const slug = current;
  current = "";
  if (slug) void loadProjectPlugins(slug);
}
