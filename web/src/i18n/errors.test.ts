import { describe, expect, test } from "bun:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { de } from "./de";

// Every validation key the Go server can return must be translated.
describe("error keys", () => {
  test("all server validation keys exist in the translations", () => {
    const root = join(import.meta.dir, "../../../internal");
    const keys = new Set<string>();
    const files = readdirSync(root, { recursive: true, encoding: "utf8" }).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"));
    for (const f of files) {
      for (const m of readFileSync(join(root, f), "utf8").matchAll(/(?:invalid|Invalid)\("([a-z_.]+)"|Key: "([a-z_.]+)"/g)) { const k = m[1] ?? m[2]; if (!k.endsWith(".")) keys.add(k); }
    }
    expect(keys.size).toBeGreaterThan(10);
    for (const k of keys) expect(Object.keys(de)).toContain(k);
    for (const code of ["unauthorized", "bad_login", "rate_limited", "forbidden", "not_found", "conflict", "csrf", "internal"]) {
      expect(Object.keys(de)).toContain(`error.${code}`);
    }
  });
});
