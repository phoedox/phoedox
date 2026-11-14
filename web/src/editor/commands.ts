// What the "/" menu and the toolbar insert (ADR-0031).

import { $createCodeNode } from "@lexical/code";
import { $createHorizontalRuleNode } from "@lexical/extension";
import { INSERT_CHECK_LIST_COMMAND, INSERT_ORDERED_LIST_COMMAND, INSERT_UNORDERED_LIST_COMMAND } from "@lexical/list";
import { $createHeadingNode, $createQuoteNode } from "@lexical/rich-text";
import { $patchStyleText, $setBlocksType } from "@lexical/selection";
import { INSERT_TABLE_COMMAND } from "@lexical/table";
import { $insertNodeToNearestRoot } from "@lexical/utils";
import { $createParagraphNode, $createTextNode, $getSelection, $insertNodes, $isRangeSelection, FORMAT_TEXT_COMMAND, type LexicalEditor, type LexicalNode } from "lexical";
import type { CalloutAttrs, Color, FenceKind, TimeKind } from "../syntax";
import { $createCalloutNode, $createFenceNode, $createInlineMathNode, $createTimeNode, $isCalloutNode, CalloutNode, colorsOfStyle } from "./blocks";

export type SlashGroup = "text" | "blocks" | "embed" | "time" | "format";

export interface SlashItem {
  id: string;
  group: SlashGroup;
  icon: string;
  /** Extra search words (both languages). */
  words: string;
  /** Runs inside the host; the host asks for values where needed. */
  run: string;
}

/** Items of the "/" menu; labels come from i18n ("slash.<id>"). */
export const SLASH_ITEMS: SlashItem[] = [
  { id: "paragraph", group: "text", icon: "Type", words: "text paragraph absatz", run: "block:paragraph" },
  { id: "h1", group: "text", icon: "Heading1", words: "heading überschrift titel h1", run: "block:h1" },
  { id: "h2", group: "text", icon: "Heading2", words: "heading überschrift h2", run: "block:h2" },
  { id: "h3", group: "text", icon: "Heading3", words: "heading überschrift h3", run: "block:h3" },
  { id: "ul", group: "text", icon: "List", words: "bullet liste aufzählung list", run: "list:ul" },
  { id: "ol", group: "text", icon: "ListOrdered", words: "numbered nummeriert liste list", run: "list:ol" },
  { id: "todo", group: "text", icon: "ListTodo", words: "todo checkbox aufgabe task check", run: "list:check" },
  { id: "quote", group: "text", icon: "TextQuote", words: "quote zitat", run: "block:quote" },
  { id: "callout", group: "blocks", icon: "MessageSquareWarning", words: "callout hinweis box notiz note warnung info", run: "callout" },
  { id: "code", group: "blocks", icon: "SquareCode", words: "code codeblock", run: "block:code" },
  { id: "request", group: "blocks", icon: "Send", words: "api request http test anfrage", run: "request" },
  { id: "table", group: "blocks", icon: "Table", words: "table tabelle", run: "table" },
  { id: "divider", group: "blocks", icon: "Minus", words: "divider trennlinie linie hr", run: "divider" },
  { id: "math", group: "blocks", icon: "Sigma", words: "math formel latex katex gleichung", run: "fence:math" },
  { id: "imath", group: "blocks", icon: "Sigma", words: "inline formel latex math", run: "imath" },
  { id: "board", group: "blocks", icon: "SquareKanban", words: "board kanban aufgaben tasks", run: "fence:board" },
  { id: "mermaid", group: "blocks", icon: "Workflow", words: "mermaid diagramm diagram flowchart ablauf sequenz sequence", run: "fence:mermaid" },
  { id: "dbmodel", group: "blocks", icon: "Database", words: "datenbank database schema sql er tabelle table model modell", run: "fence:dbmodel" },
  { id: "status", group: "blocks", icon: "Activity", words: "status dashboard kacheln tiles monitoring überwachung zustand", run: "fence:status" },
  { id: "entity", group: "blocks", icon: "Box", words: "eintrag entry entity karte card komponente component gerät device", run: "fence:entity" },
  { id: "include", group: "blocks", icon: "Copy", words: "include einbinden baustein block wiederverwenden reuse snippet textbaustein", run: "fence:include" },
  { id: "query", group: "blocks", icon: "Table", words: "query abfrage tabelle table liste list einträge entries", run: "fence:query" },
  { id: "embed", group: "embed", icon: "SquarePlay", words: "embed youtube vimeo loom github video einbetten", run: "fence:embed" },
  { id: "iframe", group: "embed", icon: "Frame", words: "iframe webseite seite page", run: "fence:iframe" },
  { id: "file", group: "embed", icon: "Paperclip", words: "file datei anhang upload bild image", run: "file" },
  { id: "date", group: "time", icon: "CalendarDays", words: "date datum tag", run: "time:date" },
  { id: "time", group: "time", icon: "Clock", words: "time uhrzeit zeit", run: "time:time" },
  { id: "datetime", group: "time", icon: "Calendar", words: "datetime datum uhrzeit termin", run: "time:datetime" },
  { id: "countdown", group: "time", icon: "Hourglass", words: "countdown timer frist", run: "time:countdown" },
  { id: "bold", group: "format", icon: "Bold", words: "bold fett", run: "format:bold" },
  { id: "italic", group: "format", icon: "Italic", words: "italic kursiv", run: "format:italic" },
  { id: "underline", group: "format", icon: "Underline", words: "underline unterstrichen", run: "format:underline" },
  { id: "strike", group: "format", icon: "Strikethrough", words: "strikethrough durchgestrichen", run: "format:strikethrough" },
  { id: "color", group: "format", icon: "Palette", words: "color farbe textfarbe markieren highlight hintergrund", run: "color" },
];

