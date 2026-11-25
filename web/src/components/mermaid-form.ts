// <phx-mermaid-form .kind=… .model=…>: the form of a Mermaid diagram kind,
// built from its description (fields and nested lists, see
// mermaid-forms/core.ts). Fires "change" with the new model.
import { LitElement, css, html, nothing, type TemplateResult } from "lit";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import type { FieldDef, ListDef, MermaidKind, Model, Row } from "../mermaid-forms/core";
import { colorField, colorFieldCss } from "./color-field";
import { base, foldCss } from "./shared";

type Path = (string | number)[];

/** A copy of obj with the value at path replaced. */
function setIn(obj: unknown, path: Path, value: unknown): unknown {
  if (!path.length) return value;
  const [k, ...rest] = path;
  if (Array.isArray(obj)) {
    const a = [...obj];
    a[k as number] = setIn(a[k as number], rest, value);
    return a;
  }
  const o = { ...(obj as Row) };
  o[k as string] = setIn(o[k as string], rest, value);
  return o;
}

const tr = (label: string, params?: Record<string, string | number>) => (label.startsWith("mmd.") || label.startsWith("chartf.") ? t(label as Key, params ?? {}) : label);

export class PhxMermaidForm extends LitElement {
  static properties = { kind: { attribute: false }, model: { attribute: false } };
  static styles = [base, foldCss, colorFieldCss, css`
    :host { display: block; }
    .hint { color: var(--muted); font-size: 12.5px; margin: 0; }
    .fields { display: flex; flex-wrap: wrap; gap: 8px 10px; align-items: flex-end; }
    label { display: grid; gap: 3px; font-size: 11.5px; color: var(--muted); min-width: 0; }
    label.s { flex: 0 1 8.5em; } label.m { flex: 1 1 10em; } label.l { flex: 2 1 14em; } label.full { flex: 1 1 100%; }
    label.s:has(select) { flex: 0 1 12em; } label.m:has(select) { flex: 1 1 13em; }
    label.check { display: flex; gap: 6px; align-items: center; color: var(--fg); font-size: 13px; flex: 0 0 auto; padding-bottom: 6px; }
    label.check input { width: auto; }
    input, select, textarea { padding: 5px 8px; min-height: 0; font-size: 13px; width: 100%; box-sizing: border-box; }
    textarea { font: 12.5px/1.45 var(--font-mono); min-height: 3.2em; resize: vertical; }
    .item { border-top: 1px solid var(--line); padding: 8px 0 4px; display: grid; gap: 6px; }
    .item:first-of-type { border-top: 0; }
    .item > .row { display: flex; gap: 8px; align-items: flex-start; }
    .item > .row > .fields { flex: 1; }
    .ops { display: flex; gap: 0; padding-top: 16px; white-space: nowrap; }
    .children { margin-left: 14px; padding-left: 12px; border-left: 2px solid var(--line); display: grid; gap: 4px; }
    .children > summary { list-style: none; cursor: pointer; margin: 2px 0 0; font-size: 12px; color: var(--muted); font-weight: 600; display: flex; gap: 4px; align-items: center; user-select: none; }
    .children > summary::-webkit-details-marker { display: none; }
    .children > summary .count { font-weight: 400; }
    button.icon { padding: 3px; border: 0; background: transparent; color: var(--muted); }
    @media (hover: hover) and (pointer: fine) { button.icon:hover { color: var(--fg); } button.icon.del:hover { color: var(--bad); } }
    button.icon:disabled { opacity: .3; }
    .add { justify-self: start; display: inline-flex; gap: 5px; align-items: center; font-size: 13px; padding: 5px 10px; }
  `];
  kind!: MermaidKind;
  model!: Model;
  /** "Further lines" starts open only when there are some. */
  private extraOpen?: boolean;

  private set(path: Path, value: unknown) {
    this.model = setIn(this.model, path, value) as Model;
    this.dispatchEvent(new CustomEvent("change", { detail: this.model }));
  }

