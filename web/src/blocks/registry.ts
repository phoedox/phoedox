// Content blocks (ADR-0085): each kind is registered once and then works in
// pages (a fenced block with the kind as language), on whiteboards (an
// element of type "block" with kind and source), in exports and previews.
// Definitions stay free of the DOM, so they also run in tests and in the
// Markdown renderer; interactive views load their components when mounted.

export interface BlockProblem { key: string; params?: Record<string, string | number>; detail?: string }
export interface BlockSize { width: number; height?: number; locale: string }
export interface BlockCtx {
  project: string;
  doc: string;
  /** A key for state per reader (block_states): the block's id or a hash of its source. */
  key: string;
  locale: string;
  /** On a whiteboard (as an activated element), not in a page. */
  board?: boolean;
  /** The block's source text. */
  source?: string;
  /** In the page editor: replaces the block's source (e.g. a marker placed by clicking). */
  edit?(source: string): void;
}

export interface BlockDef<M = any> {
  kind: string;
  /** Lucide icon for menus and whiteboard tools. */
  icon: string;
  /** i18n keys: the name in menus and a hint for writing the source. */
  label: string;
  hint: string;
  /** A source to start from. */
  example: string;
  parse(src: string): { model?: M; problems: BlockProblem[] };
  /** A static picture (with its title and legend) for whiteboards, exports and previews. */
  svg(model: M, size: BlockSize): string;
  /** The interactive view; without it the picture is shown. */
  mount?(host: HTMLElement, model: M, ctx: BlockCtx): Promise<void | (() => void)>;
  /** May go on whiteboards; live: it runs interactively there (e.g. a poll). */
  board?: { w: number; h: number; live?: boolean };
  /** The id a source names itself (state survives edits of its text). */
  idOf?(model: M): string | undefined;
}

const defs = new Map<string, BlockDef>();

export function registerBlock<M>(d: BlockDef<M>) {
  defs.set(d.kind, d as BlockDef);
}

export function blockDef(kind: string): BlockDef | undefined {
  return defs.get(kind);
}

export function blockKinds(): BlockDef[] {
  return [...defs.values()];
}

/** Kinds that may go on whiteboards. */
export function boardKinds(): BlockDef[] {
  return blockKinds().filter((d) => d.board);
}

/** A short stable hash (keys of blocks without an id). */
export function hashText(s: string): string {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return "h" + (h >>> 0).toString(36);
}

/** The state key of a block: its own id, else a hash of its source. */
export function blockKey(d: BlockDef, model: unknown, src: string): string {
  const id = d.idOf?.(model);
  return id && /^[A-Za-z0-9-]{1,40}$/.test(id) ? id : hashText(src.trim());
}
