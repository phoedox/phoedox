// The pure part of ```compare blocks: split the two versions and pair their lines.

import { diffLines } from "../diff";

/** Splits a ```compare block: "--- title" starts the old, "+++ title" the new version. */
export function parseCompare(src: string): { left: string[]; right: string[]; leftTitle: string; rightTitle: string } | undefined {
  const lines = src.replace(/\r\n/g, "\n").split("\n");
  const plus = lines.findIndex((l) => l.startsWith("+++"));
  if (plus < 0) return undefined;
  let left = lines.slice(0, plus);
  let leftTitle = "";
  if (left[0]?.startsWith("---")) {
    leftTitle = left[0].slice(3).trim();
    left = left.slice(1);
  }
  const trim = (a: string[]) => {
    while (a.length && a[a.length - 1].trim() === "") a.pop();
    return a;
  };
  return { left: trim(left), right: trim(lines.slice(plus + 1)), leftTitle, rightTitle: lines[plus].slice(3).trim() };
}

/** Rows of a side-by-side comparison: removed and added lines next to each other. */
export function compareRows(left: string[], right: string[]): { l?: string; r?: string; ln?: number; rn?: number; op: "same" | "change" | "del" | "add" }[] {
  const d = diffLines(left, right);
  const rows: { l?: string; r?: string; ln?: number; rn?: number; op: "same" | "change" | "del" | "add" }[] = [];
  let ln = 0, rn = 0;
  for (let i = 0; i < d.length; i++) {
    if (d[i].op === " ") {
      rows.push({ l: d[i].text, r: d[i].text, ln: ++ln, rn: ++rn, op: "same" });
      continue;
    }
    const dels: string[] = [], adds: string[] = [];
    while (i < d.length && d[i].op !== " ") {
      (d[i].op === "-" ? dels : adds).push(d[i].text);
      i++;
    }
    i--;
    for (let k = 0; k < Math.max(dels.length, adds.length); k++) {
      const l = dels[k], r = adds[k];
      rows.push({ l, r, ln: l !== undefined ? ++ln : undefined, rn: r !== undefined ? ++rn : undefined, op: l !== undefined && r !== undefined ? "change" : l !== undefined ? "del" : "add" });
    }
  }
  return rows;
}
