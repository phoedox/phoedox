// Markdown ⇄ Lexical for the live editor, including GFM tables, horizontal
// rules and task lists, plus a check that a page survives the round trip.
// Pages that would lose content never open in the rich editor.

import { $createHorizontalRuleNode, $isHorizontalRuleNode, HorizontalRuleNode } from "@lexical/extension";
import {
  $convertFromMarkdownString, $convertToMarkdownString, CHECK_LIST, type ElementTransformer, TRANSFORMERS as BASE, type Transformer,
} from "@lexical/markdown";
import {
  $createTableCellNode, $createTableNode, $createTableRowNode, $isTableCellNode, $isTableNode, $isTableRowNode,
  TableCellHeaderStates, TableCellNode, TableNode, TableRowNode,
} from "@lexical/table";
import { CodeHighlightNode, CodeNode } from "@lexical/code";
import { AutoLinkNode, LinkNode } from "@lexical/link";
import { ListItemNode, ListNode } from "@lexical/list";
import { HeadingNode, QuoteNode } from "@lexical/rich-text";
import { $isParagraphNode, $isTextNode, createEditor, type ElementNode, type LexicalNode } from "lexical";
import { markdownHTML } from "../md";
import { BLOCK_NODES, CALLOUT, COLOR_MATCH, FENCE, IMATH_MATCH, PAGELINK_MATCH, RAW, RAW_CLOSE, RAW_OPEN, TIME_MATCH, UNDERLINE_FORMAT, setInlineTransformers, setInnerTransformers } from "./blocks";
import { IMAGE, ImageNode } from "./image";

export const NODES = [HeadingNode, QuoteNode, ListNode, ListItemNode, LinkNode, AutoLinkNode, CodeNode, CodeHighlightNode, ImageNode,
  TableNode, TableRowNode, TableCellNode, HorizontalRuleNode, ...BLOCK_NODES];

const HR: ElementTransformer = {
  dependencies: [HorizontalRuleNode],
  export: (node) => ($isHorizontalRuleNode(node) ? "---" : null),
  regExp: /^(---|\*\*\*|___)\s?$/,
  replace: (parent, _children, _match, isImport) => {
    const hr = $createHorizontalRuleNode();
    if (isImport || parent.getNextSibling() != null) parent.replace(hr);
    else parent.insertBefore(hr);
    hr.selectNext();
  },
  type: "element",
};

// A table row: starts and ends with a pipe. Escaped pipes (\|) stay in cells.
const ROW = /^\|(.+)\|\s*$/;
const DIVIDER = /^\|(\s*:?-+:?\s*\|)+\s*$/;

function splitRow(line: string): string[] | null {
  const m = line.match(ROW);
  if (!m) return null;
  return m[1].split(/(?<!\\)\|/).map((c) => c.trim());
}

function $cell(text: string, header: boolean) {
  const cell = $createTableCellNode(header ? TableCellHeaderStates.ROW : TableCellHeaderStates.NO_STATUS);
  $convertFromMarkdownString(text.replace(/\\\|/g, "|").replace(/<br\s*\/?>/gi, "\n"), CELL_TRANSFORMERS, cell);
  return cell;
}

function cellText(cell: ElementNode): string {
  return $convertToMarkdownString(CELL_TRANSFORMERS, cell).replace(/\n+/g, "<br>").replace(/\|/g, "\\|").trim();
}

const TABLE: ElementTransformer = {
  dependencies: [TableNode, TableRowNode, TableCellNode],
  export: (node) => {
    if (!$isTableNode(node)) return null;
    const lines: string[] = [];
    node.getChildren().forEach((row, i) => {
      if (!$isTableRowNode(row)) return;
      const cells = row.getChildren().filter($isTableCellNode).map(cellText);
      lines.push(`| ${cells.join(" | ")} |`);
      // GFM needs a delimiter row after the first row (the header).
      if (i === 0) lines.push(`|${cells.map(() => "---").join("|")}|`);
    });
    return lines.join("\n");
  },
  regExp: ROW,
  replace: (parent, _children, match) => {
    const prev = parent.getPreviousSibling();
    if (DIVIDER.test(match[0])) {
      // The delimiter row: the previous table's single row is the header.
      if ($isTableNode(prev) && prev.getChildrenSize() === 1) {
        parent.remove();
        return;
      }
      return false;
    }
    const cells = splitRow(match[0]);
    if (!cells) return false;
    if ($isTableNode(prev)) {
      const width = (prev.getFirstChild() as ElementNode | null)?.getChildrenSize() ?? cells.length;
      const row = $createTableRowNode();
      for (let i = 0; i < width; i++) row.append($cell(cells[i] ?? "", false));
      prev.append(row);
      parent.remove();
      return;
    }
    const table = $createTableNode();
    const row = $createTableRowNode();
    for (const c of cells) row.append($cell(c, true));
    table.append(row);
    parent.replace(table);
  },
  type: "element",
};

/** Inline-only transformers (table cells, colored text). Colors before links: [a]{color=x} is no link. */
const CELL_TRANSFORMERS: Transformer[] = [PAGELINK_MATCH, UNDERLINE_FORMAT, TIME_MATCH, IMATH_MATCH, COLOR_MATCH, IMAGE,
  ...BASE.filter((t) => t.type === "text-format" || t.type === "text-match")];

