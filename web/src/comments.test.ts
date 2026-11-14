import { describe, expect, test } from "bun:test";
import { anchorOf, blockFor, mentionQuery, mentionedIds, splitMentions } from "./comments";

describe("comments", () => {
  test("anchors find their block, also after small edits elsewhere", () => {
    const blocks = ["Netz", "Der Router steht\n im Flur.", "Der Switch hängt im Keller."];
    expect(blockFor(anchorOf(blocks[1]), blocks)).toBe(1);
    expect(blockFor("Der Switch hängt", blocks)).toBe(2);
    expect(blockFor("Gibt es nicht", blocks)).toBe(-1);
    expect(blockFor("", blocks)).toBe(-1);
    expect(anchorOf("x".repeat(300)).length).toBe(160);
  });

  test("mention query only right after @", () => {
    expect(mentionQuery("Hallo @ja", 9)).toEqual({ start: 6, query: "ja" });
    expect(mentionQuery("@", 1)).toEqual({ start: 0, query: "" });
    expect(mentionQuery("mail@example", 12)).toBeNull();
    expect(mentionQuery("Hallo @jan ", 11)).toBeNull();
  });

  test("mentions follow the text", () => {
    const picked = new Map([["Jan Meinl", "u1"], ["Eva", "u2"]]);
    expect(mentionedIds("@Jan Meinl schau mal", picked)).toEqual(["u1"]);
    const parts = splitMentions("Frag @Eva oder @Jan Meinl.", ["Jan Meinl", "Eva"]);
    expect(parts.filter((p) => p.mention).map((p) => p.text)).toEqual(["@Eva", "@Jan Meinl"]);
    expect(parts.map((p) => p.text).join("")).toBe("Frag @Eva oder @Jan Meinl.");
    expect(splitMentions("ohne", [])).toEqual([{ text: "ohne", mention: false }]);
  });
});
