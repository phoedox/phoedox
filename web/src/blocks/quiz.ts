// Quizzes and flashcards: "? question", "+ right" / "- wrong" answers or
// "= answer" to reveal, "> explanation". Each reader's answers are their own.
import { t } from "../i18n";
import { registerBlock, type BlockProblem } from "./registry";
import { BLOCK_CSS, blockState, esc, keyValue } from "./util";

export interface QuizQuestion { q: string; options: { text: string; right: boolean }[]; answer?: string; explain?: string }
export interface Quiz { title: string; id?: string; questions: QuizQuestion[] }

export function parseQuiz(src: string): { model?: Quiz; problems: BlockProblem[] } {
  const z: Quiz = { title: "", questions: [] };
  const problems: BlockProblem[] = [];
  let cur: QuizQuestion | undefined;
  for (const raw of src.replace(/\r\n/g, "\n").split("\n")) {
    const l = raw.trim();
    if (!l || l.startsWith("#")) continue;
    const kv = !cur && keyValue(l, ["title", "titel", "id"]);
    if (kv) { if (kv[0] === "id") z.id = kv[1]; else z.title = kv[1]; continue; }
    const m = /^([?+\-=>])\s*(.*)$/.exec(l);
    if (!m) { problems.push({ key: "quiz.err_line", params: { line: l } }); continue; }
    const [, mark, text] = m;
    if (mark === "?") { cur = { q: text, options: [] }; z.questions.push(cur); continue; }
    if (!cur) { problems.push({ key: "quiz.err_line", params: { line: l } }); continue; }
    if (mark === "+" || mark === "-") cur.options.push({ text, right: mark === "+" });
    else if (mark === "=") cur.answer = text;
    else cur.explain = cur.explain ? `${cur.explain} ${text}` : text;
  }
  if (!z.questions.length) problems.push({ key: "quiz.err_empty" });
  z.questions.forEach((q, i) => {
    if (!q.options.length && !q.answer) problems.push({ key: "quiz.err_answers", params: { n: i + 1 } });
    else if (q.options.length && !q.options.some((o) => o.right)) problems.push({ key: "quiz.err_right", params: { n: i + 1 } });
  });
  return problems.length ? { problems } : { model: z, problems };
}

type Answers = Record<string, number | "known" | "unknown">;

function quizSVG(z: Quiz, width: number, height: number): string {
  const top = z.title ? 34 : 6;
  const items = z.questions.map((q, i) => `<text x="6" y="${top + i * 24 + 16}" style="font-size:13px;fill:var(--fg)">${i + 1}. ${esc(q.q)}</text>`).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}" style="font-family:var(--font-body,system-ui,sans-serif)">${z.title ? `<text x="6" y="22" style="font-size:16px;font-weight:600;fill:var(--fg)">${esc(z.title)}</text>` : ""}${items}</svg>`;
}

