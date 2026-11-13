import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { t } from "../i18n";
import { icon } from "../icon";
import { languageName, rememberLocale } from "../languages";
import { contentLocale, currentProject } from "../state";
import { base } from "./shared";

/**
 * Content language of a multilingual project (ADR-0028) in the header: a
 * globe with the language code; the menu lists the project's languages.
 * Pages without a translation fall back to the main language with a note.
 */
export class PhxLangSwitcher extends SignalWatcher(LitElement) {
  static properties = { path: {}, open: { state: true }, active: { state: true } };
  static styles = [base, css`
    :host { position: relative; display: block; }
    .btn { display: flex; align-items: center; gap: 5px; padding: 5px 9px; border-radius: 8px; border: 1px solid var(--line); background: var(--surface);
      color: var(--fg); font: 600 12px var(--font-mono); letter-spacing: .04em; cursor: pointer; transition: border-color var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { .btn:hover { border-color: var(--muted); } }
    .btn .ico { color: var(--muted); }
    ul { position: absolute; top: calc(100% + 6px); left: 0; z-index: 40; min-width: 200px; list-style: none; margin: 0; padding: 4px;
      background: var(--surface); border: 1px solid var(--line); border-radius: 10px; box-shadow: 0 14px 34px rgb(0 0 0 / .18);
      transform-origin: top left; transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out);
      @starting-style { opacity: 0; transform: scale(.97); } }
    li { display: flex; gap: 10px; align-items: center; padding: 7px 9px; border-radius: 6px; cursor: pointer; font-size: 13.5px; }
    li.act { background: var(--surface2); }
    li .code { font: 600 11px var(--font-mono); color: var(--muted); width: 26px; }
    li .main { margin-left: auto; font-size: 11.5px; color: var(--muted); }
    li[aria-selected="true"] { color: var(--accent); }
    @media (prefers-reduced-motion: reduce) { ul { transition: opacity var(--dur-fast) ease; @starting-style { opacity: 0; transform: none; } } }
  `];
  path = "";
  open = false;
  active = 0;

  connectedCallback() {
    super.connectedCallback();
    addEventListener("click", this.outside);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    removeEventListener("click", this.outside);
  }

  private outside = (e: Event) => {
    if (this.open && !e.composedPath().includes(this)) this.open = false;
  };

  private langs(): string[] {
    const p = currentProject.get();
    return p?.mainLocale ? [p.mainLocale, ...p.locales] : [];
  }

  private choose(code: string) {
    const p = currentProject.get();
    if (!p) return;
    rememberLocale(p.slug, code);
    contentLocale.set(code);
    this.open = false;
    this.renderRoot.querySelector<HTMLButtonElement>(".btn")?.focus();
  }

  private toggle() {
    this.open = !this.open;
    if (this.open) {
      this.active = Math.max(0, this.langs().indexOf(contentLocale.get()));
      this.updateComplete.then(() => this.renderRoot.querySelector<HTMLElement>("ul")?.focus());
    }
  }

  private onKey(e: KeyboardEvent) {
    const all = this.langs();
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      this.active = (this.active + (e.key === "ArrowDown" ? 1 : -1) + all.length) % all.length;
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      this.choose(all[this.active]);
    } else if (e.key === "Escape") {
      this.open = false;
      this.renderRoot.querySelector<HTMLButtonElement>(".btn")?.focus();
    }
  }

  render() {
    const p = currentProject.get();
    const all = this.langs();
    if (!p || !all.length || !location.pathname.startsWith(`/p/${p.slug}`)) return "";
    const cur = contentLocale.get() || p.mainLocale;
    return html`
      <button class="btn" aria-haspopup="listbox" aria-expanded=${this.open ? "true" : "false"} title=${t("lang.switch")}
        aria-label=${`${t("lang.switch")}: ${languageName(cur)}`} @click=${this.toggle}>${icon("Globe", 14)} ${cur.toUpperCase()}</button>
      ${this.open ? html`<ul role="listbox" tabindex="-1" aria-label=${t("lang.switch")} aria-activedescendant=${`lg-${all[this.active]}`} @keydown=${this.onKey}>
        ${all.map((code, i) => html`<li id=${`lg-${code}`} role="option" aria-selected=${code === cur ? "true" : "false"} class=${i === this.active ? "act" : ""}
          @pointerenter=${() => (this.active = i)} @click=${() => this.choose(code)}>
          <span class="code">${code.toUpperCase()}</span>${languageName(code)}${code === p.mainLocale ? html`<span class="main">${t("lang.main")}</span>` : ""}
        </li>`)}</ul>` : ""}`;
  }
}
customElements.define("phx-lang-switcher", PhxLangSwitcher);
