import { describe, expect, test } from "bun:test";
import { de } from "./de";
import { en } from "./en";
import { detectLocale, t } from "./index";

describe("i18n", () => {
  test("every German key has an English translation", () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(de).sort());
  });
  test("detects locale with fallback to German", () => {
    expect(detectLocale(["en-US", "de"])).toBe("en");
    expect(detectLocale(["fr-FR"])).toBe("de");
  });
  test("fills placeholders", () => {
    expect(t("app.status", { status: "ok", version: "1.0" }, "en")).toBe("Server: ok, version 1.0");
  });
});

test("singular texts for a count of one", () => {
  expect(t("table.count", { n: 1 }, "de")).toBe("1 Eintrag");
  expect(t("table.count", { n: 2 }, "de")).toBe("2 Einträge");
  expect(t("project.documents", { count: 1 }, "en")).toBe("1 document");
  expect(t("project.documents", { count: 0 }, "en")).toBe("0 documents");
});
