import { expect, test } from "bun:test";
import { continueTiles, matchProjects, relativeTime } from "./home";

test("relative time", () => {
  const now = Date.parse("2026-11-14T12:00:00Z");
  expect(relativeTime("2026-11-14T10:00:00Z", now, "en")).toBe("2 hours ago");
  expect(relativeTime("2026-11-13T12:00:00Z", now, "de")).toBe("gestern");
  expect(relativeTime("2026-11-14T11:59:50Z", now, "en")).toBe("this minute");
});

test("project search", () => {
  const ps = [{ name: "Phoedox", slug: "phoedox", description: "Entwicklung und Betrieb" }, { name: "Somaro", slug: "somaro", description: "" }];
  expect(matchProjects(ps, "").length).toBe(2);
  expect(matchProjects(ps, "betrieb pho").map((p) => p.slug)).toEqual(["phoedox"]);
  expect(matchProjects(ps, "xyz")).toEqual([]);
});

test("continue tiles", () => {
  const recent = [1, 2, 3, 4].map((i) => ({ kind: "recent" as const, href: `/r${i}`, title: `R${i}`, sub: "", icon: "FileText" }));
  const text = { due: (n: number) => `${n} due`, dueSub: "My tasks", sugg: (n: number) => `${n} suggestions` };
  const tiles = continueTiles(2, [{ project: "a", name: "A", count: 3 }], recent, text);
  expect(tiles.map((x) => x.kind)).toEqual(["due", "suggestions", "recent", "recent"]);
  expect(tiles[1].href).toBe("/p/a/suggestions");
  expect(continueTiles(0, [], [], text)).toEqual([]);
});
