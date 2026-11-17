// Structure blocks of pages: tabs (> [!tab] callouts that follow each other),
// steps readers tick off (> [!steps]) and a side-by-side comparison of two
// versions of a file (```compare). Without JavaScript (print, feeds) the
// content stays readable: tabs one after another, steps as a list.

import { api } from "../api";
import { compareRows, parseCompare } from "./compare-logic";
import { t } from "../i18n";
import { currentUser } from "../state";

const esc = (s: unknown) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const TAB_KEY = "phx-tab";
let tabSeq = 0;

/** <phx-tabs>: sections with data-title become tabs. Choosing a tab also
 * chooses the tab with the same title in other groups (e.g. "macOS"). */
export class PhxTabs extends HTMLElement {
  private sections: HTMLElement[] = [];
  private buttons: HTMLButtonElement[] = [];
  private uid = `t${++tabSeq}`;

  connectedCallback() {
    if (this.shadowRoot) return;
    this.sections = [...this.children].filter((c): c is HTMLElement => c instanceof HTMLElement && c.matches("section.tab"));
    if (!this.sections.length) return;
    const root = this.attachShadow({ mode: "open" });
    root.innerHTML = `<style>
      :host { display: block; margin: 1em 0; }
      [role=tablist] { display: flex; flex-wrap: wrap; gap: 2px; border-bottom: 1px solid var(--line); margin-bottom: 10px; }
      button { font: inherit; font-size: 14px; padding: 7px 14px; border: 0; border-bottom: 2px solid transparent; background: none; color: var(--muted); cursor: pointer; margin-bottom: -1px; border-radius: 6px 6px 0 0; }
      button[aria-selected=true] { color: var(--fg); border-bottom-color: var(--accent); font-weight: 600; }
      @media (hover: hover) and (pointer: fine) { button:hover { background: var(--surface2); } }
      button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
    </style><div role="tablist"></div><slot></slot>`;
    const list = root.querySelector("[role=tablist]")!;
    this.sections.forEach((s, i) => {
      const b = document.createElement("button");
      b.type = "button";
      b.setAttribute("role", "tab");
      b.id = `${this.uid}-${i}`;
      b.textContent = s.dataset.title || `${i + 1}`;
      b.addEventListener("click", () => this.select(i, true));
      b.addEventListener("keydown", (e) => {
        const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
        if (e.key === "Home" || e.key === "End" || d) {
          e.preventDefault();
          const n = e.key === "Home" ? 0 : e.key === "End" ? this.sections.length - 1 : (this.current + d + this.sections.length) % this.sections.length;
          this.select(n, true);
          this.buttons[n].focus();
        }
      });
      list.append(b);
      this.buttons.push(b);
      s.setAttribute("role", "tabpanel");
      s.setAttribute("aria-label", s.dataset.title ?? "");
      s.tabIndex = 0;
    });
    let start = 0;
    try {
      const pref = localStorage.getItem(TAB_KEY);
      const i = this.sections.findIndex((s) => s.dataset.title === pref);
      if (i >= 0) start = i;
    } catch { /* no storage */ }
    this.select(start, false);
    addEventListener(TAB_KEY, this.onOther);
    addEventListener("beforeprint", this.showAll);
    addEventListener("afterprint", this.restore);
  }

  disconnectedCallback() {
    removeEventListener(TAB_KEY, this.onOther);
    removeEventListener("beforeprint", this.showAll);
    removeEventListener("afterprint", this.restore);
  }

  private current = 0;

  select(i: number, remember: boolean) {
    this.current = i;
    this.sections.forEach((s, j) => (s.hidden = j !== i));
    this.buttons.forEach((b, j) => {
      b.setAttribute("aria-selected", String(j === i));
      b.tabIndex = j === i ? 0 : -1;
    });
    if (remember) {
      const title = this.sections[i].dataset.title ?? "";
      try { localStorage.setItem(TAB_KEY, title); } catch { /* no storage */ }
      dispatchEvent(new CustomEvent(TAB_KEY, { detail: { title, from: this } }));
    }
  }

