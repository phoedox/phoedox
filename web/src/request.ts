// Display parser for request blocks (the server parses again when running;
// see internal/requests/parse.go).

export interface RequestBlock {
  method: string;
  url: string;
  headers: [string, string][];
  body: string;
  expects: string[];
}

const methodRe = /^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS)\s+(\S+)$/;
const headerRe = /^([A-Za-z0-9-]+):\s*(.*)$/;

export function parseRequest(src: string): RequestBlock | null {
  const lines = src.replace(/\r\n/g, "\n").split("\n");
  let i = 0;
  while (i < lines.length && (!lines[i].trim() || lines[i].trim().startsWith("#"))) i++;
  const m = methodRe.exec((lines[i] ?? "").trim());
  if (!m) return null;
  const r: RequestBlock = { method: m[1], url: m[2], headers: [], body: "", expects: [] };
  for (i++; i < lines.length; i++) {
    const l = lines[i].trim();
    if (!l || l.startsWith("expect ")) break;
    const h = headerRe.exec(l);
    if (!h) return null;
    r.headers.push([h[1], h[2]]);
  }
  const body: string[] = [];
  for (; i < lines.length; i++) {
    const l = lines[i].trim();
    if (l.startsWith("expect ")) r.expects.push(l.slice(7).trim());
    else if (!r.expects.length) body.push(lines[i]);
  }
  r.body = body.join("\n").trim();
  return r;
}

export const isSafeMethod = (m: string) => m === "GET" || m === "HEAD" || m === "OPTIONS";

/** Splits text into literal parts and {{variable}} names. */
export function splitVars(s: string): { text: string; variable?: boolean }[] {
  const out: { text: string; variable?: boolean }[] = [];
  let last = 0;
  for (const m of s.matchAll(/\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g)) {
    if (m.index! > last) out.push({ text: s.slice(last, m.index) });
    out.push({ text: m[1], variable: true });
    last = m.index! + m[0].length;
  }
  if (last < s.length) out.push({ text: s.slice(last) });
  return out;
}
