// Decision matrices: options × weighted criteria. Scores are added up with
// their weights; readers can try other weights (not saved). The options can
// become the options of an open decision (ADR-0062).
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, esc, keyValue } from "./util";

export interface Criterion { name: string; weight: number }
export interface MatrixOption { name: string; scores: number[] }
export interface Matrix { title: string; criteria: Criterion[]; options: MatrixOption[]; max: number }

export function parseMatrix(src: string): { model?: Matrix; problems: BlockProblem[] } {
  const m: Matrix = { title: "", criteria: [], options: [], max: 0 };
  const problems: BlockProblem[] = [];
  let scaleMax = 0;
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["title", "titel", "criteria", "kriterien", "scale", "skala"]);
    if (kv) {
      const [k, v] = kv;
      if (k === "title" || k === "titel") m.title = v;
      else if (k === "scale" || k === "skala") scaleMax = Number(/(\d+(?:[.,]\d+)?)\s*$/.exec(v)?.[1]?.replace(",", ".") ?? 0);
      else m.criteria = v.split(",").map((c) => c.trim()).filter(Boolean).map((c) => {
        const w = /^(.*?)\s*[*×x]\s*(\d+(?:[.,]\d+)?)$/.exec(c);
        return w ? { name: w[1].trim(), weight: Number(w[2].replace(",", ".")) } : { name: c, weight: 1 };
      });
      continue;
    }
    const o = /^[-*+]\s+(.+?)\s*:\s*(.+)$/.exec(l);
    if (!o) continue;
    const parts = o[2].split(/[,;]/).map((x) => x.trim());
    const scores = parts.map((x) => Number(x.replace(",", ".")));
    if (scores.some((x, i) => parts[i] === "" || Number.isNaN(x))) problems.push({ key: "matrix.err_score", params: { option: o[1] } });
    m.options.push({ name: o[1], scores });
  }
  if (!m.criteria.length) problems.push({ key: "matrix.err_criteria" });
  if (m.options.length < 2) problems.push({ key: "matrix.err_options" });
  for (const o of m.options) {
    if (m.criteria.length && o.scores.length !== m.criteria.length) problems.push({ key: "matrix.err_count", params: { option: o.name, n: m.criteria.length } });
  }
  m.max = scaleMax || Math.max(5, ...m.options.flatMap((o) => o.scores.filter((x) => !Number.isNaN(x))));
  return problems.length ? { problems } : { model: m, problems };
}

/** Each option's share of the best possible result (0–1) with the given weights. */
export function matrixScores(m: Matrix, weights = m.criteria.map((c) => c.weight)): number[] {
  const wsum = weights.reduce((a, b) => a + b, 0) || 1;
  return m.options.map((o) => o.scores.reduce((s, x, i) => s + x * (weights[i] ?? 0), 0) / (wsum * m.max));
}

