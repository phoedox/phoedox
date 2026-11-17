// Poll blocks: a question, options, one vote per person (or several), the
// results as bars. Votes are counted on the server and never show who voted.
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockLines, blockState, esc, keyValue, yes } from "./util";

export interface Poll { question: string; options: string[]; multiple: boolean; closed: boolean; results: "always" | "after"; id?: string }

export function parsePoll(src: string): { model?: Poll; problems: BlockProblem[] } {
  const p: Poll = { question: "", options: [], multiple: false, closed: false, results: "after" };
  for (const l of blockLines(src)) {
    const kv = keyValue(l, ["question", "frage", "multiple", "closed", "results", "id"]);
    if (kv) {
      const [k, v] = kv;
      if (k === "question" || k === "frage") p.question = v;
      else if (k === "multiple") p.multiple = yes(v);
      else if (k === "closed") p.closed = yes(v);
      else if (k === "results") p.results = /always|immer/i.test(v) ? "always" : "after";
      else if (k === "id") p.id = v;
      continue;
    }
    // A flag on its own line: "multiple" or "closed".
    if (/^multiple$/i.test(l)) { p.multiple = true; continue; }
    if (/^closed$/i.test(l)) { p.closed = true; continue; }
    const opt = /^[-*+]\s+(.+)$/.exec(l);
    if (opt) p.options.push(opt[1].trim());
    else if (!p.question) p.question = l;
  }
  const problems: BlockProblem[] = [];
  if (!p.question) problems.push({ key: "poll.err_question" });
  if (p.options.length < 2) problems.push({ key: "poll.err_options" });
  if (p.options.length > 30) problems.push({ key: "poll.err_too_many" });
  const dup = p.options.find((o, i) => p.options.indexOf(o) !== i);
  if (dup) problems.push({ key: "poll.err_duplicate", params: { option: dup } });
  return problems.length ? { problems } : { model: p, problems };
}

/** The poll as a picture: the question and the options (results need the live view). */
function pollSVG(p: Poll, width: number, height: number, votes?: Record<string, number>): string {
  const total = Object.values(votes ?? {}).reduce((a, b) => a + b, 0);
  const row = Math.min(40, Math.max(26, (height - 40) / Math.max(1, p.options.length)));
  const items = p.options.map((o, i) => {
    const y = 40 + i * row, n = votes?.[o] ?? 0, frac = total ? n / total : 0;
    return `<rect x="6" y="${y}" width="${width - 12}" height="${row - 6}" rx="6" style="fill:var(--surface2)"/>` +
      (votes ? `<rect x="6" y="${y}" width="${Math.max(0, (width - 12) * frac)}" height="${row - 6}" rx="6" style="fill:var(--tb-blue)"/>` : "") +
      `<text x="16" y="${y + (row - 6) / 2 + 4.5}" style="font-size:13px;fill:var(--fg)">${esc(o)}</text>` +
      (votes ? `<text x="${width - 16}" y="${y + (row - 6) / 2 + 4.5}" text-anchor="end" style="font-size:12px;fill:var(--muted)">${n}</text>` : "");
  }).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)"><text x="6" y="22" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(p.question)}</text>${items}</svg>`;
}