  private onOther = (e: Event) => {
    const { title, from } = (e as CustomEvent<{ title: string; from: PhxTabs }>).detail;
    if (from === this) return;
    const i = this.sections.findIndex((s) => s.dataset.title === title);
    if (i >= 0) this.select(i, false);
  };

  private showAll = () => this.sections.forEach((s) => (s.hidden = false));
  private restore = () => this.select(this.current, false);
}
customElements.get("phx-tabs") || customElements.define("phx-tabs", PhxTabs);

/** <phx-steps>: the first numbered list of a > [!steps] callout gets a tick
 * per step and a progress count; each reader's ticks are their own. */
export class PhxSteps extends HTMLElement {
  private done = new Set<number>();
  private items: HTMLLIElement[] = [];
  private timer = 0;

  connectedCallback() {
    if (this.dataset.ready) return;
    this.dataset.ready = "1";
    const ol = this.querySelector("ol");
    if (!ol) return;
    this.items = [...ol.children].filter((c): c is HTMLLIElement => c instanceof HTMLLIElement);
    ol.classList.add("steps");
    this.items.forEach((li, i) => {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "step-check";
      b.setAttribute("aria-label", t("steps.tick", { n: i + 1 }));
      b.addEventListener("click", () => this.toggle(i));
      li.prepend(b);
    });
    const prog = document.createElement("div");
    prog.className = "step-progress";
    prog.setAttribute("aria-live", "polite");
    const title = this.querySelector(".cc > .ct");
    if (title) title.after(prog);
    else (this.querySelector(".cc") ?? this).prepend(prog);
    this.paint();
    void this.load();
  }

  private get storeKey() {
    return `phx-steps:${this.dataset.doc}:${this.dataset.key}`;
  }

  private get remote() {
    return !!(this.dataset.project && this.dataset.doc && this.dataset.key && currentUser.get());
  }

  private get path() {
    return `/projects/${this.dataset.project}/documents/${encodeURIComponent(this.dataset.doc!)}/blocks/${encodeURIComponent(this.dataset.key!)}`;
  }

  private async load() {
    let saved: number[] = [];
    try {
      if (this.remote) saved = ((await api<{ mine: { done?: number[] } | null }>("GET", this.path)).mine?.done ?? []);
      else saved = JSON.parse(localStorage.getItem(this.storeKey) ?? "[]");
    } catch { /* nothing saved yet */ }
    this.done = new Set(saved.filter((n) => Number.isInteger(n) && n >= 0 && n < this.items.length));
    this.paint();
  }

  private toggle(i: number) {
    if (this.done.has(i)) this.done.delete(i);
    else this.done.add(i);
    this.paint();
    clearTimeout(this.timer);
    this.timer = window.setTimeout(() => {
      const done = [...this.done].sort((a, b) => a - b);
      if (this.remote) void api("PUT", this.path, { data: done.length ? { done } : null }).catch(() => undefined);
      else try { localStorage.setItem(this.storeKey, JSON.stringify(done)); } catch { /* no storage */ }
    }, 400);
  }

  private paint() {
    this.items.forEach((li, i) => {
      const on = this.done.has(i);
      li.classList.toggle("done", on);
      li.querySelector<HTMLButtonElement>(".step-check")?.setAttribute("aria-pressed", String(on));
    });
    const p = this.querySelector(".step-progress");
    if (p) {
      const n = this.done.size, all = this.items.length;
      p.innerHTML = `<span>${esc(t("steps.progress", { done: n, all }))}</span><span class="bar"><span style="width:${all ? Math.round((n / all) * 100) : 0}%"></span></span>`;
    }
  }
}
customElements.get("phx-steps") || customElements.define("phx-steps", PhxSteps);

