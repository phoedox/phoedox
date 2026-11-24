// Diagrams of things and their relations: state, class, ER, requirement,
// C4, architecture, use case, Wardley map, event model.
import { IND, extraLines, keep, q, rows, s, splitArgs, takeBlock, unq, type FieldDef, type MermaidKind, type Model, type Row } from "./core";

const DIRS: [string, string][] = [["", "–"], ["TB", "mmd.dir.td"], ["LR", "mmd.dir.lr"], ["BT", "mmd.dir.bt"], ["RL", "mmd.dir.rl"]];
const ids = (key: string, field = "id") => (m: Model) => rows(m, key).map((r) => s(r[field])).filter(Boolean);

// ---------- state ----------
export const state: MermaidKind = {
  id: "state", label: "mmd.kind.state", detect: /^stateDiagram(-v2)?\b/,
  example: `stateDiagram-v2
    [*] --> Off
    Off --> Booting : power on
    Booting --> Running
    Running --> Off : power off
    Running --> [*]`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: DIRS, size: "s" }],
  lists: [
    { key: "states", label: "mmd.state.states", hint: "mmd.state.hint", add: "mmd.add_state", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" },
      { key: "kind", label: "mmd.f.kind", type: "select", options: [["", "mmd.state.normal"], ["choice", "mmd.state.choice"], ["fork", "mmd.state.fork"], ["join", "mmd.state.join"]], size: "s" },
    ], make: (m) => ({ id: `s${rows(m, "states").length + 1}`, label: "" }) },
    { key: "transitions", label: "mmd.state.transitions", add: "mmd.add_transition", fields: [
      { key: "from", label: "mmd.f.from", size: "s", suggest: (m) => ["[*]", ...ids("states")(m)] }, { key: "to", label: "mmd.f.to", size: "s", suggest: (m) => ["[*]", ...ids("states")(m)] },
      { key: "label", label: "mmd.f.label", size: "l" },
    ], make: () => ({ from: "", to: "", label: "" }) },
  ],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], states: [], transitions: [] };
    const extra: string[] = [];
    const st = new Map<string, Row>();
    const see = (id: string) => {
      if (id === "[*]" || st.has(id)) return st.get(id);
      const r: Row = { id, label: "" };
      st.set(id, r);
      (m.states as Row[]).push(r);
      return r;
    };
    for (let i = 0; i < body.length; i++) {
      const l = body[i].trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if (/^state\s+.*\{\s*$/.test(l) || /^note\b(?!.*:)/.test(l)) {
        const end = /^note\b/.test(l) ? body.findIndex((b, k) => k > i && /^end note\b/.test(b.trim())) : takeBlock(body, i, /\{\s*$/, /^\}$/);
        const stop = end < 0 ? body.length - 1 : end;
        for (let k = i; k <= stop; k++) extra.push(body[k].replace(/\s+$/, ""));
        i = stop;
        continue;
      }
      if ((x = /^direction\s+(TB|LR|BT|RL)$/.exec(l))) m.dir = x[1];
      else if ((x = /^(\[\*\]|[\w.-]+)\s*-->\s*(\[\*\]|[\w.-]+)\s*(?::\s*(.*))?$/.exec(l))) {
        see(x[1]); see(x[2]);
        (m.transitions as Row[]).push({ from: x[1], to: x[2], label: x[3] ?? "" });
      } else if ((x = /^state\s+"([^"]*)"\s+as\s+([\w.-]+)$/.exec(l))) see(x[2])!.label = x[1];
      else if ((x = /^state\s+([\w.-]+)\s+<<(choice|fork|join)>>$/.exec(l))) see(x[1])!.kind = x[2];
      else if ((x = /^([\w.-]+)\s*:\s*(.*)$/.exec(l)) && !/^(note|classDef|class|style)$/.test(x[1])) {
        const r = see(x[1])!;
        r.label = r.label ? `${s(r.label)}\n${x[2]}` : x[2];
      } else if ((x = /^state\s+([\w.-]+)$/.exec(l))) see(x[1]);
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = [s(m.kw) || "stateDiagram-v2"];
    if (m.dir) out.push(`${IND}direction ${s(m.dir)}`);
    for (const r of rows(m, "states")) {
      if (!s(r.id)) continue;
      if (r.kind) out.push(`${IND}state ${s(r.id)} <<${s(r.kind)}>>`);
      else if (s(r.label) && !s(r.label).includes("\n")) out.push(`${IND}state "${s(r.label).replace(/"/g, "'")}" as ${s(r.id)}`);
      else if (s(r.label)) for (const d of s(r.label).split("\n")) out.push(`${IND}${s(r.id)} : ${d}`);
    }
    for (const t of rows(m, "transitions")) if (s(t.from) && s(t.to)) out.push(`${IND}${s(t.from)} --> ${s(t.to)}${s(t.label) ? " : " + s(t.label) : ""}`);
    return [...out, ...s(m.extra).split("\n").filter((l) => l.trim()).map((l) => (l.startsWith(" ") ? l : IND + l))].join("\n");
  },
};

// ---------- class ----------
const CLASS_REL_NAMES: Record<string, string> = { "<|--": "inherit", "--|>": "inherit", "*--": "composition", "--*": "composition", "o--": "aggregation", "--o": "aggregation",
  "-->": "association", "<--": "association", "--": "link", "..>": "dependency", "<..": "dependency", "..|>": "realization", "<|..": "realization", "..": "dashed" };
const CLASS_REL = ["<|--", "--|>", "*--", "--*", "o--", "--o", "-->", "<--", "--", "..>", "<..", "..|>", "<|..", ".."];
const CLASS_REL_RE = new RegExp(`^([\\w~.-]+)\\s*(?:"([^"]*)"\\s*)?(${CLASS_REL.map((r) => r.replace(/[|*.]/g, "\\$&")).join("|")})\\s*(?:"([^"]*)"\\s*)?([\\w~.-]+)\\s*(?::\\s*(.*))?$`);

