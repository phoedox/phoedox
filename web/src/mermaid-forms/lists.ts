// List and tree diagrams: timeline, user journey, mindmap, kanban, treemap,
// gitGraph, Ishikawa, tree view, Cynefin, Venn, railroad.
import { IND, extraLines, indentOf, keep, levels, q, rows, s, splitArgs, unq, type ListDef, type MermaidKind, type Model, type Row } from "./core";

const levelField = { key: "level", label: "mmd.f.level", type: "number" as const, size: "s" as const };

export const timeline: MermaidKind = {
  id: "timeline", label: "mmd.kind.timeline", detect: /^timeline\b/,
  example: `timeline
    title History of the home network
    section 2023
        Spring : Fibre connection
        Autumn : New router : Mesh Wi-Fi
    section 2024
        March : NAS with backups`,
  fields: [{ key: "title", label: "mmd.f.title", size: "l" }],
  lists: [{ key: "sections", label: "mmd.tl.sections", hint: "mmd.tl.hint", add: "mmd.add_section", fields: [{ key: "name", label: "mmd.f.section", size: "l" }],
    make: () => ({ name: "", periods: [] }),
    children: { key: "periods", label: "mmd.tl.periods", add: "mmd.add_period", fields: [
      { key: "period", label: "mmd.tl.period", size: "s" }, { key: "events", label: "mmd.tl.events", type: "lines", size: "l" },
    ], make: () => ({ period: "", events: "" }) } }],
  parse(body) {
    const m: Model = { sections: [] };
    const extra: string[] = [];
    let sec: Row | undefined;
    let last: Row | undefined;
    const section = (name: string) => { sec = { name, periods: [] }; (m.sections as Row[]).push(sec); };
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = x[1];
      else if ((x = /^section\s+(.*)$/.exec(l))) section(x[1]);
      else if (l.startsWith(":") && last) last.events = [s(last.events), ...l.slice(1).split(":").map((e) => e.trim())].filter(Boolean).join("\n");
      else if (l.includes(":") && !l.startsWith("%%")) {
        if (!sec) section("");
        const [p, ...ev] = l.split(":");
        last = { period: p.trim(), events: ev.map((e) => e.trim()).filter(Boolean).join("\n") };
        ((sec as Row).periods as Row[]).push(last);
      } else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["timeline"];
    if (m.title) out.push(`${IND}title ${s(m.title)}`);
    for (const sec of rows(m, "sections")) {
      const named = !!s(sec.name).trim();
      if (named) out.push(`${IND}section ${s(sec.name)}`);
      for (const p of rows(sec, "periods")) if (s(p.period)) out.push(`${named ? IND + IND : IND}${s(p.period)} : ${s(p.events).split("\n").map((e) => e.trim()).filter(Boolean).join(" : ")}`);
    }
    return [...out, ...extraLines(m)].join("\n");
  },
};

