import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";
import { toast } from "./toasts";

export interface AIPrice { input: number; output: number; cacheRead: number; cacheWrite: number }
export interface AIModel extends AIPrice { id: string; label?: string; charge?: AIPrice }
type PriceKey = keyof AIPrice;
const PRICE_LABEL = { input: "ai_providers.price_in", output: "ai_providers.price_out", cacheRead: "ai_providers.price_cache_read", cacheWrite: "ai_providers.price_cache_write" } as const;
export interface AIProvider {
  id: string; orgId?: string; name: string; kind: "anthropic" | "openai"; baseUrl: string; hasKey: boolean; builtIn?: boolean;
  effort?: string; models: AIModel[]; enabled: boolean;
}

/** Presets for the form; prices of other providers come from their price lists. */
export const PRESETS: { key: string; name: string; kind: AIProvider["kind"]; url: string; effort?: string; models?: AIModel[] }[] = [
  { key: "anthropic", name: "Anthropic", kind: "anthropic", url: "", effort: "medium", models: [
    { id: "claude-opus-5-5", input: 4, output: 20, cacheRead: 0.2, cacheWrite: 5 },
    { id: "claude-sonnet-5-5", input: 2, output: 10, cacheRead: 0.1, cacheWrite: 2.5 },
    { id: "claude-haiku-5-5", input: 0.1, output: 0.5, cacheRead: 0.01, cacheWrite: 0.125 },
  ] },
  { key: "openai", name: "OpenAI", kind: "openai", url: "https://api.openai.com/v1" },
  { key: "mistral", name: "Mistral", kind: "openai", url: "https://api.mistral.ai/v1" },
  { key: "kimi", name: "Kimi (Moonshot)", kind: "openai", url: "https://api.moonshot.ai/v1" },
  { key: "openrouter", name: "OpenRouter", kind: "openai", url: "https://openrouter.ai/api/v1" },
  { key: "ollama", name: "Ollama", kind: "openai", url: "http://localhost:11434/v1" },
  { key: "gateway", name: "", kind: "openai", url: "" },
];

interface Draft { id: string; name: string; kind: AIProvider["kind"]; baseUrl: string; apiKey: string; keepKey: boolean; effort: string; models: AIModel[]; enabled: boolean }

const emptyModel = (id = ""): AIModel => ({ id, input: 0, output: 0, cacheRead: 0, cacheWrite: 0 });

