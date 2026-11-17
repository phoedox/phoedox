// Calculators: inputs and formulas inside a page ("Label (unit) = value or
// formula"). Readers change the inputs and see the results at once; nothing
// is saved. An input can take an entry's attribute: "= @nas.power_w".
import { FormulaError, evaluate, formulaNames, formulaTokens } from "../formula";
import { locale, t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, keyValue } from "./util";

export interface CalcRow { label: string; name: string; unit: string; expr: string; input: boolean; value?: number; entity?: { slug: string; attr: string } }
export interface Calc { title: string; rows: CalcRow[] }

/** "Kosten pro Jahr (€)" → name "Kosten_pro_Jahr", unit "€". */
export function calcName(label: string): { name: string; unit: string } {
  const m = /^(.*?)\s*\(([^)]*)\)\s*$/.exec(label);
  const base = (m ? m[1] : label).trim();
  return { name: base.replace(/[^\p{L}\p{N}_]+/gu, "_").replace(/^_+|_+$/g, ""), unit: m ? m[2].trim() : "" };
}

const NUM = /^-?\d[\d ]*(?:[.,]\d+)?$/;

export function parseCalc(src: string): { model?: Calc; problems: BlockProblem[] } {
  const c: Calc = { title: "", rows: [] };
  const problems: BlockProblem[] = [];
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["title", "titel"]);
    if (kv) { c.title = kv[1]; continue; }
    const i = l.indexOf("=");
    if (i <= 0) { problems.push({ key: "calc.err_line", params: { line: l } }); continue; }
    const label = l.slice(0, i).trim(), expr = l.slice(i + 1).trim();
    const { name, unit } = calcName(label);
    if (!name || !expr) { problems.push({ key: "calc.err_line", params: { line: l } }); continue; }
    if (c.rows.some((r) => r.name === name)) problems.push({ key: "calc.err_duplicate", params: { name } });
    const ent = /^@([a-z0-9-]+)\.([A-Za-z0-9_]+)$/.exec(expr);
    if (NUM.test(expr)) c.rows.push({ label, name, unit, expr, input: true, value: Number(expr.replace(/ /g, "").replace(",", ".")) });
    else if (ent) c.rows.push({ label, name, unit, expr, input: true, entity: { slug: ent[1], attr: ent[2] } });
    else {
      try { formulaTokens(expr); } catch { problems.push({ key: "calc.err_formula", params: { name } }); }
      c.rows.push({ label, name, unit, expr, input: false });
    }
  }
  const names = new Set(c.rows.map((r) => r.name));
  for (const r of c.rows) {
    if (r.input) continue;
    try {
      for (const n of formulaNames(r.expr)) if (!names.has(n)) problems.push({ key: "calc.err_unknown", params: { name: n, row: r.name } });
    } catch { /* reported above */ }
  }
  if (!c.rows.length) problems.push({ key: "calc.err_empty" });
  if (!problems.length) {
    const res = calcValues(c, {});
    for (const r of c.rows) if (res[r.name] instanceof FormulaError && (res[r.name] as FormulaError).code === "cycle") { problems.push({ key: "calc.err_cycle", params: { name: r.name } }); break; }
  }
  return problems.length ? { problems } : { model: c, problems };
}

/** All values: inputs (overridden by the reader) and formulas; errors as FormulaError. */
export function calcValues(c: Calc, inputs: Record<string, number | undefined>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const rows = new Map(c.rows.map((r) => [r.name, r]));
  const busy = new Set<string>();
  const get = (name: string): unknown => {
    if (name in out) {
      if (out[name] instanceof FormulaError) throw out[name];
      return out[name];
    }
    const r = rows.get(name);
    if (!r) throw new FormulaError("name");
    if (busy.has(name)) throw new FormulaError("cycle");
    busy.add(name);
    try {
      const v = r.input ? (inputs[name] ?? r.value ?? 0) : evaluate(r.expr, get);
      out[name] = v;
      return v;
    } catch (e) {
      out[name] = e instanceof FormulaError ? e : new FormulaError("value");
      throw out[name];
    } finally {
      busy.delete(name);
    }
  };
  for (const r of c.rows) { try { get(r.name); } catch { /* kept in out */ } }
  return out;
}

export function formatValue(v: unknown, loc = "de"): string {
  if (v instanceof FormulaError) return v.code === "div0" ? "#DIV/0" : v.code === "cycle" ? "#ZYKLUS" : "#FEHLER";
  if (typeof v === "number") return new Intl.NumberFormat(loc, { maximumFractionDigits: Math.abs(v) >= 100 ? 0 : 2 }).format(v);
  if (typeof v === "boolean") return v ? "✓" : "✗";
  return String(v ?? "");
}