export const journey: MermaidKind = {
  id: "journey", label: "mmd.kind.journey", detect: /^journey\b/,
  example: `journey
    title Setting up the new laptop
    section Unpack
        Unpack and charge: 5: Me
        Sign in: 3: Me
    section Set up
        Install apps: 2: Me, IT
        Restore files: 4: Me`,
  fields: [{ key: "title", label: "mmd.f.title", size: "l" }],
  lists: [{ key: "sections", label: "mmd.f.sections", hint: "mmd.journey.hint", add: "mmd.add_section", fields: [{ key: "name", label: "mmd.f.section", size: "l" }],
    make: () => ({ name: "", tasks: [] }),
    children: { key: "tasks", label: "mmd.f.tasks", add: "mmd.add_task", fields: [
      { key: "task", label: "mmd.f.task", size: "l" }, { key: "score", label: "mmd.journey.score", type: "select", options: [["1", "1 ☹"], ["2", "2"], ["3", "3"], ["4", "4"], ["5", "5 ☺"]], size: "s" },
      { key: "actors", label: "mmd.journey.actors", placeholder: "Me, Cat" },
    ], make: () => ({ task: "", score: "3", actors: "" }) } }],
  parse(body) {
    const m: Model = { sections: [] };
    const extra: string[] = [];
    let sec: Row | undefined;
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = x[1];
      else if ((x = /^section\s+(.*)$/.exec(l))) { sec = { name: x[1], tasks: [] }; (m.sections as Row[]).push(sec); }
      else if ((x = /^(.+?)\s*:\s*(\d)\s*(?::\s*(.*))?$/.exec(l))) {
        if (!sec) { sec = { name: "", tasks: [] }; (m.sections as Row[]).push(sec); }
        (sec.tasks as Row[]).push({ task: x[1], score: x[2], actors: x[3] ?? "" });
      } else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["journey"];
    if (m.title) out.push(`${IND}title ${s(m.title)}`);
    for (const sec of rows(m, "sections")) {
      out.push(`${IND}section ${s(sec.name) || "Steps"}`);
      for (const t of rows(sec, "tasks")) if (s(t.task)) out.push(`${IND}${IND}${s(t.task).replace(/:/g, " ")}: ${s(t.score) || "3"}${s(t.actors) ? ": " + s(t.actors) : ""}`);
    }
    return [...out, ...extraLines(m)].join("\n");
  },
};

const MM_SHAPES: [string, string, string, string][] = [
  // [value, label key, open, close]
  ["", "mmd.shape.default", "", ""], ["square", "mmd.shape.square", "[", "]"], ["rounded", "mmd.shape.rounded", "(", ")"],
  ["circle", "mmd.shape.circle", "((", "))"], ["bang", "mmd.shape.bang", "))", "(("], ["cloud", "mmd.shape.cloud", ")", "("], ["hexagon", "mmd.shape.hexagon", "{{", "}}"],
];

export const mindmap: MermaidKind = {
  id: "mindmap", label: "mmd.kind.mindmap", detect: /^mindmap\b/,
  example: `mindmap
  root((Home network))
    Internet
      Fibre
      Router
    Devices
      Laptops
      Phones
    Services
      NAS
      Backups`,
  lists: [{ key: "nodes", label: "mmd.f.nodes", hint: "mmd.tree.hint", add: "mmd.add_node", fields: [
    levelField, { key: "text", label: "mmd.f.text", size: "l" },
    { key: "shape", label: "mmd.f.shape", type: "select", options: MM_SHAPES.map(([v, k]) => [v, k]), size: "s" },
    { key: "icon", label: "mmd.mm.icon", placeholder: "fa fa-book", size: "s" },
  ], make: (m) => ({ level: Math.max(1, Number(rows(m, "nodes").at(-1)?.level ?? 1)), text: "", shape: "" }) }],
  parse(body) {
    const m: Model = { nodes: [] };
    const extra: string[] = [];
    const nodes = m.nodes as Row[];
    for (const { level, text } of levels(body)) {
      let x: RegExpExecArray | null;
      if ((x = /^::icon\((.*)\)$/.exec(text)) && nodes.length) { nodes[nodes.length - 1].icon = x[1]; continue; }
      if (text.startsWith(":::") && nodes.length) { nodes[nodes.length - 1].cls = text.slice(3); continue; }
      let node: Row = { level, text, shape: "" };
      for (const [v, , o, c] of MM_SHAPES.slice(1).sort((a, b) => b[2].length - a[2].length)) {
        const re = new RegExp(`^([\\w-]*)${o.replace(/[()[\]{}]/g, "\\$&")}(.*)${c.replace(/[()[\]{}]/g, "\\$&")}$`);
        if ((x = re.exec(text))) { node = { level, id: x[1] || undefined, text: unq(x[2]), shape: v }; break; }
      }
      nodes.push(node);
    }
    // Levels start at 0 for the root.
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["mindmap"];
    rows(m, "nodes").forEach((n, i) => {
      if (!s(n.text)) return;
      const lv = Math.max(0, Number(n.level) || 0);
      const sh = MM_SHAPES.find(([v]) => v === s(n.shape));
      const pad = "  ".repeat(lv + 1);
      out.push(pad + (sh && sh[0] ? `${s(n.id) || "n" + (i + 1)}${sh[2]}${s(n.text)}${sh[3]}` : s(n.text)));
      if (n.icon) out.push(`${pad}  ::icon(${s(n.icon)})`);
      if (n.cls) out.push(`${pad}  :::${s(n.cls)}`);
    });
    return [...out, ...extraLines(m)].join("\n");
  },
};

