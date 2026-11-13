import { expect, test } from "bun:test";
import { allowedRels, formatValue, toID, type Model } from "./model";

test("toID", () => {
  expect(toID("Wechsel-Richter Süd")).toBe("wechsel_richter_sued");
  expect(toID("1. Etage")).toBe("etage");
});

test("allowedRels", () => {
  const m: Model = { generic: ["related_to"], kinds: [
    { id: "vm", group: "software", icon: "Monitor", source: "core", count: 0, attrs: [], relations: [{ rel: "runs_on", to: ["server"] }, { rel: "x", to: ["*"] }] },
  ] };
  expect(allowedRels(m, "vm", "server")).toEqual(["related_to", "runs_on", "x"]);
  expect(allowedRels(m, "vm", "room")).toEqual(["related_to", "x"]);
  expect(allowedRels(m, "room", "vm")).toEqual(["related_to"]);
});

test("formatValue", () => {
  expect(formatValue({ name: "h", type: "number", unit: "U" }, 12)).toBe("12 U");
  expect(formatValue(undefined, { a: 1 })).toBe('{"a":1}');
});
