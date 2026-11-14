import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { render } from "./build";

const template = readFileSync(new URL("./template.html", import.meta.url), "utf8");
const load = (l: string) => JSON.parse(readFileSync(new URL(`./i18n/${l}.json`, import.meta.url), "utf8"));

describe("website", () => {
  const de = load("de"), en = load("en");
  test("both languages have the same texts", () => {
    expect(Object.keys(de).sort()).toEqual(Object.keys(en).sort());
    expect(Object.keys(de.net.nodes).sort()).toEqual(Object.keys(en.net.nodes).sort());
    expect(de.net.questions.length).toBe(en.net.questions.length);
  });
  test("every placeholder is filled", () => {
    for (const d of [de, en]) {
      const html = render(template, d);
      expect(html).not.toContain("[[");
      expect(html).toContain(`<html lang="${d.lang}">`);
    }
  });
  test("the current language is marked", () => {
    const html = render(template, de);
    expect(html).toContain('hreflang="de" lang="de" aria-current="true"');
    expect(html).not.toContain('hreflang="en" lang="en" aria-current');
  });
});
