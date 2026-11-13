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
