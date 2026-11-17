// Formulas of the sheet, without eval: numbers, text in quotes, cell refs
// (A1), ranges (A1:B3), + - * / ^ &, comparisons and functions.
export const colName = (i) => String.fromCharCode(65 + i);
export const parseRef = (r) => { const m = /^([A-Z])([1-9]\d*)$/.exec(r); return m ? [m[1].charCodeAt(0) - 65, Number(m[2]) - 1] : null; };

const FUNCS = {
  SUM: (a) => nums(a).reduce((x, y) => x + y, 0),
  AVERAGE: (a) => { const n = nums(a); return n.length ? n.reduce((x, y) => x + y, 0) / n.length : 0; },
  MIN: (a) => Math.min(...nums(a)),
  MAX: (a) => Math.max(...nums(a)),
  COUNT: (a) => nums(a).length,
  COUNTA: (a) => a.flat().filter((v) => v !== "" && v != null).length,
  ROUND: ([x, d]) => { const f = 10 ** (Number(d) || 0); return Math.round(num(x) * f) / f; },
  ABS: ([x]) => Math.abs(num(x)),
  IF: ([c, a, b]) => (truthy(c) ? a : b ?? ""),
  CONCAT: (a) => a.flat().join(""),
  UPPER: ([s]) => String(s ?? "").toUpperCase(),
  LOWER: ([s]) => String(s ?? "").toLowerCase(),
  LEN: ([s]) => String(s ?? "").length,
  TODAY: () => new Date().toISOString().slice(0, 10),
};
FUNCS.AVG = FUNCS.AVERAGE;
const num = (v) => (Array.isArray(v) ? num(v[0]) : typeof v === "number" ? v : v === "" || v == null ? 0 : isNaN(Number(v)) ? err("#VALUE!") : Number(v));
const nums = (a) => a.flat(Infinity).filter((v) => v !== "" && v != null && !isNaN(Number(v))).map(Number);
const truthy = (v) => (typeof v === "string" ? v !== "" && v.toUpperCase() !== "FALSE" : !!v);
function err(code) { throw Object.assign(new Error(code), { cell: code }); }

function tokens(src) {
  const out = []; let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (/\s/.test(c)) { i++; continue; }
    let m;
    const rest = src.slice(i);
    if ((m = /^\d+(\.\d+)?/.exec(rest))) out.push({ t: "num", v: Number(m[0]) });
    else if ((m = /^"([^"]*)"/.exec(rest))) out.push({ t: "str", v: m[1] });
    else if ((m = /^[A-Za-z][1-9]\d*:[A-Za-z][1-9]\d*/.exec(rest))) out.push({ t: "range", v: m[0].toUpperCase() });
    else if ((m = /^[A-Za-z]+(?=\s*\()/.exec(rest))) out.push({ t: "fn", v: m[0].toUpperCase() });
    else if ((m = /^[A-Za-z][1-9]\d*/.exec(rest))) out.push({ t: "ref", v: m[0].toUpperCase() });
    else if ((m = /^(<=|>=|<>|[-+*/^&(),=<>])/.exec(rest))) out.push({ t: "op", v: m[0] });
    else err("#ERROR!");
    i += m[0].length;
  }
  return out;
}

/** Evaluates a formula (without the leading "="); get(ref) gives a cell's value. */
export function evaluate(src, get) {
  const ts = tokens(src); let p = 0;
  const peek = () => ts[p]; const take = () => ts[p++];
  const expect = (v) => { const t = take(); if (!t || t.v !== v) err("#ERROR!"); };
  const range = (r) => {
    const [a, b] = r.split(":").map(parseRef);
    if (!a || !b) err("#REF!");
    const out = [];
    for (let y = Math.min(a[1], b[1]); y <= Math.max(a[1], b[1]); y++) for (let x = Math.min(a[0], b[0]); x <= Math.max(a[0], b[0]); x++) out.push(get(colName(x) + (y + 1)));
    return out;
  };
  function primary() {
    const t = take();
    if (!t) err("#ERROR!");
    if (t.t === "num" || t.t === "str") return t.v;
    if (t.t === "ref") return get(t.v);
    if (t.t === "range") return range(t.v);
    if (t.t === "fn") {
      const f = FUNCS[t.v]; if (!f) err("#NAME?");
      expect("("); const args = [];
      if (peek()?.v !== ")") { do args.push(compare()); while (peek()?.v === "," && take()); }
      expect(")");
      return f(args);
    }
    if (t.v === "(") { const v = compare(); expect(")"); return v; }
    if (t.v === "-") return -num(power());
    if (t.v === "+") return num(power());
    err("#ERROR!");
  }
  function power() { let v = primary(); while (peek()?.v === "^") { take(); v = num(v) ** num(primary()); } return v; }
  function term() { let v = power(); while (["*", "/"].includes(peek()?.v)) { const o = take().v; const r = num(power()); if (o === "/" && r === 0) err("#DIV/0!"); v = o === "*" ? num(v) * r : num(v) / r; } return v; }
  function sum() { let v = term(); while (["+", "-", "&"].includes(peek()?.v)) { const o = take().v; const r = term(); v = o === "&" ? `${v}${r}` : o === "+" ? num(v) + num(r) : num(v) - num(r); } return v; }
  function compare() {
    let v = sum();
    while (["=", "<>", "<", ">", "<=", ">="].includes(peek()?.v)) {
      const o = take().v; const r = sum();
      const [x, y] = typeof v === "number" || typeof r === "number" ? [num(v), num(r)] : [String(v), String(r)];
      v = { "=": x === y, "<>": x !== y, "<": x < y, ">": x > y, "<=": x <= y, ">=": x >= y }[o];
    }
    return v;
  }
  const v = compare();
  if (p < ts.length) err("#ERROR!");
  return Array.isArray(v) ? v[0] ?? "" : v;
}
