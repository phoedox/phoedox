// A code editor (CodeMirror 6) with colours per language from the theme
// tokens, line numbers, bracket matching, search, and formatting with
// Prettier on a shortcut (default Ctrl+Alt+L, changeable under keyboard
// shortcuts). Loaded only where code is edited: apps and block sources.
import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { css as cssLang } from "@codemirror/lang-css";
import { html as htmlLang } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { HighlightStyle, bracketMatching, foldGutter, indentOnInput, syntaxHighlighting } from "@codemirror/language";
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import { EditorView, drawSelection, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers, placeholder } from "@codemirror/view";
import { tags as tg } from "@lezer/highlight";
import { LitElement, css } from "lit";
import { formatCode, formattable, langOf, type CodeLang } from "../code-format";
import { t } from "../i18n";
import { comboFor, comboOf } from "../shortcuts";
import { toast } from "./toasts";

/** Colours of code: the theme's palette tokens, so light, dark and themes fit. */
const highlight = HighlightStyle.define([
  { tag: [tg.keyword, tg.controlKeyword, tg.moduleKeyword, tg.operatorKeyword, tg.definitionKeyword, tg.modifier], color: "var(--tc-purple)" },
  { tag: [tg.string, tg.special(tg.string), tg.regexp, tg.character], color: "var(--tc-green)" },
  { tag: [tg.number, tg.bool, tg.null, tg.atom, tg.unit], color: "var(--tc-orange)" },
  { tag: [tg.comment, tg.lineComment, tg.blockComment, tg.docComment], color: "var(--muted)", fontStyle: "italic" },
  { tag: [tg.tagName, tg.angleBracket], color: "var(--tc-red)" },
  { tag: [tg.attributeName, tg.propertyName], color: "var(--tc-blue)" },
  { tag: [tg.attributeValue], color: "var(--tc-green)" },
  { tag: [tg.typeName, tg.className, tg.namespace], color: "var(--tc-yellow)" },
  { tag: [tg.function(tg.variableName), tg.function(tg.propertyName)], color: "var(--tc-blue)" },
  { tag: [tg.variableName, tg.labelName], color: "var(--fg)" },
  { tag: [tg.definition(tg.variableName)], color: "var(--tc-pink)" },
  { tag: [tg.heading], color: "var(--tc-red)", fontWeight: "700" },
  { tag: [tg.strong], fontWeight: "700" },
  { tag: [tg.emphasis], fontStyle: "italic" },
  { tag: [tg.link, tg.url], color: "var(--tc-blue)", textDecoration: "underline" },
  { tag: [tg.meta, tg.processingInstruction, tg.documentMeta], color: "var(--muted)" },
  { tag: [tg.invalid], color: "var(--bad)" },
]);

const theme = EditorView.theme({
  "&": { height: "100%", color: "var(--fg)", backgroundColor: "var(--surface)", fontSize: "13px" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "1.6" },
  ".cm-content": { caretColor: "var(--fg)", padding: "10px 0" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--fg)" },
  ".cm-gutters": { backgroundColor: "var(--surface)", color: "var(--muted)", border: "none" },
  ".cm-activeLine": { backgroundColor: "color-mix(in srgb, var(--accent-soft) 45%, transparent)" },
  ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--fg)" },
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "var(--accent-soft)" },
  ".cm-selectionMatch": { backgroundColor: "color-mix(in srgb, var(--accent-soft) 70%, transparent)" },
  "&.cm-focused .cm-matchingBracket": { backgroundColor: "var(--accent-soft)", outline: "1px solid var(--accent)" },
  ".cm-panels": { backgroundColor: "var(--surface2)", color: "var(--fg)" },
  ".cm-tooltip": { backgroundColor: "var(--surface)", border: "1px solid var(--line)", color: "var(--fg)" },
  ".cm-tooltip-autocomplete ul li[aria-selected]": { backgroundColor: "var(--accent-soft)", color: "var(--fg)" },
  ".cm-placeholder": { color: "var(--muted)" },
});

function language(lang: CodeLang): Extension {
  switch (lang) {
    case "html": return htmlLang();
    case "css": return cssLang();
    case "js": return javascript();
    case "jsx": return javascript({ jsx: true });
    case "ts": return javascript({ typescript: true });
    case "tsx": return javascript({ jsx: true, typescript: true });
    case "json": return json();
    case "md": return markdown();
    default: return [];
  }
}