export const kanban: MermaidKind = {
  id: "kanban", label: "mmd.kind.kanban", detect: /^kanban\b/,
  example: `kanban
  todo[To do]
    t1[Order disks]@{ assigned: 'Alex', priority: 'High' }
    t2[Plan the move]
  doing[In progress]
    t3[Back up the old NAS]@{ ticket: NAS-12 }
  done[Done]
    t4[Choose the NAS]`,
  lists: [{ key: "columns", label: "mmd.kanban.columns", add: "mmd.add_column", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "title", label: "mmd.f.title", size: "l" }],
    make: (m) => ({ id: `c${rows(m, "columns").length + 1}`, title: "", cards: [] }),
    children: { key: "cards", label: "mmd.kanban.cards", add: "mmd.add_card", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "text", label: "mmd.f.text", size: "l" },
      { key: "assigned", label: "mmd.kanban.assigned", size: "s" }, { key: "ticket", label: "mmd.kanban.ticket", size: "s" },
      { key: "priority", label: "mmd.kanban.priority", type: "select", options: [["", "–"], ["Very High", "Very High"], ["High", "High"], ["Low", "Low"], ["Very Low", "Very Low"]], size: "s" },
    ], make: () => ({ id: `t${Date.now() % 100000}`, text: "" }) } }],
  parse(body) {
    const m: Model = { columns: [] };
    const extra: string[] = [];
    let col: Row | undefined;
    let colIndent = -1;
    const item = /^([\w-]*)\[(.*?)\](?:@\{(.*)\})?$/;
    for (const raw of body) {
      if (!raw.trim() || raw.trim().startsWith("%%")) continue;
      const w = indentOf(raw), l = raw.trim();
      const x = item.exec(l);
      if (colIndent < 0 || w <= colIndent) {
        colIndent = w;
        col = { id: x ? x[1] : "", title: x ? x[2] : l, cards: [] };
        (m.columns as Row[]).push(col);
        continue;
      }
      if (!x || !col) { keep(extra, l); continue; }
      const card: Row = { id: x[1], text: x[2] };
      for (const kv of splitArgs(x[3] ?? "")) {
        const [k, ...v] = kv.split(":");
        const val = v.join(":").trim().replace(/^'(.*)'$/, "$1").replace(/^"(.*)"$/, "$1");
        if (["assigned", "ticket", "priority"].includes(k.trim())) card[k.trim()] = val;
      }
      (col.cards as Row[]).push(card);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["kanban"];
    for (const c of rows(m, "columns")) {
      out.push(`  ${s(c.id)}[${s(c.title) || s(c.id) || "Column"}]`);
      for (const k of rows(c, "cards")) {
        if (!s(k.text)) continue;
        const meta = (["assigned", "ticket", "priority"] as const).filter((f) => s(k[f])).map((f) => `${f}: ${f === "ticket" ? s(k[f]) : `'${s(k[f]).replace(/'/g, "")}'`}`);
        out.push(`    ${s(k.id)}[${s(k.text).replace(/[[\]]/g, "")}]${meta.length ? `@{ ${meta.join(", ")} }` : ""}`);
      }
    }
    return [...out, ...extraLines(m, "  ")].join("\n");
  },
};

