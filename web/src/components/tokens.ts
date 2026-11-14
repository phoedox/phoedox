import { LitElement, css, html } from "lit";
import { ApiError, api, type Grant, type Project, type Token } from "../api";
import "./scope-picker";
import type { PhxScopePicker } from "./scope-picker";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

export class PhxTokens extends LitElement {
  static properties = { tokens: { state: true }, grants: { state: true }, secret: { state: true }, copied: { state: true }, names: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px; }
    form.create { display: grid; gap: 12px; margin: 16px 0; padding: 16px; }
    form.create .row input { flex: 1; min-width: 200px; }
    /* Shown once, rarely: a gentle reveal draws the eye to the secret. */
    .secret { display: flex; flex-direction: column; gap: 8px; border-color: var(--accent);
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(-4px); } }
    @media (prefers-reduced-motion: reduce) { .secret { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } } }
    code { font-family: var(--font-mono); font-size: 13px; word-break: break-all; background: var(--surface2); padding: 8px 10px; border-radius: 6px; display: block; user-select: all; }
    ul { list-style: none; padding: 0; display: flex; flex-direction: column; gap: 8px; }
    li { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
    pre { background: var(--surface2); padding: 12px; border-radius: 8px; overflow-x: auto; font-size: 12.5px; }
  `];
  tokens: Token[] = [];
  /** Project names by id for the scope chips. */
  names = new Map<string, string>();
  grants: Grant[] = [];
  secret = "";
  copied = false;

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    this.tokens = (await api<Token[]>("GET", "/tokens").catch(() => [])) ?? [];
    const ps = (await api<Project[]>("GET", "/projects").catch(() => [])) ?? [];
    this.names = new Map(ps.map((p) => [p.id, p.name]));
    this.grants = (await api<Grant[]>("GET", "/oauth/grants").catch(() => [])) ?? [];
  }

  private async create(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    try {
      const scope = this.renderRoot.querySelector<PhxScopePicker>("phx-scope-picker")!.value;
      if (!scope) {
        toast(t("scope.none_selected"), "error");
        return;
      }
      const res = await api<{ secret: string }>("POST", "/tokens", { name: new FormData(form).get("name"), ...scope });
      this.secret = res.secret;
      this.copied = false;
      form.reset();
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  private async copy() {
    try {
      await navigator.clipboard.writeText(this.secret);
      this.copied = true;
    } catch {
      this.copied = false;
    }
  }

  private async disconnect(id: string) {
    await api("DELETE", `/oauth/grants/${id}`).catch((err) => toast(err instanceof ApiError ? err.text : t("error.unknown"), "error"));
    await this.load();
  }

  private async revoke(id: string) {
    await api("DELETE", `/tokens/${id}`).catch((err) => toast(err instanceof ApiError ? err.text : t("error.unknown"), "error"));
    await this.load();
  }

  private scopeChips(access: string, projects: string[] | null) {
    return html`${access === "read" ? html`<span class="chip">${t("scope.chip_read")}</span>` : ""}
      ${projects?.length ? html`<span class="chip" title=${projects.map((id) => this.names.get(id) ?? id).join(", ")}>${projects.length === 1 ? this.names.get(projects[0]) ?? projects[0] : t("scope.chip_projects", { count: projects.length })}</span>` : ""}`;
  }

  render() {
    const mcpUrl = `${location.origin}/mcp`;
    return html`<div class="enter">
      <h1 style="margin-bottom:16px">${t("tokens.title")}</h1>
      ${settingsNav("tokens")}
      <p class="muted">${t("tokens.intro")}</p>
      <form class="create card" @submit=${this.create}>
        <div class="row"><input name="name" required placeholder=${t("tokens.name")} /><button type="submit">${icon("KeyRound", 14)} ${t("tokens.create")}</button></div>
        <phx-scope-picker></phx-scope-picker>
      </form>
      ${this.secret ? html`<div class="card secret" role="status"><b>${t("tokens.secret")}</b><code>${this.secret}</code>
        <div><button class="ghost" @click=${this.copy}>${icon(this.copied ? "Check" : "Copy", 14)} ${this.copied ? t("tokens.copied") : t("tokens.copy")}</button></div></div>` : ""}
      ${this.tokens.length === 0 ? html`<p class="muted">${t("tokens.empty")}</p>` : html`<ul>${this.tokens.map((tk) => html`<li class="card">
        <span><b>${tk.name}</b> <span class="mono muted">${tk.prefix}…</span> ${this.scopeChips(tk.access, tk.projects)}<br /><span class="muted" style="font-size:13px">${tk.lastUsedAt ? t("tokens.last_used", { date: formatDate(tk.lastUsedAt) }) : t("tokens.never_used")}</span></span>
        <button class="danger" @click=${() => this.revoke(tk.id)}>${t("tokens.revoke")}</button></li>`)}</ul>`}
      <h2 style="font-size:18px;margin:24px 0 6px">${t("tokens.apps_title")}</h2>
      <p class="muted">${t("tokens.apps_intro")}</p>
      ${this.grants.length === 0 ? html`<p class="muted">${t("tokens.apps_none")}</p>` : html`<ul>${this.grants.map((g) => html`<li class="card">
        <span><b>${g.clientName}</b> ${this.scopeChips(g.access, g.projects)}<br /><span class="muted" style="font-size:13px">${t("tokens.app_since", { date: formatDate(g.createdAt) })}${g.lastUsedAt ? html` · ${t("tokens.last_used", { date: formatDate(g.lastUsedAt) })}` : ""}</span></span>
        <button class="danger" @click=${() => this.disconnect(g.id)}>${t("tokens.revoke")}</button></li>`)}</ul>`}
      <h2 style="font-size:18px;margin:24px 0 6px">${t("tokens.mcp_title")}</h2>
      <p class="muted">${t("tokens.mcp_oauth")}</p>
      <pre>${mcpUrl}</pre>
      <p class="muted">${t("tokens.mcp_token")}</p>
      <pre>${JSON.stringify({ mcpServers: { phoedox: { type: "http", url: mcpUrl, headers: { Authorization: "Bearer phx_…" } } } }, null, 2)}</pre>
      <p class="muted">${t("tokens.mcp_skills")}</p>
    </div>`;
  }
}
customElements.define("phx-tokens", PhxTokens);