export const classDiagram: MermaidKind = {
  id: "class", label: "mmd.kind.class", detect: /^classDiagram(-v2)?\b/,
  example: `classDiagram
    class Device {
        +String name
        +String ip
        +ping() bool
    }
    class Router {
        +int ports
    }
    class Nas {
        +int disks
        +backup()
    }
    Device <|-- Router
    Device <|-- Nas
    Router "1" --> "*" Device : connects`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: DIRS, size: "s" }],
  lists: [
    { key: "classes", label: "mmd.class.classes", add: "mmd.add_class", fields: [
      { key: "name", label: "mmd.f.name", size: "m" }, { key: "annotation", label: "mmd.class.annotation", placeholder: "interface", size: "s" },
      { key: "members", label: "mmd.class.members", type: "lines", placeholder: "+String name\n+save() bool", size: "l" },
    ], make: () => ({ name: "", members: "" }) },
    { key: "relations", label: "mmd.f.relations", add: "mmd.add_relation", fields: [
      { key: "from", label: "mmd.f.from", size: "s", suggest: ids("classes", "name") }, { key: "fromCard", label: "mmd.class.card", placeholder: "1", size: "s" },
      { key: "type", label: "mmd.f.kind", type: "select", options: CLASS_REL.map((r) => [r, `mmd.class.rel_${CLASS_REL_NAMES[r]}`]), showValue: true, size: "s" },
      { key: "toCard", label: "mmd.class.card", placeholder: "*", size: "s" }, { key: "to", label: "mmd.f.to", size: "s", suggest: ids("classes", "name") },
      { key: "label", label: "mmd.f.label", size: "m" },
    ], make: () => ({ from: "", type: "-->", to: "" }) },
  ],
  parse(body) {
    const m: Model = { classes: [], relations: [] };
    const extra: string[] = [];
    const cl = new Map<string, Row>();
    const see = (name: string) => {
      let r = cl.get(name);
      if (!r) { r = { name, members: "" }; cl.set(name, r); (m.classes as Row[]).push(r); }
      return r;
    };
    const addMember = (r: Row, v: string) => { r.members = s(r.members) ? `${s(r.members)}\n${v}` : v; };
    for (let i = 0; i < body.length; i++) {
      const l = body[i].trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^class\s+([\w~.-]+)\s*\{\s*(.*?)\}?$/.exec(l)) && l.includes("{")) {
        const r = see(x[1]);
        if (l.endsWith("}")) { if (x[2].trim()) addMember(r, x[2].trim()); continue; }
        for (i++; i < body.length && body[i].trim() !== "}"; i++) {
          const mem = body[i].trim();
          const ann = /^<<(.+)>>$/.exec(mem);
          if (ann) r.annotation = ann[1];
          else if (mem) addMember(r, mem);
        }
        continue;
      }
      if (/^namespace\s+.*\{\s*$/.test(l)) {
        const end = takeBlock(body, i, /\{\s*$/, /^\}$/);
        for (let k = i; k <= end; k++) extra.push(body[k].replace(/\s+$/, ""));
        i = end;
        continue;
      }
      if ((x = /^direction\s+(TB|LR|BT|RL)$/.exec(l))) m.dir = x[1];
      else if ((x = /^class\s+([\w~.-]+)$/.exec(l))) see(x[1]);
      else if ((x = /^<<(.+)>>\s+([\w~.-]+)$/.exec(l))) see(x[2]).annotation = x[1];
      else if ((x = CLASS_REL_RE.exec(l))) {
        see(x[1]); see(x[5]);
        (m.relations as Row[]).push({ from: x[1], fromCard: x[2] ?? "", type: x[3], toCard: x[4] ?? "", to: x[5], label: x[6] ?? "" });
      } else if ((x = /^([\w~.-]+)\s*:\s*(.+)$/.exec(l)) && !/^(note|style|cssClass|classDef|click|link|callback)$/.test(x[1])) addMember(see(x[1]), x[2].trim());
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["classDiagram"];
    if (m.dir) out.push(`${IND}direction ${s(m.dir)}`);
    for (const c of rows(m, "classes")) {
      if (!s(c.name)) continue;
      const mem = s(c.members).split("\n").map((l) => l.trim()).filter(Boolean);
      if (!mem.length && !c.annotation) { out.push(`${IND}class ${s(c.name)}`); continue; }
      out.push(`${IND}class ${s(c.name)} {`);
      if (c.annotation) out.push(`${IND}${IND}<<${s(c.annotation)}>>`);
      for (const l of mem) out.push(IND + IND + l);
      out.push(`${IND}}`);
    }
    for (const r of rows(m, "relations")) {
      if (!s(r.from) || !s(r.to)) continue;
      out.push(`${IND}${s(r.from)}${s(r.fromCard) ? ` "${s(r.fromCard)}"` : ""} ${s(r.type) || "-->"}${s(r.toCard) ? ` "${s(r.toCard)}"` : ""} ${s(r.to)}${s(r.label) ? " : " + s(r.label) : ""}`);
    }
    return [...out, ...s(m.extra).split("\n").filter((l) => l.trim()).map((l) => (l.startsWith(" ") ? l : IND + l))].join("\n");
  },
};

// ---------- ER ----------
const ER_LEFT: [string, string][] = [["||", "mmd.er.one"], ["|o", "mmd.er.zero_one"], ["}|", "mmd.er.one_many"], ["}o", "mmd.er.zero_many"]];
const ER_RIGHT: [string, string][] = [["||", "mmd.er.one"], ["o|", "mmd.er.zero_one"], ["|{", "mmd.er.one_many"], ["o{", "mmd.er.zero_many"]];
const ER_RE = /^("[^"]*"|[\w-]+)\s*(\|o|\|\||\}o|\}\|)(--|\.\.)(o\||\|\||o\{|\|\{)\s*("[^"]*"|[\w-]+)\s*:\s*(.+)$/;

