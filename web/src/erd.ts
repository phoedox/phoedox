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

/** Parses CREATE TABLE statements (common SQL of Postgres, MySQL, SQLite). */
export function parseDDL(sql: string): Table[] {
  const clean = sql.replace(/--[^\n]*/g, "").replace(/\/\*[\s\S]*?\*\//g, "");
  const tables: Table[] = [];
  const re = /create\s+(?:temporary\s+|temp\s+)?table\s+(?:if\s+not\s+exists\s+)?([`"[\]\w.]+)\s*\(/gi;
  let m: RegExpExecArray | null;
  while ((m = re.exec(clean))) {
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
      const low = part.toLowerCase();
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
      if (/^(constraint|unique|check|index|key)\b/i.test(low)) continue;
      const col = /^([`"[\]\w]+)\s+(\w+(?:\s*\([^)]*\))?)/.exec(part);
      if (!col) continue;
      const c: Column = { name: ident(col[1]), type: col[2].replace(/\s+/g, ""), pk: /\bprimary\s+key\b/i.test(part), notNull: /\bnot\s+null\b/i.test(part) };
      const ref = /\breferences\s+([`"[\]\w.]+)\s*(?:\(([^)]*)\))?/i.exec(part);
      if (ref) c.ref = { table: ident(ref[1]), column: ref[2] ? ident(ref[2]) : "id" };
      t.columns.push(c);
    }
    for (const c of t.columns) if (pks.includes(c.name)) c.pk = true;
    for (const [col, table, column] of fks) {
      const c = t.columns.find((x) => x.name === col);
      if (c) c.ref = { table, column };
    }
    tables.push(t);
    re.lastIndex = i;
  }
  return tables;
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