registerBlock<Quiz>({
  kind: "quiz",
  icon: "BookOpenCheck",
  label: "slash.quiz",
  hint: "block.quiz_hint",
  example: "title: Kennst du unser Netz?\n? Welche Adresse hat der Router?\n- 192.168.1.1\n+ 192.168.10.1\n- 10.0.0.1\n> Seit dem Umbau im März liegt das Heimnetz in 192.168.10.0/24.\n\n? Wie heißt das Gäste-WLAN?\n= Gartenhaus",
  parse: parseQuiz,
  svg: (z, s) => quizSVG(z, s.width, s.height ?? (z.title ? 34 : 6) + z.questions.length * 24 + 8),
  idOf: (z) => z.id,
  async mount(host, z, ctx) {
    const state = blockState(ctx);
    let answers: Answers = {};
    const style = document.createElement("style");
    style.textContent = `${BLOCK_CSS}
      .head { display: flex; align-items: baseline; gap: 12px; margin-bottom: 8px; flex-wrap: wrap; }
      .head h3 { margin: 0; }
      .score { margin-left: auto; font-size: 13px; color: var(--muted); }
      ol { margin: 0; padding: 0; list-style: none; counter-reset: q; display: grid; gap: 12px; }
      li.q { counter-increment: q; border-top: 1px solid var(--line); padding-top: 10px; }
      li.q:first-child { border-top: 0; padding-top: 0; }
      .qt { font-weight: 600; margin-bottom: 6px; }
      .qt::before { content: counter(q) ". "; color: var(--muted); }
      .opts { display: grid; gap: 5px; }
      .opt { text-align: left; border: 1px solid var(--line); border-radius: 8px; padding: 7px 12px; background: var(--surface); color: var(--fg); }
      .opt.right { border-color: var(--good); background: color-mix(in srgb, var(--good) 12%, var(--surface)); }
      .opt.wrong { border-color: var(--bad); background: color-mix(in srgb, var(--bad) 10%, var(--surface)); }
      .opt:disabled { cursor: default; }
      .ans { padding: 8px 12px; border-radius: 8px; background: var(--surface2); margin: 4px 0; }
      .ex { margin-top: 6px; font-size: 13px; color: var(--muted); }
      .row { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 6px; }
      .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }`;
    const box = document.createElement("div");
    box.className = "blk";
    host.replaceChildren(style, box);
    const draw = () => {
      let right = 0, done = 0;
      z.questions.forEach((q, i) => {
        const a = answers[i];
        if (a === undefined) return;
        done++;
        if (a === "known" || (typeof a === "number" && q.options[a]?.right)) right++;
      });
      box.innerHTML = `<div class="head">${z.title ? `<h3>${esc(z.title)}</h3>` : ""}<span class="score" aria-live="polite">${esc(t("quiz.score", { right, all: z.questions.length }))}</span>
        ${done ? `<button type="button" class="ghost" data-again>${esc(t("quiz.again"))}</button>` : ""}</div>
        <ol>${z.questions.map((q, i) => {
          const a = answers[i];
          const body = q.options.length
            ? `<div class="opts" role="group" aria-label="${esc(q.q)}">${q.options.map((o, j) => {
                const cls = a === undefined ? "" : o.right ? "right" : a === j ? "wrong" : "";
                const note = a === undefined ? "" : o.right ? ` (${t("quiz.right")})` : a === j ? ` (${t("quiz.wrong")})` : "";
                return `<button type="button" class="opt ${cls}" data-q="${i}" data-o="${j}" ${a === undefined ? "" : "disabled"}>${esc(o.text)}<span class="sr">${esc(note)}</span></button>`;
              }).join("")}</div>`
            : a === undefined
              ? `<div class="row"><button type="button" class="ghost" data-show="${i}">${esc(t("quiz.show"))}</button></div>`
              : `<div class="ans">${esc(q.answer)}</div><div class="ex">${esc(t(a === "known" ? "quiz.knew" : "quiz.missed"))}</div>`;
          const ex = a !== undefined && q.explain ? `<div class="ex">${esc(q.explain)}</div>` : "";
          return `<li class="q"><div class="qt">${esc(q.q)}</div>${body}${ex}</li>`;
        }).join("")}</ol>`;
    };
    const save = () => state.put(Object.keys(answers).length ? { answers } : null).catch(() => undefined);
    box.addEventListener("click", (e) => {
      const b = (e.target as Element).closest("button");
      if (!b) return;
      if (b.hasAttribute("data-again")) { answers = {}; void save(); draw(); return; }
      const show = b.getAttribute("data-show");
      if (show !== null) {
        // Flashcards: reveal, then the reader says whether they knew it.
        const i = Number(show);
        const li = b.closest("li")!;
        li.querySelector(".row")!.outerHTML = `<div class="ans">${esc(z.questions[i].answer)}</div><div class="row"><button type="button" class="ghost" data-knew="${i}">${esc(t("quiz.knew"))}</button><button type="button" class="ghost" data-missed="${i}">${esc(t("quiz.missed"))}</button></div>`;
        return;
      }
      const knew = b.getAttribute("data-knew") ?? b.getAttribute("data-missed");
      if (knew !== null) { answers[knew] = b.hasAttribute("data-knew") ? "known" : "unknown"; void save(); draw(); return; }
      const q = b.getAttribute("data-q"), o = b.getAttribute("data-o");
      if (q !== null && o !== null) { answers[q] = Number(o); void save(); draw(); }
    });
    try { answers = ((await state.get<{ answers?: Answers }>()).mine?.answers) ?? {}; } catch { /* nothing saved */ }
    draw();
  },
});
