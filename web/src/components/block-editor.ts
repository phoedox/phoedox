// A full-screen editor for content blocks (charts, Mermaid diagrams, the
// other registered blocks and plugin blocks): on the left a form (charts)
// or the source in the code editor, on the right a live preview with the
// problems of the source. openBlockEditor() resolves with the new source,
// or undefined when cancelled.
import { LitElement, css, html, nothing } from "lit";
import { blockDef } from "../blocks";
import type { ChartSpec } from "../chart";
import { chartSource } from "../chart-form-model";
import { t, type Key } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import "./code-editor";
import "./chart-form";
import "./block-view";
import "./blocks";
import "./plugin-block";

export interface BlockEditorOptions {
  kind: string;
  source: string;
  /** Shown above (e.g. "Chart"); default: the block's name. */
  title?: string;
  hint?: string;
  project?: string;
  doc?: string;
  /** Kinds to choose from (a new block on a whiteboard). */
  kinds?: { kind: string; label: string; example: string; plugin?: boolean }[];
  /** A plugin block (preview by its plugin). */
  plugin?: boolean;
}

export interface BlockEditorResult { kind: string; source: string }

export function openBlockEditor(o: BlockEditorOptions): Promise<BlockEditorResult | undefined> {
  return new Promise((resolve) => {
    const el = document.createElement("phx-block-editor") as PhxBlockEditor;
    el.opts = o;
    el.kind = o.kind;
    el.src = o.source;
    el.plugin = !!o.plugin;
    el.done = (r) => {
      el.remove();
      resolve(r);
    };
    document.body.append(el);
  });
}

export class PhxBlockEditor extends LitElement {
  static properties = { kind: { state: true }, src: { state: true }, view: { state: true }, preview: { state: true }, plugin: { state: true } };
  static styles = [base, css`
    dialog { width: 100vw; height: 100dvh; max-width: none; max-height: none; margin: 0; padding: 0; border: 0; background: var(--bg); color: var(--fg); }
    dialog::backdrop { background: rgb(0 0 0 / .4); }
    .wrap { display: grid; grid-template-rows: auto 1fr; height: 100%; }
    header { display: flex; gap: 10px; align-items: center; padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--surface); flex-wrap: wrap; }
    header h2 { margin: 0; font-size: 17px; display: flex; gap: 8px; align-items: center; }
    header .sp { flex: 1; }
    .seg { display: inline-flex; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
    .seg button { border: 0; border-radius: 0; background: transparent; color: var(--muted); padding: 5px 12px; font-size: 13px; display: inline-flex; gap: 5px; align-items: center; }
    .seg button[aria-pressed="true"] { background: var(--accent-soft); color: var(--fg); }
    main { display: grid; grid-template-columns: minmax(340px, 46%) 1fr; min-height: 0; }
    .left { min-height: 0; overflow: auto; border-right: 1px solid var(--line); }
    .left.form { padding: 14px; }
    .left phx-code-editor { height: 100%; }
    .right { min-height: 0; overflow: auto; padding: 18px 22px; display: grid; align-content: start; gap: 10px; }
    .hint { color: var(--muted); font-size: 13px; margin: 0; max-width: 80ch; }
    .problem { border: 1px solid var(--line); border-left: 3px solid var(--bad); border-radius: 8px; padding: 8px 12px; background: var(--surface); font-size: 13px; }
    .problem b { color: var(--bad); }
    .card { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 16px; min-height: 200px; }
    select { width: auto; }
    @media (max-width: 860px) { main { grid-template-columns: 1fr; grid-template-rows: 1fr 1fr; } .left { border-right: 0; border-bottom: 1px solid var(--line); } }
  `];
  opts!: BlockEditorOptions;
  kind = "";
  src = "";
  plugin = false;
  /** form (charts) or source. */
  view: "form" | "source" = "form";
  /** The source shown in the preview (follows the text with a short delay). */
  preview = "";
  done!: (r: BlockEditorResult | undefined) => void;
  private start = "";
  private timer?: number;

  connectedCallback() {
    super.connectedCallback();
    this.start = this.kind + "\n" + this.src;
    this.preview = this.src;
    if (!this.hasForm) this.view = "source";
  }

  firstUpdated() {
    this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.showModal();
  }

  private get hasForm() {
    return this.kind === "chart";
  }

  /** The chart in the source, if it is valid JSON (else the form waits for the source). */
  private get chart(): ChartSpec | undefined {
    try {
      const v = JSON.parse(this.src);
      return v && typeof v === "object" && !Array.isArray(v) ? (v as ChartSpec) : undefined;
    } catch {
      return undefined;
    }
  }

