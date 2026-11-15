import { describe, expect, test } from "bun:test";
import { forEditor, importedTypes, lossless, mdBlocks, roundTrip } from "./markdown";

const TABLE = `| Bereich | Stand | Hinweis |
|---|---|---|
| Plugins | fertig | \`phx_host\` mit @ups |
| Store | **läuft** | [Link](https://store.phoedox.com) |`;

describe("markdown round trip", () => {
  test("tables become table nodes and survive", () => {
    const types = importedTypes(TABLE);
    expect(types).toContain("table");
    expect(types.filter((t) => t === "tablerow").length).toBe(3);
    expect(lossless(TABLE)).toBe(true);
    expect(roundTrip(TABLE)).toContain("| Plugins | fertig |");
  });

  test("table with text around it", () => {
    const md = `# Titel\n\nVorher.\n\n${TABLE}\n\nNachher.`;
    expect(lossless(md)).toBe(true);
  });

  test("escaped pipes stay in cells", () => {
    const md = "| a | b |\n|---|---|\n| x \\| y | z |";
    expect(lossless(md)).toBe(true);
  });

  test("common constructs", () => {
    for (const md of [
      "Absatz mit **fett**, *kursiv* und `code`.",
      "- eins\n- zwei\n  - drei",
      "1. eins\n2. zwei",
      "> Zitat",
      "```go\nfunc main() {}\n```",
      "```request\nGET {{base}}/api\n```",
      "---",
      "Text\n\n---\n\nmehr",
      "- [ ] offen\n- [x] erledigt",
      "![Bild](/api/v1/files/abc123)",
      "## Überschrift\n\nText mit @router und @haus/nas.",
    ]) {
      expect({ md, ok: lossless(md) }).toEqual({ md, ok: true });
    }
  });

  test("lossy content is detected", () => {
    expect(lossless("<details><summary>x</summary>y</details>")).toBe(false);
    expect(lossless("Text[^1]\n\n[^1]: Fußnote")).toBe(false);
  });
});

describe("building blocks round trip (ADR-0031)", () => {
  const cases = [
    "> [!note]\n> Ein Hinweis mit **fett**.",
    "> [!callout icon=User color=orange]\n> Wichtig ist die Änderung",
    "> [!warning]\n> Zeile eins\n>\n> - Liste\n> - zwei",
    "Text [rot]{color=red} und [markiert]{bg=yellow} weiter.",
    "Text [**fett rot**]{color=red} weiter.",
    "Ein ++unterstrichener++ Text.",
    "Formel $a^2+b^2=c^2$ im Satz, aber $5 und $10 nicht.",
    'Termin <time datetime="2026-12-24">24.12.2026</time> und <time datetime="2026-12-24T18:00" data-kind="countdown">Heiligabend</time>.',
    "```math\n\\int_0^1 x\\,dx\n```",
    "```embed\nhttps://youtu.be/dQw4w9WgXcQ\n```",
    "```iframe\nhttps://example.org/status\n```",
    "```board\numbau\n```",
    "```mermaid\nflowchart LR\n  A --> B\n```",
    "| a | b |\n|---|---|\n| [rot]{color=red} | $x$ |",
  ];
  for (const md of cases) {
    test(md.slice(0, 40), () => {
      expect({ md, out: roundTrip(md), ok: lossless(md) }).toEqual({ md, out: roundTrip(md), ok: true });
    });
  }
});

test("building blocks become editor nodes", () => {
  expect(importedTypes("> [!note]\n> x")).toContain("callout");
  expect(importedTypes("```math\nx\n```")).toEqual(["fence"]);
  expect(importedTypes("a $x^2$ b")).toContain("imath");
  expect(importedTypes('a <time datetime="2026-12-24">x</time> b')).toContain("phxtime");
  expect(importedTypes("a [rot]{color=red} b").filter((t) => t === "text").length).toBe(3);
});

describe("page links and brackets", () => {
  test("[[links]] keep the rich editor and save unchanged", () => {
    for (const md of ["see [[ADR-0004]] and [[proj/page|the page]].", "> [!tip]\n> see [[foo]] and `[[code]]`", "| a | b |\n|---|---|\n| [[x]] | y |"]) {
      expect({ md, ok: lossless(md), out: roundTrip(md) }).toEqual({ md, ok: true, out: md });
    }
    expect(importedTypes("see [[foo]]")).toContain("pagelink");
  });
  test("plain brackets are not saved as entities", () => {
    expect(roundTrip("Ports [1-24] here")).toBe("Ports [1-24] here");
    expect(roundTrip("- [ ] todo\n- [x] done")).toBe("- [ ] todo\n- [x] done");
    // Escaped text that would turn into a link or a task box stays escaped.
    expect(roundTrip("text \\[a\\](b)")).not.toContain("[a](b)");
    expect(roundTrip("- \\[ ] literal")).not.toContain("- [ ]");
  });
});

describe("raw blocks", () => {
  test("only the block the editor cannot show becomes source", () => {
    const md = "# Title\n\nSome **text** with [[a-page]].\n\n<details><summary>More</summary>\n\nHidden\n\n</details>\n\nText[^1] after.\n\n[^1]: A footnote\n\n- a list\n- b";
    expect(lossless(md)).toBe(false);
    const rich = forEditor(md);
    expect(rich).not.toBeNull();
    expect(importedTypes(rich!)).toContain("rawmd");
    expect(importedTypes(rich!)).toContain("heading");
    expect(roundTrip(rich!)).not.toContain("phx-raw");
  });
  test("mdBlocks keeps fences with blank lines together", () => {
    expect(mdBlocks("a\n\n```js\nx\n\ny\n```\n\nb")).toEqual(["a", "```js\nx\n\ny\n```", "b"]);
  });
});

test("an HTML block with blank lines is one raw block", () => {
  expect(mdBlocks("a\n\n<details><summary>x</summary>\n\nHidden\n\n</details>\n\nb")).toEqual(["a", "<details><summary>x</summary>\n\nHidden\n\n</details>", "b"]);
  const rich = forEditor("a\n\n<details><summary>x</summary>\n\nHidden\n\n</details>\n\nb")!;
  expect(importedTypes(rich).filter((t) => t === "rawmd").length).toBe(1);
});
