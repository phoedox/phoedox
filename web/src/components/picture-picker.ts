import { LitElement, css, html } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { uploadAvatar, uploadProjectImage } from "../pictures";
import { base } from "./shared";
import { toast } from "./toasts";

/** Choose or remove a profile picture or project image; fires "changed" with the new version. */
export class PhxPicturePicker extends LitElement {
  static properties = { project: {}, has: { type: Boolean }, busy: { state: true } };
  static styles = [base, css`
    :host { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
    .row { display: flex; gap: 8px; flex-wrap: wrap; }
    .hint { flex-basis: 100%; font-size: 12.5px; color: var(--muted); margin: 0; }
  `];
  /** Project slug; empty = own profile picture. */
  project = "";
  has = false;
  busy = false;

  private async pick(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    this.busy = true;
    try {
      const r = this.project ? await uploadProjectImage(this.project, file) : await uploadAvatar(file);
      this.dispatchEvent(new CustomEvent("changed", { detail: r.version }));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("picture.failed"), "error");
    } finally {
      this.busy = false;
    }
  }

  private async removePicture() {
    this.busy = true;
    try {
      await api("DELETE", this.project ? `/projects/${this.project}/image` : "/me/avatar");
      this.dispatchEvent(new CustomEvent("changed", { detail: "" }));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  render() {
    return html`<slot></slot>
      <div class="row">
        <button type="button" class="ghost" ?disabled=${this.busy} @click=${() => this.renderRoot.querySelector<HTMLInputElement>("input")!.click()}>${icon("Upload", 14)} ${t("picture.choose")}</button>
        ${this.has ? html`<button type="button" class="ghost" ?disabled=${this.busy} @click=${this.removePicture}>${icon("Trash2", 14)} ${t("picture.remove")}</button>` : ""}
        <input type="file" accept="image/png,image/jpeg,image/webp,image/gif,image/avif" hidden @change=${this.pick}>
      </div>
      <p class="hint">${t("picture.hint")}</p>`;
  }
}
customElements.define("phx-picture-picker", PhxPicturePicker);