/** Model providers of the instance (org "") or an organization (ADR-0067). */
export class PhxAIProviders extends LitElement {
  static properties = { org: {}, list: { state: true }, draft: { state: true }, found: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: grid; gap: 10px; }
    .prov { border: 1px solid var(--line); border-radius: 10px; padding: 10px 12px; display: grid; gap: 6px; background: var(--surface); }
    .prov header { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .prov header strong { flex: 1; min-width: 0; }
    .prov header button { padding: 4px 8px; }
    .meta { font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
    .models { display: flex; gap: 4px; flex-wrap: wrap; }
    form { border: 1px solid var(--line); border-radius: 10px; padding: 12px; display: grid; gap: 10px; background: var(--surface); }
    .grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    .model { border: 1px solid var(--line); border-radius: 8px; padding: 8px; display: grid; gap: 6px; background: var(--bg); }
    .model .head { display: grid; grid-template-columns: 1fr 1fr auto; gap: 6px; }
    .model input { padding: 5px 6px; font-size: 13px; }
    .model button { padding: 4px 6px; }
    .prices { display: grid; grid-template-columns: 110px repeat(4, minmax(0, 1fr)); gap: 6px; align-items: end; }
    .prices .what { font-size: 12px; color: var(--muted); align-self: center; }
    label.price { display: grid; gap: 2px; font-size: 11px; color: var(--muted); }
    label.switch.small { font-size: 13px; }
    @media (max-width: 600px) { .prices { grid-template-columns: repeat(2, minmax(0, 1fr)); } .prices .what { grid-column: 1 / -1; } .model .head { grid-template-columns: 1fr auto; } }
    .found { display: flex; gap: 4px; flex-wrap: wrap; }
    .found button { font-weight: 400; font-size: 12px; padding: 3px 8px; }
    label.switch { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    label.switch input { width: auto; }
    p.hint { margin: 0; font-size: 12px; color: var(--muted); }
    @media (max-width: 600px) { .grid2 { grid-template-columns: 1fr; } }
  `];
  org = "";
  list: AIProvider[] = [];
  draft?: Draft;
  found: string[] = [];
  busy = false;

  private get path() {
    return this.org ? `/orgs/${this.org}/ai/providers` : "/admin/ai/providers";
  }

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    this.list = (await api<AIProvider[]>("GET", this.path).catch(() => [])) ?? [];
    this.dispatchEvent(new CustomEvent("changed", { detail: this.list }));
  }

  private fail(e: unknown) {
    toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
  }

  private start(preset: string) {
    const p = PRESETS.find((x) => x.key === preset) ?? PRESETS[0];
    this.found = [];
    this.draft = { id: "", name: p.name, kind: p.kind, baseUrl: p.url, apiKey: "", keepKey: false, effort: p.effort ?? "", models: (p.models ?? []).map((m) => ({ ...m })), enabled: true };
  }

  private edit(p: AIProvider) {
    this.found = [];
    this.draft = { id: p.id, name: p.name, kind: p.kind, baseUrl: p.baseUrl, apiKey: "", keepKey: p.hasKey, effort: p.effort ?? "", models: p.models.map((m) => ({ ...m })), enabled: p.enabled };
  }

  private async save(e: Event) {
    e.preventDefault();
    const d = this.draft!;
    const body: Record<string, unknown> = { name: d.name, kind: d.kind, baseUrl: d.baseUrl, effort: d.kind === "anthropic" ? d.effort : "", models: d.models.filter((m) => m.id.trim()), enabled: d.enabled };
    if (d.apiKey || !d.keepKey) body.apiKey = d.apiKey;
    this.busy = true;
    try {
      const saved = await api<AIProvider>(d.id ? "PUT" : "POST", d.id ? `${this.path}/${d.id}` : this.path, body);
      toast(t("ai_providers.saved"));
      await this.load();
      // Stay in the form after adding, so models can be loaded from the provider.
      this.edit(saved);
      if (!d.id && !saved.models.length) await this.discover();
    } catch (err) {
      this.fail(err);
    } finally {
      this.busy = false;
    }
  }

  private async discover() {
    if (!this.draft?.id) return;
    this.busy = true;
    try {
      const have = new Set(this.draft.models.map((m) => m.id));
      this.found = (await api<string[]>("POST", `${this.path}/${this.draft.id}/models`)).filter((id) => !have.has(id));
      if (!this.found.length) toast(t("ai_providers.no_new_models"));
    } catch (err) {
      this.fail(err);
    } finally {
      this.busy = false;
    }
  }

  private async removeProvider(p: AIProvider) {
    if (!confirm(t("ai_providers.delete_confirm", { name: p.name }))) return;
    try {
      await api("DELETE", `${this.path}/${p.id}`);
      await this.load();
    } catch (err) {
      this.fail(err);
    }
  }

  private async toggle(p: AIProvider, on: boolean) {
    try {
      await api("PUT", `${this.path}/${p.id}`, { name: p.name, kind: p.kind, baseUrl: p.baseUrl, effort: p.effort ?? "", models: p.models, enabled: on });
      await this.load();
    } catch (err) {
      this.fail(err);
    }
  }

  private setModel(i: number, patch: Partial<AIModel>) {
    const models = this.draft!.models.slice();
    models[i] = { ...models[i], ...patch };
    this.draft = { ...this.draft!, models };
  }

  private num(v: string) {
    const n = Number(v.replace(",", "."));
    return Number.isFinite(n) && n >= 0 ? n : 0;
  }

  private priceInputs(p: { input: number; output: number; cacheRead: number; cacheWrite: number } | undefined, base: AIModel, set: (k: PriceKey, v: number) => void) {
    return (["input", "output", "cacheRead", "cacheWrite"] as const).map((k) => html`<label class="price">${t(PRICE_LABEL[k])}
      <input type="number" min="0" step="0.001" .value=${p ? String(p[k]) : ""} placeholder=${p ? "" : String(base[k])}
        @input=${(e: Event) => set(k, this.num((e.target as HTMLInputElement).value))}></label>`);
  }

  private modelRow(m: AIModel, i: number) {
    const d = this.draft!;
    const charge = m.charge;
    return html`<div class="model">
      <div class="head">
        <input required .value=${m.id} placeholder=${t("ai_providers.model_id")} aria-label=${t("ai_providers.model_id")} @input=${(e: Event) => this.setModel(i, { id: (e.target as HTMLInputElement).value })}>
        <input .value=${m.label ?? ""} placeholder=${t("ai_providers.model_label")} aria-label=${t("ai_providers.model_label")} @input=${(e: Event) => this.setModel(i, { label: (e.target as HTMLInputElement).value })}>
        <button type="button" class="ghost" aria-label=${t("ai_providers.remove_model")} @click=${() => (this.draft = { ...d, models: d.models.filter((_, k) => k !== i) })}>${icon("X", 13)}</button>
      </div>
      <div class="prices"><span class="what">${t("ai_providers.cost")}</span>${this.priceInputs(m, m, (k, v) => this.setModel(i, { [k]: v }))}</div>
      <label class="switch small"><input type="checkbox" .checked=${!!charge}
        @change=${(e: Event) => this.setModel(i, { charge: (e.target as HTMLInputElement).checked ? { input: m.input, output: m.output, cacheRead: m.cacheRead, cacheWrite: m.cacheWrite } : undefined })}>
        ${t("ai_providers.own_price")}</label>
      ${charge ? html`<div class="prices"><span class="what">${t("ai_providers.charge")}</span>${this.priceInputs(charge, m, (k, v) => this.setModel(i, { charge: { ...charge, [k]: v } }))}</div>` : nothing}
    </div>`;
  }

  private form() {
    const d = this.draft!;
    const set = (patch: Partial<Draft>) => (this.draft = { ...d, ...patch });
    return html`<form @submit=${this.save}>
      <div class="grid2">
        <label class="field">${t("ai_providers.name")}<input required maxlength="80" .value=${d.name} @input=${(e: Event) => set({ name: (e.target as HTMLInputElement).value })}></label>
        <label class="field">${t("ai_providers.kind")}<select .value=${d.kind} @change=${(e: Event) => set({ kind: (e.target as HTMLSelectElement).value as AIProvider["kind"] })}>
          <option value="anthropic" ?selected=${d.kind === "anthropic"}>Anthropic (Messages API)</option>
          <option value="openai" ?selected=${d.kind === "openai"}>${t("ai_providers.kind_openai")}</option></select></label>
      </div>
      <label class="field">${t("ai_providers.base_url")}<input type="url" .value=${d.baseUrl} ?required=${d.kind === "openai"}
        placeholder=${d.kind === "anthropic" ? "https://api.anthropic.com" : "https://…/v1"} @input=${(e: Event) => set({ baseUrl: (e.target as HTMLInputElement).value })}></label>
      <p class="hint">${t(d.kind === "openai" ? "ai_providers.base_url_hint_openai" : "ai_providers.base_url_hint_anthropic")}${this.org ? ` ${t("ai_providers.org_network_hint")}` : ""}</p>
      <label class="field">${t("ai_providers.api_key")}<input type="password" autocomplete="off" .value=${d.apiKey}
        placeholder=${d.keepKey ? t("ai_providers.key_kept") : ""} @input=${(e: Event) => set({ apiKey: (e.target as HTMLInputElement).value })}></label>
      ${d.keepKey ? html`<label class="switch"><input type="checkbox" .checked=${!d.keepKey} @change=${(e: Event) => set({ keepKey: !(e.target as HTMLInputElement).checked })}> ${t("ai_providers.remove_key")}</label>` : nothing}
      ${d.kind === "anthropic" ? html`<label class="field">${t("ai_providers.effort")}<select @change=${(e: Event) => set({ effort: (e.target as HTMLSelectElement).value })}>
        ${["", "low", "medium", "high", "xhigh", "max"].map((v) => html`<option value=${v} ?selected=${d.effort === v}>${v || t("ai_providers.effort_none")}</option>`)}</select></label>` : nothing}

      <strong>${t("ai_providers.models")}</strong>
      <p class="hint">${t("ai_providers.prices_hint")}</p>
      ${d.models.map((m, i) => this.modelRow(m, i))}
      <div class="row">
        <button type="button" class="ghost" @click=${() => (this.draft = { ...d, models: [...d.models, emptyModel()] })}>${icon("Plus", 13)} ${t("ai_providers.add_model")}</button>
        ${d.id ? html`<button type="button" class="ghost" ?disabled=${this.busy} @click=${this.discover}>${icon("RefreshCw", 13)} ${t("ai_providers.discover")}</button>` : html`<span class="hint">${t("ai_providers.discover_after_save")}</span>`}
      </div>
      ${this.found.length ? html`<div class="found" role="group" aria-label=${t("ai_providers.found")}>${this.found.map((id) => html`<button type="button" class="ghost"
        @click=${() => { this.draft = { ...this.draft!, models: [...this.draft!.models, emptyModel(id)] }; this.found = this.found.filter((x) => x !== id); }}>${icon("Plus", 12)} ${id}</button>`)}</div>` : nothing}
      <div class="row">
        <button ?disabled=${this.busy}>${t("doc.save")}</button>
        <button type="button" class="ghost" @click=${() => { this.draft = undefined; this.found = []; }}>${t("ai_providers.close")}</button>
      </div>
    </form>`;
  }

  render() {
    return html`
      ${this.list.map((p) => html`<div class="prov">
        <header>
          <strong>${p.name}</strong>
          <span class="chip">${p.kind === "anthropic" ? "Anthropic" : t("ai_providers.kind_openai")}</span>
          ${p.builtIn ? html`<span class="chip">${t("ai_providers.builtin")}</span>` : html`
            <label class="switch"><input type="checkbox" .checked=${p.enabled} @change=${(e: Event) => this.toggle(p, (e.target as HTMLInputElement).checked)}> ${t("ai_providers.enabled")}</label>
            <button class="ghost" aria-label=${t("ai_providers.edit", { name: p.name })} @click=${() => this.edit(p)}>${icon("Pencil", 13)}</button>
            <button class="danger" aria-label=${t("ai_providers.delete", { name: p.name })} @click=${() => this.removeProvider(p)}>${icon("Trash2", 13)}</button>`}
        </header>
        <div class="meta">${p.baseUrl || (p.kind === "anthropic" ? "api.anthropic.com" : "")} · ${p.hasKey ? t("ai_providers.key_set") : t("ai_providers.key_missing")}</div>
        <div class="models">${p.models.length ? p.models.map((m) => html`<span class="chip" title=${t("ai_providers.price_title", { in: (m.charge ?? m).input, out: (m.charge ?? m).output })}>${m.label || m.id}${m.charge ? html` ${icon("Tag", 11)}` : nothing}</span>`)
          : html`<span class="meta">${t("ai_providers.no_models")}</span>`}</div>
      </div>`)}
      ${this.draft ? this.form() : html`<div class="row">
        <label class="field" style="flex:1;max-width:320px">${t("ai_providers.add")}<select @change=${(e: Event) => { const v = (e.target as HTMLSelectElement).value; if (v) this.start(v); (e.target as HTMLSelectElement).value = ""; }}>
          <option value="">${t("ai_providers.choose")}</option>
          ${PRESETS.map((p) => html`<option value=${p.key}>${p.key === "gateway" ? t("ai_providers.preset_gateway") : p.name}</option>`)}
        </select></label>
      </div>`}`;
  }
}
customElements.define("phx-ai-providers", PhxAIProviders);