export const er: MermaidKind = {
  id: "er", label: "mmd.kind.er", detect: /^erDiagram\b/,
  example: `erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE_ITEM : contains
    CUSTOMER {
        string name
        string email PK
    }
    ORDER {
        int id PK
        date created
    }`,
  lists: [
    { key: "entities", label: "mmd.er.entities", add: "mmd.add_entity", fields: [
      { key: "name", label: "mmd.f.name", size: "m" }, { key: "attributes", label: "mmd.er.attributes", type: "lines", placeholder: "string name\nint id PK", size: "l" },
    ], make: () => ({ name: "", attributes: "" }) },
    { key: "relations", label: "mmd.f.relations", add: "mmd.add_relation", fields: [
      { key: "a", label: "mmd.f.from", size: "s", suggest: ids("entities", "name") }, { key: "left", label: "mmd.er.card_a", type: "select", options: ER_LEFT, size: "s" },
      { key: "ident", label: "mmd.er.identifying", type: "check" },
      { key: "right", label: "mmd.er.card_b", type: "select", options: ER_RIGHT, size: "s" }, { key: "b", label: "mmd.f.to", size: "s", suggest: ids("entities", "name") },
      { key: "label", label: "mmd.f.label", size: "m" },
    ], make: () => ({ a: "", left: "||", ident: true, right: "o{", b: "", label: "has" }) },
  ],
  parse(body) {
    const m: Model = { entities: [], relations: [] };
    const extra: string[] = [];
    const en = new Map<string, Row>();
    const see = (name: string) => {
      let r = en.get(name);
      if (!r) { r = { name, attributes: "" }; en.set(name, r); (m.entities as Row[]).push(r); }
      return r;
    };
    for (let i = 0; i < body.length; i++) {
      const l = body[i].trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^("[^"]*"|[\w-]+(?:\["[^"]*"\])?)\s*\{\s*$/.exec(l))) {
        const r = see(x[1]);
        const attrs: string[] = [];
        for (i++; i < body.length && body[i].trim() !== "}"; i++) if (body[i].trim()) attrs.push(body[i].trim());
        r.attributes = attrs.join("\n");
        continue;
      }
      if ((x = ER_RE.exec(l))) {
        see(x[1]); see(x[5]);
        (m.relations as Row[]).push({ a: x[1], left: x[2], ident: x[3] === "--", right: x[4], b: x[5], label: unq(x[6]) });
      } else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["erDiagram"];
    for (const r of rows(m, "relations")) if (s(r.a) && s(r.b)) out.push(`${IND}${s(r.a)} ${s(r.left) || "||"}${r.ident === false ? ".." : "--"}${s(r.right) || "o{"} ${s(r.b)} : ${/^[\w-]+$/.test(s(r.label)) ? s(r.label) : `"${s(r.label).replace(/"/g, "'")}"`}`);
    for (const e of rows(m, "entities")) {
      const at = s(e.attributes).split("\n").map((l) => l.trim()).filter(Boolean);
      if (!s(e.name) || !at.length) continue;
      out.push(`${IND}${s(e.name)} {`, ...at.map((a) => IND + IND + a), `${IND}}`);
    }
    return [...out, ...extraLines(m)].join("\n");
  },
};

// ---------- requirement ----------
const REQ_TYPES = ["requirement", "functionalRequirement", "interfaceRequirement", "performanceRequirement", "physicalRequirement", "designConstraint"];
const REQ_RELS = ["contains", "copies", "derives", "satisfies", "verifies", "refines", "traces"];

