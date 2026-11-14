import { expect, test } from "bun:test";
import { LIST_KINDS, deliver, onProjectChange } from "./project-live";

test("list kinds", () => {
  for (const k of ["document.create", "document.delete", "document.draft_publish", "decision.link", "suggestion.create", "project.update"]) expect(LIST_KINDS.test(k)).toBe(true);
  for (const k of ["document.draft_write", "entity.upsert", "task.update", "file.upload"]) expect(LIST_KINDS.test(k)).toBe(false);
});

test("changes are batched per subscriber and filtered by kind", async () => {
  const got: string[][] = [];
  const stop = onProjectChange(/^entity\./, (cs) => got.push(cs.map((c) => c.subject)), 10);
  deliver({ kind: "entity.upsert", subject: "a", actor: "u" });
  deliver({ kind: "document.create", subject: "d", actor: "u" });
  deliver({ kind: "entity.delete", subject: "b", actor: "u" });
  await new Promise((r) => setTimeout(r, 30));
  expect(got).toEqual([["a", "b"]]);
  stop();
  deliver({ kind: "entity.upsert", subject: "c", actor: "u" });
  await new Promise((r) => setTimeout(r, 30));
  expect(got.length).toBe(1);
});
