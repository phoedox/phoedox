import { LitElement, css, html, nothing } from "lit";
import { ApiError, api } from "../api";
import { locale, t } from "../i18n";
import { icon } from "../icon";
import { solvePow } from "../pow";
import { base } from "./shared";

interface Challenge { off?: boolean; provider?: string; siteKey?: string; challenge?: string; prefix?: string; difficulty?: number; minMs?: number; emailFallback?: boolean }
interface Visible { kind: "slider" | "rotate" | "email"; challenge: string; image?: string; piece?: string; pieceY?: number; width?: number; height?: number; size?: number }
interface Result { token?: string; visible?: Visible; blocked?: boolean; emailFallback?: boolean; retry?: boolean }

const PROVIDERS: Record<string, { script: string; global: string; name: string; privacy: string }> = {
  turnstile: { script: "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit", global: "turnstile", name: "Cloudflare Turnstile", privacy: "https://www.cloudflare.com/privacypolicy/" },
  hcaptcha: { script: "https://js.hcaptcha.com/1/api.js?render=explicit", global: "hcaptcha", name: "hCaptcha", privacy: "https://www.hcaptcha.com/privacy" },
  recaptcha: { script: "https://www.google.com/recaptcha/api.js?render=explicit", global: "grecaptcha", name: "Google reCAPTCHA", privacy: "https://policies.google.com/privacy" },
};

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (document.querySelector(`script[src="${src}"]`)) return resolve();
    const s = document.createElement("script");
    s.src = src;
    s.async = true;
    s.onload = () => resolve();
    s.onerror = () => reject(new Error("script"));
    document.head.append(s);
  });
}

/** Bot check of a place (internal/botcheck). Put it inside a form and call
 * `token()` before sending: it runs the invisible checks (proof of work in a
 * worker, behaviour signals, honeypot, timing) and, when needed, shows a
 * puzzle, the e-mail code or the provider's widget. Resolves with the pass
 * token ("" when the place is not protected). */
export class PhxBotcheck extends LitElement {
  static properties = { place: {}, visible: { state: true }, blocked: { state: true }, emailFallback: { state: true }, busy: { state: true }, error: { state: true }, value: { state: true }, emailStep: { state: true }, provider: { state: true } };
  static styles = [base, css`
    :host { display: block; }
    .box { border: 1px solid var(--line); border-radius: 12px; background: var(--surface); padding: 12px; display: grid; gap: 10px; margin-top: 4px; }
    .box h3 { margin: 0; font-size: 14.5px; }
    .stage { position: relative; width: 100%; max-width: 300px; border-radius: 8px; overflow: hidden; touch-action: none; }
    .stage img.bg { display: block; width: 100%; height: auto; }
    .stage img.piece { position: absolute; left: 0; top: 0; box-shadow: 0 2px 8px rgb(0 0 0 / 0.35); border-radius: 3px; pointer-events: none; }
    .rot { width: 160px; height: 160px; border-radius: 50%; overflow: hidden; margin: 0 auto; }
    .rot img { width: 100%; height: 100%; display: block; }
    input[type=range] { width: 100%; max-width: 300px; }
    .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .muted { color: var(--muted); font-size: 13px; margin: 0; }
    .err { color: var(--bad); font-size: 13.5px; margin: 0; }
    .linkish { background: none; border: 0; padding: 0; color: var(--accent); text-decoration: underline; cursor: pointer; font-size: 13px; }
    @media (prefers-reduced-motion: no-preference) { .rot img { transition: transform 120ms var(--ease-out); } }
  `];

  place = "";
  visible?: Visible;
  blocked = false;
  emailFallback = false;
  busy = false;
  error = "";
  value = 0;
  emailStep: "" | "address" | "code" = "";
  provider = "";

  private ch?: Challenge;
  private ready?: Promise<void>;
  private powNonce?: Promise<string>;
  private started = performance.now();
  private moves = 0;
  private dist = 0;
  private last?: [number, number];
  private keys: number[] = [];
  private touches = 0;
  private focusCount = 0;
  private honeypot?: HTMLInputElement;
  private waiting?: { resolve: (t: string) => void; reject: (e: unknown) => void };
  private providerResponse?: string;
  private providerWaiter?: (r: string) => void;
  private emailToken = "";

