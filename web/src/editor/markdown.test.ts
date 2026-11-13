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