/** <phx-code-editor .value=… lang="index.html"> – fires "input" (and keeps .value) on every change. */
export class PhxCodeEditor extends LitElement {
  static properties = { value: {}, lang: {}, readonly: { type: Boolean }, label: {}, placeholder: {} };
  static styles = css`
    :host { display: block; position: relative; min-height: 120px; height: 100%; overflow: hidden; }
    .cm-editor { height: 100%; }
  `;
  value = "";
  /** A language (html, css, js, jsx, ts, tsx, json, md) or a file name. */
  lang = "text";
  readonly = false;
  label = "";
  placeholder = "";
  private view?: EditorView;
  private langConf = new Compartment();
  private roConf = new Compartment();
  private formatting = false;

  private get codeLang(): CodeLang {
    return langOf(this.lang);
  }

  firstUpdated() {
    this.view = new EditorView({
      root: this.shadowRoot!,
      parent: this.shadowRoot!,
      state: this.newState(this.value),
    });
  }

  private newState(doc: string) {
    return EditorState.create({
      doc,
      extensions: [
        lineNumbers(), highlightActiveLineGutter(), foldGutter(), history(), drawSelection(), indentOnInput(),
        syntaxHighlighting(highlight), bracketMatching(), closeBrackets(), autocompletion(), highlightActiveLine(), highlightSelectionMatches(),
        EditorState.tabSize.of(2),
        theme,
        placeholder(this.placeholder),
        this.langConf.of(language(this.codeLang)),
        this.roConf.of([EditorState.readOnly.of(this.readonly), EditorView.editable.of(!this.readonly)]),
        EditorView.contentAttributes.of({ "aria-label": this.label || this.lang, spellcheck: "false" }),
        EditorView.domEventHandlers({ keydown: (e) => this.onKey(e) }),
        keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...completionKeymap, indentWithTab]),
        EditorView.updateListener.of((u) => {
          if (!u.docChanged) return;
          this.value = u.state.doc.toString();
          this.dispatchEvent(new Event("input", { bubbles: true, composed: true }));
        }),
      ],
    });
  }

  updated(changed: Map<string, unknown>) {
    const v = this.view;
    if (!v) return;
    if (changed.has("lang") && changed.get("lang") !== undefined) {
      // Another file: a fresh state (own undo history) in its language.
      v.setState(this.newState(this.value));
      return;
    }
    if (changed.has("readonly")) v.dispatch({ effects: this.roConf.reconfigure([EditorState.readOnly.of(this.readonly), EditorView.editable.of(!this.readonly)]) });
    if (changed.has("value") && this.value !== v.state.doc.toString()) {
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: this.value } });
    }
  }

  /** The format shortcut (Ctrl+Alt+L unless changed). */
  private onKey(e: KeyboardEvent): boolean {
    const combo = comboFor("formatCode");
    if (combo && comboOf(e) === combo) {
      e.preventDefault();
      e.stopPropagation();
      void this.format();
      return true;
    }
    return false;
  }

  /** Whether this language can be formatted. */
  get canFormat() {
    return formattable(this.codeLang) && !this.readonly;
  }

  /** Formats the whole text with Prettier; a syntax error is shown and nothing changes. */
  async format() {
    const v = this.view;
    if (!v || !this.canFormat || this.formatting) return;
    this.formatting = true;
    const before = v.state.doc.toString();
    try {
      const after = await formatCode(before, this.codeLang);
      if (after !== before && v.state.doc.toString() === before) {
        const line = v.state.doc.lineAt(v.state.selection.main.head).number;
        v.dispatch({ changes: { from: 0, to: before.length, insert: after } });
        const l = v.state.doc.line(Math.min(line, v.state.doc.lines));
        v.dispatch({ selection: { anchor: l.from }, scrollIntoView: true });
      }
    } catch (err) {
      const msg = String((err as Error)?.message ?? err).split("\n")[0];
      toast(t("code.format_failed", { error: msg }), "error");
    } finally {
      this.formatting = false;
      v.focus();
    }
  }

  focus() {
    this.view?.focus();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.view?.destroy();
    this.view = undefined;
  }

  connectedCallback() {
    super.connectedCallback();
    // Moved in the DOM (e.g. a dialog re-rendered): build the view again.
    if (this.hasUpdated && !this.view) this.firstUpdated();
  }
}
customElements.define("phx-code-editor", PhxCodeEditor);
