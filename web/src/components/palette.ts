import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, type TemplateResult } from "lit";
import { MARK_END, MARK_START, api, type Hit, type Project } from "../api";
import { t } from "../i18n";
import { fileIcon } from "../files";
import { icon } from "../icon";
import { contentLocale, currentProject, currentUser, navigate } from "../state";
import { languageName, rememberLocale } from "../languages";
import { pluginCommands } from "../plugin-host";
import { base } from "./shared";

interface Item {
  id: string;
  label: string;
  icon: string;
  href: string;
  /** Runs instead of navigating (plugin commands). */
  run?: () => void | Promise<void>;
  /** Second line (snippet) and a chip (project). */
  sub?: TemplateResult | string;
  chip?: string;
}

const docIcon: Record<string, string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink" };

/** Renders a server snippet with <mark> around matches, without innerHTML. */
export function marked(snippet: string): TemplateResult[] {
  const out: TemplateResult[] = [];
  for (const [i, part] of snippet.split(MARK_START).entries()) {
    if (i === 0) {
      out.push(html`${part}`);
      continue;
    }
    const [hit, rest = ""] = part.split(MARK_END);
    out.push(html`<mark>${hit}</mark>${rest}`);
  }
  return out;
}

/**
 * Command palette (⌘K / Ctrl+K): actions, project switch and full-text
 * search. Opened by keyboard many times a day, so it has no animation.
 */
export class PhxPalette extends SignalWatcher(LitElement) {
  static properties = { query: { state: true }, hits: { state: true }, active: { state: true }, loading: { state: true }, projects: { state: true } };
  static styles = [base, css`
    dialog { width: min(640px, calc(100vw - 32px)); max-height: min(70vh, 640px); margin: 12vh auto auto; padding: 0;
      border: 1px solid var(--line); border-radius: 14px; background: var(--surface); color: var(--fg); box-shadow: 0 24px 60px rgb(0 0 0 / .25); overflow: hidden; }
    dialog[open] { display: flex; flex-direction: column; }
    dialog::backdrop { background: rgb(0 0 0 / .3); }
    .search { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); color: var(--muted); }
    .search input { border: 0; padding: 4px 0; font-size: 16px; background: none; outline: none; }
    ul { list-style: none; margin: 0; padding: 6px; overflow-y: auto; }
    li.group { font-size: 11px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); font-weight: 600; padding: 10px 10px 4px; }
    li[role="option"] { display: grid; grid-template-columns: 20px minmax(0, 1fr) auto; gap: 2px 10px; align-items: center; padding: 8px 10px; border-radius: 8px; cursor: pointer; }
    li[role="option"][aria-selected="true"] { background: var(--accent-soft); }
    li .ico { color: var(--muted); }
    li .label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    li .sub { grid-column: 2 / 4; font-size: 12.5px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    mark { background: none; color: var(--fg); font-weight: 600; text-decoration: underline; text-decoration-color: var(--accent); text-underline-offset: 2px; }
    .chip { font-size: 11px; }
    .note { padding: 14px 16px; color: var(--muted); font-size: 14px; }
    footer { border-top: 1px solid var(--line); padding: 8px 16px; font-size: 12px; color: var(--muted); }
    @media (max-width: 600px) { dialog { margin-top: 8px; max-height: calc(100vh - 16px); } footer { display: none; } }
  `];
  query = "";
  hits: Hit[] = [];
  active = 0;
  loading = false;
  projects: Project[] = [];
  private seq = 0;
  private timer?: number;

  connectedCallback() {
    super.connectedCallback();
    addEventListener("keydown", this.onGlobalKey);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    removeEventListener("keydown", this.onGlobalKey);
  }

