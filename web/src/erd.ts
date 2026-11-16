/** Database model blocks (```dbmodel): SQL CREATE TABLE statements become an ER diagram. */

export interface Column { name: string; type: string; pk: boolean; notNull: boolean; ref?: { table: string; column: string } }
export interface Table { name: string; columns: Column[] }

/** Name without quotes and schema ("public"."users" → users). */
const ident = (s: string) => s.trim().replace(/^.*\./, "").replace(/^[`"[]|[`"\]]$/g, "");

/** Splits at commas outside parentheses. */
function splitTop(s: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let cur = "";
  for (const ch of s) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === "," && depth === 0) {
      out.push(cur);
      cur = "";
    } else cur += ch;
  }
  if (cur.trim()) out.push(cur);
  return out.map((x) => x.trim()).filter(Boolean);
}

/** Parses CREATE TABLE statements (common SQL of Postgres, MySQL, SQLite)
 * and applies later ALTER TABLE (add, drop, rename columns; rename table)
 * and DROP TABLE in order, so a folder of migrations gives the end state. */
export function parseDDL(sql: string): Table[] {
  const clean = sql.replace(/--[^\n]*/g, "").replace(/\/\*[\s\S]*?\*\//g, "");
  let tables: Table[] = [];
  const find = (n: string) => tables.find((t) => t.name.toLowerCase() === ident(n).toLowerCase());
  const re = /create\s+(?:temporary\s+|temp\s+)?table\s+(?:if\s+not\s+exists\s+)?([`"[\]\w.]+)\s*\(|alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?([`"[\]\w.]+)\s+([^;]*);?|drop\s+table\s+(?:if\s+exists\s+)?([`"[\]\w.,\s]+?)\s*(?:cascade|restrict)?\s*;/gi;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean))) {
    if (m[4]) {
      const drop = m[4].split(",").map((x) => ident(x).toLowerCase());
      tables = tables.filter((t) => !drop.includes(t.name.toLowerCase()));
      continue;
    }
    if (m[2]) {
      const t = find(m[2]);
      if (!t) continue;
      for (const action of splitTop(m[3])) {
        const ren = /^rename\s+to\s+([`"[\]\w.]+)/i.exec(action);
        if (ren) { t.name = ident(ren[1]); continue; }
        const rc = /^rename\s+(?:column\s+)?([`"[\]\w]+)\s+to\s+([`"[\]\w]+)/i.exec(action);
        if (rc) { const c = t.columns.find((x) => x.name === ident(rc[1])); if (c) c.name = ident(rc[2]); continue; }
        const dc = /^drop\s+(?:column\s+)?(?:if\s+exists\s+)?([`"[\]\w]+)/i.exec(action);
        if (dc && !/^drop\s+(constraint|default|not\s+null)/i.test(action)) { t.columns = t.columns.filter((x) => x.name !== ident(dc[1])); continue; }
        const ac = /^add\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?(.+)$/is.exec(action);
        if (ac && !/^add\s+(constraint|primary|foreign|unique|check|index)/i.test(action)) {
          const c = columnOf(ac[1]);
          if (c && !t.columns.some((x) => x.name === c.name)) t.columns.push(c);
        }
      }
      continue;
    }
    // Find the matching closing parenthesis.
    let depth = 1;
    let i = re.lastIndex;
    for (; i < clean.length && depth > 0; i++) {
      if (clean[i] === "(") depth++;
      else if (clean[i] === ")") depth--;
    }
    const body = clean.slice(re.lastIndex, i - 1);
    const t: Table = { name: ident(m[1]), columns: [] };
    const pks: string[] = [];
    const fks: [string, string, string][] = [];
    for (const part of splitTop(body)) {
      const pk = /^(?:constraint\s+\S+\s+)?primary\s+key\s*\(([^)]*)\)/i.exec(part);
      if (pk) {
        pks.push(...pk[1].split(",").map(ident));
        continue;
      }
      const fk = /^(?:constraint\s+\S+\s+)?foreign\s+key\s*\(([^)]*)\)\s*references\s+([`"[\]\w.]+)\s*(?:\(([^)]*)\))?/i.exec(part);
      if (fk) {
        fks.push([ident(fk[1]), ident(fk[2]), fk[3] ? ident(fk[3]) : "id"]);
        continue;
      }
      if (/^(constraint|unique|check|index|key)\b/i.test(part)) continue;
      const c = columnOf(part);
      if (c) t.columns.push(c);
    }
    for (const c of t.columns) if (pks.includes(c.name)) c.pk = true;
    for (const [col, table, column] of fks) {
      const c = t.columns.find((x) => x.name === col);
      if (c) c.ref = { table, column };
    }
    const old = find(t.name);
    if (old) tables = tables.filter((x) => x !== old);
    tables.push(t);
    re.lastIndex = i;
  }
  return tables;
}

function columnOf(part: string): Column | undefined {
  const col = /^([`"[\]\w]+)\s+(\w+(?:\s*\([^)]*\))?)/.exec(part.trim());
  if (!col) return undefined;
  const c: Column = { name: ident(col[1]), type: col[2].replace(/\s+/g, ""), pk: /\bprimary\s+key\b/i.test(part), notNull: /\bnot\s+null\b/i.test(part) };
  const ref = /\breferences\s+([`"[\]\w.]+)\s*(?:\(([^)]*)\))?/i.exec(part);
  if (ref) c.ref = { table: ident(ref[1]), column: ref[2] ? ident(ref[2]) : "id" };
  return c;
}

/** "from owner/repo path" as the first line of a dbmodel block. */
export function repoSource(src: string): { repo: string; path: string } | undefined {
  const m = /^\s*from\s+([\w.-]+\/[\w.-]+)\s+(\S+)\s*$/im.exec(src.split("\n").find((l) => l.trim()) ?? "");
  return m ? { repo: m[1], path: m[2] } : undefined;
}

/** Tables a block takes from the project schema ("-- tables: users, sessions"). */
export function shownTables(sql: string): string[] {
  const out: string[] = [];
  for (const m of sql.matchAll(/^\s*--\s*tables?\s*:\s*(.+)$/gim)) out.push(...m[1].split(",").map(ident).filter(Boolean));
  return out;
}

/** The tables a block draws: its own, the ones it takes from the project
 * schema, and – reduced to their keys – tables they reference that are
 * defined elsewhere in the project. */
export function withSchema(own: Table[], shown: string[], schema: Table[]): { tables: Table[]; refs: Table[]; missing: string[] } {
  const byName = new Map<string, Table>();
  for (const t of schema) {
    const k = t.name.toLowerCase();
    if ((byName.get(k)?.columns.length ?? -1) < t.columns.length) byName.set(k, t);
  }
  const tables = [...own];
  const has = (n: string) => tables.some((t) => t.name.toLowerCase() === n.toLowerCase());
  const missing: string[] = [];
  for (const n of shown) {
    if (has(n)) continue;
    const t = byName.get(n.toLowerCase());
    if (t) tables.push(t);
    else missing.push(n);
  }
  const refs: Table[] = [];
  for (const t of tables) {
    for (const c of t.columns) {
      if (!c.ref || has(c.ref.table) || refs.some((r) => r.name.toLowerCase() === c.ref!.table.toLowerCase())) continue;
      const other = byName.get(c.ref.table.toLowerCase());
      if (other) refs.push({ name: other.name, columns: other.columns.filter((x) => x.pk || x.name === c.ref!.column) });
    }
  }
  return { tables, refs, missing };
}

const safe = (s: string) => s.replace(/[^\w]/g, "_");

/** Mermaid erDiagram source for tables. */
export function toMermaid(tables: Table[]): string {
  const lines = ["erDiagram"];
  for (const t of tables) {
    lines.push(`  ${safe(t.name)} {`);
    for (const c of t.columns) {
      const keys = [c.pk ? "PK" : "", c.ref ? "FK" : ""].filter(Boolean).join(",");
      lines.push(`    ${safe(c.type.replace(/\(.*\)/, "")) || "text"} ${safe(c.name)}${keys ? " " + keys : ""}`);
    }
    lines.push("  }");
  }
  const names = new Set(tables.map((t) => t.name));
  for (const t of tables) {
    for (const c of t.columns) {
      if (!c.ref || !names.has(c.ref.table)) continue;
      lines.push(`  ${safe(c.ref.table)} ||--o{ ${safe(t.name)} : "${safe(c.name)}"`);
    }
  }
  return lines.join("\n");
}
