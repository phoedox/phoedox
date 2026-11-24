// All Mermaid diagram kinds with a form, and the gallery of kinds this
// Mermaid version can draw.
import { splitSource, type MermaidKind, type Model } from "./core";
import { packet, pie, quadrant, radar, sankey, xychart } from "./data";
import { agentflow, flowchart, swimlane } from "./flow";
import { architecture, c4, classDiagram, er, eventmodeling, requirement, state, usecase, wardley } from "./graphs";
import { cynefin, gitGraph, ishikawa, journey, kanban, mindmap, railroad, timeline, treeView, treemap, venn } from "./lists";
import { sequence } from "./sequence";
import { GANTT_EXAMPLE } from "../mermaid-gantt";

export type { MermaidKind, Model } from "./core";

/** Kinds with a generic form, in the order of the gallery. */
export const MERMAID_KINDS: MermaidKind[] = [
  flowchart, sequence, classDiagram, state, er, journey, pie, quadrant, requirement, gitGraph, c4, mindmap, timeline,
  sankey, xychart, packet, kanban, architecture, radar, treemap, venn, ishikawa, wardley, cynefin, treeView, swimlane,
  usecase, eventmodeling, agentflow, railroad,
];

/** Gallery entries: the form kinds plus Gantt (own form) and kinds without a form. */
export interface GalleryEntry { id: string; label: string; example: string; form: boolean }

export const GALLERY: GalleryEntry[] = [
  ...MERMAID_KINDS.slice(0, 2).map((k) => ({ id: k.id, label: k.label, example: k.example, form: true })),
  { id: "gantt", label: "mmd.kind.gantt", example: GANTT_EXAMPLE, form: true },
  ...MERMAID_KINDS.slice(2).map((k) => ({ id: k.id, label: k.label, example: k.example, form: true })),
  { id: "block", label: "mmd.kind.block", form: false, example: `block-beta
  columns 3
  internet["Internet"] router["Router"] nas[("NAS")]
  internet --> router --> nas` },
  { id: "zenuml", label: "mmd.kind.zenuml", form: false, example: `zenuml
  title Backup
  Laptop->NAS.upload() {
    NAS->Cloud.copy()
  }` },
];

/** The kind of a source, if it has a form. */
export function kindOf(src: string): MermaidKind | undefined {
  const { head } = splitSource(src);
  return MERMAID_KINDS.find((k) => k.detect.test(head));
}

export function parseMermaid(src: string): { kind: MermaidKind; model: Model } | undefined {
  const kind = kindOf(src);
  if (!kind) return undefined;
  const { pre, head, body } = splitSource(src);
  const model = kind.parse(body, head);
  model.pre = pre.join("\n");
  return { kind, model };
}

export function writeMermaid(kind: MermaidKind, model: Model): string {
  const pre = String(model.pre ?? "").trim();
  return (pre ? pre + "\n" : "") + kind.write(model);
}

let supported: Promise<Set<string>> | undefined;

/** The gallery entries this Mermaid version can draw: each example is checked by Mermaid itself. */
export function supportedKinds(): Promise<Set<string>> {
  supported ??= (async () => {
    const mermaid = (await import("mermaid")).default;
    const ok = new Set<string>();
    await Promise.all(GALLERY.map(async (g) => {
      try {
        if ((await mermaid.parse(g.example, { suppressErrors: true })) !== false) ok.add(g.id);
      } catch {
        /* not supported by this version */
      }
    }));
    return ok;
  })();
  return supported;
}
