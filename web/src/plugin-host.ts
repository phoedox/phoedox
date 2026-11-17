import { signal } from "@lit-labs/signals";
import { api, type ProjectPlugin } from "./api";
import { iconSVG } from "./icon";

/** An icon as a DOM element. */
function iconElement(name: string, size: number): SVGElement {
  const t = document.createElement("template");
  t.innerHTML = iconSVG(name, size);
  return t.content.firstElementChild as SVGElement;
}
import { toast } from "./components/toasts";
import { loadPluginBlocks } from "./plugin-blocks";
import { locale } from "./i18n";
import { currentProject, navigate } from "./state";
import { CLIP_PREFIX } from "./whiteboard";
import { blockState } from "./blocks/util";

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
/** Plugin elements per entry kind (several plugins may add one each). */
export const pluginViews = signal<Record<string, string[]>>({});

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
      // content: further fields of the documents API (markdown, adr …); type and title win.
      create: async (type: string, title?: string, content?: Record<string, unknown>) =>
        (await api<{ document: { id: string; slug: string } }>("POST", `/projects/${project()}/documents`, { ...(content ?? {}), type, title: title ?? "" })).document,
    },
    // Read-only helpers with the reader's rights (plugin blocks, ADR-0086).
    tasks: {
      list: (board: string) => api<unknown[]>("GET", `/projects/${project()}/tasks?board=${encodeURIComponent(board)}`),
    },
    blocks: {
      // The reader's state of a block, as core blocks keep it (block_states).
      state(ref: { doc: string; key: string }) {
        const key = String(ref?.key ?? "");
        if (!/^[A-Za-z0-9-]{1,40}$/.test(key)) throw new Error("a block key is 1–40 letters, digits and -");
        let p = "";
        try { p = project(); } catch { /* published or preview: kept in the browser */ }
        return blockState({ project: p, doc: String(ref?.doc ?? ""), key, locale: locale() });
      },
      render: (kind: string, source: string, width = 720) =>
        api<{ svg: string; problems: { line: number; message: string; severity: string }[] }>("POST", `/projects/${project()}/plugin-blocks/${encodeURIComponent(kind)}/render`, { source, width, locale: document.documentElement.lang || "en" }),
    },
    tool: <T>(name: string, input: unknown) => api<T>("POST", `/projects/${project()}/plugins/${pluginId}/tools/${encodeURIComponent(name)}`, input ?? {}),
    ui: {
      toast: (message: string, kind?: string) => (kind === "error" ? toast(String(message), "error") : toast(String(message))),
      locale: () => locale(),
      // Only pages of the current project: "d/<slug>", "e/<entry>", "board" …
      open(path: string) {
        const rest = String(path).replace(/^\/+/, "");
        if (!/^[A-Za-z0-9_\-\/?=&.%]*$/.test(rest) || rest.includes("..")) return;
        navigate(`/p/${project()}/${rest}`);
      },
      // The whiteboard's own clipboard format; pasting checks the elements like any paste.
      copyToWhiteboard: (elements: unknown[]) => navigator.clipboard.writeText(CLIP_PREFIX + JSON.stringify(Array.isArray(elements) ? elements : [])),
    },
    commands: {
      add(cmd: Command) {
        const p = project();
        pluginCommands.set([...pluginCommands.get().filter((c) => !(c.plugin === pluginId && c.id === cmd.id)), { ...cmd, plugin: pluginId, project: p }]);
      },
    },
    // A real SVG element: plugins bundle their own Lit, and templates or
    // directives of the host's Lit do not render there.
    icons: { render: (name: string, size = 16) => iconElement(pascal(name), size) },
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
  void loadPluginBlocks(slug, true);
  const list = await api<ProjectPlugin[]>("GET", `/projects/${slug}/plugins`).catch(() => [] as ProjectPlugin[]);
  const views: Record<string, string[]> = {};
  for (const p of (list ?? []).filter((x) => x.enabled && x.frontend)) {
    const def = await load(p);
    if (current !== slug) return;
    for (const [kind, tag] of Object.entries(def?.views ?? {})) {
      // Own kinds and core kinds; the element decides whether an entry is its business.
      if (tagRe.test(tag) && customElements.get(tag) && /^[a-z][a-z0-9_]{0,39}$/.test(kind)) (views[kind] ??= []).push(tag);
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
