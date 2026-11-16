import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { languageName } from "../languages";
import { currentProject, docsVersion } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Match { n: number; field: string; line: number; before: string; text: string; after: string; replacement?: string }
interface FoundDoc { id: string; slug: string; title: string; type: string; ref?: string; version: number; draft?: boolean; count: number; matches: Match[]; result?: string; reason?: string }
interface FindResult { documents: FoundDoc[]; total: number; scanned: number; truncated?: boolean; replaced?: number }
interface DocItem { id: string; title: string; type: string; category?: string; parentId?: string }

const typeIcon: Record<string, string> = { markdown: "FileText", adr: "Scale", link: "ExternalLink", board: "SquareKanban", floorplan: "LandPlot", whiteboard: "Presentation" };

/** Find (and replace) text across one, several or all documents of a project. */
export class PhxFindReplace extends SignalWatcher(LitElement) {
  static properties = {
    project: {}, find: { state: true }, replace: { state: true }, opts: { state: true }, scope: { state: true }, picked: { state: true },
    lang: { state: true }, docs: { state: true }, filter: { state: true }, res: { state: true }, off: { state: true }, busy: { state: true },
    confirming: { state: true }, done: { state: true },
  };
  static styles = [base, css`
    :host { display: block; }
    input[type="checkbox"] { width: auto; flex: none; margin: 0; }
    .lead { color: var(--muted); max-width: 70ch; margin: 4px 0 16px; }
    form { display: grid; gap: 12px; max-width: 900px; }
    .fields { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    @media (max-width: 700px) { .fields { grid-template-columns: 1fr; } }
    label.f { display: grid; gap: 4px; font-size: 13px; color: var(--muted); }
    .opts { display: flex; flex-wrap: wrap; gap: 6px 16px; font-size: 14px; }
    .opts label { display: inline-flex; gap: 6px; align-items: center; }
    .scope { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
    .picker { border: 1px solid var(--line); border-radius: 10px; background: var(--surface); padding: 8px; display: grid; gap: 6px; }
    .picker .list { max-height: 220px; overflow: auto; display: grid; gap: 2px; }
    .picker label { display: flex; gap: 8px; align-items: center; font-size: 14px; padding: 3px 4px; border-radius: 6px; color: var(--fg); }
    .picker .cat { color: var(--muted); font-size: 12px; margin-left: auto; }
    .acts { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .sum { margin: 18px 0 8px; color: var(--muted); font-size: 14px; }
    .doc { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); margin-bottom: 12px; overflow: hidden; }
    .doc header { display: flex; gap: 10px; align-items: center; padding: 9px 12px; background: var(--surface2); }
    .doc header a { color: var(--fg); font-weight: 600; text-decoration: none; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .doc header .n { margin-left: auto; color: var(--muted); font-size: 13px; white-space: nowrap; }
    .doc ul { list-style: none; margin: 0; padding: 0; }
    .doc li { display: grid; grid-template-columns: auto auto minmax(0, 1fr); gap: 10px; align-items: baseline; padding: 7px 12px; border-top: 1px solid var(--line); font-size: 14px; }
    .doc li .where { color: var(--muted); font-size: 12px; font-family: var(--font-mono); white-space: nowrap; }
    .doc li .ctx { font-family: var(--font-mono); font-size: 13px; overflow-wrap: anywhere; }
    mark { background: var(--tb-yellow); color: var(--fg); border-radius: 3px; padding: 0 1px; }
    del { background: var(--tb-red); color: var(--tc-red); text-decoration: line-through; border-radius: 3px; }
    ins { background: var(--tb-green); color: var(--tc-green); text-decoration: none; border-radius: 3px; }
    .chip.applied { background: var(--tb-green); color: var(--tc-green); } .chip.suggested { background: var(--tb-blue); color: var(--tc-blue); }
    .chip.skipped, .chip.failed { background: var(--tb-red); color: var(--tc-red); }
    .confirm { border: 1px solid color-mix(in srgb, var(--bad) 40%, var(--line)); border-radius: 10px; padding: 12px; display: grid; gap: 10px; margin-top: 12px; max-width: 900px; }
    .more { color: var(--muted); font-size: 13px; padding: 7px 12px; border-top: 1px solid var(--line); }
  `];

  project = "";
  find = "";
  replace = "";
  opts = { caseSensitive: false, wholeWord: false, regex: false, titles: false };
  scope: "all" | "some" = "all";
  picked: string[] = [];
  lang = "";
  docs: DocItem[] = [];
  filter = "";
  res?: FindResult;
  /** Matches left out: "docId:n", or "docId:*" for a whole document. */
  off = new Set<string>();
  busy = false;
  confirming = false;
  done?: FindResult;
  private loaded = "";

