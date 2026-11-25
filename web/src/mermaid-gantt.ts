// Mermaid Gantt plans as data, for the Gantt form of the block editor:
// parse the source into sections and tasks and write it back. Lines the
// form does not know (comments, other directives) are kept as they are.
// Syntax: https://mermaid.js.org/syntax/gantt.html
import { colorOk, readBarColors, readThemeCss, writeBarColors, writeThemeCss } from "./mermaid-forms/colors";
import { splitSource } from "./mermaid-forms/core";

export type GanttTag = "done" | "active" | "crit" | "milestone";
export const GANTT_TAGS: GanttTag[] = ["done", "active", "crit", "milestone"];

export interface GanttTask {
  name: string;
  tags: GanttTag[];
  id?: string;
  /** A date (in dateFormat) or "after a1 b2"; empty: after the task before. */
  start?: string;
  /** A duration (3d, 2w, 12h) or an end date or "until id". */
  end: string;
  /** Bar colour: a palette name or hex (needs an id; kept as CSS in the front matter). */
  color?: string;
}

export interface GanttSection { name: string; tasks: GanttTask[] }

export interface GanttPlan {
  title?: string;
  dateFormat?: string;
  axisFormat?: string;
  tickInterval?: string;
  /** e.g. "weekends" or "weekends 2026-12-24". */
  excludes?: string;
  todayMarker?: string;
  /** Other lines between the header and the first section (comments, directives). */
  extra: string[];
  /** Front matter, directives and comments before "gantt", kept as they are. */
  pre?: string[];
  /** themeCSS of the front matter without the bar colours of the tasks. */
  css?: string;
  sections: GanttSection[];
}

export const GANTT_EXAMPLE = `gantt
    title Move to the new NAS
    dateFormat YYYY-MM-DD
    axisFormat %d.%m
    excludes weekends
    section Preparation
        Order disks            :done, disks, 2026-12-01, 3d
        Back up the old NAS    :active, backup, after disks, 2d
    section Move
        Copy data              :crit, copy, after backup, 4d
        Switch shares          :after copy, 1d
        Old NAS off            :milestone, after copy, 0d`;

/** Whether a Mermaid source is a Gantt plan. */
export function isGantt(src: string): boolean {
  return /^gantt\b/.test(splitSource(src).head);
}

const DIRECTIVES = ["title", "dateFormat", "axisFormat", "tickInterval", "excludes", "todayMarker"] as const;

/** Splits the metadata after ":" into tags, id, start and end (Mermaid's rules). */
function parseMeta(meta: string): Omit<GanttTask, "name"> {
  const parts = meta.split(",").map((p) => p.trim()).filter((p) => p !== "");
  const tags: GanttTag[] = [];
  while (parts.length && (GANTT_TAGS as string[]).includes(parts[0])) tags.push(parts.shift() as GanttTag);
  if (parts.length >= 3) return { tags, id: parts[0], start: parts[1], end: parts.slice(2).join(", ") };
  if (parts.length === 2) return { tags, start: parts[0], end: parts[1] };
  return { tags, end: parts[0] ?? "" };
}

export function parseGantt(src: string): GanttPlan {
  const { pre, head, body } = splitSource(src);
  const { colors, raw, rest } = readBarColors(readThemeCss(pre));
  const plan: GanttPlan = { extra: [], sections: [], pre };
  let section: GanttSection | undefined;
  for (const raw of /^gantt\b/.test(head) ? body : [head, ...body]) {
    const line = raw.trim();
    if (!line) continue;
    const dir = DIRECTIVES.find((d) => line === d || line.startsWith(d + " "));
    if (dir && !section) {
      plan[dir] = line.slice(dir.length).trim();
      continue;
    }
    if (line.startsWith("section ") || line === "section") {
      section = { name: line.slice(7).trim(), tasks: [] };
      plan.sections.push(section);
      continue;
    }
    const colon = line.indexOf(":");
    if (colon > 0 && !line.startsWith("%%")) {
      if (!section) {
        section = { name: "", tasks: [] };
        plan.sections.push(section);
      }
      section.tasks.push({ name: line.slice(0, colon).trim(), ...parseMeta(line.slice(colon + 1)) });
      continue;
    }
    if (!section) plan.extra.push(line);
    else section.tasks.push({ name: line, tags: [], end: "" }); // unknown: kept as a task line without metadata
  }
  // Bar colours of known tasks go to the tasks; others stay in the CSS.
  const unknown: string[] = [];
  const tasks = new Map(plan.sections.flatMap((x) => x.tasks).filter((x) => x.id).map((x) => [x.id!, x]));
  for (const [id, c] of Object.entries(colors)) {
    const task = tasks.get(id);
    if (task) task.color = c;
    else unknown.push(raw[id]);
  }
  plan.css = [...unknown, rest].filter(Boolean).join(" ");
  return plan;
}

/** A task line. Mermaid needs a start next to an id: without one the task
 * follows the one before ("after prev"), or it is written without its id. */
function taskLine(t: GanttTask, prevId: string | undefined): string {
  const meta: string[] = [...t.tags];
  const start = (t.start ?? "").trim();
  const end = t.end.trim() || "1d";
  if (t.id && (start || prevId)) meta.push(t.id, start || `after ${prevId}`, end);
  else if (start) meta.push(start, end);
  else meta.push(end);
  return `        ${t.name.replace(/[:\n]/g, " ").trim().padEnd(22)} :${meta.join(", ")}`;
}

export function serializeGantt(p: GanttPlan): string {
  const colors: Record<string, string> = {};
  for (const x of p.sections.flatMap((sec) => sec.tasks)) if (x.id && colorOk(x.color)) colors[x.id] = x.color;
  const css = writeBarColors(colors, p.css ?? "");
  const pre = p.pre ?? [];
  const out = [...(readThemeCss(pre) === css ? pre : writeThemeCss(pre, css)), "gantt"];
  for (const d of DIRECTIVES) if (p[d]) out.push(`    ${d} ${p[d]}`);
  for (const e of p.extra) if (e && !/^gantt\b/.test(e)) out.push(`    ${e}`);
  let prevId: string | undefined;
  for (const s of p.sections) {
    if (s.name || p.sections.length > 1 || s !== p.sections[0]) out.push(`    section ${s.name || "Tasks"}`);
    for (const t of s.tasks) {
      if (!t.name.trim()) continue;
      out.push(taskLine(t, prevId));
      prevId = t.id && ((t.start ?? "").trim() || prevId) ? t.id : undefined;
    }
  }
  return out.join("\n");
}

/** Task ids of a plan (for "after …"). */
export function ganttIds(p: GanttPlan): string[] {
  return p.sections.flatMap((s) => s.tasks.map((t) => t.id).filter((id): id is string => !!id));
}

/** A free id for a new task (t1, t2 …). */
export function newGanttId(p: GanttPlan): string {
  const ids = new Set(ganttIds(p));
  let n = 1;
  while (ids.has(`t${n}`)) n++;
  return `t${n}`;
}