/** Removes the typed "/query" before running an item. */
export function $removeSlash(query: string) {
  const sel = $getSelection();
  if (!$isRangeSelection(sel) || !sel.isCollapsed()) return;
  const node = sel.anchor.getNode();
  const off = sel.anchor.offset;
  const text = node.getTextContent();
  const len = query.length + 1;
  if (off >= len && text.slice(off - len, off) === `/${query}`) {
    sel.anchor.set(node.getKey(), off - len, "text");
    sel.focus.set(node.getKey(), off, "text");
    sel.removeText();
  }
}

export function $setBlock(kind: string) {
  const sel = $getSelection();
  if (!$isRangeSelection(sel)) return;
  $setBlocksType(sel, () => kind === "quote" ? $createQuoteNode() : kind === "code" ? $createCodeNode() : kind === "paragraph" ? $createParagraphNode()
    : $createHeadingNode(kind as "h1" | "h2" | "h3"));
}

export function runList(editor: LexicalEditor, kind: string) {
  editor.dispatchCommand(kind === "ol" ? INSERT_ORDERED_LIST_COMMAND : kind === "check" ? INSERT_CHECK_LIST_COMMAND : INSERT_UNORDERED_LIST_COMMAND, undefined);
}

export function runFormat(editor: LexicalEditor, f: "bold" | "italic" | "underline" | "strikethrough" | "code") {
  editor.dispatchCommand(FORMAT_TEXT_COMMAND, f);
}

export function runTable(editor: LexicalEditor) {
  editor.dispatchCommand(INSERT_TABLE_COMMAND, { rows: "3", columns: "3", includeHeaders: { rows: true, columns: false } });
}

/** Replaces an empty current paragraph by node, or adds node after the current block. */
function $insertBlock(node: LexicalNode) {
  $insertNodeToNearestRoot(node);
}

export function $insertDivider() {
  $insertBlock($createHorizontalRuleNode());
}

export function $insertCallout(a: CalloutAttrs) {
  const c = $createCalloutNode(a);
  const p = $createParagraphNode();
  c.append(p);
  $insertBlock(c);
  p.select();
}

export function $insertRequest() {
  const code = $createCodeNode("request");
  code.append($createTextNode("GET {{base}}/"));
  $insertBlock(code);
  code.selectEnd();
}

export function $insertFence(kind: FenceKind, source: string) {
  const n = $createFenceNode(kind, source);
  $insertBlock(n);
  const after = n.getNextSibling();
  if (after) after.selectStart();
  else {
    const p = $createParagraphNode();
    n.insertAfter(p);
    p.select();
  }
}

export function $insertTime(datetime: string, kind: TimeKind, text: string) {
  $insertNodes([$createTimeNode(datetime, kind, text), $createTextNode(" ")]);
}

export function $insertInlineMath(tex: string) {
  $insertNodes([$createInlineMathNode(tex)]);
}

/** Text color and highlight for the selection; undefined keeps, null removes. */
export function $applyColors(color: Color | null | undefined, bg: Color | null | undefined) {
  const sel = $getSelection();
  if (!$isRangeSelection(sel)) return;
  const patch: Record<string, string | null> = {};
  if (color !== undefined) patch.color = color ? `var(--tc-${color})` : null;
  if (bg !== undefined) patch["background-color"] = bg ? `var(--tb-${bg})` : null;
  $patchStyleText(sel, patch);
}

export function $currentColors(): { color?: Color; bg?: Color } {
  const sel = $getSelection();
  if (!$isRangeSelection(sel)) return {};
  const n = sel.anchor.getNode();
  return "getStyle" in n ? colorsOfStyle((n as unknown as { getStyle(): string }).getStyle()) : {};
}

/** The callout around the selection, if any. */
export function $selectedCallout(): CalloutNode | null {
  const sel = $getSelection();
  if (!$isRangeSelection(sel)) return null;
  let n: LexicalNode | null = sel.anchor.getNode();
  while (n) {
    if ($isCalloutNode(n)) return n;
    n = n.getParent();
  }
  return null;
}