  private setSource(s: string, now = false) {
    this.src = s;
    clearTimeout(this.timer);
    if (now) this.preview = s;
    else this.timer = window.setTimeout(() => (this.preview = this.src), 250);
  }

  private problem(): string {
    if (this.kind === "mermaid" || this.plugin) return "";
    const def = blockDef(this.kind);
    if (!def) return "";
    const { model, problems } = def.parse(this.src);
    if (model) return "";
    const p = problems[0];
    return p ? [t(p.key as Key, p.params ?? {}), p.detail].filter(Boolean).join(" – ") : t("block.cannot_show");
  }

  private close(apply: boolean) {
    if (!apply && this.kind + "\n" + this.src !== this.start && !confirm(t("blockedit.discard"))) return;
    this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close();
    this.done(apply ? { kind: this.kind, source: this.src.trim() } : undefined);
  }

  private previewView() {
    const p = this.preview;
    if (this.kind === "mermaid") return html`<phx-mermaid data-src=${p}></phx-mermaid>`;
    if (this.plugin) return html`<phx-plugin-block data-kind=${this.kind} data-src=${p} data-project=${this.opts.project ?? ""}></phx-plugin-block>`;
    return html`<phx-block data-kind=${this.kind} data-src=${p} data-project=${this.opts.project ?? ""} data-doc=${this.opts.doc ?? ""}></phx-block>`;
  }

  render() {
    const o = this.opts;
    const def = blockDef(this.kind);
    const title = o.title ?? (def ? t(def.label as Key) : this.kind);
    const problem = this.problem();
    const chart = this.view === "form" ? this.chart : undefined;
    const lang = this.kind === "chart" ? "json" : "text";
    return html`<dialog aria-labelledby="be-title" @cancel=${(e: Event) => { e.preventDefault(); this.close(false); }}>
      <div class="wrap">
        <header>
          <h2 id="be-title">${icon(def?.icon ?? "Shapes", 18)} ${title}</h2>
          ${o.kinds?.length ? html`<select aria-label=${t("wb.block_kind")} @change=${(e: Event) => {
            const k = o.kinds!.find((x) => x.kind === (e.target as HTMLSelectElement).value);
            if (!k) return;
            this.kind = k.kind;
            this.plugin = !!k.plugin;
            this.view = k.kind === "chart" ? "form" : "source";
            this.setSource(k.example, true);
          }}>${o.kinds.map((k) => html`<option value=${k.kind} ?selected=${k.kind === this.kind}>${k.label}</option>`)}</select>` : nothing}
          ${this.hasForm ? html`<div class="seg" role="group" aria-label=${t("blockedit.view")}>
            <button aria-pressed=${this.view === "form" ? "true" : "false"} @click=${() => (this.view = "form")}>${icon("SlidersHorizontal", 14)} ${t("blockedit.form")}</button>
            <button aria-pressed=${this.view === "source" ? "true" : "false"} @click=${() => (this.view = "source")}>${icon("Code", 14)} ${t("blockedit.source")}</button>
          </div>` : nothing}
          <span class="sp"></span>
          <button class="ghost" @click=${() => this.close(false)}>${t("doc.cancel")}</button>
          <button ?disabled=${!!problem || !this.src.trim()} @click=${() => this.close(true)}>${icon("Check", 14)} ${t("blockedit.apply")}</button>
        </header>
        <main>
          ${this.view === "form" && this.hasForm
            ? html`<div class="left form">${chart
              ? html`<phx-chart-form .spec=${chart} @change=${(e: CustomEvent<ChartSpec>) => this.setSource(chartSource(e.detail))}></phx-chart-form>`
              : html`<p class="problem"><b>${t("blockedit.form_needs_json")}</b></p>`}</div>`
            : html`<div class="left"><phx-code-editor .lang=${lang} .label=${t("block.source")} .value=${this.src}
                @input=${(e: Event) => this.setSource((e.target as HTMLInputElement).value)}></phx-code-editor></div>`}
          <div class="right">
            ${(o.hint || def) && !(this.view === "form" && this.hasForm) ? html`<p class="hint">${o.hint ?? t(def!.hint as Key)}</p>` : nothing}
            ${problem ? html`<div class="problem" role="alert"><b>${t("block.cannot_show")}:</b> ${problem}</div>` : nothing}
            <div class="card" aria-label=${t("blockedit.preview")}>${this.previewView()}</div>
          </div>
        </main>
      </div>
    </dialog>`;
  }
}
customElements.get("phx-block-editor") || customElements.define("phx-block-editor", PhxBlockEditor);
