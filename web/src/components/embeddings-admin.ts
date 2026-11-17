// Semantic search (ADR-0081): system admins choose an embedding model of an
// OpenAI-compatible provider of the instance, or switch it off. Search also
// survives typos without it.

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { scard, settingsUI, srow, sswitch } from "./settings-ui";
import { base } from "./shared";
import { toast } from "./toasts";

interface Status { enabled: boolean; providerId: string; model: string; providers: { id: string; name: string }[]; have: number; total: number; lastError?: string }

export class PhxEmbeddingsAdmin extends LitElement {
  static properties = { s: { state: true }, models: { state: true }, busy: { state: true } };
  static styles = [base, settingsUI, css`
    :host { display: block; margin-top: 16px; }
    .body { padding: 0 18px 16px; display: grid; gap: 8px; }
    .muted { color: var(--muted); font-size: 13px; }
    .bad { color: var(--bad); font-size: 13px; }
    progress { width: 100%; }
  `];
  s?: Status;
  models: string[] = [];
  busy = false;

  connectedCallback() {
    super.connectedCallback();
    void this.load();
  }

  private async load() {
    this.s = await api<Status>("GET", "/admin/search/embeddings").catch(() => undefined);
    if (this.s?.providerId) void this.discover(this.s.providerId);
  }

  private async discover(id: string) {
    this.models = ((await api<string[]>("POST", `/admin/ai/providers/${id}/models`).catch(() => [])) ?? []).filter((m) => /embed/i.test(m));
  }

  private async save(patch: Partial<Status>) {
    const s = { ...this.s!, ...patch };
    this.busy = true;
    try {
      this.s = await api<Status>("PUT", "/admin/search/embeddings", { enabled: s.enabled, providerId: s.providerId, model: s.model });
      toast(t("doc.saved"));
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    const s = this.s;
    if (!s) return nothing;
    const pid = s.providerId || s.providers[0]?.id || "";
    return scard(t("semsearch.title"), t("semsearch.intro"), html`
      ${s.providers.length ? html`
        ${srow(t("semsearch.provider"), t("semsearch.provider_help"), html`<select aria-label=${t("semsearch.provider")} ?disabled=${this.busy}
            @change=${(e: Event) => { const v = (e.target as HTMLSelectElement).value; this.s = { ...s, providerId: v }; void this.discover(v); }}>
          ${s.providers.map((p) => html`<option value=${p.id} ?selected=${p.id === pid}>${p.name}</option>`)}</select>`)}
        ${srow(t("semsearch.model"), t("semsearch.model_help"), html`<input list="embed-models" .value=${s.model} placeholder="text-embedding-3-small" aria-label=${t("semsearch.model")}
            @change=${(e: Event) => (this.s = { ...s, model: (e.target as HTMLInputElement).value.trim() })}>
          <datalist id="embed-models">${this.models.map((m) => html`<option value=${m}></option>`)}</datalist>`)}
        ${srow(t("semsearch.enabled"), t("semsearch.enabled_help"), sswitch(s.enabled, t("semsearch.enabled"), (on) => void this.save({ enabled: on, providerId: pid }), this.busy || (!s.enabled && !s.model)))}
        <div class="body">
          ${s.enabled && s.model ? html`<progress max=${s.total || 1} value=${s.have}></progress>
            <span class="muted">${t("semsearch.progress", { have: s.have, total: s.total })}</span>` : nothing}
          ${s.lastError ? html`<span class="bad">${icon("TriangleAlert", 13)} ${s.lastError}</span>` : nothing}
          ${!s.enabled && s.model ? html`<div><button class="ghost" ?disabled=${this.busy} @click=${() => void this.save({ providerId: pid })}>${t("doc.save")}</button></div>` : nothing}
        </div>`
      : html`<div class="body"><p class="muted">${t("semsearch.no_provider")}</p></div>`}
      <div class="body"><p class="muted">${icon("SpellCheck", 13)} ${t("semsearch.typos")}</p></div>`);
  }
}
customElements.get("phx-embeddings-admin") || customElements.define("phx-embeddings-admin", PhxEmbeddingsAdmin);
