import { expect, test } from "bun:test";
import { matcher } from "./autolinks";

test("auto-links find references, sections and own rules", () => {
  const find = matcher(
    { refs: { "ADR-0012": { href: "/p/x/d/db", title: "Database" }, "D-640": { href: "/p/x/d/net", title: "Network" } },
      rules: [{ pattern: "JIRA-(\\d+)", to: "url", url: "https://jira.example.org/browse/$1", note: "Jira" }, { pattern: "DEC-\\d+", to: "ref" }] },
    (n) => (n === "6.4" ? "6-4-backups" : undefined));
  const text = "See ADR-0012 and D-640, §6.4, §9.9, JIRA-77 and XADR-0012 or ADR-00123.";
  const hits = find(text);
  expect(hits.map((h) => text.slice(h.start, h.end))).toEqual(["ADR-0012", "D-640", "§6.4", "JIRA-77"]);
  expect(hits[2].href).toBe("#6-4-backups");
  expect(hits[3].href).toBe("https://jira.example.org/browse/77");
  expect(hits[3].title).toBe("Jira");
});

test("own rules never produce other link schemes", () => {
  const find = matcher({ refs: {}, rules: [{ pattern: "X\\d", to: "url", url: "javascript:$0" }] }, () => undefined);
  expect(find("X1")).toEqual([]);
});
