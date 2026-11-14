import { LitElement, css, html } from "lit";
import { ApiError } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { docsVersion } from "../state";
import { base } from "./shared";

interface Result { created: string[]; skipped: string[]; entity?: string }

/** Import Markdown folders (ZIP, also Obsidian) and OpenAPI files. */
export class PhxImportView extends LitElement {
  static properties = { project: {}, busy: { state: true }, result: { state: true }, error: { state: true } };
  static styles = [base, css`
    :host { display: block; max-width: 820px; }
    .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 14px; margin-top: 14px; }
    .card { display: grid; gap: 8px; align-content: start; }
    .card h2 { font-size: 16px; margin: 0; display: flex; gap: 8px; align-items: center; }
    .hint { font-size: 13px; color: var(--muted); margin: 0; }
    .res { margin-top: 16px; }
    .res ul { columns: 2; font-size: 14px; }
  `];
  project = "";
  busy = "";
  result?: Result;
  error = "";

  private async upload(kind: "markdown" | "openapi", e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    this.busy = kind;
    this.error = "";
    this.result = undefined;
    try {
      // Raw body: the server reads ZIP, JSON or YAML itself.
      const res = await fetch(`/api/v1/projects/${this.project}/import/${kind}`, { method: "POST", body: file, headers: { "X-Phoedox": "1" }, credentials: "same-origin" });
      const data = await res.json();
      if (!res.ok) throw new ApiError(res.status, data.code ?? "unknown", data.key ?? "error.unknown", data.params ?? {});
      this.result = data as Result;
      docsVersion.set(docsVersion.get() + 1);
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = "";
    }
  }

  render() {
    const r = this.result;
    return html`<div class="enter">
      <h1>${t("import.title")}</h1>
      <div class="cards">
        <div class="card"><h2>${icon("FolderOpen", 16)} ${t("import.markdown")}</h2>
          <p class="hint">${t("import.markdown_hint")}</p>
          <label class="btn" style="justify-self:start" aria-busy=${this.busy === "markdown" ? "true" : "false"}>${icon("Upload", 14)} ${t("import.choose_zip")}
            <input type="file" accept=".zip,application/zip" hidden ?disabled=${!!this.busy} @change=${(e: Event) => this.upload("markdown", e)}></label></div>
        <div class="card"><h2>${icon("Braces", 16)} ${t("import.openapi")}</h2>
          <p class="hint">${t("import.openapi_hint")}</p>
          <label class="btn" style="justify-self:start" aria-busy=${this.busy === "openapi" ? "true" : "false"}>${icon("Upload", 14)} ${t("import.choose_spec")}
            <input type="file" accept=".json,.yaml,.yml,application/json,application/yaml" hidden ?disabled=${!!this.busy} @change=${(e: Event) => this.upload("openapi", e)}></label></div>
      </div>
      ${this.busy ? html`<p class="muted" role="status">${t("import.running")}</p>` : ""}
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      ${r ? html`<div class="res" role="status">
        <p><strong>${t("import.done", { count: r.created.length })}</strong>${r.entity ? html` · <a href="/p/${this.project}/e/${r.entity}">${t("import.entity")}</a>` : ""}</p>
        <ul>${r.created.map((s) => html`<li><a href="/p/${this.project}/d/${s}">${s}</a></li>`)}</ul>
        ${r.skipped.length ? html`<p class="hint">${t("import.skipped", { count: r.skipped.length })}: ${r.skipped.slice(0, 20).join(", ")}${r.skipped.length > 20 ? " …" : ""}</p>` : ""}
      </div>` : ""}
    </div>`;
  }
}
customElements.define("phx-import-view", PhxImportView);
