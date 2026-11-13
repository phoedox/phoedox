import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { api, type Project } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject, navigate } from "../state";
import { base } from "./shared";
import { projectBadge } from "../pictures";

/** Project picker in the header: a dropdown with search (prototype "Arbeitsplatz"). */
export class PhxProjectSwitcher extends SignalWatcher(LitElement) {
  static properties = { path: {}, open: { state: true }, projects: { state: true }, query: { state: true }, active: { state: true } };
  static styles = [base, css`
    :host { position: relative; display: block; min-width: 0; }
    .btn { display: flex; align-items: center; gap: 8px; max-width: 280px; padding: 4px 10px 4px 5px; border-radius: 8px;
      border: 1px solid var(--line); background: var(--surface); color: var(--fg); font: 500 14px var(--font-body); cursor: pointer;
      transition: border-color var(--dur-fast) ease; }
    @media (hover: hover) and (pointer: fine) { .btn:hover { border-color: var(--muted); } }
    .btn .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
    .btn .car { color: var(--muted); flex: none; transition: transform var(--dur-fast) var(--ease-out); }
    .btn[aria-expanded="true"] .car { transform: rotate(180deg); }
    .pi { width: 24px; height: 24px; border-radius: 6px; display: grid; place-items: center; color: #fff; font: 600 10px var(--font-mono); flex: none; background: var(--accent); }
    .pi.none { background: var(--surface2); color: var(--muted); }
    .pop { position: absolute; top: calc(100% + 6px); left: 0; width: 320px; max-width: calc(100vw - 32px); z-index: 40; padding: 8px;
      display: flex; flex-direction: column; gap: 6px; background: var(--surface); border: 1px solid var(--line); border-radius: 10px;
      box-shadow: 0 14px 34px rgb(0 0 0 / .18); transform-origin: top left;
      transition: opacity var(--dur-fast) var(--ease-out), transform var(--dur-fast) var(--ease-out);
      @starting-style { opacity: 0; transform: scale(.97); } }
    ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; max-height: 300px; overflow: auto; }
    li { display: grid; grid-template-columns: 24px minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 6px 8px; border-radius: 6px; cursor: pointer; }
    li.act { background: var(--surface2); }
    li[aria-selected="true"] .n { color: var(--accent); }
    .n { font-weight: 500; font-size: 13.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .s { font-size: 11.5px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .k { font-size: 11px; color: var(--muted); }
    .empty { padding: 8px; color: var(--muted); font-size: 13px; }
    .foot { border-top: 1px solid var(--line); padding-top: 6px; display: flex; justify-content: space-between; font-size: 12px; color: var(--muted); }
    .foot a { color: var(--muted); }
    @media (max-width: 640px) { .btn .name { display: none; } .btn { padding: 4px; } }
    @media (prefers-reduced-motion: reduce) { .pop { transition: opacity var(--dur-fast) ease; @starting-style { opacity: 0; transform: none; } } }
  `];
  /** Current location, so the button follows navigation. */
  path = "";
  open = false;
  projects?: Project[];
  query = "";
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

  private async toggle() {
    this.open = !this.open;
    if (!this.open) return;
    this.query = "";
    this.active = 0;
    this.projects = undefined;
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLInputElement>("input")?.focus());
    this.projects = (await api<Project[]>("GET", "/projects").catch(() => [])) ?? [];
    const cur = currentProject.get()?.slug;
    this.active = Math.max(0, this.filtered().findIndex((p) => p.slug === cur));
  }

  private filtered(): Project[] {
    const q = this.query.trim().toLowerCase();
    const all = this.projects ?? [];
    return q ? all.filter((p) => `${p.name} ${p.slug} ${p.short} ${p.description} ${p.orgName ?? ""}`.toLowerCase().includes(q)) : all;
  }

  private go(p: Project) {
    this.open = false;
    navigate(`/p/${p.slug}`);
    this.renderRoot.querySelector<HTMLButtonElement>(".btn")?.focus();
  }

  private onKey(e: KeyboardEvent) {
    const list = this.filtered();
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!list.length) return;
      this.active = (this.active + (e.key === "ArrowDown" ? 1 : -1) + list.length) % list.length;
      this.updateComplete.then(() => this.renderRoot.querySelector("li.act")?.scrollIntoView({ block: "nearest" }));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (list[this.active]) this.go(list[this.active]);
    } else if (e.key === "Escape") {
      this.open = false;
      this.renderRoot.querySelector<HTMLButtonElement>(".btn")?.focus();
    }
  }

  render() {
    const cur = currentProject.get();
    const inProject = location.pathname.startsWith("/p/") && cur;
    const list = this.filtered();
    const role = (r: string) => (["owner", "editor", "reader"].includes(r) ? t(`settings.role.${r as "owner" | "editor" | "reader"}`) : "");
    return html`
      <button class="btn" aria-haspopup="listbox" aria-expanded=${this.open ? "true" : "false"} aria-label=${t("switcher.label")} @click=${this.toggle}>
        ${inProject ? html`${projectBadge(cur, 24)}<span class="name">${cur.name}</span>`
          : html`<span class="pi none">${icon("FolderOpen", 13)}</span><span class="name">${t("switcher.choose")}</span>`}
        <span class="car">${icon("ChevronDown", 14)}</span>
      </button>
      ${this.open ? html`<div class="pop">
        <input type="search" role="combobox" aria-expanded="true" aria-controls="pp-list" aria-activedescendant=${list[this.active] ? `pp-${list[this.active].slug}` : ""}
          placeholder=${t("switcher.search")} aria-label=${t("switcher.search")} .value=${this.query}
          @input=${(e: Event) => { this.query = (e.target as HTMLInputElement).value; this.active = 0; }} @keydown=${this.onKey}>
        ${!this.projects ? html`<div class="empty">${t("app.loading")}</div>`
          : list.length ? html`<ul id="pp-list" role="listbox" aria-label=${t("nav.projects")}>${list.map((p, i) => html`
            <li id="pp-${p.slug}" role="option" aria-selected=${p.slug === cur?.slug ? "true" : "false"} class=${i === this.active ? "act" : ""}
              @pointerenter=${() => (this.active = i)} @click=${() => this.go(p)}>
              ${projectBadge(p, 24)}
              <span><div class="n">${p.name}</div>${p.description || p.orgName ? html`<div class="s">${p.orgName ? `${p.orgName} · ` : ""}${p.description}</div>` : ""}</span>
              <span class="k">${role(p.role)}</span>
            </li>`)}</ul>`
          : html`<div class="empty">${t("switcher.none")}</div>`}
        <div class="foot"><a href="/" @click=${() => (this.open = false)}>${t("switcher.all")}</a><span>${t("switcher.keys")}</span></div>
      </div>` : ""}`;
  }
}
customElements.define("phx-project-switcher", PhxProjectSwitcher);
