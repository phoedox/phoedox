// Feedback widget for websites and games: a button that opens the portal's
// report form in a frame served by Phoedox (/feedback/<handle>/embed).
//
//   <script src="https://phoedox.example/feedback-widget.js" data-portal="starfall" async></script>
//
// Options (data-* on the script tag): portal (required), lang, position
// (right | left), button ("false" hides it; open with the API), label,
// color (#rrggbb of the button), canvas (CSS selector of a canvas to take
// screenshots from).
//
// API on window.PhoedoxFeedback: open({ type, title, screenshot }), close(),
// setContext({ key: value }), setPlayerToken(token), setScreenshot(fn).
//
// The form, the bot check, the screenshot editor and sending all run inside
// the frame on the Phoedox origin; this page only passes context and, for
// games, a screenshot of its canvas.
import { MessageSquare, X } from "lucide";
import { strings } from "./strings";

type Shot = Blob | HTMLCanvasElement;
type ShotFn = () => Shot | Promise<Shot>;
interface OpenOptions { type?: string; title?: string; screenshot?: Shot }
interface Api {
  open(o?: OpenOptions): void;
  close(): void;
  setContext(ctx: Record<string, unknown>): void;
  setPlayerToken(token: string): void;
  setScreenshot(fn: ShotFn | null): void;
}

declare global {
  interface Window { PhoedoxFeedback?: Api }
}

type IconNode = [string, Record<string, string | number>][];

