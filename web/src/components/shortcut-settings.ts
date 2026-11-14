import { SignalWatcher } from "@lit-labs/signals";
import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { SHORTCUT_ACTIONS, comboFor, comboLabel, comboOf, customShortcuts } from "../shortcuts";
import { currentProject } from "../state";
import { settingsNav, settingsNavStyles } from "./settings-nav";
import { base } from "./shared";
import { toast } from "./toasts";

/** Personal keyboard shortcuts: press a new combination for an action. */
export class PhxShortcutSettings extends SignalWatcher(LitElement) {
  static properties = { recording: { state: true }, error: { state: true } };
  static styles = [base, settingsNavStyles, css`
    :host { display: block; max-width: 760px; margin: 0 auto; padding: 28px 16px 60px; }
    table { width: 100%; border-collapse: collapse; }
    td { padding: 8px 8px 8px 0; border-bottom: 1px solid var(--line); vertical-align: middle; }
    td.keys { white-space: nowrap; text-align: right; }
    kbd { font: 12px var(--font-mono); border: 1px solid var(--line); border-radius: 5px; padding: 2px 7px; background: var(--surface); }
    button.rec { min-width: 150px; }
    button.rec[aria-pressed="true"] { border-color: var(--accent); color: var(--accent); }
    td button { padding: 3px 8px; font-size: 13px; }
  `];
  recording = "";
  error = "";

  connectedCallback() {
    super.connectedCallback();
    currentProject.set(null);
  }

  private async save(next: Record<string, string>) {
    this.error = "";
    try {
      customShortcuts.set(await api<Record<string, string>>("PUT", "/me/shortcuts", next));
      toast(t("shortcut.saved"));
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    }
  }

  /** While recording, the next key combination becomes the action's shortcut. */
  private onKey = (e: KeyboardEvent) => {
    if (!this.recording) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.key === "Escape") {
      this.stop();
      return;
    }
    const combo = comboOf(e);
    if (!combo) return; // only modifiers so far
    const id = this.recording;
    this.stop();
    const taken = SHORTCUT_ACTIONS.find((a) => a.id !== id && comboFor(a.id).toLowerCase() === combo.toLowerCase());
    if (taken) {
      this.error = t("shortcut.taken", { combo: comboLabel(combo), action: t(taken.label as any) });
      return;
    }
    void this.save({ ...customShortcuts.get(), [id]: combo });
  };

  private record(id: string) {
    this.recording = id;
    this.error = "";
    // Capture before the global shortcuts see the keys.
    window.addEventListener("keydown", this.onKey, true);
  }

  private stop() {
    this.recording = "";
    window.removeEventListener("keydown", this.onKey, true);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.stop();
  }

  render() {
    const custom = customShortcuts.get();
    return html`<div class="enter">
      ${settingsNav("shortcuts")}
      <h1>${t("shortcut.title")}</h1>
      <p class="muted">${t("shortcut.intro")}</p>
      ${this.error ? html`<p class="error" role="alert">${this.error}</p>` : ""}
      <table>${SHORTCUT_ACTIONS.map((a) => {
        const combo = comboFor(a.id, custom);
        const changed = a.id in custom;
        return html`<tr>
          <td>${t(a.label as any)}</td>
          <td class="keys">
            <button class="ghost rec" aria-pressed=${this.recording === a.id ? "true" : "false"} @click=${() => (this.recording === a.id ? this.stop() : this.record(a.id))}
              aria-label=${t("shortcut.change", { action: t(a.label as any) })}>
              ${this.recording === a.id ? t("shortcut.press") : combo ? html`<kbd>${comboLabel(combo)}</kbd>` : html`<span class="muted">${t("shortcut.none")}</span>`}</button>
            ${combo ? html`<button class="ghost" title=${t("shortcut.off")} aria-label=${t("shortcut.off")} @click=${() => this.save({ ...custom, [a.id]: "" })}>${icon("X", 13)}</button>` : ""}
            ${changed ? html`<button class="ghost" title=${t("shortcut.reset")} aria-label=${t("shortcut.reset")} @click=${() => { const next = { ...custom }; delete next[a.id]; void this.save(next); }}>${icon("RotateCcw", 13)}</button>` : ""}
          </td></tr>`;
      })}</table>
      <p class="muted" style="font-size:13px">${t("shortcut.hint")}</p>
    </div>`;
  }
}
customElements.define("phx-shortcut-settings", PhxShortcutSettings);
