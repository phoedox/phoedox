import { describe, expect, test } from "bun:test";
import type { Decision } from "./api";
import { decideQueue, queueHref, queueStep } from "./decide-queue";

const dec = (number: number, extra: Partial<Decision> = {}) =>
  ({ id: `id${number}`, slug: `adr-${number}`, number, ref: `ADR-${number}`, title: `D${number}`, status: "proposed", options: 2, ...extra }) as Decision;

describe("decide queue", () => {
  const all = [
    dec(5),
    dec(1, { status: "accepted" }),
    dec(3),
    dec(4, { needsOptions: true }),
    dec(2, { options: 0 }),
    dec(7),
  ];

  test("only proposed decisions with options that wait for a choice, oldest first", () => {
    expect(decideQueue(all).map((d) => d.number)).toEqual([3, 5, 7]);
  });

  test("steps: position, previous, next", () => {
    const q = decideQueue(all);
    expect(queueStep(q, q[0].number)).toMatchObject({ pos: 1, total: 3, prev: undefined, next: q[1] });
    expect(queueStep(q, q[1].number)).toMatchObject({ pos: 2, prev: q[0], next: q[2] });
  });

  test("after the last one the next wraps around to skipped ones", () => {
    const q = decideQueue(all);
    expect(queueStep(q, q[2].number).next).toBe(q[0]);
  });

  test("a decided page is out of the queue and continues with the next one", () => {
    const q = decideQueue(all);
    const s = queueStep(q, 6);
    expect(s.pos).toBe(0);
    expect(s.next?.number).toBe(7);
    expect(s.prev?.number).toBe(5);
  });

  test("the last decision decided: nothing left", () => {
    const q = decideQueue([dec(3)]);
    expect(queueStep(q.filter((d) => d.number !== 3), 3).next).toBeUndefined();
  });

  test("links", () => {
    expect(queueHref("p", dec(3))).toBe("/p/p/d/id3?queue=decide");
  });
});
