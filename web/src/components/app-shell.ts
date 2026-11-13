import { LitElement, css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { t } from "../i18n";

type Health = { status: string; version: string };

/** Root element. Routing per project/document (/p/:project/doc/:id) comes next. */
@customElement("phx-app")
export class PhxApp extends LitElement {
  static styles = css`
    :host { display: block; min-height: 100vh; }
    header { display: flex; align-items: center; gap: 12px; padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--surface); }
    b { font-family: var(--font-display); font-size: 18px; }
    main { padding: 24px 16px; color: var(--muted); }
  `;

  @state() private health?: Health;
  @state() private error = false;

  connectedCallback(): void {
    super.connectedCallback();
    fetch("/healthz")
      .then((r) => r.json() as Promise<Health>)
      .then((h) => (this.health = h))
      .catch(() => (this.error = true));
  }

  render() {
    return html`
      <header><b>Phoedox</b><span>${t("nav.projects")}</span></header>
      <main>
        ${this.error
          ? t("app.offline")
          : this.health
            ? t("app.status", { status: this.health.status, version: this.health.version })
            : t("app.loading")}
      </main>
    `;
  }
}
