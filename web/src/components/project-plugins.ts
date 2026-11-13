import { LitElement, css, html } from "lit";
import { ApiError, api, type ProjectPlugin } from "../api";
import { formatDate, t } from "../i18n";
import { icon } from "../icon";
import { reloadProjectPlugins } from "../plugin-host";
import { permLabel } from "../plugin-perms";
import { base } from "./shared";
import { toast } from "./toasts";

/** Plugins of a project (owners): switch on/off, secrets, last run. */
export class PhxProjectPlugins extends LitElement {
  static properties = { project: {}, list: { state: true }, busy: { state: true }, open: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .pl { padding: 12px 14px; margin-bottom: 8px; display: grid; gap: 8px; }
    .pl header { display: flex; gap: 10px; align-items: flex-start; flex-wrap: wrap; }
    .pl header .t { flex: 1; min-width: 200px; }
    .pl h3 { font-size: 15px; margin: 0; }
    label.switch { display: flex; gap: 8px; align-items: center; font-size: 14px; }
    label.switch input { width: auto; }
    ul { margin: 0; padding: 0; list-style: none; display: grid; gap: 2px; font-size: 13px; }
    form { display: grid; gap: 6px; max-width: 520px; }
    form label { display: grid; gap: 3px; font-size: 13px; }
    .muted.small { font-size: 13px; margin: 0; }
  `];
  project = "";
  list: ProjectPlugin[] = [];
  busy = false;
  open = "";

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    this.list = (await api<ProjectPlugin[]>("GET", `/projects/${this.project}/plugins`).catch(() => [] as ProjectPlugin[])) ?? [];
  }

  private async set(p: ProjectPlugin, on: boolean) {
    this.busy = true;
    try {
      await api("PUT", `/projects/${this.project}/plugins/${p.id}`, { enabled: on });
      toast(t(on ? "plugins.project_on" : "plugins.project_off", { name: p.name }));
      reloadProjectPlugins();
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
      await this.load();
    }
  }

  private async secrets(e: SubmitEvent, p: ProjectPlugin) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const values: Record<string, string> = {};
    for (const [k, v] of new FormData(form)) if (String(v)) values[k] = String(v);
    if (!Object.keys(values).length) return;
    this.busy = true;
    try {
      await api("PUT", `/projects/${this.project}/plugins/${p.id}/secrets`, values);
      toast(t("plugins.secrets_saved"));
      form.reset();
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async clearSecret(p: ProjectPlugin, name: string) {
    try {
      await api("PUT", `/projects/${this.project}/plugins/${p.id}/secrets`, { [name]: "" });
      await this.load();
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    }
  }

  render() {
    if (!this.list.length) return html`<p class="muted small">${t("plugins.project_none")}</p>`;
    return html`${this.list.map((p) => {
      const secrets = Object.entries(p.secrets ?? {});
      return html`<article class="pl card">
        <header><span style="color:var(--signal)">${icon("Puzzle", 18)}</span>
          <div class="t"><h3>${p.name} <span class="muted" style="font-weight:400">${p.version}</span></h3>
            ${p.description ? html`<p class="muted small">${p.description}</p>` : ""}</div>
          <label class="switch"><input type="checkbox" .checked=${p.enabled} ?disabled=${this.busy || (!p.enabled && !!p.problem)}
            @change=${(e: Event) => this.set(p, (e.target as HTMLInputElement).checked)}> ${t("plugins.project_enabled")}</label>
        </header>
        ${p.problem ? html`<p class="muted small">${icon("CircleAlert", 13)} ${t(`plugins.unusable.${p.problem}` as never)}</p>` : ""}
        <details ?open=${this.open === p.id} @toggle=${(e: Event) => (this.open = (e.target as HTMLDetailsElement).open ? p.id : "")}>
          <summary class="muted" style="font-size:13px">${t("plugins.project_details")}</summary>
          <div style="display:grid;gap:8px;margin-top:8px">
            <ul>${(p.permissions ?? []).map((x) => html`<li>${icon("KeyRound", 12)} ${permLabel(x)}</li>`)}</ul>
            ${p.kinds.length || p.tools.length ? html`<p class="muted small">${p.kinds.length ? t("plugins.adds_kinds", { kinds: p.kinds.join(", ") }) : ""}
              ${p.tools.length ? t("plugins.adds_tools", { tools: p.tools.join(", ") }) : ""}</p>` : ""}
            ${p.enabled && p.lastRun ? html`<p class="muted small">${t("plugins.last_run", { date: formatDate(p.lastRun) })}
              ${p.lastError ? html`<br><span class="error">${p.lastError}</span>` : ""}</p>` : ""}
          </div>
        </details>
        ${p.enabled && secrets.length ? html`<form @submit=${(e: SubmitEvent) => this.secrets(e, p)}>
          ${secrets.map(([name, set]) => html`<label>${t("plugins.secret", { name })}
            <span style="display:flex;gap:6px"><input type="password" name=${name} autocomplete="new-password" placeholder=${set ? t("plugins.secret_set") : ""} style="flex:1">
            ${set ? html`<button type="button" class="ghost" style="padding:3px 8px" aria-label=${t("plugins.secret_clear", { name })} @click=${() => this.clearSecret(p, name)}>${icon("X", 13)}</button>` : ""}</span></label>`)}
          <div><button class="ghost" ?disabled=${this.busy}>${t("plugins.secrets_save")}</button></div>
          <p class="muted small">${t("plugins.secrets_hint")}</p>
        </form>` : ""}
      </article>`;
    })}`;
  }
}
customElements.define("phx-project-plugins", PhxProjectPlugins);