  protected willUpdate() {
    if (this.project && this.loaded !== this.project) {
      this.loaded = this.project;
      const doc = new URLSearchParams(location.search).get("doc");
      if (doc) {
        this.scope = "some";
        this.picked = [doc];
      }
      void api<DocItem[]>("GET", `/projects/${this.project}/documents`)
        .then((d) => (this.docs = d.filter((x) => !x.parentId && ["markdown", "adr", "link"].includes(x.type)).sort((a, b) => a.title.localeCompare(b.title)))).catch(() => {});
    }
  }

  private body(apply: boolean) {
    const only: Record<string, number[]> = {};
    const versions: Record<string, number> = {};
    if (apply && this.res) {
      for (const d of this.res.documents) {
        versions[d.id] = d.version;
        if (this.off.has(`${d.id}:*`)) only[d.id] = [];
        else if (d.matches.some((m) => this.off.has(`${d.id}:${m.n}`))) {
          only[d.id] = d.matches.filter((m) => !this.off.has(`${d.id}:${m.n}`)).map((m) => m.n);
        }
      }
    }
    return {
      find: this.find, ...this.opts, lang: this.lang || undefined,
      documents: this.scope === "some" ? this.picked : undefined,
      replace: this.replace !== "" || apply ? this.replace : undefined,
      apply, only: apply ? only : undefined, versions: apply ? versions : undefined,
    };
  }

