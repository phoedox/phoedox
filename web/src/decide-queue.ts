import type { Decision } from "./api";

/** URL parameter that turns a decision page into a step of the queue. */
export const decideQueueParam = "queue";
export const decideQueueValue = "decide";

/** Decisions that wait for a person's choice, oldest first: proposed, with
 * options, and not answered with "nothing fits" (those wait for new options). */
export function decideQueue(all: Decision[]): Decision[] {
  return all.filter((d) => d.status === "proposed" && (d.options ?? 0) > 0 && !d.needsOptions).sort((a, b) => a.number - b.number);
}

/** Where the current decision (number n) stands in the queue: its position (0 when it
 * is no longer in it), the previous one and the next one. The next one wraps
 * around to decisions skipped earlier, so nothing is left behind. */
export function queueStep(queue: Decision[], n: number) {
  const i = queue.findIndex((d) => d.number === n);
  const later = queue.filter((d) => d.number > n);
  const earlier = queue.filter((d) => d.number < n);
  return {
    pos: i + 1,
    total: queue.length,
    prev: earlier.at(-1),
    next: later[0] ?? earlier[0],
  };
}

/** Link to a decision as a step of the queue. */
export function queueHref(project: string, d: Decision): string {
  return `/p/${project}/d/${d.id}?${decideQueueParam}=${decideQueueValue}`;
}
