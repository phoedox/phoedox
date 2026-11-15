import { LitElement, css, html } from "lit";
import { ApiError, api, type CodeItem, type CodeLink, type CodeRepo } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

const kindIcon = { commit: "GitCommitHorizontal", pull: "GitPullRequest", release: "Tag" } as const;

/** Commits, pull requests and releases of a task card (ADR-0063). */
export class PhxTaskCode extends LitElement {
  static properties = {
    project: {}, task: {}, canEdit: { type: Boolean }, links: { state: true }, repos: { state: true },
    adding: { state: true }, results: { state: true }, busy: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    h3 { font-size: 13px; margin: 4px 0 6px; color: var(--muted); font-weight: 600; display: flex; gap: 6px; align-items: center; }
    ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 4px; }
    li { display: flex; gap: 8px; align-items: center; font-size: 13px; padding: 5px 8px; border-radius: 8px; background: var(--surface2); }
    li a { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg); }
    li code { font-size: 12px; }
    li .auto { font-size: 11px; color: var(--muted); }
    li button { padding: 2px; }
    .add { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; margin-top: 6px; }
    .add select, .add input { width: auto; flex: 1; min-width: 120px; }
    .results { display: grid; gap: 2px; margin-top: 6px; max-height: 220px; overflow: auto; }
    .results button { justify-content: flex-start; text-align: left; font-weight: 400; background: none; border: 1px solid var(--line); color: var(--fg); gap: 8px; }
    .results button small { color: var(--muted); margin-left: auto; white-space: nowrap; }
    p.hint { font-size: 12px; margin: 4px 0 0; }
  `];
  project = "";
  task = "";
  canEdit = false;
  links: CodeLink[] = [];
  repos: CodeRepo[] = [];
  /** Which kind is being added ("" = none). */
  adding: "" | "commit" | "pull" | "release" = "";
  results: CodeItem[] = [];
  busy = false;
  private repo = "";

  async connectedCallback() {
    super.connectedCallback();
    this.repos = (await api<CodeRepo[]>("GET", `/projects/${this.project}/code/repos`).catch(() => [])) ?? [];
    this.repo = this.repos[0]?.id ?? "";
    if (this.repos.length) this.links = (await api<CodeLink[]>("GET", `/projects/${this.project}/tasks/${this.task}/code`).catch(() => [])) ?? [];
  }

  private changed() {
    this.dispatchEvent(new CustomEvent("changed", { detail: this.links, bubbles: true, composed: true }));
  }

  private async search(q: string) {
    if (!this.repo) return;
    this.busy = true;
    try {
      this.results = this.adding === "release"
        ? (await api<CodeItem[]>("GET", `/projects/${this.project}/code/repos/${this.repo}/releases`)) ?? []
        : (await api<CodeItem[]>("GET", `/projects/${this.project}/code/repos/${this.repo}/commits?q=${encodeURIComponent(q)}`)) ?? [];
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async link(kind: string, ref: string) {
    this.busy = true;
    try {
      this.links = (await api<CodeLink[]>("POST", `/projects/${this.project}/tasks/${this.task}/code`, { repo: this.repo, kind, ref })) ?? [];
      this.adding = "";
      this.results = [];
      this.changed();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async unlink(l: CodeLink) {
    try {
      await api("DELETE", `/projects/${this.project}/tasks/${this.task}/code/${l.id}`);
      this.links = this.links.filter((x) => x.id !== l.id);
      this.changed();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  private start(kind: "commit" | "pull" | "release") {
    this.adding = kind;
    this.results = [];
    if (kind !== "pull") void this.search("");
  }

  private form() {
    const kind = this.adding;
    if (!kind) return "";
    return html`<div class="add">
      ${this.repos.length > 1 ? html`<select aria-label=${t("code.repo")} @change=${(e: Event) => { this.repo = (e.target as HTMLSelectElement).value; if (kind !== "pull") void this.search(""); }}>
        ${this.repos.map((r) => html`<option value=${r.id} ?selected=${r.id === this.repo}>${r.owner}/${r.name}</option>`)}</select>` : ""}
      ${kind === "commit" ? html`<input type="search" placeholder=${t("code.search_commit")} aria-label=${t("code.search_commit")}
          @keydown=${(e: KeyboardEvent) => { if (e.key === "Enter") { e.preventDefault(); void this.search((e.target as HTMLInputElement).value); } }}>` : ""}
      ${kind === "pull" ? html`<input inputmode="numeric" placeholder=${t("code.pull_number")} aria-label=${t("code.pull_number")}
          @keydown=${(e: KeyboardEvent) => { if (e.key === "Enter") { e.preventDefault(); void this.link("pull", (e.target as HTMLInputElement).value); } }}>` : ""}
      <button type="button" class="ghost" @click=${() => { this.adding = ""; this.results = []; }}>${t("doc.cancel")}</button>
    </div>
    ${this.busy ? html`<p class="muted hint">${t("app.loading")}</p>` : ""}
    ${this.results.length ? html`<div class="results" role="listbox" aria-label=${t(kind === "release" ? "code.releases" : "code.commits")}>
      ${this.results.map((r) => html`<button type="button" role="option" @click=${() => this.link(kind, r.ref)}>
        ${icon(kindIcon[kind], 14)} ${kind === "commit" ? html`<code>${r.ref.slice(0, 7)}</code>` : html`<b>${r.ref}</b>`} ${r.title}
        ${r.at ? html`<small>${formatDate(r.at)}</small>` : ""}</button>`)}</div>`
      : !this.busy && kind !== "pull" ? html`<p class="muted hint">${t("code.nothing_found")}</p>` : ""}`;
  }

  render() {
    if (!this.repos.length) return "";
    return html`<h3>${icon("GitBranch", 13)} ${t("code.title")}</h3>
      ${this.links.length ? html`<ul>${this.links.map((l) => html`<li>
        ${icon(kindIcon[l.kind], 14)}
        <a href=${l.url ?? "#"} target="_blank" rel="noopener external" title=${l.title ?? l.ref}>${l.kind === "commit" ? html`<code>${l.ref.slice(0, 7)}</code>` : l.kind === "pull" ? html`#${l.ref}` : html`<b>${l.ref}</b>`} ${l.title ?? ""}</a>
        ${this.repos.length > 1 ? html`<span class="auto">${l.repo}</span>` : ""}
        ${l.auto ? html`<span class="auto" title=${t("code.auto_hint")}>${t("code.auto")}</span>` : ""}
        ${this.canEdit ? html`<button type="button" class="ghost icon" title=${t("code.unlink")} aria-label=${t("code.unlink")} @click=${() => this.unlink(l)}>${icon("X", 13)}</button>` : ""}
      </li>`)}</ul>` : html`<p class="muted hint">${t("code.empty")}</p>`}
      ${this.canEdit && !this.adding ? html`<div class="add">
        <button type="button" class="ghost" @click=${() => this.start("commit")}>${icon("GitCommitHorizontal", 14)} ${t("code.add_commit")}</button>
        <button type="button" class="ghost" @click=${() => this.start("pull")}>${icon("GitPullRequest", 14)} ${t("code.add_pull")}</button>
        <button type="button" class="ghost" @click=${() => this.start("release")}>${icon("Tag", 14)} ${t("code.add_release")}</button>
      </div>` : ""}
      ${this.form()}`;
  }
}
customElements.define("phx-task-code", PhxTaskCode);
