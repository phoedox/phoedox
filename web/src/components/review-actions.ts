import { LitElement, css, html } from "lit";
import { ApiError, api, type DraftInfo } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { currentProject } from "../state";
import { base } from "./shared";
import { toast } from "./toasts";

/** Approval workflow in the draft bar (ADR-0042): editors ask for a review,
 * owners send a draft back with a note. Publishing stays with the host. */
export class PhxReviewActions extends LitElement {
  static properties = { project: {}, doc: {}, draft: { attribute: false }, asking: { state: true }, busy: { state: true } };
  static styles = [base, css`
    :host { display: contents; }
    .badge { display: inline-flex; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); }
    dialog textarea { width: 100%; min-height: 90px; margin: 8px 0 12px; }
  `];
  project = "";
  doc = "";
  draft?: DraftInfo;
  asking: "" | "review" | "reject" = "";
  busy = false;

  private get isOwner() {
    return currentProject.get()?.role === "owner";
  }

  private open(what: "review" | "reject") {
    this.asking = what;
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.showModal());
  }

  private async send(e: Event) {
    e.preventDefault();
    const note = (this.renderRoot.querySelector("textarea") as HTMLTextAreaElement).value;
    const what = this.asking;
    this.busy = true;
    try {
      const d = await api<DraftInfo>("POST", `/projects/${this.project}/documents/${this.doc}/draft/${what}`, { note });
      this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close();
      toast(what === "review" ? t("review.requested_toast") : t("review.rejected_toast"), "ok");
      this.dispatchEvent(new CustomEvent("draft-changed", { detail: d, bubbles: true, composed: true }));
    } catch (err) {
      toast(err instanceof ApiError ? err.text : t("error.unknown"), "error");
    } finally {
      this.busy = false;
    }
  }

  private dialog() {
    if (!this.asking) return "";
    const review = this.asking === "review";
    return html`<dialog @close=${() => (this.asking = "")} aria-labelledby="rv-title"><form @submit=${this.send}>
      <h2 id="rv-title">${review ? t("review.request_title") : t("review.reject_title")}</h2>
      <p class="muted">${review ? t("review.request_text") : t("review.reject_text")}</p>
      <label>${t("review.note")}<textarea name="note" ?required=${!review}></textarea></label>
      <div class="row"><button type="button" class="ghost" @click=${() => this.renderRoot.querySelector<HTMLDialogElement>("dialog")?.close()}>${t("doc.cancel")}</button>
        <button ?disabled=${this.busy}>${review ? html`${icon("Send", 14)} ${t("review.request")}` : html`${icon("Undo2", 14)} ${t("review.reject")}`}</button></div>
    </form></dialog>`;
  }

  render() {
    const d = this.draft;
    if (!d?.approval) return "";
    return html`
      ${d.reviewBy ? html`<span class="badge" role="status">${icon("Hourglass", 14)} ${t("review.waiting", { name: d.reviewBy.name })}</span>` : ""}
      ${!d.canPublish && d.canEnd && !d.reviewBy ? html`<button @click=${() => this.open("review")}>${icon("Send", 15)} ${t("review.request")}</button>` : ""}
      ${this.isOwner && d.reviewBy ? html`<button class="ghost" @click=${() => this.open("reject")}>${icon("Undo2", 15)} ${t("review.reject")}</button>` : ""}
      ${this.dialog()}`;
  }
}
customElements.define("phx-review-actions", PhxReviewActions);
