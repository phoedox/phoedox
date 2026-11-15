import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { dollars } from "../ai-chat";
import { locale, t } from "../i18n";
import { currentUser } from "../state";
import { base } from "./shared";
import "./ai-providers";
import { toast } from "./toasts";

interface OrgAIChat { enabled: boolean; limit: number; budget: number | null; userBudget: number | null; defaultUserBudget: number; usedCents: number; usedInstanceCents: number; defaultModel: string; available: boolean }
interface ChatModel { ref: string; provider: string; label: string; own?: boolean }

/** AI chat of an organization (ADR-0066): switch and budgets for org admins;
 * system admins also set the organization's limit. */
export class PhxOrgAIChat extends LitElement {
  static properties = { org: {}, cap: {}, info: { state: true }, models: { state: true } };
  models: ChatModel[] = [];
  static styles = [base, css`
    :host { display: block; }
    form { display: grid; gap: 10px; max-width: 520px; }
    label.switch { display: flex; gap: 10px; align-items: center; font-weight: 600; }
    label.switch input, input[type=checkbox] { width: auto; }
    input[type=number] { max-width: 180px; }
    p { margin: 0; font-size: 13px; }
    h3 { font-size: 15px; margin: 18px 0 6px; font-family: var(--font-display); }
  `];
  org = "";
  /** The organization's cap set by system admins (cents; null = default). */
  cap: number | null = null;
  info?: OrgAIChat;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    this.info = await api<OrgAIChat>("GET", `/orgs/${this.org}/ai-chat`).catch(() => undefined);
    // Models to choose the default from: the organization's own and the instance's.
    const own = (await api<{ id: string; name: string; enabled: boolean; models: { id: string; label?: string }[] }[]>("GET", `/orgs/${this.org}/ai/providers`).catch(() => [])) ?? [];
    this.models = own.filter((p) => p.enabled).flatMap((p) => p.models.map((m) => ({ ref: `${p.id}/${m.id}`, provider: p.name, label: m.label || m.id, own: true })));
  }

  /** Cents from a dollar input; empty goes back to the default (-1). */
  private cents(f: FormData, name: string): number {
    const raw = String(f.get(name) ?? "").trim().replace(",", ".");
    if (raw === "") return -1;
    const v = Number(raw);
    return Number.isFinite(v) && v >= 0 ? Math.round(v * 100) : -1;
  }

  private async save(e: SubmitEvent) {
    e.preventDefault();
    const f = new FormData(e.target as HTMLFormElement);
    const body: Record<string, unknown> = { aiChat: f.get("chat") === "on", aiChatBudget: this.cents(f, "budget"), aiChatUserBudget: this.cents(f, "user"), aiChatModel: String(f.get("model") ?? "") };
    if (currentUser.get()?.isAdmin) body.aiChatCap = this.cents(f, "cap");
    try {
      await api("PATCH", `/orgs/${this.org}`, body);
      await this.load();
      toast(t("settings.saved"));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    const i = this.info;
    if (!i) return html``;
    const loc = locale();
    const val = (c: number | null) => (c === null || c === undefined ? "" : (c / 100).toFixed(2));
    const limit = i.limit ? dollars(i.limit, loc) : "∞";
    return html`<form @submit=${this.save}>
      ${i.available ? "" : html`<p class="muted">${t("orgs.ai_chat_unavailable")}</p>`}
      <label class="switch"><input type="checkbox" name="chat" .checked=${i.enabled}> ${t("orgs.ai_chat")}</label>
      <p class="muted">${t("orgs.ai_chat_hint")}</p>
      <p class="muted">${t("orgs.ai_chat_limit", { limit, used: dollars(i.usedInstanceCents, loc) })}</p>
      <label class="field">${t("orgs.ai_chat_budget")}<input name="budget" type="number" min="0" step="0.01" .value=${val(i.budget)} placeholder="∞"></label>
      <label class="field">${t("orgs.ai_chat_user_budget")}<input name="user" type="number" min="0" step="0.01" .value=${val(i.userBudget)} placeholder=${i.defaultUserBudget ? dollars(i.defaultUserBudget, loc) : "∞"}></label>
      ${currentUser.get()?.isAdmin ? html`<label class="field">${t("orgs.ai_chat_cap")}<input name="cap" type="number" min="0" step="0.01" .value=${val(this.cap)} placeholder=${t("orgs.ai_chat_cap_default")}></label>` : ""}
      <label class="field">${t("orgs.ai_chat_default_model")}<select name="model">
        <option value="" ?selected=${!i.defaultModel}>${t("orgs.ai_chat_model_instance")}</option>
        ${this.models.map((m) => html`<option value=${m.ref} ?selected=${i.defaultModel === m.ref}>${m.provider}: ${m.label}</option>`)}
      </select></label>
      <p class="muted">${t("orgs.ai_chat_used", { used: dollars(i.usedCents, loc) })}</p>
      <div><button>${t("doc.save")}</button></div>
    </form>
    <h3>${t("ai_providers.title_org")}</h3>
    <p class="muted">${t("ai_providers.intro_org")}</p>
    <phx-ai-providers .org=${this.org} @changed=${() => void this.load()}></phx-ai-providers>`;
  }
}
customElements.define("phx-org-ai-chat", PhxOrgAIChat);