/** Trees written by indentation (treemap, Ishikawa, tree view). */
function treeKind(o: { id: string; kw: RegExp; example: string; fields: ListDef["fields"]; line: (text: string, level: number) => Row; out: (r: Row) => string; head?: string; hint?: string }): MermaidKind {
  return {
    id: o.id, label: `mmd.kind.${o.id}`, detect: o.kw, example: o.example,
    lists: [{ key: "nodes", label: "mmd.f.nodes", hint: o.hint ?? "mmd.tree.hint", add: "mmd.add_node", fields: [levelField, ...o.fields],
      make: (m) => ({ level: Number(rows(m, "nodes").at(-1)?.level ?? 0) }) }],
    parse(body, head) {
      const m: Model = { kw: head.split(/\s+/)[0], nodes: [] };
      for (const { level, text } of levels(body)) (m.nodes as Row[]).push(o.line(text, level));
      return m;
    },
    write(m) {
      return [s(m.kw) || o.head || o.id, ...rows(m, "nodes").map((r) => IND.repeat(Math.max(0, Number(r.level) || 0) + 1) + o.out(r)).filter((l) => l.trim())].join("\n");
    },
  };
}

export const treemap = treeKind({
  id: "treemap", kw: /^treemap(-beta)?\b/, head: "treemap-beta", hint: "mmd.treemap.hint",
  example: `treemap-beta
"Storage"
    "Photos": 420
    "Videos": 830
"Backups"
    "Laptops": 210
    "Phones": 90`,
  fields: [{ key: "name", label: "mmd.f.name", size: "l" }, { key: "value", label: "mmd.f.value", type: "number", size: "s" }, { key: "cls", label: "mmd.f.class", size: "s" }],
  line: (text, level) => {
    const x = /^("[^"]*"|[^:]+?)(?::::(\S+))?\s*(?::\s*([-\d.]+))?\s*(?::::(\S+))?$/.exec(text);
    return x ? { level, name: unq(x[1]), value: x[3] !== undefined ? Number(x[3]) : undefined, cls: x[2] ?? x[4] } : { level, name: text };
  },
  out: (r) => (s(r.name) ? `"${s(r.name).replace(/"/g, "'")}"${r.cls ? ":::" + s(r.cls) : ""}${s(r.value) !== "" ? `: ${Number(r.value)}` : ""}` : ""),
});

export const ishikawa = treeKind({
  id: "ishikawa", kw: /^ishikawa(-beta)?\b/, head: "ishikawa-beta", hint: "mmd.ishikawa.hint",
  example: `ishikawa-beta
    Wi-Fi is slow
    Hardware
        Old router
        Walls
    Setup
        Wrong channel
    Use
        Too many devices`,
  fields: [{ key: "text", label: "mmd.f.text", size: "l" }],
  line: (text, level) => ({ level, text }),
  out: (r) => s(r.text),
});

export const treeView = treeKind({
  id: "treeView", kw: /^treeView(-beta)?\b/, head: "treeView-beta", hint: "mmd.treeview.hint",
  example: `treeView-beta
    "docs/"
        "setup.md"
        "network.md" ## the main page
    "src/"
        "app.ts" :::highlight`,
  fields: [{ key: "name", label: "mmd.f.name", size: "l" }, { key: "note", label: "mmd.treeview.note", size: "m" }, { key: "cls", label: "mmd.f.class", size: "s" }],
  line: (text, level) => {
    const x = /^("[^"]*"|\S+)\s*(.*)$/.exec(text)!;
    const rest = x[2];
    const note = /##\s*(.*?)(?=\s*(?::::|icon\(|$))/.exec(rest)?.[1];
    const cls = /:::(\S+)/.exec(rest)?.[1];
    const icon = /icon\(([^)]*)\)/.exec(rest)?.[1];
    return { level, name: unq(x[1]), note, cls, icon };
  },
  out: (r) => (s(r.name) ? `${/\s/.test(s(r.name)) ? `"${s(r.name)}"` : s(r.name)}${r.icon ? ` icon(${s(r.icon)})` : ""}${r.cls ? ` :::${s(r.cls)}` : ""}${r.note ? ` ## ${s(r.note)}` : ""}` : ""),
});

