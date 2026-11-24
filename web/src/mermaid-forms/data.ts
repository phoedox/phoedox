// Data diagrams: pie, quadrant chart, xy chart, sankey, radar, packet.
import { IND, extraLines, keep, q, rows, s, splitArgs, unq, type MermaidKind, type Model, type Row } from "./core";

const num = (v: string): number | string => (v.trim() !== "" && !Number.isNaN(Number(v)) ? Number(v) : v.trim());

export const pie: MermaidKind = {
  id: "pie", label: "mmd.kind.pie", detect: /^pie\b/,
  example: `pie showData
    title Pets adopted by volunteers
    "Dogs" : 386
    "Cats" : 85
    "Rats" : 15`,
  fields: [
    { key: "title", label: "mmd.f.title", size: "l" },
    { key: "showData", label: "mmd.pie.show_data", type: "check" },
  ],
  lists: [{ key: "slices", label: "mmd.pie.slices", add: "mmd.add_entry", fields: [
    { key: "label", label: "mmd.f.label", size: "l" },
    { key: "value", label: "mmd.f.value", type: "number", size: "s" },
  ], make: () => ({ label: "", value: 1 }) }],
  parse(body, head) {
    const m: Model = { showData: /\bshowData\b/.test(head), slices: [] };
    const t = /\btitle\s+(.*)$/.exec(head);
    if (t) m.title = t[1].trim();
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = x[1].trim();
      else if ((x = /^"([^"]*)"\s*:\s*([-\d.]+)$/.exec(l))) (m.slices as Row[]).push({ label: x[1], value: Number(x[2]) });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    return [`pie${m.showData ? " showData" : ""}`, ...(m.title ? [`${IND}title ${s(m.title)}`] : []),
      ...rows(m, "slices").filter((r) => s(r.label)).map((r) => `${IND}"${s(r.label).replace(/"/g, "'")}" : ${Number(r.value) || 0}`),
      ...extraLines(m)].join("\n");
  },
};

export const quadrant: MermaidKind = {
  id: "quadrant", label: "mmd.kind.quadrant", detect: /^quadrantChart\b/,
  example: `quadrantChart
    title Reach and engagement of campaigns
    x-axis Low Reach --> High Reach
    y-axis Low Engagement --> High Engagement
    quadrant-1 We should expand
    quadrant-2 Need to promote
    quadrant-3 Re-evaluate
    quadrant-4 May be improved
    Campaign A: [0.3, 0.6]
    Campaign B: [0.45, 0.23]
    Campaign C: [0.57, 0.69]`,
  fields: [
    { key: "title", label: "mmd.f.title", size: "l" },
    { key: "xLow", label: "mmd.q.x_low" }, { key: "xHigh", label: "mmd.q.x_high" },
    { key: "yLow", label: "mmd.q.y_low" }, { key: "yHigh", label: "mmd.q.y_high" },
    { key: "q1", label: "mmd.q.q1" }, { key: "q2", label: "mmd.q.q2" }, { key: "q3", label: "mmd.q.q3" }, { key: "q4", label: "mmd.q.q4" },
  ],
  lists: [{ key: "points", label: "mmd.q.points", hint: "mmd.q.points_hint", add: "mmd.add_point", fields: [
    { key: "name", label: "mmd.f.name", size: "l" },
    { key: "x", label: "x (0–1)", type: "number", size: "s" },
    { key: "y", label: "y (0–1)", type: "number", size: "s" },
    { key: "style", label: "mmd.q.style", placeholder: "radius: 10, color: #ff3300" },
  ], make: () => ({ name: "", x: 0.5, y: 0.5 }) }],
  parse(body) {
    const m: Model = { points: [] };
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = x[1];
      else if ((x = /^([xy])-axis\s+(.*?)(?:\s*-->\s*(.*))?$/.exec(l))) {
        m[`${x[1]}Low`] = x[2].trim();
        if (x[3] !== undefined) m[`${x[1]}High`] = x[3].trim();
      } else if ((x = /^quadrant-([1-4])\s+(.*)$/.exec(l))) m[`q${x[1]}`] = x[2];
      else if ((x = /^(.+?)\s*:\s*\[\s*([-\d.]+)\s*,\s*([-\d.]+)\s*\]\s*(.*)$/.exec(l))) (m.points as Row[]).push({ name: unq(x[1]), x: Number(x[2]), y: Number(x[3]), style: x[4] || undefined });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = ["quadrantChart"];
    if (m.title) out.push(`${IND}title ${s(m.title)}`);
    for (const a of ["x", "y"]) if (m[`${a}Low`]) out.push(`${IND}${a}-axis ${s(m[`${a}Low`])}${m[`${a}High`] ? ` --> ${s(m[`${a}High`])}` : ""}`);
    for (const n of [1, 2, 3, 4]) if (m[`q${n}`]) out.push(`${IND}quadrant-${n} ${s(m[`q${n}`])}`);
    for (const p of rows(m, "points")) if (s(p.name)) out.push(`${IND}${/[:\[\]]/.test(s(p.name)) ? q(p.name, true) : s(p.name)}: [${Number(p.x) || 0}, ${Number(p.y) || 0}]${p.style ? " " + s(p.style) : ""}`);
    return [...out, ...extraLines(m)].join("\n");
  },
};

