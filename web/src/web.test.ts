import { describe, expect, test } from "bun:test";
import { contrast, projectColors } from "./color";
import { linkMentions } from "./md";

describe("mentions", () => {
  test("links @slug and @project/slug, but not e-mail addresses", () => {
    const out = linkMentions("Der @router-flur und @homelab/pve-01, Mail an a@b.de", "lindenstrasse");
    expect(out).toContain("[@router-flur](/p/lindenstrasse/e/router-flur");
    expect(out).toContain("[@homelab/pve-01](/p/homelab/e/pve-01");
    expect(out).toContain("a@b.de");
    expect(out).not.toContain("/e/b");
  });
});

describe("project colours", () => {
  test("light and dark variants reach 4.5:1", () => {
    for (const c of ["#F26A1B", "#FFEB3B", "#0B1A4A", "#7C3AED"]) {
      const { light, dark } = projectColors(c);
      const hx = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
      expect(contrast(hx(light), [255, 255, 255])).toBeGreaterThanOrEqual(4.5);
      expect(contrast(hx(dark), [28, 24, 21])).toBeGreaterThanOrEqual(4.5);
    }
  });
});