export const gitGraph: MermaidKind = {
  id: "gitGraph", label: "mmd.kind.gitGraph", detect: /^gitGraph\b/,
  example: `gitGraph
    commit id: "init"
    branch develop
    checkout develop
    commit id: "feature"
    checkout main
    merge develop tag: "v1.0"
    commit`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: [["", "LR"], ["TB:", "TB"], ["BT:", "BT"]], size: "s" }],
  lists: [{ key: "steps", label: "mmd.git.steps", hint: "mmd.git.hint", add: "mmd.add_step", fields: [
    { key: "cmd", label: "mmd.git.cmd", type: "select", options: [["commit", "commit"], ["branch", "branch"], ["checkout", "checkout"], ["merge", "merge"], ["cherry-pick", "cherry-pick"]], size: "s" },
    { key: "name", label: "mmd.git.branch", size: "s", show: (r) => ["branch", "checkout", "merge"].includes(s(r.cmd)), suggest: (m) => ["main", ...rows(m, "steps").filter((r) => r.cmd === "branch").map((r) => s(r.name))] },
    { key: "id", label: "mmd.f.id", size: "s", show: (r) => ["commit", "merge", "cherry-pick"].includes(s(r.cmd)) },
    { key: "tag", label: "mmd.git.tag", size: "s", show: (r) => ["commit", "merge", "cherry-pick"].includes(s(r.cmd)) },
    { key: "type", label: "mmd.f.kind", type: "select", options: [["", "NORMAL"], ["REVERSE", "REVERSE"], ["HIGHLIGHT", "HIGHLIGHT"]], size: "s", show: (r) => ["commit", "merge"].includes(s(r.cmd)) },
  ], make: () => ({ cmd: "commit" }) }],
  parse(body, head) {
    const m: Model = { dir: /\b(TB|BT|LR):/.exec(head)?.[1] ? `${/\b(TB|BT|LR):/.exec(head)![1]}:` : "", steps: [] };
    if (m.dir === "LR:") m.dir = "";
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      const x = /^(commit|branch|checkout|switch|merge|cherry-pick)\b\s*(.*)$/.exec(l);
      if (!x) { keep(extra, l); continue; }
      const step: Row = { cmd: x[1] === "switch" ? "checkout" : x[1] };
      let rest = x[2];
      const attr = (k: string) => {
        const y = new RegExp(`\\b${k}:\\s*("[^"]*"|\\S+)`).exec(rest);
        if (y) { rest = rest.replace(y[0], ""); return unq(y[1]); }
        return undefined;
      };
      step.id = attr("id");
      step.tag = attr("tag");
      step.type = attr("type");
      const order = attr("order");
      if (["branch", "checkout", "merge"].includes(s(step.cmd))) step.name = rest.trim().split(/\s+/)[0];
      if (order || /\S/.test(rest.replace(s(step.name), ""))) { keep(extra, l); continue; }
      (m.steps as Row[]).push(step);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = [`gitGraph${m.dir ? " " + s(m.dir) : ""}`];
    for (const r of rows(m, "steps")) {
      const c = s(r.cmd) || "commit";
      const parts = [c];
      if (["branch", "checkout", "merge"].includes(c)) parts.push(s(r.name) || "main");
      if (r.id && c !== "branch" && c !== "checkout") parts.push(`id: ${q(r.id, true)}`);
      if (r.tag && ["commit", "merge", "cherry-pick"].includes(c)) parts.push(`tag: ${q(r.tag, true)}`);
      if (r.type && ["commit", "merge"].includes(c)) parts.push(`type: ${s(r.type)}`);
      out.push(IND + parts.join(" "));
    }
    return [...out, ...extraLines(m)].join("\n");
  },
};

const CYNEFIN = ["complex", "complicated", "clear", "chaotic", "confusion"];

