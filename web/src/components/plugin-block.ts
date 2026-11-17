// <phx-plugin-block data-kind data-project>: a block a plugin adds (ADR-0086).
// The server's picture (the plugin's block_render, cleaned) shows first; a
// plugin element, when the plugin has one, then takes over with the source,
// the picture and the project.
import { api } from "../api";
import { locale, t } from "../i18n";
import { clearPluginBlockCache, loadPluginBlocks, pluginBlock, renderPluginBlock, type PluginBlockInfo } from "../plugin-blocks";
import { reloadProjectPlugins } from "../plugin-host";
import { hashText } from "../blocks/registry";
import { currentProject, currentUser } from "../state";

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
.need { border: 1px dashed var(--line); border-radius: 8px; padding: 10px 12px; background: var(--surface); font-size: 13px; }
.need b { display: block; margin-bottom: 2px; }
.need p { margin: 2px 0 0; color: var(--muted); }
.need button { margin-top: 8px; font: inherit; font-size: 13px; padding: 5px 12px; border-radius: 6px; border: 1px solid var(--accent); background: var(--accent); color: var(--on-color); cursor: pointer; }
.need button:disabled { opacity: .6; cursor: default; }
.need pre { margin: 8px 0 0; white-space: pre-wrap; font: 12px/1.45 var(--font-mono, monospace); color: var(--muted); }
`;

export class PhxPluginBlock extends HTMLElement {
  source?: string;
  shown = "";
  private renderMissing = missingView;

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
    if (info?.missing) {
      this.renderMissing(root, info, project, src);
      return;
    }
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
        const el = document.createElement(tag) as HTMLElement & { source: string; svg: string; project: string; doc: string; stateKey: string };
        el.source = src;
        el.svg = r.svg;
        el.project = project;
        // Where the reader's state lives: the same keys as core blocks (a
        // whiteboard element's id, else a hash of the source), so state
        // survives a block's move into a plugin.
        el.doc = this.dataset.doc ?? "";
        el.stateKey = this.dataset.key || hashText(src.trim());
        root.innerHTML = `<style>${STYLE}</style>`;
        root.append(el);
        return;
      }
      root.innerHTML = `<style>${STYLE}</style><div class="pic" role="img" aria-label="${esc(info?.label ?? kind)}">${r.svg}</div>`;
      // The plugin's module may still be loading (e.g. right after enabling it).
      if (tag && tag.includes("-")) void customElements.whenDefined(tag).then(() => {
        if (this.shown !== kind + "\n" + src) return;
        this.shown = "";
        void this.render();
      });
    } catch {
      root.innerHTML = `<style>${STYLE}</style><div class="err" role="alert"><b>${esc(info?.label ?? kind)}: ${esc(t("plugin_block.failed"))}</b><pre>${esc(src)}</pre></div>`;
    }
  }
}
// A kind that moved into a plugin which is not active here (ADR-0088): say
// which plugin it needs and let owners enable it, admins install it.
function missingView(this: PhxPluginBlock, root: ShadowRoot, info: PluginBlockInfo, project: string, src: string) {
  const m = info.missing!;
  const owner = currentProject.get()?.slug === project && currentProject.get()?.role === "owner";
  const admin = !!currentUser.get()?.isAdmin;
  let action = "", hint = "";
  if (m.installed && owner) action = "enable";
  else if (!m.installed && admin && m.store) action = "install";
  else if (!m.installed && admin) hint = t("plugin_block.upload_hint");
  else if (m.installed) hint = t("plugin_block.ask_owner");
  else hint = t("plugin_block.ask_admin");
  root.innerHTML = `<style>${STYLE}</style><div class="need" role="note"><b>${esc(info.label)}: ${esc(t("plugin_block.needs", { plugin: m.plugin }))}</b>
    ${hint ? `<p>${esc(hint)}</p>` : ""}${action ? `<button type="button">${esc(t(action === "enable" ? "plugin_block.enable" : "plugin_block.install"))}</button>` : ""}
    <pre>${esc(src)}</pre></div>`;
  const btn = root.querySelector("button");
  btn?.addEventListener("click", async () => {
    btn.disabled = true;
    try {
      if (action === "install") await api("POST", "/admin/plugins/install", { id: m.plugin });
      if (action === "enable" || owner) await api("PUT", `/projects/${project}/plugins/${m.plugin}`, { enabled: true });
      clearPluginBlockCache();
      this.shown = "";
      reloadProjectPlugins(); // loads the plugin's elements as well
      await loadPluginBlocks(project, true);
      void this.render();
    } catch {
      btn.disabled = false;
      const p = document.createElement("p");
      p.textContent = t(action === "install" ? "plugin_block.install_failed" : "plugin_block.failed");
      btn.after(p);
    }
  });
}

customElements.get("phx-plugin-block") || customElements.define("phx-plugin-block", PhxPluginBlock);
