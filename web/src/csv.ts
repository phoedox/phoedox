// CSV for the table view: RFC 4180 quoting, comma or semicolon (Excel in
// German locales writes semicolons), BOM and CRLF tolerant.

/** Guesses the delimiter from the header line. */
export function sniffDelimiter(text: string): "," | ";" | "\t" {
  const head = text.replace(/^﻿/, "").split(/\r?\n/, 1)[0] ?? "";
  const count = (c: string) => head.split(c).length - 1;
  const tab = count("\t"), semi = count(";"), comma = count(",");
  if (tab > semi && tab > comma) return "\t";
  return semi > comma ? ";" : ",";
}

/** Parses CSV into rows of cells; empty trailing lines are dropped. */
export function parseCSV(text: string, delimiter = sniffDelimiter(text)): string[][] {
  const s = text.replace(/^﻿/, "");
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (quoted) {
      if (c === '"' && s[i + 1] === '"') { cell += '"'; i++; }
      else if (c === '"') quoted = false;
      else cell += c;
      continue;
    }
    if (c === '"' && cell === "") quoted = true;
    else if (c === delimiter) { row.push(cell); cell = ""; }
    else if (c === "\n" || c === "\r") {
      if (c === "\r" && s[i + 1] === "\n") i++;
      row.push(cell); rows.push(row); row = []; cell = "";
    } else cell += c;
  }
  if (cell !== "" || row.length) { row.push(cell); rows.push(row); }
  return rows.filter((r) => r.some((c) => c.trim() !== ""));
}

const needsQuotes = (v: string, d: string) => v.includes(d) || /["\r\n]/.test(v) || /^\s|\s$/.test(v);

/** Writes rows as CSV; cells that start like a formula are prefixed so
 * spreadsheet programs do not run them (CSV injection). */
export function toCSV(rows: unknown[][], delimiter = ","): string {
  return rows.map((r) => r.map((c) => {
    let v = c === undefined || c === null ? "" : typeof c === "object" ? JSON.stringify(c) : String(c);
    if (/^[=+\-@\t\r]/.test(v) && !/^-?\d+([.,]\d+)?$/.test(v)) v = "'" + v;
    return needsQuotes(v, delimiter) ? `"${v.replace(/"/g, '""')}"` : v;
  }).join(delimiter)).join("\r\n") + "\r\n";
}
