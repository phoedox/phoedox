import { describe, expect, test } from "bun:test";
import { blockDef } from "./blocks";
import { calcName, calcValues, formatValue, parseCalc } from "./blocks/calc";
import { parsePoll } from "./blocks/poll";
import { parseQuiz } from "./blocks/quiz";
import { FormulaError, evaluate, formulaNames } from "./formula";

describe("formulas with names", () => {
  test("arithmetic, names, functions, percent", () => {
    const v: Record<string, number> = { Leistung: 25, Stunden_pro_Tag: 24, Preis: 0.32 };
    const get = (n: string) => { if (!(n in v)) throw new FormulaError("name"); return v[n]; };
    expect(evaluate("Leistung * Stunden_pro_Tag * 365 / 1000 * Preis", get)).toBeCloseTo(70.08);
    expect(evaluate("ROUND(10/3; 1)", get)).toBe(3.3);
    expect(evaluate("19%*100", get)).toBe(19);
    expect(evaluate('IF(Leistung > 20, "viel", "wenig")', get)).toBe("viel");
    expect(evaluate("2,5 * 2", get)).toBe(5);
    expect(() => evaluate("1/0", get)).toThrow("div0");
    expect(() => evaluate("Unbekannt + 1", get)).toThrow("name");
    expect(formulaNames("Größe * Anzahl + SUM(a, b)")).toEqual(["Größe", "Anzahl", "a", "b"]);
  });
});

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

describe("calculator", () => {
  test("inputs, formulas, units and errors", () => {
    expect(calcName("Kosten pro Jahr (€)")).toEqual({ name: "Kosten_pro_Jahr", unit: "€" });
    const { model, problems } = parseCalc("title: Strom\nLeistung (W) = 25\nPreis (€/kWh) = 0,32\nKosten pro Jahr (€) = Leistung * 24 * 365 / 1000 * Preis\nPro Monat (€) = Kosten_pro_Jahr / 12\nNAS (W) = @nas.power_w");
    expect(problems).toEqual([]);
    expect(model!.rows.find((r) => r.name === "NAS")!.entity).toEqual({ slug: "nas", attr: "power_w" });
    const v = calcValues(model!, {});
    expect(v.Kosten_pro_Jahr).toBeCloseTo(70.08);
    expect(calcValues(model!, { Leistung: 50 }).Pro_Monat).toBeCloseTo(11.68);
    expect(formatValue(1234.567, "de")).toBe("1.235");
    expect(formatValue(5.678, "de")).toBe("5,68");
    expect(parseCalc("A = B + 1\nB = A * 2").problems[0].key).toBe("calc.err_cycle");
    expect(parseCalc("A = C + 1").problems[0]).toEqual({ key: "calc.err_unknown", params: { name: "C", row: "A" } });
    expect(parseCalc("nur Text").problems[0].key).toBe("calc.err_line");
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
    for (const [k, src] of [["poll", "Q\n- a\n- b"], ["calc", "A = 1\nB = A + 1"], ["quiz", "? Q\n+ a"]]) {
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