export const requirement: MermaidKind = {
  id: "requirement", label: "mmd.kind.requirement", detect: /^requirementDiagram\b/,
  example: `requirementDiagram
    requirement backup_req {
        id: 1
        text: Back up all laptops every night.
        risk: high
        verifymethod: test
    }
    element nas_backup {
        type: service
    }
    nas_backup - satisfies -> backup_req`,
  lists: [
    { key: "reqs", label: "mmd.req.requirements", add: "mmd.add_requirement", fields: [
      { key: "type", label: "mmd.f.kind", type: "select", options: REQ_TYPES.map((t) => [t, `mmd.req.t.${t}`]), size: "s" },
      { key: "name", label: "mmd.f.name", size: "s" }, { key: "rid", label: "mmd.f.id", size: "s" },
      { key: "text", label: "mmd.f.text", size: "l" },
      { key: "risk", label: "mmd.req.risk", type: "select", options: [["", "–"], ["low", "mmd.req.low"], ["medium", "mmd.req.medium"], ["high", "mmd.req.high"]], size: "s" },
      { key: "verify", label: "mmd.req.verify", type: "select", options: [["", "–"], ["analysis", "mmd.req.analysis"], ["inspection", "mmd.req.inspection"], ["test", "mmd.req.test"], ["demonstration", "mmd.req.demonstration"]], size: "s" },
    ], make: (m) => ({ type: "requirement", name: `req${rows(m, "reqs").length + 1}`, rid: String(rows(m, "reqs").length + 1), text: "" }) },
    { key: "elements", label: "mmd.req.elements", add: "mmd.add_element", fields: [
      { key: "name", label: "mmd.f.name", size: "s" }, { key: "etype", label: "mmd.f.kind", size: "s" }, { key: "docref", label: "mmd.req.docref", size: "l" },
    ], make: () => ({ name: "", etype: "" }) },
    { key: "relations", label: "mmd.f.relations", add: "mmd.add_relation", fields: [
      { key: "from", label: "mmd.f.from", size: "s", suggest: (m) => [...ids("reqs", "name")(m), ...ids("elements", "name")(m)] },
      { key: "rel", label: "mmd.f.kind", type: "select", options: REQ_RELS.map((r) => [r, `mmd.req.r.${r}`]), size: "s" },
      { key: "to", label: "mmd.f.to", size: "s", suggest: (m) => [...ids("reqs", "name")(m), ...ids("elements", "name")(m)] },
    ], make: () => ({ from: "", rel: "satisfies", to: "" }) },
  ],
  parse(body) {
    const m: Model = { reqs: [], elements: [], relations: [] };
    const extra: string[] = [];
    for (let i = 0; i < body.length; i++) {
      const l = body[i].trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^(\w+)\s+("[^"]*"|[\w-]+)\s*\{\s*$/.exec(l)) && (REQ_TYPES.includes(x[1]) || x[1] === "element")) {
        const props: Record<string, string> = {};
        for (i++; i < body.length && body[i].trim() !== "}"; i++) {
          const p = /^(\w+)\s*:\s*(.*)$/.exec(body[i].trim());
          if (p) props[p[1].toLowerCase()] = unq(p[2]);
        }
        if (x[1] === "element") (m.elements as Row[]).push({ name: unq(x[2]), etype: props.type ?? "", docref: props.docref ?? "" });
        else (m.reqs as Row[]).push({ type: x[1], name: unq(x[2]), rid: props.id ?? "", text: props.text ?? "", risk: (props.risk ?? "").toLowerCase(), verify: (props.verifymethod ?? "").toLowerCase() });
        continue;
      }
      if ((x = /^("[^"]*"|[\w-]+)\s*-\s*(\w+)\s*->\s*("[^"]*"|[\w-]+)$/.exec(l))) (m.relations as Row[]).push({ from: unq(x[1]), rel: x[2], to: unq(x[3]) });
      else if ((x = /^("[^"]*"|[\w-]+)\s*<-\s*(\w+)\s*-\s*("[^"]*"|[\w-]+)$/.exec(l))) (m.relations as Row[]).push({ from: unq(x[3]), rel: x[2], to: unq(x[1]) });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const nm = (v: unknown) => (/^[\w-]+$/.test(s(v)) ? s(v) : `"${s(v)}"`);
    const out = ["requirementDiagram"];
    for (const r of rows(m, "reqs")) {
      if (!s(r.name)) continue;
      out.push(`${IND}${s(r.type) || "requirement"} ${nm(r.name)} {`);
      if (s(r.rid)) out.push(`${IND}${IND}id: ${s(r.rid)}`);
      if (s(r.text)) out.push(`${IND}${IND}text: ${s(r.text)}`);
      if (s(r.risk)) out.push(`${IND}${IND}risk: ${s(r.risk)}`);
      if (s(r.verify)) out.push(`${IND}${IND}verifymethod: ${s(r.verify)}`);
      out.push(`${IND}}`);
    }
    for (const e of rows(m, "elements")) {
      if (!s(e.name)) continue;
      out.push(`${IND}element ${nm(e.name)} {`);
      if (s(e.etype)) out.push(`${IND}${IND}type: ${s(e.etype)}`);
      if (s(e.docref)) out.push(`${IND}${IND}docref: ${s(e.docref)}`);
      out.push(`${IND}}`);
    }
    for (const r of rows(m, "relations")) if (s(r.from) && s(r.to)) out.push(`${IND}${nm(r.from)} - ${s(r.rel) || "satisfies"} -> ${nm(r.to)}`);
    return [...out, ...extraLines(m)].join("\n");
  },
};

// ---------- C4 ----------
const C4_ELEMENTS = ["Person", "Person_Ext", "System", "System_Ext", "SystemDb", "SystemDb_Ext", "SystemQueue", "SystemQueue_Ext",
  "Container", "Container_Ext", "ContainerDb", "ContainerDb_Ext", "ContainerQueue", "ContainerQueue_Ext",
  "Component", "Component_Ext", "ComponentDb", "ComponentDb_Ext", "ComponentQueue", "ComponentQueue_Ext"];
const C4_BOUNDARIES = ["Boundary", "Enterprise_Boundary", "System_Boundary", "Container_Boundary", "Deployment_Node", "Node", "Node_L", "Node_R"];
const C4_RELS = ["Rel", "BiRel", "Rel_U", "Rel_Up", "Rel_D", "Rel_Down", "Rel_L", "Rel_Left", "Rel_R", "Rel_Right", "Rel_Back"];
const withTech = (macro: string) => /^(Container|Component)/.test(macro);

