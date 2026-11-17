// <phx-block data-kind="…">: a registered content block (ADR-0085) in a page,
// a published page or a whiteboard overlay. The source is the <pre> inside
// (pages), data-src (the editor) or the source property (whiteboards).

import { blockDef, blockKey, type BlockDef } from "../blocks";
import { locale, t, type Key } from "../i18n";

const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

const STYLE = `
:host { display: block; margin: 1em 0; }
:host([data-board]) { margin: 0; height: 100%; }
.err { border: 1px solid var(--line); border-left: 3px solid var(--bad); border-radius: 8px; padding: 8px 12px; background: var(--surface); font-size: 13px; }
.err b { display: block; color: var(--bad); margin-bottom: 4px; }
.err pre { margin: 6px 0 0; white-space: pre-wrap; font: 12px/1.45 var(--font-mono, monospace); color: var(--muted); }
.host { height: 100%; }
.pic svg { max-width: 100%; height: auto; display: block; }
`;

export class PhxBlock extends HTMLElement {
  /** Set by whiteboards instead of a source text. */
  source?: string;
  private cleanup?: void | (() => void);
  private shown = "";

  static get observedAttributes() {
    return ["data-src", "data-kind", "data-editable"];
  }

  connectedCallback() {
    void this.render();
  }

  disconnectedCallback() {
    if (typeof this.cleanup === "function") this.cleanup();
    this.cleanup = undefined;
    this.shown = "";
  }

  attributeChangedCallback() {
    if (this.isConnected) void this.render();
  }

  /** Draws again (e.g. after the source property changed). */
  async render() {
    const kind = this.dataset.kind ?? "";
    const src = this.source ?? this.dataset.src ?? this.querySelector("pre")?.textContent ?? "";
    if (this.shown === kind + "\n" + src) return;
    this.shown = kind + "\n" + src;
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    if (typeof this.cleanup === "function") this.cleanup();
    this.cleanup = undefined;
    const def = blockDef(kind);
    if (!def) {
      root.innerHTML = `<style>${STYLE}</style><div class="err" role="alert"><b>${esc(t("block.unknown_kind", { kind }))}</b><pre>${esc(src)}</pre></div>`;
      return;
    }
    const { model, problems } = def.parse(src);
    if (!model) {
      const p = problems[0];
      const detail = p ? [t(p.key as Key, p.params ?? {}), p.detail].filter(Boolean).join(" – ") : "";
      root.innerHTML = `<style>${STYLE}</style><div class="err" role="alert"><b>${esc(t(def.label as Key))}: ${esc(t("block.cannot_show"))}</b>${esc(detail)}<pre>${esc(src)}</pre></div>`;
      return;
    }
    root.innerHTML = `<style>${STYLE}</style><div class="host"></div>`;
    const host = root.querySelector<HTMLElement>(".host")!;
    if (def.mount) {
      this.cleanup = await def.mount(host, model, {
        project: this.dataset.project ?? "",
        doc: this.dataset.doc ?? "",
        key: this.dataset.key || blockKey(def, model, src),
        locale: locale(),
        board: this.hasAttribute("data-board"),
        source: src,
        // In the editor the block may change its own source; the editor applies it.
        edit: this.hasAttribute("data-editable")
          ? (source: string) => this.dispatchEvent(new CustomEvent("phx-block-edit", { detail: { source }, bubbles: true, composed: true }))
          : undefined,
      });
      return;
    }
    this.picture(def, model, host);
  }

  private picture(def: BlockDef, model: unknown, host: HTMLElement) {
    const w = Math.max(240, Math.floor(this.getBoundingClientRect().width) || 640);
    host.className = "host pic";
    host.innerHTML = def.svg(model, { width: w, locale: locale() });
  }
}
customElements.get("phx-block") || customElements.define("phx-block", PhxBlock);
