// Flowcharts and their relatives (swimlanes, agent flows): nodes with a
// shape and text, edges with line, arrow and text, groups (subgraphs, lanes,
// flows). Statements the form does not know stay as further lines.
import { readFlowColor, writeFlowColors } from "./colors";
import { IND, extraLines, keep, rows, s, type FieldDef, type MermaidKind, type Model, type Row } from "./core";

// [value, label key, open, close]
export const FC_SHAPES: [string, string, string, string][] = [
  ["rect", "mmd.shape.rect", "[", "]"], ["rounded", "mmd.shape.rounded", "(", ")"], ["stadium", "mmd.shape.stadium", "([", "])"],
  ["subroutine", "mmd.shape.subroutine", "[[", "]]"], ["cylinder", "mmd.shape.cylinder", "[(", ")]"], ["circle", "mmd.shape.circle", "((", "))"],
  ["circle2", "mmd.shape.double_circle", "(((", ")))"], ["asym", "mmd.shape.asym", ">", "]"], ["rhombus", "mmd.shape.rhombus", "{", "}"],
  ["hexagon", "mmd.shape.hexagon", "{{", "}}"], ["parallelogram", "mmd.shape.parallelogram", "[/", "/]"], ["parallelogram_alt", "mmd.shape.parallelogram_alt", "[\\", "\\]"],
  ["trapezoid", "mmd.shape.trapezoid", "[/", "\\]"], ["trapezoid_alt", "mmd.shape.trapezoid_alt", "[\\", "/]"],
];

const ID = /^[\p{L}\p{N}_][\p{L}\p{N}_-]*/u;

export interface FlowNode { id: string; text?: string; shape?: string; meta?: string }

/** A node at the start of str: id, optional shape with text, optional @{…}. */
export function parseNode(str: string): { node: FlowNode; len: number } | null {
  const idm = ID.exec(str);
  if (!idm) return null;
  let id = idm[0];
  // A link may follow directly ("A-->B"): the id must not swallow its dashes.
  const dash = /-+[->ox.=]/.exec(id);
  if (dash) id = id.slice(0, dash.index);
  if (!id) return null;
  let i = id.length;
  const node: FlowNode = { id };
  const rest = str.slice(i);
  let best: { shape: string; open: string; end: number } | null = null;
  for (const [v, , o, c] of FC_SHAPES) {
    if (!rest.startsWith(o)) continue;
    const end = rest.indexOf(c, o.length);
    if (end < 0) continue;
    if (!best || o.length > best.open.length || (o.length === best.open.length && end < best.end)) best = { shape: v, open: o, end };
  }
  if (best) {
    const close = FC_SHAPES.find(([v]) => v === best!.shape)![3];
    let text = rest.slice(best.open.length, best.end).trim();
    if (text.length >= 2 && text.startsWith('"') && text.endsWith('"')) text = text.slice(1, -1);
    node.text = text.replace(/#quot;/g, '"');
    node.shape = best.shape;
    i += best.end + close.length;
  }
  const meta = /^@\{([^}]*)\}/.exec(str.slice(i));
  if (meta) {
    node.meta = meta[1].trim();
    i += meta[0].length;
  }
  return { node, len: i };
}

export interface FlowLink { line: "solid" | "dotted" | "thick"; head: "arrow" | "none" | "circle" | "cross" | "both"; text?: string }

/** A link at the start of str (after spaces). */
export function parseLink(str: string): { link: FlowLink; len: number } | null {
  const lead = /^\s*/.exec(str)![0].length;
  const t = str.slice(lead);
  const kind = (body: string, head: string | undefined, both: boolean): FlowLink => ({
    line: body.startsWith("=") ? "thick" : body.includes(".") ? "dotted" : "solid",
    head: both ? "both" : head === ">" ? "arrow" : head === "o" ? "circle" : head === "x" ? "cross" : "none",
  });
  // Text in the middle: -- text -->, == text ==>, -. text .->
  let m = /^(<)?(--|==|-\.)\s+([^|]+?)\s+(-{2,}|={2,}|\.+-)([>ox]?)(?=\s|$|[\p{L}\p{N}_])/u.exec(t);
  if (m) return { link: { ...kind(m[2] + m[4], m[5], !!m[1]), text: m[3].trim() }, len: lead + m[0].length };
  m = /^(<)?(-{2,}|={2,}|-\.+-)([>ox])?/.exec(t);
  if (!m || (m[2] === "--" && !m[3] && !m[1])) return null;
  const link = kind(m[2], m[3], !!m[1]);
  let len = lead + m[0].length;
  const pipe = /^\s*\|([^|]*)\|/.exec(str.slice(len));
  if (pipe) {
    link.text = pipe[1].trim().replace(/^"(.*)"$/, "$1");
    len += pipe[0].length;
  }
  return { link, len };
}

