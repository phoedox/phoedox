import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

interface Ref { slug: string; name: string; kind: string }
interface Dup { a: Ref; b: Ref; reason: "mac" | "serial" | "hostname"; value: string }

/** Entries that look like the same device, with one-click merging. */
export class PhxDuplicates extends LitElement {
  static properties = { project: {}, dups: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    :host([hidden]) { display: none; }
    .box { border: 1px solid var(--signal); background: color-mix(in srgb, var(--signal) 8%, var(--surface)); border-radius: 12px; padding: 10px 14px; margin-bottom: 14px; }
    h2 { font-size: 15px; margin: 0 0 6px; display: flex; gap: 6px; align-items: center; }
    ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
    li { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; font-size: 14px; }
    .why { color: var(--muted); font-size: 12.5px; }
    button { padding: 2px 9px; font-size: 12.5px; }
  `];
  project = "";
  dups: Dup[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    this.hidden = true;
    void this.load();
  }

  private async load() {
    this.dups = (await api<Dup[]>("GET", `/projects/${this.project}/duplicates`).catch(() => [])) ?? [];
    this.hidden = this.dups.length === 0;
  }

  private async merge(keep: Ref, remove: Ref) {
    if (!confirm(t("dup.confirm", { keep: keep.name, remove: remove.name }))) return;
    this.busy = true;
    try {
      await api("POST", `/projects/${this.project}/entities-merge`, { keep: keep.slug, remove: remove.slug });
      toast(t("dup.done", { name: keep.name }));
      this.dispatchEvent(new CustomEvent("changed", { bubbles: true, composed: true }));
      await this.load();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    if (!this.dups.length) return html``;
    const edit = ["editor", "owner"].includes(currentProject.get()?.role ?? "");
    const link = (r: Ref) => html`<a href="/p/${this.project}/e/${r.slug}">${r.name}</a>`;
    return html`<div class="box" role="region" aria-label=${t("dup.title")}>
      <h2>${icon("Copy", 15)} ${t("dup.title")}</h2>
      <ul>${this.dups.map((d) => html`<li>${link(d.a)} ↔ ${link(d.b)}
        <span class="why">${t(`dup.reason.${d.reason}` as any, { value: d.value })}</span>
        ${edit ? html`<button class="ghost" ?disabled=${this.busy} @click=${() => this.merge(d.a, d.b)}>${t("dup.keep", { name: d.a.name })}</button>
          <button class="ghost" ?disabled=${this.busy} @click=${() => this.merge(d.b, d.a)}>${t("dup.keep", { name: d.b.name })}</button>` : ""}</li>`)}</ul>
    </div>`;
  }
}
customElements.define("phx-duplicates", PhxDuplicates);
