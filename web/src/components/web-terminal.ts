import { LitElement, css, html, unsafeCSS } from "lit";
import xtermCSS from "@xterm/xterm/css/xterm.css?inline";
import { t } from "../i18n";
import { icon } from "../icon";
import { base } from "./shared";

/** A web terminal through the agent (ADR-0048). The connection waits until
 * the agent picked up the signed job; everything typed and shown is recorded. */
export class PhxWebTerminal extends LitElement {
  static properties = { project: {}, job: {}, host: {}, state: { state: true } };
  static styles = [base, unsafeCSS(xtermCSS), css`
    :host { display: block; }
    .bar { display: flex; gap: 8px; align-items: center; margin-bottom: 6px; font-size: 13px; }
    .bar b { margin-right: auto; }
    .term { height: 420px; border-radius: 8px; overflow: hidden; }
    .note { font-size: 12.5px; color: var(--muted); margin: 6px 0 0; }
  `];
  project = "";
  job = "";
  host = "";
  state: "connecting" | "open" | "closed" = "connecting";
  private ws?: WebSocket;
  private term?: import("@xterm/xterm").Terminal;
  private observer?: ResizeObserver;

  async firstUpdated() {
    const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]);
    const el = this.renderRoot.querySelector<HTMLElement>(".term")!;
    const term = new Terminal({ cursorBlink: true, fontFamily: "IBM Plex Mono, ui-monospace, monospace", fontSize: 13, convertEol: false });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    fit.fit();
    this.term = term;
    term.writeln(t("term.waiting"));
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/api/v1/projects/${this.project}/terminal/${this.job}`);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    const resize = () => {
      fit.fit();
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    };
    ws.onopen = () => resize();
    ws.onmessage = (e) => {
      if (this.state !== "open") {
        this.state = "open";
        term.reset();
      }
      term.write(new Uint8Array(e.data as ArrayBuffer));
    };
    ws.onclose = () => {
      this.state = "closed";
      term.writeln(`\r\n${t("term.closed")}`);
      this.dispatchEvent(new CustomEvent("closed", { bubbles: true, composed: true }));
    };
    const enc = new TextEncoder();
    term.onData((d) => ws.readyState === WebSocket.OPEN && ws.send(enc.encode(d)));
    this.observer = new ResizeObserver(() => resize());
    this.observer.observe(el);
    term.focus();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.observer?.disconnect();
    this.ws?.close();
    this.term?.dispose();
  }

  private end() {
    this.ws?.send(JSON.stringify({ type: "close" }));
    this.ws?.close();
  }

  render() {
    return html`<div class="bar"><b>${icon("SquareTerminal", 14)} ${this.host}</b>
      <span class="muted" role="status">${t(`term.${this.state}` as any)}</span>
      <button class="ghost" ?disabled=${this.state === "closed"} @click=${this.end}>${icon("X", 13)} ${t("term.end")}</button></div>
      <div class="term"></div>
      <p class="note">${t("term.recorded")}</p>`;
  }
}
customElements.define("phx-web-terminal", PhxWebTerminal);