export const c4: MermaidKind = {
  id: "c4", label: "mmd.kind.c4", detect: /^C4(Context|Container|Component|Dynamic|Deployment)\b/,
  example: `C4Context
    title Home IT
    Person(family, "Family", "Uses laptops and phones")
    System_Boundary(home, "Home network") {
        System(nas, "NAS", "Files and backups")
        System(router, "Router", "Internet access")
    }
    System_Ext(cloud, "Cloud storage", "Off-site copy")
    Rel(family, nas, "Saves photos to")
    Rel(nas, cloud, "Copies backups to", "HTTPS")`,
  fields: [
    { key: "kw", label: "mmd.c4.level", type: "select", options: [["C4Context", "Context"], ["C4Container", "Container"], ["C4Component", "Component"], ["C4Dynamic", "Dynamic"], ["C4Deployment", "Deployment"]], size: "s" },
    { key: "title", label: "mmd.f.title", size: "l" },
  ],
  lists: [
    { key: "elements", label: "mmd.c4.elements", add: "mmd.add_element", fields: [
      { key: "macro", label: "mmd.f.kind", type: "select", options: C4_ELEMENTS.map((e) => [e, e.replace(/_/g, " ")]), size: "s" },
      { key: "alias", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "m" },
      { key: "tech", label: "mmd.c4.tech", size: "s", show: (r) => withTech(s(r.macro)) }, { key: "descr", label: "mmd.f.description", size: "l" },
      { key: "group", label: "mmd.c4.boundary", size: "s", suggest: ids("boundaries", "alias") },
    ], make: (m) => ({ macro: "System", alias: `e${rows(m, "elements").length + 1}`, label: "" }) },
    { key: "rels", label: "mmd.f.relations", add: "mmd.add_relation", fields: [
      { key: "macro", label: "mmd.f.kind", type: "select", options: C4_RELS.map((r) => [r, r.replace(/_/g, " ")]), size: "s" },
      { key: "from", label: "mmd.f.from", size: "s", suggest: ids("elements", "alias") }, { key: "to", label: "mmd.f.to", size: "s", suggest: ids("elements", "alias") },
      { key: "label", label: "mmd.f.label", size: "m" }, { key: "tech", label: "mmd.c4.tech", size: "s" },
    ], make: () => ({ macro: "Rel", from: "", to: "", label: "" }) },
    { key: "boundaries", label: "mmd.c4.boundaries", add: "mmd.add_boundary", fields: [
      { key: "macro", label: "mmd.f.kind", type: "select", options: C4_BOUNDARIES.map((b) => [b, b.replace(/_/g, " ")]), size: "s" },
      { key: "alias", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" },
      { key: "parent", label: "mmd.f.parent", size: "s", suggest: ids("boundaries", "alias") },
    ], make: (m) => ({ macro: "System_Boundary", alias: `b${rows(m, "boundaries").length + 1}`, label: "" }) },
  ],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], elements: [], rels: [], boundaries: [] };
    const extra: string[] = [];
    const stack: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^title\s+(.*)$/.exec(l))) { m.title = x[1]; continue; }
      if (l === "}" && stack.length) { stack.pop(); continue; }
      if ((x = /^(\w+)\s*\((.*)\)\s*(\{)?$/.exec(l))) {
        const [macro, argText, open] = [x[1], x[2], x[3]];
        const args = splitArgs(argText);
        const plain = args.filter((a) => !a.startsWith("$")).map(unq);
        const more = args.filter((a) => a.startsWith("$")).join(", ");
        if (C4_BOUNDARIES.includes(macro) && open) {
          (m.boundaries as Row[]).push({ macro, alias: plain[0], label: plain[1] ?? "", parent: stack[stack.length - 1] ?? "", more });
          stack.push(plain[0]);
          continue;
        }
        if (C4_ELEMENTS.includes(macro) && !open) {
          const tech = withTech(macro);
          (m.elements as Row[]).push({ macro, alias: plain[0], label: plain[1] ?? "", tech: tech ? plain[2] ?? "" : "", descr: (tech ? plain[3] : plain[2]) ?? "", group: stack[stack.length - 1] ?? "", more });
          continue;
        }
        if (C4_RELS.includes(macro) && !open) {
          (m.rels as Row[]).push({ macro, from: plain[0], to: plain[1], label: plain[2] ?? "", tech: plain[3] ?? "", more });
          continue;
        }
      }
      keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const qq = (v: unknown) => `"${s(v).replace(/"/g, "'")}"`;
    const out = [s(m.kw) || "C4Context"];
    if (m.title) out.push(`${IND}title ${s(m.title)}`);
    const els = rows(m, "elements").filter((e) => s(e.alias));
    const bnds = rows(m, "boundaries").filter((b) => s(b.alias));
    const el = (e: Row, pad: string) => {
      const args = [s(e.alias), qq(e.label)];
      if (withTech(s(e.macro))) args.push(qq(e.tech), qq(e.descr));
      else if (s(e.descr)) args.push(qq(e.descr));
      while (args.length > 2 && args[args.length - 1] === '""') args.pop();
      if (s(e.more)) args.push(s(e.more));
      return `${pad}${s(e.macro) || "System"}(${args.join(", ")})`;
    };
    const bnd = (b: Row, depth: number) => {
      const pad = IND.repeat(depth);
      out.push(`${pad}${s(b.macro) || "Boundary"}(${s(b.alias)}, ${qq(b.label)}${s(b.more) ? ", " + s(b.more) : ""}) {`);
      for (const c of bnds.filter((x) => s(x.parent) === s(b.alias))) bnd(c, depth + 1);
      for (const e of els.filter((x) => s(x.group) === s(b.alias))) out.push(el(e, pad + IND));
      out.push(`${pad}}`);
    };
    // In list order; a boundary is written where its first element is (or at the end).
    const top = (alias: string): string => {
      const b = bnds.find((x) => s(x.alias) === alias);
      return b && s(b.parent) && bnds.some((p) => s(p.alias) === s(b.parent)) ? top(s(b.parent)) : alias;
    };
    const done = new Set<string>();
    for (const e of els) {
      const g = s(e.group) && bnds.some((b) => s(b.alias) === s(e.group)) ? top(s(e.group)) : "";
      if (!g) out.push(el(e, IND));
      else if (!done.has(g)) { done.add(g); bnd(bnds.find((b) => s(b.alias) === g)!, 1); }
    }
    for (const b of bnds.filter((x) => (!s(x.parent) || !bnds.some((p) => s(p.alias) === s(x.parent))) && !done.has(s(x.alias)))) bnd(b, 1);
    for (const r of rows(m, "rels")) {
      if (!s(r.from) || !s(r.to)) continue;
      const args = [s(r.from), s(r.to), qq(r.label)];
      if (s(r.tech)) args.push(qq(r.tech));
      if (s(r.more)) args.push(s(r.more));
      out.push(`${IND}${s(r.macro) || "Rel"}(${args.join(", ")})`);
    }
    return [...out, ...extraLines(m)].join("\n");
  },
};

// ---------- architecture ----------
const SIDES: [string, string][] = [["L", "mmd.side.l"], ["R", "mmd.side.r"], ["T", "mmd.side.t"], ["B", "mmd.side.b"]];
const ICONS = ["cloud", "database", "disk", "internet", "server"];
const archIds = (m: Model) => [...ids("services")(m), ...ids("junctions")(m)];