  private onMove = (e: PointerEvent) => {
    this.moves++;
    if (this.last) this.dist += Math.hypot(e.clientX - this.last[0], e.clientY - this.last[1]);
    this.last = [e.clientX, e.clientY];
  };
  private onKey = () => { this.keys.push(performance.now()); };
  private onTouch = () => { this.touches++; };
  private onFocus = () => { this.focusCount++; };

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("pointermove", this.onMove, { passive: true });
    window.addEventListener("keydown", this.onKey, { passive: true });
    window.addEventListener("touchstart", this.onTouch, { passive: true });
    window.addEventListener("focusin", this.onFocus, { passive: true });
    this.ready = this.init();
    // Honeypot: a field people never see, in the form itself (bots fill it).
    const form = this.closest("form");
    if (form && !form.querySelector("input[data-hp]")) {
      const hp = document.createElement("input");
      hp.name = "website";
      hp.tabIndex = -1;
      hp.autocomplete = "off";
      hp.setAttribute("aria-hidden", "true");
      hp.dataset.hp = "1";
      hp.style.cssText = "position:absolute;left:-10000px;width:1px;height:1px;opacity:0";
      form.append(hp);
      this.honeypot = hp;
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("pointermove", this.onMove);
    window.removeEventListener("keydown", this.onKey);
    window.removeEventListener("touchstart", this.onTouch);
    window.removeEventListener("focusin", this.onFocus);
  }

  private async init() {
    this.visible = undefined;
    this.blocked = false;
    this.error = "";
    this.emailStep = "";
    this.ch = await api<Challenge>("POST", "/bot/challenge", { place: this.place }).catch(() => ({ off: true }) as Challenge);
    this.emailFallback = !!this.ch.emailFallback;
    if (this.ch.off) return;
    if (this.ch.provider && this.ch.provider !== "own") {
      this.provider = this.ch.provider;
      await this.renderProvider();
      return;
    }
    this.powNonce = solvePow(this.ch.prefix!, this.ch.difficulty!);
  }

  private async renderProvider() {
    const p = PROVIDERS[this.provider];
    if (!p || !this.ch?.siteKey) return;
    await loadScript(p.script).catch(() => (this.error = t("bot.provider_failed")));
    let host = this.querySelector<HTMLDivElement>("div[data-widget]");
    if (!host) {
      host = document.createElement("div");
      host.dataset.widget = "1";
      this.append(host);
    }
    host.replaceChildren();
    const g = () => (window as any)[p.global];
    for (let i = 0; i < 50 && !g()?.render; i++) await new Promise((r) => setTimeout(r, 100));
    g()?.render(host, { sitekey: this.ch.siteKey, callback: (r: string) => { this.providerResponse = r; this.providerWaiter?.(r); } });
  }

  private signals() {
    const iv = this.keys.slice(1).map((k, i) => k - this.keys[i]);
    const mean = iv.reduce((a, b) => a + b, 0) / (iv.length || 1);
    const variance = iv.reduce((a, b) => a + (b - mean) ** 2, 0) / (iv.length || 1);
    const nav = navigator as Navigator & { webdriver?: boolean };
    const interaction = Math.min(1, this.moves / 40 + this.keys.length / 15 + this.touches / 3);
    return {
      elapsedMs: Math.round(performance.now() - this.started), honeypot: !!this.honeypot?.value, pointerMoves: this.moves, pointerDist: Math.round(this.dist),
      keys: this.keys.length, keyVariance: Math.round(variance), touches: this.touches, focus: this.focusCount, webdriver: !!nav.webdriver,
      headless: /HeadlessChrome/.test(navigator.userAgent), plugins: navigator.plugins?.length ?? 0, languages: navigator.languages?.length ?? 0,
      behaviour: Math.round(interaction * 100) / 100,
    };
  }

