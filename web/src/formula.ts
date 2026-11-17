// Formulas of calculator blocks (and anything else that computes): numbers,
// text in quotes, names of other values, + - * / ^ &, comparisons and a few
// functions – without eval. Names are letters, digits and _ (any script).

export class FormulaError extends Error {
  constructor(readonly code: string) {
    super(code);
  }
}

const FUNCS: Record<string, (a: unknown[]) => unknown> = {
  SUM: (a) => nums(a).reduce((x, y) => x + y, 0),
  AVERAGE: (a) => { const n = nums(a); return n.length ? n.reduce((x, y) => x + y, 0) / n.length : 0; },
  MIN: (a) => Math.min(...nums(a)),
  MAX: (a) => Math.max(...nums(a)),
  ROUND: ([x, d]) => { const f = 10 ** (Number(d) || 0); return Math.round(num(x) * f) / f; },
  CEIL: ([x]) => Math.ceil(num(x)),
  FLOOR: ([x]) => Math.floor(num(x)),
  ABS: ([x]) => Math.abs(num(x)),
  SQRT: ([x]) => Math.sqrt(num(x)),
  IF: ([c, a, b]) => (truthy(c) ? a : b ?? ""),
};
FUNCS.AVG = FUNCS.AVERAGE;
export const FORMULA_FUNCTIONS = Object.keys(FUNCS);

const num = (v: unknown): number => {
  if (typeof v === "number") return v;
  if (v === "" || v == null) return 0;
  const n = Number(v);
  if (Number.isNaN(n)) throw new FormulaError("value");
  return n;
};
const nums = (a: unknown[]) => a.flat(Infinity as 1).filter((v) => v !== "" && v != null && !Number.isNaN(Number(v))).map(Number);
const truthy = (v: unknown) => (typeof v === "string" ? v !== "" && v.toUpperCase() !== "FALSE" : !!v);

type Tok = { t: "num" | "str" | "name" | "fn" | "op"; v: string | number };

export function formulaTokens(src: string): Tok[] {
  const out: Tok[] = [];
  let i = 0;
  while (i < src.length) {
    const rest = src.slice(i);
    let m: RegExpExecArray | null;
    if ((m = /^\s+/.exec(rest))) { i += m[0].length; continue; }
    if ((m = /^\d+(?:[.,]\d+)?/.exec(rest))) out.push({ t: "num", v: Number(m[0].replace(",", ".")) });
    else if ((m = /^"([^"]*)"/.exec(rest))) out.push({ t: "str", v: m[1] });
    else if ((m = /^[\p{L}_][\p{L}\p{N}_]*(?=\s*\()/u.exec(rest)) && FUNCS[m[0].toUpperCase()]) out.push({ t: "fn", v: m[0].toUpperCase() });
    else if ((m = /^[\p{L}_][\p{L}\p{N}_]*/u.exec(rest))) out.push({ t: "name", v: m[0] });
    else if ((m = /^(<=|>=|<>|[-+*/^&(),;=<>%])/.exec(rest))) out.push({ t: "op", v: m[0] === ";" ? "," : m[0] });
    else throw new FormulaError("syntax");
    i += m[0].length;
  }
  return out;
}

/** The names a formula uses. */
export function formulaNames(src: string): string[] {
  return [...new Set(formulaTokens(src).filter((t) => t.t === "name").map((t) => String(t.v)))];
}

/** Evaluates a formula; get(name) gives another value (throws FormulaError("name") if unknown). */
export function evaluate(src: string, get: (name: string) => unknown): unknown {
  const ts = formulaTokens(src);
  let p = 0;
  const peek = () => ts[p];
  const take = () => ts[p++];
  const expect = (v: string) => { const t = take(); if (!t || t.v !== v) throw new FormulaError("syntax"); };
  function primary(): unknown {
    const t = take();
    if (!t) throw new FormulaError("syntax");
    if (t.t === "num" || t.t === "str") return t.v;
    if (t.t === "name") return get(String(t.v));
    if (t.t === "fn") {
      expect("(");
      const args: unknown[] = [];
      if (peek()?.v !== ")") { do args.push(compare()); while (peek()?.v === "," && take()); }
      expect(")");
      return FUNCS[String(t.v)](args);
    }
    if (t.v === "(") { const v = compare(); expect(")"); return v; }
    if (t.v === "-") return -num(power());
    if (t.v === "+") return num(power());
    throw new FormulaError("syntax");
  }
  function percent(): unknown {
    let v = primary();
    while (peek()?.v === "%") { take(); v = num(v) / 100; }
    return v;
  }
  function power(): unknown { let v = percent(); while (peek()?.v === "^") { take(); v = num(v) ** num(percent()); } return v; }
  function term(): unknown {
    let v = power();
    while (peek()?.v === "*" || peek()?.v === "/") {
      const o = take().v;
      const r = num(power());
      if (o === "/" && r === 0) throw new FormulaError("div0");
      v = o === "*" ? num(v) * r : num(v) / r;
    }
    return v;
  }
  function sum(): unknown {
    let v = term();
    while (peek()?.v === "+" || peek()?.v === "-" || peek()?.v === "&") {
      const o = take().v;
      const r = term();
      v = o === "&" ? `${v}${r}` : o === "+" ? num(v) + num(r) : num(v) - num(r);
    }
    return v;
  }
  function compare(): unknown {
    let v = sum();
    while (["=", "<>", "<", ">", "<=", ">="].includes(String(peek()?.v))) {
      const o = take().v;
      const r = sum();
      const [x, y] = typeof v === "number" || typeof r === "number" ? [num(v), num(r)] : [String(v), String(r)];
      v = { "=": x === y, "<>": x !== y, "<": x < y, ">": x > y, "<=": x <= y, ">=": x >= y }[o as string];
    }
    return v;
  }
  const v = compare();
  if (p < ts.length) throw new FormulaError("syntax");
  return v;
}
