import { describe, expect, test } from "bun:test";
import { markdownHTML } from "./md";
import { CALLOUT_START, calloutHead, parseCallout } from "./syntax";
import { compareRows, parseCompare } from "./components/compare-logic";

describe("structure blocks", () => {
  test("callouts can fold and carry a title (Obsidian style)", () => {
    const m = CALLOUT_START.exec("> [!faq]- Why does the NAS sleep?")!;
    const a = parseCallout(m[1], m[2], m[3], m[4])!;
    expect(a).toMatchObject({ kind: "faq", fold: "-", title: "Why does the NAS sleep?" });
    expect(calloutHead(a)).toBe("> [!faq]- Why does the NAS sleep?");
    expect(calloutHead(parseCallout("note", "", "", "")!)).toBe("> [!note]");
    expect(calloutHead({ ...parseCallout("steps", " id=ab12", "", "Setup")! })).toBe("> [!steps id=ab12] Setup");
    expect(CALLOUT_START.exec("> [!NOTE]")?.[1]).toBe("NOTE"); // GitHub alerts still work
  });

  test("foldable callouts become <details>, open with +", () => {
    const html = markdownHTML("> [!faq]- Why?\n> Because.\n\n> [!note]+ Open\n> Yes.", "p");
    expect(html).toContain('<details class="callout fold cb-gray" data-kind="faq">');
    expect(html).toContain('<span class="ct">Why?</span>');
    expect(html).toContain('data-kind="note" open>');
  });

  test("tabs that follow each other form one group", () => {
    const html = markdownHTML("> [!tab] Windows\n> Press **Win**.\n\n> [!tab] macOS\n> Press Cmd.\n\nAfter.", "p");
    expect((html.match(/<phx-tabs>/g) ?? []).length).toBe(1);
    expect(html).toContain('<section class="tab" data-title="Windows"><h4 class="tab-h">Windows</h4>');
    expect(html).toContain("<strong>Win</strong>");
    expect(html).toContain('data-title="macOS"');
    expect(html).toContain("<p>After.</p>");
  });

  test("steps keep their id as key", () => {
    const html = markdownHTML("> [!steps id=x1] Setup\n> 1. Plug in\n> 2. Switch on", "home", "doc1");
    expect(html).toContain('<phx-steps data-project="home" data-doc="doc1" data-key="x1">');
    expect(html).toContain("<ol>");
    expect(markdownHTML("> [!steps] Setup\n> 1. a", "home", "d")).toMatch(/data-key="t[0-9a-z]+"/);
  });

  test("comparisons pair removed and added lines", () => {
    const c = parseCompare("--- old\na\nb\nc\n+++ new\na\nB\nc\nd\n")!;
    expect(c.leftTitle).toBe("old");
    expect(c.rightTitle).toBe("new");
    const rows = compareRows(c.left, c.right);
    expect(rows.map((r) => r.op)).toEqual(["same", "change", "same", "add"]);
    expect(rows[1]).toMatchObject({ l: "b", r: "B", ln: 2, rn: 2 });
    expect(parseCompare("no plus line")).toBeUndefined();
    expect(markdownHTML("```compare\n--- a\nx\n+++ b\ny\n```", "p")).toContain("<phx-compare><pre>");
  });
});