  private onGlobalKey = (e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
      e.preventDefault();
      this.toggle();
    }
  };

  private get dialog() {
    return this.renderRoot.querySelector("dialog")!;
  }

  /** Opens the palette (also used by the search button in the app bar). */
  async open() {
    if (!currentUser.get() || this.dialog.open) return;
    this.query = "";
    this.hits = [];
    this.active = 0;
    this.dialog.showModal();
    this.renderRoot.querySelector("input")?.focus();
    this.projects = (await api<Project[]>("GET", "/projects").catch(() => [])) ?? [];
  }

  toggle() {
    if (this.dialog?.open) this.dialog.close();
    else this.open();
  }

  private onInput(e: InputEvent) {
    this.query = (e.target as HTMLInputElement).value;
    this.active = 0;
    clearTimeout(this.timer);
    const q = this.query.trim();
    if (!q) {
      this.hits = [];
      this.loading = false;
      return;
    }
    this.loading = true;
    this.timer = window.setTimeout(async () => {
      const mine = ++this.seq;
      const hits = (await api<Hit[]>("GET", `/search?q=${encodeURIComponent(q)}&limit=20`).catch(() => [])) ?? [];
      if (mine !== this.seq) return; // a newer query is on its way
      const here = currentProject.get()?.id;
      // Keep the server's ranking, but show the current project first.
      this.hits = [...hits.filter((h) => h.projectId === here), ...hits.filter((h) => h.projectId !== here)];
      this.loading = false;
    }, 120);
  }

  private actions(): Item[] {
    const p = currentProject.get();
    const u = currentUser.get();
    const inProject = p && location.pathname.startsWith(`/p/${p.slug}`);
    const items: Item[] = [];
    if (inProject && p) {
      if (p.role === "editor" || p.role === "owner") {
        items.push({ id: "a-page", label: t("palette.new_page", { project: p.name }), icon: "FilePlus", href: `/p/${p.slug}/new/markdown` });
        items.push({ id: "a-adr", label: t("palette.new_decision", { project: p.name }), icon: "Scale", href: `/p/${p.slug}/new/adr` });
        items.push({ id: "a-link", label: t("palette.new_link", { project: p.name }), icon: "Link", href: `/p/${p.slug}/new/link` });
      }
      items.push({ id: "a-dec", label: t("palette.decisions", { project: p.name }), icon: "ListChecks", href: `/p/${p.slug}/decisions` });
      for (const c of pluginCommands.get().filter((c) => c.project === p.slug)) {
        items.push({ id: `pl-${c.plugin}-${c.id}`, label: c.label, icon: "Puzzle", href: "", run: () => c.run() });
      }
    }
    items.push({ id: "a-projects", label: t("nav.projects"), icon: "FolderOpen", href: "/" });
    items.push({ id: "a-profile", label: t("nav.profile"), icon: "UserRound", href: "/settings/profile" });
    items.push({ id: "a-security", label: t("nav.security"), icon: "ShieldCheck", href: "/settings/security" });
    items.push({ id: "a-tokens", label: t("nav.tokens"), icon: "KeyRound", href: "/settings/tokens" });
    if (u?.isAdmin) items.push({ id: "a-users", label: t("users.title"), icon: "Users", href: "/admin/users" });
    if (u?.isAdmin) items.push({ id: "a-plugins", label: t("plugins.title"), icon: "Puzzle", href: "/admin/plugins" });
    return items;
  }

  /** Visible groups for the current query. */
  private groups(): [string, Item[]][] {
    const q = this.query.trim().toLowerCase();
    const match = (s: string) => !q || s.toLowerCase().includes(q);
    const here = currentProject.get()?.id;
    const actions = this.actions().filter((a) => match(a.label));
    const projects: Item[] = this.projects.filter((p) => p.id !== here && match(p.name))
      .map((p) => ({ id: `p-${p.id}`, label: t("palette.go_project", { project: p.name }), icon: "FolderOpen", href: `/p/${p.slug}` }));
    const results: Item[] = this.hits.map((h) => ({
      id: `h-${h.id}`, label: h.title,
      icon: h.type === "entity" ? "Box" : h.type === "file" ? fileIcon(h.kind) : docIcon[h.kind] ?? "FileText",
      href: h.type === "file" ? `/p/${h.projectSlug}/files?open=${h.id}` : `/p/${h.projectSlug}/${h.type === "entity" ? "e" : "d"}/${h.slug}`,
      // A match in a translation opens the page in that language.
      run: h.locale ? () => { rememberLocale(h.projectSlug, h.locale!); if (h.projectId === here) contentLocale.set(h.locale!); navigate(`/p/${h.projectSlug}/d/${h.slug}`); } : undefined,
      sub: h.snippet ? html`${marked(h.snippet)}` : undefined,
      chip: [h.projectId === here ? "" : h.projectName, h.locale ? languageName(h.locale) : ""].filter(Boolean).join(" · ") || undefined,
    }));
    const out: [string, Item[]][] = [];
    if (results.length) out.push([t("palette.results"), results]);
    if (actions.length) out.push([t("palette.actions"), q ? actions.slice(0, 4) : actions]);
    if (projects.length) out.push([t("palette.projects"), q ? projects.slice(0, 4) : projects.slice(0, 6)]);
    return out;
  }

  private flat() {
    return this.groups().flatMap(([, items]) => items);
  }

  private go(item: Item | undefined) {
    if (!item) return;
    this.dialog.close();
    if (item.run) void item.run();
    else navigate(item.href);
  }

  private onKey(e: KeyboardEvent) {
    const items = this.flat();
    const move = (to: number) => {
      e.preventDefault();
      this.active = (to + items.length) % Math.max(items.length, 1);
      this.updateComplete.then(() => this.renderRoot.querySelector(`#opt-${this.active}`)?.scrollIntoView({ block: "nearest" }));
    };
    if (e.key === "ArrowDown") move(this.active + 1);
    else if (e.key === "ArrowUp") move(this.active - 1);
    else if (e.key === "Enter" && !e.isComposing) {
      e.preventDefault();
      this.go(items[this.active]);
    }
  }

  render() {
    let n = 0;
    const groups = this.groups();
    const q = this.query.trim();
    return html`<dialog aria-label=${t("palette.label")} @click=${(e: Event) => e.target === this.dialog && this.dialog.close()}>
      <div class="search">
        ${icon("Search", 18)}
        <input role="combobox" aria-expanded="true" aria-controls="results" aria-autocomplete="list"
          aria-activedescendant=${`opt-${this.active}`} placeholder=${t("palette.placeholder")} autocomplete="off" spellcheck="false"
          .value=${this.query} @input=${this.onInput} @keydown=${this.onKey} />
      </div>
      <ul id="results" role="listbox" aria-label=${t("palette.label")}>
        ${groups.map(([label, items]) => html`
          <li class="group" role="presentation">${label}</li>
          ${items.map((it) => {
            const i = n++;
            return html`<li role="option" id=${`opt-${i}`} aria-selected=${i === this.active ? "true" : "false"}
              @click=${() => this.go(it)} @mousemove=${() => this.active !== i && (this.active = i)}>
              ${icon(it.icon, 16)}<span class="label">${it.label}</span>${it.chip ? html`<span class="chip">${it.chip}</span>` : html`<span></span>`}
              ${it.sub ? html`<span class="sub">${it.sub}</span>` : ""}
            </li>`;
          })}`)}
      </ul>
      ${q && !this.loading && !this.hits.length && !groups.length ? html`<p class="note">${t("palette.empty")}</p>` : ""}
      ${q && this.loading && !this.hits.length ? html`<p class="note">${t("palette.searching")}</p>` : ""}
      <footer>${t("palette.hint")}</footer>
    </dialog>`;
  }
}
customElements.define("phx-palette", PhxPalette);
