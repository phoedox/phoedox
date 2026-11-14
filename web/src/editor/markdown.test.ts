import { describe, expect, test } from "bun:test";
import { importedTypes, lossless, roundTrip } from "./markdown";

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
