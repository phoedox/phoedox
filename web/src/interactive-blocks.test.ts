import { describe, expect, test } from "bun:test";
import { blockDef } from "./blocks";
import { parsePoll } from "./blocks/poll";
import { parseQuiz } from "./blocks/quiz";

describe("poll", () => {
  test("question and answers", () => {
    const { model } = parsePoll("question: Wohin?\n- Zoo\n- Museum\nmultiple: ja\nid: ausflug");
    expect(model).toEqual({ question: "Wohin?", options: ["Zoo", "Museum"], multiple: true, closed: false, results: "after", id: "ausflug" });
    expect(parsePoll("Wohin?\n- Zoo\n- Zoo").problems[0].key).toBe("poll.err_duplicate");
    expect(parsePoll("Wohin?\n- Zoo").problems[0].key).toBe("poll.err_options");
    expect(blockDef("poll")!.board?.live).toBe(true);
    expect(blockDef("poll")!.idOf!(model)).toBe("ausflug");
  });
});

describe("quiz", () => {
  test("choices, flashcards and explanations", () => {
    const { model } = parseQuiz("title: Netz\n? Router?\n- 1.1\n+ 10.1\n> Seit März.\n\n? WLAN?\n= Gartenhaus");
    expect(model!.questions).toEqual([
      { q: "Router?", options: [{ text: "1.1", right: false }, { text: "10.1", right: true }], explain: "Seit März." },
      { q: "WLAN?", options: [], answer: "Gartenhaus" },
    ]);
    expect(parseQuiz("? A\n- x\n- y").problems[0].key).toBe("quiz.err_right");
    expect(parseQuiz("? A").problems[0].key).toBe("quiz.err_answers");
    expect(blockDef("quiz")!.board).toBeUndefined();
  });

  test("every block draws a picture", () => {
    for (const [k, src] of [["poll", "Q\n- a\n- b"], ["quiz", "? Q\n+ a"]]) {
      const d = blockDef(k)!;
      const svg = d.svg(d.parse(src).model, { width: 400, locale: "de" });
      expect(svg.startsWith("<svg")).toBe(true);
    }
  });
});

test("poll flags on their own line", () => {
  const r = parsePoll("multiple\nWas nehmen wir mit?\n- Picknick\n- Ball\nclosed");
  expect(r.model?.question).toBe("Was nehmen wir mit?");
  expect(r.model?.multiple).toBe(true);
  expect(r.model?.closed).toBe(true);
});