  private async run(e?: Event) {
    e?.preventDefault();
    if (!this.find || (this.scope === "some" && !this.picked.length)) return;
    this.busy = true;
    this.done = undefined;
    this.confirming = false;
    try {
      this.res = await api<FindResult>("POST", `/projects/${this.project}/find`, this.body(false));
      this.off = new Set();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async apply() {
    this.busy = true;
    try {
      const r = await api<FindResult>("POST", `/projects/${this.project}/find`, this.body(true));
      this.done = r;
      this.confirming = false;
      toast(t("replace.done", { count: r.replaced ?? 0 }));
      docsVersion.set(docsVersion.get() + 1);
      this.res = undefined;
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private toggle(key: string) {
    const off = new Set(this.off);
    if (off.has(key)) off.delete(key);
    else off.add(key);
    this.off = off;
  }

  private selectedCount() {
    if (!this.res) return 0;
    let n = 0;
    for (const d of this.res.documents) {
      if (this.off.has(`${d.id}:*`)) continue;
      n += d.count - d.matches.filter((m) => this.off.has(`${d.id}:${m.n}`)).length;
    }
    return n;
  }

  private matchView(d: FoundDoc, m: Match, canReplace: boolean) {
    const key = `${d.id}:${m.n}`;
    const on = !this.off.has(key) && !this.off.has(`${d.id}:*`);
    const showRepl = canReplace && this.replace !== "" && on;
    return html`<li>
      ${canReplace ? html`<input type="checkbox" .checked=${on} ?disabled=${this.off.has(`${d.id}:*`)} aria-label=${t("replace.include_match", { n: m.n })} @change=${() => this.toggle(key)}>` : html`<span></span>`}
      <span class="where">${m.field === "markdown" ? "" : t(`replace.field.${m.field}` as any) + " · "}${t("replace.line", { line: m.line })}</span>
      <span class="ctx">${m.before}${showRepl ? html`<del>${m.text}</del><ins>${m.replacement}</ins>` : html`<mark>${m.text}</mark>`}${m.after}</span>
    </li>`;
  }

  private docView(d: FoundDoc, canReplace: boolean) {
    const whole = this.off.has(`${d.id}:*`);
    return html`<section class="doc" aria-label=${d.title}>
      <header>
        ${canReplace ? html`<input type="checkbox" .checked=${!whole} aria-label=${t("replace.include_doc", { title: d.title })} @change=${() => this.toggle(`${d.id}:*`)}>` : nothing}
        ${icon(typeIcon[d.type] ?? "FileText", 15)}
        <a href="/p/${this.project}/d/${d.id}">${d.ref ? `${d.ref} · ` : ""}${d.title}</a>
        ${d.draft ? html`<span class="chip" title=${t("replace.draft_hint")}>${t("replace.draft")}</span>` : nothing}
        ${d.result ? html`<span class="chip ${d.result}" title=${d.reason ?? ""}>${t(`replace.result.${d.result}` as any)}</span>` : nothing}
        <span class="n">${t("replace.matches", { count: d.count })}</span>
      </header>
      ${d.matches.length ? html`<ul>${d.matches.map((m) => this.matchView(d, m, canReplace))}</ul>` : nothing}
      ${d.count > d.matches.length && !d.result ? html`<div class="more">${t("replace.more", { count: d.count - d.matches.length })}</div>` : nothing}
    </section>`;
  }

  render() {
    const p = currentProject.get();
    const canReplace = ["editor", "owner"].includes(p?.role ?? "");
    const langs = p?.mainLocale ? p.locales : [];
    const shownDocs = this.docs.filter((d) => !this.filter || d.title.toLowerCase().includes(this.filter.toLowerCase()));
    const n = this.selectedCount();
    return html`
      <h1>${t(canReplace ? "replace.title" : "replace.title_find")}</h1>
      <p class="lead">${t("replace.lead")}</p>
      <form @submit=${this.run}>
        <div class="fields">
          <label class="f">${t("replace.find")}<input type="text" .value=${this.find} required autofocus spellcheck="false"
            @input=${(e: Event) => (this.find = (e.target as HTMLInputElement).value)}></label>
          ${canReplace ? html`<label class="f">${t("replace.with")}<input type="text" .value=${this.replace} spellcheck="false" placeholder=${t("replace.with_hint")}
            @input=${(e: Event) => (this.replace = (e.target as HTMLInputElement).value)}></label>` : nothing}
        </div>
        <div class="opts">
          ${(["caseSensitive", "wholeWord", "regex", "titles"] as const).map((k) => html`<label><input type="checkbox" .checked=${this.opts[k]}
            @change=${(e: Event) => (this.opts = { ...this.opts, [k]: (e.target as HTMLInputElement).checked })}> ${t(`replace.opt.${k}` as any)}</label>`)}
        </div>
        <div class="scope">
          <label class="f">${t("replace.scope")}
            <select @change=${(e: Event) => (this.scope = (e.target as HTMLSelectElement).value as "all" | "some")}>
              <option value="all" ?selected=${this.scope === "all"}>${t("replace.scope_all")}</option>
              <option value="some" ?selected=${this.scope === "some"}>${t("replace.scope_some")}</option>
            </select></label>
          ${langs.length ? html`<label class="f">${t("replace.lang")}
            <select @change=${(e: Event) => (this.lang = (e.target as HTMLSelectElement).value)}>
              <option value="" ?selected=${!this.lang}>${languageName(p!.mainLocale)} (${t("replace.lang_main")})</option>
              ${langs.map((l) => html`<option value=${l} ?selected=${this.lang === l}>${languageName(l)}</option>`)}
            </select></label>` : nothing}
        </div>
        ${this.scope === "some" ? html`<div class="picker">
          <input type="search" placeholder=${t("replace.filter_docs")} aria-label=${t("replace.filter_docs")} .value=${this.filter} @input=${(e: Event) => (this.filter = (e.target as HTMLInputElement).value)}>
          <div class="list">${shownDocs.map((d) => html`<label><input type="checkbox" .checked=${this.picked.includes(d.id)}
            @change=${(e: Event) => (this.picked = (e.target as HTMLInputElement).checked ? [...this.picked, d.id] : this.picked.filter((x) => x !== d.id))}>
            ${icon(typeIcon[d.type] ?? "FileText", 14)} ${d.title}<span class="cat">${d.category ?? ""}</span></label>`)}</div>
          <span class="muted" style="font-size:13px">${t("replace.picked", { count: this.picked.length })}</span>
        </div>` : nothing}
        <div class="acts">
          <button type="submit" ?disabled=${this.busy || !this.find || (this.scope === "some" && !this.picked.length)}>${icon("Search", 15)} ${t("replace.find_button")}</button>
          ${canReplace && this.res && this.res.total > 0 ? html`<button type="button" class="ghost" ?disabled=${this.busy || n === 0}
            @click=${() => (this.confirming = true)}>${icon("Replace", 15)} ${t("replace.replace_button", { count: n })}</button>` : nothing}
        </div>
      </form>
      ${this.confirming && this.res ? html`<div class="confirm" role="alertdialog" aria-label=${t("replace.confirm_title")}>
        <b>${t("replace.confirm_title")}</b>
        <span>${t("replace.confirm", { count: n, docs: this.res.documents.filter((d) => !this.off.has(`${d.id}:*`)).length, find: this.find, replace: this.replace || t("replace.nothing") })}</span>
        <div class="acts"><button class="danger" ?disabled=${this.busy} @click=${this.apply}>${icon("ReplaceAll", 15)} ${t("replace.confirm_button")}</button>
          <button class="ghost" @click=${() => (this.confirming = false)}>${t("doc.cancel")}</button></div>
      </div>` : nothing}
      ${this.done ? html`<p class="sum" role="status">${t("replace.done_summary", { count: this.done.replaced ?? 0, docs: this.done.documents.filter((d) => d.result === "applied" || d.result === "suggested").length })}</p>
        ${this.done.documents.map((d) => this.docView({ ...d, matches: [] }, false))}` : nothing}
      ${this.res ? html`<p class="sum" role="status">${this.res.total
        ? t("replace.summary", { count: this.res.total, docs: this.res.documents.length, scanned: this.res.scanned })
        : t("replace.none", { scanned: this.res.scanned })}${this.res.truncated ? ` ${t("replace.truncated")}` : ""}</p>
        ${this.res.documents.map((d) => this.docView(d, canReplace))}` : nothing}
    `;
  }
}
customElements.get("phx-find-replace") || customElements.define("phx-find-replace", PhxFindReplace);
