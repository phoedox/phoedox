import { describe, expect, test } from "bun:test";
import { parseCardSubtasks, subtasksShown, toggleAll, toggleCard } from "./card-subtasks";

describe("subtasks on cards", () => {
  test("reads the older board switch and bad values", () => {
    expect(parseCardSubtasks("1")).toEqual({ all: true, cards: {} });
    expect(parseCardSubtasks("0")).toEqual({ all: false, cards: {} });
    expect(parseCardSubtasks(null)).toEqual({ all: false, cards: {} });
    expect(parseCardSubtasks("{oops")).toEqual({ all: false, cards: {} });
    expect(parseCardSubtasks(JSON.stringify({ all: true, cards: { a: false, b: "x" } }))).toEqual({ all: true, cards: { a: false } });
  });

  test("one card switched on while the board shows none", () => {
    let s = parseCardSubtasks(null);
    s = toggleCard(s, "a");
    expect(subtasksShown(s, "a")).toBe(true);
    expect(subtasksShown(s, "b")).toBe(false);
    s = toggleCard(s, "a");
    expect(s.cards).toEqual({});
  });

  test("one card hidden while the board shows all", () => {
    const s = toggleCard({ all: true, cards: {} }, "a");
    expect(subtasksShown(s, "a")).toBe(false);
    expect(subtasksShown(s, "b")).toBe(true);
  });

  test("the board switch resets the cards", () => {
    const s = toggleAll(toggleCard({ all: false, cards: {} }, "a"));
    expect(s).toEqual({ all: true, cards: {} });
    expect(parseCardSubtasks(JSON.stringify(s))).toEqual(s);
  });
});