export const architecture: MermaidKind = {
  id: "architecture", label: "mmd.kind.architecture", detect: /^architecture(-beta)?\b/,
  example: `architecture-beta
    group home(cloud)[Home]
    service internet(internet)[Internet]
    service router(server)[Router] in home
    service nas(disk)[NAS] in home
    service db(database)[Photos DB] in home
    internet:R --> L:router
    router:R --> L:nas
    nas:B -- T:db`,
  lists: [
    { key: "services", label: "mmd.arch.services", hint: "mmd.arch.icons_hint", add: "mmd.add_service", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "icon", label: "mmd.arch.icon", size: "s", suggest: () => ICONS },
      { key: "label", label: "mmd.f.label", size: "l" }, { key: "in", label: "mmd.f.group", size: "s", suggest: ids("groups") },
    ], make: (m) => ({ id: `svc${rows(m, "services").length + 1}`, icon: "server", label: "" }) },
    { key: "edges", label: "mmd.f.edges", add: "mmd.add_edge", fields: [
      { key: "from", label: "mmd.f.from", size: "s", suggest: archIds }, { key: "fromSide", label: "mmd.arch.side", type: "select", options: SIDES, size: "s" },
      { key: "arrows", label: "mmd.f.head", type: "select", options: [["", "mmd.head.none"], [">", "mmd.head.arrow"], ["<", "mmd.arch.back"], ["<>", "mmd.head.both"]], size: "s" },
      { key: "toSide", label: "mmd.arch.side", type: "select", options: SIDES, size: "s" }, { key: "to", label: "mmd.f.to", size: "s", suggest: archIds },
    ], make: () => ({ from: "", fromSide: "R", arrows: ">", toSide: "L", to: "" }) },
    { key: "groups", label: "mmd.arch.groups", add: "mmd.add_group", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "icon", label: "mmd.arch.icon", size: "s", suggest: () => ICONS },
      { key: "label", label: "mmd.f.label", size: "l" }, { key: "in", label: "mmd.f.parent", size: "s", suggest: ids("groups") },
    ], make: (m) => ({ id: `grp${rows(m, "groups").length + 1}`, icon: "cloud", label: "" }) },
    { key: "junctions", label: "mmd.arch.junctions", add: "mmd.add_junction", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "in", label: "mmd.f.group", size: "s", suggest: ids("groups") }],
      make: (m) => ({ id: `j${rows(m, "junctions").length + 1}` }) },
  ],
  parse(body) {
    const m: Model = { services: [], edges: [], groups: [], junctions: [] };
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^(group|service)\s+([\w-]+)\(([^)]*)\)\[([^\]]*)\](?:\s+in\s+([\w-]+))?$/.exec(l))) (m[x[1] === "group" ? "groups" : "services"] as Row[]).push({ id: x[2], icon: x[3], label: x[4], in: x[5] ?? "" });
      else if ((x = /^junction\s+([\w-]+)(?:\s+in\s+([\w-]+))?$/.exec(l))) (m.junctions as Row[]).push({ id: x[1], in: x[2] ?? "" });
      else if ((x = /^([\w-]+):([LRTB])\s*(<)?--(>)?\s*([LRTB]):([\w-]+)$/.exec(l))) (m.edges as Row[]).push({ from: x[1], fromSide: x[2], arrows: `${x[3] ?? ""}${x[4] ?? ""}`, toSide: x[5], to: x[6] });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["architecture-beta"];
    const inn = (r: Row) => (s(r.in) ? ` in ${s(r.in)}` : "");
    for (const g of rows(m, "groups")) if (s(g.id)) out.push(`${IND}group ${s(g.id)}(${s(g.icon) || "cloud"})[${s(g.label)}]${inn(g)}`);
    for (const v of rows(m, "services")) if (s(v.id)) out.push(`${IND}service ${s(v.id)}(${s(v.icon) || "server"})[${s(v.label)}]${inn(v)}`);
    for (const j of rows(m, "junctions")) if (s(j.id)) out.push(`${IND}junction ${s(j.id)}${inn(j)}`);
    for (const e of rows(m, "edges")) {
      if (!s(e.from) || !s(e.to)) continue;
      const a = s(e.arrows);
      out.push(`${IND}${s(e.from)}:${s(e.fromSide) || "R"} ${a.includes("<") ? "<" : ""}--${a.includes(">") ? ">" : ""} ${s(e.toSide) || "L"}:${s(e.to)}`);
    }
    return [...out, ...extraLines(m)].join("\n");
  },
};

// ---------- use case ----------
const UC_OPS: [string, string][] = [["--", "mmd.uc.assoc"], ["-->", "mmd.head.arrow"], ["<--", "mmd.arch.back"], ["--o", "mmd.head.circle"], ["--x", "mmd.head.cross"], ["--|>", "mmd.uc.generalize"], ["include", "mmd.uc.include"], ["extend", "mmd.uc.extend"]];
const ucIds = (m: Model) => [...ids("actors")(m), ...ids("cases")(m)];

