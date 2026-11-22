/** Which cards of a board show their subtasks: a board-wide default plus a
 * choice per card. Kept per board in this browser. */
export interface CardSubtasks {
  all: boolean;
  /** Per card: true = shown, false = hidden, regardless of the default. */
  cards: Record<string, boolean>;
}

/** Reads the stored value; "1"/"0" is the older board-wide switch. */
export function parseCardSubtasks(raw: string | null): CardSubtasks {
  if (raw === "1" || raw === "0") return { all: raw === "1", cards: {} };
  try {
    const v = JSON.parse(raw ?? "");
    if (v && typeof v === "object") {
      const cards: Record<string, boolean> = {};
      for (const [k, b] of Object.entries(v.cards ?? {})) if (typeof b === "boolean") cards[k] = b;
      return { all: v.all === true, cards };
    }
  } catch { /* nothing stored yet */ }
  return { all: false, cards: {} };
}

export function subtasksShown(s: CardSubtasks, card: string): boolean {
  return s.cards[card] ?? s.all;
}

/** Switches one card; a choice equal to the default is dropped. */
export function toggleCard(s: CardSubtasks, card: string): CardSubtasks {
  const cards = { ...s.cards };
  const show = !subtasksShown(s, card);
  if (show === s.all) delete cards[card];
  else cards[card] = show;
  return { all: s.all, cards };
}

/** The board-wide switch: all cards follow it again. */
export function toggleAll(s: CardSubtasks): CardSubtasks {
  return { all: !s.all, cards: {} };
}
