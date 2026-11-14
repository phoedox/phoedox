import { LitElement, css, html } from "lit";
import { base } from "./shared";

type Action = { label: string; run: () => void };
type Toast = { id: number; text: string; kind: "ok" | "error"; action?: Action; leaving?: boolean };

/** Toasts: transitions (not keyframes) so rapid toasts retarget; exit the way they entered. */
export class PhxToasts extends LitElement {
  static properties = { items: { state: true } };
  static styles = [base, css`
    :host { position: fixed; right: 16px; bottom: 16px; z-index: 50; display: flex; flex-direction: column; gap: 8px; pointer-events: none; }
    .t {
      pointer-events: auto; background: var(--fg); color: var(--bg); border-radius: 10px; padding: 10px 14px; font-size: 14px;
      box-shadow: 0 10px 30px rgb(0 0 0 / 0.2); max-width: 360px;
      transition: opacity var(--dur) var(--ease-out), transform var(--dur) var(--ease-out);
      @starting-style { opacity: 0; transform: translateY(100%); }
    }
    .t.error { background: var(--bad); color: var(--on-color); }
    .t { display: flex; align-items: center; gap: 12px; }
    .t button { background: none; border: 0; padding: 0; color: inherit; font-weight: 700; text-decoration: underline; text-underline-offset: 3px; }
    .t.leaving { opacity: 0; transform: translateY(100%); }
    @media (prefers-reduced-motion: reduce) {
      .t { transition: opacity var(--dur) ease; @starting-style { opacity: 0; transform: none; } }
      .t.leaving { transform: none; }
    }
  `];
  items: Toast[] = [];
  private seq = 0;

  show(text: string, kind: Toast["kind"] = "ok", action?: Action) {
    const id = ++this.seq;
    this.items = [...this.items, { id, text, kind, action }];
    // Toasts with an action stay long enough to reach the button.
    setTimeout(() => this.dismiss(id), action ? 8000 : kind === "error" ? 5000 : 2500);
  }

  private dismiss(id: number) {
    if (!this.items.some((t) => t.id === id && !t.leaving)) return;
    this.items = this.items.map((t) => (t.id === id ? { ...t, leaving: true } : t));
    setTimeout(() => (this.items = this.items.filter((t) => t.id !== id)), 260);
  }

  render() {
    return this.items.map((t) => html`<div class="t ${t.kind} ${t.leaving ? "leaving" : ""}" role=${t.kind === "error" ? "alert" : "status"}>
      <span>${t.text}</span>
      ${t.action ? html`<button @click=${() => { t.action!.run(); this.dismiss(t.id); }}>${t.action.label}</button>` : ""}
    </div>`);
  }
}
customElements.define("phx-toasts", PhxToasts);

export function toast(text: string, kind: "ok" | "error" = "ok", action?: Action) {
  let el = document.querySelector("phx-toasts") as PhxToasts | null;
  if (!el) {
    el = document.createElement("phx-toasts") as PhxToasts;
    document.body.append(el);
  }
  el.show(text, kind, action);
}