/** <phx-compare>: two versions side by side with the differences marked. */
export class PhxCompare extends HTMLElement {
  static get observedAttributes() {
    return ["data-src"];
  }
  connectedCallback() {
    this.render();
  }
  attributeChangedCallback() {
    if (this.isConnected) this.render();
  }
  private render() {
    const src = this.dataset.src ?? this.querySelector("pre")?.textContent ?? "";
    const root = this.shadowRoot ?? this.attachShadow({ mode: "open" });
    const c = parseCompare(src);
    const style = `<style>
      :host { display: block; margin: 1em 0; }
      .wrap { border: 1px solid var(--line); border-radius: 10px; overflow: auto; background: var(--surface); }
      table { border-collapse: collapse; width: 100%; font: 12.5px/1.5 var(--font-mono, monospace); table-layout: fixed; }
      th { text-align: left; font: 600 12px var(--font-body, sans-serif); color: var(--muted); padding: 6px 10px; border-bottom: 1px solid var(--line); background: var(--surface2); }
      td { padding: 0 8px; vertical-align: top; white-space: pre-wrap; word-break: break-word; }
      col.cn { width: 3.2em; }
      td.n { color: var(--muted); text-align: right; user-select: none; padding-right: 6px; }
      td.side { border-left: 1px solid var(--line); }
      .del { background: color-mix(in srgb, var(--bad) 14%, transparent); }
      .add { background: color-mix(in srgb, var(--good) 14%, transparent); }
      .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
      .err { color: var(--bad); font-size: 13px; }
      @media (max-width: 640px) { col.cn { width: 0; } td.n { display: none; } }
    </style>`;
    if (!c) {
      root.innerHTML = `${style}<p class="err">${esc(t("compare.error"))}</p><pre>${esc(src)}</pre>`;
      return;
    }
    const rows = compareRows(c.left, c.right);
    const changed = rows.filter((r) => r.op !== "same").length;
    root.innerHTML = `${style}<div class="wrap"><table aria-label="${esc(t("compare.label", { count: changed }))}"><colgroup><col class="cn"><col><col class="cn"><col></colgroup>
      <thead><tr><th colspan="2">${esc(c.leftTitle || t("compare.before"))}</th><th colspan="2" class="side">${esc(c.rightTitle || t("compare.after"))}</th></tr></thead><tbody>
      ${rows.map((r) => `<tr><td class="n">${r.ln ?? ""}</td><td class="${r.op === "del" || r.op === "change" ? "del" : ""}">${r.l !== undefined ? esc(r.l) : ""}${r.op === "del" || r.op === "change" ? `<span class="sr"> (${esc(t("compare.removed"))})</span>` : ""}</td><td class="n side">${r.rn ?? ""}</td><td class="${r.op === "add" || r.op === "change" ? "add" : ""}">${r.r !== undefined ? esc(r.r) : ""}${r.op === "add" || r.op === "change" ? `<span class="sr"> (${esc(t("compare.added"))})</span>` : ""}</td></tr>`).join("")}
      </tbody></table></div>`;
  }
}
customElements.get("phx-compare") || customElements.define("phx-compare", PhxCompare);

/** Foldable callouts and tabs open for printing. */
addEventListener("beforeprint", () => {
  const open = (root: Document | ShadowRoot) => {
    root.querySelectorAll("details.fold:not([open])").forEach((d) => { d.setAttribute("open", ""); d.setAttribute("data-print-opened", ""); });
    root.querySelectorAll("*").forEach((el) => { if (el.shadowRoot) open(el.shadowRoot); });
  };
  open(document);
});
addEventListener("afterprint", () => {
  const close = (root: Document | ShadowRoot) => {
    root.querySelectorAll("details[data-print-opened]").forEach((d) => { d.removeAttribute("open"); d.removeAttribute("data-print-opened"); });
    root.querySelectorAll("*").forEach((el) => { if (el.shadowRoot) close(el.shadowRoot); });
  };
  close(document);
});
