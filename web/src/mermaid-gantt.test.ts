import { describe, expect, test } from "bun:test";
import { GANTT_EXAMPLE, ganttIds, isGantt, newGanttId, parseGantt, serializeGantt } from "./mermaid-gantt";

describe("mermaid gantt", () => {
  test("recognises gantt plans", () => {
    expect(isGantt(GANTT_EXAMPLE)).toBe(true);
    expect(isGantt("%% plan\ngantt\n  title x")).toBe(true);
    expect(isGantt("flowchart LR\n  A --> B")).toBe(false);
  });

  test("parses header, sections and tasks by Mermaid's rules", () => {
    const p = parseGantt(GANTT_EXAMPLE);
    expect(p.title).toBe("Move to the new NAS");
    expect(p.dateFormat).toBe("YYYY-MM-DD");
    expect(p.excludes).toBe("weekends");
    expect(p.sections.map((s) => s.name)).toEqual(["Preparation", "Move"]);
    expect(p.sections[0].tasks[0]).toEqual({ name: "Order disks", tags: ["done"], id: "disks", start: "2026-12-01", end: "3d" });
    expect(p.sections[1].tasks[1]).toEqual({ name: "Switch shares", tags: [], start: "after copy", end: "1d" });
    expect(p.sections[1].tasks[2]).toEqual({ name: "Old NAS off", tags: ["milestone"], start: "after copy", end: "0d" });
    expect(parseGantt("gantt\n  A :5d").sections[0].tasks[0]).toEqual({ name: "A", tags: [], end: "5d" });
    expect(parseGantt("gantt\n  A :crit, active, a1, 2026-01-01, 2026-01-09").sections[0].tasks[0])
      .toEqual({ name: "A", tags: ["crit", "active"], id: "a1", start: "2026-01-01", end: "2026-01-09" });
    expect(ganttIds(p)).toEqual(["disks", "backup", "copy"]);
    expect(newGanttId(p)).toBe("t1");
  });

  test("writes it back so that it parses the same", () => {
    const p = parseGantt(GANTT_EXAMPLE);
    const again = parseGantt(serializeGantt(p));
    expect(again).toEqual(p);
    const src = serializeGantt(p);
    expect(src.startsWith("gantt\n    title Move to the new NAS")).toBe(true);
    expect(src).toContain("    section Move");
    expect(src).toMatch(/Copy data\s+:crit, copy, after backup, 4d/);
  });

  test("keeps comments; a task with an id but no start follows the one before", () => {
    const p = parseGantt("gantt\n  %% the plan\n  dateFormat YYYY-MM-DD\n  A :a1, 2026-01-05, 3d\n  B :b1, after a1, 2d");
    expect(p.extra).toEqual(["%% the plan"]);
    p.sections[0].tasks[1].start = "";
    expect(serializeGantt(p)).toMatch(/B\s+:b1, after a1, 2d/);
    p.sections[0].tasks[0].start = "";
    const src = serializeGantt(p);
    expect(src).toMatch(/A\s+:3d/); // first task: no start possible with an id
    expect(src).toMatch(/B\s+:2d/);
  });
});