  /** Runs the checks; resolves with the pass token. */
  async token(): Promise<string> {
    await this.ready;
    const ch = this.ch;
    if (!ch || ch.off) return "";
    try {
      if (this.provider) {
        const response = this.providerResponse ?? (await new Promise<string>((r) => (this.providerWaiter = r)));
        const res = await api<Result>("POST", "/bot/provider", { challenge: ch.challenge, response });
        return await this.handle(res);
      }
      this.busy = true;
      const nonce = await this.powNonce!;
      const res = await api<Result>("POST", "/bot/verify", { challenge: ch.challenge, nonce, signals: this.signals() });
      return await this.handle(res);
    } finally {
      this.busy = false;
      // Challenges and tokens are single use: prepare the next attempt.
      this.started = performance.now();
      this.providerResponse = undefined;
      this.ready = this.init();
    }
  }

  private handle(res: Result): Promise<string> {
    if (res.token) return Promise.resolve(res.token);
    this.emailFallback = !!res.emailFallback || this.emailFallback;
    if (res.visible) this.show(res.visible);
    if (res.blocked) {
      this.blocked = true;
      if (!this.emailFallback) return Promise.reject(new Error(t("bot.blocked")));
    }
    return new Promise((resolve, reject) => (this.waiting = { resolve, reject }));
  }

  private show(v: Visible) {
    this.visible = v;
    this.value = 0;
    this.error = "";
    void this.updateComplete.then(() => this.renderRoot.querySelector<HTMLInputElement>("input[type=range]")?.focus());
  }

  private done(token: string) {
    this.visible = undefined;
    this.blocked = false;
    this.emailStep = "";
    this.waiting?.resolve(token);
    this.waiting = undefined;
  }