export const cynefin: MermaidKind = {
  id: "cynefin", label: "mmd.kind.cynefin", detect: /^cynefin(-beta)?\b/,
  example: `cynefin-beta
  title Our IT topics
  complex
    "New AI assistant"
  complicated
    "Network redesign"
  clear
    "Password resets"
  chaotic
    "Ransomware alert"
  chaotic --> complex : "after the first response"`,
  fields: [{ key: "title", label: "mmd.f.title", size: "l" }],
  lists: [
    { key: "items", label: "mmd.cynefin.items", add: "mmd.add_item", fields: [
      { key: "domain", label: "mmd.cynefin.domain", type: "select", options: CYNEFIN.map((d) => [d, `mmd.cynefin.${d}`]), size: "s" }, { key: "text", label: "mmd.f.text", size: "l" },
    ], make: () => ({ domain: "complex", text: "" }) },
    { key: "moves", label: "mmd.cynefin.moves", add: "mmd.add_transition", fields: [
      { key: "from", label: "mmd.f.from", type: "select", options: CYNEFIN.map((d) => [d, `mmd.cynefin.${d}`]), size: "s" },
      { key: "to", label: "mmd.f.to", type: "select", options: CYNEFIN.map((d) => [d, `mmd.cynefin.${d}`]), size: "s" },
      { key: "label", label: "mmd.f.label", size: "l" },
    ], make: () => ({ from: "chaotic", to: "complex", label: "" }) },
  ],
  parse(body) {
    const m: Model = { items: [], moves: [] };
    const extra: string[] = [];
    let dom = "";
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = unq(x[1]);
      else if (CYNEFIN.includes(l)) dom = l;
      else if ((x = /^(\w+)\s*-->\s*(\w+)\s*(?::\s*(.*))?$/.exec(l)) && CYNEFIN.includes(x[1]) && CYNEFIN.includes(x[2])) (m.moves as Row[]).push({ from: x[1], to: x[2], label: x[3] ? unq(x[3]) : "" });
      else if (dom && /^".*"$/.test(l)) (m.items as Row[]).push({ domain: dom, text: unq(l) });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["cynefin-beta"];
    if (m.title) out.push(`  title ${s(m.title)}`);
    for (const d of CYNEFIN) {
      const items = rows(m, "items").filter((r) => r.domain === d && s(r.text));
      if (!items.length && d === "confusion") continue;
      out.push(`  ${d}`, ...items.map((r) => `    "${s(r.text).replace(/"/g, "'")}"`));
    }
    for (const t of rows(m, "moves")) if (t.from !== t.to) out.push(`  ${s(t.from)} --> ${s(t.to)}${t.label ? ` : "${s(t.label).replace(/"/g, "'")}"` : ""}`);
    return [...out, ...extraLines(m, "  ")].join("\n");
  },
};