function calcSVG(c: Calc, width: number, height: number, loc: string): string {
  const vals = calcValues(c, {});
  const top = c.title ? 34 : 6;
  const row = Math.min(30, Math.max(22, (height - top - 4) / Math.max(1, c.rows.length)));
  const rows = c.rows.map((r, i) => {
    const y = top + i * row + row / 2 + 4;
    const v = r.entity ? "–" : formatValue(vals[r.name], loc);
    return `<text x="6" y="${y}" style="font-size:13px;fill:var(--${r.input ? "muted" : "fg"})">${esc(r.label)}</text><text x="${width - 6}" y="${y}" text-anchor="end" style="font-size:13px;fill:var(--fg)${r.input ? "" : ";font-weight:600"}">${esc(v)}${r.unit ? " " + esc(r.unit) : ""}</text>`;
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)">${c.title ? `<text x="6" y="22" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(c.title)}</text>` : ""}${rows}</svg>`;
}

registerBlock<Calc>({
  kind: "calc",
  icon: "Calculator",
  label: "slash.calc",
  hint: "block.calc_hint",
  example: "title: Stromkosten des NAS\nLeistung (W) = 25\nStrompreis (€/kWh) = 0,32\nStunden pro Tag (h) = 24\nKosten pro Jahr (€) = Leistung * Stunden_pro_Tag * 365 / 1000 * Strompreis\nKosten pro Monat (€) = Kosten_pro_Jahr / 12",
  parse: parseCalc,
  svg: (c, s) => calcSVG(c, s.width, s.height ?? (c.title ? 34 : 6) + c.rows.length * 28 + 6, s.locale),
  board: { w: 420, h: 260, live: true },
  async mount(host, c, ctx) {
    const inputs: Record<string, number | undefined> = {};
    const loc = locale();
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .rows { display: grid; grid-template-columns: 1fr auto; gap: 6px 14px; align-items: center; }
      .rows label, .rows .lbl { color: var(--muted); }
      .rows .res { font-weight: 600; font-variant-numeric: tabular-nums; text-align: right; }
      .rows .lbl.res-l { color: var(--fg); font-weight: 600; }
      .rows input { width: 9em; font: inherit; text-align: right; padding: 4px 8px; border: 1px solid var(--line); border-radius: 6px; background: var(--bg); color: var(--fg); }
      .unit { color: var(--muted); font-size: 12.5px; margin-left: 4px; font-weight: 400; }
      .err { color: var(--bad); }
      .foot { margin-top: 10px; display: flex; gap: 8px; align-items: center; }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    let id = 0;
    box.innerHTML = `${c.title ? `<h3>${esc(c.title)}</h3>` : ""}<div class="rows">${c.rows.map((r) => {
      const fid = `c${ctx.key}-${id++}`;
      return r.input
        ? `<label for="${fid}">${esc(r.label)}</label><span><input id="${fid}" type="number" step="any" inputmode="decimal" data-n="${esc(r.name)}" value="${r.value ?? ""}">${r.unit ? `<span class="unit">${esc(r.unit)}</span>` : ""}</span>`
        : `<span class="lbl res-l">${esc(r.label)}</span><span class="res" data-o="${esc(r.name)}" aria-live="polite"></span>`;
    }).join("")}</div><div class="foot"><button type="button" class="ghost" data-reset hidden>${esc(t("calc.reset"))}</button><span class="muted" data-note hidden>${esc(t("calc.not_saved"))}</span></div>`;
    const update = () => {
      const vals = calcValues(c, inputs);
      for (const r of c.rows) {
        if (r.input) continue;
        const el = box.querySelector<HTMLElement>(`[data-o="${CSS.escape(r.name)}"]`);
        if (!el) continue;
        const v = vals[r.name];
        el.classList.toggle("err", v instanceof FormulaError);
        el.innerHTML = `${esc(formatValue(v, loc))}${r.unit ? `<span class="unit">${esc(r.unit)}</span>` : ""}`;
      }
      const changed = Object.keys(inputs).length > 0;
      box.querySelector<HTMLElement>("[data-reset]")!.hidden = !changed;
      box.querySelector<HTMLElement>("[data-note]")!.hidden = !changed;
    };
    box.addEventListener("input", (e) => {
      const el = e.target as HTMLInputElement;
      const n = el.dataset.n;
      if (!n) return;
      const v = el.value === "" ? undefined : Number(el.value);
      if (v === undefined || Number.isNaN(v)) delete inputs[n];
      else inputs[n] = v;
      update();
    });
    box.addEventListener("click", (e) => {
      if (!(e.target as Element).closest("[data-reset]")) return;
      for (const k of Object.keys(inputs)) if (!c.rows.find((r) => r.name === k)?.entity) delete inputs[k];
      box.querySelectorAll<HTMLInputElement>("input[data-n]").forEach((el) => {
        const r = c.rows.find((x) => x.name === el.dataset.n)!;
        if (!r.entity) el.value = String(r.value ?? "");
      });
      update();
    });
    update();
    // Inputs from entries: their current attribute values.
    const ents = c.rows.filter((r) => r.entity);
    if (ents.length && ctx.project) {
      const { api } = await import("../api");
      await Promise.all(ents.map(async (r) => {
        try {
          const e = await api<{ attrs?: Record<string, unknown> }>("GET", `/projects/${ctx.project}/entities/${r.entity!.slug}`);
          const v = Number(e.attrs?.[r.entity!.attr]);
          if (!Number.isNaN(v)) {
            r.value = v;
            const el = box.querySelector<HTMLInputElement>(`input[data-n="${CSS.escape(r.name)}"]`);
            if (el) el.value = String(v);
          }
        } catch { /* entry not readable: stays empty */ }
      }));
      update();
    }
  },
});
