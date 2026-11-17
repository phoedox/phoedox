// The checkout page of providers whose checkout runs on our own domain
// (Paddle: Paddle.js opens its overlay for ?_ptxn=<transaction>). This
// address is approved as Paddle's default payment link (ADR-0079).

import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

interface PayInfo { provider: string; clientToken: string; sandbox: boolean; successUrl: string; cancelUrl: string }

interface PaddleJS {
  Environment: { set(env: string): void };
  Initialize(o: { token: string; eventCallback?: (e: { name?: string }) => void }): void;
  Checkout: { open(o: { transactionId: string; settings?: Record<string, unknown> }): void };
}

const PADDLE_JS = "https://cdn.paddle.com/paddle/v2/paddle.js";

function loadScript(src: string): Promise<void> {
  return new Promise((ok, fail) => {
    if (document.querySelector(`script[src="${src}"]`)) return ok();
    const s = document.createElement("script");
    s.src = src;
    s.onload = () => ok();
    s.onerror = () => fail(new Error("script"));
    document.head.append(s);
  });
}

export class PhxPay extends LitElement {
  static properties = { error: { state: true }, done: { state: true } };
  static styles = [base, css`
    :host { display: grid; place-items: center; min-height: 100dvh; background: var(--bg); }
    .card { max-width: 420px; padding: 32px 24px; text-align: center; }
    .card p { color: var(--muted); }
    img { width: 40px; height: 40px; }
  `];

  error = "";
  done = false;
  private info?: PayInfo;

  connectedCallback() {
    super.connectedCallback();
    void this.start();
  }

  private async start() {
    const txn = new URLSearchParams(location.search).get("_ptxn") ?? "";
    if (!txn) {
      this.error = t("pay.page_missing");
      return;
    }
    try {
      this.info = await api<PayInfo>("GET", `/payments/pay/${encodeURIComponent(txn)}`);
      if (!this.info.clientToken) throw new Error("token");
      await loadScript(PADDLE_JS);
      const P = (window as unknown as { Paddle?: PaddleJS }).Paddle;
      if (!P) throw new Error("paddle");
      if (this.info.sandbox) P.Environment.set("sandbox");
      P.Initialize({ token: this.info.clientToken, eventCallback: (e) => this.onEvent(e.name ?? "") });
      P.Checkout.open({ transactionId: txn, settings: { displayMode: "overlay", locale: document.documentElement.lang || undefined } });
    } catch (e) {
      this.error = e instanceof ApiError && e.status !== 404 ? e.text : t("pay.page_missing");
    }
  }

  private onEvent(name: string) {
    if (!this.info) return;
    if (name === "checkout.completed") {
      this.done = true;
      setTimeout(() => location.assign(this.info!.successUrl), 1500);
    } else if (name === "checkout.closed" && !this.done) {
      location.assign(this.info.cancelUrl);
    }
  }

  render() {
    return html`<div class="card">
      <img src="/logo.svg" alt="" />
      ${this.error
        ? html`<p>${icon("CircleAlert", 16)} ${this.error}</p>`
        : this.done ? html`<p>${icon("CircleCheck", 16)} ${t("pay.thanks")}</p>` : html`<p>${t("pay.page_opening")}</p>`}
      ${this.error ? html`<a href="/">${t("pay.page_back")}</a>` : nothing}
    </div>`;
  }
}
customElements.get("phx-pay") || customElements.define("phx-pay", PhxPay);
