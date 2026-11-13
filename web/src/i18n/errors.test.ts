import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { de } from "./de";

// Every validation key the Go server can return must be translated.
describe("error keys", () => {
  test("all server validation keys exist in the translations", () => {
    const dir = join(import.meta.dir, "../../../internal/core");
    const keys = new Set<string>();
    for (const f of readdirSync(dir).filter((f) => f.endsWith(".go"))) {
      for (const m of readFileSync(join(dir, f), "utf8").matchAll(/invalid\("([a-z_.]+)"/g)) keys.add(m[1]);
    }
    expect(keys.size).toBeGreaterThan(10);
    for (const k of keys) expect(Object.keys(de)).toContain(k);
    for (const code of ["unauthorized", "bad_login", "rate_limited", "forbidden", "not_found", "conflict", "csrf", "internal"]) {
      expect(Object.keys(de)).toContain(`error.${code}`);
    }
  });
});