  private async solve() {
    if (!this.visible) return;
    this.busy = true;
    try {
      const v = this.visible;
      const value = v.kind === "slider" ? Math.round(this.value * (v.width! / this.stageWidth())) : this.value;
      const res = await api<Result>("POST", "/bot/solve", { challenge: v.challenge, value });
      if (res.token) return this.done(res.token);
      if (res.visible) {
        this.show(res.visible);
        this.error = t("bot.wrong");
      }
      if (res.blocked) {
        this.visible = undefined;
        this.blocked = true;
        this.emailFallback = !!res.emailFallback;
        if (!this.emailFallback) {
          this.waiting?.reject(new Error(t("bot.blocked")));
          this.waiting = undefined;
        }
      }
    } catch (e) {
      this.error = e instanceof ApiError ? e.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private stageWidth() {
    return this.renderRoot.querySelector<HTMLElement>(".stage")?.clientWidth || this.visible?.width || 300;
  }

  private async sendCode(e: SubmitEvent | Event) {
    e.preventDefault();
    const email = this.renderRoot.querySelector<HTMLInputElement>("input[name=botmail]")?.value ?? "";
    this.busy = true;
    try {
      const res = await api<Result>("POST", "/bot/email", { challenge: this.visible?.challenge ?? this.ch?.challenge, email, locale: locale() });
      this.emailToken = res.visible?.challenge ?? "";
      this.emailStep = "code";
      this.error = "";
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private async verifyCode(e: Event) {
    e.preventDefault();
    const code = this.renderRoot.querySelector<HTMLInputElement>("input[name=botcode]")?.value ?? "";
    this.busy = true;
    try {
      const res = await api<Result>("POST", "/bot/email/verify", { challenge: this.emailToken, code });
      if (res.token) return this.done(res.token);
      if (res.visible) this.emailToken = res.visible.challenge;
      this.error = res.blocked ? t("bot.blocked") : t("bot.code_wrong");
    } catch (err) {
      this.error = err instanceof ApiError ? err.text : t("error.unknown");
    } finally {
      this.busy = false;
    }
  }

  private onKeyStage(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      void this.solve();
    }
  }

  private puzzle() {
    const v = this.visible!;
    if (v.kind === "slider") {
      const scale = this.stageWidth() / (v.width ?? 300);
      const max = Math.round(((v.width ?? 300) - (v.size ?? 44)) * scale);
      return html`<h3>${t("bot.slider")}</h3>
        <div class="stage"><img class="bg" src=${v.image!} alt="" @load=${() => this.requestUpdate()}>
          <img class="piece" src=${v.piece!} alt="" style="width:${(v.size ?? 44) * scale}px;top:${(v.pieceY ?? 0) * scale}px;transform:translateX(${this.value}px)"></div>
        <input type="range" min="0" max=${max} step="1" .value=${String(this.value)} aria-label=${t("bot.slider")} @input=${(e: Event) => (this.value = Number((e.target as HTMLInputElement).value))} @keydown=${this.onKeyStage}>`;
    }
    return html`<h3>${t("bot.rotate")}</h3>
      <div class="rot"><img src=${v.image!} alt="" style="transform:rotate(${this.value}deg)"></div>
      <div class="row" style="justify-content:center;flex-wrap:nowrap">
        <button type="button" class="ghost" aria-label=${t("bot.rotate_left")} @click=${() => (this.value = (this.value + 345) % 360)}>${icon("RotateCcw", 15)}</button>
        <input type="range" min="0" max="359" step="1" .value=${String(this.value)} aria-label=${t("bot.rotate")} @input=${(e: Event) => (this.value = Number((e.target as HTMLInputElement).value))} @keydown=${this.onKeyStage} style="max-width:200px;min-width:0;flex:1">
        <button type="button" class="ghost" aria-label=${t("bot.rotate_right")} @click=${() => (this.value = (this.value + 15) % 360)}>${icon("RotateCw", 15)}</button>
      </div>`;
  }

  private emailUI() {
    return this.emailStep === "code"
      ? html`<p class="muted">${t("bot.code_sent")}</p>
        <div class="row"><input name="botcode" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="123456" style="max-width:140px" aria-label=${t("bot.code")}
          @keydown=${(e: KeyboardEvent) => e.key === "Enter" && this.verifyCode(e)}>
        <button type="button" ?disabled=${this.busy} @click=${this.verifyCode}>${t("bot.verify_code")}</button></div>`
      : html`<div class="row"><input name="botmail" type="email" autocomplete="email" placeholder=${t("bot.email")} aria-label=${t("bot.email")} style="max-width:260px"
          @keydown=${(e: KeyboardEvent) => e.key === "Enter" && this.sendCode(e)}>
        <button type="button" ?disabled=${this.busy} @click=${this.sendCode}>${t("bot.send_code")}</button></div>`;
  }

  render() {
    const p = this.provider ? PROVIDERS[this.provider] : undefined;
    const notice = p ? html`<p class="muted">${t("bot.provider_notice", { provider: p.name })} <a href=${p.privacy} target="_blank" rel="noopener">${t("bot.privacy")}</a></p>` : nothing;
    if (!this.visible && !this.blocked && !this.emailStep) {
      return html`<slot></slot>${notice}${this.error ? html`<p class="err">${this.error}</p>` : nothing}`;
    }
    return html`<slot></slot><div class="box" role="group" aria-label=${t("bot.title")}>
      ${this.emailStep ? html`<h3>${t("bot.email_title")}</h3>${this.emailUI()}`
        : this.blocked ? html`<p class="err">${icon("ShieldAlert", 14)} ${t("bot.blocked")}</p>`
        : html`${this.puzzle()}<div class="row"><button type="button" ?disabled=${this.busy} @click=${this.solve}>${t("bot.check")}</button></div>`}
      ${this.error ? html`<p class="err" role="alert">${this.error}</p>` : nothing}
      ${this.emailFallback && !this.emailStep ? html`<button type="button" class="linkish" @click=${() => (this.emailStep = "address")}>${t("bot.cant_solve")}</button>` : nothing}
    </div>${notice}`;
  }
}
customElements.get("phx-botcheck") || customElements.define("phx-botcheck", PhxBotcheck);

/** Runs a request with the bot check of a place: asks for a token first
 * when the place is protected. */
export async function withBot(el: PhxBotcheck | null | undefined): Promise<string> {
  return el ? el.token() : "";
}
