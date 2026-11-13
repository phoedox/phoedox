import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { de } from "./de";
import { iconNames } from "../icon";

type Core = { generic: string[]; kinds: { id: string; group: string; icon: string; attrs: { name: string; options?: string[] }[]; relations?: { rel: string }[] }[] };

// Core kinds are named by the web app: every kind, group, attribute,
// option and relation needs a translation, and every icon must be bundled.
test("core kinds are translated and their icons bundled", () => {
  const core: Core = JSON.parse(readFileSync(join(import.meta.dir, "../../../resources/kinds/core.json"), "utf8"));
  const keys = new Set(Object.keys(de));
  const need: string[] = core.generic.map((r) => `rel.${r}`).concat("kindgroup.custom");
  for (const k of core.kinds) {
    need.push(`kind.${k.id}`, `kindgroup.${k.group}`);
    expect(iconNames).toContain(k.icon);
    for (const a of k.attrs) {
      need.push(`attr.${a.name}`);
      for (const o of a.options ?? []) if (!/^(cat|postgres|mysql|mariadb|sqlite|mongodb|redis)/.test(o)) need.push(`opt.${o}`);
    }
    for (const r of k.relations ?? []) need.push(`rel.${r.rel}`);
  }
  for (const k of need) expect(keys.has(k) ? k : `missing ${k}`).toBe(k);
});

test("icons for own kinds are bundled", () => {
  const src = readFileSync(join(import.meta.dir, "../../../internal/core/kinds.go"), "utf8");
  const list = src.match(/var KindIcons = \[\]string\{([^}]+)\}/)![1].match(/"(\w+)"/g)!.map((s) => s.slice(1, -1));
  for (const i of list) expect(iconNames).toContain(i);
});