export const xychart: MermaidKind = {
  id: "xychart", label: "mmd.kind.xychart", detect: /^xychart(-beta)?\b/,
  example: `xychart
    title "Sales Revenue"
    x-axis [jan, feb, mar, apr, may, jun]
    y-axis "Revenue (in $)" 4000 --> 11000
    bar [5000, 6000, 7500, 8200, 9500, 10500]
    line [5000, 6000, 7500, 8200, 9500, 10500]`,
  fields: [
    { key: "title", label: "mmd.f.title", size: "l" },
    { key: "horizontal", label: "mmd.xy.horizontal", type: "check" },
    { key: "xTitle", label: "mmd.xy.x_title" },
    { key: "xLabels", label: "mmd.xy.x_labels", placeholder: "jan, feb, mar", size: "l" },
    { key: "yTitle", label: "mmd.xy.y_title" },
    { key: "yMin", label: "mmd.f.min", type: "number", size: "s" }, { key: "yMax", label: "mmd.f.max", type: "number", size: "s" },
  ],
  lists: [{ key: "series", label: "mmd.xy.series", add: "mmd.add_series", fields: [
    { key: "type", label: "mmd.f.kind", type: "select", options: [["bar", "mmd.xy.bar"], ["line", "mmd.xy.line"]], size: "s" },
    { key: "name", label: "mmd.f.name", size: "s" },
    { key: "values", label: "mmd.xy.values", placeholder: "1, 2, 3", size: "l" },
  ], make: () => ({ type: "bar", values: "" }) }],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], horizontal: /\bhorizontal\b/.test(head), series: [] };
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = unq(x[1]);
      else if ((x = /^x-axis\s*(?:("[^"]*"|\S+)\s*)?\[(.*)\]$/.exec(l))) {
        if (x[1]) m.xTitle = unq(x[1]);
        m.xLabels = splitArgs(x[2]).map(unq).join(", ");
      } else if ((x = /^y-axis\s*("[^"]*"|[^\d\s"][^\s]*)?\s*(?:([-\d.]+)\s*-->\s*([-\d.]+))?$/.exec(l))) {
        if (x[1]) m.yTitle = unq(x[1]);
        if (x[2] !== undefined) { m.yMin = Number(x[2]); m.yMax = Number(x[3]); }
      } else if ((x = /^(bar|line)\s*(?:("[^"]*")\s*)?\[(.*)\]$/.exec(l))) (m.series as Row[]).push({ type: x[1], name: x[2] ? unq(x[2]) : undefined, values: splitArgs(x[3]).join(", ") });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = [`${s(m.kw) || "xychart"}${m.horizontal ? " horizontal" : ""}`];
    if (m.title) out.push(`${IND}title ${q(m.title, true)}`);
    if (m.xLabels || m.xTitle) out.push(`${IND}x-axis ${m.xTitle ? q(m.xTitle, true) + " " : ""}[${s(m.xLabels).split(",").map((v) => v.trim()).filter(Boolean).map((v) => q(v)).join(", ")}]`);
    if (m.yTitle || m.yMin !== undefined) out.push(`${IND}y-axis${m.yTitle ? " " + q(m.yTitle, true) : ""}${m.yMin !== undefined && m.yMax !== undefined && s(m.yMin) !== "" ? ` ${m.yMin} --> ${m.yMax}` : ""}`);
    for (const r of rows(m, "series")) out.push(`${IND}${s(r.type) || "bar"}${r.name ? " " + q(r.name, true) : ""} [${s(r.values).split(",").map((v) => v.trim()).filter(Boolean).join(", ")}]`);
    return [...out, ...extraLines(m)].join("\n");
  },
};

export const sankey: MermaidKind = {
  id: "sankey", label: "mmd.kind.sankey", detect: /^sankey(-beta)?\b/,
  example: `sankey
Electricity grid,Over generation / exports,104.453
Electricity grid,Heating and cooling - homes,113.726
Electricity grid,Industry,342.165
Solar,Electricity grid,59.901
Wind,Electricity grid,289.366`,
  lists: [{ key: "links", label: "mmd.sankey.links", add: "mmd.add_flow", fields: [
    { key: "source", label: "mmd.f.from", size: "l", suggest: (m) => [...new Set(rows(m, "links").flatMap((r) => [s(r.source), s(r.target)]))] },
    { key: "target", label: "mmd.f.to", size: "l", suggest: (m) => [...new Set(rows(m, "links").flatMap((r) => [s(r.source), s(r.target)]))] },
    { key: "value", label: "mmd.f.value", type: "number", size: "s" },
  ], make: () => ({ source: "", target: "", value: 1 }) }],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], links: [] };
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      if (!l) continue;
      const parts = splitArgs(l);
      if (parts.length === 3 && !Number.isNaN(Number(parts[2]))) (m.links as Row[]).push({ source: unq(parts[0]), target: unq(parts[1]), value: Number(parts[2]) });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const csv = (v: unknown) => (/[,"]/.test(s(v)) ? `"${s(v).replace(/"/g, '""')}"` : s(v));
    return [s(m.kw) || "sankey", ...rows(m, "links").filter((r) => s(r.source) && s(r.target)).map((r) => `${csv(r.source)},${csv(r.target)},${Number(r.value) || 0}`), ...extraLines(m, "")].join("\n");
  },
};

export const radar: MermaidKind = {
  id: "radar", label: "mmd.kind.radar", detect: /^radar(-beta)?\b/,
  example: `radar-beta
    title Grades
    axis m["Math"], s["Science"], e["English"]
    axis h["History"], g["Geography"], a["Art"]
    curve a["Alice"]{85, 90, 80, 70, 75, 90}
    curve b["Bob"]{70, 75, 85, 80, 90, 85}
    max 100
    min 0`,
  fields: [
    { key: "title", label: "mmd.f.title", size: "l" },
    { key: "min", label: "mmd.f.min", type: "number", size: "s" }, { key: "max", label: "mmd.f.max", type: "number", size: "s" },
  ],
  lists: [
    { key: "axes", label: "mmd.radar.axes", add: "mmd.add_axis", fields: [{ key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" }], make: (m) => ({ id: `a${rows(m, "axes").length + 1}`, label: "" }) },
    { key: "curves", label: "mmd.radar.curves", hint: "mmd.radar.curves_hint", add: "mmd.add_curve", fields: [
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label" }, { key: "values", label: "mmd.xy.values", placeholder: "1, 2, 3", size: "l" },
    ], make: (m) => ({ id: `c${rows(m, "curves").length + 1}`, label: "", values: "" }) },
  ],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], axes: [], curves: [] };
    const extra: string[] = [];
    const item = /^([\w-]+)(?:\["([^"]*)"\])?$/;
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = unq(x[1]);
      else if ((x = /^(min|max)\s+([-\d.]+)$/.exec(l))) m[x[1]] = Number(x[2]);
      else if ((x = /^axis\s+(.*)$/.exec(l))) {
        for (const a of splitArgs(x[1])) {
          const y = item.exec(a);
          if (y) (m.axes as Row[]).push({ id: y[1], label: y[2] ?? "" });
        }
      } else if ((x = /^curve\s+([\w-]+)(?:\["([^"]*)"\])?\s*\{(.*)\}$/.exec(l))) (m.curves as Row[]).push({ id: x[1], label: x[2] ?? "", values: splitArgs(x[3]).join(", ") });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    const out = [s(m.kw) || "radar-beta"];
    if (m.title) out.push(`${IND}title ${s(m.title)}`);
    const axes = rows(m, "axes").filter((a) => s(a.id));
    if (axes.length) out.push(`${IND}axis ${axes.map((a) => `${s(a.id)}${a.label ? `["${s(a.label)}"]` : ""}`).join(", ")}`);
    for (const c of rows(m, "curves")) if (s(c.id)) out.push(`${IND}curve ${s(c.id)}${c.label ? `["${s(c.label)}"]` : ""}{${s(c.values).split(",").map((v) => v.trim()).filter(Boolean).join(", ")}}`);
    if (s(m.max) !== "") out.push(`${IND}max ${m.max}`);
    if (s(m.min) !== "") out.push(`${IND}min ${m.min}`);
    return [...out, ...extraLines(m)].join("\n");
  },
};

export const packet: MermaidKind = {
  id: "packet", label: "mmd.kind.packet", detect: /^packet(-beta)?\b/,
  example: `packet
    title UDP Packet
    0-15: "Source Port"
    16-31: "Destination Port"
    32-47: "Length"
    48-63: "Checksum"
    64-95: "Data (variable length)"`,
  fields: [{ key: "title", label: "mmd.f.title", size: "l" }],
  lists: [{ key: "fields", label: "mmd.packet.fields", hint: "mmd.packet.hint", add: "mmd.add_field", fields: [
    { key: "bits", label: "mmd.packet.bits", placeholder: "0-15 / +16", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" },
  ], make: () => ({ bits: "+8", label: "" }) }],
  parse(body, head) {
    const m: Model = { kw: head.split(/\s+/)[0], fields: [] };
    const extra: string[] = [];
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if ((x = /^title\s+(.*)$/.exec(l))) m.title = unq(x[1]);
      else if ((x = /^(\+?\d+(?:-\d+)?)\s*:\s*(.*)$/.exec(l))) (m.fields as Row[]).push({ bits: x[1], label: unq(x[2]) });
      else keep(extra, l);
    }
    m.extra = extra.join("\n");
    return m;
  },
  write(m) {
    return [s(m.kw) || "packet", ...(m.title ? [`${IND}title ${s(m.title)}`] : []),
      ...rows(m, "fields").filter((r) => s(r.bits)).map((r) => `${IND}${s(r.bits)}: "${s(r.label).replace(/"/g, "'")}"`), ...extraLines(m)].join("\n");
  },
};

export { num };
