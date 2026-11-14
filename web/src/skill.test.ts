import { expect, test } from "bun:test";
import { skillName, validSkillName } from "./skill";

test("skill names from titles", () => {
  expect(skillName("Störung: Router neu starten")).toBe("stoerung-router-neu-starten");
  expect(skillName("Café Ops — Runbook")).toBe("cafe-ops-runbook");
  expect(skillName("Claude helper")).toBe("helper");
  expect(skillName("!!!")).toBe("skill");
  expect(skillName("a".repeat(80)).length).toBe(64);
  expect(validSkillName("incident-response")).toBe(true);
  expect(validSkillName("Bad Name")).toBe(false);
  expect(validSkillName("my-claude")).toBe(false);
});
