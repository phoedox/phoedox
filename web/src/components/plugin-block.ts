// <phx-plugin-block data-kind data-project>: a block a plugin adds (ADR-0086).
// The server's picture (the plugin's block_render, cleaned) shows first; a
// plugin element, when the plugin has one, then takes over with the source,
// the picture and the project.
import { locale, t } from "../i18n";
import { pluginBlock, renderPluginBlock } from "../plugin-blocks";

const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

const STYLE = `
:host { display: block; margin: 1em 0; }
:host([data-board]) { margin: 0; height: 100%; }
.pic svg { max-width: 100%; height: auto; display: block; }
.err { border: 1px solid var(--line); border-left: 3px solid var(--bad); border-radius: 8px; padding: 8px 12px; background: var(--surface); font-size: 13px; }
.err b { display: block; color: var(--bad); margin-bottom: 4px; }
.err ul { margin: 4px 0 0; padding-left: 18px; }
.err pre { margin: 6px 0 0; white-space: pre-wrap; font: 12px/1.45 var(--font-mono, monospace); color: var(--muted); }
.by { font-size: 11px; color: var(--muted); margin-top: 4px; }
`;

export class PhxPluginBlock extends HTMLElement {
  source?: string;
  private shown = "";

  static get observedAttributes() {
    return ["data-src", "data-kind"];
  }

  connectedCallback() {
    void this.render();
  }

  attributeChangedCallback() {
    if (this.isConnected) void this.render();
  }

  async render() {
    const kind = this.dataset.kind ?? "";
    const project = this.dataset.project ?? "";
    const src = this.source ?? this.dataset.src ?? this.querySelector("pre")?.textContent ?? "";
    if (this.shown === kind + "\n" + src) return;
    this.shown = kind + "\n" + src;
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    const info = pluginBlock(kind);
    const width = Math.max(320, Math.floor(this.getBoundingClientRect().width) || 720);
    root.innerHTML = `<style>${STYLE}</style><div class="pic" aria-busy="true"></div>`;
    try {
      const r = await renderPluginBlock(project, kind, src, width, locale());
      if (this.shown !== kind + "\n" + src) return;
      const errors = r.problems.filter((p) => p.severity !== "warning");
      if (!r.svg && errors.length) {
        root.innerHTML = `<style>${STYLE}</style><div class="err" role="alert"><b>${esc(info?.label ?? kind)}: ${esc(t("block.cannot_show"))}</b>
          <ul>${errors.map((p) => `<li>${esc(t("plugin_block.line", { line: p.line }))} ${esc(p.message)}</li>`).join("")}</ul><pre>${esc(src)}</pre></div>`;
        return;
      }
      // The plugin's element shows it interactively; the picture is its first paint.
      const tag = info?.element;
      if (tag && customElements.get(tag)) {
        const el = document.createElement(tag) as HTMLElement & { source: string; svg: string; project: string };
        el.source = src;
        el.svg = r.svg;
        el.project = project;
        root.innerHTML = `<style>${STYLE}</style>`;
        root.append(el);
        return;
      }
      root.innerHTML = `<style>${STYLE}</style><div class="pic" role="img" aria-label="${esc(info?.label ?? kind)}">${r.svg}</div>`;
    } catch {
      root.innerHTML = `<style>${STYLE}</style><div class="err" role="alert"><b>${esc(info?.label ?? kind)}: ${esc(t("plugin_block.failed"))}</b><pre>${esc(src)}</pre></div>`;
    }
  }
}
customElements.get("phx-plugin-block") || customElements.define("phx-plugin-block", PhxPluginBlock);