const OPS: Record<string, Record<string, string>> = {
  solid: { arrow: "-->", none: "---", circle: "--o", cross: "--x", both: "<-->" },
  dotted: { arrow: "-.->", none: "-.-", circle: "-.-o", cross: "-.-x", both: "<-.->" },
  thick: { arrow: "==>", none: "===", circle: "==o", cross: "==x", both: "<==>" },
};
export const linkOp = (line: unknown, head: unknown) => OPS[s(line) || "solid"]?.[s(head) || "arrow"] ?? "-->";

/** A node as written: id plus shape and text (quoted when needed). */
export function nodeText(n: Row, forceShape = false): string {
  const sh = FC_SHAPES.find(([v]) => v === s(n.shape)) ?? (forceShape || s(n.text) ? FC_SHAPES[0] : undefined);
  let out = s(n.id);
  if (sh && (s(n.text) || forceShape)) {
    const txt = s(n.text) || s(n.id);
    const needQ = /[[\](){}|<>"#;]/.test(txt) || /^[\s-]/.test(txt);
    out += sh[2] + (needQ ? `"${txt.replace(/"/g, "#quot;")}"` : txt) + sh[3];
  }
  if (s(n.meta)) out += `@{ ${s(n.meta)} }`;
  return out;
}

interface Opts { groupWord: string; dir?: boolean }

/** Parses the statements of a flowchart-like diagram. */
export function parseFlow(body: string[], o: Opts): Model {
  const m: Model = { nodes: [], edges: [], groups: [] };
  const extra: string[] = [];
  const nodes = new Map<string, Row>();
  const stack: string[] = [];
  const colors: { ids: string[]; color?: string; raw: string }[] = [];
  const see = (n: FlowNode) => {
    let r = nodes.get(n.id);
    if (!r) {
      r = { id: n.id, text: "", shape: "", group: stack[stack.length - 1] ?? "" };
      nodes.set(n.id, r);
      (m.nodes as Row[]).push(r);
    }
    if (n.text !== undefined) { r.text = n.text; r.shape = n.shape; }
    if (n.meta !== undefined) r.meta = n.meta;
  };
  for (const raw of body) {
    let l = raw.trim().replace(/;\s*$/, "");
    if (!l) continue;
    let x: RegExpExecArray | null;
    if ((x = new RegExp(`^${o.groupWord}\\s+(.+)$`).exec(l))) {
      const g = /^([\p{L}\p{N}_-]+)\s*\[(.*)\]$/u.exec(x[1].trim());
      const id = g ? g[1] : x[1].trim().replace(/^"(.*)"$/, "$1");
      (m.groups as Row[]).push({ id, title: g ? g[2].replace(/^"(.*)"$/, "$1") : "", parent: stack[stack.length - 1] ?? "" });
      stack.push(id);
      continue;
    }
    if (l === "end" && stack.length) { stack.pop(); continue; }
    if ((x = /^direction\s+(TB|TD|BT|LR|RL)$/.exec(l)) && stack.length) {
      const g = (m.groups as Row[]).find((r) => r.id === stack[stack.length - 1]);
      if (g) g.dir = x[1];
      continue;
    }
    const col = readFlowColor(l);
    if (col) { colors.push({ ...col, raw }); continue; }
    if (/^(classDef|class|style|linkStyle|click|accTitle|accDescr|%%)\b/.test(l) || l.includes("&") || l.includes(":::")) { keep(extra, raw); continue; }
    // node (link node)*
    const chain: (FlowNode | FlowLink)[] = [];
    let rest = l;
    let ok = true;
    const first = parseNode(rest);
    if (!first) ok = false;
    else {
      chain.push(first.node);
      rest = rest.slice(first.len);
      while (rest.trim()) {
        const lk = parseLink(rest);
        if (!lk) { ok = false; break; }
        rest = rest.slice(lk.len).replace(/^\s+/, "");
        const nd = parseNode(rest);
        if (!nd) { ok = false; break; }
        chain.push(lk.link, nd.node);
        rest = rest.slice(nd.len);
      }
    }
    if (!ok) { keep(extra, raw); continue; }
    for (let i = 0; i < chain.length; i += 2) see(chain[i] as FlowNode);
    for (let i = 1; i < chain.length; i += 2) {
      const lk = chain[i] as FlowLink;
      (m.edges as Row[]).push({ from: (chain[i - 1] as FlowNode).id, to: (chain[i + 1] as FlowNode).id, text: lk.text ?? "", line: lk.line, head: lk.head });
    }
  }
  // Colours of nodes the form knows; anything else stays as a further line.
  for (const c of colors) {
    if (!c.color) continue;
    const known = c.ids.filter((id) => nodes.has(id));
    for (const id of known) nodes.get(id)!.color = c.color;
    const unknown = c.ids.filter((id) => !nodes.has(id));
    if (unknown.length) keep(extra, c.raw.replace(c.ids.join(","), unknown.join(",")));
  }
  m.extra = extra.join("\n");
  return m;
}

/** Writes nodes (inside their groups), then edges. */
export function writeFlow(m: Model, o: Opts & { forceShape?: boolean; groupTitle?: (g: Row) => string }): string[] {
  const out: string[] = [];
  const nodes = rows(m, "nodes").filter((n) => s(n.id));
  const groups = rows(m, "groups").filter((g) => s(g.id));
  const linked = new Set(rows(m, "edges").flatMap((e) => [s(e.from), s(e.to)]));
  const defLine = (n: Row) => (s(n.text) || s(n.meta) || o.forceShape || s(n.group) || !linked.has(s(n.id)) ? nodeText(n, o.forceShape) : "");
  const writeGroup = (g: Row, depth: number) => {
    const pad = IND.repeat(depth);
    out.push(`${pad}${o.groupWord} ${o.groupTitle ? o.groupTitle(g) : `${s(g.id)}${s(g.title) ? `["${s(g.title)}"]` : ""}`}`);
    if (g.dir) out.push(`${pad}${IND}direction ${s(g.dir)}`);
    for (const c of groups.filter((x) => s(x.parent) === s(g.id))) writeGroup(c, depth + 1);
    for (const n of nodes.filter((x) => s(x.group) === s(g.id))) out.push(`${pad}${IND}${nodeText(n, o.forceShape)}`);
    out.push(`${pad}end`);
  };
  // In list order; a group is written where its first node is (or at the end).
  const top = (id: string): string => {
    const g = groups.find((x) => s(x.id) === id);
    return g && s(g.parent) && groups.some((p) => s(p.id) === s(g.parent)) ? top(s(g.parent)) : id;
  };
  const done = new Set<string>();
  for (const n of nodes) {
    const g = s(n.group) && groups.some((x) => s(x.id) === s(n.group)) ? top(s(n.group)) : "";
    if (!g) {
      const d = defLine(n);
      if (d) out.push(IND + d);
    } else if (!done.has(g)) { done.add(g); writeGroup(groups.find((x) => s(x.id) === g)!, 1); }
  }
  for (const g of groups.filter((x) => (!s(x.parent) || !groups.some((p) => s(p.id) === s(x.parent))) && !done.has(s(x.id)))) writeGroup(g, 1);
  for (const e of rows(m, "edges")) {
    if (!s(e.from) || !s(e.to)) continue;
    const text = s(e.text).trim();
    out.push(`${IND}${s(e.from)} ${linkOp(e.line, e.head)}${text ? `|${/[|"]/.test(text) ? `"${text.replace(/["|]/g, "'")}"` : text}|` : ""} ${s(e.to)}`);
  }
  out.push(...writeFlowColors(nodes.map((n) => ({ id: s(n.id), color: n.color })), IND));
  return [...out, ...extraLines(m)];
}

const nodeIds = (m: Model) => rows(m, "nodes").map((n) => s(n.id)).filter(Boolean);
const groupIds = (m: Model) => rows(m, "groups").map((g) => s(g.id)).filter(Boolean);
const DIRS: [string, string][] = [["TD", "mmd.dir.td"], ["LR", "mmd.dir.lr"], ["BT", "mmd.dir.bt"], ["RL", "mmd.dir.rl"]];

const edgeFields: FieldDef[] = [
  { key: "from", label: "mmd.f.from", size: "s", suggest: nodeIds }, { key: "to", label: "mmd.f.to", size: "s", suggest: nodeIds },
  { key: "text", label: "mmd.f.text", size: "m" },
  { key: "line", label: "mmd.f.line", type: "select", options: [["solid", "mmd.line.solid"], ["dotted", "mmd.line.dotted"], ["thick", "mmd.line.thick"]], size: "s" },
  { key: "head", label: "mmd.f.head", type: "select", options: [["arrow", "mmd.head.arrow"], ["none", "mmd.head.none"], ["both", "mmd.head.both"], ["circle", "mmd.head.circle"], ["cross", "mmd.head.cross"]], size: "s" },
];

function nextId(m: Model, prefix: string): string {
  const ids = new Set(nodeIds(m));
  let n = ids.size + 1;
  while (ids.has(`${prefix}${n}`)) n++;
  return `${prefix}${n}`;
}

export const flowchart: MermaidKind = {
  id: "flowchart", label: "mmd.kind.flowchart", detect: /^(flowchart|graph)\b/,
  example: `flowchart LR
    internet((Internet)) --> router[Router]
    router --> switch[Switch]
    switch --> nas[(NAS)]
    switch -.->|Wi-Fi| laptop[Laptop]`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: DIRS, size: "s" }],
  lists: [
    { key: "nodes", label: "mmd.f.nodes", add: "mmd.add_node", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "text", label: "mmd.f.text", size: "l" },
      { key: "shape", label: "mmd.f.shape", type: "select", options: FC_SHAPES.map(([v, k]) => [v, k]), size: "s" },
      { key: "group", label: "mmd.f.group", size: "s", suggest: groupIds },
      { key: "color", label: "mmd.f.color", type: "color" },
    ], make: (m) => ({ id: nextId(m, "n"), text: "", shape: "rect", group: "" }) },
    { key: "edges", label: "mmd.f.edges", add: "mmd.add_edge", fields: edgeFields, make: () => ({ from: "", to: "", line: "solid", head: "arrow" }) },
    { key: "groups", label: "mmd.flow.groups", hint: "mmd.flow.groups_hint", add: "mmd.add_group", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "title", label: "mmd.f.title", size: "l" },
      { key: "parent", label: "mmd.f.parent", size: "s", suggest: groupIds },
      { key: "dir", label: "mmd.f.direction", type: "select", options: [["", "–"], ...DIRS], size: "s" },
    ], make: (m) => ({ id: `g${rows(m, "groups").length + 1}`, title: "" }) },
  ],
  parse(body, head) {
    const m = parseFlow(body, { groupWord: "subgraph" });
    const x = /^(flowchart|graph)\s*(TB|TD|BT|LR|RL)?/.exec(head)!;
    m.kw = x[1];
    m.dir = x[2] === "TB" ? "TD" : (x[2] ?? "TD");
    return m;
  },
  write(m) {
    return [`${s(m.kw) || "flowchart"} ${s(m.dir) || "TD"}`, ...writeFlow(m, { groupWord: "subgraph" })].join("\n");
  },
};

export const swimlane: MermaidKind = {
  id: "swimlane", label: "mmd.kind.swimlane", detect: /^swimlane(-beta)?\b/,
  example: `swimlane-beta LR
  subgraph Customer
    order[Place order]
    receive([Receive parcel])
  end
  subgraph Shop
    pack[Pack goods]
    ship[Ship]
  end
  order --> pack --> ship --> receive`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: [["TB", "mmd.dir.td"], ["LR", "mmd.dir.lr"], ["BT", "mmd.dir.bt"], ["RL", "mmd.dir.rl"]], size: "s" }],
  lists: [
    { key: "groups", label: "mmd.swimlane.lanes", add: "mmd.add_lane", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "title", label: "mmd.f.title", size: "l" }],
      make: (m) => ({ id: `lane${rows(m, "groups").length + 1}`, title: "" }) },
    { key: "nodes", label: "mmd.f.nodes", add: "mmd.add_node", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "text", label: "mmd.f.text", size: "l" },
      { key: "shape", label: "mmd.f.shape", type: "select", options: FC_SHAPES.slice(0, 9).map(([v, k]) => [v, k]), size: "s" },
      { key: "group", label: "mmd.swimlane.lane", size: "s", suggest: groupIds },
      { key: "color", label: "mmd.f.color", type: "color" },
    ], make: (m) => ({ id: nextId(m, "n"), text: "", shape: "rect", group: groupIds(m)[0] ?? "" }) },
    { key: "edges", label: "mmd.f.edges", add: "mmd.add_edge", fields: edgeFields, make: () => ({ from: "", to: "", line: "solid", head: "arrow" }) },
  ],
  parse(body, head) {
    const m = parseFlow(body, { groupWord: "subgraph" });
    m.dir = /\b(TB|TD|BT|LR|RL)\b/.exec(head)?.[1]?.replace("TD", "TB") ?? "TB";
    return m;
  },
  write(m) {
    return [`swimlane-beta${s(m.dir) && s(m.dir) !== "TB" ? " " + s(m.dir) : ""}`,
      ...writeFlow(m, { groupWord: "subgraph", groupTitle: (g) => `${s(g.id)}${s(g.title) ? ` [${s(g.title)}]` : ""}` }).map((l) => l.replace(/^ {4}/, "  "))].join("\n");
  },
};