function matrixSVG(m: Matrix, width: number, height: number): string {
  const scores = matrixScores(m);
  const best = Math.max(...scores);
  const top = m.title ? 34 : 6;
  const row = Math.min(38, Math.max(24, (height - top - 6) / m.options.length));
  const label = Math.min(width * 0.4, 12 + Math.max(...m.options.map((o) => o.name.length)) * 7);
  const bars = m.options.map((o, i) => {
    const y = top + i * row, w = (width - label - 60) * scores[i], c = scores[i] === best ? "green" : "gray";
    return `<text x="6" y="${y + row / 2 + 4}" style="font-size:13px;fill:var(--fg)${scores[i] === best ? ";font-weight:600" : ""}">${esc(o.name)}</text>
      <rect x="${label}" y="${y + 4}" width="${Math.max(2, w)}" height="${row - 10}" rx="5" style="fill:var(--tc-${c})"/>
      <text x="${label + Math.max(2, w) + 6}" y="${y + row / 2 + 4}" style="font-size:12px;fill:var(--muted)">${Math.round(scores[i] * 100)} %</text>`;
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)">${m.title ? `<text x="6" y="22" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(m.title)}</text>` : ""}${bars}</svg>`;
}

registerBlock<Matrix>({
  kind: "matrix",
  icon: "Grid3x3",
  label: "slash.matrix",
  hint: "block.matrix_hint",
  example: "title: Welches NAS kaufen wir?\ncriteria: Preis*3, Lautstärke*2, Speicher*3, Stromverbrauch*1\nscale: 1-5\n- Synology DS224+: 3, 4, 3, 4\n- QNAP TS-264: 4, 3, 4, 3\n- Eigenbau: 5, 2, 5, 2",
  parse: parseMatrix,
  svg: (m, s) => matrixSVG(m, s.width, s.height ?? (m.title ? 34 : 6) + m.options.length * 34 + 8),
  board: { w: 520, h: 260 },
  async mount(host, m, ctx) {
    let weights = m.criteria.map((c) => c.weight);
    let tuning = false;
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .wrap { overflow-x: auto; }
      table { border-collapse: collapse; width: 100%; font-size: 13.5px; }
      th, td { padding: 6px 8px; border-bottom: 1px solid var(--line); text-align: center; white-space: nowrap; }
      th:first-child, td:first-child { text-align: left; white-space: normal; }
      th { font-weight: 600; color: var(--muted); font-size: 12.5px; }
      th .w { display: block; font-weight: 400; }
      th input { width: 3.4em; font: inherit; text-align: center; border: 1px solid var(--line); border-radius: 6px; background: var(--bg); color: var(--fg); padding: 2px; }
      td.total { min-width: 150px; text-align: left; }
      .bar { display: inline-block; height: 10px; border-radius: 5px; background: var(--tc-gray); vertical-align: middle; margin-right: 6px; transition: width var(--dur, 200ms) var(--ease-out, ease-out); }
      tr.best td { background: color-mix(in srgb, var(--good) 9%, transparent); }
      tr.best .bar { background: var(--good); }
      tr.best td:first-child { font-weight: 600; }
      .foot { display: flex; gap: 8px; margin-top: 10px; flex-wrap: wrap; align-items: center; }
      @media (prefers-reduced-motion: reduce) { .bar { transition: none; } }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    const draw = () => {
      const scores = matrixScores(m, weights);
      const best = Math.max(...scores);
      const changed = weights.some((w, i) => w !== m.criteria[i].weight);
      box.innerHTML = `${m.title ? `<h3>${esc(m.title)}</h3>` : ""}<div class="wrap"><table>
        <thead><tr><th scope="col">${esc(t("matrix.option"))}</th>${m.criteria.map((c, i) => `<th scope="col">${esc(c.name)}<span class="w">${tuning
          ? `<input type="number" min="0" max="10" step="1" value="${weights[i]}" data-w="${i}" aria-label="${esc(t("matrix.weight_of", { name: c.name }))}">`
          : `× ${weights[i]}`}</span></th>`).join("")}<th scope="col">${esc(t("matrix.total"))}</th></tr></thead>
        <tbody>${m.options.map((o, i) => `<tr class="${scores[i] === best ? "best" : ""}"><td>${esc(o.name)}</td>${o.scores.map((x) => `<td>${x}</td>`).join("")}
          <td class="total"><span class="bar" style="width:${Math.round(scores[i] * 100)}px"></span>${Math.round(scores[i] * 100)} %</td></tr>`).join("")}</tbody></table></div>
        <div class="foot"><button type="button" class="ghost" data-tune aria-pressed="${tuning}">${esc(t(tuning ? "matrix.tune_done" : "matrix.tune"))}</button>
        ${changed ? `<button type="button" class="ghost" data-reset>${esc(t("matrix.reset"))}</button><span class="muted">${esc(t("matrix.not_saved"))}</span>` : ""}
        ${ctx.project && !ctx.board ? `<button type="button" class="ghost" data-adr>${esc(t("matrix.to_decision"))}</button>` : ""}</div>`;
    };
    box.addEventListener("input", (e) => {
      const i = (e.target as HTMLElement).getAttribute("data-w");
      if (i === null) return;
      weights[Number(i)] = Math.max(0, Number((e.target as HTMLInputElement).value) || 0);
      const scores = matrixScores(m, weights), best = Math.max(...scores);
      box.querySelectorAll("tbody tr").forEach((tr, j) => {
        tr.classList.toggle("best", scores[j] === best);
        const cell = tr.querySelector(".total")!;
        cell.innerHTML = `<span class="bar" style="width:${Math.round(scores[j] * 100)}px"></span>${Math.round(scores[j] * 100)} %`;
      });
    });
    box.addEventListener("click", async (e) => {
      const b = (e.target as Element).closest("button");
      if (!b) return;
      if (b.hasAttribute("data-tune")) { tuning = !tuning; draw(); }
      if (b.hasAttribute("data-reset")) { weights = m.criteria.map((c) => c.weight); draw(); }
      if (b.hasAttribute("data-adr")) {
        // An open decision whose options are the matrix's options (people choose in Phoedox).
        const scores = matrixScores(m, weights);
        const { api } = await import("../api");
        const { toast } = await import("../components/toasts");
        try {
          const r = await api<{ document: { slug: string } }>("POST", `/projects/${ctx.project}/documents`, {
            type: "adr", title: m.title || t("matrix.decision_title"),
            adr: { status: "proposed", context: t("matrix.decision_context"), options: m.options.map((o, i) => ({
              title: o.name,
              description: `${m.criteria.map((c, j) => `- ${c.name} (× ${weights[j]}): ${o.scores[j]}`).join("\n")}\n\n**${t("matrix.total")}: ${Math.round(scores[i] * 100)} %**`,
            })) },
          });
          toast(t("matrix.decision_made"));
          (await import("../state")).navigate(`/p/${ctx.project}/d/${r.document.slug}`);
        } catch {
          toast(t("matrix.decision_failed"), "error");
        }
      }
    });
    draw();
  },
});