function svgIcon(node: IconNode, size: number) {
  const ns = "http://www.w3.org/2000/svg";
  const s = document.createElementNS(ns, "svg");
  for (const [k, v] of Object.entries({ width: size, height: size, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": 2, "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true" })) s.setAttribute(k, String(v));
  for (const [tag, attrs] of node) {
    const el = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
    s.append(el);
  }
  return s;
}

function pickLang(wanted: string): keyof typeof strings {
  const l = wanted.slice(0, 2).toLowerCase();
  return l in strings ? (l as keyof typeof strings) : "en";
}

function toBlob(shot: Shot): Promise<Blob | null> {
  if (shot instanceof Blob) return Promise.resolve(shot);
  return new Promise((resolve) => {
    try {
      shot.toBlob((b) => resolve(b), "image/png");
    } catch {
      resolve(null); // tainted canvas
    }
  });
}

const CSS = `
:host { all: initial; }
.btn { position: fixed; bottom: 20px; z-index: 2147483000; display: inline-flex; align-items: center; gap: 8px; padding: 10px 16px; border: 0; border-radius: 999px;
  background: var(--phx-accent, #e8590c); color: #fff; font: 600 14px/1.2 system-ui, sans-serif; cursor: pointer; box-shadow: 0 4px 16px rgb(0 0 0 / 0.22);
  transition: transform 140ms cubic-bezier(0.23, 1, 0.32, 1); }
.btn:active { transform: scale(0.97); }
.btn:focus-visible { outline: 3px solid #fff; outline-offset: 2px; box-shadow: 0 0 0 6px var(--phx-accent, #e8590c); }
.btn.left, .panel.left { left: 20px; } .btn.right, .panel.right { right: 20px; }
.panel { position: fixed; bottom: 20px; z-index: 2147483001; width: 400px; height: min(680px, calc(100vh - 40px)); max-width: calc(100vw - 40px);
  border-radius: 16px; overflow: hidden; background: #fff; box-shadow: 0 12px 48px rgb(0 0 0 / 0.28);
  opacity: 0; transform: scale(0.96); visibility: hidden; pointer-events: none;
  transition: opacity 200ms cubic-bezier(0.23, 1, 0.32, 1), transform 200ms cubic-bezier(0.23, 1, 0.32, 1), visibility 0s linear 200ms; }
.panel.right { transform-origin: bottom right; } .panel.left { transform-origin: bottom left; }
.panel.open { opacity: 1; transform: none; visibility: visible; pointer-events: auto; transition-delay: 0s; }
.panel.capturing { transition: none; opacity: 0; visibility: hidden; }
.panel.big { width: calc(100vw - 40px); height: calc(100vh - 40px); }
.panel iframe { border: 0; width: 100%; height: 100%; display: block; color-scheme: normal; }
@media (max-width: 520px) { .panel, .panel.big { left: 0; right: 0; bottom: 0; width: 100vw; max-width: none; height: 100dvh; border-radius: 0; } }
@media (prefers-reduced-motion: reduce) { .panel { transform: none; } .btn { transition: none; } }
`;

function init() {
  const script = (document.currentScript as HTMLScriptElement | null) ?? document.querySelector<HTMLScriptElement>("script[data-portal][src*='feedback-widget']");
  if (!script || window.PhoedoxFeedback) return;
  const ds = script.dataset;
  const portal = (ds.portal ?? "").trim();
  if (!/^[a-z0-9-]{3,40}$/.test(portal)) {
    console.warn("Phoedox feedback: data-portal is missing or invalid");
    return;
  }
  const origin = new URL(script.src, location.href).origin;
  const lang = pickLang(ds.lang || document.documentElement.lang || navigator.language || "en");
  const text = strings[lang];
  const side = ds.position === "left" ? "left" : "right";

  const host = document.createElement("div");
  host.setAttribute("data-phoedox-feedback", "");
  if (/^#[0-9a-fA-F]{6}$/.test(ds.color ?? "")) host.style.setProperty("--phx-accent", ds.color!);
  const root = host.attachShadow({ mode: "open" });
  const style = document.createElement("style");
  style.textContent = CSS;
  root.append(style);

  let button: HTMLButtonElement | null = null;
  if (ds.button !== "false") {
    button = document.createElement("button");
    button.className = `btn ${side}`;
    button.type = "button";
    button.setAttribute("aria-haspopup", "dialog");
    button.setAttribute("aria-expanded", "false");
    const label = document.createElement("span");
    label.textContent = ds.label || text["fbw.l.open"];
    button.append(svgIcon(MessageSquare as unknown as IconNode, 16), label);
    button.addEventListener("click", () => (panel.classList.contains("open") ? api.close() : api.open()));
    root.append(button);
  }

  const panel = document.createElement("div");
  panel.className = `panel ${side}`;
  panel.setAttribute("role", "dialog");
  panel.setAttribute("aria-label", text["fbw.l.dialog"]);
  root.append(panel);

  let frame: HTMLIFrameElement | null = null;
  let ready = false;
  let context: Record<string, string> = {};
  let playerToken = "";
  let shotFn: ShotFn | null = null;
  let pending: OpenOptions | undefined;
  const canvasSel = ds.canvas;

  const post = (msg: Record<string, unknown>) => frame?.contentWindow?.postMessage({ phx: "fb", ...msg }, origin);

  function pageContext() {
    return {
      page: location.href.split("#")[0].slice(0, 500),
      pageTitle: document.title.slice(0, 200),
      viewport: `${innerWidth}×${innerHeight}`,
      screen: `${screen.width}×${screen.height}`,
      pixelRatio: String(devicePixelRatio || 1),
      language: navigator.language,
      userAgent: navigator.userAgent.slice(0, 200),
      ...context,
    };
  }

  const hostShot = () => !!shotFn || !!(canvasSel && document.querySelector(canvasSel));

  async function takeHostShot(): Promise<Blob | null> {
    try {
      if (shotFn) return await toBlob(await shotFn());
      const c = canvasSel ? document.querySelector<HTMLCanvasElement>(canvasSel) : null;
      return c ? await toBlob(c) : null;
    } catch {
      return null;
    }
  }

  async function sendInit() {
    const o = pending;
    pending = undefined;
    const screenshot = o?.screenshot ? await toBlob(o.screenshot) : null;
    post({ t: "init", context: pageContext(), playerToken, hostShot: hostShot(), type: o?.type, title: o?.title, screenshot });
  }

  addEventListener("message", (e: MessageEvent) => {
    if (!frame || e.source !== frame.contentWindow || e.origin !== origin) return;
    const m = e.data as { phx?: string; t?: string; big?: boolean };
    if (m?.phx !== "fb") return;
    switch (m.t) {
      case "ready":
        ready = true;
        void sendInit();
        break;
      case "close":
        api.close();
        break;
      case "hide": // the frame captures the tab: get out of the picture
        panel.classList.add("capturing");
        setTimeout(() => post({ t: "hidden" }), 120);
        break;
      case "show":
        panel.classList.remove("capturing");
        break;
      case "size":
        panel.classList.toggle("big", !!m.big);
        break;
      case "shot":
        void takeHostShot().then((blob) => post({ t: "shot", blob }));
        break;
    }
  });

  addEventListener("keydown", (e: KeyboardEvent) => {
    if (e.key === "Escape" && panel.classList.contains("open")) api.close();
  });

  const api: Api = {
    open(o) {
      pending = o ?? pending;
      if (!frame) {
        frame = document.createElement("iframe");
        frame.title = text["fbw.l.dialog"];
        frame.allow = "display-capture; clipboard-write";
        frame.src = `${origin}/feedback/${portal}/embed?lang=${lang}`;
        panel.append(frame);
      } else if (ready) {
        void sendInit();
      }
      panel.classList.add("open");
      button?.setAttribute("aria-expanded", "true");
      frame.focus();
    },
    close() {
      panel.classList.remove("open", "big", "capturing");
      button?.setAttribute("aria-expanded", "false");
      button?.focus();
    },
    setContext(ctx) {
      const out: Record<string, string> = {};
      for (const [k, v] of Object.entries(ctx ?? {}).slice(0, 15)) {
        if (/^[A-Za-z0-9_.-]{1,40}$/.test(k) && v != null) out[k] = String(v).slice(0, 500);
      }
      context = out;
      if (ready) post({ t: "context", context: pageContext() });
    },
    setPlayerToken(token) {
      playerToken = String(token ?? "");
      if (ready) post({ t: "context", context: pageContext(), playerToken });
    },
    setScreenshot(fn) {
      shotFn = fn;
      if (ready) post({ t: "context", context: pageContext(), hostShot: hostShot() });
    },
  };
  window.PhoedoxFeedback = api;
  const mount = () => {
    document.body.append(host);
    dispatchEvent(new CustomEvent("phoedox-feedback-ready", { detail: api }));
  };
  if (document.body) mount();
  else addEventListener("DOMContentLoaded", mount, { once: true });
}

init();