const AF_SHAPES: [string, string][] = [["", "–"], ["task", "mmd.af.task"], ["tool", "mmd.af.tool"], ["input", "mmd.af.input"], ["decision", "mmd.af.decision"], ["refdoc", "mmd.af.refdoc"], ["action", "mmd.af.action"]];

export const agentflow: MermaidKind = {
  id: "agentflow", label: "mmd.kind.agentflow", detect: /^agentflow(-beta)?\b/,
  example: `agentflow LR
  brief["Brief"]@{ shape: input }
  draft["Draft copy"]@{ shape: task }
  check["Good enough?"]@{ shape: decision }
  publish["Publish"]@{ shape: action }
  brief --> draft --> check
  check -- yes --> publish
  check -- no --> draft`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: [["", "mmd.dir.td"], ["LR", "mmd.dir.lr"], ["BT", "mmd.dir.bt"], ["RL", "mmd.dir.rl"]], size: "s" }],
  lists: [
    { key: "nodes", label: "mmd.f.nodes", add: "mmd.add_node", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "text", label: "mmd.f.label", size: "l" },
      { key: "ashape", label: "mmd.f.shape", type: "select", options: AF_SHAPES, size: "s" },
      { key: "group", label: "mmd.af.flow", size: "s", suggest: groupIds },
    ], make: (m) => ({ id: nextId(m, "step"), text: "", ashape: "task" }) },
    { key: "edges", label: "mmd.f.edges", add: "mmd.add_edge", fields: [
      edgeFields[0], edgeFields[1], edgeFields[2],
      { key: "line", label: "mmd.af.kind", type: "select", options: [["solid", "mmd.af.sequence"], ["dotted", "mmd.af.reference"], ["fail", "mmd.af.failure"]], size: "s" },
    ], make: () => ({ from: "", to: "", line: "solid", head: "arrow" }) },
    { key: "groups", label: "mmd.af.flows", add: "mmd.add_group", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "title", label: "mmd.f.label", size: "l" }, { key: "parent", label: "mmd.f.parent", size: "s", suggest: groupIds }],
      make: (m) => ({ id: `flow${rows(m, "groups").length + 1}`, title: "" }) },
  ],
  parse(body, head) {
    const m = parseFlow(body, { groupWord: "flow" });
    m.dir = /\b(TB|TD|BT|LR|RL)\b/.exec(head)?.[1] ?? "";
    for (const n of rows(m, "nodes")) {
      const sh = /^shape:\s*([\w-]+)$/.exec(s(n.meta));
      if (sh) { n.ashape = sh[1]; delete n.meta; }
    }
    for (const e of rows(m, "edges")) if (e.head === "cross") e.line = "fail";
    return m;
  },
  write(m) {
    const copy: Model = { ...m, nodes: rows(m, "nodes").map((n) => ({ ...n, shape: "rect", meta: n.ashape ? `shape: ${s(n.ashape)}` : n.meta })),
      edges: rows(m, "edges").map((e) => (e.line === "fail" ? { ...e, line: "solid", head: "cross" } : e.line === "dotted" ? { ...e, head: "none" } : { ...e, head: "arrow" })) };
    return [`agentflow${s(m.dir) ? " " + s(m.dir) : ""}`, ...writeFlow(copy, { groupWord: "flow", forceShape: true })].join("\n");
  },
};
