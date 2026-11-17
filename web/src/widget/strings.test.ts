import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { widgetStrings } from "../../scripts/widget-strings";

test("widget strings are generated from the translation tables", () => {
  const root = join(import.meta.dir, "..", "..");
  const have = readFileSync(join(root, "src", "widget", "strings.ts"), "utf8");
  expect(have).toBe(widgetStrings(root)); // run: bun scripts/widget-strings.ts
  expect(have).toContain("fbw.l.open");
});
