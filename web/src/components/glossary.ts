// Project settings → Glossary: terms with their translation per language,
// terms kept untranslated and spellings to avoid (style guide).
import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { languageName } from "../languages";
import { settingsUI } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Entry { term: string; translations?: Record<string, string>; keep?: boolean; avoid?: Record<string, string[]>; note?: string }

const csv = (v: string) => v.split(",").map((x) => x.trim()).filter(Boolean);

export class PhxGlossary extends LitElement {
  static properties = { project: {}, locales: { attribute: false }, mainLocale: {}, canEdit: { type: Boolean }, entries: { state: true }, dirty: { state: true }, q: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; }
    .tools { display: flex; gap: 8px; align-items: center; padding: 0 18px 12px; flex-wrap: wrap; }
    .tools input { max-width: 260px; }
    .wrap { overflow-x: auto; padding: 0 18px 12px; }
    table { border-collapse: collapse; width: 100%; font-size: 13.5px; }
    th { text-align: left; font-size: 12px; font-weight: 500; color: var(--muted); padding: 6px; white-space: nowrap; }
    td { padding: 4px 6px; border-top: 1px solid var(--surface2); vertical-align: middle; }
    td input:not([type=checkbox]) { padding: 6px 8px; font-size: 13px; min-width: 120px; }
    td input[type=checkbox] { width: auto; }
    .empty { color: var(--muted); font-size: 13.5px; padding: 0 18px 12px; margin: 0; }
  `];

  project = "";
  locales: string[] = [];
  mainLocale = "";
  canEdit = false;
  entries: Entry[] = [];
  dirty = false;
  q = "";

  willUpdate(ch: Map<string, unknown>) {
    if (ch.has("project") && this.project) void api<Entry[]>("GET", `/projects/${this.project}/glossary`).then((e) => (this.entries = e)).catch(() => undefined);
  }

  private set(i: number, p: Partial<Entry>) {
    this.entries = this.entries.map((e, j) => (j === i ? { ...e, ...p } : e));
    this.dirty = true;
  }

  private async save() {
    try {
      this.entries = await api<Entry[]>("PUT", `/projects/${this.project}/glossary`, { entries: this.entries.filter((e) => e.term.trim()) });
      this.dirty = false;
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    }
  }

  render() {
    const ro = !this.canEdit;
    const q = this.q.toLowerCase();
    const rows = this.entries.map((e, i) => ({ e, i })).filter(({ e }) => !q || e.term.toLowerCase().includes(q) || Object.values(e.translations ?? {}).some((v) => v.toLowerCase().includes(q)));
    return html`<div class="tools">
        <input type="search" placeholder=${t("glossary.search")} .value=${this.q} @input=${(e: Event) => (this.q = (e.target as HTMLInputElement).value)}>
        ${this.canEdit ? html`<button class="ghost" @click=${() => { this.entries = [{ term: "" }, ...this.entries]; this.dirty = true; }}>${icon("Plus", 14)} ${t("glossary.add")}</button>` : nothing}
        ${this.dirty ? html`<button @click=${this.save}>${icon("Save", 14)} ${t("doc.save")}</button>` : nothing}
      </div>
      ${this.entries.length ? html`<div class="wrap"><table>
        <thead><tr><th>${t("glossary.term")}${this.mainLocale ? ` (${languageName(this.mainLocale)})` : ""}</th>
          ${this.locales.map((l) => html`<th>${languageName(l)}</th>`)}
          <th>${t("glossary.keep")}</th><th>${t("glossary.avoid")}</th><th>${t("glossary.note")}</th><th></th></tr></thead>
        <tbody>${rows.map(({ e, i }) => html`<tr>
          <td><input .value=${e.term} ?disabled=${ro} aria-label=${t("glossary.term")} @input=${(ev: Event) => this.set(i, { term: (ev.target as HTMLInputElement).value })}></td>
          ${this.locales.map((l) => html`<td><input .value=${e.translations?.[l] ?? ""} ?disabled=${ro || e.keep} aria-label=${languageName(l)}
            @input=${(ev: Event) => this.set(i, { translations: { ...(e.translations ?? {}), [l]: (ev.target as HTMLInputElement).value } })}></td>`)}
          <td><input type="checkbox" .checked=${!!e.keep} ?disabled=${ro} aria-label=${t("glossary.keep")} @change=${(ev: Event) => this.set(i, { keep: (ev.target as HTMLInputElement).checked })}></td>
          <td><input .value=${(e.avoid?.[""] ?? []).join(", ")} ?disabled=${ro} placeholder="email" aria-label=${t("glossary.avoid")}
            @change=${(ev: Event) => this.set(i, { avoid: { ...(e.avoid ?? {}), "": csv((ev.target as HTMLInputElement).value) } })}></td>
          <td><input .value=${e.note ?? ""} ?disabled=${ro} aria-label=${t("glossary.note")} @input=${(ev: Event) => this.set(i, { note: (ev.target as HTMLInputElement).value })}></td>
          <td>${this.canEdit ? html`<button class="danger" aria-label=${t("doc.delete")} @click=${() => { this.entries = this.entries.filter((_, j) => j !== i); this.dirty = true; }}>${icon("Trash2", 13)}</button>` : nothing}</td>
        </tr>`)}</tbody></table></div>` : html`<p class="empty">${t("glossary.empty")}</p>`}`;
  }
}
customElements.get("phx-glossary") || customElements.define("phx-glossary", PhxGlossary);