export const usecase: MermaidKind = {
  id: "usecase", label: "mmd.kind.usecase", detect: /^usecase(-beta)?\b/,
  example: `usecase-beta
direction LR
actor parent("Parent")
actor child("Child")
backup("Back up photos")
share("Share an album")
restore["Restore a file"]
parent --> backup
child --> share
backup ..> : include restore`,
  fields: [{ key: "dir", label: "mmd.f.direction", type: "select", options: DIRS, size: "s" }],
  lists: [
    { key: "actors", label: "mmd.uc.actors", add: "mmd.add_actor", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" }],
      make: (m) => ({ id: `actor${rows(m, "actors").length + 1}`, label: "" }) },
    { key: "cases", label: "mmd.uc.cases", add: "mmd.add_usecase", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" },
      { key: "box", label: "mmd.f.shape", type: "select", options: [["", "mmd.shape.ellipse"], ["rect", "mmd.shape.rect"]], size: "s" },
      { key: "boundary", label: "mmd.uc.boundary", size: "s", suggest: ids("boundaries") },
    ], make: (m) => ({ id: `uc${rows(m, "cases").length + 1}`, label: "" }) },
    { key: "relations", label: "mmd.f.relations", add: "mmd.add_relation", fields: [
      { key: "from", label: "mmd.f.from", size: "s", suggest: ucIds }, { key: "op", label: "mmd.f.kind", type: "select", options: UC_OPS, size: "s" },
      { key: "to", label: "mmd.f.to", size: "s", suggest: ucIds }, { key: "label", label: "mmd.f.label", size: "m", show: (r) => !["include", "extend"].includes(s(r.op)) },
    ], make: () => ({ from: "", op: "-->", to: "" }) },
    { key: "boundaries", label: "mmd.uc.boundaries", add: "mmd.add_boundary", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "title", label: "mmd.f.title", size: "l" }],
      make: (m) => ({ id: `sys${rows(m, "boundaries").length + 1}`, title: "" }) },
  ],
  parse(body) {
    const m: Model = { actors: [], cases: [], relations: [], boundaries: [] };
    const extra: string[] = [];
    let box = "";
    const cases = new Map<string, Row>();
    const seeCase = (id: string, label = "", rect = false) => {
      if ((m.actors as Row[]).some((a) => a.id === id)) return;
      let r = cases.get(id);
      if (!r) { r = { id, label, box: rect ? "rect" : "", boundary: box }; cases.set(id, r); (m.cases as Row[]).push(r); }
      else if (label) { r.label = label; r.box = rect ? "rect" : ""; }
    };
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if ((x = /^direction\s+(TD|TB|BT|LR|RL)$/.exec(l))) m.dir = x[1] === "TD" ? "TB" : x[1];
      else if ((x = /^systemBoundary\s+(\w+)(?:\["([^"]*)"\])?$/.exec(l))) { (m.boundaries as Row[]).push({ id: x[1], title: x[2] ?? "" }); box = x[1]; }
      else if (l === "end" && box) box = "";
      else if ((x = /^actor\s+(\w+)(?:\("([^"]*)"\))?$/.exec(l))) (m.actors as Row[]).push({ id: x[1], label: x[2] ?? "" });
      else if ((x = /^(\w+)\s*\.\.>\s*:\s*(include|extend)\s+(\w+)$/.exec(l))) { seeCase(x[1]); seeCase(x[3]); (m.relations as Row[]).push({ from: x[1], op: x[2], to: x[3] }); }
      else if ((x = /^(\w+)\s+(?:--\s+"([^"]*)"\s+)?(--|-->|<--|--o|--x|--\|>)\s+(\w+)$/.exec(l))) { seeCase(x[1]); seeCase(x[4]); (m.relations as Row[]).push({ from: x[1], op: x[3], label: x[2] ?? "", to: x[4] }); }
      else if ((x = /^(\w+)\("([^"]*)"\)$/.exec(l)) || (x = /^(\w+)\(([^)"]*)\)$/.exec(l))) seeCase(x[1], x[2]);
      else if ((x = /^(\w+)\[([^\]]*)\]$/.exec(l))) seeCase(x[1], unq(x[2]), true);
      else if (/^\w+$/.test(l)) seeCase(l);
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["usecase-beta"];
    if (m.dir) out.push(`direction ${s(m.dir)}`);
    for (const a of rows(m, "actors")) if (s(a.id)) out.push(`actor ${s(a.id)}${s(a.label) ? `("${s(a.label)}")` : ""}`);
    const caseLine = (c: Row, pad = "") => `${pad}${s(c.id)}${s(c.label) ? (c.box === "rect" ? `[${s(c.label)}]` : `("${s(c.label)}")`) : ""}`;
    const bnds = rows(m, "boundaries").filter((b) => s(b.id));
    for (const c of rows(m, "cases")) if (s(c.id) && !bnds.some((b) => b.id === c.boundary)) out.push(caseLine(c));
    for (const b of bnds) {
      out.push(`systemBoundary ${s(b.id)}${s(b.title) ? `["${s(b.title)}"]` : ""}`);
      for (const c of rows(m, "cases")) if (s(c.id) && c.boundary === b.id) out.push(caseLine(c, "  "));
      out.push("end");
    }
    for (const r of rows(m, "relations")) {
      if (!s(r.from) || !s(r.to)) continue;
      const op = s(r.op) || "-->";
      if (op === "include" || op === "extend") out.push(`${s(r.from)} ..> : ${op} ${s(r.to)}`);
      else out.push(`${s(r.from)} ${s(r.label) && op !== "--" ? `-- "${s(r.label)}" ` : ""}${op} ${s(r.to)}`);
    }
    return [...out, ...extraLines(m, "")].join("\n");
  },
};

// ---------- Wardley map ----------
const WL_LINKS: [string, string][] = [["->", "mmd.wl.dep"], ["-->", "mmd.wl.dep_strong"], ["-.->", "mmd.wl.dep_dotted"], ["+>", "mmd.wl.flow"], ["+<", "mmd.wl.flow_back"], ["+<>", "mmd.wl.flow_both"]];
const wlNames = (m: Model) => [...ids("components", "name")(m), ...ids("anchors", "name")(m)];
const coord: FieldDef[] = [
  { key: "v", label: "mmd.wl.visibility", type: "number", size: "s" }, { key: "e", label: "mmd.wl.evolution", type: "number", size: "s" },
];

