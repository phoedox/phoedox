import { expect, test } from "bun:test";
import { windowTitle } from "./window-title";

test("window title names the open project", () => {
  expect(windowTitle()).toBe("Phoedox");
  expect(windowTitle("Smarte Einkaufsliste")).toBe("Smarte Einkaufsliste · Phoedox");
  expect(windowTitle("Docs", "Acme")).toBe("Docs · Acme");
  expect(windowTitle(undefined, "Acme")).toBe("Acme");
});
