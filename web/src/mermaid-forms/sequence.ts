// Sequence diagrams: participants and an ordered list of steps (messages,
// notes, and every other statement – loops, alternatives, activations – as
// a step of its own, so the order is kept).
import { IND, rows, s, type MermaidKind, type Model, type Row } from "./core";

const ARROWS: [string, string][] = [
  ["->>", "mmd.seq.sync"], ["-->>", "mmd.seq.reply"], ["->", "mmd.seq.line"], ["-->", "mmd.seq.line_dotted"],
  ["-x", "mmd.seq.lost"], ["--x", "mmd.seq.lost_dotted"], ["-)", "mmd.seq.async"], ["--)", "mmd.seq.async_dotted"], ["<<->>", "mmd.seq.both"], ["<<-->>", "mmd.seq.both_dotted"],
];
const ARROW_RE = new RegExp(`^(.+?)\\s*(${[...ARROWS].map(([a]) => a).sort((a, b) => b.length - a.length).map((a) => a.replace(/[)(|]/g, "\\$&")).join("|")})([+-]?)\\s*(.+?)\\s*:\\s*(.*)$`);
const partIds = (m: Model) => rows(m, "participants").map((p) => s(p.id)).filter(Boolean);

export const sequence: MermaidKind = {
  id: "sequence", label: "mmd.kind.sequence", detect: /^sequenceDiagram\b/,
  example: `sequenceDiagram
    actor Me
    participant Laptop
    participant NAS
    Me->>Laptop: Save photo
    Laptop->>NAS: Upload
    NAS-->>Laptop: Stored
    Note over Laptop,NAS: Nightly backup copies it again`,
  fields: [{ key: "autonumber", label: "mmd.seq.autonumber", type: "check" }],
  lists: [
    { key: "participants", label: "mmd.seq.participants", add: "mmd.add_participant", fields: [
      { key: "kind", label: "mmd.f.kind", type: "select", options: [["participant", "mmd.seq.participant"], ["actor", "mmd.seq.actor"]], size: "s" },
      { key: "id", label: "mmd.f.id", size: "s" }, { key: "label", label: "mmd.f.label", size: "l" },
    ], make: (m) => ({ kind: "participant", id: `P${rows(m, "participants").length + 1}`, label: "" }) },
    { key: "steps", label: "mmd.seq.steps", hint: "mmd.seq.hint", add: "mmd.add_step", fields: [
      { key: "kind", label: "mmd.f.kind", type: "select", options: [["msg", "mmd.seq.message"], ["note", "mmd.seq.note"], ["raw", "mmd.seq.raw"]], size: "s" },
      { key: "from", label: "mmd.f.from", size: "s", suggest: partIds, show: (r) => r.kind !== "raw" },
      { key: "arrow", label: "mmd.seq.arrow", type: "select", options: ARROWS, size: "s", show: (r) => r.kind === "msg" },
      { key: "act", label: "mmd.seq.activation", type: "select", options: [["", "–"], ["+", "mmd.seq.activate"], ["-", "mmd.seq.deactivate"]], size: "s", show: (r) => r.kind === "msg" },
      { key: "pos", label: "mmd.seq.position", type: "select", options: [["over", "mmd.seq.over"], ["right of", "mmd.seq.right_of"], ["left of", "mmd.seq.left_of"]], size: "s", show: (r) => r.kind === "note" },
      { key: "to", label: "mmd.f.to", size: "s", suggest: partIds, show: (r) => r.kind !== "raw" },
      { key: "text", label: "mmd.f.text", size: "l" },
    ], make: (m) => ({ kind: "msg", from: partIds(m)[0] ?? "", arrow: "->>", to: partIds(m)[1] ?? "", text: "" }) },
  ],
  parse(body) {
    const m: Model = { participants: [], steps: [], autonumber: false };
    const parts = new Map<string, Row>();
    const see = (id: string) => {
      const k = id.trim();
      if (!parts.has(k)) { const r: Row = { kind: "participant", id: k, label: "" }; parts.set(k, r); (m.participants as Row[]).push(r); }
    };
    for (const raw of body) {
      const l = raw.trim();
      let x: RegExpExecArray | null;
      if (!l) continue;
      if (l === "autonumber") m.autonumber = true;
      else if ((x = /^(participant|actor)\s+(.+?)(?:\s+as\s+(.+))?$/.exec(l)) && !/@\{/.test(l)) {
        const id = x[2].trim();
        const r: Row = parts.get(id) ?? { id };
        Object.assign(r, { kind: x[1], label: x[3]?.trim() ?? "" });
        if (!parts.has(id)) { parts.set(id, r); (m.participants as Row[]).push(r); }
      } else if ((x = ARROW_RE.exec(l))) {
        see(x[1]); see(x[4]);
        (m.steps as Row[]).push({ kind: "msg", from: x[1].trim(), arrow: x[2], act: x[3], to: x[4].trim(), text: x[5] });
      } else if ((x = /^note\s+(over|right of|left of)\s+([^:]+?)\s*:\s*(.*)$/i.exec(l))) {
        const ps = x[2].split(",").map((p) => p.trim());
        (m.steps as Row[]).push({ kind: "note", pos: x[1].toLowerCase(), from: ps[0], to: ps[1] ?? "", text: x[3] });
      } else (m.steps as Row[]).push({ kind: "raw", text: l });
    }
    return m;
  },
  write(m) {
    const out = ["sequenceDiagram"];
    if (m.autonumber) out.push(`${IND}autonumber`);
    for (const p of rows(m, "participants")) if (s(p.id)) out.push(`${IND}${s(p.kind) || "participant"} ${s(p.id)}${s(p.label) ? " as " + s(p.label) : ""}`);
    let depth = 1;
    for (const st of rows(m, "steps")) {
      const text = s(st.text).replace(/\n/g, " ");
      if (st.kind === "raw") {
        if (/^(end|else|and|option)\b/.test(text)) depth = Math.max(1, depth - 1);
        if (text) out.push(IND.repeat(depth) + text);
        if (/^(loop|alt|opt|par|critical|break|rect|box|else|and|option)\b/.test(text)) depth++;
        continue;
      }
      if (!s(st.from)) continue;
      if (st.kind === "note") out.push(`${IND.repeat(depth)}Note ${s(st.pos) || "over"} ${s(st.from)}${s(st.to) ? "," + s(st.to) : ""}: ${text}`);
      else if (s(st.to)) out.push(`${IND.repeat(depth)}${s(st.from)}${s(st.arrow) || "->>"}${s(st.act)}${s(st.to)}: ${text}`);
    }
    return out.join("\n");
  },
};
