import { LitElement, css, html } from "lit";
import { ApiError, api, type CatalogEntry, type PluginInfo, type PluginLog } from "../api";
import { formatSize } from "../files";
import { formatDate, locale, t } from "../i18n";
import { icon } from "../icon";
import { permLabel } from "../plugin-perms";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

interface List { plugins: PluginInfo[]; store: boolean; enabled: boolean; unsigned: boolean }

/** Plugins of the instance (system admins): install, update, switch, licenses, logs. */
export class PhxPluginsAdmin extends LitElement {
  static properties = {
    list: { state: true }, error: { state: true }, busy: { state: true }, catalog: { state: true }, catalogError: { state: true },
    confirming: { state: true }, logs: { state: true }, logsFor: { state: true }, removing: { state: true },
  };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 900px; margin: 0 auto; padding: 28px 16px 60px; }
    h1 { margin-bottom: 16px; }
    h2 { font-size: 18px; margin: 30px 0 10px; }
    p { line-height: 1.5; }
    .plugin { margin-bottom: 12px; display: grid; gap: 10px; }
    .plugin header { display: flex; gap: 12px; align-items: flex-start; flex-wrap: wrap; }
    .plugin header .t { flex: 1; min-width: 220px; }
    .plugin header h3 { font-size: 16px; margin: 0; }
    .pic { display: inline-grid; place-items: center; width: 38px; height: 38px; border-radius: 10px; flex: none; color: var(--signal);
      background: color-mix(in srgb, var(--signal) 14%, transparent); }
    .chips { display: flex; flex-wrap: wrap; gap: 5px; }
    .chip.good { color: var(--good); } .chip.bad { color: var(--bad); }
    .perms { margin: 0; padding: 0; list-style: none; display: grid; gap: 3px; font-size: 13px; }
    .perms li { display: flex; flex-wrap: wrap; gap: 2px 8px; align-items: baseline; }
    .perms code { font-size: 12px; color: var(--muted); }
    .bar { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    .bar button, .bar .btn { padding: 4px 10px; font-size: 13px; }
    .warn { border-left: 3px solid var(--bad); padding: 8px 12px; background: var(--surface2); border-radius: 8px; font-size: 14px; }
    .warn.soft { border-left-color: var(--signal); }
    form.inline { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
    form.inline input { flex: 1; min-width: 200px; }
    table { width: 100%; border-collapse: collapse; font-size: 12px; }
    td { padding: 3px 8px 3px 0; border-bottom: 1px solid var(--line); vertical-align: top; }
    td.lv { text-transform: uppercase; color: var(--muted); }
    td.lv.error { color: var(--bad); }
    .upload { display: grid; gap: 8px; max-width: 560px; }
    .upload label { display: grid; gap: 4px; font-size: 14px; }
    textarea { width: 100%; min-height: 90px; font-family: var(--font-mono); font-size: 12px; }
  `];
  list?: List;
  error = "";
  busy = false;
  catalog?: CatalogEntry[];
  catalogError = "";
  confirming = "";
  logs: PluginLog[] = [];
  logsFor = "";
  removing = "";

  async connectedCallback() {
    super.connectedCallback();
    await this.load();
  }

  private async load() {
    try {
      this.list = await api<List>("GET", "/admin/plugins");
      this.error = "";
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async call<T>(fn: () => Promise<T>, done?: string): Promise<T | undefined> {
    this.busy = true;
    try {
      const r = await fn();
      if (done) toast(done);
      return r;
    } catch (e) {
      toast(e instanceof ApiError ? e.text : t("error.unknown"), "error");
      return undefined;
    } finally {
      this.busy = false;
    }
  }

  private async upload(e: SubmitEvent) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const f = new FormData(form);
    const pkg = f.get("package") as File | null;
    if (!pkg || !pkg.size) return;
    const headers: Record<string, string> = { "X-Phoedox": "1", "Content-Type": "application/gzip" };
    const sigFile = f.get("signature") as File | null;
    if (sigFile && sigFile.size) {
      try {
        const sig = JSON.parse(await sigFile.text()) as { keyId: string; sig: string };
        headers["Phoedox-Signature"] = `${sig.keyId}:${sig.sig}`;
      } catch {
        toast(t("plugins.bad_sig_file"), "error");
        return;
      }
    }
    const r = await this.call(async () => {
      const res = await fetch("/api/v1/admin/plugins", { method: "POST", headers, body: pkg, credentials: "same-origin" });
      const d = await res.json().catch(() => ({}));
      if (!res.ok) throw new ApiError(res.status, d.code ?? "unknown", d.key ?? "error.unknown", d.params ?? {});
      return d as PluginInfo;
    });
    if (r) {
      toast(t("plugins.installed", { name: r.name, version: r.version }));
      form.reset();
      await this.load();
    }
  }

  private async loadCatalog() {
    try {
      this.catalog = await api<CatalogEntry[]>("GET", "/admin/plugins/catalog");
      this.catalogError = "";
    } catch (e) {
      this.catalogError = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  private async install(id: string, version: string) {
    const r = await this.call(() => api<PluginInfo>("POST", "/admin/plugins/install", { id, version }));
    if (r) {
      toast(t("plugins.installed", { name: r.name, version: r.version }));
      this.confirming = "";
      await Promise.all([this.load(), this.catalog ? this.loadCatalog() : undefined]);
    }
  }

  private async toggle(p: PluginInfo) {
    if (await this.call(() => api("PATCH", `/admin/plugins/${p.id}`, { enabled: !p.enabled }))) await this.load();
  }

  private async uninstall(p: PluginInfo) {
    if (await this.call(() => api("DELETE", `/admin/plugins/${p.id}`), t("plugins.uninstalled", { name: p.name }))) {
      this.removing = "";
      await this.load();
    }
  }

  private async showLogs(p: PluginInfo) {
    if (this.logsFor === p.id) {
      this.logsFor = "";
      return;
    }
    this.logs = (await this.call(() => api<PluginLog[]>("GET", `/admin/plugins/${p.id}/logs`))) ?? [];
    this.logsFor = p.id;
  }

  private async redeem(e: SubmitEvent, p: PluginInfo) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const code = String(new FormData(form).get("code") ?? "").trim();
    if (await this.call(() => api("POST", `/admin/plugins/${p.id}/license/redeem`, { code }), t("plugins.license_saved"))) {
      form.reset();
      await this.load();
    }
  }

  private async pasteLicense(e: SubmitEvent, p: PluginInfo) {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const text = String(new FormData(form).get("file") ?? "").trim();
    const r = await this.call(async () => {
      const res = await fetch(`/api/v1/admin/plugins/${p.id}/license`, { method: "PUT", headers: { "X-Phoedox": "1", "Content-Type": "application/json" }, body: text, credentials: "same-origin" });
      const d = await res.json().catch(() => ({}));
      if (!res.ok) throw new ApiError(res.status, d.code ?? "unknown", d.key ?? "error.unknown", d.params ?? {});
      return d;
    }, t("plugins.license_saved"));
    if (r) {
      form.reset();
      await this.load();
    }
  }

  private perms(list: string[]) {
    if (!list.length) return html`<p class="muted" style="margin:0;font-size:13px">${t("plugins.no_perms")}</p>`;
    return html`<ul class="perms">${list.map((p) => html`<li>${icon("KeyRound", 12)}<span>${permLabel(p)}</span><code>${p}</code></li>`)}</ul>`;
  }

  private license(p: PluginInfo) {
    const l = p.license;
    if (!l.required && !l.valid && !l.hint) return "";
    const state = l.valid
      ? (l.trialEnd && !l.plan ? html`<span class="chip">${t("plugins.trial_until", { date: formatDate(l.trialEnd) })}</span>`
        : html`<span class="chip good">${icon("BadgeCheck", 12)} ${t("plugins.licensed", { plan: l.plan ?? "" })}${l.expires ? ` · ${t("share.until", { date: formatDate(l.expires) })}` : ""}</span>`)
      : html`<span class="chip bad">${t(l.required ? "plugins.license_needed" : "plugins.license_none")}</span>`;
    return html`<div style="display:grid;gap:6px">
      <div class="chips">${state}${l.hint === "commercial" ? html`<span class="chip">${t("plugins.license_commercial")}</span>` : ""}
        ${l.problem ? html`<span class="chip bad">${t(`plugins.license_problem.${l.problem}` as never)}</span>` : ""}</div>
      ${this.list?.store ? html`<form class="inline" @submit=${(e: SubmitEvent) => this.redeem(e, p)}>
        <input name="code" required placeholder="PHX-XXXX-XXXX-XXXX" aria-label=${t("plugins.license_code")} class="mono" autocomplete="off">
        <button class="ghost" ?disabled=${this.busy}>${t("plugins.redeem")}</button></form>` : ""}
      <details><summary class="muted" style="font-size:13px">${t("plugins.license_paste")}</summary>
        <form @submit=${(e: SubmitEvent) => this.pasteLicense(e, p)} style="display:grid;gap:6px;margin-top:6px">
          <textarea name="file" required aria-label=${t("plugins.license_file")}></textarea>
          <div><button class="ghost" ?disabled=${this.busy}>${t("plugins.license_save")}</button></div></form></details>
    </div>`;
  }

  private card(p: PluginInfo) {
    return html`<article class="plugin card">
      <header>
        <span class="pic">${icon("Puzzle", 20)}</span>
        <div class="t"><h3>${p.name} <span class="muted" style="font-weight:400">${p.version}</span></h3>
          <code class="muted" style="font-size:12px">${p.id}</code>
          ${p.description ? html`<p class="muted" style="margin:4px 0 0;font-size:14px">${p.description}</p>` : ""}</div>
        <div class="chips">
          ${p.signed ? html`<span class="chip good" title=${p.signedBy}>${icon("BadgeCheck", 12)} ${t("plugins.signed")}</span>` : html`<span class="chip bad">${icon("TriangleAlert", 12)} ${t("plugins.unsigned")}</span>`}
          <span class="chip">${t(p.source === "store" ? "plugins.from_store" : "plugins.from_upload")}</span>
          <span class="chip ${p.enabled ? "good" : ""}">${t(p.enabled ? "plugins.on" : "plugins.off")}</span>
        </div>
      </header>
      ${p.recall ? html`<p class="warn ${p.recall === "warning" ? "soft" : ""}">${icon("TriangleAlert", 14)} <b>${t(p.recall === "critical" ? (p.criticalPath ? "plugins.recall_critical_path" : "plugins.recall_critical") : "plugins.recall_warning")}</b>
        ${p.recallNote ? html`<br>${p.recallNote}` : ""}</p>` : ""}
      ${p.problem ? html`<p class="warn">${icon("CircleAlert", 14)} ${t(p.problem === "incompatible" ? "plugins.problem_incompatible" : "plugins.problem_manifest", { needs: p.needs ?? "" })}</p>` : ""}
      ${p.latest ? html`<p class="warn soft">${t("plugins.update", { version: p.latest })}
        <button class="ghost" style="padding:3px 9px;margin-left:6px" ?disabled=${this.busy} @click=${() => this.install(p.id, p.latest!)}>${t("plugins.update_now")}</button></p>` : ""}
      ${this.perms(p.permissions)}
      <div class="chips">
        ${p.kinds.map((k) => html`<span class="chip">${icon("Shapes", 12)} ${k}</span>`)}
        ${p.tools.map((k) => html`<span class="chip">${icon("Bot", 12)} ${k}</span>`)}
        ${p.events.map((k) => html`<span class="chip">${icon("Clock", 12)} ${k}</span>`)}
        ${p.frontend ? html`<span class="chip" title=${t("plugins.frontend_hint")}>${icon("Monitor", 12)} ${t("plugins.frontend")}</span>` : ""}
        ${p.criticalPath ? html`<span class="chip">${icon("ShieldCheck", 12)} ${t("plugins.critical_path")}</span>` : ""}
      </div>
      ${this.license(p)}
      <div class="bar">
        <span class="muted" style="font-size:13px;margin-right:auto">${t("plugins.used_in", { count: p.projects })} · ${formatSize(p.size, locale())} · ${formatDate(p.updatedAt)}</span>
        <button class="ghost" ?disabled=${this.busy} @click=${() => this.toggle(p)}>${icon("Power", 13)} ${t(p.enabled ? "plugins.disable" : "plugins.enable")}</button>
        <button class="ghost" aria-expanded=${this.logsFor === p.id} @click=${() => this.showLogs(p)}>${icon("ScrollText", 13)} ${t("plugins.logs")}</button>
        ${this.removing === p.id
          ? html`<span class="muted" style="font-size:13px">${t("plugins.uninstall_confirm")}</span>
            <button class="danger" ?disabled=${this.busy} @click=${() => this.uninstall(p)}>${t("plugins.uninstall")}</button>
            <button class="ghost" @click=${() => (this.removing = "")}>${t("doc.cancel")}</button>`
          : html`<button class="danger" @click=${() => (this.removing = p.id)}>${icon("Trash2", 13)} ${t("plugins.uninstall")}</button>`}
      </div>
      ${this.logsFor === p.id ? (this.logs.length ? html`<table>${this.logs.map((l) => html`<tr><td class="muted" style="white-space:nowrap">${formatDate(l.time)}</td>
        <td class="lv ${l.level}">${l.level}</td><td class="mono">${l.msg}</td></tr>`)}</table>` : html`<p class="muted" style="margin:0;font-size:13px">${t("plugins.no_logs")}</p>`) : ""}
    </article>`;
  }

  private entry(c: CatalogEntry) {
    const upToDate = c.installed === c.version;
    return html`<article class="plugin card">
      <header><span class="pic">${icon("Puzzle", 20)}</span>
        <div class="t"><h3>${c.name} <span class="muted" style="font-weight:400">${c.version}</span></h3>
          <span class="muted" style="font-size:13px">${c.author}${c.verified ? html` · ${icon("BadgeCheck", 12)} ${t("plugins.verified_author")}` : ""}</span>
          ${c.summary ? html`<p class="muted" style="margin:4px 0 0;font-size:14px">${c.summary}</p>` : ""}</div>
        ${upToDate ? html`<span class="chip good">${t("plugins.installed_chip")}</span>`
          : html`<button ?disabled=${this.busy} @click=${() => (this.confirming = this.confirming === c.id ? "" : c.id)}>${icon("Download", 13)} ${t(c.installed ? "plugins.update_now" : "plugins.install")}</button>`}
      </header>
      ${c.recall ? html`<p class="warn">${icon("TriangleAlert", 14)} ${t("plugins.recall_warning")} ${c.recallNote ?? ""}</p>` : ""}
      ${this.confirming === c.id ? html`<div class="warn soft" style="display:grid;gap:8px">
        <b>${t("plugins.confirm_perms")}</b>${this.perms(c.permissions)}
        <div><button ?disabled=${this.busy} @click=${() => this.install(c.id, c.version)}>${t("plugins.install_with_perms")}</button>
          <button class="ghost" @click=${() => (this.confirming = "")}>${t("doc.cancel")}</button></div></div>` : ""}
    </article>`;
  }

  render() {
    const l = this.list;
    return html`<div class="enter">
      <h1>${t("plugins.title")}</h1>
      ${settingsNav("plugins")}
      ${this.error ? html`<p class="error">${this.error}</p>` : ""}
      ${l ? html`
        ${!l.enabled ? html`<p class="warn">${t("plugins.server_off")}</p>` : ""}
        <p class="muted">${t("plugins.intro")}</p>
        ${l.plugins.length ? l.plugins.map((p) => this.card(p)) : html`<p class="muted">${t("plugins.none")}</p>`}
        ${l.store ? html`<h2>${icon("Store", 16)} ${t("plugins.store")}</h2>
          ${this.catalog ? (this.catalog.length ? this.catalog.map((c) => this.entry(c)) : html`<p class="muted">${t("plugins.store_empty")}</p>`)
            : html`<button class="ghost" @click=${this.loadCatalog}>${icon("Store", 14)} ${t("plugins.browse")}</button>`}
          ${this.catalogError ? html`<p class="error">${this.catalogError}</p>` : ""}` : ""}
        <h2>${icon("Upload", 16)} ${t("plugins.upload")}</h2>
        <form class="upload" @submit=${this.upload}>
          <label>${t("plugins.package")}<input type="file" name="package" required accept=".phxp,.tgz,.gz,application/gzip"></label>
          <label>${t("plugins.signature")}<input type="file" name="signature" accept=".sig,.json,application/json"></label>
          <p class="muted" style="margin:0;font-size:13px">${t(l.unsigned ? "plugins.unsigned_allowed" : "plugins.signature_hint")}</p>
          <div><button ?disabled=${this.busy || !l.enabled}>${icon("Upload", 14)} ${t("plugins.upload_install")}</button></div>
        </form>` : !this.error ? html`<p class="muted">${t("app.loading")}</p>` : ""}
    </div>`;
  }
}
customElements.define("phx-plugins-admin", PhxPluginsAdmin);
