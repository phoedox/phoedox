import { api } from "./api";
import { hasKey, t } from "./i18n";

export type AttrType = "text" | "number" | "bool" | "date" | "ip" | "url" | "select";
export const attrTypes: readonly AttrType[] = ["text", "number", "bool", "date", "ip", "url", "select"];
export interface Attr { name: string; label?: string; type: AttrType; unit?: string; options?: string[]; required?: boolean; project?: boolean }
export interface Relation { rel: string; to: string[]; project?: boolean }
export interface Kind {
  id: string; name?: string; description?: string; group: string; icon: string; source: "core" | "project" | `plugin:${string}`;
  attrs: Attr[]; relations?: Relation[]; count: number;
}
export interface Model { kinds: Kind[]; generic: string[]; unknown?: Record<string, number> }
export interface KindInput { name?: string; description?: string; icon?: string; attrs?: Attr[]; relations?: Relation[] }

/** Icons a project may pick for own kinds (same list as the server). */
export const kindIcons = ["Shapes", "Tag", "CircleDot", "Box", "Boxes", "Cpu", "Package", "Workflow", "Database", "Server", "Monitor",
  "Plug", "PlugZap", "Lightbulb", "Router", "Wifi", "Network", "Cable", "Phone", "UserRound", "Layers", "DoorOpen", "Container"];

/** Group colors come from tokens; own kinds use the project accent. */
export const groupColor: Record<string, string> = {
  general: "var(--muted)", building: "var(--signal)", network: "var(--wire)", infrastructure: "var(--fg)",
  power: "var(--bad)", software: "var(--good)", custom: "var(--accent)", plugin: "var(--signal)",
};

const tr = (key: string, fallback: string) => (hasKey(key) ? t(key) : fallback);

export const kindName = (k: Kind | undefined, id = k?.id ?? "") => k?.name || tr(`kind.${id}`, id);
export const groupName = (g: string) => tr(`kindgroup.${g}`, g);
export const attrLabel = (a: Attr) => a.label || tr(`attr.${a.name}`, a.name.replace(/_/g, " "));
export const relLabel = (rel: string) => tr(`rel.${rel}`, rel.replace(/_/g, " "));
export const optLabel = (o: string) => tr(`opt.${o}`, o);

/** Value of an attribute for display, with unit and translated choices. */
export function formatValue(a: Attr | undefined, v: unknown): string {
  if (v === null || v === undefined || v === "") return "";
  if (!a) return typeof v === "object" ? JSON.stringify(v) : String(v);
  switch (a.type) {
    case "bool": return t(v ? "common.yes" : "common.no");
    case "number": return `${Number(v).toLocaleString()}${a.unit ? ` ${a.unit}` : ""}`;
    case "select": return optLabel(String(v));
    default: return String(v);
  }
}

export const findKind = (m: Model | undefined, id: string) => m?.kinds.find((k) => k.id === id);

/** Relations allowed from one kind to another (mirrors core.Model.AllowedRels). */
export function allowedRels(m: Model, from: string, to: string): string[] {
  const out = [...m.generic];
  for (const r of findKind(m, from)?.relations ?? []) {
    if ((r.to.includes(to) || r.to.includes("*")) && !out.includes(r.rel)) out.push(r.rel);
  }
  return out;
}

/** Turns a display name into a kind or attribute id: "Wechsel-Richter" -> "wechsel_richter". */
export function toID(name: string): string {
  return name.toLowerCase().replace(/ä/g, "ae").replace(/ö/g, "oe").replace(/ü/g, "ue").replace(/ß/g, "ss")
    .replace(/[^a-z0-9]+/g, "_").replace(/^[^a-z]+|_+$/g, "").slice(0, 40);
}

const cache = new Map<string, Promise<Model>>();

/** Data model of a project, cached until it changes. */
export function loadModel(project: string, fresh = false): Promise<Model> {
  if (fresh || !cache.has(project)) {
    const p = api<Model>("GET", `/projects/${project}/model`);
    cache.set(project, p);
    p.catch(() => cache.delete(project));
  }
  return cache.get(project)!;
}
export const forgetModel = (project: string) => cache.delete(project);
