// Plugin blocks (ADR-0086): block kinds a project's plugins add. The server
// draws them (the plugin's block_render) as cleaned SVG; a plugin element,
// when there is one, shows the block interactively. This module keeps the
// kinds of the current project and the drawn pictures.
import { signal } from "@lit-labs/signals";
import { api } from "./api";

export interface PluginBlockInfo {
  kind: string; label: string; icon?: string; plugin: string; version: string;
  element?: string; frontend?: string; board?: boolean; example?: string;
}
export interface PluginBlockResult { svg: string; problems: { line: number; message: string; severity: string }[] }

/** kind -> info of the current project's plugin blocks. */
export const pluginBlocks = signal<Map<string, PluginBlockInfo>>(new Map());
let loadedFor = "";

export function pluginBlock(kind: string): PluginBlockInfo | undefined {
  return pluginBlocks.get().get(kind);
}

export async function loadPluginBlocks(project: string, force = false) {
  if (!force && loadedFor === project) return;
  loadedFor = project;
  const list = await api<PluginBlockInfo[]>("GET", `/projects/${project}/plugin-blocks`).catch(() => [] as PluginBlockInfo[]);
  if (loadedFor !== project) return;
  pluginBlocks.set(new Map((list ?? []).map((b) => [b.kind, b])));
}

const cache = new Map<string, PluginBlockResult>();
const pending = new Map<string, Promise<PluginBlockResult>>();
const keyOf = (project: string, kind: string, source: string, width: number) => `${project}\n${kind}\n${width}\n${source}`;

/** Draws a plugin block (cached per project, kind, width and source). */
export function renderPluginBlock(project: string, kind: string, source: string, width = 720, locale = "en"): Promise<PluginBlockResult> {
  const w = Math.round(width / 40) * 40 || 720;
  const k = keyOf(project, kind, source, w);
  const hit = cache.get(k);
  if (hit) return Promise.resolve(hit);
  let p = pending.get(k);
  if (!p) {
    p = api<PluginBlockResult>("POST", `/projects/${project}/plugin-blocks/${encodeURIComponent(kind)}/render`, { source, width: w, locale })
      .then((r) => {
        cache.set(k, r);
        if (cache.size > 300) cache.delete(cache.keys().next().value!);
        // Whiteboards draw from the cache and repaint when a picture arrives.
        dispatchEvent(new CustomEvent("phx-plugin-block-drawn"));
        return r;
      })
      .finally(() => pending.delete(k));
    pending.set(k, p);
  }
  return p;
}

/** A picture already drawn, or undefined (then starts drawing it). */
export function cachedPluginBlock(project: string, kind: string, source: string, width: number, locale = "en"): PluginBlockResult | undefined {
  const w = Math.round(width / 40) * 40 || 720;
  const hit = cache.get(keyOf(project, kind, source, w));
  if (!hit) void renderPluginBlock(project, kind, source, width, locale).catch(() => undefined);
  return hit;
}
