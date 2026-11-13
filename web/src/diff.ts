// Line diff (Myers, O((N+M)·D)). Large or very different texts fall back
// to "everything replaced" to keep the browser responsive.

export type DiffLine = { op: " " | "+" | "-"; text: string };

const MAX_LINES = 20000;
const MAX_EDITS = 3000;

export function diffLines(a: string[], b: string[]): DiffLine[] {
  const n = a.length;
  const m = b.length;
  const replaced = (): DiffLine[] => [...a.map((text) => ({ op: "-" as const, text })), ...b.map((text) => ({ op: "+" as const, text }))];
  if (n + m > MAX_LINES) return replaced();
  const max = n + m;
  const off = max + 1;
  const v = new Int32Array(2 * max + 3);
  const trace: Int32Array[] = [];
  let done = false;
  for (let d = 0; d <= max && !done; d++) {
    if (d > MAX_EDITS) return replaced();
    trace.push(v.slice());
    for (let k = -d; k <= d; k += 2) {
      let x = k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]) ? v[off + k + 1] : v[off + k - 1] + 1;
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) {
        x++;
        y++;
      }
      v[off + k] = x;
      if (x >= n && y >= m) {
        done = true;
        break;
      }
    }
  }
  const out: DiffLine[] = [];
  let x = n;
  let y = m;
  for (let d = trace.length - 1; d >= 0; d--) {
    const vv = trace[d];
    const k = x - y;
    const prevK = k === -d || (k !== d && vv[off + k - 1] < vv[off + k + 1]) ? k + 1 : k - 1;
    const prevX = vv[off + prevK];
    const prevY = prevX - prevK;
    while (x > prevX && y > prevY) {
      out.push({ op: " ", text: a[x - 1] });
      x--;
      y--;
    }
    if (d > 0) {
      if (x === prevX) out.push({ op: "+", text: b[y - 1] });
      else out.push({ op: "-", text: a[x - 1] });
    }
    x = prevX;
    y = prevY;
  }
  return out.reverse();
}

export type Hunk = DiffLine | { op: "…"; count: number };

/** Keeps `context` unchanged lines around changes and folds the rest. */
export function fold(lines: DiffLine[], context = 3): Hunk[] {
  const keep = lines.map(() => false);
  lines.forEach((l, i) => {
    if (l.op === " ") return;
    for (let j = Math.max(0, i - context); j <= Math.min(lines.length - 1, i + context); j++) keep[j] = true;
  });
  const out: Hunk[] = [];
  let skipped = 0;
  lines.forEach((l, i) => {
    if (keep[i]) {
      if (skipped) out.push({ op: "…", count: skipped });
      skipped = 0;
      out.push(l);
    } else skipped++;
  });
  if (skipped) out.push({ op: "…", count: skipped });
  return out;
}