export const wardley: MermaidKind = {
  id: "wardley", label: "mmd.kind.wardley", detect: /^wardley(-beta)?\b/,
  example: `wardley-beta
title Home backup
anchor Family [0.95, 0.6]
component Photo backup [0.8, 0.55]
component NAS [0.5, 0.45] (build)
component Cloud storage [0.35, 0.8] (buy)
component Power [0.1, 0.95]
Family -> Photo backup
Photo backup -> NAS
Photo backup -> Cloud storage
NAS -> Power`,
  fields: [{ key: "title", label: "mmd.f.title", size: "l" }],
  lists: [
    { key: "components", label: "mmd.wl.components", hint: "mmd.wl.hint", add: "mmd.add_component", fields: [
      { key: "name", label: "mmd.f.name", size: "l" }, ...coord, { key: "more", label: "mmd.wl.more", placeholder: "(build) (inertia)", size: "m" },
    ], make: () => ({ name: "", v: 0.5, e: 0.5 }) },
    { key: "anchors", label: "mmd.wl.anchors", add: "mmd.add_anchor", fields: [{ key: "name", label: "mmd.f.name", size: "l" }, ...coord], make: () => ({ name: "", v: 0.95, e: 0.5 }) },
    { key: "links", label: "mmd.f.edges", add: "mmd.add_edge", fields: [
      { key: "from", label: "mmd.f.from", size: "m", suggest: wlNames }, { key: "op", label: "mmd.f.kind", type: "select", options: WL_LINKS, size: "s" },
      { key: "to", label: "mmd.f.to", size: "m", suggest: wlNames }, { key: "label", label: "mmd.f.label", size: "m" },
    ], make: () => ({ from: "", op: "->", to: "" }) },
  ],
  parse(body) {
    const m: Model = { components: [], anchors: [], links: [] };
    const extra: string[] = [];
    for (let i = 0; i < body.length; i++) {
      const l = body[i].trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if (/^pipeline\b.*\{\s*$/.test(l)) {
        const end = takeBlock(body, i, /\{\s*$/, /^\}$/);
        for (let k = i; k <= end; k++) extra.push(body[k].replace(/\s+$/, ""));
        i = end;
        continue;
      }
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = unq(x[1]);
      else if ((x = /^(component|anchor)\s+(.+?)\s*\[\s*([\d.]+)\s*,\s*([\d.]+)\s*\]\s*(.*)$/.exec(l))) {
        const row: Row = { name: unq(x[2]), v: Number(x[3]), e: Number(x[4]) };
        if (x[1] === "component") { row.more = x[5]; (m.components as Row[]).push(row); }
        else if (!x[5]) (m.anchors as Row[]).push(row);
        else keep(extra, l);
      } else if (/^(size|evolve|note|annotations?|accelerator|deaccelerator|evolution|market|ecosystem)\b/.test(l) || /-\.-\s*\(/.test(l)) keep(extra, l);
      else if ((x = /^(.+?)\s*(\+<>|\+<|\+>|-\.->|-->|->)\s*(.+?)(?:\s*;\s*(.*))?$/.exec(l))) (m.links as Row[]).push({ from: unq(x[1]), op: x[2], to: unq(x[3]), label: x[4] ?? "" });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const nm = (v: unknown) => (/^[A-Za-z][\w -]*$/.test(s(v)) ? s(v) : `"${s(v)}"`);
    const out = ["wardley-beta"];
    if (m.title) out.push(`title ${s(m.title)}`);
    for (const a of rows(m, "anchors")) if (s(a.name)) out.push(`anchor ${nm(a.name)} [${Number(a.v) || 0}, ${Number(a.e) || 0}]`);
    for (const c of rows(m, "components")) if (s(c.name)) out.push(`component ${nm(c.name)} [${Number(c.v) || 0}, ${Number(c.e) || 0}]${s(c.more) ? " " + s(c.more) : ""}`);
    for (const l of rows(m, "links")) if (s(l.from) && s(l.to)) out.push(`${nm(l.from)} ${s(l.op) || "->"} ${nm(l.to)}${s(l.label) ? "; " + s(l.label) : ""}`);
    return [...out, ...s(m.extra).split("\n").filter((l) => l.trim())].join("\n");
  },
};

// ---------- event model ----------
const EM_TYPES: [string, string][] = [["ui", "mmd.em.ui"], ["cmd", "mmd.em.cmd"], ["evt", "mmd.em.evt"], ["rmo", "mmd.em.rmo"], ["pcr", "mmd.em.pcr"]];
const EM_ALIAS: Record<string, string> = { processor: "pcr", command: "cmd", readmodel: "rmo", event: "evt" };

export const eventmodeling: MermaidKind = {
  id: "eventmodeling", label: "mmd.kind.eventmodeling", detect: /^eventmodeling\b/,
  example: `eventmodeling

tf 01 ui CartUI
tf 02 cmd AddItem
tf 03 evt ItemAdded
tf 04 rmo CartItems`,
  lists: [{ key: "frames", label: "mmd.em.frames", hint: "mmd.em.hint", add: "mmd.add_frame", fields: [
    { key: "nn", label: "mmd.em.nn", size: "s" },
    { key: "type", label: "mmd.f.kind", type: "select", options: EM_TYPES, size: "s" },
    { key: "name", label: "mmd.f.name", size: "m" },
    { key: "reset", label: "mmd.em.reset", type: "check" },
    { key: "more", label: "mmd.em.more", placeholder: "->> 03 / { key: value }", size: "l" },
  ], make: (m) => ({ nn: String(rows(m, "frames").length + 1).padStart(2, "0"), type: "evt", name: "" }) }],
  parse(body) {
    const m: Model = { frames: [] };
    const extra: string[] = [];
    for (let i = 0; i < body.length; i++) {
      const l = body[i].trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if (/^data\b.*\{\s*$/.test(l)) {
        const end = takeBlock(body, i, /\{\s*$/, /^\}$/);
        for (let k = i; k <= end; k++) extra.push(body[k].replace(/\s+$/, ""));
        i = end;
        continue;
      }
      if ((x = /^(tf|timeframe|rf|resetframe)\s+(\d+)\s+(\w+)\s+(\S+)\s*(.*)$/.exec(l))) {
        const type = EM_ALIAS[x[3]] ?? x[3];
        if (EM_TYPES.some(([t]) => t === type)) { (m.frames as Row[]).push({ nn: x[2], type, name: x[4], reset: /^r/.test(x[1]), more: x[5] }); continue; }
      }
      keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["eventmodeling", ""];
    for (const f of rows(m, "frames")) if (s(f.nn) && s(f.name)) out.push(`${f.reset ? "rf" : "tf"} ${s(f.nn)} ${s(f.type) || "evt"} ${s(f.name).replace(/\s+/g, "")}${s(f.more) ? " " + s(f.more) : ""}`);
    return [...out, ...s(m.extra).split("\n").filter((l) => l.trim())].join("\n");
  },
};

export { q };