registerBlock<Poll>({
  kind: "poll",
  icon: "Vote",
  label: "slash.poll",
  hint: "block.poll_hint",
  example: "question: Wohin geht der Familienausflug?\n- Zoo\n- Museum\n- Schwimmbad",
  parse: parsePoll,
  svg: (p, s) => pollSVG(p, s.width, s.height ?? 40 + p.options.length * 34 + 10),
  idOf: (p) => p.id,
  board: { w: 420, h: 300, live: true },
  async mount(host, p, ctx) {
    const state = blockState(ctx);
    let mine: string[] = [];
    let votes: Record<string, number> = {};
    let voters = 0;
    let busy = false;
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .opts { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
      .opt { position: relative; display: flex; align-items: center; gap: 10px; width: 100%; text-align: left; border: 1px solid var(--line); border-radius: 8px; padding: 9px 12px; background: var(--surface); color: var(--fg); overflow: hidden; }
      .opt[aria-checked=true] { border: 2px solid var(--accent); padding: 8px 11px; }
      .opt .bar { position: absolute; inset: 0 auto 0 0; background: var(--tb-blue); z-index: 0; transition: width var(--dur, 200ms) var(--ease-out, ease-out); }
      .opt > :not(.bar) { position: relative; z-index: 1; }
      .opt .mark { width: 16px; height: 16px; flex: none; border: 2px solid var(--muted); border-radius: 50%; box-sizing: border-box; }
      .opt.multi .mark { border-radius: 4px; }
      .opt[aria-checked=true] .mark { border-color: var(--accent); background: radial-gradient(var(--accent) 45%, transparent 50%); }
      .opt.multi[aria-checked=true] .mark { background: var(--accent); }
      .opt .n { margin-left: auto; font-variant-numeric: tabular-nums; color: var(--muted); font-size: 13px; }
      .opt:disabled { cursor: default; }
      .foot { display: flex; gap: 10px; align-items: center; margin-top: 10px; flex-wrap: wrap; }
      @media (prefers-reduced-motion: reduce) { .opt .bar { transition: none; } }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    const draw = () => {
      const total = Object.values(votes).reduce((a, b) => a + b, 0);
      const show = p.closed || p.results === "always" || mine.length > 0;
      const canVote = !p.closed && state.remote;
      box.innerHTML = `<h3 id="q">${esc(p.question)}</h3>
        <div class="opts" role="${p.multiple ? "group" : "radiogroup"}" aria-labelledby="q">
        ${p.options.map((o, i) => {
          // Several answers each: the share of people; one answer each: the share of votes.
          const base = p.multiple ? voters : total;
          const n = votes[o] ?? 0, pct = base ? Math.round((n / base) * 100) : 0, on = mine.includes(o);
          return `<button type="button" class="opt${p.multiple ? " multi" : ""}" role="${p.multiple ? "checkbox" : "radio"}" aria-checked="${on}" data-i="${i}" ${canVote && !busy ? "" : "disabled"}>
            ${show ? `<span class="bar" style="width:${pct}%"></span>` : ""}<span class="mark" aria-hidden="true"></span><span>${esc(o)}</span>${show ? `<span class="n">${n} · ${pct}%</span>` : ""}</button>`;
        }).join("")}</div>
        <div class="foot"><span class="muted" aria-live="polite">${esc(p.closed ? t("poll.closed") : !state.remote ? t("poll.sign_in") : show ? t(voters === 1 ? "poll.voters_one" : "poll.voters", { n: voters }) : t(p.multiple ? "poll.choose_many" : "poll.choose_one"))}</span>
        ${mine.length && canVote ? `<button type="button" class="ghost" data-undo>${esc(t("poll.undo"))}</button>` : ""}</div>`;
    };
    const apply = (r: { mine: { choices?: string[] } | null; votes?: Record<string, number>; voters?: number }) => {
      mine = (r.mine?.choices ?? []).filter((c) => p.options.includes(c));
      votes = r.votes ?? {};
      voters = r.voters ?? 0;
      draw();
    };
    box.addEventListener("click", async (e) => {
      const b = (e.target as Element).closest("button");
      if (!b || busy) return;
      let next: string[] | null;
      if (b.hasAttribute("data-undo")) next = null;
      else {
        const o = p.options[Number(b.getAttribute("data-i"))];
        next = p.multiple ? (mine.includes(o) ? mine.filter((x) => x !== o) : [...mine, o]) : [o];
        if (!next.length) next = null;
      }
      busy = true;
      try { apply(await state.put<{ choices?: string[] }>(next ? { choices: next } : null)); } catch { /* keep the old view */ } finally { busy = false; draw(); }
    });
    try { apply(await state.get<{ choices?: string[] }>()); } catch { draw(); }
    // Others vote too: look again now and then while the page is visible.
    const timer = window.setInterval(() => { if (!document.hidden && !busy && state.remote) state.get<{ choices?: string[] }>().then(apply, () => undefined); }, 30000);
    return () => clearInterval(timer);
  },
});