/** Images before links: ![a](b) must not become a link; our fences and callouts before code and quotes. */
export const TRANSFORMERS: Transformer[] = [RAW, CALLOUT, FENCE, TABLE, HR, CHECK_LIST, PAGELINK_MATCH, UNDERLINE_FORMAT, TIME_MATCH, IMATH_MATCH, COLOR_MATCH, IMAGE, ...BASE];
setInnerTransformers(TRANSFORMERS);
setInlineTransformers(CELL_TRANSFORMERS);

/**
 * Lexical writes literal brackets as &#91; and &#93;. Turn them back into
 * brackets where that cannot change the meaning: not before "(", "[", "{" or
 * ":" (links, references, colours) and not as a task box after a list marker.
 */
export function tidyBrackets(md: string): string {
  return md.replace(/&#91;((?:(?!&#9[13];)[^\n])*)&#93;(?![([{:])/g, (all, inner: string, at: number, src: string) => {
    const line = src.slice(src.lastIndexOf("\n", at - 1) + 1, at);
    if (/^\s*(?:[-*+]|\d+[.)])\s+$/.test(line) && /^[ xX]?$/.test(inner)) return all;
    if (line.endsWith("!") || line.endsWith("\\")) return all;
    return `[${inner}]`;
  });
}

/** Converts Markdown to Markdown through the editor (as a save would). */
export function roundTrip(md: string): string {
  const editor = createEditor({ namespace: "check", nodes: NODES, onError: (e) => { throw e; } });
  let out = "";
  editor.update(() => $convertFromMarkdownString(md, TRANSFORMERS), { discrete: true });
  editor.getEditorState().read(() => { out = tidyBrackets($convertToMarkdownString(TRANSFORMERS)); });
  return out;
}

/** Compares Markdown by what it shows (rendered HTML as in pages, whitespace ignored). */
function shows(md: string): string {
  const html = markdownHTML(md, "p");
  return html.replace(/>\s+</g, "><").replace(/\s+/g, " ").replace(/ align="left"/g, "").trim();
}

/**
 * Tells whether the editor shows and saves a page without losing anything.
 * Returns false for content it cannot represent (HTML blocks, footnotes,
 * aligned table columns, …); such pages open in the Markdown source editor.
 */
export function lossless(md: string): boolean {
  if (!md.trim()) return true;
  try {
    return shows(roundTrip(md)) === shows(md);
  } catch {
    return false;
  }
}

/** Splits Markdown into top-level blocks at blank lines outside code fences. */
export function mdBlocks(md: string): string[] {
  const out: string[] = [];
  let cur: string[] = [], fence = "";
  for (const line of md.split("\n")) {
    const f = /^\s*(`{3,}|~{3,})/.exec(line);
    if (f) {
      if (!fence) fence = f[1];
      else if (line.trim().startsWith(fence) && line.trim().replace(/[`~]/g, "") === "") fence = "";
    }
    if (!fence && !line.trim() && !f) {
      if (cur.length) out.push(cur.join("\n"));
      cur = [];
      continue;
    }
    cur.push(line);
  }
  if (cur.length) out.push(cur.join("\n"));
  // An HTML block with blank lines inside (<details> …) stays one block.
  const merged: string[] = [];
  for (let i = 0; i < out.length; i++) {
    const tag = /^<([a-z][\w-]*)[\s>]/i.exec(out[i])?.[1];
    let block = out[i];
    if (tag && !new RegExp(`</${tag}>`, "i").test(block)) {
      let j = i + 1;
      while (j < out.length && !new RegExp(`</${tag}>`, "i").test(out[j])) j++;
      if (j < out.length) {
        block = out.slice(i, j + 1).join("\n\n");
        i = j;
      }
    }
    merged.push(block);
  }
  return merged;
}

/**
 * Markdown ready for the rich editor: blocks it cannot represent are marked
 * as raw blocks (shown as in the page, edited as source). Returns null when
 * the page as a whole would still change; it then opens as source.
 */
export function forEditor(md: string): string | null {
  if (lossless(md)) return md;
  const marked = mdBlocks(md).map((b) => (lossless(b) ? b : `${RAW_OPEN}\n${b}\n${RAW_CLOSE}`)).join("\n\n");
  try {
    return shows(roundTrip(marked)) === shows(md) ? marked : null;
  } catch {
    return null;
  }
}

/** For tests: the node types after import. */
export function importedTypes(md: string): string[] {
  const editor = createEditor({ namespace: "check", nodes: NODES, onError: (e) => { throw e; } });
  const types: string[] = [];
  editor.update(() => $convertFromMarkdownString(md, TRANSFORMERS), { discrete: true });
  editor.getEditorState().read(() => {
    const walk = (n: LexicalNode) => {
      types.push(n.getType());
      if ("getChildren" in n) (n as ElementNode).getChildren().forEach(walk);
    };
    walk(editor.getEditorState()._nodeMap.get("root")!);
  });
  return types.filter((t) => t !== "root");
}

export { $isParagraphNode, $isTextNode };