  private field(f: FieldDef, row: Row, path: Path, listKey: string): TemplateResult | typeof nothing {
    if (f.show && !f.show(row)) return nothing;
    const v = row[f.key];
    const p = [...path, f.key];
    const label = tr(f.label);
    switch (f.type) {
      case "color":
        return colorField(label, v, (x) => this.set(p, x || undefined), f.size ?? "s");
      case "check":
        return html`<label class="check"><input type="checkbox" .checked=${!!v} @change=${(e: Event) => this.set(p, (e.target as HTMLInputElement).checked)}>${label}</label>`;
      case "select":
        return html`<label class=${f.size ?? "m"}>${label}<select @change=${(e: Event) => this.set(p, (e.target as HTMLSelectElement).value)}>
          ${(f.options ?? []).map(([val, lab]) => html`<option value=${val} ?selected=${String(v ?? "") === val}>${f.showValue ? `${val}  ${tr(lab)}` : tr(lab)}</option>`)}
          ${v !== undefined && v !== "" && !(f.options ?? []).some(([val]) => val === String(v)) ? html`<option value=${String(v)} selected>${String(v)}</option>` : nothing}
        </select></label>`;
      case "lines":
        return html`<label class="full">${label}<textarea rows=${Math.min(8, Math.max(2, String(v ?? "").split("\n").length))} placeholder=${f.placeholder ?? ""} .value=${String(v ?? "")}
          @change=${(e: Event) => this.set(p, (e.target as HTMLTextAreaElement).value)}></textarea></label>`;
      case "number":
        return html`<label class=${f.size ?? "s"}>${label}<input inputmode="decimal" placeholder=${f.placeholder ?? ""} .value=${v === undefined || v === null ? "" : String(v)}
          @change=${(e: Event) => {
            const raw = (e.target as HTMLInputElement).value.trim().replace(",", ".");
            this.set(p, raw === "" || Number.isNaN(Number(raw)) ? undefined : Number(raw));
          }}></label>`;
      default:
        return html`<label class=${f.size ?? "m"}>${label}<input placeholder=${f.placeholder ?? ""} list=${f.suggest ? `dl-${listKey}-${f.key}` : ""} .value=${String(v ?? "")}
          @change=${(e: Event) => this.set(p, (e.target as HTMLInputElement).value)}></label>`;
    }
  }

  private datalists(l: ListDef): TemplateResult[] {
    return l.fields.filter((f) => f.suggest).map((f) => html`<datalist id=${`dl-${l.key}-${f.key}`}>${[...new Set(f.suggest!(this.model))].map((o) => html`<option value=${o}></option>`)}</datalist>`);
  }

  private list(l: ListDef, items: Row[], path: Path): TemplateResult {
    const move = (i: number, d: -1 | 1) => {
      const a = [...items];
      const [x] = a.splice(i, 1);
      a.splice(i + d, 0, x);
      this.set(path, a);
    };
    return html`${items.map((row, i) => html`<div class="item">
        <div class="row">
          <div class="fields">${l.fields.map((f) => this.field(f, row, [...path, i], l.key))}</div>
          <span class="ops">
            <button class="icon" aria-label=${t("gantt.up")} title=${t("gantt.up")} ?disabled=${i === 0} @click=${() => move(i, -1)}>${icon("ArrowUp", 13)}</button>
            <button class="icon" aria-label=${t("gantt.down")} title=${t("gantt.down")} ?disabled=${i === items.length - 1} @click=${() => move(i, 1)}>${icon("ArrowDown", 13)}</button>
            <button class="icon del" aria-label=${t("chartf.remove")} title=${t("chartf.remove")} @click=${() => this.set(path, items.filter((_, k) => k !== i))}>${icon("X", 14)}</button>
          </span>
        </div>
        ${l.children ? (() => {
          const kids = Array.isArray(row[l.children!.key]) ? (row[l.children!.key] as Row[]) : [];
          return html`<details class="children" open><summary><span class="chev">${icon("ChevronRight", 13)}</span>${tr(l.children!.label)} <span class="count">${kids.length}</span></summary>
            ${this.list(l.children!, kids, [...path, i, l.children!.key])}</details>`;
        })() : nothing}
      </div>`)}
      <button class="ghost add" @click=${() => this.set(path, [...items, l.make ? l.make(this.model) : {}])}>${icon("Plus", 13)} ${tr(l.add)}</button>
      ${this.datalists(l)}${l.children ? this.datalists(l.children) : nothing}`;
  }

  render() {
    const k = this.kind, m = this.model;
    if (!k || !m) return nothing;
    return html`
      ${k.fields?.length ? html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("Settings2", 15)} ${t("chartf.general")}</summary><div class="body">
        <div class="fields">${k.fields.map((f) => this.field(f, m, [], "root"))}</div></div></details>` : nothing}
      ${(k.lists ?? []).map((l) => html`<details class="sec" open><summary><span class="chev">${icon("ChevronRight", 14)}</span>${tr(l.label)} <span class="count">${Array.isArray(m[l.key]) ? (m[l.key] as Row[]).length : 0}</span></summary><div class="body">
        ${l.hint ? html`<p class="hint">${tr(l.hint)}</p>` : nothing}
        ${this.list(l, Array.isArray(m[l.key]) ? (m[l.key] as Row[]) : [], [l.key])}
      </div></details>`)}
      <details class="sec" ?open=${(this.extraOpen ??= !!String(m.extra ?? "").trim())}><summary><span class="chev">${icon("ChevronRight", 14)}</span>${icon("Code", 15)} ${t("mmd.extra")}</summary><div class="body">
        <p class="hint">${t("mmd.extra_hint")}</p>
        <textarea rows=${Math.min(10, Math.max(2, String(m.extra ?? "").split("\n").length))} .value=${String(m.extra ?? "")} @change=${(e: Event) => this.set(["extra"], (e.target as HTMLTextAreaElement).value)}></textarea>
      </div></details>`;
  }
}
customElements.get("phx-mermaid-form") || customElements.define("phx-mermaid-form", PhxMermaidForm);