export const venn: MermaidKind = {
  id: "venn", label: "mmd.kind.venn", detect: /^venn(-beta)?\b/,
  example: `venn-beta
  title Who uses what
  set A["Laptop"]
  set B["Phone"]
  set C["Tablet"]
  union A,B["Mail"]
  union A,B,C["Calendar"]`,
  fields: [{ key: "title", label: "mmd.f.title", size: "l" }],
  lists: [
    { key: "sets", label: "mmd.venn.sets", add: "mmd.add_set", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" }, { key: "size", label: "mmd.f.size", type: "number", size: "s" }],
      make: (m) => ({ id: String.fromCharCode(65 + rows(m, "sets").length), label: "" }) },
    { key: "unions", label: "mmd.venn.unions", hint: "mmd.venn.unions_hint", add: "mmd.add_union", fields: [
      { key: "members", label: "mmd.venn.members", placeholder: "A,B", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" }, { key: "size", label: "mmd.f.size", type: "number", size: "s" },
    ], make: () => ({ members: "A,B", label: "" }) },
  ],
  parse(body) {
    const m: Model = { sets: [], unions: [] };
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = unq(x[1]);
      else if ((x = /^set\s+("[^"]*"|[\w]+)(?:\["([^"]*)"\])?(?::(\d+(?:\.\d+)?))?$/.exec(l))) (m.sets as Row[]).push({ id: unq(x[1]), label: x[2] ?? "", size: x[3] !== undefined ? Number(x[3]) : undefined });
      else if ((x = /^union\s+([^[:]+?)(?:\["([^"]*)"\])?(?::(\d+(?:\.\d+)?))?$/.exec(l))) (m.unions as Row[]).push({ members: x[1].replace(/\s+/g, ""), label: x[2] ?? "", size: x[3] !== undefined ? Number(x[3]) : undefined });
      else keep(extra, raw.replace(/\s+$/, ""));
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const id = (v: unknown) => (/^\w+$/.test(s(v)) ? s(v) : `"${s(v)}"`);
    const out = ["venn-beta"];
    if (m.title) out.push(`  title ${s(m.title)}`);
    for (const r of rows(m, "sets")) if (s(r.id)) out.push(`  set ${id(r.id)}${r.label ? `["${s(r.label)}"]` : ""}${s(r.size) !== "" ? `:${r.size}` : ""}`);
    for (const r of rows(m, "unions")) if (s(r.members)) out.push(`  union ${s(r.members).replace(/\s+/g, "")}${r.label ? `["${s(r.label)}"]` : ""}${s(r.size) !== "" ? `:${r.size}` : ""}`);
    return [...out, ...extraLines(m, "  ")].join("\n");
  },
};

export const railroad: MermaidKind = {
  id: "railroad", label: "mmd.kind.railroad", detect: /^railroad(-ebnf|-abnf|-peg)?-beta\b/,
  example: `railroad-ebnf-beta
  title "Host name"
  letter = "a" | "b" | "c" ;
  digit = "0" | "1" | "2" ;
  label = letter , { letter | digit | "-" } ;
  host = label , { "." , label } ;`,
  fields: [
    { key: "kw", label: "mmd.railroad.notation", type: "select", options: [["railroad-ebnf-beta", "EBNF"], ["railroad-abnf-beta", "ABNF"], ["railroad-peg-beta", "PEG"], ["railroad-beta", "IR"]], size: "s" },
    { key: "title", label: "mmd.f.title", size: "l" },
  ],
  lists: [{ key: "rules", label: "mmd.railroad.rules", hint: "mmd.railroad.hint", add: "mmd.add_rule", fields: [
    { key: "name", label: "mmd.f.name", size: "s" }, { key: "def", label: "mmd.railroad.def", size: "l" },
  ], make: () => ({ name: "", def: "" }) }],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], rules: [] };
    const extra: string[] = [];
    const text = body.join("\n");
    const op = m.kw === "railroad-peg-beta" ? "<-" : "(?:::=|=)";
    let rest = text.replace(/^\s*title\s+(.*)$/m, (_a, t: string) => { m.title = unq(t); return ""; });
    rest = rest.replace(/^\s*(acc(?:Title|Descr)\s*:.*)$/gm, (a) => { keep(extra, a); return ""; });
    const re = new RegExp(`([\\w-]+)\\s*${op}\\s*([\\s\\S]*?);`, "g");
    let x: RegExpExecArray | null;
    let used = "";
    while ((x = re.exec(rest))) { (m.rules as Row[]).push({ name: x[1], def: x[2].replace(/\s+/g, " ").trim() }); used += x[0]; }
    const left = rest.replace(re, "").trim();
    if (left) keep(extra, left);
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const op = m.kw === "railroad-peg-beta" ? "<-" : "=";
    const out = [s(m.kw) || "railroad-ebnf-beta"];
    if (m.title) out.push(`  title "${s(m.title).replace(/"/g, "'")}"`);
    for (const r of rows(m, "rules")) if (s(r.name)) out.push(`  ${s(r.name)} ${op} ${s(r.def).trim().replace(/;\s*$/, "")} ;`);
    return [...out, ...extraLines(m, "  ")].join("\n");
  },
};
